# Migration Notes — v2 → rogue-core

Fresh start. No data migration. Each section below documents what exists in v2,
what changes for rogue-core, and what to watch out for.

Review one section per day. Check it off when it works.

---

## Status

- [ ] contacts
- [ ] admin
- [ ] powerset
- [ ] queue
- [ ] gc
- [ ] telegram
- [ ] scheduler
- [ ] scraper
- [ ] memory
- [ ] phd
- [ ] agents (copy + verify)
- [ ] power sets (copy + update tool refs)
- [ ] config.yaml
- [ ] build + smoke test

---

## MCP Tools

### contacts

**What it does**: Maps human names to Telegram user IDs. Simple alias CRUD.

**v2 DB**: `$ROGUE_DATA/dbs/contacts.sqlite`
**v3 namespace**: `contacts`

**Schema**:
```sql
aliases (id, alias UNIQUE, user_id, username, display_name, created_at, updated_at)
```

**Tools**: `alias_set`, `alias_get`, `alias_list`, `alias_delete`

**Store migration pattern**:
```go
store := core.NewSQLiteStore(os.Getenv("ROGUE_DATA"), logger)
db, _ := store.Namespace("contacts").DB()
// creates $ROGUE_DATA/db/contacts/store.sqlite
```

**mcp-go upgrade**: v0.27 → v0.45. Change `req.Params.Arguments["x"]` to `req.GetArguments()["x"]`.

**Env vars**: `ROGUE_DATA` only.

**Complexity**: Low. Good first migration — template for others.

---

### admin

**What it does**: Read-only usage analytics. Queries `usage_stats` table.

**v2 DB**: `$ROGUE_DATA/dbs/iam.sqlite`
**v3 namespace**: `iam` (shared with helmet + powerset)

**Schema** (read-only, created by Helmet):
```sql
usage_stats (id, timestamp, user_id, chat_id, bot_id, message_type, power_sets,
             input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
             cost_usd, duration_ms, num_turns, hit_max_turns)
```

**Tools**: `usage_summary`, `usage_by_type`, `usage_by_power_set`, `usage_by_user`, `usage_recent`

**Watch out**: The v2 schema has `bot_id` and `power_sets` columns. The rogue-core Helmet/Warp schema has `agent_id` and `source_id` instead. Column names differ. Either:
1. Adapt admin queries to rogue-core's schema, or
2. Align the Helmet/Warp `usage_stats` schema to match v2 columns

**Recommendation**: Adapt admin queries. The rogue-core schema is the canonical one now. Map `bot_id` → `agent_id`, `power_sets` → `powers` (or whatever Warp writes). Read `core/warp.go` to see exact columns.

**`since` parsing**: Supports "today", "1d", "7d", "30d", ISO date. Keep this logic.

---

### powerset

**What it does**: Grant/revoke/list power sets per user+agent+channel.

**v2 DB**: `$ROGUE_DATA/dbs/iam.sqlite`
**v3 namespace**: `iam`

**Schema**:
```sql
-- v2 table name
user_power_sets (user_id, bot_id, chat_id DEFAULT 0, power_set_name, assigned_by, assigned_at)
UNIQUE INDEX: (user_id, chat_id, power_set_name)

-- rogue-core table name (created by Helmet)
user_powers (agent_id, user_id, channel_id, power_name, assigned_by, assigned_at)
UNIQUE: (agent_id, user_id, channel_id, power_name)
```

**Watch out**: Different table names AND column names:
- `user_power_sets` → `user_powers`
- `bot_id` → `agent_id`
- `chat_id` (int, 0=global) → `channel_id` (string, ""=global)
- `power_set_name` → `power_name`

The powerset tool needs to query `user_powers` with the rogue-core column names.

**Tools**: `powerset_list`, `powerset_user`, `powerset_grant`, `powerset_revoke`, `powerset_create`

**Env vars**: `ROGUE_DATA`, `POWERS_DIR` (replaces `ROGUE_HOME` — point directly at power_sets/).

**`powerset_create`**: Writes a `.md` file to the powers directory. Needs `POWERS_DIR` env.

---

### queue

**What it does**: Persistent task queues with priorities, due dates, search.

**v2 DB**: `$ROGUE_DATA/dbs/queues.sqlite`
**v3 namespace**: `queue`

**Schema**:
```sql
queues (id, name UNIQUE, description, file, created_at)

queue_items (id, queue_id FK, title, notes, file, source, priority 1-5,
             status, due_at, created_at, resolved_at)
INDEXES: (queue_id, status), (queue_id, priority, created_at), (created_at)
```

**Tools**: `queue_create`, `queue_list`, `queue_items`, `queue_push`, `queue_peek`,
`queue_resolve`, `queue_remove`, `queue_reorder`, `queue_move`, `queue_search`, `queue_update`

**Use cases**:
- Main queue for cross-project work
- Project-specific queues (clusterlab, offmon, lupo)
- Scheduler pushes completed tasks here (queue field in scheduled tasks)
- Source tracking: manual, scheduler, demon, reminder

**Complexity**: Medium. Many tools but straightforward CRUD + search.

---

### gc

**What it does**: Cleans up temp files, old logs, stale sessions. Mostly filesystem ops.

**v2 DB**: `$ROGUE_DATA/dbs/iam.sqlite` (sessions table only)
**v3 namespace**: `iam` (for session cleanup)

**Tools**: `gc_run` (dry_run parameter, default true)

**Cleanup targets**:
1. `/private/tmp/claude-501/` — all temp files
2. `~/.claude/debug/` — files older than 7 days
3. `~/.claude/todos/` — files older than 7 days AND ≤2 bytes
4. `~/.claude/tasks/` — orphaned (no matching team)
5. `~/.claude/shell-snapshots/` — older than 30 days
6. Sessions in DB older than 60 days

**Watch out**: The session cleanup queries `sessions` table. Rogue-core doesn't have a `sessions` table in the IAM namespace — it has `chats`. Decide whether GC should clean `chats` or skip DB cleanup entirely.

**Protected paths**: `projects`, `file-history`, `plugins` — never deleted.

---

### telegram

**What it does**: Queues outbound messages/files/photos for async Telegram delivery.

**v2 storage**: File-based queue at `$ROGUE_DATA/telegram-outbox/`
**v3 approach**: Same file queue, or use `store.Namespace("telegram")` for file ops

**No database. No schema.**

**Tools**: `send_file`, `send_photo`, `send_message`

**Env vars**: `ROGUE_BOT_ID`, `ROGUE_CHAT_ID` (set by pipeline at spawn time)

**Outbox message format** (JSON file):
```json
{
  "type": "file|photo|message",
  "bot_id": "...",
  "chat_id": "...",
  "file_path": "...",
  "caption": "...",
  "text": "...",
  "timestamp": 1234567890
}
```

**Watch out**: The pipeline's `sources/telegram/` source needs to poll/watch this outbox directory and actually send messages. The MCP tool only queues — something else consumes. In v2, the main `cmd/rogue/` binary handled this. In rogue-core, the Telegram source's `Send()` method handles direct responses, but file uploads queued by the MCP tool need a separate consumer. Check if this is handled or needs adding.

---

### scheduler

**What it does**: Schedule tasks for future execution (one-shot or cron).

**v2 DB**: `$ROGUE_DATA/dbs/threads.sqlite`
**v3 namespace**: `scheduler`

**Schema**:
```sql
scheduled_tasks (id, user_id, chat_id, bot_id, message_text, scheduled_for,
                 cron_expr, require_reply, queue, status, last_run, run_count, created_at)
```

**v3 schema** (created by `core/schedule.go`):
```sql
tasks (id, agent_id, user_id, channel_id, source_id, message_text, scheduled_for,
       cron_expr, reply, queue, status, tags, created_at, updated_at)
```

**Column mapping**:
- `bot_id` → `agent_id`
- `chat_id` → `channel_id`
- `require_reply` → `reply`
- v3 adds: `source_id`, `tags`, `updated_at`
- v3 drops: `last_run`, `run_count`

**Tools**: `schedule`, `list_tasks`, `cancel_task`, `delay_task`

**Watch out**: The `schedule` tool in v2 takes `bot_id`, `chat_id`, `user_id` as required params. In rogue-core, these come from env vars (`ROGUE_AGENT_ID`, `ROGUE_CHANNEL_ID`, etc.) set by the pipeline at MCP spawn time. The tool should auto-fill from env when not explicitly passed.

**Binary name collision**: `cmd/rogue-scheduler/` (core MCP) exists alongside this tool. The core one was a quick implementation during phase 7. This v2 tool is more complete. Decision: replace `cmd/rogue-scheduler/` with the migrated `tools/scheduler/`.

---

### scraper

**What it does**: Headless Chrome scraping. Loads JS-rendered pages, extracts text or structured data, takes screenshots.

**No database.**

**Tools**: `scrape`, `scrape_multi`, `scrape_extract`

**Dependencies**: `chromedp` (adds ~15MB to binary). This is the only tool with a heavy external dependency.

**File output**: Screenshots saved to `$ROGUE_DATA/screenshots/screenshot_{nanoTimestamp}.png`

**Use cases**:
- `scrape`: Load page, get full text or CSS-selected content, optional screenshot
- `scrape_multi`: Batch scrape multiple URLs
- `scrape_extract`: Structured extraction — repeating elements with field selectors (like scraping product listings, tables, search results). Uses `querySelectorAll` + field mapping.

**Store pattern**: Could use `store.Namespace("scraper").FilePath("screenshots/...")` for screenshot storage, but direct filesystem access is simpler. No DB needed.

**Watch out**: Needs Chrome/Chromium installed on the host. The `chromedp` dep will pull in a lot of indirect dependencies to rogue-core's `go.sum`.

---

### memory

**What it does**: The Swiss Army knife. Multi-database SQL access, file read/write, context tracking, open items, git backup.

**v2 DB**: Pool manager opening any `$ROGUE_DATA/dbs/{name}.sqlite` on demand
**v3 approach**: `store.Namespace(dbName).DB()` — dynamic namespace per database name

**Databases accessed** (dynamically, any name works):
- `rogue` — core data (quests, tasks, ideas, talks, bookmarks, memory)
- `finances` — monthly budget tracking
- `reading` — book tracking
- `watching` — movie/series tracking
- `running` — race and training tracking
- `contacts` — alias mappings
- `threads` — contexts and open items (also used by scheduler)
- `iam` — users, sessions, permissions, usage
- `phd` — academic research

**Tools**:

| Tool | What it does |
|------|-------------|
| `sql` | Execute any SQL on any named database |
| `file_read` | Read file from `$ROGUE_DATA/files/` |
| `file_write` | Write file to `$ROGUE_DATA/files/` |
| `file_list` | List files, supports glob patterns |
| `db_list` | List all available databases |
| `backup` | WAL checkpoint all DBs, git commit+push |
| `context_create` | Create focus context (threads DB) |
| `context_list` | List contexts by status |
| `context_activate` | Activate context (max 3) |
| `context_sleep` | Sleep a context |
| `context_sleep_all` | Sleep all active contexts |
| `item_create` | Create open item/task |
| `item_list` | List items |
| `item_resolve` | Mark item done |
| `item_update` | Update item fields |

**Store migration — the `db_list` problem**:
v2 scans `$ROGUE_DATA/dbs/*.sqlite`. With Store, databases live at `$ROGUE_DATA/db/{name}/store.sqlite`. The `db_list` handler needs to scan `$ROGUE_DATA/db/` directories instead.

**Store migration — the `sql` handler**:
v2: `dbManager.Get(dbName)` → opens `$ROGUE_DATA/dbs/{dbName}.sqlite`
v3: `store.Namespace(dbName).DB()` → opens `$ROGUE_DATA/db/{dbName}/store.sqlite`
Direct mapping. The tool user passes `db="rogue"` and it just works.

**Store migration — file ops**:
v2: direct path `$ROGUE_DATA/files/{path}`
v3 option A: `store.Namespace("files").ReadFile(path)` → `$ROGUE_DATA/files/files/{path}` (double "files" — ugly)
v3 option B: keep direct filesystem access for files, only use Store for DB
**Recommendation**: Option B. Files don't need namespacing — they're already organized by the user. Use `filepath.Join(os.Getenv("ROGUE_DATA"), "files", path)` directly.

**Store migration — backup**:
v2: iterates all DB files, runs `PRAGMA wal_checkpoint`, then `git add/commit/push` in ROGUE_DATA
v3: `store.Backup(ctx)` handles WAL checkpoints. Git ops stay as direct `exec.Command`.

**Complexity**: High. This is the biggest tool and the most important one. Migrate last.

---

### phd

**What it does**: Systematic literature review automation. Searches academic databases, downloads PDFs, extracts text, screens papers with LLM, tracks PRISMA flow.

**v2 DB**: `$ROGUE_DATA/dbs/phd.sqlite`
**v3 namespace**: `phd`

**Schema**:
```sql
projects (id, name, description, owner, created_at)
topics (id, name, description, project_id FK, created_at)
papers (id, external_id UNIQUE, title, authors, abstract, year, source, url,
        pdf_path, pdf_url, full_text, fingerprint, created_at)
searches (id, topic_id FK, keywords, databases, year_range, paper_count, created_at)
screening_decisions (id, paper_id FK, stage, decision, reason, criteria_used, created_at)
extractions (id, paper_id FK, category, content, citation, created_at)
paper_topics (paper_id, topic_id) — many-to-many
search_papers (search_id, paper_id) — many-to-many
```

**Tools**: 15 tools total — search, download, screen, extract, list, export (PRISMA, BibTeX, CSV)

**External APIs**: arXiv, PubMed, Semantic Scholar

**THE BIG ISSUE — direct LLM dependency**:

`internal/llm/llm.go` imports `claude-agent-sdk-go` and calls Claude directly for:
1. `screen_papers_batch` — batch screening against criteria
2. `assess_paper_eligibility` — full-text eligibility assessment
3. `extract_paper_insights` — extract structured insights by category

This violates the rogue-core architecture where tools are AI-agnostic and the agent layer (Cerebro) handles LLM calls.

**Migration plan for LLM removal**:
1. Remove `internal/llm/` entirely
2. Remove `claude-agent-sdk-go` dependency
3. Change `screen_papers_batch` to return paper abstracts + criteria as structured data. The agent (Doom) reads the output and makes screening decisions, then calls a new `submit_screening_decision` tool to record them.
4. Change `assess_paper_eligibility` similarly — return full text + criteria, let agent assess, then record via tool.
5. Change `extract_paper_insights` — return full text + categories, let agent extract, then store via tool.
6. Add new tools: `submit_screening_decision(paper_id, stage, decision, reason)`, `submit_extraction(paper_id, category, content, citation)`
7. The agent workflow becomes: search → get papers → tool returns text → agent thinks → agent calls submit tool

**Benefits**: Works with any AI provider, not just Claude. The agent persona (Doom) already has the domain expertise in its system prompt.

**Dependencies after cleanup**: `ledongthuc/pdf` (PDF text extraction), HTTP clients for search APIs. No LLM SDK.

**Complexity**: High. But the LLM removal is a separate task from the Store migration. Do Store migration first (just swap DB init), document the LLM removal plan, execute it later.

---

## Agents

### rogue
- **Role**: External Te — decision maker, executor, boundary enforcer
- **Persona**: Southern drawl tics, 1-3 sentence responses, max 3 active contexts, no silent deletions
- **Powers typically granted**: admin (all tools)
- **Migration notes**: Copy as-is. The persona references tool behaviors but not specific implementations.

### doom
- **Role**: PhD researcher — literature review, gap analysis, methodology critique
- **Persona**: Doctor Doom persona, cold authority, deploys "Doombots" via teams
- **Powers typically granted**: phd, memory, scheduler, teams
- **Migration notes**: Copy as-is. References "Latveria" team and "Doombots" (teammates). The team orchestration uses Claude Code's native team feature, not rogue-specific code. The PHD LLM removal will change Doom's workflow — currently the tools do screening, post-migration Doom does the thinking and records decisions.

### magik
- **Role**: Orchestrator — decomposes problems, deploys agent teams, coordinates
- **Persona**: Russian tics, cold precision, no tables ever, spawns "demons" (teammates)
- **Powers typically granted**: builder, teams, memory
- **Migration notes**: Copy as-is. References "Limbo" team and demon spawning. Same team mechanism as Doom.

### psylocke
- **Role**: External Ni — foresight, convergence, cuts noise, strikes at essence
- **Persona**: Sparse piercing sentences, eliminates options
- **Powers typically granted**: readonly, scout
- **Migration notes**: Copy as-is. Minimal tool usage, mostly conversational.

### emma
- **Role**: Inner strategist — power dynamics, boundary setting, dignity
- **Persona**: Polished, controlled, strategic. No emotional padding.
- **Powers typically granted**: readonly
- **Migration notes**: Copy as-is. Conversational agent, minimal tool usage.

### luna
- **Role**: Emotional regulator — stabilizes, soothes, restores flow
- **Persona**: Soft-spoken, reassuring, protective of energy
- **Powers typically granted**: readonly, journal
- **Migration notes**: Copy as-is. Conversational, may use journal for emotional processing.

### cyclops
- **Role**: Inner commander — objective clarity, commitment, holds the line
- **Persona**: Direct, commanding, no hypotheticals
- **Powers typically granted**: readonly
- **Migration notes**: Copy as-is. Conversational.

### groot
- **Role**: The tree
- **Persona**: "I Am Groot" for everything
- **Powers typically granted**: none
- **Migration notes**: Copy as-is. Three words.

---

## Power Sets

### admin
- **Grants**: All memory tools + powerset tools + scheduler tools + admin analytics
- **Migration notes**: Update tool references if any tool names change. This is the "god mode" power set.

### memory
- **Grants**: sql, file_read, file_write, file_list, db_list, backup
- **Migration notes**: Core data tools. Tool names stay the same.

### scheduler
- **Grants**: schedule, list_tasks, cancel_task, delay_task
- **Migration notes**: Tool names stay the same. Parameter names may change (`bot_id` → `agent_id`).
- **Watch out**: The power set instructions reference `bot_id` parameter. Update to `agent_id`.

### queue
- **Grants**: All queue_* tools
- **Migration notes**: Tool names stay the same. No parameter changes.

### powers
- **Grants**: powerset_list, powerset_user, powerset_grant, powerset_revoke, powerset_create
- **Migration notes**: Tool names stay the same. Parameter `bot_id` → `agent_id`.

### contacts
- **Grants**: alias_set, alias_get, alias_list, alias_delete
- **Migration notes**: No changes needed.

### scraper
- **Grants**: scrape, scrape_multi, scrape_extract
- **Migration notes**: No changes needed.

### telegram
- **Grants**: send_file, send_photo, send_message
- **Migration notes**: No changes needed.

### phd
- **Grants**: All phd tools (15 total)
- **Migration notes**: After LLM removal, add `submit_screening_decision` and `submit_extraction` tools. Remove `screen_papers_batch`, `assess_paper_eligibility`, `extract_paper_insights` from the tool list (or keep them with new behavior — return data instead of LLM results).

### threads
- **Grants**: context_* and item_* tools
- **Migration notes**: These are part of the memory MCP server. Tool names stay the same.

### journal
- **Grants**: file_read, file_write, file_list, schedule
- **Migration notes**: References file paths like `files/journal/YYYY-MM-DD.md`. With Store, files stay at `$ROGUE_DATA/files/` (direct access, not namespaced). No change needed.
- **Watch out**: The `schedule` tool is from the scheduler MCP server. Power set references tools across different MCP servers — this is fine, Cerebro resolves tools from the MCPRegistry regardless of which server provides them.

### reading
- **Grants**: sql (db=reading), file_read, file_write
- **Database**: `reading` namespace
- **Tables**: books, book_takeaways, books_wishlist
- **Migration notes**: Schema stays the same. DB access via `store.Namespace("reading").DB()` from the memory tool's `sql` handler.

### watching
- **Grants**: sql (db=watching)
- **Database**: `watching` namespace
- **Table**: watchlist
- **Migration notes**: Same as reading.

### running
- **Grants**: sql (db=running)
- **Database**: `running` namespace
- **Tables**: races, runs
- **Migration notes**: Same as reading.

### finances
- **Grants**: sql (db=finances)
- **Database**: `finances` namespace
- **Table**: months
- **Migration notes**: Same as reading.

### readonly
- **Grants**: sql (SELECT only), file_read, file_list, db_list, context_list, item_list, list_tasks
- **Migration notes**: Restricts SQL to SELECT/PRAGMA/EXPLAIN. This restriction is enforced by the agent's system prompt, not the tool itself. The `sql` tool doesn't validate query type.
- **Watch out**: If you want hard enforcement, add query validation to the `sql` handler. But the current approach (trust the agent + power set instructions) works fine.

### builder
- **Grants**: Read, Glob, Grep, Edit, Write, Bash
- **Migration notes**: These are Claude Code native tools, not MCP tools. Cerebro passes them as `--allowedTools` to the claude CLI. No migration needed.

### hackathon, clusterlab, lupo, offmon
- **Grants**: file_read, file_write + Claude Code native tools (Read, Glob, Grep, Edit, Write, Bash)
- **Migration notes**: Each has a working directory and job memory structure. The file_read/write reference paths in `$ROGUE_DATA/files/`. The native tools reference local filesystem paths. No migration needed — just copy the power set files.
- **Watch out**: The `--add-dir` flag (working directory) is driven by the `directories` field in the power set frontmatter. Verify Cerebro passes this correctly.

### mutate
- **Grants**: file_read, file_write + native tools
- **Migration notes**: Working directory points to `/Users/niemand/Desktop/agents/rogue`. Self-modification power. In rogue-core, this should point to the rogue-core repo directory. Update the `directories` frontmatter.

### scout
- **Grants**: Read, Glob, Grep, WebSearch
- **Migration notes**: Read-only native tools + web search. No MCP tools. No changes needed.

### quests
- **Grants**: sql, db_list, backup, context tools, item tools
- **Database**: `rogue` namespace
- **Tables**: quest_meta, tasks, ideas, talk_sessions, bookmarks, memory
- **Migration notes**: The `sql` handler accesses `db="rogue"`. With Store, this becomes `store.Namespace("rogue").DB()`. Schema stays the same — tables are created by the power set instructions, not by tool code. The agent creates tables on first use via `sql` tool.

### teams
- **Grants**: TeamCreate, TeamDelete, SendMessage, Task
- **Migration notes**: These are Claude Code native tools for team orchestration. Not MCP tools. No migration needed.

---

## Store Patterns — How Each Tool Should Use It

### Pattern 1: Single namespace (contacts, admin, queue, gc)
```go
store := core.NewSQLiteStore(os.Getenv("ROGUE_DATA"), logger)
defer store.Close()
db, _ := store.Namespace("contacts").DB()
// All operations on this one DB
```

### Pattern 2: Shared namespace (admin + powerset + gc → iam)
```go
db, _ := store.Namespace("iam").DB()
// Multiple tools share the same namespace
// Schema created by Helmet — tools just query it
// Watch for schema assumptions — read Helmet's ensureSchema()
```

### Pattern 3: Dynamic namespace (memory's sql handler)
```go
// User passes db="running" or db="finances"
dbName := args["db"].(string)
db, _ := store.Namespace(dbName).DB()
// Creates $ROGUE_DATA/db/{dbName}/store.sqlite on first access
```

### Pattern 4: File access without namespace (memory's file ops)
```go
// Direct path — don't use Store for files
filesDir := filepath.Join(os.Getenv("ROGUE_DATA"), "files")
path := filepath.Join(filesDir, userPath)
// Validate: no ".." traversal, stays within filesDir
```

### Pattern 5: No DB (scraper, telegram)
```go
// These tools don't need Store at all
// Scraper writes screenshots to $ROGUE_DATA/screenshots/
// Telegram writes JSON to $ROGUE_DATA/telegram-outbox/
```

---

## Config Shape

```yaml
store:
  data_dir: "env:ROGUE_DATA"

helmet:
  root_resolver:
    type: env_match
    env: OWNER_ID
  powers_dir: ./power_sets
  agents_dir: ./agents

cerebro:
  default_provider: claude-code
  max_turns: 100
  max_agent_depth: 3
  tools:
    rogue-memory:
      command: ./bin/rogue-memory
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-scheduler:
      command: ./bin/rogue-scheduler
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-powerset:
      command: ./bin/rogue-powerset
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
        POWERS_DIR: ./power_sets
    rogue-admin:
      command: ./bin/rogue-admin
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-contacts:
      command: ./bin/rogue-contacts
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-queue:
      command: ./bin/rogue-queue
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-gc:
      command: ./bin/rogue-gc
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-scraper:
      command: ./bin/rogue-scraper
    rogue-telegram:
      command: ./bin/rogue-telegram
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-phd:
      command: ./bin/rogue-phd
      env:
        ROGUE_DATA: "env:ROGUE_DATA"

telepath:
  sources:
    - type: telegram
      id: "telegram:rogue"
      token: "env:ROGUE_BOT_TOKEN"
      agent: rogue
      debounce_ms: 5000

scheduler:
  tick_interval: "30s"
```

---

## Open Questions

1. **Core MCP binaries overlap**: `cmd/rogue-scheduler/`, `cmd/rogue-iam/`, `cmd/rogue-store/` duplicate functionality that the migrated custom tools provide. Drop the core ones? Or keep both and let the config choose?

2. **Telegram outbox consumer**: The `telegram` MCP tool queues messages as JSON files. What consumes them? In v2, the main binary polled the outbox. In rogue-core, the Telegram source's `Send()` handles direct responses from the pipeline, but MCP-queued messages need a separate consumer loop.

3. **File namespace**: The memory tool's file ops use `$ROGUE_DATA/files/` directly. Store's `Namespace.ReadFile()` adds an extra namespace level (`$ROGUE_DATA/files/{namespace}/`). Keep direct access for file ops to avoid path disruption.

4. **Schema ownership**: Who creates tables? In v2, each tool runs its own migrations. In rogue-core, Helmet creates IAM tables. If admin/powerset/gc also try to create the same tables, the `IF NOT EXISTS` clause handles it gracefully. But schema drift is possible if tools add columns that Helmet doesn't know about.

5. **GC sessions table**: v2 GC cleans `sessions` table. Rogue-core has `chats` table instead. Update GC to clean `chats`, or skip DB cleanup.
