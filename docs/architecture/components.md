# Components

## Telepath (Message Bus)

Bidirectional message bus. Manages source registration, inbound message routing, and outbound response delivery.

- Registers multiple sources (Telegram, CLI, agent)
- Provides inbound channel for the pipeline
- Routes responses to the correct source by ID
- Exposes `Source(id)` for capability checks

## Helmet (IAM)

Identity, access, and power management. Every message passes through Helmet.

**Responsibilities:**
- User resolution (get or create)
- Block/approval checks
- Chat session mapping (source + channel + agent → chat_id)
- Agent persona loading (from filesystem)
- Power resolution (from DB + filesystem/embedded)
- Tag generation (auto + admin-configured)

**Output:** `EnrichedMessage` — the original message plus user, session, agent, powers, and tags.

## Cerebro (Orchestrator)

Agent execution orchestrator. Selects the AI provider, assembles the prompt, manages MCP config, and handles session state.

**Responsibilities:**
- Provider selection (via tags or default)
- ROOT.md template application
- Session context injection
- MCP config generation (filtered by PowerSet)
- Session state persistence
- Environment building (user/channel/source vars + source env)

## Warp (Response Handler)

Routes agent responses back to the user and records usage stats.

**Responsibilities:**
- Response routing through Telepath
- Usage recording (tokens, cost, duration, turns)
- Metadata forwarding

## Store (Storage)

Namespaced SQLite + file storage. Dynamic namespaces — any string creates a new isolated storage scope.

**Layout:**
```
$ROGUE_DATA/<namespace>/db/store.sqlite
$ROGUE_DATA/<namespace>/files/
```

## Schedule (Scheduler)

Time-based task scheduler. Runs a tick loop that checks for due tasks and emits them as messages.

**States:** `pending → running → done/awaiting_ack`

**Features:**
- One-shot and cron tasks
- Acknowledgment tracking
- Crash recovery (running → pending on restart)
- Source routing (tasks fire through the original source)

## Coordinator (Supervisor)

Process supervisor that watches the pipeline binary and restarts it on changes. Used for development hot-reload.

**Features:**
- File watcher on the pipeline binary
- Graceful shutdown (SIGTERM, then SIGKILL after timeout)
- Automatic restart on binary change
