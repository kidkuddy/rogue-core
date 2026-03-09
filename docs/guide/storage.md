# Storage

Rogue Core uses namespaced SQLite databases and file storage. Each namespace is fully isolated with its own database and file directory.

## Data Layout

```
$ROGUE_DATA/
  iam/
    db/store.sqlite          — users, sessions, powers, usage
  scheduler/
    db/store.sqlite          — scheduled tasks
  conversations/
    db/store.sqlite          — session state
  contacts/
    db/store.sqlite          — user-defined namespace
    files/
      avatar.png             — user-defined file
```

## Namespaces

Namespaces are dynamic — any string works. The database and file directory are created on first access.

```go
store.Namespace("contacts").DB()           // → $ROGUE_DATA/contacts/db/store.sqlite
store.Namespace("contacts").FilePath("x")  // → $ROGUE_DATA/contacts/files/x
```

Framework namespaces: `iam`, `scheduler`, `conversations`. Everything else is user-defined.

## MCP Tools (rogue-store)

The storage MCP server exposes:

| Tool | Description |
|------|-------------|
| `sql(namespace, query)` | Execute SQL against a namespace's database |
| `file_read(namespace, name)` | Read a file |
| `file_write(namespace, name, content)` | Write a file |
| `file_list(namespace)` | List files in a namespace |
| `file_delete(namespace, name, confirm)` | Delete a file (confirm must be "yes delete") |
| `backup()` | WAL checkpoint all databases |

## Schema Conventions

- Use `ALTER TABLE ... ADD COLUMN` for migrations (silently fails if column exists)
- Never drop columns or tables
- All timestamps use `datetime('now')` defaults
- Use `TEXT` for IDs, `DATETIME` for timestamps, `BOOLEAN` for flags
