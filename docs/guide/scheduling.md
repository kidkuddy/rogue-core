# Scheduling

The scheduler creates time-triggered tasks that fire as messages through the pipeline. When a task fires, it's processed as if the user sent a message to the specified agent.

## Task Types

### One-Shot Tasks

Fire once at a specific time:

```
schedule(agent_id="rogue", message="remind me to call the dentist", scheduled_for="2026-03-10T09:00:00Z")
```

### Recurring Tasks (Cron)

Fire on a cron schedule:

```
schedule(agent_id="rogue", message="weekly review time", scheduled_for="2026-03-10T09:00:00Z", cron_expr="0 9 * * 1")
```

Cron format: `minute hour dom month dow`

Examples:
- `0 9 * * *` — daily at 9am
- `0 20 * * 5` — Friday at 8pm
- `0 10 * * 1` — Monday at 10am
- `*/30 * * * *` — every 30 minutes

## Task Lifecycle

```
pending → running → done           (one-shot, no ack)
pending → running → awaiting_ack   (one-shot, requires ack)
pending → running → pending        (cron, no ack — reschedules)
pending → running → awaiting_ack   (cron, requires ack — blocks next run)
```

## Acknowledgment System

Tasks can require explicit user acknowledgment before they're considered complete:

```
schedule(agent_id="rogue", message="review finances", scheduled_for="...", requires_ack=true)
```

When `requires_ack` is true:
- After firing, the task stays in `awaiting_ack`
- The user must explicitly acknowledge: `ack_task(task_id)`
- Cron tasks **do not reschedule** until the current run is ACK'd
- Delaying an `awaiting_ack` task moves it back to `pending`

This prevents the common problem of reminders firing and being lost — important tasks stay visible until acted on.

### When to Use ACK

- Reminders that need confirmation ("did you actually do this?")
- Review tasks ("weekly financial review")
- Check-ins that shouldn't stack up
- Any task where "fired" doesn't mean "handled"

### When Not to Use ACK

- Background/silent tasks (`reply=false`)
- Informational broadcasts
- Tasks where firing is the entire action

## Operations

| Action | Description |
|--------|-------------|
| `schedule(...)` | Create a new task |
| `list_tasks(status?)` | List tasks, optionally filtered |
| `cancel_task(task_id)` | Cancel a pending/running task |
| `delay_task(task_id, duration)` | Postpone a pending or awaiting_ack task |
| `ack_task(task_id)` | Acknowledge a completed task |

## Crash Recovery

On startup, the scheduler resets any tasks stuck in `running` back to `pending`. Tasks in `awaiting_ack` are preserved — they represent intentional user-facing state.

## Source Routing

Tasks inherit the source from their creation context. A task created via Telegram routes its response back through Telegram. The `source_id`, `channel_id`, and `user_id` fields control routing.
