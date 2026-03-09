# IAM (Helmet)

Helmet handles identity, access, and power management. Every message passes through Helmet before reaching the AI provider.

## User Lifecycle

1. **First message** — user is auto-created with `approved=false`
2. **Approval gate** — if `require_approval: true`, unapproved users are silently ignored
3. **Root bypass** — root users are always approved
4. **Blocking** — blocked users are silently dropped at the earliest stage

## Root Users

Root is determined by the `root_resolver` config:

```yaml
helmet:
  root_resolver:
    type: env_match
    env: OWNER_ID      # User ID must match $OWNER_ID
```

Root users always receive the `powers` power, which lets them manage other users and power grants.

## Power Grants

Powers are granted per `(user_id, agent_id, channel_id)` tuple:

- **Channel-scoped**: grants apply only to a specific channel
- **Global**: `channel_id = ""` grants apply across all channels for that agent

When resolving a user's PowerSet, both channel-specific and global grants are merged.

## Data Model

```
users
  - id, username, first_name, approved, blocked, created_at, last_seen

chats
  - id, source_id, channel_id, agent_id (UNIQUE)

user_powers
  - agent_id, user_id, channel_id, power_name, assigned_by (UNIQUE)

tags
  - chat_id, tag (UNIQUE)

usage_stats
  - timestamp, user_id, agent_id, chat_id, source_id
  - input_tokens, output_tokens, cache_read/write_tokens
  - cost_usd, duration_ms, num_turns, hit_max_turns
```

All stored in `$ROGUE_DATA/iam/db/store.sqlite`.

## MCP Tools (rogue-iam)

| Tool | Description |
|------|-------------|
| `user_list()` | List all users |
| `user_approve(user_id)` | Approve a user |
| `user_block(user_id)` | Block a user |
| `user_unblock(user_id)` | Unblock a user |
| `power_list(user_id?, agent_id?)` | List power grants |
| `power_grant(user_id, agent_id, power_name, channel_id?)` | Grant a power |
| `power_revoke(user_id, agent_id, power_name, channel_id?)` | Revoke a power |
| `usage_summary(since?, tags?)` | Aggregate usage stats |
| `usage_recent(limit?)` | Recent usage entries |

## Tags

Auto-generated tags are added to every message:
- `user:{user_id}`, `source:{source_id}`, `channel:{channel_id}`, `agent:{agent_id}`

Admin-configured tags can be added per chat via the `tags` table.

Tags are used for provider selection (e.g., `provider:claude-code`) and usage filtering.
