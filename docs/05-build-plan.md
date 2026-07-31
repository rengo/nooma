# 05 — Build plan (v1)

Milestone order. Each one ends in something **runnable and demonstrable**, with tests.
Cross-cutting rule: the cognitive core is pure services behind interfaces (repos, providers,
channels) — unit tests touch neither SQLite nor the network; integration tests do (against a
real temporary vault). See [`06-harness.md`](06-harness.md).

## M0 — Skeleton: binary + vault

Prior decisions: **[ADR-0001](adr/0001-sqlite-driver.md)** (SQLite driver, closed
2026-07-28).

- ~~ADR-0001 spike~~ — **done**. `ncruces/go-sqlite3` accepted, sqlite-vec dropped, vector
  proximity is a brute-force dot product in Go.
- Go repo layout (`cmd/nooma`, `internal/...`), config loader (yml + .env), vault resolution
  (arg → `$NOOMA_VAULT` → upward search from the working directory → `~/.nooma/`, see
  [`01-architecture.md`](01-architecture.md)), single-writer lockfile.
- Embedded migrations + `PRAGMA user_version`; creates the complete schema from
  [`03-data-model.md`](03-data-model.md).
- CLI: `init` (minimal wizard), `serve` (HTTP hello + UI placeholder), `status`, `version`,
  `doctor` (config + integrity_check).
- **Demo**: `nooma init && nooma serve`. **Met on Linux (2026-07-30, PRs #18–#38) and on Windows
  (2026-07-30, PR #40).** Both are *run*, not inferred: `integration` and `e2e` execute the L3 and L4
  suites on `ubuntu-latest`, `integration (windows)` and `e2e (windows)` execute them on
  `windows-latest`, and all four are required checks on every PR.

  **`darwin` and every ARM target have build coverage only**, and the distinction is the one
  [ADR-0013](adr/0013-cross-compile-targets.md)'s seven targets exist to keep visible: cross-compiling
  proves the code *builds* for a target, never that it *behaves* there. `darwin` shares the unix code
  path with Linux, which is a reason to expect it to work, not evidence that it does — `windows/amd64`
  cross-compiled green from the day the harness landed while the store could not open a vault there at
  all. Naming a runner for macOS and ARM is scheduled with the release work, not with M0.

  One behavior is **unverified on Windows** and stated rather than implied: that `nooma serve` exits
  with status zero and releases the vault lock on Ctrl+C. The test harness cannot deliver an interrupt
  to another process there — Windows has no POSIX signals, and a console Ctrl+C needs the child in its
  own process group plus `GenerateConsoleCtrlEvent`. Everything else about the lock is covered by tests
  that do run there. Tracked in `openspec/changes/m0-skeleton/tasks.md` §7.7.

## M1 — Capture and recall

Prior decisions: **[ADR-0002](adr/0002-default-llm-preset.md)** (LLM preset),
**[ADR-0003](adr/0003-embeddings.md)** (embeddings),
**[ADR-0005](adr/0005-v1-scope.md)** (scope),
**[ADR-0010](adr/0010-hybrid-recall-fusion.md)** (fusion),
**[ADR-0012](adr/0012-vector-proximity-search.md)** (vector proximity).

- `LLMProvider` / `EmbeddingProvider` interfaces + implementations (anthropic, openai, ollama).
  `tasks:` config routing task → provider.
- Capture pipeline: classify (the complete taxonomy from
  [`02-cognitive-core.md`](02-cognitive-core.md) §5, with per-field degradation to null), unit
  persistence, embedding + FTS sync.
- Hybrid recall (RRF) + dedup/relation judge with persist/surface thresholds.
- In-place correction edits + signal.
- HTTP API: capture, recall, read-only units.
- **`nooma init`'s two first-class paths** — Cloud (recommended) and Ollama — writing a real
  `providers:` and `tasks:` block instead of the commented placeholder M0 ships, and never writing
  a secret: the config holds `api_key_env`, the *name* of an environment variable.
- **`nooma doctor`'s structured-JSON quality gate**: send a fixed prompt set to the provider
  configured for each task and verify the returned JSON validates. A failure names the provider as
  unsuitable **for that task**, never in general — a model can be excellent at chat and bad at
  JSON, and the user has to see that difference. Its prompt corpus is the same one that feeds the
  test golden files ([`06-harness.md`](06-harness.md) §5): written once, used twice.
- **Demo**: capture via API/CLI, ask "what do you know about X?" and get a real recall.

The last two are [ADR-0002](adr/0002-default-llm-preset.md)'s own deliverables, and they were
missing from this list until 2026-07-31 — the ADR was `Accepted` on 2026-07-27 with no milestone
owning either. They land in M1 because M1 is where providers become real: a wizard that offers to
configure a provider before any provider exists would be offering a promise, and a `doctor` gate
that grades JSON quality needs something to grade.

## M2 — Sleep and weight

Prior decisions: **[ADR-0009](adr/0009-scheduler-downtime.md)** (downtime).

- `effective_weight` + priority + the two focuses + hysteresis (pure functions, HEAVILY tested).
- In-process scheduler (cron + boot catch-up per ADR-0009).
- Nightly consolidation: the 8 phases in order, each individually invocable
  (`nooma consolidate`). `decision_log` in every phase with an effect.
- **Demo**: a vault with simulated weeks of data — cold things get archived, related things get
  connected, beliefs get derived; the decision_log tells the story.

## M3 — The mouth: Telegram + prospection

Prior decisions: **[ADR-0006](adr/0006-v1-channel-telegram.md)** (channel),
**[ADR-0014](adr/0014-telegram-transport.md)** (transport).

- Telegram adapter (channel interface, `allowed_chat_ids`), over **long polling** — Nooma reaches
  Telegram and Telegram never reaches Nooma, so the channel opens no inbound port
  ([ADR-0014](adr/0014-telegram-transport.md)). A late delivery after the process was down is a
  normal case for the trigger work below, not an error.
- Triggers: armed at capture (dated events, recurring ones), due scan, digest with cadence +
  `current_state` gates, interruptive push ≥ 0.7 + quiet hours.
- Ephemeral timers end to end (arm, list, cancel, fire with rephrasing).
- Conversational check-ins: goal nudge, uncertain relation confirmation, state ("feeling
  loaded?"), task check-in — the orthogonal classify fields from §5.
- **Demo**: "remind me in 15 min about X" over Telegram, and a real morning digest.

## M4 — The mirror: complete UI

Prior decisions: **[ADR-0007](adr/0007-http-auth.md)** (auth),
**[ADR-0008](adr/0008-ui-stack.md)** (UI stack).

- All the views from [`01-architecture.md`](01-architecture.md) Layer 2: today/focus, capture,
  graph (with edge curation), beliefs (edit/delete → signals), activity, admin.
- Auth token when bind ≠ localhost.
- **Demo**: a fully usable product without touching the terminal.

## M5 — The learner

- `learning_signals` emitted from ALL surfaces (chat + UI).
- The `learn` pass: per-type relation thresholds + goal cadence with cooldown.
- Natural-language summary (UI + API) of what was learned, correctable.
- **Demo**: reject 6 `same_topic` connections → the threshold rises and the decision_log
  explains it.

## M6 — Release polish

Prior decisions: **[ADR-0004](adr/0004-license.md)** (license).

- `nooma export` / `import` (.noomabundle), complete `doctor` (providers, hardware, channels),
  embedding `reindex`.
- Goreleaser / multi-platform binary CI, public README, license.
- **Demo**: installable release used by a stranger without help.

## After v1

Multi-format perception + measurements + tracking UI (v2) → voice transcription →
multi-tenant mode → extra channels. None of this constrains the v1 design: the seams are
already there (perception door with shape-based routing, `measurements` in the schema,
channel = adapter).
