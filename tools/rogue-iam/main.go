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

var store core.Store

func main() {
	dataDir := os.Getenv("ROGUE_DATA")
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "ROGUE_DATA environment variable is required")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store = core.NewSQLiteStore(dataDir, logger)
	defer store.Close()

	s := server.NewMCPServer("rogue-iam", "1.0.0")

	s.AddTool(mcp.NewTool("usage_summary",
		mcp.WithDescription("Get aggregated usage statistics for a time period."),
		mcp.WithString("since", mcp.Description("Start date in RFC3339 or YYYY-MM-DD format. Defaults to 24h ago.")),
		mcp.WithString("tag", mcp.Description("Optional tag filter (e.g. 'agent:rogue', 'user:123')")),
	), handleUsageSummary)

	s.AddTool(mcp.NewTool("usage_recent",
		mcp.WithDescription("Get recent usage entries."),
		mcp.WithNumber("limit", mcp.Description("Number of entries to return (default 20)")),
	), handleUsageRecent)

	s.AddTool(mcp.NewTool("power_list",
		mcp.WithDescription("List power assignments for a user or agent."),
		mcp.WithString("user_id", mcp.Description("Filter by user ID")),
		mcp.WithString("agent_id", mcp.Description("Filter by agent ID")),
	), handlePowerList)

	s.AddTool(mcp.NewTool("power_grant",
		mcp.WithDescription("Grant a power to a user for a specific agent. Use channel_id='' for global (all channels)."),
		mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID to grant power to")),
		mcp.WithString("agent_id", mcp.Required(), mcp.Description("Agent ID scope (e.g. 'rogue', 'doom')")),
		mcp.WithString("power_name", mcp.Required(), mcp.Description("Power name to grant (e.g. 'memory', 'scratchpad')")),
		mcp.WithString("channel_id", mcp.Description("Channel ID scope. Empty string = global (all channels).")),
	), handlePowerGrant)

	s.AddTool(mcp.NewTool("power_revoke",
		mcp.WithDescription("Revoke a power from a user."),
		mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID to revoke power from")),
		mcp.WithString("agent_id", mcp.Required(), mcp.Description("Agent ID scope")),
		mcp.WithString("power_name", mcp.Required(), mcp.Description("Power name to revoke")),
		mcp.WithString("channel_id", mcp.Description("Channel ID scope. Empty string = global.")),
	), handlePowerRevoke)

	s.AddTool(mcp.NewTool("user_list",
		mcp.WithDescription("List all known users."),
	), handleUserList)

	s.AddTool(mcp.NewTool("user_approve",
		mcp.WithDescription("Approve a user so they can interact with the bot."),
		mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID to approve")),
	), handleUserApprove)

	s.AddTool(mcp.NewTool("user_block",
		mcp.WithDescription("Block a user. All their messages will be silently dropped."),
		mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID to block")),
	), handleUserBlock)

	s.AddTool(mcp.NewTool("user_unblock",
		mcp.WithDescription("Unblock a previously blocked user."),
		mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID to unblock")),
	), handleUserUnblock)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func handleUsageSummary(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	sinceStr, _ := args["since"].(string)
	tag, _ := args["tag"].(string)

	since := time.Now().Add(-24 * time.Hour)
	if sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = t
		} else if t, err := time.Parse("2006-01-02", sinceStr); err == nil {
			since = t
		}
	}

	var tags []string
	if tag != "" {
		tags = []string{tag}
	}

	summary, err := core.QueryUsage(store, since, tags...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}

	out, _ := json.MarshalIndent(summary, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func handleUsageRecent(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	limitF, _ := args["limit"].(float64)
	limit := 20
	if limitF > 0 {
		limit = int(limitF)
	}

	db, err := store.Namespace("iam").DB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db error: %v", err)), nil
	}

	rows, err := db.Query(`SELECT
		timestamp, user_id, agent_id, chat_id, source_id,
		input_tokens, output_tokens, cost_usd, duration_ms, num_turns, tags
		FROM usage_stats ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}
	defer rows.Close()

	type entry struct {
		Timestamp    string  `json:"timestamp"`
		UserID       string  `json:"user_id"`
		AgentID      string  `json:"agent_id"`
		ChatID       string  `json:"chat_id"`
		SourceID     string  `json:"source_id"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		CostUSD      float64 `json:"cost_usd"`
		DurationMS   int64   `json:"duration_ms"`
		NumTurns     int     `json:"num_turns"`
		Tags         string  `json:"tags"`
	}

	var entries []entry
	for rows.Next() {
		var e entry
		rows.Scan(&e.Timestamp, &e.UserID, &e.AgentID, &e.ChatID, &e.SourceID,
			&e.InputTokens, &e.OutputTokens, &e.CostUSD, &e.DurationMS, &e.NumTurns, &e.Tags)
		entries = append(entries, e)
	}

	if entries == nil {
		entries = []entry{}
	}

	out, _ := json.MarshalIndent(entries, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func handlePowerGrant(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	userID, _ := args["user_id"].(string)
	agentID, _ := args["agent_id"].(string)
	powerName, _ := args["power_name"].(string)
	channelID, _ := args["channel_id"].(string)

	if userID == "" || agentID == "" || powerName == "" {
		return mcp.NewToolResultError("user_id, agent_id, and power_name are required"), nil
	}

	db, err := store.Namespace("iam").DB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db error: %v", err)), nil
	}

	assignedBy := os.Getenv("ROGUE_USER_ID")
	_, err = db.Exec(`
		INSERT INTO user_powers (agent_id, user_id, channel_id, power_name, assigned_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(agent_id, user_id, channel_id, power_name)
		DO UPDATE SET assigned_by = excluded.assigned_by, assigned_at = datetime('now')
	`, agentID, userID, channelID, powerName, assignedBy)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("grant failed: %v", err)), nil
	}

	scope := "global"
	if channelID != "" {
		scope = "channel:" + channelID
	}
	return mcp.NewToolResultText(fmt.Sprintf("Granted power '%s' to user %s for agent %s (scope: %s)", powerName, userID, agentID, scope)), nil
}

func handlePowerRevoke(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	userID, _ := args["user_id"].(string)
	agentID, _ := args["agent_id"].(string)
	powerName, _ := args["power_name"].(string)
	channelID, _ := args["channel_id"].(string)

	if userID == "" || agentID == "" || powerName == "" {
		return mcp.NewToolResultError("user_id, agent_id, and power_name are required"), nil
	}

	db, err := store.Namespace("iam").DB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db error: %v", err)), nil
	}

	result, err := db.Exec(
		"DELETE FROM user_powers WHERE agent_id = ? AND user_id = ? AND channel_id = ? AND power_name = ?",
		agentID, userID, channelID, powerName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("revoke failed: %v", err)), nil
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No matching grant found for power '%s'", powerName)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Revoked power '%s' from user %s for agent %s", powerName, userID, agentID)), nil
}

func handlePowerList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	userID, _ := args["user_id"].(string)
	agentID, _ := args["agent_id"].(string)

	db, err := store.Namespace("iam").DB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db error: %v", err)), nil
	}

	query := `SELECT user_id, agent_id, channel_id, power_name, assigned_by
		FROM user_powers WHERE 1=1`
	var qArgs []any

	if userID != "" {
		query += " AND user_id = ?"
		qArgs = append(qArgs, userID)
	}
	if agentID != "" {
		query += " AND agent_id = ?"
		qArgs = append(qArgs, agentID)
	}
	query += " ORDER BY user_id, agent_id"

	rows, err := db.Query(query, qArgs...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}
	defer rows.Close()

	type assignment struct {
		UserID    string `json:"user_id"`
		AgentID   string `json:"agent_id"`
		ChannelID string `json:"channel_id"`
		PowerName string `json:"power_name"`
		AssignedBy string `json:"assigned_by"`
	}

	var results []assignment
	for rows.Next() {
		var a assignment
		rows.Scan(&a.UserID, &a.AgentID, &a.ChannelID, &a.PowerName, &a.AssignedBy)
		results = append(results, a)
	}

	if results == nil {
		results = []assignment{}
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func handleUserList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	db, err := store.Namespace("iam").DB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db error: %v", err)), nil
	}

	// Migration: add blocked column if missing
	db.Exec("ALTER TABLE users ADD COLUMN blocked BOOLEAN DEFAULT 0")

	rows, err := db.Query(`SELECT id, username, first_name, approved, blocked, created_at, last_seen FROM users ORDER BY last_seen DESC`)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}
	defer rows.Close()

	type user struct {
		UserID    string `json:"user_id"`
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
		Approved  bool   `json:"approved"`
		Blocked   bool   `json:"blocked"`
		CreatedAt string `json:"created_at"`
		LastSeen  string `json:"last_seen"`
	}

	var users []user
	for rows.Next() {
		var u user
		rows.Scan(&u.UserID, &u.Username, &u.FirstName, &u.Approved, &u.Blocked, &u.CreatedAt, &u.LastSeen)
		users = append(users, u)
	}

	if users == nil {
		users = []user{}
	}

	out, _ := json.MarshalIndent(users, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func handleUserApprove(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID, _ := req.GetArguments()["user_id"].(string)
	if userID == "" {
		return mcp.NewToolResultError("user_id is required"), nil
	}

	db, err := store.Namespace("iam").DB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db error: %v", err)), nil
	}

	result, err := db.Exec("UPDATE users SET approved = 1 WHERE id = ?", userID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("approve failed: %v", err)), nil
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("User %s not found", userID)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("User %s approved", userID)), nil
}

func handleUserBlock(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID, _ := req.GetArguments()["user_id"].(string)
	if userID == "" {
		return mcp.NewToolResultError("user_id is required"), nil
	}

	db, err := store.Namespace("iam").DB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db error: %v", err)), nil
	}

	result, err := db.Exec("UPDATE users SET blocked = 1 WHERE id = ?", userID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("block failed: %v", err)), nil
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("User %s not found", userID)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("User %s blocked", userID)), nil
}

func handleUserUnblock(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID, _ := req.GetArguments()["user_id"].(string)
	if userID == "" {
		return mcp.NewToolResultError("user_id is required"), nil
	}

	db, err := store.Namespace("iam").DB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db error: %v", err)), nil
	}

	result, err := db.Exec("UPDATE users SET blocked = 0 WHERE id = ?", userID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("block failed: %v", err)), nil
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("User %s not found", userID)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("User %s unblocked", userID)), nil
}
