package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kidkuddy/rogue-core/core"
)

var schedule core.Schedule

func main() {
	dataDir := os.Getenv("ROGUE_DATA")
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "ROGUE_DATA environment variable is required")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := core.NewSQLiteStore(dataDir, logger)
	defer store.Close()

	schedule = core.NewSchedule(store, logger)

	s := server.NewMCPServer("rogue-scheduler", "1.0.0")

	s.AddTool(mcp.NewTool("schedule",
		mcp.WithDescription("Create a new scheduled task. The task will fire at the specified time and send the message to the specified agent."),
		mcp.WithString("agent_id", mcp.Required(), mcp.Description("Target agent ID (e.g. 'rogue', 'doom')")),
		mcp.WithString("message", mcp.Required(), mcp.Description("Message text to send when the task fires")),
		mcp.WithString("scheduled_for", mcp.Required(), mcp.Description("When to fire, in RFC3339 format (e.g. '2026-03-09T09:00:00Z')")),
		mcp.WithString("cron_expr", mcp.Description("Optional cron expression for recurring tasks (e.g. '0 9 * * *' for daily at 9am)")),
		mcp.WithString("user_id", mcp.Description("User ID who created this task")),
		mcp.WithString("channel_id", mcp.Description("Channel ID for response routing")),
		mcp.WithString("source_id", mcp.Description("Source ID for response routing")),
		mcp.WithString("queue", mcp.Description("Optional queue name to push results to")),
		mcp.WithBoolean("reply", mcp.Description("Whether to send response back (default true)")),
		mcp.WithBoolean("requires_ack", mcp.Description("If true (default), task stays in 'awaiting_ack' after firing until explicitly acknowledged. Cron tasks won't reschedule until ACK'd.")),
	), handleSchedule)

	s.AddTool(mcp.NewTool("cancel_task",
		mcp.WithDescription("Cancel a pending or running scheduled task."),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID to cancel")),
	), handleCancel)

	s.AddTool(mcp.NewTool("list_tasks",
		mcp.WithDescription("List scheduled tasks, optionally filtered by status and/or agent. Agent defaults to current agent (ROGUE_AGENT_ID). System tasks (created by power schedules) are hidden by default."),
		mcp.WithString("status", mcp.Description("Filter by status: pending, running, awaiting_ack, done, failed, cancelled. Empty = all.")),
		mcp.WithString("agent_id", mcp.Description("Filter by agent ID. Defaults to ROGUE_AGENT_ID env var. Pass '*' to see all agents.")),
		mcp.WithBoolean("include_system", mcp.Description("Include system-managed tasks (power schedules). Default false.")),
	), handleList)

	s.AddTool(mcp.NewTool("delay_task",
		mcp.WithDescription("Postpone a pending or awaiting_ack task by a duration. Transitions awaiting_ack back to pending."),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID to delay")),
		mcp.WithString("duration", mcp.Required(), mcp.Description("Duration to delay by (e.g. '30m', '2h', '1h30m')")),
	), handleDelay)

	s.AddTool(mcp.NewTool("ack_task",
		mcp.WithDescription("Acknowledge a task that is awaiting_ack. One-shot tasks transition to done. Cron tasks reschedule their next run."),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID to acknowledge")),
	), handleAck)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func handleSchedule(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	agentID, _ := req.GetArguments()["agent_id"].(string)
	message, _ := req.GetArguments()["message"].(string)
	scheduledForStr, _ := req.GetArguments()["scheduled_for"].(string)

	if agentID == "" || message == "" || scheduledForStr == "" {
		return mcp.NewToolResultError("agent_id, message, and scheduled_for are required"), nil
	}

	scheduledFor, err := time.Parse(time.RFC3339, scheduledForStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid scheduled_for format: %v (use RFC3339)", err)), nil
	}

	reply := true
	if r, ok := req.GetArguments()["reply"].(bool); ok {
		reply = r
	}

	requiresAck := true
	if r, ok := req.GetArguments()["requires_ack"].(bool); ok {
		requiresAck = r
	}

	task := core.ScheduledTask{
		AgentID:      agentID,
		MessageText:  message,
		ScheduledFor: scheduledFor,
		Reply:        reply,
		RequiresAck:  requiresAck,
	}

	if v, ok := req.GetArguments()["cron_expr"].(string); ok {
		task.CronExpr = v
	}
	if v, ok := req.GetArguments()["user_id"].(string); ok {
		task.UserID = v
	}
	if v, ok := req.GetArguments()["channel_id"].(string); ok {
		task.ChannelID = v
	}
	if v, ok := req.GetArguments()["source_id"].(string); ok {
		task.SourceID = v
	}
	if v, ok := req.GetArguments()["queue"].(string); ok {
		task.Queue = v
	}

	// Get env context
	if task.UserID == "" {
		task.UserID = os.Getenv("ROGUE_USER_ID")
	}
	if task.ChannelID == "" {
		task.ChannelID = os.Getenv("ROGUE_CHANNEL_ID")
	}
	if task.SourceID == "" {
		task.SourceID = os.Getenv("ROGUE_SOURCE_ID")
	}

	id, err := schedule.Create(task)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create failed: %v", err)), nil
	}

	out, _ := json.MarshalIndent(map[string]any{
		"task_id":       id,
		"agent_id":      agentID,
		"scheduled_for": scheduledFor.Format(time.RFC3339),
		"cron_expr":     task.CronExpr,
		"status":        "pending",
	}, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func handleCancel(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID, _ := req.GetArguments()["task_id"].(string)
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	if err := schedule.Cancel(taskID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cancel failed: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Task %s cancelled.", taskID)), nil
}

func handleList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	status, _ := req.GetArguments()["status"].(string)
	agentID, _ := req.GetArguments()["agent_id"].(string)
	includeSystem, _ := req.GetArguments()["include_system"].(bool)

	// Default to current agent; '*' means all agents
	if agentID == "" {
		agentID = os.Getenv("ROGUE_AGENT_ID")
	}
	if agentID == "*" {
		agentID = ""
	}

	tasks, err := schedule.ListTasks(status, agentID, includeSystem)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list failed: %v", err)), nil
	}

	if tasks == nil {
		tasks = []core.ScheduledTask{}
	}

	out, _ := json.MarshalIndent(tasks, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func handleDelay(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID, _ := req.GetArguments()["task_id"].(string)
	durationStr, _ := req.GetArguments()["duration"].(string)

	if taskID == "" || durationStr == "" {
		return mcp.NewToolResultError("task_id and duration are required"), nil
	}

	d, err := time.ParseDuration(durationStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid duration: %v", err)), nil
	}

	if err := schedule.Delay(taskID, d); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("delay failed: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Task %s delayed by %s.", taskID, d)), nil
}

func handleAck(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID, _ := req.GetArguments()["task_id"].(string)
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	if err := schedule.Ack(taskID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("ack failed: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Task %s acknowledged.", taskID)), nil
}
