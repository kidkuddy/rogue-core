package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestCerebroSessionStatePersistence(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	defer store.Close()

	// Provider that returns a session state
	provider := &StubProvider{Logger: logger}
	cerebro := NewCerebro(store, provider, nil, 100, 3, logger).(*defaultCerebro)

	msg := &EnrichedMessage{
		Message: Message{
			ID: "msg-1", SourceID: "test:src", AgentID: "rogue",
			ChannelID: "chan-1", UserID: "user-1", Text: "hello", Reply: true,
		},
		ChatID: "chat-uuid-1",
		Agent:  AgentConfig{ID: "rogue"},
	}

	// First execution — no session state
	result, err := cerebro.Execute(context.Background(), msg)
	if err != nil {
		t.Fatalf("first execution failed: %v", err)
	}
	if result.Content == "" {
		t.Error("expected non-empty response")
	}

	// Manually save state (simulating a provider that returns a session ID)
	cerebro.saveSessionState("chat-uuid-1", "claude-session-abc123")

	// Verify state was persisted
	loaded := cerebro.loadSessionState("chat-uuid-1")
	if loaded != "claude-session-abc123" {
		t.Errorf("expected session state 'claude-session-abc123', got '%v'", loaded)
	}

	// State should survive across calls (same store)
	loaded2 := cerebro.loadSessionState("chat-uuid-1")
	if loaded2 != loaded {
		t.Error("session state should be consistent")
	}

	// Different chat should have no state
	noState := cerebro.loadSessionState("different-chat")
	if noState != nil {
		t.Errorf("expected nil state for unknown chat, got %v", noState)
	}
}

func TestCerebroProviderRouting(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	defer store.Close()

	defaultProvider := &StubProvider{Logger: logger}
	cerebro := NewCerebro(store, defaultProvider, nil, 100, 3, logger)

	// Register a second provider
	customProvider := &customStubProvider{id: "my-agent", response: "custom response"}
	cerebro.RegisterProvider(customProvider)

	// Message with no provider tag — should use default
	msg := &EnrichedMessage{
		Message: Message{ID: "msg-1", Text: "hello", Reply: true},
		ChatID:  "chat-1",
		Agent:   AgentConfig{ID: "rogue"},
		Tags:    []string{"user:1", "agent:rogue"},
	}

	result, err := cerebro.Execute(context.Background(), msg)
	if err != nil {
		t.Fatalf("default provider failed: %v", err)
	}
	if !strings.Contains(result.Content, "[stub response] received:") || !strings.Contains(result.Content, "hello") {
		t.Errorf("expected stub response containing 'hello', got: %s", result.Content)
	}

	// Message with provider tag — should route to custom
	msg.Tags = append(msg.Tags, "provider:my-agent")
	msg.ChatID = "chat-2"

	result, err = cerebro.Execute(context.Background(), msg)
	if err != nil {
		t.Fatalf("custom provider failed: %v", err)
	}
	if result.Content != "custom response" {
		t.Errorf("expected 'custom response', got: %s", result.Content)
	}
}

func TestMCPRegistryGenerateConfig(t *testing.T) {
	dir := t.TempDir()
	registry := NewMCPRegistry(dir).(*defaultMCPRegistry)

	registry.RegisterServer("rogue-store", MCPTool{
		Command: "/bin/rogue-store",
		Env:     map[string]string{"ROGUE_DATA": "/data"},
	})
	registry.RegisterServer("rogue-scheduler", MCPTool{
		Command: "/bin/rogue-scheduler",
		Args:    []string{"--mode", "mcp"},
	})
	registry.RegisterServer("rogue-phd", MCPTool{
		Command: "/bin/rogue-phd",
	})

	// Request tools from store and scheduler only — phd should not appear
	tools := []string{
		"rogue-store__sql",
		"rogue-store__file_read",
		"rogue-scheduler__schedule",
		"rogue-scheduler__list_tasks",
	}

	configPath, err := registry.GenerateConfig(tools, map[string]string{"EXTRA": "val"})
	if err != nil {
		t.Fatalf("generate config failed: %v", err)
	}

	// Read and verify
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var config mcpConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	if len(config.MCPServers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(config.MCPServers))
	}

	if _, ok := config.MCPServers["rogue-store"]; !ok {
		t.Error("expected rogue-store in config")
	}
	if _, ok := config.MCPServers["rogue-scheduler"]; !ok {
		t.Error("expected rogue-scheduler in config")
	}
	if _, ok := config.MCPServers["rogue-phd"]; ok {
		t.Error("rogue-phd should NOT be in config (no tools requested)")
	}

	// Verify env merging
	storeServer := config.MCPServers["rogue-store"]
	if storeServer.Env["ROGUE_DATA"] != "/data" {
		t.Error("expected ROGUE_DATA=/data in store env")
	}
	if storeServer.Env["EXTRA"] != "val" {
		t.Error("expected EXTRA=val in store env (merged)")
	}
}

// customStubProvider returns a fixed response.
type customStubProvider struct {
	id       string
	response string
}

func (p *customStubProvider) ID() string { return p.id }
func (p *customStubProvider) Execute(ctx context.Context, req AgentRequest) (*AgentResult, error) {
	return &AgentResult{Content: p.response, Usage: Usage{NumTurns: 1}}, nil
}
func (p *customStubProvider) RegisterProvider(provider AgentProvider) {}
