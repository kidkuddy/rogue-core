package core

import (
	"context"
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

// NewCerebro creates an orchestrator with a default provider.
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

// RegisterProvider adds an additional agent provider.
// Use tags like "provider:my-agent" on a session to route to it.
func (c *defaultCerebro) RegisterProvider(provider AgentProvider) {
	c.providers[provider.ID()] = provider
	c.logger.Info("provider registered", "provider_id", provider.ID())
}

func (c *defaultCerebro) Execute(ctx context.Context, msg *EnrichedMessage) (*AgentResult, error) {
	// Check agent-to-agent depth limit
	if msg.AgentTurnDepth > c.maxDepth {
		return nil, fmt.Errorf("agent turn depth %d exceeds max %d", msg.AgentTurnDepth, c.maxDepth)
	}

	// Select provider based on tags (look for "provider:<id>" tag)
	provider := c.providers[c.defaultProvider]
	for _, tag := range msg.Tags {
		if len(tag) > 9 && tag[:9] == "provider:" {
			providerID := tag[9:]
			if p, ok := c.providers[providerID]; ok {
				provider = p
			} else {
				c.logger.Warn("unknown provider in tag, using default",
					"requested", providerID,
					"default", c.defaultProvider,
				)
			}
		}
	}

	c.logger.Info("executing agent",
		"chat_id", msg.ChatID,
		"agent_id", msg.Agent.ID,
		"provider", provider.ID(),
		"turn_depth", msg.AgentTurnDepth,
	)

	// TODO: Load conversation state from Store("conversations")

	// TODO: Generate dynamic MCP config based on PowerSet
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
		SessionState:  nil, // TODO: load from Store("conversations")
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

	// TODO: Save conversation state to Store("conversations")

	c.logger.Info("agent execution complete",
		"chat_id", msg.ChatID,
		"turns", result.Usage.NumTurns,
		"cost_usd", result.Usage.CostUSD,
		"hit_max_turns", result.HitMaxTurns,
	)

	return result, nil
}
