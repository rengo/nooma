# ADR-0049 — Backup is a built-in snapshot; the upload stays outside the binary

- **Status**: Proposed
- **Date**: 2026-10-22
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: running Nooma unattended on a server, ahead of M6

## Context

The client runs Nooma around the clock on a DigitalOcean droplet, in Docker. The vault holds
personal data and exists in one place, so it needs a daily off-machine copy now, not at M6,
where `nooma export`/`import` and the `.noomabundle` format are planned.

`nooma serve` holds the vault for as long as it runs. Copying `nooma.db` with the filesystem
while it writes is not a backup: in WAL mode the latest commits may live only in
`nooma.db-wal`, and a copy of the two files taken at slightly different moments may not open
(docs/03-data-model.md §"Operational properties of the vault").

The client already backs up another app with rclone to Google Drive.

## Options evaluated

| Option | Tradeoff |
|---|---|
| Stop serve, copy the files, start serve | Consistent, but takes the brain offline daily, and a failed start leaves it down |
| `sqlite3 .backup` on the host | Needs a SQLite CLI matching the vault's features (FTS5) next to a static binary, and knows nothing about which other files a restore needs |
| **`nooma backup`: `VACUUM INTO` plus `nooma.yml`, verified, pruned by name** | One more command; it runs while serve runs and checks its own output |
| Upload inside the binary (a Drive client) | Puts Google credentials and an OAuth flow in the binary, and encryption with them |

## Decision

1. `nooma backup <vault> <dest-dir> [--keep N]` writes `nooma-YYYYMMDD-HHMMSS.db`, a `VACUUM
   INTO` snapshot, and `nooma-YYYYMMDD-HHMMSS.yml`, the vault's configuration, stamped in UTC.
   It takes no vault lock: it reads through a read-only connection, so it can neither migrate nor
   write the vault, whatever its version, and WAL lets it run beside serve. The destination may not
   be inside the vault.
2. The snapshot is opened read-only and must pass `PRAGMA integrity_check` before it gets its
   final name; on any failure the command exits non-zero and leaves no partial file.
3. It does not copy `.env` (secrets stay with the deployment), `derived/` (recomputable),
   `logs/` or `nooma.lock`. It refuses a vault whose `attachments/` holds anything, until it
   learns to carry them.
4. `--keep N` deletes all but the newest N backups, counting and deleting only files named
   exactly as above. These are copies the command made, not vault data: non-negotiable 6
   ("nothing is deleted in the vault") is not in play.
5. Restore is a procedure, not a command: stop serve, put the snapshot in place as the vault's
   database beside the backed-up `nooma.yml`, start. `sqlite.Open` migrates an older snapshot
   forward.
6. The upload stays outside the binary. On the server, rclone copies the backup directory
   through a `crypt` remote, so Google only stores ciphertext. Nooma holds no cloud credentials
   and no upload code.

## Consequences

### What it enables

A daily, verified, encrypted off-site copy taken without stopping the brain, with the cloud
provider swappable without touching the binary.

### What it costs

A second backup format before M6: `nooma export` will produce a bundle, and this snapshot is a
plain database. The encryption key lives in the rclone config on the server and must also be
kept somewhere else, or the backups cannot be read.

### Reversal criteria

Attachments start being written (backup must carry them or stop refusing), or M6's bundle
covers the same need with verification and makes this command redundant.
