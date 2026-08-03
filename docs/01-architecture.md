# 01 — Architecture

## Stack

| Piece | Choice | Notes |
|---|---|---|
| Language | Go | Cross-compiles to Linux/macOS/Windows/ARM, no runtime, goroutines for the scheduler |
| Storage | SQLite (WAL) via `ncruces/go-sqlite3` | One file per vault. ACID transactions. No cgo ([ADR-0001](adr/0001-sqlite-driver.md)) |
| Vectors | `BLOB` + brute-force dot product in Go | No extension; proximity search is ~40 lines ([ADR-0012](adr/0012-vector-proximity-search.md)) |
| Full-text | FTS5 | Hybrid search (lexical + semantic) with no extra infrastructure |
| Jobs | Goroutines + in-process scheduler | Internal cron. No external queues, no Redis |
| UI | Served by the same binary | SSR + htmx as the base (see [ADR-0008](adr/0008-ui-stack.md)) |
| LLM | Per-provider interfaces | Anthropic, OpenAI, Ollama, whisper.cpp — configured per task |

Explicitly discarded: libSQL/Turso (a fork, a dependency with no gain), Rust (pays iteration
speed for performance this project does not need), external queues, and **sqlite-vec** — it
only compiles against a driver version frozen in 2024, and a brute-force scan measured faster
at target scale ([ADR-0012](adr/0012-vector-proximity-search.md)).

## The binary and the vault

```
# Installed mode                      # Portable mode
/usr/local/bin/nooma                  my-usb-drive/
~/.nooma/                             ├── nooma            # the executable
└── pablo.nooma/     # the vault      └── pablo.nooma/     # the vault
```

`~/.nooma/` holds vaults; it is not itself a vault. There is no installation-level config file:
configuration lives **inside** each vault, so a vault stays one self-contained object.

### Vault structure

```
pablo.nooma/
├── nooma.db              # SQLite: units, relations, triggers, beliefs, decision_log…
├── nooma.yml             # user config (channels, providers, schedules)
├── .env                  # secrets (API keys) — never committable
├── attachments/          # immutable originals (PDFs, photos, audio)
├── derived/              # recomputable (OCR, transcriptions) — skippable in backup
└── logs/
```

- **SQLite holds the structural data; the filesystem holds the blobs.** Attachments over
  ~100 KB live outside, the DB stores paths (SQLite's own official recommendation).
- **`attachments/` is immutable** (source of truth); **`derived/` is recomputable** (if the
  OCR model changes, it gets regenerated).
- From the outside, the vault is ONE object: copied, moved, and backed up as a unit.
- **`.env` is created `0600`.** On Linux and macOS that is owner-only, verified by the test
  suite. **On Windows it is not**, stated rather than implied: Windows has no POSIX mode bits,
  `os.Chmod` there only toggles the read-only attribute, and file protection is by ACL instead —
  so `.env` on Windows carries whatever protection the vault directory's own ACLs give it, not
  owner-only by construction. A Windows user who wants that guarantee needs to set it on the
  vault directory themselves. Present since M0; a real, platform-specific gap a Windows-run test
  first surfaced on `15` (`openspec/changes/m1c-surface/tasks.md` §C18).

### Vault resolution at startup

A directory **is a vault** when it contains a `nooma.yml` that is not a directory. That is the only
marker, because `nooma.db`'s location is configurable.

1. **Explicit argument**: `nooma serve pablo.nooma` or `nooma serve /home/pablo/pablo.nooma`. The
   path may be relative — it resolves against the working directory — or absolute. It is used as
   given: if it is not a vault, startup fails naming that path and does **not** fall through to the
   steps below. A typo must not silently open a different brain.
2. **`$NOOMA_VAULT`**, with the same semantics.
3. **An upward search from the working directory.** Starting at the working directory and walking up
   one directory at a time to the filesystem root, each directory is tested: first *is this a vault*,
   then *does this directory contain exactly one vault named `*.nooma*`*. The first hit wins, so the
   nearest vault wins. There is no `$HOME` ceiling — the ascent runs to the root, like git's search
   for `.git`.
4. **`~/.nooma/`** (installed mode): exactly one `*.nooma` vault inside it.

Resolution searches three places and no others: the path it is given, the working directory and its
ancestors, and `~/.nooma/`. Where the binary itself lives is irrelevant — the same executable on a
USB drive, in `/usr/local/bin`, or built into a source tree resolves identically.

Portable mode falls out of step 3: on removable media the invocation is
`cd /media/usb && ./nooma serve`, and the drive is the working directory.

For the searching steps, the candidate count is part of the contract:

| Candidates found | Behavior |
|---|---|
| exactly one | it is the vault |
| none | startup fails, naming the directory searched, and points at `nooma init` |
| two or more | startup fails, listing every candidate and showing the command form that disambiguates |

**The binary never chooses between candidates.** Opening the wrong brain is the worst failure this
step can produce, and it would be silent.

Two details that follow from the glob:

- A `*.nooma` directory that is not a vault (no `nooma.yml`) is **reported**, not skipped — the error
  names it and says what is missing.
- The literal name `.nooma` is never a candidate, even though `*.nooma` matches it. `~/.nooma` is the
  container, not a vault, and must never be reported as a broken one.

`nooma init` with no argument creates `~/.nooma/<username>.nooma` and prints the path. It never
writes vault contents into the working directory.

## Three layers, one process

### Layer 1 — The brain (always active)

The cognitive core (see [`02-cognitive-core.md`](02-cognitive-core.md)): capture, recall,
consolidation, prospection, self-model, learning, glass box. An in-process scheduler fires
nightly consolidation and the proactive check. Exposes an HTTP API on `localhost:7777`. It
works with or without channels: with no channel it still captures (via API/UI), thinks, and
queues nudges that remain visible in the UI.

### Layer 2 — The UI (active by default)

**The binary serves the user's complete frontend, not just an admin panel.** Same process,
`localhost:7777/ui`. This is the product's lean-back surface:

| View | What it solves |
|---|---|
| `/ui` | Today: task focus + load focus, pending digest, system status |
| `/ui/capture` | Written capture + file attachment (perception, once it exists) |
| `/ui/graph` | Graph of units and relations; edge-level curation (split/confirm connections) |
| `/ui/beliefs` | Self-model: beliefs by facet, edit/delete (each action emits a learning signal) |
| `/ui/activity` | Glass box: chronological decision_log, "why did you do that" |
| `/ui/tracking` | Measurement series + derived insights (once perception exists) |
| `/ui/admin` | System config, job status, thresholds, logs |

UI principles: a mirror, not an advisor (insight-led, the chart as evidence); server-side
rendered with targeted interactivity (htmx); dense, sober self-hosted-tool aesthetics; no
heavy build tooling. The graph is the only view that may require a dedicated JS component
(see [ADR-0008](adr/0008-ui-stack.md)). Turned off with `--no-ui`.

### Layer 3 — Channel adapters (enabled by config)

Each channel (Telegram, mail, …) is a Go package behind a common interface. Enabled in the
yml + valid credentials → a goroutine that listens and publishes. The brain does not know
which channels exist: it publishes events ("there is an alert") and consumes incoming
messages through an interface. **Adding a channel = one new adapter, zero core changes.**

- **Telegram: first-class channel.** BotFather bot + token + `allowed_chat_ids`
  (see [ADR-0006](adr/0006-v1-channel-telegram.md) — without that list, the channel does not
  start). Zero legal or cost friction.
- **WhatsApp: present but disabled by default**, with a clear warning (neither path — the
  unofficial protocol nor the Cloud API with Business Verification — works for "install the
  binary and go").
- Mail, Matrix, Discord, Slack: the community can contribute adapters that respect the
  interface.

## CLI

| Command | Description |
|---|---|
| `nooma init` | Wizard: creates the vault, base config, LLM preset, credentials, channels |
| `nooma serve [vault]` | Starts everything: API + UI + channels + scheduler |
| `nooma status` | Status without starting the server: vault path, schema version, lock holder, size, effective config. Brain state (last consolidation, channel activity) joins it in M2, when there is brain state to report |
| `nooma doctor` | Checks config, provider connectivity, LLM answer quality, permissions, hardware |
| `nooma capture <text> [vault]` | Sends text to a running `nooma serve` instance's `POST /capture` over HTTP and prints the result — an HTTP client, never a second direct-vault writer; fails if no server answers |
| `nooma consolidate [vault]` | Runs consolidation once and exits (a pure subcommand, also used by the scheduler) |
| `nooma reindex [vault]` | Re-embeds the whole vault after an embedding model change |
| `nooma export [vault]` | Packages the vault into a portable `.noomabundle` |
| `nooma import <bundle>` | Unpacks a bundle into a vault |
| `nooma version` | Version and build info |

`nooma doctor` is a key piece of the experience: it is what makes the binary feel cared for
(provider unreachable → how to install it; local model on weak hardware → latency warning;
configured channel → reachable or not).

## Configuration — `nooma.yml`

A YAML file at the root of the vault. Secrets are ALWAYS referenced by environment variable
(loaded from the `.env` next to it); the yml is committable, the `.env` never is.

```yaml
server:
  bind: 127.0.0.1                     # default; a non-loopback bind requires auth_token_env
  http_port: 7777
  ui: true
  auth_token_env: NOOMA_AUTH_TOKEN    # mandatory when bind is not loopback (ADR-0007)

database:
  path: ./nooma.db                    # relative to the vault root; may not escape it

# Reusable providers, shared across multiple tasks
providers:
  claude_cloud:
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    model: claude-sonnet-4-5
  claude_haiku:
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    model: claude-haiku-4-5
  gpt_cloud:
    type: openai
    api_key_env: OPENAI_API_KEY
    model: gpt-4o
  local_llama:
    type: ollama
    endpoint: http://localhost:11434
    model: llama3.1:70b
  whisper_local:
    type: whisper_cpp
    binary_path: /usr/local/bin/whisper
    model_path: ~/models/whisper-large-v3.bin

# Each brain task picks a provider — the user SEES which model does what
tasks:
  chat:                 { provider: claude_cloud }   # the mouth
  capture_processing:   { provider: claude_haiku }   # classify: normalize, classify, weigh
  relation_evaluation:  { provider: claude_haiku }   # the nightly connect judge
  belief_derivation:    { provider: claude_haiku }   # self-model derivation
  embedding:            { provider: local_llama }    # see ADR-0003
  audio_transcription:  { provider: whisper_local }
  image_description:    { provider: claude_cloud }

channels:
  telegram:
    enabled: true
    bot_token_env: TELEGRAM_BOT_TOKEN
    allowed_chat_ids: [123456789]

schedules:
  consolidate: "0 3 * * *"        # nightly
  proactive_check: "*/5 * * * *"  # scan for due triggers + urgent push
```

Philosophy: reusable providers (one key, N tasks), explicit tasks (nothing hidden in
defaults), free cloud+local mixing, separated secrets.

### The three kinds of LLM work

1. **Conversational chat** — latency and quality matter. Sonnet/GPT-4/large Llama.
2. **Structural processing** (classify, judge, derive) — strict JSON, latency does not matter.
   Cheap tiers… but CAREFUL: small models (3B) do structured JSON BADLY, and these are exactly
   the tasks that cannot fail. See [ADR-0002](adr/0002-default-llm-preset.md).
3. **Attachment extraction** (Whisper, vision) — specialized models, not text LLMs.

Internal abstraction: `LLMProvider`, `MultimodalProvider`, `TranscriptionProvider`,
`EmbeddingProvider`. Each `type` in the yml implements the matching interface. Consequences:
adapters contributable by the community, testing with mocks, changing provider = a config
change.

## Multi-tenant mode (future, not v1)

Flag `--multi-tenant --brains-dir=./brains/`: the binary resolves the SQLite connection per
authenticated user, with an LRU connection pool. Each user = their own file
(`./brains/maria.nooma`); a query cannot cross tenants by construction. The central scheduler
runs the same jobs for each vault, staggered. Jobs are pure subcommands
(`nooma consolidate --vault=…`) — identical logic in isolated and multi-tenant mode.

**The discipline**: if a hosted service needs something, it either lives outside (orchestrating
the public HTTP API) or the public API gets extended. Service-specific features inside the
binary's core: NO.
