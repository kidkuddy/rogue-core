package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"crypto/rand"
	"encoding/hex"
)

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type defaultHelmet struct {
	store        Store
	rootResolver RootResolver
	logger       *slog.Logger
}

func NewHelmet(store Store, rootResolver RootResolver, logger *slog.Logger) Helmet {
	return &defaultHelmet{
		store:        store,
		rootResolver: rootResolver,
		logger:       logger,
	}
}

func (h *defaultHelmet) Process(ctx context.Context, msg Message) (*EnrichedMessage, error) {
	h.logger.Info("processing message",
		"source_id", msg.SourceID,
		"user_id", msg.UserID,
		"channel_id", msg.ChannelID,
		"agent_id", msg.AgentID,
	)

	// TODO: Validate source (reject unregistered sources)

	// TODO: Get or create user from Store("iam")
	user := User{
		ID:        msg.UserID,
		Username:  fmt.Sprintf("user_%s", msg.UserID),
		FirstName: "Unknown",
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
	}

	// TODO: Map (source, channel, agent) -> chat_id from Store("iam")
	chatID := generateID()

	// TODO: Get or create session from Store("iam")
	session := Session{
		ID:        generateID(),
		ChannelID: msg.ChannelID,
		AgentID:   msg.AgentID,
		SourceID:  msg.SourceID,
	}

	// Resolve root status
	isRoot := h.rootResolver(msg.UserID)

	// TODO: Load agent persona from disk
	agent := AgentConfig{
		ID:      msg.AgentID,
		Persona: fmt.Sprintf("[stub] persona for agent %s", msg.AgentID),
	}

	// TODO: Resolve powers from Store("iam") and build PowerSet
	powerSet := PowerSet{
		Powers:       nil,
		Tools:        nil,
		Directories:  nil,
		Instructions: "",
	}

	// Build auto-generated tags
	tags := []string{
		"user:" + msg.UserID,
		"source:" + msg.SourceID,
		"channel:" + msg.ChannelID,
		"agent:" + msg.AgentID,
	}
	// TODO: Merge admin-configured tags from Store("iam")

	enriched := &EnrichedMessage{
		Message:  msg,
		ChatID:   chatID,
		Session:  session,
		User:     user,
		Agent:    agent,
		PowerSet: powerSet,
		Tags:     tags,
		IsRoot:   isRoot,
	}

	h.logger.Info("message enriched",
		"chat_id", chatID,
		"is_root", isRoot,
		"powers", len(powerSet.Powers),
		"tags", tags,
	)

	return enriched, nil
}
