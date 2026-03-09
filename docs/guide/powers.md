# Powers

Powers are capability bundles that grant tools, directories, and instructions to users. They're the authorization layer between users and AI capabilities.

## Power File Format

Powers are markdown files with YAML frontmatter:

```markdown
---
name: memory
description: Namespaced storage for structured data and files
tools:
  - mcp__rogue-store__sql
  - mcp__rogue-store__file_read
  - mcp__rogue-store__file_write
directories:
  - /path/to/allow
---

## Memory

Instructions for the agent when this power is active.

### Rules

- Always use the namespace from the domain power
- Never delete data without confirmation
```

### Frontmatter Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | no | Power name (defaults to filename) |
| `description` | no | Human-readable description |
| `tools` | no | List of MCP tools to grant |
| `directories` | no | List of filesystem paths to allow |
| `namespace` | no | Default namespace for storage operations |
| `requires` | no | Documentation-only dependency list |

## Core Powers

Three powers ship embedded with rogue-core:

- **powers** — Power and user management (grant, revoke, list, approve/block). Root users get this automatically.
- **memory** — Namespaced SQLite + file storage. Paired with domain powers.
- **scheduler** — Task scheduling with cron, one-shot, and acknowledgment support.

Core powers are loaded from the binary itself. Instance powers with the same name **override** core powers.

## Instance Powers

Create power files in your `powers_dir` to add capabilities:

```markdown
---
name: scratchpad
description: Ephemeral notes and scratch space
requires:
  - memory
namespace: scratchpad
---

## Scratchpad

Temporary storage for notes, drafts, and working data.
Use namespace `scratchpad` for all operations.
```

## Granting Powers

Powers are granted per user, per agent, per channel:

```
power_grant(user_id, agent_id, power_name, channel_id)
```

- **channel_id = ""** → global grant (applies to all channels for that agent)
- **channel_id = "12345"** → scoped to that specific channel

Root users always have the `powers` power to manage grants.

## PowerSet Resolution

When a message arrives, Helmet resolves the user's PowerSet:

1. Query `user_powers` for channel-specific grants
2. Query `user_powers` for global grants (channel_id = "")
3. If root: add `powers` power
4. Deduplicate
5. Load each power file (instance dir first, then core embedded)
6. Merge: union of tools, union of directories, concatenate instructions

The merged PowerSet determines what tools the agent can use and what instructions it receives.

## Domain Powers

Domain powers define a namespace and pair with the `memory` power:

```markdown
---
name: reading
namespace: reading
requires:
  - memory
---

## Reading List

Track books. Use namespace `reading` for all storage.
```

When granting a domain power, always grant `memory` alongside it — the domain power provides the namespace and context, while memory provides the storage tools.
