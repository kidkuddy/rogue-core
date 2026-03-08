package core

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func setupHelmetTest(t *testing.T) (Store, *defaultHelmet, string) {
	t.Helper()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)

	// Create a test power file
	powersDir := filepath.Join(dir, "powers")
	os.MkdirAll(powersDir, 0755)
	os.WriteFile(filepath.Join(powersDir, "memory.md"), []byte(`---
name: memory
description: Raw data access
namespace: power:memory
tools:
  - rogue-store__sql
  - rogue-store__file_read
  - rogue-store__file_write
directories:
  - /data/files
---

## Instructions

Use SQL for structured data queries.
`), 0644)

	os.WriteFile(filepath.Join(powersDir, "queue.md"), []byte(`---
name: queue
description: Task queue management
namespace: power:queue
tools:
  - rogue-queue__push
  - rogue-queue__peek
  - rogue-queue__resolve
---

## Instructions

Push items to queues for tracking.
`), 0644)

	// Create a test agent persona
	agentsDir := filepath.Join(dir, "agents")
	os.MkdirAll(agentsDir, 0755)
	os.WriteFile(filepath.Join(agentsDir, "rogue.md"), []byte("You are Rogue. Southern charm, direct."), 0644)

	helmet := NewHelmet(store, func(uid string) bool {
		return uid == "root-user"
	}, logger, WithPowersDir(powersDir), WithAgentsDir(agentsDir)).(*defaultHelmet)

	return store, helmet, dir
}

func TestHelmetUserCreation(t *testing.T) {
	store, helmet, _ := setupHelmetTest(t)
	defer store.Close()

	msg := Message{
		ID:        "msg-1",
		SourceID:  "telegram:bot1",
		AgentID:   "rogue",
		ChannelID: "chan-100",
		UserID:    "user-42",
		ChatType:  "private",
		Text:      "hello",
		Reply:     true,
		Metadata:  map[string]any{"username": "alice", "first_name": "Alice"},
	}

	enriched, err := helmet.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}

	if enriched.User.ID != "user-42" {
		t.Errorf("expected user ID user-42, got %s", enriched.User.ID)
	}
	if enriched.User.Username != "alice" {
		t.Errorf("expected username alice, got %s", enriched.User.Username)
	}
	if enriched.User.FirstName != "Alice" {
		t.Errorf("expected first_name Alice, got %s", enriched.User.FirstName)
	}
}

func TestHelmetSessionMapping(t *testing.T) {
	store, helmet, _ := setupHelmetTest(t)
	defer store.Close()

	msg := Message{
		ID: "msg-1", SourceID: "telegram:bot1", AgentID: "rogue",
		ChannelID: "chan-100", UserID: "user-1", ChatType: "private",
		Text: "first", Reply: true,
	}

	enriched1, err := helmet.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("first process: %v", err)
	}

	// Same source+channel+agent should get same chat_id
	msg.ID = "msg-2"
	msg.Text = "second"
	enriched2, err := helmet.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("second process: %v", err)
	}

	if enriched1.ChatID != enriched2.ChatID {
		t.Errorf("same source+channel+agent should map to same chat_id: %s vs %s",
			enriched1.ChatID, enriched2.ChatID)
	}

	// Different agent should get different chat_id
	msg.AgentID = "doom"
	enriched3, err := helmet.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("third process: %v", err)
	}

	if enriched1.ChatID == enriched3.ChatID {
		t.Error("different agent should get different chat_id")
	}

	// Different source should get different chat_id
	msg.AgentID = "rogue"
	msg.SourceID = "slack:workspace1"
	enriched4, err := helmet.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("fourth process: %v", err)
	}

	if enriched1.ChatID == enriched4.ChatID {
		t.Error("different source should get different chat_id")
	}
}

func TestHelmetRootResolver(t *testing.T) {
	store, helmet, _ := setupHelmetTest(t)
	defer store.Close()

	// Root user
	msg := Message{
		ID: "msg-1", SourceID: "test:src", AgentID: "rogue",
		ChannelID: "chan-1", UserID: "root-user", ChatType: "private",
		Text: "hello", Reply: true,
	}
	enriched, _ := helmet.Process(context.Background(), msg)
	if !enriched.IsRoot {
		t.Error("root-user should be root")
	}

	// Non-root user
	msg.UserID = "regular-user"
	enriched, _ = helmet.Process(context.Background(), msg)
	if enriched.IsRoot {
		t.Error("regular-user should not be root")
	}
}

func TestHelmetAgentPersona(t *testing.T) {
	store, helmet, _ := setupHelmetTest(t)
	defer store.Close()

	msg := Message{
		ID: "msg-1", SourceID: "test:src", AgentID: "rogue",
		ChannelID: "chan-1", UserID: "user-1", ChatType: "private",
		Text: "hello", Reply: true,
	}
	enriched, _ := helmet.Process(context.Background(), msg)

	if enriched.Agent.ID != "rogue" {
		t.Errorf("expected agent ID rogue, got %s", enriched.Agent.ID)
	}
	if enriched.Agent.Persona == "" {
		t.Error("expected non-empty persona for rogue")
	}

	// Non-existent agent
	msg.AgentID = "nonexistent"
	enriched, _ = helmet.Process(context.Background(), msg)
	if enriched.Agent.Persona != "" {
		t.Error("expected empty persona for nonexistent agent")
	}
}

func TestHelmetPowerResolution(t *testing.T) {
	store, helmet, _ := setupHelmetTest(t)
	defer store.Close()

	// Assign powers
	helmet.AssignPower("rogue", "user-1", "chan-1", "memory", "admin")
	helmet.AssignPower("rogue", "user-1", "chan-1", "queue", "admin")

	msg := Message{
		ID: "msg-1", SourceID: "test:src", AgentID: "rogue",
		ChannelID: "chan-1", UserID: "user-1", ChatType: "private",
		Text: "hello", Reply: true,
	}
	enriched, err := helmet.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}

	if len(enriched.PowerSet.Powers) != 2 {
		t.Errorf("expected 2 powers, got %d", len(enriched.PowerSet.Powers))
	}

	// Should have union of tools
	expectedTools := map[string]bool{
		"rogue-store__sql":        true,
		"rogue-store__file_read":  true,
		"rogue-store__file_write": true,
		"rogue-queue__push":       true,
		"rogue-queue__peek":       true,
		"rogue-queue__resolve":    true,
	}
	for _, tool := range enriched.PowerSet.Tools {
		if !expectedTools[tool] {
			t.Errorf("unexpected tool: %s", tool)
		}
		delete(expectedTools, tool)
	}
	if len(expectedTools) > 0 {
		t.Errorf("missing tools: %v", expectedTools)
	}

	if enriched.PowerSet.Instructions == "" {
		t.Error("expected non-empty combined instructions")
	}
}

func TestHelmetGlobalPowers(t *testing.T) {
	store, helmet, _ := setupHelmetTest(t)
	defer store.Close()

	// Assign global power (channel_id = "")
	helmet.AssignPower("rogue", "user-1", "", "memory", "admin")

	msg := Message{
		ID: "msg-1", SourceID: "test:src", AgentID: "rogue",
		ChannelID: "any-channel", UserID: "user-1", ChatType: "private",
		Text: "hello", Reply: true,
	}
	enriched, err := helmet.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}

	if len(enriched.PowerSet.Powers) != 1 {
		t.Errorf("expected 1 global power, got %d", len(enriched.PowerSet.Powers))
	}
}

func TestHelmetTags(t *testing.T) {
	store, helmet, _ := setupHelmetTest(t)
	defer store.Close()

	msg := Message{
		ID: "msg-1", SourceID: "telegram:bot1", AgentID: "rogue",
		ChannelID: "chan-1", UserID: "user-1", ChatType: "private",
		Text: "hello", Reply: true,
	}
	enriched, _ := helmet.Process(context.Background(), msg)

	// Should have auto-generated tags
	tagSet := make(map[string]bool)
	for _, tag := range enriched.Tags {
		tagSet[tag] = true
	}

	expected := []string{
		"user:user-1",
		"source:telegram:bot1",
		"channel:chan-1",
		"agent:rogue",
	}
	for _, exp := range expected {
		if !tagSet[exp] {
			t.Errorf("missing auto-generated tag: %s", exp)
		}
	}
}

func TestHelmetPowerRevoke(t *testing.T) {
	store, helmet, _ := setupHelmetTest(t)
	defer store.Close()

	// Assign then revoke
	helmet.AssignPower("rogue", "user-1", "chan-1", "memory", "admin")
	helmet.RevokePower("rogue", "user-1", "chan-1", "memory")

	msg := Message{
		ID: "msg-1", SourceID: "test:src", AgentID: "rogue",
		ChannelID: "chan-1", UserID: "user-1", ChatType: "private",
		Text: "hello", Reply: true,
	}
	enriched, err := helmet.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}

	if len(enriched.PowerSet.Powers) != 0 {
		t.Errorf("expected 0 powers after revoke, got %d", len(enriched.PowerSet.Powers))
	}
}
