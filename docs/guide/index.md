# Introduction

Rogue Core is a Go framework for building AI agent pipelines. A message arrives from a source (Telegram, CLI), passes through IAM, hits an AI provider (Claude Code), and the response routes back out.

Everything is config-driven and interface-based. The framework handles the plumbing — you define the agents, powers, and config.

## Two-Repo Pattern

Rogue Core uses a separation between **framework** and **instance**:

- **rogue-core** (public) — the framework. Pipeline, IAM, storage, scheduling, MCP tools. Imported as a Go module.
- **instance** (private) — your deployment. Agent personas, power overrides, config, ROOT.md. References rogue-core.

```
my-instance/
  agents/          — agent persona files (.md)
  powers/          — power overrides (.md with YAML frontmatter)
  config.yaml      — pipeline configuration
  ROOT.md          — shared prompt template
  rogue-core/      — framework (gitignored, cloned separately)
```

## Component Codenames

Each component has an X-Men-inspired codename:

| Codename | Role |
|----------|------|
| **Telepath** | Message bus — sources in, responses out |
| **Helmet** | IAM — users, sessions, powers, approval gate |
| **Cerebro** | Agent orchestrator — provider selection, prompt assembly, MCP config |
| **Warp** | Response handler — routing, usage recording |
| **Store** | Namespaced SQLite + file storage |
| **Schedule** | Cron + one-shot task scheduler |
| **Coordinator** | Process supervisor — hot reload on binary change |

## Key Design Decisions

- **SQLite everywhere** — no external database dependencies. Each namespace gets its own `store.sqlite`.
- **Powers over roles** — fine-grained capability bundles instead of coarse roles. A power grants specific tools + instructions to a specific user on a specific agent in a specific channel.
- **Embedded core powers** — scheduler, memory, and powers management ship with the framework. Instance powers override core powers by name.
- **Config-driven** — agent assignment, source routing, MCP tools, IAM rules — all in one YAML file with env substitution.
