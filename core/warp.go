package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type defaultWarp struct {
	telepath Telepath
	store    Store
	logger   *slog.Logger
}

func NewWarp(telepath Telepath, store Store, logger *slog.Logger) Warp {
	return &defaultWarp{
		telepath: telepath,
		store:    store,
		logger:   logger,
	}
}

func (w *defaultWarp) Handle(ctx context.Context, msg *EnrichedMessage, result *AgentResult) error {
	// Always record usage, even if reply is suppressed
	w.recordUsage(msg, result)

	// Check reply flag
	if !msg.Reply {
		w.logger.Info("reply suppressed", "message_id", msg.ID, "chat_id", msg.ChatID)
		return nil
	}

	// Determine target: cross-channel redirect or same channel
	targetSource := msg.SourceID
	targetChannel := msg.ChannelID

	if cc, ok := extractCrossChannel(result.Metadata); ok {
		targetSource = cc.TargetSource
		targetChannel = cc.TargetChannel
		w.logger.Info("cross-channel redirect",
			"from_source", msg.SourceID,
			"to_source", targetSource,
			"to_channel", targetChannel,
		)
	}

	// Build response
	resp := Response{
		MessageID:    msg.ID,
		TargetSource: targetSource,
		ChannelID:    targetChannel,
		Text:         result.Content,
		Attachments:  extractAttachments(result.Metadata),
		Metadata:     result.Metadata,
	}

	w.logger.Info("routing response",
		"message_id", msg.ID,
		"target_source", resp.TargetSource,
		"channel_id", resp.ChannelID,
		"text_len", len(resp.Text),
	)

	// Emit to Telepath for delivery
	return w.telepath.Outbound(ctx, resp)
}

// --- Cross-Channel ---

type crossChannelTarget struct {
	TargetSource  string
	TargetChannel string
}

func extractCrossChannel(metadata map[string]any) (crossChannelTarget, bool) {
	if metadata == nil {
		return crossChannelTarget{}, false
	}
	cc, ok := metadata["cross_channel"]
	if !ok {
		return crossChannelTarget{}, false
	}

	switch v := cc.(type) {
	case map[string]any:
		ts, _ := v["target_source"].(string)
		tc, _ := v["target_channel"].(string)
		if ts != "" && tc != "" {
			return crossChannelTarget{TargetSource: ts, TargetChannel: tc}, true
		}
	case map[string]string:
		if v["target_source"] != "" && v["target_channel"] != "" {
			return crossChannelTarget{TargetSource: v["target_source"], TargetChannel: v["target_channel"]}, true
		}
	}
	return crossChannelTarget{}, false
}

func extractAttachments(metadata map[string]any) []Attachment {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata["attachments"]
	if !ok {
		return nil
	}
	// Try JSON round-trip for flexibility
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var atts []Attachment
	if err := json.Unmarshal(data, &atts); err != nil {
		return nil
	}
	return atts
}

// --- Usage Recording ---

func (w *defaultWarp) ensureUsageSchema() *sql.DB {
	db, err := w.store.Namespace("iam").DB()
	if err != nil {
		return nil
	}
	// Schema matches Helmet's usage_stats table
	db.Exec(`CREATE TABLE IF NOT EXISTS usage_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT (datetime('now')),
		user_id TEXT,
		channel_id TEXT,
		agent_id TEXT,
		chat_id TEXT,
		source_id TEXT,
		message_type TEXT DEFAULT 'message',
		powers TEXT DEFAULT '',
		tags TEXT DEFAULT '',
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		cache_read_tokens INTEGER DEFAULT 0,
		cache_write_tokens INTEGER DEFAULT 0,
		cost_usd REAL DEFAULT 0,
		duration_ms INTEGER DEFAULT 0,
		num_turns INTEGER DEFAULT 0,
		hit_max_turns BOOLEAN DEFAULT 0,
		session_state TEXT DEFAULT ''
	)`)
	return db
}

func (w *defaultWarp) recordUsage(msg *EnrichedMessage, result *AgentResult) {
	db := w.ensureUsageSchema()
	if db == nil {
		return
	}

	tagsStr := strings.Join(msg.Tags, ",")

	_, err := db.Exec(`INSERT INTO usage_stats
		(timestamp, user_id, channel_id, agent_id, chat_id, source_id,
		 tags, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
		 cost_usd, duration_ms, num_turns, hit_max_turns)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format("2006-01-02 15:04:05"),
		msg.UserID, msg.ChannelID, msg.Agent.ID, msg.ChatID, msg.SourceID,
		tagsStr, result.Usage.InputTokens, result.Usage.OutputTokens,
		result.Usage.CacheReadTokens, result.Usage.CacheWriteTokens,
		result.Usage.CostUSD, result.Usage.DurationMS,
		result.Usage.NumTurns, result.HitMaxTurns,
	)
	if err != nil {
		w.logger.Warn("failed to record usage", "error", err)
		return
	}

	w.logger.Info("usage recorded",
		"chat_id", msg.ChatID,
		"user_id", msg.UserID,
		"cost_usd", result.Usage.CostUSD,
		"turns", result.Usage.NumTurns,
	)
}

// --- Usage Query Helpers ---

// UsageSummary holds aggregated usage stats.
type UsageSummary struct {
	TotalCostUSD     float64 `json:"total_cost_usd"`
	TotalInputTokens int64   `json:"total_input_tokens"`
	TotalOutputTokens int64  `json:"total_output_tokens"`
	TotalTurns       int     `json:"total_turns"`
	ExecutionCount   int     `json:"execution_count"`
	Period           string  `json:"period"`
}

// QueryUsage returns aggregated usage for a time period.
func QueryUsage(store Store, since time.Time, tags ...string) (*UsageSummary, error) {
	db, err := store.Namespace("iam").DB()
	if err != nil {
		return nil, err
	}

	query := `SELECT
		COALESCE(SUM(cost_usd), 0),
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(num_turns), 0),
		COUNT(*)
		FROM usage_stats WHERE timestamp >= ?`

	args := []any{since.UTC().Format("2006-01-02 15:04:05")}

	if len(tags) > 0 {
		placeholders := make([]string, len(tags))
		for i, tag := range tags {
			placeholders[i] = "tags LIKE ?"
			args = append(args, "%"+tag+"%")
		}
		query += " AND (" + strings.Join(placeholders, " OR ") + ")"
	}

	var summary UsageSummary
	err = db.QueryRow(query, args...).Scan(
		&summary.TotalCostUSD,
		&summary.TotalInputTokens,
		&summary.TotalOutputTokens,
		&summary.TotalTurns,
		&summary.ExecutionCount,
	)
	if err != nil {
		return nil, fmt.Errorf("query usage: %w", err)
	}

	summary.Period = since.Format("2006-01-02")
	return &summary, nil
}
