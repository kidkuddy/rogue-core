package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type defaultSchedule struct {
	store    Store
	logger   *slog.Logger
	tick     time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// ScheduleOption configures schedule behavior.
type ScheduleOption func(*defaultSchedule)

// WithTickInterval sets the scheduler tick interval (default 30s).
func WithTickInterval(d time.Duration) ScheduleOption {
	return func(s *defaultSchedule) { s.tick = d }
}

func NewSchedule(store Store, logger *slog.Logger, opts ...ScheduleOption) Schedule {
	s := &defaultSchedule{
		store:  store,
		logger: logger,
		tick:   30 * time.Second,
		stopCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *defaultSchedule) ensureSchema() *sql.DB {
	db, err := s.store.Namespace("scheduler").DB()
	if err != nil {
		return nil
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		agent_id TEXT NOT NULL,
		user_id TEXT DEFAULT '',
		channel_id TEXT DEFAULT '',
		source_id TEXT DEFAULT '',
		message_text TEXT NOT NULL,
		scheduled_for DATETIME NOT NULL,
		cron_expr TEXT DEFAULT '',
		reply BOOLEAN DEFAULT TRUE,
		queue TEXT DEFAULT '',
		status TEXT DEFAULT 'pending',
		tags TEXT DEFAULT '[]',
		created_at DATETIME DEFAULT (datetime('now')),
		updated_at DATETIME DEFAULT (datetime('now'))
	)`)
	return db
}

func (s *defaultSchedule) Start(ctx context.Context, bus chan<- Message) error {
	db := s.ensureSchema()
	if db == nil {
		return fmt.Errorf("failed to initialize scheduler database")
	}

	// Recover tasks stuck in "running" from a previous crash
	db.Exec(`UPDATE tasks SET status = 'pending' WHERE status = 'running'`)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.logger.Info("schedule tick loop started", "interval", s.tick)

		ticker := time.NewTicker(s.tick)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.processDueTasks(ctx, db, bus)
			}
		}
	}()

	return nil
}

func (s *defaultSchedule) processDueTasks(ctx context.Context, db *sql.DB, bus chan<- Message) {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	rows, err := db.Query(`
		SELECT id, agent_id, user_id, channel_id, source_id, message_text,
		       scheduled_for, cron_expr, reply, queue, tags
		FROM tasks
		WHERE status = 'pending' AND scheduled_for <= ?
		ORDER BY scheduled_for ASC
		LIMIT 10`, now)
	if err != nil {
		s.logger.Warn("failed to query due tasks", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var task ScheduledTask
		var tagsJSON string
		var scheduledForStr string
		var reply bool

		err := rows.Scan(
			&task.ID, &task.AgentID, &task.UserID, &task.ChannelID,
			&task.SourceID, &task.MessageText, &scheduledForStr,
			&task.CronExpr, &reply, &task.Queue, &tagsJSON,
		)
		if err != nil {
			s.logger.Warn("failed to scan task", "error", err)
			continue
		}

		task.Reply = reply
		task.ScheduledFor = parseTime(scheduledForStr)
		json.Unmarshal([]byte(tagsJSON), &task.Tags)

		// Mark as running
		db.Exec(`UPDATE tasks SET status = 'running', updated_at = datetime('now') WHERE id = ?`, task.ID)

		s.logger.Info("firing scheduled task",
			"task_id", task.ID,
			"agent_id", task.AgentID,
			"text_len", len(task.MessageText),
		)

		// Emit message to bus
		msg := Message{
			ID:        fmt.Sprintf("sched-%s", task.ID),
			SourceID:  "scheduler",
			AgentID:   task.AgentID,
			ChannelID: task.ChannelID,
			UserID:    task.UserID,
			ChatType:  "scheduled",
			Text:      task.MessageText,
			Reply:     task.Reply,
			Metadata: map[string]any{
				"task_id":   task.ID,
				"queue":     task.Queue,
				"source_id": task.SourceID,
			},
		}

		select {
		case bus <- msg:
			// Mark done (or reschedule cron)
			if task.CronExpr != "" {
				next := nextCronTime(task.CronExpr, time.Now())
				db.Exec(`UPDATE tasks SET status = 'pending', scheduled_for = ?, updated_at = datetime('now') WHERE id = ?`,
					next.UTC().Format("2006-01-02 15:04:05"), task.ID)
				s.logger.Info("cron task rescheduled", "task_id", task.ID, "next", next)
			} else {
				db.Exec(`UPDATE tasks SET status = 'done', updated_at = datetime('now') WHERE id = ?`, task.ID)
			}
		case <-ctx.Done():
			// Revert to pending on shutdown
			db.Exec(`UPDATE tasks SET status = 'pending', updated_at = datetime('now') WHERE id = ?`, task.ID)
			return
		}
	}
}

func (s *defaultSchedule) Stop(ctx context.Context) error {
	close(s.stopCh)
	s.wg.Wait()
	s.logger.Info("schedule stopped")
	return nil
}

func (s *defaultSchedule) Create(task ScheduledTask) (string, error) {
	db := s.ensureSchema()
	if db == nil {
		return "", fmt.Errorf("scheduler database unavailable")
	}

	if task.ID == "" {
		task.ID = generateID()
	}
	if task.Status == "" {
		task.Status = "pending"
	}

	tagsJSON, _ := json.Marshal(task.Tags)

	_, err := db.Exec(`INSERT INTO tasks
		(id, agent_id, user_id, channel_id, source_id, message_text,
		 scheduled_for, cron_expr, reply, queue, status, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.AgentID, task.UserID, task.ChannelID,
		task.SourceID, task.MessageText,
		task.ScheduledFor.UTC().Format("2006-01-02 15:04:05"),
		task.CronExpr, task.Reply, task.Queue, task.Status, string(tagsJSON),
	)
	if err != nil {
		return "", fmt.Errorf("create task: %w", err)
	}

	s.logger.Info("task created", "task_id", task.ID, "agent_id", task.AgentID,
		"scheduled_for", task.ScheduledFor)
	return task.ID, nil
}

func (s *defaultSchedule) Cancel(taskID string) error {
	db := s.ensureSchema()
	if db == nil {
		return fmt.Errorf("scheduler database unavailable")
	}

	res, err := db.Exec(`UPDATE tasks SET status = 'cancelled', updated_at = datetime('now')
		WHERE id = ? AND status IN ('pending', 'running')`, taskID)
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task %s not found or not cancellable", taskID)
	}

	s.logger.Info("task cancelled", "task_id", taskID)
	return nil
}

func (s *defaultSchedule) List(status string) ([]ScheduledTask, error) {
	db := s.ensureSchema()
	if db == nil {
		return nil, fmt.Errorf("scheduler database unavailable")
	}

	query := `SELECT id, agent_id, user_id, channel_id, source_id, message_text,
		scheduled_for, cron_expr, reply, queue, status, tags
		FROM tasks`
	var args []any

	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY scheduled_for ASC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		var t ScheduledTask
		var tagsJSON, scheduledForStr string
		var reply bool

		err := rows.Scan(
			&t.ID, &t.AgentID, &t.UserID, &t.ChannelID, &t.SourceID,
			&t.MessageText, &scheduledForStr, &t.CronExpr, &reply,
			&t.Queue, &t.Status, &tagsJSON,
		)
		if err != nil {
			continue
		}
		t.Reply = reply
		t.ScheduledFor = parseTime(scheduledForStr)
		json.Unmarshal([]byte(tagsJSON), &t.Tags)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (s *defaultSchedule) Delay(taskID string, duration time.Duration) error {
	db := s.ensureSchema()
	if db == nil {
		return fmt.Errorf("scheduler database unavailable")
	}

	res, err := db.Exec(`UPDATE tasks
		SET scheduled_for = datetime(scheduled_for, '+' || ? || ' seconds'),
		    updated_at = datetime('now')
		WHERE id = ? AND status = 'pending'`,
		int(duration.Seconds()), taskID)
	if err != nil {
		return fmt.Errorf("delay task: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task %s not found or not pending", taskID)
	}

	s.logger.Info("task delayed", "task_id", taskID, "duration", duration)
	return nil
}

// --- Cron ---

// nextCronTime computes the next fire time from a cron expression using robfig/cron.
// Supports standard 5-field cron: "minute hour dom month dow".
func nextCronTime(expr string, from time.Time) time.Time {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(expr)
	if err != nil {
		// Fallback: 24 hours from now
		return from.Add(24 * time.Hour)
	}
	return schedule.Next(from)
}

// parseTime tries multiple SQLite datetime formats.
func parseTime(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

