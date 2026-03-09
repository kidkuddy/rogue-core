package core

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type defaultCerebro struct {
	store           Store
	providers       map[string]AgentProvider
	defaultProvider string
	mcpRegistry     MCPRegistry
	maxTurns        int
	maxDepth        int
	rootPrompt      string // loaded ROOT.md content
	useRootPrompt   bool   // whether to apply ROOT.md (default true)
	prependPersona  bool   // prepend persona if no {{agent_persona}} (default true)
	logger          *slog.Logger
}

// CerebroOption configures cerebro behavior.
type CerebroOption func(*defaultCerebro)

// WithRootPromptConfig loads ROOT.md and configures template behavior.
func WithRootPromptConfig(path string, enabled bool, prependPersona bool) CerebroOption {
	return func(c *defaultCerebro) {
		c.useRootPrompt = enabled
		c.prependPersona = prependPersona
		if !enabled || path == "" {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			c.logger.Warn("ROOT.md not found, skipping", "path", path, "error", err)
			return
		}
		c.rootPrompt = strings.TrimSpace(string(data))
		c.logger.Info("ROOT.md loaded", "path", path, "size", len(c.rootPrompt))
	}
}

func NewCerebro(store Store, defaultProvider AgentProvider, mcpRegistry MCPRegistry, maxTurns int, maxDepth int, logger *slog.Logger, opts ...CerebroOption) Cerebro {
	c := &defaultCerebro{
		store:           store,
		providers:       make(map[string]AgentProvider),
		defaultProvider: defaultProvider.ID(),
		mcpRegistry:     mcpRegistry,
		maxTurns:        maxTurns,
		maxDepth:        maxDepth,
		useRootPrompt:   true, // default
		prependPersona:  true, // default
		logger:          logger,
	}
	c.providers[defaultProvider.ID()] = defaultProvider
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *defaultCerebro) RegisterProvider(provider AgentProvider) {
	c.providers[provider.ID()] = provider
	c.logger.Info("provider registered", "provider_id", provider.ID())
}

func (c *defaultCerebro) Execute(ctx context.Context, msg *EnrichedMessage) (*AgentResult, error) {
	if msg.AgentTurnDepth > c.maxDepth {
		return nil, fmt.Errorf("agent turn depth %d exceeds max %d", msg.AgentTurnDepth, c.maxDepth)
	}

	// Select provider based on tags
	provider := c.providers[c.defaultProvider]
	for _, tag := range msg.Tags {
		if len(tag) > 9 && tag[:9] == "provider:" {
			providerID := tag[9:]
			if p, ok := c.providers[providerID]; ok {
				provider = p
			} else {
				c.logger.Warn("unknown provider in tag, using default",
					"requested", providerID, "default", c.defaultProvider)
			}
		}
	}

	c.logger.Info("executing agent",
		"chat_id", msg.ChatID,
		"agent_id", msg.Agent.ID,
		"provider", provider.ID(),
		"turn_depth", msg.AgentTurnDepth,
	)

	// Load conversation state from Store
	sessionState := c.loadSessionState(msg.ChatID)

	// Build env early so we can pass it to MCP config too
	sessionEnv := c.buildEnv(msg)

	// Generate dynamic MCP config — pass full session env so MCP tools get
	// ROGUE_CHANNEL_ID, ROGUE_MESSAGE_ID, TELEGRAM_BOT_TOKEN, etc.
	var mcpConfigPath string
	if c.mcpRegistry != nil && len(msg.PowerSet.Tools) > 0 {
		var err error
		mcpConfigPath, err = c.mcpRegistry.GenerateConfig(msg.PowerSet.Tools, sessionEnv)
		if err != nil {
			return nil, fmt.Errorf("failed to generate MCP config: %w", err)
		}
	}

	// Prepend session context to instructions so the agent knows who it's talking to
	sessionContext := fmt.Sprintf(`## Session Context

- **Your agent ID**: %s
- **User ID**: %s
- **Channel ID**: %s
- **Chat ID**: %s`,
		msg.Agent.ID, msg.UserID, msg.ChannelID, msg.ChatID)
	instructions := msg.PowerSet.Instructions
	if instructions != "" {
		instructions = sessionContext + "\n\n---\n\n" + instructions
	} else {
		instructions = sessionContext
	}

	// Build final persona (apply ROOT.md template if configured)
	persona := c.buildPersona(msg.Agent.Persona)

	req := AgentRequest{
		ChatID:        msg.ChatID,
		SessionState:  sessionState,
		Persona:       persona,
		Prompt:        msg.Text,
		Attachments:   msg.Attachments,
		Tools:         msg.PowerSet.Tools,
		Directories:   msg.PowerSet.Directories,
		Instructions:  instructions,
		MCPConfigPath: mcpConfigPath,
		Tags:          msg.Tags,
		MaxTurns:      c.maxTurns,
		Env: sessionEnv,
	}

	result, err := provider.Execute(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("agent execution failed: %w", err)
	}

	// Save conversation state
	if result.SessionState != nil {
		c.saveSessionState(msg.ChatID, result.SessionState)
	}

	c.logger.Info("agent execution complete",
		"chat_id", msg.ChatID,
		"turns", result.Usage.NumTurns,
		"cost_usd", result.Usage.CostUSD,
		"hit_max_turns", result.HitMaxTurns,
	)

	return result, nil
}

// buildPersona applies ROOT.md template to the agent persona.
// If ROOT.md contains {{agent_persona}}, it substitutes inline.
// Otherwise, if prependPersona is true, persona is prepended before ROOT.md.
func (c *defaultCerebro) buildPersona(agentPersona string) string {
	if !c.useRootPrompt || c.rootPrompt == "" {
		return agentPersona
	}

	if strings.Contains(c.rootPrompt, "{{agent_persona}}") {
		return strings.ReplaceAll(c.rootPrompt, "{{agent_persona}}", agentPersona)
	}

	if c.prependPersona && agentPersona != "" {
		return agentPersona + "\n\n---\n\n" + c.rootPrompt
	}

	return c.rootPrompt
}

func (c *defaultCerebro) buildEnv(msg *EnrichedMessage) map[string]string {
	env := map[string]string{
		"ROGUE_USER_ID":    msg.UserID,
		"ROGUE_CHANNEL_ID": msg.ChannelID,
		"ROGUE_AGENT_ID":   msg.Agent.ID,
		"ROGUE_CHAT_ID":    msg.ChatID,
		"ROGUE_SOURCE_ID":  msg.SourceID,
	}
	// Pass message ID from metadata if available (for reactions)
	if msg.Metadata != nil {
		if mid, ok := msg.Metadata["telegram_message_id"]; ok {
			env["ROGUE_MESSAGE_ID"] = fmt.Sprintf("%v", mid)
		}
	}
	// Merge source env (e.g., TELEGRAM_BOT_TOKEN)
	for k, v := range msg.SourceEnv {
		env[k] = v
	}
	return env
}

// --- Conversation State Persistence ---

func (c *defaultCerebro) ensureConversationSchema() *sql.DB {
	db, err := c.store.Namespace("conversations").DB()
	if err != nil {
		return nil
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		chat_id TEXT PRIMARY KEY,
		provider TEXT DEFAULT '',
		state TEXT DEFAULT '',
		updated_at DATETIME DEFAULT (datetime('now'))
	)`)
	return db
}

func (c *defaultCerebro) loadSessionState(chatID string) any {
	db := c.ensureConversationSchema()
	if db == nil {
		return nil
	}

	var state string
	err := db.QueryRow("SELECT state FROM sessions WHERE chat_id = ?", chatID).Scan(&state)
	if err != nil || state == "" {
		return nil
	}
	return state
}

func (c *defaultCerebro) saveSessionState(chatID string, state any) {
	db := c.ensureConversationSchema()
	if db == nil {
		return
	}

	stateStr := fmt.Sprintf("%v", state)
	db.Exec(`INSERT INTO sessions (chat_id, state, updated_at) VALUES (?, ?, datetime('now'))
		ON CONFLICT(chat_id) DO UPDATE SET state = excluded.state, updated_at = datetime('now')`,
		chatID, stateStr)

	c.logger.Info("session state saved", "chat_id", chatID)
}
