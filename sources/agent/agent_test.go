package agent

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/kidkuddy/rogue-core/core"
)

func TestAgentSourceID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	src := New("rogue", logger)
	if src.ID() != "agent:rogue" {
		t.Errorf("expected agent:rogue, got %s", src.ID())
	}
}

func TestAgentSourceSend(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	src := New("doom", logger)

	inbound := make(chan core.Message, 10)
	ctx := context.Background()
	src.Start(ctx, inbound)

	// Simulate a response from agent "rogue" to agent "doom"
	resp := core.Response{
		MessageID:    "msg-from-rogue",
		TargetSource: "agent:doom",
		ChannelID:    "agent:rogue->doom",
		Text:         "hey doom, handle this",
		Metadata: map[string]any{
			"sender_agent":    "rogue",
			"agent_turn_depth": 0,
		},
	}

	err := src.Send(ctx, resp)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	select {
	case msg := <-inbound:
		if msg.AgentID != "doom" {
			t.Errorf("expected agent doom, got %s", msg.AgentID)
		}
		if msg.ChatType != "agent" {
			t.Errorf("expected chat_type agent, got %s", msg.ChatType)
		}
		if msg.AgentTurnDepth != 1 {
			t.Errorf("expected depth 1 (incremented from 0), got %d", msg.AgentTurnDepth)
		}
		if msg.Text != "hey doom, handle this" {
			t.Errorf("expected message text, got %s", msg.Text)
		}
		if msg.SourceID != "agent:doom" {
			t.Errorf("expected source agent:doom, got %s", msg.SourceID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestAgentSourceEmit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	src := New("rogue", logger)

	inbound := make(chan core.Message, 10)
	ctx := context.Background()
	src.Start(ctx, inbound)

	src.Emit(ctx, "doom", "proactive message", 0)

	select {
	case msg := <-inbound:
		if msg.AgentID != "doom" {
			t.Errorf("expected target agent doom, got %s", msg.AgentID)
		}
		if msg.UserID != "rogue" {
			t.Errorf("expected sender rogue, got %s", msg.UserID)
		}
		if msg.AgentTurnDepth != 0 {
			t.Errorf("expected depth 0, got %d", msg.AgentTurnDepth)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestAgentSourceDepthIncrement(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	src := New("target", logger)

	inbound := make(chan core.Message, 10)
	ctx := context.Background()
	src.Start(ctx, inbound)

	// Simulate depth=2 coming in
	resp := core.Response{
		Text: "chain message",
		Metadata: map[string]any{
			"agent_turn_depth": 2,
			"sender_agent":    "middle-agent",
		},
	}
	src.Send(ctx, resp)

	msg := <-inbound
	if msg.AgentTurnDepth != 3 {
		t.Errorf("expected depth 3 (2+1), got %d", msg.AgentTurnDepth)
	}
}

func TestAgentSourceNotStarted(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	src := New("offline", logger)

	err := src.Send(context.Background(), core.Response{Text: "should fail"})
	if err == nil {
		t.Error("expected error when source not started")
	}
}
