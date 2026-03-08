package core

import (
	"context"
	"log/slog"
)

type defaultPipeline struct {
	telepath Telepath
	helmet   Helmet
	cerebro  Cerebro
	warp     Warp
	schedule Schedule
	logger   *slog.Logger
}

func NewPipeline(telepath Telepath, helmet Helmet, cerebro Cerebro, warp Warp, schedule Schedule, logger *slog.Logger) Pipeline {
	return &defaultPipeline{
		telepath: telepath,
		helmet:   helmet,
		cerebro:  cerebro,
		warp:     warp,
		schedule: schedule,
		logger:   logger,
	}
}

func (p *defaultPipeline) Start(ctx context.Context) error {
	// Start Telepath (all sources)
	if err := p.telepath.Start(ctx); err != nil {
		return err
	}

	// Start Schedule — emits to the same inbound channel as sources
	if inboundChan, ok := p.telepath.(interface{ InboundChan() chan<- Message }); ok {
		if err := p.schedule.Start(ctx, inboundChan.InboundChan()); err != nil {
			return err
		}
	}

	// Main message loop
	go p.loop(ctx)

	p.logger.Info("pipeline started")
	return nil
}

func (p *defaultPipeline) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-p.telepath.Inbound():
			if !ok {
				return
			}
			p.handleMessage(ctx, msg)
		}
	}
}

func (p *defaultPipeline) handleMessage(ctx context.Context, msg Message) {
	p.logger.Info("message received",
		"id", msg.ID,
		"source", msg.SourceID,
		"agent", msg.AgentID,
		"text_len", len(msg.Text),
	)

	// Helmet: IAM processing
	enriched, err := p.helmet.Process(ctx, msg)
	if err != nil {
		p.logger.Error("helmet rejected message", "error", err, "message_id", msg.ID)
		return
	}

	// Cerebro: Agent execution
	result, err := p.cerebro.Execute(ctx, enriched)
	if err != nil {
		p.logger.Error("cerebro execution failed", "error", err, "message_id", msg.ID)
		return
	}

	// Warp: Response routing
	if err := p.warp.Handle(ctx, enriched, result); err != nil {
		p.logger.Error("warp routing failed", "error", err, "message_id", msg.ID)
		return
	}

	p.logger.Info("message handled", "id", msg.ID)
}

func (p *defaultPipeline) Stop(ctx context.Context) error {
	p.logger.Info("pipeline stopping")

	if err := p.schedule.Stop(ctx); err != nil {
		p.logger.Error("schedule stop error", "error", err)
	}

	return p.telepath.Stop(ctx)
}
