# Agents

Agents are AI personas defined as markdown files. Each agent has a distinct personality, voice, and role.

## Agent File

Create a markdown file in your `agents_dir`:

```markdown
<!-- agents/assistant.md -->
# Assistant

You are a helpful assistant. You answer questions clearly and concisely.

## How You Speak

- Direct and clear
- No unnecessary filler
```

The filename (without `.md`) becomes the agent ID. So `agents/rogue.md` creates agent `rogue`.

## Multiple Agents

Each agent can have its own Telegram bot, conversation history, and power set:

```yaml
telepath:
  sources:
    - type: telegram
      id: "telegram:rogue"
      token: "env:TELEGRAM_BOT_TOKEN_ROGUE"
      agent: rogue
    - type: telegram
      id: "telegram:doom"
      token: "env:TELEGRAM_BOT_TOKEN_DOOM"
      agent: doom
```

## Persona + ROOT.md

The final system prompt is assembled from multiple layers:

1. **ROOT.md** — shared rules across all agents (if enabled)
2. **Agent persona** — the agent's markdown file
3. **Time context** — current datetime, timezone, weekday
4. **Session context** — agent ID, user ID, channel ID, chat ID
5. **Power instructions** — from granted powers

If ROOT.md contains `{{agent_persona}}`, the agent's persona is substituted inline. Otherwise, the persona is prepended before ROOT.md content.

## Tool-Aware Prompts

Agent prompts should be tool-aware but not tool-dependent. Use conditional language:

```markdown
## Research

When you can search the literature directly, do it.
When you can't, say so plainly and give guidance verbally.
```

This way the agent works with or without tools — it adapts based on what powers are granted.

## Session Context

Every agent execution receives session context automatically:

- **Agent ID** — which agent is responding
- **User ID** — who sent the message
- **Channel ID** — which chat/channel
- **Chat ID** — unique conversation identifier

Agents use this for context-aware operations (e.g., the powers agent knows who to grant permissions to without asking).
