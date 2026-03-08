package core

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func setupScheduleTest(t *testing.T) (*slog.Logger, Store) {
	t.Helper()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	t.Cleanup(func() { store.Close() })
	return logger, store
}

func TestScheduleCreateAndList(t *testing.T) {
	logger, store := setupScheduleTest(t)
	sched := NewSchedule(store, logger)

	task := ScheduledTask{
		AgentID:      "rogue",
		UserID:       "user-1",
		ChannelID:    "chan-1",
		SourceID:     "telegram:rogue",
		MessageText:  "check something",
		ScheduledFor: time.Now().Add(1 * time.Hour),
		Reply:        true,
		Tags:         []string{"agent:rogue"},
	}

	id, err := sched.Create(task)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty task ID")
	}

	tasks, err := sched.List("pending")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].AgentID != "rogue" {
		t.Errorf("expected agent rogue, got %s", tasks[0].AgentID)
	}
	if tasks[0].MessageText != "check something" {
		t.Errorf("expected 'check something', got %s", tasks[0].MessageText)
	}
}

func TestScheduleCancel(t *testing.T) {
	logger, store := setupScheduleTest(t)
	sched := NewSchedule(store, logger)

	id, _ := sched.Create(ScheduledTask{
		AgentID:      "rogue",
		MessageText:  "will be cancelled",
		ScheduledFor: time.Now().Add(1 * time.Hour),
	})

	err := sched.Cancel(id)
	if err != nil {
		t.Fatalf("cancel failed: %v", err)
	}

	tasks, _ := sched.List("cancelled")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 cancelled task, got %d", len(tasks))
	}

	// Cancel again should fail
	err = sched.Cancel(id)
	if err == nil {
		t.Error("expected error cancelling already-cancelled task")
	}
}

func TestScheduleDelay(t *testing.T) {
	logger, store := setupScheduleTest(t)
	sched := NewSchedule(store, logger)

	original := time.Now().Add(1 * time.Hour)
	id, _ := sched.Create(ScheduledTask{
		AgentID:      "rogue",
		MessageText:  "delayed task",
		ScheduledFor: original,
	})

	err := sched.Delay(id, 30*time.Minute)
	if err != nil {
		t.Fatalf("delay failed: %v", err)
	}

	tasks, _ := sched.List("pending")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// The task should be scheduled later than the original time
	if !tasks[0].ScheduledFor.After(original.Add(29 * time.Minute)) {
		t.Errorf("task should be delayed by ~30min, got %v (original: %v)",
			tasks[0].ScheduledFor, original)
	}
}

func TestScheduleTickFiring(t *testing.T) {
	logger, store := setupScheduleTest(t)
	sched := NewSchedule(store, logger, WithTickInterval(100*time.Millisecond))

	// Create a task that's already due
	_, err := sched.Create(ScheduledTask{
		AgentID:      "rogue",
		UserID:       "user-1",
		ChannelID:    "chan-1",
		MessageText:  "overdue task",
		ScheduledFor: time.Now().Add(-1 * time.Minute), // already past
		Reply:        true,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	bus := make(chan Message, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = sched.Start(ctx, bus)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Wait for the task to fire
	select {
	case msg := <-bus:
		if msg.ChatType != "scheduled" {
			t.Errorf("expected chat_type 'scheduled', got '%s'", msg.ChatType)
		}
		if msg.Text != "overdue task" {
			t.Errorf("expected 'overdue task', got '%s'", msg.Text)
		}
		if msg.AgentID != "rogue" {
			t.Errorf("expected agent rogue, got %s", msg.AgentID)
		}
		if msg.SourceID != "scheduler" {
			t.Errorf("expected source 'scheduler', got '%s'", msg.SourceID)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for scheduled task to fire")
	}

	sched.Stop(ctx)

	// Task should be marked done
	tasks, _ := sched.List("done")
	if len(tasks) != 1 {
		t.Errorf("expected 1 done task, got %d", len(tasks))
	}
}

func TestScheduleCronReschedule(t *testing.T) {
	logger, store := setupScheduleTest(t)
	sched := NewSchedule(store, logger, WithTickInterval(100*time.Millisecond))

	_, err := sched.Create(ScheduledTask{
		AgentID:      "rogue",
		MessageText:  "cron task",
		ScheduledFor: time.Now().Add(-1 * time.Minute),
		CronExpr:     "0 9 * * *", // daily at 9am
		Reply:        false,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	bus := make(chan Message, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sched.Start(ctx, bus)

	// Wait for fire
	select {
	case <-bus:
		// fired
	case <-ctx.Done():
		t.Fatal("timeout waiting for cron task")
	}

	sched.Stop(ctx)

	// Should be rescheduled as pending, not done
	pending, _ := sched.List("pending")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending (rescheduled) task, got %d", len(pending))
	}
	if pending[0].ScheduledFor.Before(time.Now()) {
		t.Errorf("rescheduled task should be in the future, got %v", pending[0].ScheduledFor)
	}
}

func TestScheduleListAll(t *testing.T) {
	logger, store := setupScheduleTest(t)
	sched := NewSchedule(store, logger)

	sched.Create(ScheduledTask{AgentID: "a", MessageText: "t1", ScheduledFor: time.Now().Add(1 * time.Hour)})
	sched.Create(ScheduledTask{AgentID: "b", MessageText: "t2", ScheduledFor: time.Now().Add(2 * time.Hour)})

	id3, _ := sched.Create(ScheduledTask{AgentID: "c", MessageText: "t3", ScheduledFor: time.Now().Add(3 * time.Hour)})
	sched.Cancel(id3)

	// List all (no status filter)
	all, _ := sched.List("")
	if len(all) != 3 {
		t.Errorf("expected 3 total tasks, got %d", len(all))
	}

	pending, _ := sched.List("pending")
	if len(pending) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(pending))
	}
}
