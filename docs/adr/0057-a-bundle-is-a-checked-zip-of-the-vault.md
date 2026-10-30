# ADR-0057 — A bundle is a checked zip of the vault

- **Status**: Proposed
- **Date**: 2026-10-29
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M6 (`nooma export`, link C1; `nooma import`, link C2)

## Context

The client runs a production vault on a server and a development vault on a notebook, and wants
to move a brain between machines. `nooma backup` ([ADR-0049](0049-backup-is-a-built-in-snapshot.md))
already writes a verified snapshot and `nooma.yml`, so a move is possible today by hand: copy two
files, place them, fix their owner, start. Three things are missing for a stranger to do it
safely:

- **One file to move.** Two loose files, the second easy to forget, and attachments not carried
  at all (backup refuses a vault that has any).
- **Integrity that travels.** A backup is verified where it is written; nothing checks it after it
  crossed a network or a USB stick.
- **A restore that cannot go wrong.** A restore is a procedure, and the import that replaces it
  (C2) needs something it can check before it writes anything.

The client chose copy only, never merge, and no password on the bundle (2026-10-29).

## Options evaluated

| Option | Tradeoff |
|---|---|
| Keep backup's two files, document the move | No new code; no attachments, no check after transport, a manual restore |
| A JSON export of every table | Readable by other tools, but ~20 tables to round-trip losslessly across every future migration, forever; SQLite already opens with `sqlite3` |
| **A zip: the snapshot, `nooma.yml`, attachments, and a manifest of sha256s** | One file, checked on both ends, opened by every OS's own archive tool; the database stays the one format the binary already migrates |
| A tar.gz | Equivalent content; Windows does not open it natively |
| Encrypt the bundle | Rejected by the client; transport encryption stays outside the binary, as ADR-0049 keeps it |

## Decision

1. **A `.noomabundle` is a zip** holding `nooma.db`, `nooma.yml`, every regular file under
   `attachments/`, and `manifest.json`:
   - `bundle_format` (`1`), an integer that moves only when the layout changes, never with the
     schema or the binary;
   - `nooma_version`, the binary that wrote it, and `schema_version`, the database's
     `user_version`;
   - `created_at`, UTC;
   - `embedding_models`, each model with how many embedding rows the database holds under it;
   - `files`: each file's path, size and sha256.
2. **Never in a bundle**: `.env` (secrets stay with the deployment), `derived/` (recomputable),
   `logs/` and `nooma.lock`. `nooma.yml` goes in byte for byte. Its schema has no field for a
   literal secret, only the names of the variables that carry one, and the decoder refuses a
   key it does not know, a repeated key, and a second YAML document when the file loads, so
   none of those reaches a bundle. What the file can still carry is text nooma never reads: a
   comment travels with it. Export refuses a provider endpoint carrying a user or password, or
   a query string, in its URL, since a URL is the one value that can embed a key. A link or any non-regular file under `attachments/` is refused,
   since it could carry a file from outside the vault.
3. **`nooma export <vault> <file.noomabundle>`** takes no lock and runs beside serve. The
   database is backup's own snapshot (`VACUUM INTO` over a read-only connection) that passed
   `integrity_check`. Export never migrates the vault, and a vault behind the binary is bundled
   at its own schema. The bundle is written as `<file>.partial`, re-read and checked against its
   manifest, and only then renamed. On any failure the command exits non-zero and leaves no
   bundle, no `.partial` and no working folder. The name must end in `.noomabundle`, must not
   exist, and must not be inside the vault.
4. **A bundle is accepted only when it matches its manifest exactly** (`bundle.Verify`): a
   format this binary reads, `nooma.db` and `nooma.yml` present, every listed file present once
   with its size and sha256, and no entry the manifest does not list. A bundle may come from
   anywhere, so the check also refuses what a reader must not unpack:
   - a name that is not a clean relative path, or appears twice;
   - an entry that is not a regular file: a directory, a link, a device;
   - two names a case-insensitive filesystem would make one (`Photo.txt`, `photo.txt`), folded
     upper then lower so letters with two lower forms collide too (`Σ.txt`, `ς.txt`);
   - a name Windows cannot hold: a device name with any extension and any spaces before it
     (`CON`, `PRN`, `AUX`, `NUL`, `CONIN$`, `CONOUT$`, and `COM` or `LPT` followed by `0`–`9`
     or `¹²³`: `aux.txt`, `CON .txt`, `COM¹.txt`, `LPT0.log`), a trailing dot or space, one of
     `<>:"|?*`, a control character. Export refuses these too, so a bundle made anywhere
     imports everywhere;
   - a manifest over 1 MiB, read no further than that;
   - an entry whose zip header disagrees with the manifest's size. No entry is decompressed past
     its declared size: the zip reader stops at the header's size, and Verify limits the read
     to it as well.

   Export runs this check on what it wrote; import (C2) runs it before writing anything.
5. **The snapshot travels as it is.** Doc 03 requires a rebuild of the word index after a
   vacuum, because `units.rowid` may be renumbered. Export only reads, so the rebuild is
   import's, before the vault first opens (C2).
6. **`nooma import <file.noomabundle> <vault-dir>` makes a new vault, and only that.** Copy
   only, never merge (the client, 2026-10-29). Everything that can refuse refuses before
   anything is written:
   - a target that exists and is not empty (an empty directory, as `install -d` makes, is
     fine), so import never writes over a vault;
   - a bundle `Verify` rejects, a newer `bundle_format` among them;
   - a `schema_version` newer than this binary's, naming the nooma version that wrote it;
   - too little free space for the declared files plus a margin (10%, at least 16 MiB). A
     platform that cannot report free space is warned about, and the import goes on.

   The bundle then unpacks into a working folder beside the target, every file checked again
   against its sha256 as it is written. The database must pass `integrity_check` at the schema
   the manifest states. `sqlite.Open` migrates an older bundle forward, and the word index is
   rebuilt, as doc 03 requires after a vacuum. The vault's `attachments/`, `derived/` and
   `logs/` folders are created. Only then is the folder renamed into place; any failure removes
   it and leaves no vault.
7. **An imported copy has Telegram off.** `channels.telegram.enabled: true` becomes `false`,
   and the command says why: two copies polling one bot token steal each other's messages.
   The value is replaced where it stands in the text, so every comment and line of
   `nooma.yml` stays as it was, and the result is decoded strictly before the vault exists. If
   the value's position cannot be confirmed in the text, the file is re-serialized instead,
   and the command says its layout may have moved (a tagged `!!bool true` does, because the
   YAML library reports its column one early; measured). Turning Telegram back on is a deliberate
   act in the one copy that should answer.
   Beyond that one edit, `nooma.yml` travels as the source machine's configuration: its
   `server:` bind and token, its `providers:` and `tasks:`. Adapting them to the machine that
   receives the brain is the operator's job, before the copy is started (docs/08 §7).
8. **The bundle complements backup and replaces nothing.** Backup stays the daily rotation the
   server's rclone job uploads, and its file names stay what that job matches.

## Consequences

### What it enables

- A brain moves as one file whose every byte is checked again where it lands.
- Attachments travel, which backup still refuses.
- C2's import has a contract to refuse against before it writes a vault.

### What it costs

- A second copy format beside backup's, and one more command.
- The bundle is plaintext: it holds the whole brain, and protecting it in transit is the user's
  job, as with a backup.
- Unicode normalization is not folded: macOS may store `é` decomposed (NFD) where another
  system stores it composed (NFC), so two names that look identical can still be two entries.
  A bundle written on one system and imported on macOS could, in that rare case, write two
  files that macOS shows as one. Known, and not handled. Nor is full Unicode case folding:
  upper then lower keeps `ß` and `SS` apart, which a filesystem applying full folding might
  merge.
- The manifest guards against corruption, not tampering: whoever can rewrite the zip can rewrite
  its manifest.
- On a server the export file is written as the container's user, so moving it off the host
  takes `sudo` (docs/08).

### Reversal criteria

Evidence that bundles are wanted by other tools (a JSON export beside the snapshot), that
tampering in transit is a real risk (a signed manifest), or that backup and bundle should be
one format (backup writing bundles, with the rclone job's names changed in the same step).
