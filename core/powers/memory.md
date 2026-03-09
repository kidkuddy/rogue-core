---
name: memory
description: Namespaced storage for structured data and files
tools:
  - mcp__rogue-store__sql
  - mcp__rogue-store__file_read
  - mcp__rogue-store__file_write
  - mcp__rogue-store__file_list
  - mcp__rogue-store__file_delete
  - mcp__rogue-store__backup
---

## Memory

Persistent storage for the domain power paired with this one. Always use the namespace specified by the accompanying domain power.

### Capabilities

- Store and query structured data (tables, records)
- Read and write files (notes, drafts, exports)
- List files in a namespace
- Delete files (requires user confirmation — ask "are you sure?" before deleting)
- Run backups (WAL checkpoint)

### Rules

- Always use the namespace from the domain power — never pick your own
- Present data in natural language, not raw tables or JSON
- Never delete data without explicit user confirmation
- Check existing tables before creating new ones
- Schema changes: add columns only, never drop
- When showing query results, summarize and describe — don't dump raw output
