package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kidkuddy/rogue-core/core"
)

// Source enables agent-to-agent communication via Telepath.
// When an agent wants to message another agent, it creates a Response
// targeting this source. The source converts it to a Message and
// re-emits it onto the inbound channel with incremented AgentTurnDepth.
type Source struct {
	id      string // "agent:<agent_id>"
	agentID string
	logger  *slog.Logger

	inbound chan<- core.Message
	mu      sync.Mutex
	msgSeq  int
}

// New creates an agent source for inter-agent messaging.
func New(agentID string, logger *slog.Logger) *Source {
	return &Source{
		id:      "agent:" + agentID,
		agentID: agentID,
		logger:  logger,
	}
}

func (s *Source) ID() string { return s.id }

func (s *Source) Start(ctx context.Context, inbound chan<- core.Message) error {
	s.mu.Lock()
	s.inbound = inbound
	s.mu.Unlock()

	s.logger.Info("agent source started", "source_id", s.id)
	return nil
}

// Send receives a Response from another agent and converts it to a Message
// that gets routed back through the pipeline to this agent.
func (s *Source) Send(ctx context.Context, resp core.Response) error {
	s.mu.Lock()
	inbound := s.inbound
	s.msgSeq++
	seq := s.msgSeq
	s.mu.Unlock()

	if inbound == nil {
		return fmt.Errorf("agent source %s not started", s.id)
	}

	// Extract turn depth from metadata
	turnDepth := 0
	if resp.Metadata != nil {
		if d, ok := resp.Metadata["agent_turn_depth"].(int); ok {
			turnDepth = d
		} else if d, ok := resp.Metadata["agent_turn_depth"].(float64); ok {
			turnDepth = int(d)
		}
	}

	// Extract sender agent ID from metadata
	senderAgent := ""
	if resp.Metadata != nil {
		if sa, ok := resp.Metadata["sender_agent"].(string); ok {
			senderAgent = sa
		}
	}

	msg := core.Message{
		ID:             fmt.Sprintf("agent-%s-%d", s.agentID, seq),
		SourceID:       s.id,
		AgentID:        s.agentID,
		ChannelID:      resp.ChannelID,
		UserID:         senderAgent,
		ChatType:       "agent",
		Text:           resp.Text,
		Reply:          true,
		AgentTurnDepth: turnDepth + 1,
		Metadata: map[string]any{
			"sender_agent":    senderAgent,
			"original_msg_id": resp.MessageID,
		},
	}

	s.logger.Info("agent-to-agent message",
		"from", senderAgent,
		"to", s.agentID,
		"depth", msg.AgentTurnDepth,
	)

	select {
	case inbound <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Source) Stop(ctx context.Context) error {
	s.logger.Info("agent source stopped", "source_id", s.id)
	return nil
}

// Emit sends a message from this agent to another agent via the inbound channel.
// This is used when an agent proactively wants to contact another agent.
func (s *Source) Emit(ctx context.Context, targetAgentID, text string, turnDepth int) {
	s.mu.Lock()
	inbound := s.inbound
	s.msgSeq++
	seq := s.msgSeq
	s.mu.Unlock()

	if inbound == nil {
		return
	}

	msg := core.Message{
		ID:             fmt.Sprintf("agent-%s-%d", s.agentID, seq),
		SourceID:       s.id,
		AgentID:        targetAgentID,
		ChannelID:      fmt.Sprintf("agent:%s->%s", s.agentID, targetAgentID),
		UserID:         s.agentID,
		ChatType:       "agent",
		Text:           text,
		Reply:          true,
		AgentTurnDepth: turnDepth,
	}

	select {
	case inbound <- msg:
	case <-ctx.Done():
	}
}
