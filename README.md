# rogue-core

Go framework for running AI agents. Handles the boring parts — message routing, IAM, tool orchestration, scheduling — so you just bring your agents, tools, and config.

## how it works

```
Source (telegram, cli, agent)
  → Telepath (message bus)
    → Helmet (IAM — who are you, what can you do)
      → Cerebro (pick a provider, run the agent)
        → Warp (route the response back)
```

Everything is config-driven. You write a `config.yaml`, point it at your agents and tools, and the pipeline handles the rest.

## structure

```
core/           — framework internals (pipeline, IAM, store, scheduler)
providers/      — AI provider adapters (claude code, bring your own)
sources/        — message source adapters (telegram, cli, agent-to-agent)
cmd/            — binaries
  rogue-pipeline/     — main message loop
  rogue-coordinator/  — process supervisor (hot reload, crash recovery)
  rogue-store/        — storage MCP server
  rogue-iam/          — IAM MCP server
  rogue-scheduler/    — scheduler MCP server
```

## usage

this is a library. you import it into your own instance repo:

```
go get github.com/kidkuddy/rogue-core
```

your instance repo has your agents, power sets, custom tools, and a `config.yaml`:

```
my-instance/
├── agents/        — agent persona files (.md)
├── powers/    — capability bundles (.md)
├── tools/         — your custom MCP servers
├── config.yaml    — wires everything together
└── go.mod         — requires rogue-core
```

## config

```yaml
store:
  data_dir: "env:ROGUE_DATA"

helmet:
  powers_dir: ./power_sets
  agents_dir: ./agents
  root_resolver:
    type: env_match
    env: OWNER_ID

cerebro:
  default_provider: claude-code
  max_turns: 100
  max_agent_depth: 3
  tools:
    rogue-store:
      command: ./bin/rogue-store
      env:
        ROGUE_DATA: "env:ROGUE_DATA"
    my-custom-tool:
      command: ./bin/my-tool

telepath:
  sources:
    - type: telegram
      id: tg:main
      agent: rogue
      token: "env:TELEGRAM_TOKEN"
      debounce_ms: 5000
    - type: cli
      id: cli:dev
      agent: rogue

scheduler:
  tick_interval: 30s
```

env values support `"env:VAR_NAME"` and `"${VAR_NAME}"` substitution.

## component codenames

| codename | what it does |
|---|---|
| **Telepath** | message bus — sources in, responses out |
| **Helmet** | IAM — users, sessions, powers, root resolution |
| **Cerebro** | agent orchestrator — picks provider, manages state |
| **Warp** | response handler — routes replies, tracks usage |
| **Store** | namespaced sqlite + file storage |
| **Schedule** | cron + one-shot task scheduling |
| **Coordinator** | process supervisor — hot reload on binary change |

## data layout

all data lives under `store.data_dir`:

```
<data_dir>/
├── db/<namespace>/data.db    — sqlite databases
└── files/<namespace>/        — file storage
```

namespaces are automatic. helmet uses `iam`, cerebro uses `conversations`, scheduler uses `scheduler`. your tools can create any namespace they want.

## build

```bash
make build   # builds all binaries
make test    # runs tests
make clean   # removes binaries
```

## running

```bash
# dev mode (cli source, no config needed)
./rogue-pipeline

# with config
./rogue-pipeline config.yaml

# with coordinator (auto-restart, hot reload)
./rogue-coordinator ./rogue-pipeline config.yaml
```

the coordinator watches the pipeline binary for changes and restarts automatically. send `SIGHUP` to force restart, `SIGUSR1` to forward config reload.

## writing a custom source

implement the `Source` interface:

```go
type Source interface {
    ID() string
    Start(ctx context.Context, inbound chan<- Message) error
    Send(ctx context.Context, resp Response) error
    Stop(ctx context.Context) error
}
```

register it in your instance's pipeline setup or add a new source type to the config factory.

## writing a custom provider

implement `AgentProvider`:

```go
type AgentProvider interface {
    ID() string
    Execute(ctx context.Context, req AgentRequest) (*AgentResult, error)
}
```

register it with `cerebro.RegisterProvider(myProvider)`.
