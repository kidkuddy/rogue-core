package core

import (
	"context"
	"log/slog"
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
	// Check reply flag
	if !msg.Reply {
		w.logger.Info("reply suppressed", "message_id", msg.ID, "chat_id", msg.ChatID)
		// TODO: Still record usage, potentially push to queue
		return nil
	}

	// TODO: Check for cross-channel redirect in result.Metadata

	// TODO: Record usage to Store("iam") with tags

	// Build response
	resp := Response{
		MessageID:    msg.ID,
		TargetSource: msg.SourceID,
		ChannelID:    msg.ChannelID,
		Text:         result.Content,
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
