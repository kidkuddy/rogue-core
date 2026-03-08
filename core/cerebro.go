package core

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

type defaultCerebro struct {
	store           Store
	providers       map[string]AgentProvider
	defaultProvider string
	mcpRegistry     MCPRegistry
	maxTurns        int
	maxDepth        int
	logger          *slog.Logger
}

func NewCerebro(store Store, defaultProvider AgentProvider, mcpRegistry MCPRegistry, maxTurns int, maxDepth int, logger *slog.Logger) Cerebro {
	c := &defaultCerebro{
		store:           store,
		providers:       make(map[string]AgentProvider),
		defaultProvider: defaultProvider.ID(),
		mcpRegistry:     mcpRegistry,
		maxTurns:        maxTurns,
		maxDepth:        maxDepth,
		logger:          logger,
	}
	c.providers[defaultProvider.ID()] = defaultProvider
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

	// Generate dynamic MCP config
	var mcpConfigPath string
	if c.mcpRegistry != nil && len(msg.PowerSet.Tools) > 0 {
		var err error
		mcpConfigPath, err = c.mcpRegistry.GenerateConfig(msg.PowerSet.Tools, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to generate MCP config: %w", err)
		}
	}

	req := AgentRequest{
		ChatID:        msg.ChatID,
		SessionState:  sessionState,
		Persona:       msg.Agent.Persona,
		Prompt:        msg.Text,
		Attachments:   msg.Attachments,
		Tools:         msg.PowerSet.Tools,
		Directories:   msg.PowerSet.Directories,
		Instructions:  msg.PowerSet.Instructions,
		MCPConfigPath: mcpConfigPath,
		Tags:          msg.Tags,
		MaxTurns:      c.maxTurns,
		Env: map[string]string{
			"ROGUE_USER_ID":    msg.UserID,
			"ROGUE_CHANNEL_ID": msg.ChannelID,
			"ROGUE_AGENT_ID":   msg.Agent.ID,
			"ROGUE_CHAT_ID":    msg.ChatID,
		},
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
