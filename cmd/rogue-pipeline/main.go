package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kidkuddy/rogue-core/core"
	"github.com/kidkuddy/rogue-core/sources/cli"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	store := core.NewStubStore()

	// Telepath
	telepath := core.NewTelepath(logger)

	// CLI source
	agentID := "rogue"
	if len(os.Args) > 1 {
		agentID = os.Args[1]
	}
	cliSource := cli.New(agentID, os.Stdin, os.Stdout, logger)
	telepath.RegisterSource(cliSource)

	// Helmet
	rootResolver := func(userID string) bool { return true }
	helmet := core.NewHelmet(store, rootResolver, logger)

	// Cerebro with stub provider
	provider := &core.StubProvider{Logger: logger}
	cerebro := core.NewCerebro(store, provider, nil, 100, 3, logger)

	// Warp
	warp := core.NewWarp(telepath, store, logger)

	// Pipeline
	pipeline := core.NewPipeline(telepath, helmet, cerebro, warp, core.NewSchedule(store, logger), logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := pipeline.Start(ctx); err != nil {
		logger.Error("pipeline start failed", "error", err)
		os.Exit(1)
	}

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down")
	pipeline.Stop(ctx)
}
