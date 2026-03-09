# Quick Start

## Prerequisites

- Go 1.23+
- Claude Code CLI (`claude`)
- A Telegram bot token (for Telegram sources)

## Setup

```bash
# Clone the framework
git clone https://github.com/kidkuddy/rogue-core.git
cd rogue-core

# Build everything
make build
```

This produces:
- `rogue-pipeline` — main binary
- `rogue-coordinator` — process supervisor
- `rogue-store` — storage MCP server
- `rogue-scheduler` — scheduling MCP server
- `rogue-iam` — IAM MCP server
- `rogue-telegram` — Telegram interaction MCP server

## Instance Setup

Create your instance directory alongside rogue-core:

```bash
mkdir my-instance && cd my-instance

# Create directory structure
mkdir agents powers
```

### Minimal Config

```yaml
# config.yaml
store:
  data_dir: ./data

helmet:
  require_approval: false
  root_resolver:
    type: always_true
  powers_dir: ./powers
  agents_dir: ./agents

cerebro:
  default_provider: claude-code
  max_turns: 50
  tools:
    rogue-store:
      command: ./rogue-core/rogue-store
      env:
        ROGUE_DATA: ./data
    rogue-scheduler:
      command: ./rogue-core/rogue-scheduler
      env:
        ROGUE_DATA: ./data
    rogue-iam:
      command: ./rogue-core/rogue-iam
      env:
        ROGUE_DATA: ./data

telepath:
  sources:
    - type: cli
      id: cli:main
      agent: assistant

scheduler:
  tick_interval: "30s"
```

### Minimal Agent

```markdown
<!-- agents/assistant.md -->
# Assistant

You are a helpful assistant.
```

### Run

```bash
cd rogue-core && ./rogue-pipeline ../config.yaml
```

The CLI source will start an interactive session. Type a message and the agent responds.

## Adding Telegram

1. Create a bot via [@BotFather](https://t.me/BotFather)
2. Add to config:

```yaml
telepath:
  sources:
    - type: telegram
      id: "telegram:assistant"
      token: "env:TELEGRAM_BOT_TOKEN"
      agent: assistant
      debounce_ms: 5000
```

3. Set the env var and run:

```bash
export TELEGRAM_BOT_TOKEN=your_token_here
cd rogue-core && ./rogue-pipeline ../config.yaml
```

## Using the Coordinator

The coordinator watches the pipeline binary and restarts it on changes (hot reload):

```bash
cd rogue-core && ./rogue-coordinator ./rogue-pipeline ../config.yaml
```
