package core

import "time"

// Message is the universal message struct emitted by any source.
type Message struct {
	ID             string         `json:"id"`
	SourceID       string         `json:"source_id"`
	AgentID        string         `json:"agent_id"`
	ChannelID      string         `json:"channel_id"`
	UserID         string         `json:"user_id"`
	ChatType       string         `json:"chat_type"` // "private", "group", "scheduled", "agent"
	Text           string         `json:"text"`
	Attachments    []Attachment   `json:"attachments,omitempty"`
	ReplyTo        *MessageRef    `json:"reply_to,omitempty"`
	Reply          bool           `json:"reply"`
	AgentTurnDepth int            `json:"agent_turn_depth"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// Attachment represents a file attached to a message.
type Attachment struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
}

// MessageRef references a previous message (for reply context).
type MessageRef struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// EnrichedMessage is a Message enriched by Helmet with session, user, and power data.
type EnrichedMessage struct {
	Message
	ChatID   string      `json:"chat_id"` // Agentic conversation ID (UUID)
	Session  Session     `json:"session"`
	User     User        `json:"user"`
	Agent    AgentConfig `json:"agent"`
	PowerSet PowerSet    `json:"power_set"`
	Tags     []string    `json:"tags"`
	IsRoot    bool              `json:"is_root"`
	Approved  bool              `json:"approved"` // root is always approved; others need explicit approval
	SourceEnv map[string]string `json:"-"`         // env vars from source (e.g., TELEGRAM_BOT_TOKEN), not serialized
}

// User represents a resolved user record.
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	FirstName string    `json:"first_name"`
	Approved  bool      `json:"approved"`
	Blocked   bool      `json:"blocked"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

// Session represents a conversation session.
type Session struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	AgentID   string `json:"agent_id"`
	SourceID  string `json:"source_id"`
}

// AgentConfig holds the resolved agent persona.
type AgentConfig struct {
	ID      string `json:"id"`
	Persona string `json:"persona"`
}

// PowerSet is the resolved union of a user's assigned powers.
type PowerSet struct {
	Powers       []Power  `json:"powers"`
	Tools        []string `json:"tools"`
	Directories  []string `json:"directories"`
	Instructions string   `json:"instructions"`
}

// PowerSchedule defines a recurring task that activates when a power is granted.
type PowerSchedule struct {
	Cron        string `json:"cron" yaml:"cron"`
	Message     string `json:"message" yaml:"message"`
	RequiresAck bool   `json:"requires_ack" yaml:"requires_ack"`
}

// Power is a named capability bundle.
type Power struct {
	Name         string          `json:"name"`
	Namespace    string          `json:"namespace"`
	Tools        []string        `json:"tools"`
	Directories  []string        `json:"directories"`
	Instructions string          `json:"instructions"`
	Schedules    []PowerSchedule `json:"schedules,omitempty"`
}

// Response is an outbound message routed back through Telepath.
type Response struct {
	MessageID    string         `json:"message_id"`
	TargetSource string         `json:"target_source"`
	ChannelID    string         `json:"channel_id"`
	Text         string         `json:"text"`
	Attachments  []Attachment   `json:"attachments,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// AgentRequest is what Cerebro passes to an AgentProvider.
type AgentRequest struct {
	ChatID        string
	SessionState  any
	Persona       string
	Prompt        string
	Attachments   []Attachment
	Tools         []string
	Directories   []string
	Instructions  string
	MCPConfigPath string
	Tags          []string
	MaxTurns      int
	Env           map[string]string
}

// AgentResult is what an AgentProvider returns.
type AgentResult struct {
	Content      string
	SessionState any
	Usage        Usage
	HitMaxTurns  bool
	Metadata     map[string]any
}

// Usage tracks resource consumption for an agent execution.
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64
	DurationMS       int64
	NumTurns         int
}

// ScheduledTask represents a time-triggered task.
type ScheduledTask struct {
	ID           string
	AgentID      string
	UserID       string
	ChannelID    string
	SourceID     string
	MessageText  string
	ScheduledFor time.Time
	CronExpr     string
	Reply        bool
	Queue        string
	Status       string // pending | running | awaiting_ack | done | failed | cancelled
	RequiresAck  bool
	System       bool   // system-managed (created by power schedules)
	PowerName    string // which power owns this task (for system tasks)
	Tags         []string
}

// MCPTool describes an MCP server binary for the tool registry.
type MCPTool struct {
	Command string
	Args    []string
	Env     map[string]string
}
