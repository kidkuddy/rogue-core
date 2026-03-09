package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kidkuddy/rogue-core/core"
	"github.com/kidkuddy/rogue-core/providers/claudecode"
	"github.com/kidkuddy/rogue-core/sources/agent"
	"github.com/kidkuddy/rogue-core/sources/cli"
	"github.com/kidkuddy/rogue-core/sources/telegram"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load config
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := core.LoadConfig(configPath)
	if err != nil {
		// Fallback: minimal CLI mode with stub provider
		logger.Warn("no config loaded, running in stub CLI mode", "error", err)
		runStubMode(logger)
		return
	}

	// Resolve relative paths in config against the config file's directory
	configDir, _ := filepath.Abs(filepath.Dir(configPath))
	resolveRelativePaths(cfg, configDir)

	runFromConfig(cfg, configDir, logger)
}

func runFromConfig(cfg *core.Config, configDir string, logger *slog.Logger) {
	// Store
	store := core.NewSQLiteStore(cfg.Store.DataDir, logger)
	defer store.Close()

	// Telepath
	telepath := core.NewTelepath(logger)

	// Register sources from config
	for _, srcCfg := range cfg.Telepath.Sources {
		src, err := buildSource(srcCfg, logger)
		if err != nil {
			logger.Error("failed to build source", "id", srcCfg.ID, "error", err)
			continue
		}
		telepath.RegisterSource(src)
	}

	// Helmet
	rootResolver := core.BuildRootResolver(cfg.Helmet.RootResolver)
	var helmetOpts []core.HelmetOption
	if cfg.Helmet.PowersDir != "" {
		helmetOpts = append(helmetOpts, core.WithPowersDir(cfg.Helmet.PowersDir))
	}
	if cfg.Helmet.AgentsDir != "" {
		helmetOpts = append(helmetOpts, core.WithAgentsDir(cfg.Helmet.AgentsDir))
	}
	helmet := core.NewHelmet(store, rootResolver, logger, helmetOpts...)

	// MCP Registry
	var mcpRegistry core.MCPRegistry
	if len(cfg.Cerebro.Tools) > 0 {
		tmpDir := cfg.Store.DataDir + "/tmp"
		registry := core.NewMCPRegistry(tmpDir)
		for name, tool := range cfg.Cerebro.Tools {
			core.RegisterServer(registry, name, core.MCPTool{
				Command: tool.Command,
				Args:    tool.Args,
				Env:     tool.Env,
			})
		}
		mcpRegistry = registry
	}

	// Cerebro
	var provider core.AgentProvider
	switch cfg.Cerebro.DefaultProvider {
	case "claude-code":
		provider = claudecode.New(logger)
	default:
		provider = &core.StubProvider{Logger: logger}
	}
	// Root prompt config
	var cerebroOpts []core.CerebroOption
	useRootPrompt := cfg.Cerebro.RootPrompt == nil || *cfg.Cerebro.RootPrompt
	prependPersona := cfg.Cerebro.PrependPersona == nil || *cfg.Cerebro.PrependPersona
	rootPath := cfg.Cerebro.RootPromptPath
	if rootPath == "" {
		rootPath = filepath.Join(configDir, "ROOT.md")
	}
	cerebroOpts = append(cerebroOpts, core.WithRootPromptConfig(rootPath, useRootPrompt, prependPersona))

	cerebro := core.NewCerebro(store, provider, mcpRegistry, cfg.Cerebro.MaxTurns, cfg.Cerebro.MaxAgentDepth, logger, cerebroOpts...)

	// Warp
	warp := core.NewWarp(telepath, store, logger)

	// Schedule
	var schedOpts []core.ScheduleOption
	if cfg.Scheduler.TickInterval != "" {
		if d, err := time.ParseDuration(cfg.Scheduler.TickInterval); err == nil {
			schedOpts = append(schedOpts, core.WithTickInterval(d))
		}
	}
	schedule := core.NewSchedule(store, logger, schedOpts...)

	// Pipeline
	var pipelineOpts []core.PipelineOption
	requireApproval := cfg.Helmet.RequireApproval == nil || *cfg.Helmet.RequireApproval
	pipelineOpts = append(pipelineOpts, core.WithRequireApprovalGate(requireApproval))
	pipeline := core.NewPipeline(telepath, helmet, cerebro, warp, schedule, logger, pipelineOpts...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := pipeline.Start(ctx); err != nil {
		logger.Error("pipeline start failed", "error", err)
		os.Exit(1)
	}

	logger.Info("pipeline running from config")

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down")
	pipeline.Stop(ctx)
}

func runStubMode(logger *slog.Logger) {
	store := core.NewStubStore()
	telepath := core.NewTelepath(logger)

	agentID := "rogue"
	cliSource := cli.New(agentID, os.Stdin, os.Stdout, logger)
	telepath.RegisterSource(cliSource)

	rootResolver := func(string) bool { return true }
	helmet := core.NewHelmet(store, rootResolver, logger)

	provider := &core.StubProvider{Logger: logger}
	cerebro := core.NewCerebro(store, provider, nil, 100, 3, logger)
	warp := core.NewWarp(telepath, store, logger)
	pipeline := core.NewPipeline(telepath, helmet, cerebro, warp, core.NewSchedule(store, logger), logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := pipeline.Start(ctx); err != nil {
		logger.Error("pipeline start failed", "error", err)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	pipeline.Stop(ctx)
}

func resolveRelativePaths(cfg *core.Config, baseDir string) {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		abs, err := filepath.Abs(filepath.Join(baseDir, p))
		if err != nil {
			return p
		}
		return abs
	}

	cfg.Store.DataDir = resolve(cfg.Store.DataDir)
	cfg.Helmet.PowersDir = resolve(cfg.Helmet.PowersDir)
	cfg.Helmet.AgentsDir = resolve(cfg.Helmet.AgentsDir)
	cfg.Cerebro.RootPromptPath = resolve(cfg.Cerebro.RootPromptPath)

	for name, tool := range cfg.Cerebro.Tools {
		tool.Command = resolve(tool.Command)
		cfg.Cerebro.Tools[name] = tool
	}
}

func buildSource(cfg core.SourceConfig, logger *slog.Logger) (core.Source, error) {
	switch cfg.Type {
	case "telegram":
		if cfg.Token == "" {
			return nil, fmt.Errorf("telegram source %s: token required", cfg.ID)
		}
		var opts []telegram.Option
		if cfg.DebounceMS > 0 {
			opts = append(opts, telegram.WithDebounce(time.Duration(cfg.DebounceMS)*time.Millisecond))
		}
		return telegram.New(cfg.ID, cfg.Agent, cfg.Token, logger, opts...), nil

	case "cli":
		return cli.New(cfg.Agent, os.Stdin, os.Stdout, logger), nil

	case "agent":
		return agent.New(cfg.Agent, logger), nil

	default:
		return nil, fmt.Errorf("unknown source type: %s", cfg.Type)
	}
}
