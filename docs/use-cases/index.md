# Use Cases

Rogue Core is designed for building personal AI agent systems. Here are the patterns it supports.

## Personal Assistant Team

Run multiple AI agents, each with a distinct personality and role, all talking to you via separate Telegram bots:

- **Rogue** — decision maker, boundary enforcer, task tracker
- **Doom** — PhD research assistant, literature review, methodology critique
- **Magik** — orchestrator, decomposes complex tasks into agent teams
- **Psylocke** — strategic foresight, convergence, pattern detection

Each agent has its own Telegram bot, conversation memory, and power set. Powers can be granted per agent per channel.

## Scheduled Reminders with Accountability

Use the scheduler with `requires_ack` for reminders that stick:

- "Remind me to review finances every Sunday" → cron task with `requires_ack`
- Task fires every Sunday, stays in `awaiting_ack` until you confirm you did it
- If you don't ACK, it won't reschedule — visible reminder that you skipped it
- Delay it if you need more time — it goes back to `pending`

## Domain-Specific Knowledge Bases

Create domain powers that pair with the memory power:

- **Reading list** — track books, ratings, notes (namespace: `reading`)
- **Finances** — budget tracking, expense logging (namespace: `finances`)
- **Research** — literature database, paper notes, PRISMA flow (namespace: `phd`)
- **Contacts** — people you've met, context, follow-ups (namespace: `contacts`)

Each domain gets its own SQLite database and file storage, fully isolated.

## Agent-Controlled Reactions

Agents can react to your Telegram messages with emoji — a lightweight feedback mechanism:

- Agent understands your message → reacts with a relevant emoji
- Task completed → thumbs up reaction
- Something needs attention → warning emoji
- Proactive messages → agent sends to your chat without you initiating

## Multi-Channel Access Control

Different powers per channel:

- Grant `finances` power only in your private Rogue chat
- Grant `reading` power globally across all channels
- Research powers only available through the Doom bot

Channel-scoped by default, global when explicitly requested.
