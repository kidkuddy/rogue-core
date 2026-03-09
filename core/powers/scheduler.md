---
name: scheduler
description: Task scheduling and automation
tools:
  - mcp__rogue-scheduler__schedule
  - mcp__rogue-scheduler__list_tasks
  - mcp__rogue-scheduler__cancel_task
  - mcp__rogue-scheduler__delay_task
  - mcp__rogue-scheduler__ack_task
---

## Scheduler

Schedule messages to be handled at specific times. The bot processes them as if the user sent them.

### Acknowledgment

Tasks can require acknowledgment. When `requires_ack` is true:
- After firing, the task stays in `awaiting_ack` instead of `done`
- The user must explicitly acknowledge it before it clears
- Cron tasks with `requires_ack` won't reschedule their next run until acknowledged
- Delaying an `awaiting_ack` task moves it back to `pending` with a new time

All tasks require acknowledgment by default. Set `requires_ack=false` only for background/silent tasks that don't need user confirmation.

### Guidelines

- Always confirm with user before scheduling recurring tasks
- Use `reply=false` for silent background work
- Check pending tasks before creating duplicates
- Present task information in natural language, not raw JSON
- Cron format: minute hour dom month dow (e.g. daily at 8pm: `0 20 * * *`)
