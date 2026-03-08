package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: rogue-coordinator <pipeline-binary> [args...]\n")
		os.Exit(1)
	}

	pipelineBin := os.Args[1]
	pipelineArgs := os.Args[2:]

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	coord := &coordinator{
		pipelineBin:  pipelineBin,
		pipelineArgs: pipelineArgs,
		logger:       logger,
		watchInterval: 5 * time.Second,
	}

	coord.Run()
}

type coordinator struct {
	pipelineBin   string
	pipelineArgs  []string
	logger        *slog.Logger
	watchInterval time.Duration

	cmd  *exec.Cmd
	mu   sync.Mutex
	stop bool
}

func (c *coordinator) Run() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGUSR1)

	c.logger.Info("coordinator starting",
		"pipeline", c.pipelineBin,
		"args", c.pipelineArgs,
	)

	// Start pipeline
	if err := c.startPipeline(); err != nil {
		c.logger.Error("failed to start pipeline", "error", err)
		os.Exit(1)
	}

	// Get initial checksum
	lastChecksum := c.binaryChecksum()

	// Watch loop
	ticker := time.NewTicker(c.watchInterval)
	defer ticker.Stop()

	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGUSR1:
				c.logger.Info("SIGUSR1 received, forwarding config reload to pipeline")
				c.forwardSignal(syscall.SIGUSR1)
			case syscall.SIGHUP:
				c.logger.Info("SIGHUP received, restarting pipeline")
				c.restartPipeline()
			case syscall.SIGTERM, syscall.SIGINT:
				c.logger.Info("shutdown signal received", "signal", sig)
				c.stop = true
				c.stopPipeline()
				return
			}

		case <-ticker.C:
			checksum := c.binaryChecksum()
			if checksum != lastChecksum && lastChecksum != "" && checksum != "" {
				c.logger.Info("pipeline binary changed, restarting",
					"old", lastChecksum[:12],
					"new", checksum[:12],
				)
				lastChecksum = checksum
				c.restartPipeline()
			}

			// Check if pipeline crashed
			c.mu.Lock()
			cmd := c.cmd
			c.mu.Unlock()

			if cmd != nil && cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				if !c.stop {
					c.logger.Warn("pipeline exited unexpectedly, restarting",
						"exit_code", cmd.ProcessState.ExitCode(),
					)
					time.Sleep(1 * time.Second) // brief pause before restart
					c.restartPipeline()
					lastChecksum = c.binaryChecksum()
				}
			}
		}
	}
}

func (c *coordinator) startPipeline() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	absPath, err := filepath.Abs(c.pipelineBin)
	if err != nil {
		absPath = c.pipelineBin
	}

	cmd := exec.Command(absPath, c.pipelineArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start pipeline: %w", err)
	}

	c.cmd = cmd
	c.logger.Info("pipeline started", "pid", cmd.Process.Pid)

	// Wait for exit in background
	go func() {
		cmd.Wait()
	}()

	return nil
}

func (c *coordinator) stopPipeline() {
	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	c.logger.Info("stopping pipeline", "pid", cmd.Process.Pid)

	// Send SIGTERM for graceful shutdown
	cmd.Process.Signal(syscall.SIGTERM)

	// Wait up to 30s for graceful exit
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("pipeline stopped gracefully")
	case <-time.After(30 * time.Second):
		c.logger.Warn("pipeline did not stop in 30s, killing")
		cmd.Process.Kill()
	}
}

func (c *coordinator) restartPipeline() {
	c.stopPipeline()
	if err := c.startPipeline(); err != nil {
		c.logger.Error("failed to restart pipeline", "error", err)
	}
}

func (c *coordinator) forwardSignal(sig os.Signal) {
	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		c.logger.Warn("no pipeline process to forward signal to")
		return
	}

	if err := cmd.Process.Signal(sig); err != nil {
		c.logger.Warn("failed to forward signal", "signal", sig, "error", err)
	}
}

func (c *coordinator) binaryChecksum() string {
	f, err := os.Open(c.pipelineBin)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
