package core

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func setupPipelineTest(t *testing.T) (*slog.Logger, Store, string) {
	t.Helper()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	t.Cleanup(func() { store.Close() })
	return logger, store, dir
}

func TestPipelineEndToEnd(t *testing.T) {
	logger, store, _ := setupPipelineTest(t)

	telepath := NewTelepath(logger)

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

	rootResolver := func(userID string) bool { return userID == "user-456" }
	helmet := NewHelmet(store, rootResolver, logger)

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

	pipeline.Stop(ctx)
}

func TestPipelineReplySuppressed(t *testing.T) {
	logger, store, _ := setupPipelineTest(t)

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
				Reply:     false,
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
	logger, store, _ := setupPipelineTest(t)

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
				AgentTurnDepth: 5,
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
		t.Errorf("expected no responses (depth exceeded), got %d", len(source.Received))
	}

	pipeline.Stop(ctx)
}

func TestPipelineMultipleSources(t *testing.T) {
	logger, store, _ := setupPipelineTest(t)

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

	if len(source1.Received) > 0 && source1.Received[0].MessageID != "msg-t1" {
		t.Errorf("source1 got wrong message: %s", source1.Received[0].MessageID)
	}
	if len(source2.Received) > 0 && source2.Received[0].MessageID != "msg-t2" {
		t.Errorf("source2 got wrong message: %s", source2.Received[0].MessageID)
	}

	pipeline.Stop(ctx)
}

func TestPipelineWithPowers(t *testing.T) {
	logger, store, dir := setupPipelineTest(t)

	// Create power files
	os.MkdirAll(dir+"/powers", 0755)
	os.WriteFile(dir+"/powers/memory.md", []byte(`---
name: memory
tools:
  - rogue-store__sql
  - rogue-store__file_read
---

Use SQL for queries.
`), 0644)

	telepath := NewTelepath(logger)

	source := &StubSource{
		SourceID: "test:src",
		AgentID:  "rogue",
		Messages: []Message{
			{ID: "msg-p1", ChannelID: "chan-1", UserID: "user-1", ChatType: "private", Text: "test powers", Reply: true},
		},
		Logger: logger,
	}
	telepath.RegisterSource(source)

	helmet := NewHelmet(store, func(string) bool { return true }, logger,
		WithPowersDir(dir+"/powers"))

	// Assign power before processing
	helmet.(*defaultHelmet).AssignPower("rogue", "user-1", "chan-1", "memory", "admin")

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

	if len(source.Received) != 1 {
		t.Fatalf("expected 1 response, got %d", len(source.Received))
	}

	pipeline.Stop(ctx)
}

func TestPipelineApprovalGate(t *testing.T) {
	logger, store, _ := setupPipelineTest(t)

	telepath := NewTelepath(logger)

	source := &StubSource{
		SourceID: "test:approval",
		AgentID:  "rogue",
		Messages: []Message{
			{
				ID:        "msg-unapproved",
				ChannelID: "chan-1",
				UserID:    "stranger",
				ChatType:  "private",
				Text:      "hello",
				Reply:     true,
			},
		},
		Logger: logger,
	}
	telepath.RegisterSource(source)

	rootResolver := func(string) bool { return false }
	helmet := NewHelmet(store, rootResolver, logger)

	provider := &StubProvider{Logger: logger}
	cerebro := NewCerebro(store, provider, nil, 100, 3, logger)
	warp := NewWarp(telepath, store, logger)

	pipeline := NewPipeline(telepath, helmet, cerebro, warp, NewSchedule(store, logger), logger,
		WithRequireApprovalGate(true))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("pipeline start failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if len(source.Received) != 0 {
		t.Errorf("expected 0 responses for unapproved user, got %d", len(source.Received))
	}

	pipeline.Stop(ctx)
}

func TestPipelineApprovalGateRootBypass(t *testing.T) {
	logger, store, _ := setupPipelineTest(t)

	telepath := NewTelepath(logger)

	source := &StubSource{
		SourceID: "test:root-bypass",
		AgentID:  "rogue",
		Messages: []Message{
			{
				ID:        "msg-root",
				ChannelID: "chan-1",
				UserID:    "owner",
				ChatType:  "private",
				Text:      "root message",
				Reply:     true,
			},
		},
		Logger: logger,
	}
	telepath.RegisterSource(source)

	rootResolver := func(userID string) bool { return userID == "owner" }
	helmet := NewHelmet(store, rootResolver, logger)

	provider := &StubProvider{Logger: logger}
	cerebro := NewCerebro(store, provider, nil, 100, 3, logger)
	warp := NewWarp(telepath, store, logger)

	pipeline := NewPipeline(telepath, helmet, cerebro, warp, NewSchedule(store, logger), logger,
		WithRequireApprovalGate(true))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("pipeline start failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if len(source.Received) != 1 {
		t.Errorf("expected 1 response for root user, got %d", len(source.Received))
	}

	pipeline.Stop(ctx)
}
