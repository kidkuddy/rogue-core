# Telegram Integration

Rogue Core supports Telegram as a message source with typing indicators, emoji reactions, and proactive messaging.

## Source Setup

Each agent gets its own Telegram bot:

```yaml
telepath:
  sources:
    - type: telegram
      id: "telegram:rogue"
      token: "env:TELEGRAM_BOT_TOKEN_ROGUE"
      agent: rogue
      debounce_ms: 5000
```

### Debounce

`debounce_ms` batches rapid messages into one. If a user sends 3 messages in quick succession, they're concatenated (with blank line separation) and processed as a single request. Default: no debounce.

## Typing Indicators

The Telegram source implements `TypingSource`. When an agent is processing a message, a typing indicator ("typing...") appears in the chat automatically. It starts before execution and stops when the response is ready.

## Reactions & Proactive Messages (rogue-telegram)

The `rogue-telegram` MCP server enables agent-controlled Telegram interactions:

| Tool | Description |
|------|-------------|
| `react(emoji, message_id?, chat_id?)` | React to a message with an emoji |
| `send_message(text, chat_id?)` | Send a proactive message to a chat |

These tools use env vars (`TELEGRAM_BOT_TOKEN`, `ROGUE_CHANNEL_ID`, `ROGUE_MESSAGE_ID`) that are automatically passed from the Telegram source through the pipeline.

### Granting Telegram Power

Create a telegram power (or use an instance override):

```markdown
---
name: telegram
description: Telegram interactions
tools:
  - mcp__rogue-telegram__react
  - mcp__rogue-telegram__send_message
---

React to messages and send proactive messages.
```

## Environment Flow

Telegram-specific env vars flow through the system:

1. `TelegramSource.SourceEnv()` exposes `TELEGRAM_BOT_TOKEN`
2. Pipeline injects it into `EnrichedMessage.SourceEnv`
3. Cerebro's `buildEnv()` merges it with session env (user_id, channel_id, etc.)
4. `GenerateConfig()` passes all env to MCP tool processes
5. `rogue-telegram` reads the token and channel from its environment
