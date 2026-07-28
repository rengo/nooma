# Nooma

A personal digital brain: a self-contained Go binary running over a portable vault — a folder
with SQLite inside — that holds one person's entire memory.

It is not a filing cabinet that answers. It weighs what you capture, lets what you don't touch
decay, connects what's related, consolidates at night, reaches out when something warrants it,
and learns from how you respond.

```
nooma init      # create a vault
nooma serve     # brain + web UI + channels + scheduler, one process
```

Two objects, never to be confused:

- **The binary** (`nooma`) — the program. Generic, identical for everyone. Like `vlc`.
- **The vault** (`pablo.nooma/`) — the brain. "Taking my brain with me" means copying the folder.

## Principles

- **Private by default as topology, not policy.** Each brain is a separate file. There is no
  shared table to leak from.
- **Glass box.** The vault opens with `sqlite3`. Every automatic decision is recorded with its
  reasoning in a queryable log.
- **Nothing is deleted, it goes quiet.** Weight drops toward zero and the unit is archived, but
  it stays and can resurface.
- **Complexity on demand.** No external services by default. One process, one file.

## Status

**Foundation.** The design is complete; the code is being built. See
[`docs/`](docs/README.md) for the full specification and
[`docs/adr/`](docs/adr/README.md) for the decisions in force and their reasoning.

Start with [`docs/02-cognitive-core.md`](docs/02-cognitive-core.md) — it is the source of truth
for the brain's behavior, independent of the stack.

## License

[AGPL-3.0](LICENSE). You can run it, modify it, and take it anywhere. If you offer a modified
Nooma as a network service, you have to publish your changes.
See [ADR-0004](docs/adr/0004-license.md).
