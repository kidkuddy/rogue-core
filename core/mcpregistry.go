package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type defaultMCPRegistry struct {
	tools  map[string]MCPTool // tool prefix -> server config
	tmpDir string
}

// NewMCPRegistry creates a registry that maps tool name prefixes to MCP server configs.
// Tool names follow the pattern "server-name__tool_name". The registry maps server names.
func NewMCPRegistry(tmpDir string) MCPRegistry {
	return &defaultMCPRegistry{
		tools:  make(map[string]MCPTool),
		tmpDir: tmpDir,
	}
}

// RegisterServer adds an MCP server to the registry.
// serverName is the prefix used in tool names (e.g., "rogue-store" for tools like "rogue-store__sql").
func (r *defaultMCPRegistry) RegisterServer(serverName string, tool MCPTool) {
	r.tools[serverName] = tool
}

// mcpConfig matches the .mcp.json format Claude Code expects.
type mcpConfig struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func (r *defaultMCPRegistry) GenerateConfig(tools []string, env map[string]string) (string, error) {
	// Determine which servers are needed
	neededServers := make(map[string]bool)
	for _, tool := range tools {
		parts := strings.SplitN(tool, "__", 2)
		if len(parts) >= 1 {
			neededServers[parts[0]] = true
		}
	}

	config := mcpConfig{
		MCPServers: make(map[string]mcpServerConfig),
	}

	for serverName := range neededServers {
		tool, ok := r.tools[serverName]
		if !ok {
			continue
		}

		serverEnv := make(map[string]string)
		for k, v := range tool.Env {
			serverEnv[k] = v
		}
		// Merge request-level env vars
		for k, v := range env {
			serverEnv[k] = v
		}

		config.MCPServers[serverName] = mcpServerConfig{
			Command: tool.Command,
			Args:    tool.Args,
			Env:     serverEnv,
		}
	}

	// Write to temp file
	if err := os.MkdirAll(r.tmpDir, 0755); err != nil {
		return "", fmt.Errorf("create mcp tmp dir: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}

	tmpFile := filepath.Join(r.tmpDir, fmt.Sprintf("mcp-%d.json", os.Getpid()))
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return "", err
	}

	return tmpFile, nil
}

// RegisterServer adds a server to the registry (exported for instance repos).
func RegisterServer(registry MCPRegistry, name string, tool MCPTool) {
	if r, ok := registry.(*defaultMCPRegistry); ok {
		r.RegisterServer(name, tool)
	}
}
