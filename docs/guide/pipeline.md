# Pipeline

The pipeline is the core message loop. It wires all components together and processes messages sequentially.

## Message Flow

```
Source (Telegram/CLI) → Telepath (bus) → Helmet (IAM) → Cerebro (orchestrator) → Warp (response)
                                                                ↑
                                                        Schedule (timer)
```

1. **Source** emits a `Message` to Telepath's inbound channel
2. **Helmet** enriches it — resolves user, session, agent, powers, tags
3. **Approval gate** — unapproved users are silently ignored
4. **Source capabilities** — typing indicator starts, env vars captured
5. **Cerebro** executes the agent — selects provider, loads session, assembles prompt, runs AI
6. **Typing stops** — indicator removed
7. **Warp** routes the response back through Telepath to the source

## Pipeline Options

```go
pipeline := core.NewPipeline(telepath, helmet, cerebro, warp, schedule, logger,
    core.WithRequireApprovalGate(true),
)
```

## Scheduled Messages

The scheduler emits messages to the same inbound channel as sources. Scheduled messages have `ChatType: "scheduled"` and carry task metadata. They flow through the same IAM → Cerebro → Warp pipeline as user messages.

## Error Handling

Each stage can reject a message:
- **Helmet** rejects blocked users
- **Approval gate** silently drops unapproved users
- **Cerebro** errors on provider failures
- **Warp** errors on routing failures

Errors are logged but don't crash the pipeline. The loop continues processing the next message.
