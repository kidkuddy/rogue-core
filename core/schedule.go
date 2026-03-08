package core

import (
	"context"
	"log/slog"
	"time"
)

type defaultSchedule struct {
	store  Store
	logger *slog.Logger
}

func NewSchedule(store Store, logger *slog.Logger) Schedule {
	return &defaultSchedule{
		store:  store,
		logger: logger,
	}
}

func (s *defaultSchedule) Start(ctx context.Context, bus chan<- Message) error {
	s.logger.Info("schedule started (stub)")
	// TODO: Tick loop — check Store("scheduler") for due tasks, emit to bus
	return nil
}

func (s *defaultSchedule) Stop(ctx context.Context) error {
	s.logger.Info("schedule stopped")
	return nil
}

func (s *defaultSchedule) Create(task ScheduledTask) (string, error) {
	s.logger.Info("task created (stub)", "agent_id", task.AgentID, "scheduled_for", task.ScheduledFor)
	// TODO: Insert into Store("scheduler")
	return "stub-task-id", nil
}

func (s *defaultSchedule) Cancel(taskID string) error {
	s.logger.Info("task cancelled (stub)", "task_id", taskID)
	return nil
}

func (s *defaultSchedule) List(status string) ([]ScheduledTask, error) {
	s.logger.Info("listing tasks (stub)", "status", status)
	return nil, nil
}

func (s *defaultSchedule) Delay(taskID string, duration time.Duration) error {
	s.logger.Info("task delayed (stub)", "task_id", taskID, "duration", duration)
	return nil
}
