package core

import (
	"context"
	"fmt"
	"log/slog"
)

// StubProvider is a test AgentProvider that echoes the prompt back.
type StubProvider struct {
	Logger *slog.Logger
}

func (p *StubProvider) ID() string { return "stub" }

func (p *StubProvider) Execute(ctx context.Context, req AgentRequest) (*AgentResult, error) {
	if p.Logger != nil {
		p.Logger.Info("stub provider executing",
			"chat_id", req.ChatID,
			"prompt_len", len(req.Prompt),
			"tools", len(req.Tools),
		)
	}

	return &AgentResult{
		Content:      fmt.Sprintf("[stub response] received: %s", req.Prompt),
		SessionState: nil,
		Usage: Usage{
			InputTokens:  int64(len(req.Prompt)),
			OutputTokens: 50,
			NumTurns:     1,
		},
	}, nil
}

// StubSource is a test Source that emits a single message then stops.
type StubSource struct {
	SourceID  string
	AgentID   string
	Messages  []Message
	Received  []Response
	Logger    *slog.Logger
}

func (s *StubSource) ID() string { return s.SourceID }

func (s *StubSource) Start(ctx context.Context, inbound chan<- Message) error {
	if s.Logger != nil {
		s.Logger.Info("stub source started", "source_id", s.SourceID)
	}

	go func() {
		for _, msg := range s.Messages {
			msg.SourceID = s.SourceID
			if msg.AgentID == "" {
				msg.AgentID = s.AgentID
			}
			inbound <- msg
		}
	}()

	return nil
}

func (s *StubSource) Send(ctx context.Context, resp Response) error {
	if s.Logger != nil {
		s.Logger.Info("stub source received response",
			"message_id", resp.MessageID,
			"text", resp.Text,
		)
	}
	s.Received = append(s.Received, resp)
	return nil
}

func (s *StubSource) Stop(ctx context.Context) error { return nil }
