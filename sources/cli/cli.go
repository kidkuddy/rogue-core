package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/kidkuddy/rogue-core/core"
)

type Source struct {
	id      string
	agentID string
	reader  io.Reader
	writer  io.Writer
	logger  *slog.Logger
}

func New(agentID string, reader io.Reader, writer io.Writer, logger *slog.Logger) *Source {
	return &Source{
		id:      "cli:" + agentID,
		agentID: agentID,
		reader:  reader,
		writer:  writer,
		logger:  logger,
	}
}

func (s *Source) ID() string { return s.id }

func (s *Source) Start(ctx context.Context, inbound chan<- core.Message) error {
	s.logger.Info("cli source started", "agent", s.agentID)
	fmt.Fprintf(s.writer, "[%s] ready. type a message:\n", s.agentID)

	go func() {
		scanner := bufio.NewScanner(s.reader)
		msgCount := 0
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			if text == "/quit" || text == "/exit" {
				s.logger.Info("cli quit requested")
				return
			}

			msgCount++
			inbound <- core.Message{
				ID:        fmt.Sprintf("cli-%d", msgCount),
				SourceID:  s.id,
				AgentID:   s.agentID,
				ChannelID: "cli",
				UserID:    "cli-user",
				ChatType:  "private",
				Text:      text,
				Reply:     true,
			}
		}
	}()

	return nil
}

func (s *Source) Send(ctx context.Context, resp core.Response) error {
	fmt.Fprintf(s.writer, "\n[%s] %s\n\n", s.agentID, resp.Text)
	return nil
}

func (s *Source) Stop(ctx context.Context) error {
	s.logger.Info("cli source stopped")
	return nil
}
