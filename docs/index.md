---
layout: home
hero:
  name: Rogue Core
  text: AI Agent Pipeline Framework
  tagline: Config-driven, interface-based framework for building AI agent systems in Go.
  actions:
    - theme: brand
      text: Get Started
      link: /guide/
    - theme: alt
      text: Architecture
      link: /architecture/
features:
  - title: Pipeline Architecture
    details: Message flows through Telepath (bus) → Helmet (IAM) → Cerebro (orchestrator) → Warp (response). Each component is interface-driven and replaceable.
  - title: Powers System
    details: Capability bundles that grant tools, directories, and instructions per user per agent per channel. Core powers ship embedded, instance powers override.
  - title: Multi-Agent
    details: Run multiple AI agents with distinct personas, each with their own Telegram bot, tool access, and conversation memory.
  - title: Scheduling
    details: Cron and one-shot task scheduling with acknowledgment tracking. Tasks fire as messages through the same pipeline.
---
