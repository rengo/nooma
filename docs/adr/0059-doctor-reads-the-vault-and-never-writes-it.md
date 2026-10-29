# ADR-0059 — Doctor reads the vault and never writes it; consistency is checked per model

- **Status**: Proposed
- **Date**: 2026-10-29
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M6 (a complete `nooma doctor`, link B; providers, channels and hardware follow)

## Context

`nooma doctor` is meant to be the command you can run at any time and believe. Two things it
did made that untrue.

- **It migrated the vault.** Its vault checks opened the database through `sqlite.Open`, which
  migrates a vault behind the binary forward. A doctor run therefore changed the file it was
  diagnosing, took no vault lock while doing so, and could run beside `nooma serve`. It also
  made its own schema check vacuous: the version it read was always the one it had just
  migrated to, so "behind" could never be reported.
- **It counted vectors under any model.** `CountLiveWithoutEmbedding` joined units to
  `unit_embeddings` on the unit alone. Recall reads only the configured model's vectors (I21),
  so after a change of embedding model every old unit was unreachable by meaning, and doctor
  still printed `ok vault coverage`. Nothing at runtime said so either.

`docs/03-data-model.md` also promised a units↔word-index consistency check "in M6", which
nothing performed.

## Options evaluated

| Option | Tradeoff |
|---|---|
| Keep `sqlite.Open`, take the vault lock first | Doctor still writes the vault, and refuses to run while serve does: the time you most want a diagnosis |
| **Open read-only (`mode=ro`), the same `Vault` type** | Every store read works unchanged and SQLite refuses every write; the vault may be behind or ahead of the binary, which the schema check now reports |
| A separate read-only type with its own queries | A second surface that drifts from the store's reads |

For the word index:

| Option | Tradeoff |
|---|---|
| FTS5's `integrity-check` command | Thorough, but it is an `INSERT` command and fails on a read-only connection (measured: "attempt to write a readonly database") |
| `count(*)` on `units_fts` | Proves nothing: an external-content FTS5 table reads its rows from `units`, so the count always matches |
| **Join `units.rowid` with `units_fts_docsize` both ways** | Read-only; finds a unit the index lost and an entry naming no unit (measured: a removed entry shows 2 indexed against 3 units). It does not see an entry indexed with a unit's old text |

## Decision

1. **Doctor opens the vault read-only** (`sqlite.OpenReadOnly`). It never migrates and never
   writes the database file (I55). It takes no lock and runs beside `serve`. Read-only is about
   that file: when `nooma.db-wal` and `nooma.db-shm` are missing, SQLite creates them beside it,
   owned by whoever runs doctor, and they stay after it exits (measured). On a server, doctor
   therefore runs as the user serve runs as, inside its container, never as root on the host
   path ([08-deploy.md](../08-deploy.md)): sidecars owned by root can stop serve, uid 65532,
   from opening the vault.
2. **The schema check compares the file's version with the binary's** (`sqlite.SchemaTarget`).
   Equal is ok. Behind is ok and says "serve migrates it to N on start". Ahead fails and says to
   upgrade nooma. Zero fails, as before.
3. **The vault lock is reported, never failed on**: free, or held by process N.
4. **Coverage is per model.** `CountLiveWithoutEmbedding(ctx, model)` counts the live units with
   no vector under that model, none at all or only another's. Doctor reads the model the
   embedding task's provider names. Its FAIL gives the count, the other models live units hold,
   with how many each, and the fix. With no embedding model configured it says so on an ok line.
5. **The word index is checked by the docsize join** above. Its FAIL names what is missing and
   what is orphaned.
6. **A fix that does not exist yet is named as such.** Both FAILs say that `nooma reindex` will
   re-embed or rebuild, "(not in this version yet)", until the command ships. A check never
   sends the user to a command the binary does not have.
7. **Serve warns at boot, once, and starts anyway.** When live units lack the configured
   model's vector, `serve` writes one `WARNING:` line naming the count and the model and
   pointing at `nooma doctor`. It never refuses to start over it: the client chose start-and-warn
   (2026-10-29), because a brain that will not start also stops its reminders.

`nooma status` had the same defect: its comment said it read the schema version read-only,
and it opened the vault through `sqlite.Open`. It now reads it through `OpenReadOnly` too.

Doctor's permissions check still creates and removes one probe file in the vault directory:
it is the test of whether the directory is writable, and it touches no database.

## Consequences

### What it enables

- A doctor run is safe at any time, including beside a running serve, and a vault behind the
  binary is reported rather than silently upgraded.
- A change of embedding model is visible: in doctor, with the count and the models present, and
  at every boot until it is fixed.
- The word index that a search depends on is checked, not assumed.

### What it costs

- `ports.EmbeddingRepo.CountLiveWithoutEmbedding` takes a model, so every implementation
  changed; `sqlite.EmbeddingRepo` gains `LiveModels`, a store method no brain code reads.
- The word-index check does not catch an entry indexed with a unit's earlier text. FTS5's own
  check would, and it needs a writable connection doctor refuses to open.
- Until `nooma reindex` ships, a FAIL here has no in-binary fix, and says so.

### Reversal criteria

FTS5 gaining a read-only integrity check, or a stale-text gap seen in a real vault, would justify
running the thorough check on a copy. Evidence that users run doctor to repair rather than to
diagnose would justify a separate repair command, never a writing doctor.
