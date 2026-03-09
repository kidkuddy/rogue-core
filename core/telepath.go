package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type defaultTelepath struct {
	sources  map[string]Source
	inbound  chan Message
	mu       sync.RWMutex
	logger   *slog.Logger
}

func NewTelepath(logger *slog.Logger) Telepath {
	return &defaultTelepath{
		sources: make(map[string]Source),
		inbound: make(chan Message, 100),
		logger:  logger,
	}
}

func (t *defaultTelepath) RegisterSource(source Source) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sources[source.ID()] = source
	t.logger.Info("source registered", "source_id", source.ID())
}

func (t *defaultTelepath) Start(ctx context.Context) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, src := range t.sources {
		if err := src.Start(ctx, t.inbound); err != nil {
			return fmt.Errorf("failed to start source %s: %w", src.ID(), err)
		}
		t.logger.Info("source started", "source_id", src.ID())
	}
	return nil
}

func (t *defaultTelepath) Inbound() <-chan Message {
	return t.inbound
}

func (t *defaultTelepath) Source(id string) Source {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sources[id]
}

// InboundChan returns a writable channel for Schedule to emit messages into.
func (t *defaultTelepath) InboundChan() chan<- Message {
	return t.inbound
}

func (t *defaultTelepath) Outbound(ctx context.Context, resp Response) error {
	t.mu.RLock()
	src, ok := t.sources[resp.TargetSource]
	t.mu.RUnlock()

	if !ok {
		return fmt.Errorf("unknown target source: %s", resp.TargetSource)
	}

	t.logger.Info("routing response",
		"target_source", resp.TargetSource,
		"channel_id", resp.ChannelID,
		"message_id", resp.MessageID,
	)
	return src.Send(ctx, resp)
}

func (t *defaultTelepath) Stop(ctx context.Context) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var errs []error
	for _, src := range t.sources {
		if err := src.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop source %s: %w", src.ID(), err))
		}
	}
	close(t.inbound)

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping sources: %v", errs)
	}
	return nil
}
