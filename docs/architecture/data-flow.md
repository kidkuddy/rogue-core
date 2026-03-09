# Data Flow

## Message Lifecycle

### 1. Ingestion

A user sends a message via Telegram. The Telegram source:
- Debounces rapid messages (if configured)
- Extracts metadata (message_id, username, first_name)
- Emits a `Message` to Telepath's inbound channel

### 2. IAM Processing (Helmet)

Helmet receives the raw `Message` and returns an `EnrichedMessage`:

```
Message
  ├→ getOrCreateUser()     → User record (approved, blocked, last_seen)
  ├→ block check           → drop if blocked
  ├→ getOrCreateChat()     → chat_id (UUID) for (source, channel, agent) tuple
  ├→ loadAgent()           → AgentConfig (persona + time context)
  ├→ resolvePowerSet()     → PowerSet (tools, directories, instructions)
  └→ buildTags()           → auto-generated + admin-configured tags
```

### 3. Pipeline Checks

```
EnrichedMessage
  ├→ approval gate         → drop if require_approval && !approved
  ├→ typing indicator      → start if source supports it
  └→ source env capture    → TELEGRAM_BOT_TOKEN, etc.
```

### 4. Agent Execution (Cerebro)

```
EnrichedMessage
  ├→ select provider       → based on tags or default
  ├→ load session state    → previous conversation state
  ├→ build env             → ROGUE_USER_ID, ROGUE_CHANNEL_ID, source env
  ├→ generate MCP config   → filtered by PowerSet tools, with session env
  ├→ build persona         → apply ROOT.md template
  ├→ build instructions    → session context + power instructions
  ├→ execute provider      → Claude Code CLI with full args
  └→ save session state    → persist for next message
```

### 5. Response Routing (Warp)

```
AgentResult
  ├→ build Response        → target source, channel, text
  ├→ Telepath.Outbound()   → route to correct source
  └→ record usage          → tokens, cost, duration → usage_stats
```

## Environment Propagation

Environment variables flow through multiple layers:

```
Source (SourceEnv)
  → Pipeline (EnrichedMessage.SourceEnv)
    → Cerebro (buildEnv merges with session vars)
      → MCP Config (GenerateConfig injects into tool processes)
        → MCP Tool (reads from os.Getenv)
```

This allows MCP tools to access source-specific credentials (like Telegram bot tokens) without hardcoding them.

## Scheduled Task Flow

```
Schedule tick loop
  → query due tasks (status='pending', scheduled_for <= now)
    → mark running
      → emit Message to inbound channel (same as user messages)
        → Pipeline processes normally (Helmet → Cerebro → Warp)
          → mark done / awaiting_ack / reschedule cron
```

Scheduled tasks route through the original source — if created via Telegram, the response goes back through Telegram.
