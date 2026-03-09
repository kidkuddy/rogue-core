# Architecture Overview

Rogue Core is built around a linear pipeline with interface-driven components. Each component has a single responsibility and communicates through well-defined interfaces.

## System Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                        Instance                              │
│  agents/*.md    powers/*.md    ROOT.md    config.yaml         │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                      Rogue Core                              │
│                                                              │
│  ┌──────────┐  ┌────────┐  ┌─────────┐  ┌──────┐           │
│  │ Telepath  │→ │ Helmet │→ │ Cerebro │→ │ Warp │           │
│  │  (bus)    │  │ (IAM)  │  │ (orch)  │  │(resp)│           │
│  └──────────┘  └────────┘  └─────────┘  └──────┘           │
│       ↑                         ↑                            │
│  ┌────┴─────┐             ┌────┴──────┐                     │
│  │ Sources   │             │ MCP Tools  │                     │
│  │ telegram  │             │ store      │                     │
│  │ cli       │             │ scheduler  │                     │
│  │ agent     │             │ iam        │                     │
│  └──────────┘             │ telegram   │                     │
│       ↑                    └───────────┘                     │
│  ┌────┴─────┐                                                │
│  │ Schedule  │                                                │
│  │ (timer)   │                                                │
│  └──────────┘                                                │
└─────────────────────────────────────────────────────────────┘
```

## Interface-Driven Design

Every component is defined as a Go interface:

```go
type Source interface {
    ID() string
    Start(ctx context.Context, inbound chan<- Message) error
    Send(ctx context.Context, resp Response) error
    Stop(ctx context.Context) error
}

type Helmet interface {
    Process(ctx context.Context, msg Message) (*EnrichedMessage, error)
}

type Cerebro interface {
    Execute(ctx context.Context, msg *EnrichedMessage) (*AgentResult, error)
    RegisterProvider(provider AgentProvider)
}

type Warp interface {
    Handle(ctx context.Context, msg *EnrichedMessage, result *AgentResult) error
}
```

This means you can swap any component — use a different AI provider, add a new message source, or replace the IAM layer entirely.

## Optional Interfaces

Sources can optionally implement additional capabilities:

```go
type TypingSource interface {
    StartTyping(channelID string) func()
}

type EnvSource interface {
    SourceEnv() map[string]string
}
```

The pipeline checks for these at runtime and uses them when available.

## Prompt Assembly

The final system prompt sent to the AI provider is assembled in layers:

```
1. ROOT.md template (with {{agent_persona}} substitution or prepend)
2. Agent persona (.md file + current time context)
3. Session context (agent_id, user_id, channel_id, chat_id)
4. Power instructions (concatenated from all granted powers)
```

## MCP Tool Architecture

MCP tools are separate Go binaries that communicate via stdio. Each tool is registered in `config.yaml` with its command and environment variables. At runtime, Cerebro generates a dynamic MCP config file with only the tools granted by the user's PowerSet.

Session-specific env vars (user_id, channel_id, bot tokens) are injected into the MCP server's environment at invocation time.
