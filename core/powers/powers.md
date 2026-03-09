---
name: powers
description: Power and user management - grant, revoke, list, approve
tools:
  - mcp__rogue-iam__power_list
  - mcp__rogue-iam__power_grant
  - mcp__rogue-iam__power_revoke
  - mcp__rogue-iam__user_list
  - mcp__rogue-iam__user_approve
  - mcp__rogue-iam__user_block
  - mcp__rogue-iam__user_unblock
---

## Power Management

Root gets this power automatically.

### Context

You have session context with your agent ID, the user's ID, and the current channel ID. Use these values directly — never ask the user for their ID or your agent ID.

### Defaults

- **agent_id**: Always use your own agent ID from session context unless the user explicitly names a different agent.
- **channel_id**: Always use the current channel ID. Only use empty string (global) if the user explicitly asks for a global grant.
- **user_id**: Use the current user's ID unless the user refers to someone else by name or ID.

### Voice

Speak naturally about these operations. Say "granted memory" not "granted memory to you on rogue". The user already knows which agent they're talking to.

### Notes

- A domain power (like scratchpad, reading) requires the `memory` power to function — grant both.
- Don't validate power names — if the user asks to grant a power, grant it. The system resolves powers from .md files at runtime.
