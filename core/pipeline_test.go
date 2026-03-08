package core

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestPipelineEndToEnd(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Store (stub)
	store := NewStubStore()

	// Telepath
	telepath := NewTelepath(logger)

	// Source: emits one test message
	source := &StubSource{
		SourceID: "test:source1",
		AgentID:  "rogue",
		Messages: []Message{
			{
				ID:        "msg-001",
				ChannelID: "chan-123",
				UserID:    "user-456",
				ChatType:  "private",
				Text:      "hello from the test",
				Reply:     true,
			},
		},
		Logger: logger,
	}
	telepath.RegisterSource(source)

	// Helmet (default with stub store)
	rootResolver := func(userID string) bool {
		return userID == "user-456"
	}
	helmet := NewHelmet(store, rootResolver, logger)

	// Cerebro (with stub provider)
	provider := &StubProvider{Logger: logger}
	cerebro := NewCerebro(store, provider, nil, 100, 3, logger)

	// Warp
	warp := NewWarp(telepath, store, logger)

	// Pipeline
	pipeline := NewPipeline(telepath, helmet, cerebro, warp, NewSchedule(store, logger), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("pipeline start failed: %v", err)
	}

	// Wait for message to flow through
	time.Sleep(500 * time.Millisecond)

	// Verify response was received
	if len(source.Received) == 0 {
		t.Fatal("expected at least one response, got none")
	}

	resp := source.Received[0]
	if resp.MessageID != "msg-001" {
		t.Errorf("expected message_id msg-001, got %s", resp.MessageID)
	}
	if resp.TargetSource != "test:source1" {
		t.Errorf("expected target_source test:source1, got %s", resp.TargetSource)
	}
	if resp.Text == "" {
		t.Error("expected non-empty response text")
	}

	t.Logf("response: %s", resp.Text)

	pipeline.Stop(ctx)
}

func TestPipelineReplySuppressed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := NewStubStore()
	telepath := NewTelepath(logger)

	source := &StubSource{
		SourceID: "test:source2",
		AgentID:  "rogue",
		Messages: []Message{
			{
				ID:        "msg-002",
				ChannelID: "chan-789",
				UserID:    "user-100",
				ChatType:  "scheduled",
				Text:      "background task",
				Reply:     false, // suppressed
			},
		},
		Logger: logger,
	}
	telepath.RegisterSource(source)

	helmet := NewHelmet(store, func(string) bool { return false }, logger)
	provider := &StubProvider{Logger: logger}
	cerebro := NewCerebro(store, provider, nil, 100, 3, logger)
	warp := NewWarp(telepath, store, logger)
	pipeline := NewPipeline(telepath, helmet, cerebro, warp, NewSchedule(store, logger), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("pipeline start failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if len(source.Received) != 0 {
		t.Errorf("expected no responses (reply suppressed), got %d", len(source.Received))
	}

	pipeline.Stop(ctx)
}

func TestPipelineAgentDepthLimit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := NewStubStore()
	telepath := NewTelepath(logger)

	source := &StubSource{
		SourceID: "agent:doom",
		AgentID:  "rogue",
		Messages: []Message{
			{
				ID:             "msg-003",
				ChannelID:      "chan-agent",
				UserID:         "agent-doom",
				ChatType:       "agent",
				Text:           "agent-to-agent message",
				Reply:          true,
				AgentTurnDepth: 5, // exceeds default max of 3
			},
		},
		Logger: logger,
	}
	telepath.RegisterSource(source)

	helmet := NewHelmet(store, func(string) bool { return false }, logger)
	provider := &StubProvider{Logger: logger}
	cerebro := NewCerebro(store, provider, nil, 100, 3, logger)
	warp := NewWarp(telepath, store, logger)
	pipeline := NewPipeline(telepath, helmet, cerebro, warp, NewSchedule(store, logger), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("pipeline start failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Agent depth exceeded — Cerebro should reject, no response delivered
	if len(source.Received) != 0 {
		t.Errorf("expected no responses (depth exceeded), got %d", len(source.Received))
	}

	pipeline.Stop(ctx)
}

func TestPipelineMultipleSources(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := NewStubStore()
	telepath := NewTelepath(logger)

	source1 := &StubSource{
		SourceID: "telegram:bot-rogue",
		AgentID:  "rogue",
		Messages: []Message{
			{ID: "msg-t1", ChannelID: "tg-chan-1", UserID: "user-1", ChatType: "private", Text: "hello rogue", Reply: true},
		},
		Logger: logger,
	}
	source2 := &StubSource{
		SourceID: "telegram:bot-doom",
		AgentID:  "doom",
		Messages: []Message{
			{ID: "msg-t2", ChannelID: "tg-chan-2", UserID: "user-1", ChatType: "private", Text: "hello doom", Reply: true},
		},
		Logger: logger,
	}
	telepath.RegisterSource(source1)
	telepath.RegisterSource(source2)

	helmet := NewHelmet(store, func(string) bool { return true }, logger)
	provider := &StubProvider{Logger: logger}
	cerebro := NewCerebro(store, provider, nil, 100, 3, logger)
	warp := NewWarp(telepath, store, logger)
	pipeline := NewPipeline(telepath, helmet, cerebro, warp, NewSchedule(store, logger), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("pipeline start failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if len(source1.Received) != 1 {
		t.Errorf("source1: expected 1 response, got %d", len(source1.Received))
	}
	if len(source2.Received) != 1 {
		t.Errorf("source2: expected 1 response, got %d", len(source2.Received))
	}

	// Verify responses went to correct sources
	if len(source1.Received) > 0 && source1.Received[0].MessageID != "msg-t1" {
		t.Errorf("source1 got wrong message: %s", source1.Received[0].MessageID)
	}
	if len(source2.Received) > 0 && source2.Received[0].MessageID != "msg-t2" {
		t.Errorf("source2 got wrong message: %s", source2.Received[0].MessageID)
	}

	pipeline.Stop(ctx)
}
