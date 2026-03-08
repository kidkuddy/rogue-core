package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kidkuddy/rogue-core/core"
)

type Provider struct {
	Binary string // path to claude binary, default "claude"
	Logger *slog.Logger
}

func New(logger *slog.Logger) *Provider {
	return &Provider{
		Binary: "claude",
		Logger: logger,
	}
}

func (p *Provider) ID() string { return "claude-code" }

func (p *Provider) Execute(ctx context.Context, req core.AgentRequest) (*core.AgentResult, error) {
	start := time.Now()

	args := []string{
		"--print",
		"--output-format", "json",
		"--max-turns", fmt.Sprintf("%d", req.MaxTurns),
	}

	// Resume session if state exists
	if sessionID, ok := req.SessionState.(string); ok && sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	// System prompt: persona + instructions
	systemPrompt := req.Persona
	if req.Instructions != "" {
		if systemPrompt != "" {
			systemPrompt += "\n\n---\n\n"
		}
		systemPrompt += req.Instructions
	}
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	// Tool filtering: --allowedTools controls MCP tools, --disallowedTools controls builtins.
	// We always block builtins unless explicitly granted, and only allow granted MCP tools.
	builtins := []string{"Bash", "Edit", "Write", "Read", "Glob", "Grep",
		"Agent", "NotebookEdit", "WebFetch", "WebSearch"}

	// Separate MCP tools from builtin tools
	var mcpTools []string
	grantedBuiltins := make(map[string]bool)
	for _, t := range req.Tools {
		isBuiltin := false
		for _, b := range builtins {
			if t == b {
				isBuiltin = true
				grantedBuiltins[b] = true
				break
			}
		}
		if !isBuiltin {
			mcpTools = append(mcpTools, t)
		}
	}

	// Allow only granted MCP tools
	if len(mcpTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, mcpTools...)
	}

	// Block builtins not explicitly granted
	var blockedBuiltins []string
	for _, b := range builtins {
		if !grantedBuiltins[b] {
			blockedBuiltins = append(blockedBuiltins, b)
		}
	}
	if len(blockedBuiltins) > 0 {
		args = append(args, "--disallowedTools")
		args = append(args, blockedBuiltins...)
	}

	// Directory access
	for _, dir := range req.Directories {
		args = append(args, "--add-dir", dir)
	}

	// MCP config
	if req.MCPConfigPath != "" {
		args = append(args, "--mcp-config", req.MCPConfigPath)
	}

	p.Logger.Info("spawning claude",
		"chat_id", req.ChatID,
		"tools", len(req.Tools),
		"max_turns", req.MaxTurns,
		"resume", req.SessionState != nil,
	)

	cmd := exec.CommandContext(ctx, p.Binary, args...)
	cmd.Stdin = strings.NewReader(req.Prompt)

	// Build environment
	env := os.Environ()
	for k, v := range req.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("claude error: %s", stderr.String())
		}
		return nil, fmt.Errorf("claude execution failed: %w", err)
	}

	duration := time.Since(start)

	// Parse JSON response
	var resp claudeResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		p.Logger.Warn("json parse failed, using raw output", "error", err)
		return &core.AgentResult{
			Content: strings.TrimSpace(stdout.String()),
			Usage:   core.Usage{DurationMS: duration.Milliseconds()},
		}, nil
	}

	hitMaxTurns := resp.Subtype == "error_max_turns"
	content := resp.Result
	if strings.TrimSpace(content) == "" {
		if hitMaxTurns {
			content = fmt.Sprintf("Hit max turns (%d). Break this up.", resp.NumTurns)
		} else {
			content = "Done."
		}
	}

	p.Logger.Info("claude complete",
		"chat_id", req.ChatID,
		"turns", resp.NumTurns,
		"cost_usd", resp.TotalCostUSD,
		"duration_ms", duration.Milliseconds(),
		"hit_max_turns", hitMaxTurns,
	)

	return &core.AgentResult{
		Content:      content,
		SessionState: resp.SessionID,
		Usage: core.Usage{
			InputTokens:      int64(resp.Usage.InputTokens),
			OutputTokens:     int64(resp.Usage.OutputTokens),
			CacheReadTokens:  int64(resp.Usage.CacheReadInputTokens),
			CacheWriteTokens: int64(resp.Usage.CacheCreationInputTokens),
			CostUSD:          resp.TotalCostUSD,
			DurationMS:       duration.Milliseconds(),
			NumTurns:         resp.NumTurns,
		},
		HitMaxTurns: hitMaxTurns,
	}, nil
}

type claudeResponse struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	Result       string  `json:"result"`
	SessionID    string  `json:"session_id"`
	DurationMs   int     `json:"duration_ms"`
	NumTurns     int     `json:"num_turns"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Usage        struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}
