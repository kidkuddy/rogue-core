# Configuration

All configuration lives in a single YAML file. Environment variable substitution is supported via `"env:VAR"` or `"${VAR}"` syntax.

## Full Reference

```yaml
store:
  data_dir: "env:ROGUE_DATA"           # Base directory for all data

helmet:
  require_approval: true               # Unapproved users silently ignored (default true)
  root_resolver:
    type: env_match                     # "env_match", "user_list", "always_true", "always_false"
    env: OWNER_ID                       # For env_match: compare user_id against this env var
    # users: ["user1", "user2"]         # For user_list: explicit list
  powers_dir: ./powers                  # Instance power files directory
  agents_dir: ./agents                  # Agent persona files directory

cerebro:
  default_provider: claude-code         # AI provider to use
  max_turns: 100                        # Max conversation turns per request
  max_agent_depth: 3                    # Max nested agent spawns
  root_prompt: true                     # Use ROOT.md as base prompt (default true)
  prepend_persona: true                 # Prepend agent persona if no {{agent_persona}} (default true)
  root_prompt_path: ./ROOT.md           # Path to root prompt file (default ROOT.md)
  tools:                                # MCP tool server registry
    rogue-store:
      command: ./rogue-core/rogue-store
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-scheduler:
      command: ./rogue-core/rogue-scheduler
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-iam:
      command: ./rogue-core/rogue-iam
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    rogue-telegram:
      command: ./rogue-core/rogue-telegram

telepath:
  sources:
    - type: telegram                    # "telegram", "cli", "agent"
      id: "telegram:rogue"             # Unique source identifier
      token: "env:TELEGRAM_BOT_TOKEN"   # Bot token
      agent: rogue                      # Agent ID to route to
      debounce_ms: 5000                 # Message debounce (telegram only)

scheduler:
  tick_interval: "30s"                  # How often to check for due tasks
```

## Environment Substitution

Two formats are supported:

```yaml
# Full value replacement
token: "env:MY_TOKEN"

# Inline substitution
command: "${HOME}/bin/tool"
```

## Path Resolution

All relative paths in config are resolved against the config file's directory. So if your config is at `/home/user/instance/config.yaml`, then `./powers` resolves to `/home/user/instance/powers`.

## Root Resolver Types

| Type | Description |
|------|-------------|
| `env_match` | User ID must match the value of an env var |
| `user_list` | User ID must be in the provided list |
| `always_true` | Everyone is root (development) |
| `always_false` | No one is root |
