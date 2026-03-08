# Rogue Core

## What This Is

Go framework for AI agent pipelines. Message comes in from a source, goes through IAM, hits an AI provider, response goes back out. Everything is interface-driven and config-driven.

## Codebase

```
core/             — framework (pipeline, IAM, store, scheduler, types, interfaces)
providers/        — AI provider adapters (claudecode)
sources/          — message source adapters (telegram, cli, agent)
cmd/
  rogue-pipeline/     — main binary, runs the message loop
  rogue-coordinator/  — process supervisor (hot reload, crash recovery)
tools/
  rogue-store/        — MCP server: namespaced DB + file access
  rogue-scheduler/    — MCP server: task scheduling
  rogue-iam/          — MCP server: usage/power/user queries
```

## Component Codenames

- **Telepath** — message bus (sources in, responses out)
- **Helmet** — IAM (users, sessions, powers, approval gate)
- **Cerebro** — agent orchestrator (provider selection, session state, MCP config)
- **Warp** — response handler (routing, usage recording)
- **Store** — namespaced SQLite + file storage
- **Schedule** — cron + one-shot task scheduler
- **Coordinator** — process supervisor (hot reload on binary change)

## Key Patterns

### Store namespaces
```go
store.Namespace("iam").DB()           // $ROGUE_DATA/iam/db/store.sqlite
store.Namespace("scheduler").DB()     // $ROGUE_DATA/scheduler/db/store.sqlite
store.Namespace("contacts").DB()      // $ROGUE_DATA/contacts/db/store.sqlite
// Files: $ROGUE_DATA/<namespace>/files/
```
Dynamic — any string works. DB created on first access.

### Config
YAML with `"env:VAR"` and `"${VAR}"` substitution. See `core/config.go`.

### Approval gate
`helmet.require_approval` defaults to `true`. New users get `approved=false`. Root always bypasses. Set `require_approval: false` to let everyone through.

### Powers
Power set files in `powers/` with YAML frontmatter (name, tools, directories) and markdown body (instructions). Granted per user+agent+channel via `user_powers` table.

### MCP tools
mcp-go v0.45. Use `req.GetArguments()["key"]` not `req.Params.Arguments["key"]`.

## Build & Test

```bash
make build    # all binaries
make test     # go test ./... -count=1
make clean    # rm binaries
```

Go is not Rust — no `--release` flag. Just `go build` / `go test`.


## Commit Convention

```
feat(rogue-core): ...
fix(rogue-core): ...
refactor(rogue-core): ...
doc: ...
```

No Co-Authored-By lines. No Claude-related text in commits or PRs.

## Rules

- Read code before changing it
- Run tests after changes
- Don't add features beyond what's asked
- Don't create documentation files unless asked
- Schema migrations: `ALTER TABLE ... ADD COLUMN` (silently fails if exists). Never drop columns.
- Fresh start — no backward compatibility with v2 data layout
