package core

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func setupWarpTest(t *testing.T) (*slog.Logger, Store, Telepath) {
	t.Helper()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	t.Cleanup(func() { store.Close() })

	telepath := NewTelepath(logger)
	return logger, store, telepath
}

func TestWarpBasicResponse(t *testing.T) {
	logger, store, telepath := setupWarpTest(t)

	source := &StubSource{SourceID: "test:src", AgentID: "rogue", Logger: logger}
	telepath.RegisterSource(source)

	warp := NewWarp(telepath, store, logger)

	msg := &EnrichedMessage{
		Message: Message{
			ID:       "msg-1",
			SourceID: "test:src",
			AgentID:  "rogue",
			ChannelID: "chan-1",
			UserID:   "user-1",
			Text:     "hello",
			Reply:    true,
		},
		ChatID: "chat-1",
		Agent:  AgentConfig{ID: "rogue"},
		Tags:   []string{"user:1", "agent:rogue"},
	}

	result := &AgentResult{
		Content: "hi there",
		Usage: Usage{
			InputTokens:  100,
			OutputTokens: 50,
			CostUSD:      0.01,
			DurationMS:   500,
			NumTurns:     1,
		},
	}

	err := warp.Handle(context.Background(), msg, result)
	if err != nil {
		t.Fatalf("warp handle failed: %v", err)
	}

	if len(source.Received) != 1 {
		t.Fatalf("expected 1 response, got %d", len(source.Received))
	}

	resp := source.Received[0]
	if resp.Text != "hi there" {
		t.Errorf("expected 'hi there', got '%s'", resp.Text)
	}
	if resp.TargetSource != "test:src" {
		t.Errorf("expected target test:src, got %s", resp.TargetSource)
	}
	if resp.ChannelID != "chan-1" {
		t.Errorf("expected channel chan-1, got %s", resp.ChannelID)
	}
}

func TestWarpReplySuppressed(t *testing.T) {
	logger, store, telepath := setupWarpTest(t)

	source := &StubSource{SourceID: "test:src", AgentID: "rogue", Logger: logger}
	telepath.RegisterSource(source)

	warp := NewWarp(telepath, store, logger)

	msg := &EnrichedMessage{
		Message: Message{
			ID:    "msg-2",
			Reply: false, // suppressed
		},
		ChatID: "chat-2",
		Agent:  AgentConfig{ID: "rogue"},
	}

	result := &AgentResult{Content: "should not be sent", Usage: Usage{NumTurns: 1}}

	err := warp.Handle(context.Background(), msg, result)
	if err != nil {
		t.Fatalf("warp handle failed: %v", err)
	}

	if len(source.Received) != 0 {
		t.Errorf("expected 0 responses (suppressed), got %d", len(source.Received))
	}
}

func TestWarpUsageRecording(t *testing.T) {
	logger, store, telepath := setupWarpTest(t)
	warp := NewWarp(telepath, store, logger)

	msg := &EnrichedMessage{
		Message: Message{ID: "msg-3", SourceID: "test:src", UserID: "user-1", Reply: false},
		ChatID:  "chat-3",
		Agent:   AgentConfig{ID: "rogue"},
		Tags:    []string{"user:1", "agent:rogue"},
	}

	result := &AgentResult{
		Content: "done",
		Usage: Usage{
			InputTokens:  500,
			OutputTokens: 200,
			CostUSD:      0.05,
			DurationMS:   2000,
			NumTurns:     3,
		},
	}

	_ = warp.Handle(context.Background(), msg, result)

	// Query usage
	summary, err := QueryUsage(store, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("query usage failed: %v", err)
	}

	if summary.ExecutionCount != 1 {
		t.Errorf("expected 1 execution, got %d", summary.ExecutionCount)
	}
	if summary.TotalCostUSD != 0.05 {
		t.Errorf("expected cost 0.05, got %f", summary.TotalCostUSD)
	}
	if summary.TotalInputTokens != 500 {
		t.Errorf("expected 500 input tokens, got %d", summary.TotalInputTokens)
	}
	if summary.TotalTurns != 3 {
		t.Errorf("expected 3 turns, got %d", summary.TotalTurns)
	}
}

func TestWarpUsageWithTagFilter(t *testing.T) {
	logger, store, telepath := setupWarpTest(t)
	warp := NewWarp(telepath, store, logger)

	// Record usage for two different agents
	for _, agent := range []string{"rogue", "doom"} {
		msg := &EnrichedMessage{
			Message: Message{ID: "msg-" + agent, SourceID: "test:src", UserID: "user-1", Reply: false},
			ChatID:  "chat-" + agent,
			Agent:   AgentConfig{ID: agent},
			Tags:    []string{"agent:" + agent},
		}
		result := &AgentResult{Usage: Usage{CostUSD: 0.10, NumTurns: 1}}
		_ = warp.Handle(context.Background(), msg, result)
	}

	// Query all
	all, _ := QueryUsage(store, time.Now().Add(-1*time.Hour))
	if all.ExecutionCount != 2 {
		t.Errorf("expected 2 total executions, got %d", all.ExecutionCount)
	}

	// Query filtered by agent tag
	rogue, _ := QueryUsage(store, time.Now().Add(-1*time.Hour), "agent:rogue")
	if rogue.ExecutionCount != 1 {
		t.Errorf("expected 1 rogue execution, got %d", rogue.ExecutionCount)
	}
}

func TestWarpCrossChannelRedirect(t *testing.T) {
	logger, store, telepath := setupWarpTest(t)

	source1 := &StubSource{SourceID: "telegram:rogue", AgentID: "rogue", Logger: logger}
	source2 := &StubSource{SourceID: "telegram:doom", AgentID: "doom", Logger: logger}
	telepath.RegisterSource(source1)
	telepath.RegisterSource(source2)

	warp := NewWarp(telepath, store, logger)

	msg := &EnrichedMessage{
		Message: Message{
			ID:        "msg-cc",
			SourceID:  "telegram:rogue",
			ChannelID: "chan-rogue",
			UserID:    "user-1",
			Reply:     true,
		},
		ChatID: "chat-cc",
		Agent:  AgentConfig{ID: "rogue"},
	}

	result := &AgentResult{
		Content: "cross-channel message",
		Usage:   Usage{NumTurns: 1},
		Metadata: map[string]any{
			"cross_channel": map[string]any{
				"target_source":  "telegram:doom",
				"target_channel": "chan-doom",
			},
		},
	}

	err := warp.Handle(context.Background(), msg, result)
	if err != nil {
		t.Fatalf("warp handle failed: %v", err)
	}

	// Should NOT go to source1 (original source)
	if len(source1.Received) != 0 {
		t.Errorf("source1 should not receive cross-channel response, got %d", len(source1.Received))
	}

	// Should go to source2 (redirected)
	if len(source2.Received) != 1 {
		t.Fatalf("source2 should receive 1 response, got %d", len(source2.Received))
	}

	resp := source2.Received[0]
	if resp.ChannelID != "chan-doom" {
		t.Errorf("expected channel chan-doom, got %s", resp.ChannelID)
	}
	if resp.Text != "cross-channel message" {
		t.Errorf("expected 'cross-channel message', got '%s'", resp.Text)
	}
}

func TestWarpSuppressedStillRecordsUsage(t *testing.T) {
	logger, store, telepath := setupWarpTest(t)
	warp := NewWarp(telepath, store, logger)

	msg := &EnrichedMessage{
		Message: Message{ID: "msg-sup", Reply: false, SourceID: "sched", UserID: "user-1"},
		ChatID:  "chat-sup",
		Agent:   AgentConfig{ID: "rogue"},
		Tags:    []string{"agent:rogue"},
	}

	result := &AgentResult{
		Content: "background result",
		Usage:   Usage{CostUSD: 0.25, NumTurns: 5},
	}

	_ = warp.Handle(context.Background(), msg, result)

	summary, err := QueryUsage(store, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("query usage failed: %v", err)
	}

	if summary.ExecutionCount != 1 {
		t.Errorf("usage should be recorded even when reply suppressed, got %d executions", summary.ExecutionCount)
	}
	if summary.TotalCostUSD != 0.25 {
		t.Errorf("expected cost 0.25, got %f", summary.TotalCostUSD)
	}
}
