# Proposal — M0: the binary becomes runnable

Deliver [`docs/06-harness.md`](../../../docs/06-harness.md) §9 step 5 — M0 as laid out in
[`docs/05-build-plan.md`](../../../docs/05-build-plan.md): the configuration loader (`nooma.yml`
plus `.env`), vault resolution, the single-writer lockfile, and the CLI commands `init`,
`serve`, `status` and `doctor`.

This is the first change in the project that ships user-facing behavior. Everything merged so
far is scaffolding: today the binary answers `version` and prints a usage line.

---

## 1. Why now

Step 4 closed with thirteen PRs. The harness is complete and armed: seven gates block every PR,
the vault schema is executable, the migration runner is crash-tested, and four goldens keep the
docs and the code from drifting. What does not exist is a way to *use* any of it.

| Fact | Consequence |
|---|---|
| `cmd/nooma/main.go` is 53 lines with a single `case "version"` | Nothing in the tree can open a vault outside a test. `internal/store/sqlite.Open` has no caller in `cmd/`. |
| `internal/config/` contains only `doc.go` | `docs/01-architecture.md` documents a complete `nooma.yml` schema that no code has ever parsed. |
| `docs/03-data-model.md` §"Operational properties" promises a single-writer lockfile | No lockfile exists. Two `nooma serve` processes over one vault is currently undefined behavior — which is fine only because `serve` does not exist either. |
| `go.mod` has one dependency | Four dependency decisions (YAML, dotenv, CLI dispatch, OS locking) are still open, and M0 is where they get made. |

The asymmetry that makes this the honest next step: the harness was built to be used by domain
code, and until domain code exists we cannot know whether the harness helps or merely exists.
M0 is the smallest change that produces a running binary, so it is the cheapest place to find
out.

---

## 2. Success criteria

The change is done when:

- [ ] `nooma init` creates a complete, valid vault from nothing, non-interactively and
      interactively.
- [ ] `nooma serve` starts an HTTP listener on `127.0.0.1:7777` serving an API hello and a `/ui`
      placeholder, holds the write lock for its lifetime, and releases it on exit.
- [ ] A second `nooma serve` over the same vault fails with a clear message naming the holder,
      and does not touch the database.
- [ ] `nooma status` and `nooma doctor` both work on a locked vault without taking the write
      lock.
- [ ] `nooma doctor` reports config validity, vault permissions, `PRAGMA integrity_check`, the
      schema version, and the effective bind with whether it is exposed.
- [ ] Every key in `docs/01-architecture.md`'s `nooma.yml` block decodes into the config struct,
      and an unknown key is a **load error**, proven by a test.
- [ ] A non-loopback `server.bind` without `server.auth_token_env` refuses to start
      ([ADR-0007](../../../docs/adr/0007-http-auth.md)).
- [ ] `docs/01-architecture.md` no longer contradicts itself on where `nooma.yml` lives or how a
      vault is discovered.
- [ ] `make check-all` green; L4 covers all five commands; the cross-compile matrix builds
      linux/darwin/windows on amd64 and arm64 plus `linux/arm` — **7 targets**, up from today's 4
      (`darwin/amd64`, `windows/arm64` and `linux/arm` are new), authorized by a new ADR
      superseding ADR-0001's acceptance criterion 5; `make cross-compile` covers the same seven
      targets and is part of `check-all`.
- [ ] **Demo**: `nooma init && nooma serve`, walked end to end.

---

## 3. Scope

### 3.1 The boundary rule

`docs/05-build-plan.md`'s M0 bullet list includes "embedded migrations + `PRAGMA user_version`",
which step 4 already delivered. The rule this change adopts, in the same spirit as step 4's
boundary rule:

> **M0 wires what exists and adds only what a running binary needs. It creates no domain
> concept.**

Concretely: M0 calls `sqlite.Open`, it does not extend the schema. M0 does not create
`unit.Status`, `ports.UnitRepo`, `recall.VectorQuery`, `recall.VectorIndex` or
`unit.AllStatuses` — the five symbols `test/conformance/pending_symbols.txt` anchors to. Those
belong to M1, and the pending-red gate stays red as it is.

This has one non-obvious consequence, stated here so it is not discovered during
implementation: `docs/01-architecture.md` describes `nooma status` as reporting "last
consolidation, channels, size". Last consolidation is a domain row, and
`testdata/schema/store_api.golden` exists precisely to keep the store surface from growing a way
to read one. So **M0's `status` reports only what M0 owns**: the resolved vault path, the schema
version, the lock holder, the database size on disk, and a summary of the effective config.
Brain state joins `status` in M2, when there is brain state to report.

### 3.2 In scope

1. **Doc corrections** to `docs/01-architecture.md`: the vault layout and the vault-resolution
   algorithm, both already decided (see §4.1).
2. **CI**: move the L4 e2e job and the cross-compile matrix from `push: main` onto
   `pull_request`.
3. **`internal/config`**: the full `nooma.yml` schema as Go types, strict decoding, `.env`
   loading, validation, and vault resolution.
4. **A config↔doc gate**: the YAML block in `docs/01-architecture.md` must decode into the
   config struct.
5. **`internal/store`**: the `nooma.lock` single-writer lock, and an integrity-check entry point
   for `doctor`.
6. **`internal/httpapi`**: a minimal server — API hello, `/ui` placeholder, and ADR-0007's
   refusal.
7. **`cmd/nooma`**: subcommand dispatch and the four new commands.
8. **Tests**: L1 for config and resolution, L3 for the lock under real contention, L4 for all
   five commands.

### 3.3 Explicit non-goals

- No provider, LLM, or embedding code. `providers:` and `tasks:` are *decoded and shape-checked*,
  never interpreted. (M1)
- No channel starts. `channels.telegram` is decoded and validated; the adapter is M3.
- No scheduler, no consolidation, no capture, no recall. (M1–M2)
- No real UI. `/ui` returns a placeholder page. (M4)
- No auth handshake. ADR-0007's session cookie is M4; M0 implements only the refusal to start.
- No `consolidate`, `reindex`, `export`, `import`. Later milestones — and `export` is blocked on
  an ADR (see §8).
- No hardware check in `doctor`: "minimum hardware" is an open dated decision due before M6.

---

## 4. Approach

### 4.1 The docs get corrected first, in their own PR

`docs/01-architecture.md` contradicts itself in three places, and all three block the config
loader. A fourth omission and a fifth outright bad rule were found while writing this change, and
belong in the same PR. All five are settled, and the reasoning is recorded outside this document
(engram: `architecture/vault-layout`, `architecture/vault-discovery`):

1. **`nooma.yml` lives inside the vault.** The doc's installed-mode tree placed it at
   `~/.nooma/`, beside the vault, while two prose sections place it at the vault root. The
   tie-breaker is the doc's own stronger promise — *"from the outside, the vault is ONE object:
   copied, moved, and backed up as a unit"*. Config outside the vault breaks that. The tree gets
   corrected.
2. **The default vault is a `*.nooma` directory inside `~/.nooma/`**, not `~/.nooma/` itself.
   Resolution step 4 named the parent directory as if it were the vault.
3. **Discovery is exactly-one-or-error.** The globbing steps look for `*.nooma`; the candidate
   count is part of the contract. One → use it. Zero → an error naming the searched directory and
   pointing at `nooma init`. Two or more → an error listing every candidate and requiring an
   explicit argument. The binary never chooses.
4. **The `server:` block gains `bind` and `auth_token_env`.** It documents only `http_port` and
   `ui`, but [ADR-0007](../../../docs/adr/0007-http-auth.md) requires
   `server.bind` (defaulting to `127.0.0.1`) and `server.auth_token_env`. Neither key is
   documented anywhere in the schema. An `Accepted` ADR is never edited, so the config block is
   what gets corrected — and the config↔doc gate of §4.4 would have failed on its first run
   without it, which is that gate earning its place before it is even written.
5. **Step 3 becomes an upward search from the working directory, not the executable's
   directory.** The doc said "vault next to the executable (portable mode)". Nothing else in the
   tree resolves *data* relative to `argv[0]`, and no conventional CLI does either — git searches
   upward from the cwd, testing each ancestor directory in turn until it finds `.git` or reaches
   the filesystem root; `kubectl` and `docker` read `$HOME` or take a flag. Step 3 now does exactly
   what git does: starting at the cwd, each directory visited is tested — is it itself a vault, or
   does it contain exactly one usable `*.nooma` directory — before moving to its parent, all the
   way to the filesystem root. There is no `$HOME` ceiling: any ceiling is one more rule the user
   has to memorise, and git's own model has none either. Executable-relative discovery, besides not
   doing any of this, is a silent-wrong-vault generator: a stray `test.nooma` in
   `/usr/local/bin/`, or a contributor's throwaway vault sitting beside the `go build` output in
   the repository root, becomes the default. And it does not mean what the user thinks —
   `os.Executable` returns the *resolved* path, so a symlinked install searches the real binary's
   directory, which the user never typed. Portable mode loses nothing: on removable media the
   invocation is `cd /media/usb && ./nooma serve`, so the cwd already contains the vault, found at
   the very first level the ascent tests. Spec R6.1, R6.2 and R6.6 carry the full argument.

Points 3 and 5 are worth their weight, and they are the same point twice. The obvious
implementation of discovery — `filepath.Glob` then `[0]` — picks the alphabetically first match
and reports nothing; the obvious reading of the doc's step 3 hands that glob a directory the user
cannot predict. Writing into the wrong brain is the worst failure this component can have, and in
both cases it would be silent. Nine defects in `complete-harness` came from exactly this family;
these two are designed against it up front.

A docs-only PR keeps these decisions reviewable on their own, instead of buried inside a
loader's control flow.

### 4.2 CI moves before the OS-dependent code lands

`.github/workflows/main.yml` runs L4 and the cross-compile matrix on `push: branches: [main]`
only; `ci.yml`'s comment block records the reasoning. That was correct when the tree had no
platform-specific code and one e2e test. M0 breaks both assumptions: the lockfile is the first
genuinely OS-dependent code in the project, and M0's demo is explicitly
"Linux/macOS/Windows/ARM". Under the current triggers, a Windows locking bug and every new L4
test would first execute *after* merge, on a protected branch, with the gate reporting green
during review.

So the second PR moves both jobs onto `pull_request`. Both are cheap: the matrix is build-only,
and e2e compiles one binary.

### 4.3 The config loader rejects what it does not understand

Two properties matter more than the rest:

- **Unknown keys are errors.** `docs/01-architecture.md` sells the config on "explicit tasks,
  nothing hidden in defaults". A loader that ignores a key it does not know breaks that promise
  in the most expensive way available: a user fixes a typo'd key, restarts, and nothing changes.
  Strict decoding is a requirement, not a preference.
- **Secrets are referenced, never held.** The schema carries `api_key_env`, `bot_token_env`,
  `auth_token_env` — *names* of environment variables. The loader resolves names to values only
  where a value is needed, and a config dump must be printable without leaking anything. This is
  what makes `nooma doctor` and `nooma status` safe to run and paste.

The `.env` file next to the config is loaded into the process environment before resolution, so
that an explicit environment variable set by the operator wins over the file.

Validation enforces the structural safe defaults of `CLAUDE.md` non-negotiable 7 **at load
time**, before the components they protect exist: `channels.telegram.enabled: true` with an
empty `allowed_chat_ids` is a config error, even though M0 starts no channel. The alternative —
adding the check when the adapter arrives in M3 — leaves the promise unenforced for two
milestones and makes it somebody's future discipline instead of a gate.

### 4.4 The config↔doc gate

The project already has four doc↔code goldens. The config block in doc 01 deserves the fifth,
and it is nearly free: `test/support/schema/markdown.go` already extracts fenced blocks from a
markdown document strictly (it was hardened for exactly this class of bug). The gate parses the
`yaml` block under §"Configuration — `nooma.yml`" and decodes it into the config struct under
the same strict rules the loader uses.

What it buys: the documented example cannot drift from the parser. A key added to the struct
without documenting it, or documented without being added, fails a test that names it. Doc 01's
config block stops being an illustration and becomes a fixture.

### 4.5 The lockfile

`docs/03-data-model.md` fixes the contract: `nooma.lock` in the vault, containing the PID; a
second `nooma serve` "fails clearly, it does not corrupt". Three implementation strategies exist
(exclusive create plus staleness detection, OS advisory locking behind build tags, or a hybrid);
the choice belongs to design, not here. What this proposal fixes are the properties:

- A crashed process must not leave a vault permanently unusable. This is the requirement that
  eliminates a naive PID file: after `kill -9`, the file survives.
- The lock is for **writers**. `status` and `doctor` are read-only and must work on a vault that
  `serve` currently holds, reporting the holder rather than refusing.
- The failure message names the holding PID, because the user's next action is to find that
  process.

Verification is L3 with a real second process — the precedent exists in the migration
crash-mid-transaction test.

Both the lock and `doctor`'s integrity check widen `internal/store/**`'s exported surface, so
`testdata/schema/store_api.golden` gets regenerated in the same PRs. That is the golden working
as designed: a widening becomes a reviewable diff.

### 4.6 The commands

`main.go`'s `run(args, out)` shape stays — it is already testable without touching process
streams. It grows a dispatch table and per-command flag parsing.

| Command | M0 behavior |
|---|---|
| `init` | Creates the vault directory, runs migrations, writes `nooma.yml` and a `.env` skeleton, creates `attachments/`, `derived/`, `logs/`. Interactive on a TTY; `--non-interactive` with flags otherwise, which is also how L4 drives it. |
| `serve` | Resolves the vault, loads config, takes the write lock, applies ADR-0007's refusal, listens on the configured bind, serves an API hello and a `/ui` placeholder, releases the lock on shutdown. |
| `status` | Read-only: resolved vault path, schema version, lock holder if any, database size, effective config summary. No domain rows (§3.1). |
| `doctor` | Read-only: config validity, vault directory permissions, `PRAGMA integrity_check`, schema version, effective bind and whether exposed. |
| `version` | Unchanged. |

---

## 5. The PR chain

Chain strategy `stacked-to-main`: each PR targets `main` and merges in order. The soft ceiling
is 400 changed lines.

| # | Slice | Content | Est. lines |
|---|---|---|---|
| 1 | `docs/m0-vault-layout` | The five doc-01 corrections plus doc 05's M0 bullet | ~140 |
| 2 | `ci/e2e-and-cross-compile-on-pr` | Move both jobs onto `pull_request`; new ADR-0013 (superseding ADR-0001's acceptance criterion 5) expanding the cross-compile matrix from today's 4 targets to **7** (`darwin/amd64`, `windows/arm64` and `linux/arm` added); `make cross-compile` **and** `make test-e2e` added to `check-all` (R2.3, R2.4); `main.yml`'s and `ci.yml`'s header comments plus `CLAUDE.md`'s Workflow section and the `Makefile` header corrected | ~180 |
| 3 | `feat/config-schema` | The full `nooma.yml` as Go types, strict decode, `.env` (including duplicate-key and bare-`#` rejection), validation, L1 | ~400 |
| 4 | `test/config-doc-gate` | A new section-scoped, exactly-one-or-error `yaml` extractor (modeled on `goldenset.ExtractJSONFence`, not a reuse of `schema/markdown.go`'s SQL collector) plus reflection-based key-schema comparison, including the map-typed `providers`/`tasks` union rule; the doc-01 config block as a fixture | ~260 |
| 5 | `feat/vault-resolution` | Upward-search resolution (cwd to filesystem root), exactly-one-usable-candidate-or-error at every level, the `.nooma`-name exclusion, a hard failure on a `readDir` error mid-ascent, `nooma.yml`-is-not-a-directory in the predicate, L1; plus an L2 tree-scan test banning `os.Executable` under `internal/config` (D16 — **not** a `forbidigo` rule; `.golangci.yml` is not touched) | ~360 |
| 6 | `fix/store-golden-var-const` | Extends `renderExportedDecl` to render exported `var`/`const` declarations (D14) and regenerates `testdata/schema/store_api.golden`; the diff surfaces exactly one thing — the pre-existing `ErrRelativeDBPath` (`internal/store/sqlite/dsn.go:15`), reviewed as pre-existing, not new | ~70 |
| 7 | `feat/vault-lock` | `nooma.lock` (acquire-before-write ordering, D4, single `WriteAt` for the PID region), L3 under real contention on `ubuntu-latest` **and** `windows-latest` — both jobs gain their Windows leg here (D6), with an explicit `make` install step on each (D17) — `ErrVaultInUse` sentinel, store-API golden regenerated (renderer already widened by slice 6, so this diff shows only `ErrVaultInUse`) | ~360 |
| 8 | `feat/cli-init` | Dispatch table, `init` interactive and not (shared input struct, D15), corrected temp-dir-then-rename mechanism with the `Lstat` guard for file/symlink targets and a collision-resistant temp-dir suffix (D12), L4 | ~410 |
| 9 | `feat/cli-status-doctor` | Both read-only commands, `(*Vault).IntegrityCheck`, L4 | ~400 |
| 10 | `feat/cli-serve` | httpapi hello, `/ui` placeholder, ADR-0007 refusal, L4 | ~400 |

Dependencies: `1 → 3`, `3 → 4`, `3 → 5`, `5 → 8`, `2 → 7`, `6 → 7`, `(5,7) → 9`, `(5,7) → 10`.
Slice 2 also precedes every slice that adds a new L4 test — `2 → 8`, `2 → 9`, `2 → 10` — because §4.2
argues slice 2 exists precisely so every new L4 test blocks the merge before it lands; stacking
already merges slice 2 first in practice, which is why these three edges were previously left off the
graph, but they are real dependencies and are stated here rather than left implicit. PRs 1 and 2
are independent of each other and of everything else, so they go first.

**On these estimates.** Step 4's retrospective measured implementation estimates 2–4x low, and
review remediation roughly doubling that again. The numbers above are per-PR ceilings chosen to
respect the 400-line rule, not predictions — expect PRs 3, 8, 9 and 10 to split. The split
decision is made *before* `sdd-apply` runs on each one, per the policy adopted last change,
never discovered afterwards.

---

## 6. Strict TDD ordering

Strict TDD is active for this project. M0 introduces no new brain invariant — I01–I21 come from
`docs/02-cognitive-core.md`, which M0 does not touch — so the discipline applies at the unit
level: the test is written, watched fail for the right reason, and only then satisfied.

Four properties are worth writing as tests before any implementation exists, because each one is
a property somebody will weaken later:

1. An unknown key in `nooma.yml` fails the load, naming the key.
2. Two `*.nooma` candidates produce an error listing both; zero produces an error naming the
   searched directory.
3. A second writer on a held vault fails, and the message contains the holder's PID.
4. A non-loopback bind without `server.auth_token_env` prevents startup.

---

## 7. Verification

- `make check-all` green on every PR in the chain.
- `make test-e2e` covers all five commands.
- The cross-compile matrix builds seven targets: `linux`, `darwin`, `windows` on `amd64` and
  `arm64` (the cartesian six), plus `linux/arm`.
- The lock verified with a real second process, not a mock.
- The demo run by hand: `nooma init && nooma serve`, then `nooma status` and `nooma doctor`
  against the running instance, then a second `serve` to watch it refuse.

---

## 8. Risks and open questions

| # | Risk | Mitigation |
|---|---|---|
| R1 | Estimates 2–4x low, doubled again by review remediation | Per-slice split decided before apply; the chain above is already ten slices, not five |
| R2 | Cross-compilation proves the lockfile *builds* on Windows, not that it *works* | **Decided** (design.md D6): the e2e (L4) job runs a `windows-latest` leg, and the L3 `integration` job — where the lock's actual contention (R8.2) and crash-recovery (R8.3) tests live — gains one too, so `LockFileEx` is genuinely exercised, not only cross-compiled |
| R3 | The config schema decodes blocks (`providers`, `tasks`) whose semantics arrive in M1, so a shape chosen now may not survive contact | Decode and shape-check only, never interpret; the config↔doc gate makes a later change visible in one diff |
| R4 | `init`'s interactive path is the one L4 cannot drive, so it is the one that rots | The non-interactive path is the primary contract; the wizard is a thin prompt layer over it, and that layering is a design requirement |
| R5 | Widening the store surface twice in one change | Both widenings are single-purpose and reviewed via the golden diff |

**Open questions for design:**

1. **YAML parser.** `goccy/go-yaml` (actively maintained, strict mode available) versus
   `gopkg.in/yaml.v3` (archived upstream, `KnownFields(true)` available). Strict unknown-key
   rejection is non-negotiable; the choice is which library provides it with the better
   maintenance story.
2. **`.env` loading**: hand-rolled (roughly 40 testable lines, zero dependencies) versus
   `joho/godotenv`.
3. **CLI dispatch**: stdlib `flag` with the existing switch shape versus a framework. The
   project's stated identity is a self-contained binary with minimal dependencies.
4. **Lock mechanism**: exclusive create with staleness detection, OS advisory locking behind
   build tags, or a hybrid.

Windows runtime coverage — originally open question 1 — is decided; see R2 above.

**Out of scope but now unblocked and worth recording:** `nooma export` needs an ADR before it
lands. `units.id` is `TEXT PRIMARY KEY`, so `units.rowid` is implicit and `VACUUM` may renumber
it, while `units_fts` uses `content_rowid='rowid'` and ADR-0001 criterion 4 commits export to
`VACUUM INTO`. Three probes found rowids preserved in practice, so the exposure is latent, not
active. `export` is not an M0 command, so M0 does not resolve it.

### 8.1 Known debt carried into implementation

Three adversarial review rounds produced 32 remediated findings. The round-3 findings below were
deliberately **not** remediated in the plan: each is either a one-line correction or something the
first test in its area surfaces immediately, and a fourth review round over prose was judged to
yield less than the first failing test. They are recorded here so that they arrive as debt, not as
discoveries.

| # | Item | Lands in |
|---|---|---|
| 1 | `docs/06-harness.md` §6 says e2e and cross-compile do not run on every PR — false after R2.1. The sentence beside it, "the full matrix depends on ADR-0001 and cannot be designed until the spike closes", died when ADR-0001 closed two build-order steps ago | **slice 2** (task 2.7) — the PR that makes the claim false, per non-negotiable #1; originally filed under slice 1 because it is a docs edit, which is the wrong grouping |
| 2 | `docs/05-build-plan.md`'s M0 bullet still reads "arg → env → portable → home", the executable-relative model R6.6 removes | slice 1 (task 1.5) |
| 3 | D12 calls the final rename "kernel-atomic" without qualification, but Go's own `os.Rename` doc states "on non-Unix platforms Rename is not an atomic operation" (`$(go env GOROOT)/src/os/file.go:435`), and `init`'s L4 tests run on `windows-latest`. The claim needs scoping to POSIX plus a Windows probe | slice 8 (`init`), with the probe recorded in design §1 |
| 4 | The upward search does not define what a `readDir` **error** at an intermediate level means — a directory with execute-but-not-list permission could be treated as "zero candidates, keep ascending", which is the silent-skip family. It must be a hard failure naming the directory | PR **5** (`feat/vault-resolution`), as an L1 case |
| 5 | D8's vault predicate checks that `nooma.yml` exists, not that it is a regular file. A directory named `nooma.yml` would pass and defer a confusing error to config load. `DirEntry.IsDir()` is already available from the same `readDir` call | PR **5**, as an L1 case |
| 6 | D12 enumerates two race windows for the empty-target branch and misses a third: two racing `init`s both calling `os.Remove(target)`, where the loser sees `ENOENT` — a different error shape from the `EEXIST`/`ENOTEMPTY` pair the design classifies | slice 8 |
| 7 | design §4 says provider `type` is validated "against the four documented types"; doc 01 has four provider *entries* but three distinct types (`anthropic` twice, `ollama`, `whisper_cpp`) | PR **3** (config schema) |
| 8 | Two passages quote `docs/06-harness.md` §3 with wording that document does not contain ("a real second process"; "no test touches the network" — the latter is `CLAUDE.md`'s non-negotiable #5) | any PR touching design §5 or §8 |

Items 4 and 5 are the two that would otherwise become real defects rather than documentation
inaccuracies, so they carry L1 test cases rather than prose corrections, and they are named in slice 5's
task list explicitly.

---

## 9. Next step

`sdd-tasks` — turn the ten-slice chain into an ordered task list, with §8.1's items 4 and 5 attached to
slice 5 as test cases, item 2 attached to slice 1's docs sweep and item 1 to slice 2, where it becomes true. Planning is otherwise closed:
proposal, spec and design are complete and have survived three adversarial rounds.
