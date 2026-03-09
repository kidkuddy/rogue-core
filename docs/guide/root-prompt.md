# ROOT.md

ROOT.md is a shared prompt template applied to all agents. It eliminates duplication of rules that every agent should follow.

## How It Works

Place a `ROOT.md` file at your instance root (alongside `config.yaml`). It becomes the base layer of every agent's system prompt.

### Template Mode

If ROOT.md contains `{{agent_persona}}`, the agent's persona is substituted at that position:

```markdown
# System Rules

Always respond in English.
Never use tables unless asked.

---

{{agent_persona}}

---

## Additional Context

These rules apply to all interactions.
```

### Prepend Mode

If no `{{agent_persona}}` placeholder exists, the agent's persona is prepended before ROOT.md content:

```
[Agent Persona]
---
[ROOT.md Content]
```

## Configuration

```yaml
cerebro:
  root_prompt: true           # Enable ROOT.md (default: true)
  prepend_persona: true       # Prepend persona if no {{agent_persona}} (default: true)
  root_prompt_path: ./ROOT.md # Path to file (default: ROOT.md relative to config)
```

Set `root_prompt: false` to disable ROOT.md entirely.

## What to Put in ROOT.md

Good candidates for ROOT.md:

- Communication style rules (no tables, natural language, no emojis)
- Forbidden phrases and sycophancy rules
- Internal operations rules (never mention tools, MCP, permissions)
- Output format preferences
- Shared values or decision filters

Bad candidates (keep in agent files):

- Agent personality and voice
- Agent-specific tics and catchphrases
- Role-specific instructions
- Domain expertise
