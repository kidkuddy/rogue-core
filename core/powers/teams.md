---
name: teams
description: Team orchestration — spawn, coordinate, and manage agent teams
tools:
  - TeamCreate
  - TeamDelete
  - SendMessage
  - Task
---

## Teams

Spawn and manage multi-agent teams for parallel work.

### Tools

- `TeamCreate` — Create a new team. Requires `team_name`. Optional `description` and `agent_type`.
- `TeamDelete` — Remove team + task directories. All teammates must be shut down first.
- `SendMessage` — Communicate with teammates. Types:
  - `message` — DM a specific teammate (requires `recipient`, `content`, `summary`)
  - `broadcast` — Message ALL teammates. Expensive. Use only for critical/blocking issues.
  - `shutdown_request` — Ask a teammate to gracefully exit (requires `recipient`)
  - `shutdown_response` — Respond to a shutdown request (requires `request_id`, `approve`)
  - `plan_approval_response` — Approve/reject a teammate's plan (requires `request_id`, `recipient`, `approve`)
- `Task` — Spawn a teammate into the team. Set `team_name` and `name` params. Pick `subagent_type` based on what tools the agent needs:
  - `general-purpose` — full access (read, write, edit, bash, all tools). Use for implementation.
  - `Explore` — read-only. Use for research, search, codebase exploration.
  - `Plan` — read-only. Use for architecture and planning.
  - `Bash` — shell only. Use for git, commands, terminal tasks.

### Workflow

1. `TeamCreate` — create the team
2. `Task` — spawn teammates with clear mandates (use `team_name` + `name`)
3. Teammates auto-join and pick up tasks
4. `SendMessage` (type: message) — coordinate, unblock, redirect
5. `SendMessage` (type: shutdown_request) — shut down each teammate when done
6. `TeamDelete` — clean up after all teammates confirm shutdown

### Rules

- Teammates go idle between turns — this is normal. Send a message to wake them.
- Messages from teammates are delivered automatically. No need to poll.
- Refer to teammates by **name**, never by UUID.
- Default to `message` over `broadcast`. Broadcast = N separate deliveries.
- Read team config at `~/.claude/teams/{team-name}/config.json` to discover members.
- Check task list at `~/.claude/tasks/{team-name}/` for shared coordination.

### When to Spawn Teams

- Multi-scope work (frontend + backend + tests)
- Research + build combos (explore then implement)
- Parallel independent tasks (3+ subtasks)
- Complex projects needing coordination

### When NOT to Spawn

- Single-scope task
- Quick config/command
- Strategy or advice (no execution needed)
