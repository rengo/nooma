# Tasks — M0: the binary becomes runnable

Implementation task list for `m0-skeleton`, derived from `spec.md` (15 sections, 68 requirements)
and `design.md` (17 decisions). Chain strategy **`stacked-to-main`**: each PR targets `main` and
merges in order. Tasks 5.7 and 5.8 add two more requirements during PR 5, bringing the spec to 70 —
see **C1**.

**Strict TDD is active.** Every behavioral task states the test first and what its red looks like.
A red that is not the stated one means the test is wrong, not the expectation — stop and re-read the
requirement rather than adjusting the assertion.

Verification commands are drawn from the project's real targets: `make check`, `make check-all`,
`make test`, `make test-integration`, `make test-e2e`, `make cover`, `make cross-compile` (new,
PR 2), `make store-api-golden`, `make pending-red`. Tasks whose verification is a CI workflow or a
GitHub-side setting are marked **outside local verification** rather than mapped to a nearby command
that would misrepresent what actually checks them.

---

## Conflicts surfaced (do not silently resolve)

### C1 — Two known-debt items have no requirement number to cite

`proposal.md` §8.1 items 4 and 5 assign PR 5 two L1 test cases — a `readDir` error mid-ascent must
be a hard failure, and the vault predicate must reject a *directory* named `nooma.yml` — but neither
behavior has a numbered requirement in `spec.md` §6. Tasks 5.7 and 5.8 below therefore create the
requirements as part of that PR: `R6.8` and `R6.9`, written before their tests, in the same commit.

This is the project's own rule applied literally: `CLAUDE.md` non-negotiable #1 says code and spec
never diverge silently, and a test asserting behavior no requirement states is a divergence in the
direction people forgive. Do **not** implement 5.7/5.8 as "just a test" — write the requirement,
then the test, then the code.

### C2 — `R7.1b`'s number breaks the document's own convention

Every other requirement is `R<section>.<n>`. `R7.1b` was inserted after `R7.1` rather than appended
as `R7.7`, to keep it beside the requirement it qualifies without renumbering `R7.2`–`R7.6` and
invalidating cross-references. It is cited from `R10.4` and from design §8's test matrix. Leave the
number alone during apply; renumbering it is churn with a stale-reference risk and no reader
benefit.

### C3 — Design §1's `os.Rename` ground-truth row is ext4-only, and PR 8 must not inherit the gap

Design §1 records the `os.Rename` probe as run "locally on ext4", and D12's race argument calls the
final rename "kernel-atomic" without qualification. Go's own doc states the opposite for non-Unix
platforms (`$(go env GOROOT)/src/os/file.go:435`), and `init`'s L4 tests run on `windows-latest`.
This is `proposal.md` §8.1 item 3, assigned to PR 8: task 8.7 scopes the claim and adds the probe.
Until then, no task may cite "atomic rename" as a safety argument on Windows.

---

## PR 1 — Docs: the five doc-01 corrections plus doc 05's M0 bullet (~140 lines)

Docs only. No behavioral test: `spec.md` §1's requirements are verified by reading the documents,
which is what their own "Verified by" says. Touches no `internal/core/` path, so `docs-sync.yml`
does not fire and no `no-spec-change` label is needed.

- [ ] **1.1** `docs/01-architecture.md` §"The binary and the vault": remove `nooma.yml` from the
      installed-mode tree so config appears only at the vault root, matching §"Vault structure" and
      §"Configuration".
      Requirement: R1.1.
- [ ] **1.2** `docs/01-architecture.md` §"Vault resolution at startup": rewrite the four steps.
      Step 3 becomes the upward search — the cwd, then each parent to the filesystem root, testing
      at each level whether the directory is a vault or contains exactly one usable `*.nooma` — with
      the no-`$HOME`-ceiling statement. Step 4 names a `*.nooma` directory *inside* `~/.nooma/`.
      State the zero / one / many outcomes, that the literal name `.nooma` is never a candidate, and
      that an explicit argument may be relative. The phrase "next to the executable" must not remain
      anywhere in the document.
      Requirement: R1.2, and R6.1/R6.2/R6.6/R6.7 for the wording.
- [ ] **1.3** `docs/01-architecture.md` §"Configuration — `nooma.yml`": add `server.bind` (documented
      default `127.0.0.1`) and `server.auth_token_env` to the example block. Do **not** edit
      `docs/adr/0007-http-auth.md` — it is `Accepted` (non-negotiable #2); doc 01 is the incomplete
      one.
      Requirement: R1.3.
- [ ] **1.4** `docs/01-architecture.md` CLI table: `nooma status`'s row currently promises "last
      consolidation, channels, size". Date the brain-state half to M2 explicitly, or reword to what
      M0 reports. Do not silently drop the claim.
      Requirement: R1.4.
- [ ] **1.5** `docs/05-build-plan.md` M0 bullet: "vault resolution (arg → env → portable → home)"
      still describes the executable-relative model R6.6 removes. Rewrite to the four-step model.
      Requirement: proposal §8.1 item 2.
- [ ] Verify: `make check` (confirms nothing outside markdown broke); `rg "next to the executable"
      docs/` returns nothing; read all five edits against `spec.md` §1's "Verified by" clauses.

**Not in this PR, and the reason is the rule itself.** `docs/06-harness.md` §6 states that L4 and the
cross-compilation matrix do not run on every PR. That claim becomes false — but it becomes false in
**PR 2**, which changes the triggers. Correcting it here would leave the document wrong in the other
direction for as long as PR 1 sits merged alone, and would link ADR-0013 before it exists. Doc and
reality are fixed in the *same* PR (non-negotiable #1), so it moves to task 2.7.

This was found while writing PR 1, not while planning it: the plan had filed the correction here
because it is a docs edit, without asking which PR makes it true. "Docs go in the docs PR" is the
wrong grouping — the right one is "each claim lands with the change that makes it true".

---

## PR 2 — CI: triggers, ADR-0013, seven targets, `check-all` parity (~180 lines)

Lands before any OS-dependent code, so the lockfile's Windows behavior is exercised on the PR that
introduces it rather than after merge.

- [ ] **2.1** Write `docs/adr/0013-cross-compile-targets.md`, superseding **ADR-0001's acceptance
      criterion 5 only**. Record: criterion 5 lists four targets at line 47 while line 106 of the
      same file reports "PASS — 6/6" without naming them; the spike branch's `spike/RESULTS.md`
      names the actual six (`linux/amd64`, `linux/arm64`, `linux/arm`, `darwin/amd64`,
      `darwin/arm64`, `windows/amd64`), so `windows/arm64` was never tested and `linux/arm` was.
      Decision: seven targets — the cartesian six plus `linux/arm`. `windows/arm64` is justified by
      direct local measurement; `linux/arm` is retained because the spike verified it and a 32-bit
      Raspberry Pi is this project's own stated hardware. Do **not** edit ADR-0001.
      Requirement: R2.1; design D13.
- [ ] **2.2** `.github/workflows/main.yml`: add `pull_request` to the trigger; expand the
      cross-compile matrix from 4 entries to 7; rewrite the header comment, which currently explains
      why these jobs live outside `ci.yml` and would otherwise contradict the triggers below it.
      Verify: **outside local verification** (workflow trigger). `make cross-compile` (task 2.4)
      covers the build half locally.
      Requirement: R2.1.
- [ ] **2.3** `.github/workflows/ci.yml`: correct the comment near line 124
      ("cross-compilation matrix -> main.yml, on push to main only"), stale for the same reason.
      Requirement: R2.1; design §7.
- [ ] **2.4** `Makefile`: add a `cross-compile` target building the same seven `GOOS`/`GOARCH` pairs
      (`GOOS=x GOARCH=y go build ./...`); add both `cross-compile` and `test-e2e` to `check-all`.
      Verify: `make cross-compile` exits zero; `make check-all` exits zero.
      Requirement: R2.3, R2.4.
- [ ] **2.5** Update `CLAUDE.md`'s Workflow section and the `Makefile` header to name both new
      `check-all` targets. Delete any claim that e2e is excluded from `check-all` "for a documented
      reason" — no such reason exists in this repository, and `CLAUDE.md` names `docs-sync.yml` as
      the sole exception, for PR metadata, which e2e does not need.
      Requirement: R2.4.
- [ ] **2.6** Register every matrix-generated status context in the repository ruleset: 7
      cross-compile legs plus 2 e2e legs = **9** new contexts, on top of the 7 that already exist.
      Verify each string against the `name:` the workflow actually produces for that leg **before**
      applying — a required context that never posts is never satisfied and permanently blocks every
      merge to `main`. A matrix-aware synthetic gate is an acceptable alternative if its dependency
      on every leg is verified.
      Verify: **outside local verification** (GitHub configuration). No throwaway PR is needed —
      this slice's own PR posts the names. Push first, let the checks run, read the strings from
      `gh pr checks`, then register them and re-read the applied ruleset to confirm.

      **Done 2026-07-30 on #19**: 8 new contexts registered, not 9 — `e2e` posts a single context
      until slice 7 matrixes it. The ruleset now holds 15. See task 7.7b, which exists because
      matrixing that job renames its check and would otherwise leave `e2e` registered and never
      posted.
      Requirement: R2.2.
- [ ] **2.7** `docs/06-harness.md` §6: the sentence "what does **not** run on every PR: L4 (e2e),
      driver benchmarks, and the cross-compilation matrix" becomes false with task 2.2 — correct it
      here, in the PR that makes it false, not in PR 1. The sentence beside it, "the full matrix
      depends on ADR-0001 and cannot be designed until the spike closes", died when ADR-0001 closed
      two build-order steps ago; correct it too, and link ADR-0013, which task 2.1 creates in this
      same PR.
      Requirement: proposal §8.1 item 1; non-negotiable #1.

---

## Slice 3 — Config schema, strict decode, `.env`, validation

Depends on slice 1 (the schema must match doc 01 as corrected).

**Measured: it split into three.** Estimated ~400 review lines for all of slice 3; slice 3a alone
came to **530** before trimming, 327 of them tests. That is this project's own predicted 2–4x
underestimate, arriving on the first slice of real code. Owner decision (2026-07-30): respect the
ceiling, do not take a `size:exception`. **The chain is twelve slices, not ten**, and every later
estimate is the ceiling it was chosen to fit, not a forecast.

| | Content | Lines |
|---|---|---|
| **3a** | Schema types; `Decode` rejecting unknown keys, duplicates, wrong types and malformed input; empty document accepted; absence preserved | **398** |
| **3b** | `ApplyDefaults` + the four defaults, literal-credential rejection, `Summary` and the no-secret guarantee | **311** |
| **3c** | `.env`: the strict subset, its rejections, and file-does-not-beat-environment precedence | ~320 |
| **3d** | Validation: Telegram `allowed_chat_ids`, unset `*_env`, `database.path` escape, aggregate errors, documented types and task names | ~300 |

Slice 3b measured **630** against its own ~280 estimate and split again by the same rule — 2.2x, on
top of 3a's 1.3x. Slice 3 is **four** slices and the chain is **thirteen**. The pattern is now
measured three times running, so treat any remaining estimate in this file as roughly half of what
the work will be.

- [ ] **3.1** [setup, not TDD] `go get github.com/goccy/go-yaml@v1.19.2`; replace
      `internal/config/doc.go`'s placeholder text with the package contract.
      Verify: `go build ./...`.
      Requirement: design D1.
- [ ] **3.2** Test first: `TestLoadRejectsUnknownKey`, table-driven with one case per nesting level
      — top level, inside `server`, inside a named provider, inside a named task, inside
      `channels.telegram`, inside `schedules`. Each asserts the error names the offending key.
      **Red**: `undefined: config.Load`.
      Requirement: R3.2.
- [ ] **3.3** Write the schema types (`config.go`) covering `server`, `database`, `providers`,
      `tasks`, `channels`, `schedules`, every key in doc 01 as corrected by 1.3. Secrets are
      `*_env` name-holding fields only — no field anywhere may hold a literal credential, which is
      what makes R4.1 structural.
      Requirement: R3.1, R4.1.
- [ ] **3.4** Write `Load` with `goccy`'s `Strict()`. Note for the implementer: `Strict()` covers
      unknown fields only. Duplicate-key rejection is goccy's *default*, gated by a separate
      `allowDuplicateMapKey` flag — do not "add" it and do not disable it.
      Verify: `make test` — 3.2 goes green.
      Requirement: R3.2, R3.3; design D1.
- [ ] **3.5** Test first: defaults. `TestLoadAppliesDocumentedDefaults` asserts exactly four keys
      may be absent (`server.bind`, `server.http_port`, `server.ui`, `database.path`) and that
      "absent" stays distinguishable from "explicitly set to the default", so `status` can report
      which the user chose. Then implement post-decode default application.
      Verify: `make test`.
      Requirement: R3.4, R3.5.
- [ ] **3.6** Test first: `TestDotenv` over the accepted subset (`KEY=VALUE`, optional quotes, `#`
      comments, blank lines) **and** every rejection — a malformed line, `export` prefixes, a
      duplicate `KEY` within one file, and a bare unquoted `#` after a value. Each rejection names
      file and line. Then write `dotenv.go` (~40 lines).
      Verify: `make test`.
      Requirement: R4.3, R4.4; design D2.
- [ ] **3.7** Test first: `.env` precedence — variable only in the file, only in the environment,
      and in both (the environment wins). Then wire it into the load order.
      Verify: `make test`.
      Requirement: R4.3.
- [ ] **3.8** Test first: `TestConfigRenderNeverLeaksSecrets` puts a sentinel value in the
      environment, renders the config, and asserts the sentinel does not appear. Then implement the
      renderer.
      Verify: `make test`.
      Requirement: R3.5, R4.2.
- [ ] **3.9** Test first: validation. `channels.telegram.enabled: true` with empty
      `allowed_chat_ids` fails; enabled-with-ids passes; disabled-and-empty passes. A `*_env` whose
      variable is unset fails only when the consuming component is enabled. `database.path`
      resolves against the vault root, rejects a path escaping it, and yields an absolute path.
      Two independent violations both appear in one error. Then implement validation as a slice of
      named checks per D10, so "report everything" is structural rather than discipline.
      Verify: `make test`.
      Requirement: R5.1, R5.2, R5.3, R5.4.
- [ ] **3.10** Fix design §4's "four documented types" to "three" — doc 01 has four provider
      *entries* using three distinct `type` values (`anthropic` twice, `ollama`, `whisper_cpp`) —
      and validate `type` against those three plus the seven documented task names.
      Requirement: R3.1; proposal §8.1 item 7.
- [ ] Verify: `make check-all`.

---

## PR 4 — The config↔doc gate (~260 lines)

Depends on PR 3.

- [ ] **4.1** Test first: the extractor. Synthetic documents with zero, one and two candidate
      `yaml` fences in the target section, each asserting the expected error or the single block.
      **Red**: `undefined: <extractor>`.
      Requirement: R9.2.
- [ ] **4.2** Write a **new** section-scoped, language-tagged, exactly-one-or-error extractor,
      modeled on `test/support/goldenset/markdown.go`'s `ExtractJSONFence` shape. Do **not** reuse
      `test/support/schema/markdown.go`: it is hardcoded to ` ```sql ` fences, deliberately collects
      *every* match across the whole document, and has no arity check or section scoping. Without
      the section scope this gate would pass by luck today — doc 01 happens to contain exactly one
      ` ```yaml ` fence — and silently pick the first the day a second appears.
      Verify: `make test`.
      Requirement: R9.2; design §6.
- [ ] **4.3** Test first: `TestHarness_ConfigMatchesDoc01` in `test/conformance/`. Watch it fail in
      **both** directions before it passes: once by removing a key from doc 01, once by adding a
      field to the config struct. A gate observed failing in only one direction is half a gate.
      Requirement: R9.1.
- [ ] **4.4** Implement the comparison by **reflection over the `yaml` struct tags**, not by
      re-encoding the decoded value. A value-driven round-trip is vacuous the moment any field
      carries `omitempty`: a zero-valued undocumented field vanishes from the re-encode and the gate
      passes.
      Requirement: R9.1; design §6.
- [ ] **4.5** Implement the map-typed rule for `providers`/`tasks`: the schema side recurses into the
      map's *value* type and never compares the map's own keys; the document side **unions** field
      names across every entry before comparing. Add a case built from doc 01's real `providers:`
      block, whose four entries use disjoint field subsets — a per-entry completeness check is
      unsatisfiable on it.
      Verify: `make test`.
      Requirement: R9.1; design §6.
- [ ] **4.6** The gate decodes and compares schemas and **must not validate**. Doc 01's
      `embedding: { provider: ... }` placeholder decodes to the Go string `"..."`, which any provider
      validator would reject. Put this in the test file's own comment, so the first person to
      "improve" the gate by adding validation gets stopped in review.
      Requirement: R9.1.
- [ ] Verify: `make check-all`.

---

## PR 5 — Vault resolution (~360 lines)

Depends on PR 3. Closes known-debt items 4 and 5, which per **C1** means writing their requirements
first.

- [ ] **5.1** Test first: `TestResolveStepPrecedence`, driving each of the four steps in isolation
      with the earlier ones unavailable, plus precedence cases where two consecutive steps could
      both succeed. **Red**: `undefined: config.ResolveVault`.
      Requirement: R6.1.
- [ ] **5.2** Implement resolution over D7's injected `environment` (`getenv`, `getwd`, `homeDir`,
      `readDir`). **No `executable` member** — that is what makes R6.6 structural rather than a
      review note.
      Verify: `make test`.
      Requirement: R6.1; design D7.
- [ ] **5.3** Test first: the ascent. A cwd two levels inside a vault
      (`pablo.nooma/attachments/sub`) resolves to `pablo.nooma`, not to a different vault in
      `~/.nooma/`. Nearest wins. 3a beats 3b at the same level. The ascent stops at the filesystem
      root, verified on a path whose `filepath.Dir` reaches a fixed point.
      Verify: `make test`.
      Requirement: R6.1.
- [ ] **5.4** Test first: candidate counts per level — zero (error naming the directory searched and
      pointing at `nooma init`), one (used), two and three (error listing every candidate and showing
      the disambiguating command). Plus: a level with an *unusable* candidate — a `*.nooma` directory
      with no `nooma.yml` — surfaces it in the error rather than skipping it silently.
      Verify: `make test`.
      Requirement: R6.2.
- [ ] **5.5** Test first: `.nooma` itself is never a candidate. The glob `*.nooma` matches the name
      `.nooma`, so `~/.nooma` is a candidate *by name* on any ascent that reaches `$HOME`. It has no
      `nooma.yml`, so the predicate rejects it — and the error must **not** then report the container
      as a broken vault. Include a legitimately hidden vault (`.work.nooma`) to prove the exclusion
      is name-literal and does not over-fire.
      Verify: `make test`.
      Requirement: R6.7.
- [ ] **5.6** Test first: an explicit argument is used as given and never falls through; a relative
      argument resolves against the cwd, so `nooma serve pablo.nooma` from `/home/pablo` and the
      absolute form name the same vault; the resolved path is absolute before it can reach
      `sqlite.Open`, which rejects a relative path.
      Verify: `make test`.
      Requirement: R6.4.
- [ ] **5.7** **Write `R6.8` into `spec.md` first** (per C1): a `readDir` error at any level of the
      ascent is a hard resolution failure naming the directory and the error, never treated as "zero
      candidates, keep ascending". Then the test — a directory with execute-but-not-list permission
      — then the code.
      Verify: `make test`.
      Requirement: R6.8 (new); proposal §8.1 item 4.
- [ ] **5.8** **Write `R6.9` into `spec.md` first** (per C1): the vault predicate requires
      `nooma.yml` to exist *and not be a directory*, using the `DirEntry.IsDir()` already available
      from the same `readDir` call. Then the test — a directory named `nooma.yml` — then the code.
      Requirement: R6.9 (new); proposal §8.1 item 5.
- [ ] **5.9** Test first: `TestConfigNeverCallsOsExecutable` in `test/conformance/` — parse every
      non-test `.go` file under `internal/config/`, fail if any references `os.Executable`, naming
      file and line. Assert a non-empty corpus **before** asserting the property, per D10, so it
      cannot pass vacuously if the walk breaks.
      **Do not** implement this as a `forbidigo` rule. Measured against the pinned
      `golangci-lint v2.12.2`, `exclusions.rules` entries OR together: a second rule scoped to
      `internal/config/` suppresses the existing `internal/core/` one as well, silently disabling
      non-negotiable #3's enforcement. `.golangci.yml` is not touched by this PR.
      Verify: `make test`; confirm the scan fails by temporarily adding a call.
      Requirement: R6.6; design D16.
- [ ] **5.10** Test first: D8's partial-vault diagnostic names what is missing, probing the
      **default** `./nooma.db` only. A vault with a customised `database.path` trades that
      diagnostic away — state the limit in the code comment rather than implying a check that cannot
      exist (the path lives in the file that is missing).
      Requirement: R6.5; design D8.
- [ ] Verify: `make check-all`.

---

## PR 6 — Widen the store-API golden to `var` and `const` (~70 lines)

Independent of PRs 3–5; must precede PR 7. Small and deliberately alone: regenerating the golden
surfaces a pre-existing symbol, and that must be reviewable on its own rather than mixed into the
lock's diff.

- [ ] **6.1** Test first: extend `test/conformance/store_api_test.go`'s expectations so the golden
      must contain `ErrRelativeDBPath`. **Red**: the golden lacks it, because
      `renderExportedDecl` returns `nil` for every `GenDecl` whose `Tok != token.TYPE`
      (`store_api_test.go:158-160`) — exported `var` and `const` have been invisible to this golden
      since it was written.
      Requirement: R8.5; design D14.
- [ ] **6.2** Extend `renderExportedDecl` to render exported `var` and `const` declarations.
      Verify: `make test`.
      Requirement: R8.5.
- [ ] **6.3** `make store-api-golden` and review the diff. It must contain exactly one addition:
      `ErrRelativeDBPath` (`internal/store/sqlite/dsn.go:15`), pre-existing and unrelated to this
      change. Say so in the PR description — the point of this PR is that PR 7's golden diff then
      contains only `ErrVaultInUse`.
      Verify: `make check-all`; `git diff -- testdata/schema` shows one line added.
      Requirement: R8.5.

---

## PR 7 — The single-writer lock (~360 lines)

Depends on PR 6. Also where both CI jobs gain their Windows leg, since this is the first
OS-dependent code.

- [ ] **7.1** Test first: `TestSecondWriterFails` in `test/integration/` with a **real second
      process** (precedent: `migrate_race_integration_test.go`). Asserts non-zero exit, the holding
      PID in the message, and that the database's modification time is unchanged. **Red**:
      `undefined: vaultlock.Acquire`.
      Requirement: R8.2.
- [ ] **7.2** Create `internal/store/vaultlock/` with `lock.go`, `lock_unix.go` (`//go:build
      !windows`), `lock_windows.go`. `unix.Flock(fd, LOCK_EX|LOCK_NB)` and
      `windows.LockFileEx(..., LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY)`. Promote
      `golang.org/x/sys` from indirect to direct with `go mod tidy` — it costs no new module, it is
      already in `go.sum` through the driver.
      Requirement: R8.1; design D4, D5.
- [ ] **7.3** Implement `Acquire` in this order, which is load-bearing: **lock first**, then write
      the PID. Neither `flock` (advisory, whole-file) nor `LockFileEx` (byte range at offset 1024)
      protects the PID region, so a writer that writes its PID before contesting the lock overwrites
      the true holder's PID before discovering it lost — and `status` then reports a dead process.
      On lock failure, read the existing untouched PID and return it as the holder.
      Verify: `make test-integration`.
      Requirement: R8.2, R8.4; design D4.
- [ ] **7.4** The PID region is one `WriteAt(buf, 0)` of the **full 1024 bytes** of a freshly
      allocated (therefore zero-initialised) buffer — the single full-width write *is* the zeroing,
      so no previous holder's digits can survive. The lock byte is at offset **1024**, outside that
      region: on Windows an exclusive byte range genuinely blocks reads, so a PID stored at the lock
      byte would make R8.4 impossible. `ReadHolder` reads the region, stops at the first NUL, and
      takes no lock.
      Requirement: R8.4; design D4.
- [ ] **7.5** Test first: `TestLockSurvivesSIGKILL` — start a real child holding the lock,
      `SIGKILL` it, assert a new acquisition succeeds. This is the requirement that eliminated the
      naive PID file, so it must be observed failing against a PID-file implementation if there is
      ever any doubt.
      Verify: `make test-integration`.
      Requirement: R8.3.
- [ ] **7.6** Test first: `TestReadHolderDuringAcquire` reads the PID region concurrently with
      `Acquire` and asserts every read returns either the empty holder or the complete winning PID,
      never a partial one. This is the regression guard against reintroducing a two-write form.
      Verify: `make test-integration`.
      Requirement: R8.4; design D4.
- [ ] **7.7** Add `windows-latest` to `ci.yml`'s `integration` job matrix and to `main.yml`'s `e2e`
      job matrix, each with an explicit `make` install step — GNU Make is **not** on PATH on
      GitHub's Windows runners (MSYS2 is present but, per the runner-image documentation, not added
      to PATH), so without this the Windows leg fails on a missing tool and proves nothing about
      `LockFileEx`. Prefer the explicit install over a PATH hack that depends on MSYS2 shipping
      `make`.
      Verify: **outside local verification**; confirm on the PR's own checks.
      Requirement: R15.1; design D6, D17.
- [ ] **7.7b** **Re-register the e2e contexts in the ruleset, in this same slice.** Matrixing the
      e2e job renames the check it posts: today it posts one context, `e2e`, which slice 2 registered
      as required. Adding a matrix makes GitHub post `e2e (ubuntu-latest)` and
      `e2e (windows-latest)` instead, so the registered `e2e` context stops being produced — and a
      required context that is never produced is never satisfied, which **permanently blocks every
      merge to `main`**.

      Order matters: push the workflow change, let the checks post, read the two real names from
      `gh pr checks`, then replace `e2e` with them in `required_status_checks`. Do not type the names
      from this task — read them from what the workflow produced, which is how slice 2 found this
      trap in the first place.

      After this task the ruleset holds **16** contexts (7 pre-existing + 7 cross-compile + 2 e2e),
      not the 15 slice 2 left. Spec R2.2's "9 new contexts" is the end state across both slices, and
      the registration happens in two steps because the second step does not exist until the job is
      matrixed.
      Verify: **outside local verification**; re-read the applied ruleset and confirm every context
      matches a name the workflows actually post.
      Requirement: R2.2.
- [ ] **7.8** Export `ErrVaultInUse` so `cmd/nooma` can distinguish "held" from other I/O failures;
      `make store-api-golden`. The diff must show only `ErrVaultInUse` — slice 6 already surfaced
      `ErrRelativeDBPath`.
      Verify: `make check-all`.
      Requirement: R8.5.

---

## PR 8 — CLI dispatch and `nooma init` (~410 lines — expect a split)

Depends on PR 5 and PR 2.

- [ ] **8.1** Test first: `TestUsageListsEveryCommand` — bare `nooma` exits zero and names exactly
      `init`, `serve`, `status`, `doctor`, `version`. Then the dispatch table over the existing
      `run(args, out)` shape, plus a stderr writer so R10.5 is testable at L1.
      Requirement: R10.1, R10.3, R10.5; design D3.
- [ ] **8.2** Test: a command that does not exist yet (`consolidate`) exits non-zero with an
      unknown-command error. There must be **no stub** printing "not implemented" — a stub teaches
      the user the command exists.
      Verify: `make test`.
      Requirement: R10.1.
- [ ] **8.3** Test first: `nooma version` still matches `^nooma \S+ \(\S+\)\n$`. The existing
      `test/e2e/version_test.go` must pass **unmodified** — if it needs editing, the dispatch
      refactor broke a contract.
      Verify: `make test-e2e`.
      Requirement: R10.2.
- [ ] **8.4** Test first: `init`'s target default (R7.1b), four L4 cases with `$HOME` pointed at a
      temp dir — bare `init` creates `~/.nooma/<username>.nooma` and prints its absolute path; bare
      `init` refuses when `~/.nooma/` already holds a usable vault, naming it; a relative argument;
      an absolute argument. A bare `init` must never write vault contents into the cwd.
      Requirement: R7.1b.
- [ ] **8.5** Test first: `init` creates the complete vault — `nooma.db` at the current
      `user_version`, a valid `nooma.yml`, a `.env` skeleton documenting D2's accepted subset, and
      `attachments/`, `derived/`, `logs/`. Then implement.
      Verify: `make test-e2e`; `make test-integration` for the `user_version` assertion.
      Requirement: R7.1.
- [ ] **8.6** Test first: the target guards, in this order. `Lstat` the target and **refuse without
      touching anything** when it exists and is not a plain directory — including when it is a
      symlink. Only a real directory reaches the empty/non-empty branch. Cases: a plain file
      (`touch pablo.nooma`), a symlink to an empty directory, an existing empty directory
      (succeeds), an existing non-empty directory (refuses).
      The file case matters because `os.ReadDir` on a plain file returns an *error*, not an empty
      listing: an emptiness check written as `len(entries) == 0` without requiring `err == nil`
      classifies a stray file as empty, and `os.Remove` deletes plain files — turning `init` into a
      delete.
      Verify: `make test-e2e`.
      Requirement: R7.5; design D12.
- [ ] **8.7** Implement build-in-sibling-temp-then-rename: temp suffix is PID plus a random
      component (two racing `init`s must not build into one directory); rename when the target does
      not exist; `os.Remove` then rename when it exists and is empty; clean up the temp directory on
      any failure. **Scope the atomicity claim**: Go's `os.Rename` is documented non-atomic on
      non-Unix platforms (`$(go env GOROOT)/src/os/file.go:435`), and this PR's L4 tests run on
      Windows — add a Windows probe row to design §1 and qualify D12's "kernel-atomic" wording
      (C3, proposal §8.1 item 3). Classify all three race outcomes: `EEXIST` and `ENOTEMPTY` from
      the rename, and `ENOENT` from a losing `Remove`.
      Verify: `make test-e2e`; `make check-all`.
      Requirement: R7.3, R7.4, R7.6; design D12.
- [ ] **8.8** Test first: the wizard collects the same input struct the non-interactive path takes
      (D15), so the interactive path cannot produce a vault the tested path cannot. The
      non-interactive path is the primary contract and runs with stdin closed.
      Verify: `make test`; `make test-e2e`.
      Requirement: R7.2; design D15.

---

## PR 9 — `status` and `doctor` (~400 lines — expect a split)

Depends on PR 5 and PR 7.

- [ ] **9.1** Test first: `status` reports the resolved vault path, the schema `user_version`, the
      lock holder if any, the database size on disk, and a config summary (bind, port, UI,
      configured channels). **Red**: unknown command.
      Requirement: R12.1.
- [ ] **9.2** Test first: `status` and `doctor` both succeed on a vault `serve` currently holds, and
      report the holder's PID. Neither may call `Acquire`, block on it, or fail because of it.
      Verify: `make test-e2e`.
      Requirement: R8.4, R12.3.
- [ ] **9.3** `status` reads **no domain row**. The boundary is already enforced three ways — the
      unexported handle, `sqlite-containment` (`cmd/**` cannot import `database/sql`), and
      `store_api.golden` — so this task is a review check, not new code: confirm no query against
      `units`, `relations`, `triggers`, `beliefs` or `decision_log` was added.
      Requirement: R12.2.
- [ ] **9.4** Add `(*Vault).IntegrityCheck(ctx) error` to `internal/store/sqlite`. Name it
      explicitly: `(*Vault).Check` already exists at `open.go:192` as an unrelated FTS5-registration
      probe and is already in the golden. Regenerate the golden.
      Verify: `make test-integration`; `make check-all`.
      Requirement: R13.5.
- [ ] **9.5** Test first: `doctor` reports config validity, vault-directory permissions,
      `PRAGMA integrity_check`, `user_version` against the expected migration count, and the
      effective bind with whether it is exposed — each as an individually identifiable result. Then
      implement checks as a slice of named `{name, run}` values per D10, so "report every failure"
      is structural. Per-check L3 tests break one thing at a time.
      Verify: `make test-integration`; `make test-e2e`.
      Requirement: R13.1, R13.2.
- [ ] **9.6** Test first: `doctor` exits zero when every check passes and non-zero when any fails,
      so it is usable in a script.
      Verify: `make test-e2e`.
      Requirement: R13.3.
- [ ] **9.7** Confirm by absence: no provider connectivity, model availability, or hardware check
      exists in `doctor`'s code path. Providers arrive in M1; "minimum hardware" is an open dated
      decision due before M6. A check that cannot be implemented honestly is worse than an absent
      one, because its passing means nothing.
      Requirement: R13.4.
- [ ] **9.8** Test first: a sentinel secret in the environment appears in neither stdout nor stderr
      of `status` or `doctor`.
      Verify: `make test-e2e`.
      Requirement: R4.2.

---

## PR 10 — `nooma serve` (~400 lines — expect a split)

Depends on PR 5 and PR 7.

- [ ] **10.1** Test first: `decideBinding(cfg)` truth table at L1 — loopback / non-loopback ×
      token present / absent. Include the adversarial addresses: `127.0.0.1` and all of
      `127.0.0.0/8`, `::1`, the literal `localhost`, `0.0.0.0`, `::`, and `127.0.0.1.evil` and
      `0127.0.0.1`, which must **not** read as loopback. Decide by `net.ParseIP` plus
      `IP.IsLoopback`, never by string comparison; the literal `localhost` is a special case with
      **no DNS lookup**; any other non-IP hostname fails safe as exposed.
      Requirement: R11.3; design D9.
- [ ] **10.2** Test first: a non-loopback bind without `server.auth_token_env` exits non-zero
      **without listening on any socket**. Assert nothing is listening on the configured address
      afterwards. A server that starts and then complains has already exposed the port.
      Verify: `make test-e2e`.
      Requirement: R11.2; design D11.
- [ ] **10.3** Implement `internal/httpapi`: a minimal mux serving an API hello and a `/ui`
      placeholder. `decideBinding` is called before `net.Listen`, so no path can create a listener
      while skipping the refusal.
      Verify: `make test-e2e`.
      Requirement: R11.1.
- [ ] **10.4** Test first: `serve` on an ephemeral loopback port answers both endpoints with 2xx and
      a non-empty body. Note in the test file that a loopback listener is not "touching the network"
      in `docs/06-harness.md` §3's sense, so a future reader does not wonder.
      Requirement: R11.1, R15.2.
- [ ] **10.5** Test first: `serve` takes the write lock for its lifetime and releases it on exit,
      including on `SIGINT` and `SIGTERM` — stop the listener, release the lock, close the database,
      exit zero. The kernel would release the lock anyway; the explicit release exists so the test
      observes a released lock rather than a released-by-luck one.
      Verify: `make test-e2e`.
      Requirement: R8.1, R11.5.
- [ ] **10.6** Confirm the M0 boundary held: `make pending-red` still passes and
      `test/conformance/pending_symbols.txt` is unmodified across the whole chain; `git diff` over
      `internal/store/sqlite/migrations/` is empty; no file under `internal/core/**` was touched.
      Verify: `make check-all`; `git diff --stat main...HEAD`.
      Requirement: R14.1, R14.2, R14.3.
- [ ] **10.7** Run the demo by hand and record the output in the PR: `nooma init && nooma serve`,
      then `nooma status` and `nooma doctor` against the running instance, then a second `serve` to
      watch it refuse.
      Requirement: proposal §2's demo criterion.

---

## Review Workload Forecast

**Chained PRs recommended: yes — already chained, ten of them.**
**400-line budget risk: high on PRs 3, 8, 9 and 10.**
**Decision needed before apply: no** — the split is already decided; what remains is honoring it.

`complete-harness` measured its own estimates and found implementation underestimated **2–4x**,
with review remediation roughly doubling that again. The line figures above are per-PR *ceilings
chosen to respect the 400-line rule*, not predictions. Concretely, expect:

| PR | Forecast | Split trigger |
|---|---|---|
| 3 | ~400 | Likely splits: schema + strict decode, then `.env` + validation |
| 5 | ~360 | May split: resolution, then the two new requirements (5.7–5.9) |
| 8 | ~410 | Very likely splits: dispatch + `init` core, then the target guards |
| 9 | ~400 | Likely splits: `status`, then `doctor` + `IntegrityCheck` |
| 10 | ~400 | May split: binding decision + refusal, then the server |

The split decision is made **before** `sdd-apply` runs on each PR, never discovered afterwards —
the policy adopted during `complete-harness` after discovering it the hard way twice.

**Four-lens pre-PR review is not optional on the code PRs.** During `complete-harness` it found a
blocker or a critical in **every** code PR, with CI fully green throughout, and one defect had
already shipped to `main` and survived its own PR's review.

---

## Residual risks mapped to tasks

| Risk | Where it is addressed | Residual |
|---|---|---|
| `flock`/`LockFileEx` unreliable over NFS/SMB | design §9 risk 1 | Accepted: the guarantee is scoped to local filesystems. A network-filesystem `doctor` check is a later milestone. |
| Windows byte-range lock semantics differ from `flock` | 7.4, 7.7 | The Windows CI leg is what makes 7.4 more than a compiling assumption. |
| `os.Rename` non-atomic on Windows | 8.7, C3 | Probe added in 8.7; until then no task may cite atomic rename as a Windows safety argument. |
| Map-keyed sections escape `Strict()` | 3.10, 4.5 | Compensated by name validation plus the gate's union rule. A new task name stays a two-place change. |
| Config schema decodes M1/M3 semantics | 3.3 | Shape-checked, never interpreted. The §6 gate makes any later shape change one visible diff. |
| The pending-red gate is self-dismantling but not self-updating | 10.6 | If M1 names a symbol differently the gate stays green. Anchor comments mitigate; they do not eliminate. |

---

## Traceability

Every requirement group in `spec.md` maps to at least one task above:

| Spec | Tasks |
|---|---|
| §1 doc corrections (R1.1–R1.4) | 1.1–1.4 |
| §2 CI (R2.1–R2.4) | 2.1–2.7, 7.7 |
| §3 config schema (R3.1–R3.5) | 3.1–3.5, 3.10 |
| §4 secrets (R4.1–R4.4) | 3.3, 3.6–3.8, 9.8 |
| §5 validation (R5.1–R5.4) | 3.9 |
| §6 resolution (R6.1–R6.9) | 5.1–5.10 |
| §7 `init` (R7.1–R7.6, R7.1b) | 8.4–8.8 |
| §8 the lock (R8.1–R8.5) | 6.1–6.3, 7.1–7.8, 10.5 |
| §9 the config↔doc gate (R9.1–R9.3) | 4.1–4.6 |
| §10 CLI surface (R10.1–R10.5) | 8.1–8.3 |
| §11 `serve` (R11.1–R11.5) | 10.1–10.5 |
| §12 `status` (R12.1–R12.3) | 9.1–9.3 |
| §13 `doctor` (R13.1–R13.5) | 9.4–9.7 |
| §14 boundaries (R14.1–R14.4) | 10.6 |
| §15 test levels (R15.1–R15.3) | 7.7, 10.4, and every "Test first" line |
| proposal §8.1 known debt | 1.5, 2.7, 3.10, 5.7, 5.8, 8.7 |
