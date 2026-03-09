package core

import (
	"context"
	"database/sql"
	"time"
)

// Source is a message source adapter (Telegram, CLI, Scheduler, Agent).
type Source interface {
	ID() string
	Start(ctx context.Context, inbound chan<- Message) error
	Send(ctx context.Context, resp Response) error
	Stop(ctx context.Context) error
}

// TypingSource is optionally implemented by sources that support typing indicators.
type TypingSource interface {
	// StartTyping begins a typing indicator loop. Returns a stop function.
	StartTyping(channelID string) func()
}

// EnvSource is optionally implemented by sources that expose env vars for MCP tools.
// For example, Telegram exposes TELEGRAM_BOT_TOKEN so agent-controlled MCP tools
// can call the Telegram API (e.g., send reactions).
type EnvSource interface {
	// SourceEnv returns env vars to pass to MCP tool processes.
	SourceEnv() map[string]string
}

// Telepath is the bidirectional message bus.
type Telepath interface {
	// RegisterSource adds a source to the bus.
	RegisterSource(source Source)

	// Start begins routing messages through the pipeline.
	Start(ctx context.Context) error

	// Inbound returns the channel for incoming messages.
	Inbound() <-chan Message

	// Outbound sends a response to the appropriate source.
	Outbound(ctx context.Context, resp Response) error

	// Source returns a registered source by ID, or nil.
	Source(id string) Source

	// Stop gracefully shuts down all sources and the bus.
	Stop(ctx context.Context) error
}

// Helmet is the IAM layer.
type Helmet interface {
	Process(ctx context.Context, msg Message) (*EnrichedMessage, error)
}

// Cerebro is the agent orchestrator.
type Cerebro interface {
	Execute(ctx context.Context, msg *EnrichedMessage) (*AgentResult, error)
	RegisterProvider(provider AgentProvider)
}

// AgentProvider is an AI provider implementation (Claude Code, Copilot, etc.).
type AgentProvider interface {
	ID() string
	Execute(ctx context.Context, req AgentRequest) (*AgentResult, error)
}

// Warp is the response handler.
type Warp interface {
	Handle(ctx context.Context, msg *EnrichedMessage, result *AgentResult) error
}

// Store is the namespaced storage layer.
type Store interface {
	Namespace(name string) Namespace
	Backup(ctx context.Context) error
	Close() error
}

// Namespace is an isolated storage scope within Store.
type Namespace interface {
	DB() (*sql.DB, error)
	FilePath(name string) string
	FileList() ([]FileInfo, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte) error
}

// FileInfo describes a file in a namespace.
type FileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// Schedule is the time-based task scheduler.
type Schedule interface {
	Start(ctx context.Context, bus chan<- Message) error
	Stop(ctx context.Context) error
	Create(task ScheduledTask) (string, error)
	Cancel(taskID string) error
	List(status, agentID string) ([]ScheduledTask, error)
	ListTasks(status, agentID string, includeSystem bool) ([]ScheduledTask, error)
	Delay(taskID string, duration time.Duration) error
	Ack(taskID string) error
	SyncPowerSchedules(agentID, userID, sourceID, channelID string, power Power) error
	RemovePowerSchedules(powerName string) error
}

// RootResolver determines if a user is root.
type RootResolver func(userID string) bool

// MCPRegistry generates dynamic MCP configs per session.
type MCPRegistry interface {
	GenerateConfig(tools []string, env map[string]string) (string, error)
}

// Pipeline wires all components together and runs the message loop.
type Pipeline interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
