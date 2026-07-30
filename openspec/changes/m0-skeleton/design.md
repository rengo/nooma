# Design — M0: the binary becomes runnable

Technical design for `m0-skeleton`. It settles the four remaining open questions from
`proposal.md` §8 (YAML parser, `.env` loading, CLI dispatch, lock mechanism — D1 through D4) and
fixes the mechanisms that satisfy `spec.md`. Windows runtime coverage, originally open question 1,
was already decided (D6) and is hardened further here by extending Windows coverage to the L3
lock tests, not just the L4 e2e job. Where a claim about a library or a syscall appears below, it
was verified locally against the module cache or by running code — not asserted from memory.

---

## 1. Ground truth this design was verified against

| Claim | How it was verified |
|---|---|
| `golang.org/x/sys v0.46.0` is already in the module graph | `go.sum` carries it; it arrives indirect through the SQLite driver |
| `unix.Flock` exists for linux, darwin/amd64 and darwin/arm64 | `zsyscall_linux.go:820`, `zsyscall_darwin_{amd64,arm64}.go` in the cached module |
| `windows.LockFileEx` plus `LOCKFILE_EXCLUSIVE_LOCK` and `LOCKFILE_FAIL_IMMEDIATELY` exist | `zsyscall_windows.go:2923`, `syscall_windows.go:67-68` |
| `gopkg.in/yaml.v3` has exactly two published versions, v3.0.0 and v3.0.1 | `go list -m -versions gopkg.in/yaml.v3` |
| v3.0.1 is dated 2022-05-27; `goccy`'s v1.19.2 is dated 2026-01-08 | `go list -m -json` on both, reading `Time` |
| `yaml.v3`'s `Decoder.KnownFields(bool)` exists | `yaml.go:110` in the cached module |
| `goccy/go-yaml` has `Strict()` and `DisallowUnknownField()` | `option.go:55` and `option.go:65` at v1.19.2 |
| `goccy/go-yaml` has zero module dependencies | its `go.mod` declares no `require` block |
| Both libraries reject unknown keys, duplicate keys and type mismatches | a probe program run against both — output in D1 |
| `internal/store/sqlite.Open` rejects a relative path | `dsn.go:15`, `ErrRelativeDBPath` |
| `cmd/**` cannot import `database/sql` | `.golangci.yml`'s `sqlite-containment` rule, `$all` minus `internal/store/**` and `test/integration/**` |
| The store-API golden walks `internal/store/**` recursively | `test/conformance/store_api_test.go:107` (the recursive `filepath.WalkDir` call; line 47, cited by an earlier draft of this design, is only where `storeDir` is computed) |
| L4 and cross-compile run only on `push: main` today | `.github/workflows/main.yml:15-17`, and `ci.yml:120-124`'s own comment |
| `os.Rename` fails onto an existing directory whether it is empty or non-empty | probe run locally on ext4: both cases return `file exists`; only a non-existent target succeeds. Go's `os.Rename` does an `Lstat` and returns `EEXIST` without calling `rename(2)`, even though POSIX `rename(2)` itself permits replacing an empty directory (`$(go env GOROOT)/src/os/file_unix.go`) |
| The cross-compile matrix has 4 entries today, not 6 | `.github/workflows/main.yml` — exactly `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64` |
| ADR-0001 contradicts itself on the matrix size | `docs/adr/0001-sqlite-driver.md:47` lists 4 targets as acceptance criterion 5; line 106 of the same file reports "**PASS** — 6/6 targets" |
| The store-API golden's decl walker drops every non-`TYPE` `GenDecl` | `test/conformance/store_api_test.go:158-160` — `case *ast.GenDecl: if d.Tok != token.TYPE { return nil }`; exported `var` and `const` are invisible to the golden |
| Every job in `ci.yml` runs on `ubuntu-latest`, with no matrix | `.github/workflows/ci.yml` — `lint`, `test`, `build`, `integration`, `pending-red`, `coverage` are all `runs-on: ubuntu-latest` |
| `(*Vault).Check` already exists and means something unrelated | `internal/store/sqlite/open.go:192` — an FTS5-registration probe, already in `testdata/schema/store_api.golden` |
| goccy's duplicate-key rejection is independent of `Strict()` | `option.go:55` — `Strict()` sets only `disallowUnknownField = true`; duplicate-key rejection is gated by a separate `allowDuplicateMapKey` flag, default `false`, disabled only by the opt-out `AllowDuplicateMapKey()`. `yaml.v3` has the same split: the duplicate-key error is independent of `KnownFields` |
| `os.Executable` returns the resolved path, not the symlink the user invoked | probe: a binary at `real/exeprobe` invoked through a symlink at `bin/exeprobe` reported `os.Args[0] = .../bin/exeprobe` but `os.Executable = .../real/exeprobe`. This is why D7 has no `executable` member and R6.6 removes executable-relative discovery |
| `docs/01-architecture.md`'s task example decodes but would fail validation | the literal placeholder `embedding: { provider: ... }` decodes cleanly to the Go string `"..."` under goccy's `Strict()`; a validator checking that provider name against the declared `providers:` map would reject it |

---

## 2. Decision record

### D1 — YAML: `github.com/goccy/go-yaml` v1.19.2, in `Strict()` mode

Both candidates satisfy `spec.md` R3.2 and R3.3. The decision came from measuring what each one
*says*, because the error message is the feature: a config error is the most common failure a
user will ever see from this binary, and `docs/01-architecture.md` sells `doctor` on making the
binary "feel cared for".

The probe decoded six documents through both libraries. Both accepted the valid one and rejected
all five bad ones — no silent acceptance in either. The difference is in the reporting:

| Case | `yaml.v3` + `KnownFields(true)` | `goccy` + `Strict()` |
|---|---|---|
| Unknown key `http_prot` | `line 3: field http_prot not found in type main.Server` | `[3:3] unknown field "http_prot"` + three source lines with a caret under the key |
| Unknown top-level key | `line 3: field sevrer not found in type main.Config` | `[3:1] unknown field "sevrer"` + source excerpt |
| Duplicate key | `line 3: mapping key "http_port" already defined at line 2` | `[3:3] mapping key "http_port" already defined at [2:3]` + excerpt |
| Wrong type | `line 2: cannot unmarshal !!str `not-a-n...` into int` | `[2:14] cannot unmarshal string into Go struct field Config.Server of type int` + caret at the value |

The wrong-type row is the one that decides it. `yaml.v3` reports the line and the Go types, and
**never names the key** — the user gets "cannot unmarshal `!!str` into int" and has to count
lines. `goccy` gives line, column, the struct path, and points at the offending value.

Secondary factors, all pointing the same way:

- **Maintenance.** `yaml.v3` has published two versions ever — v3.0.0 and v3.0.1, the latter dated
  **2022-05-27**, four years ago. `goccy`'s v1.19.2 is dated **2026-01-08**, with seven releases
  across four minor versions (v1.16.0, v1.17.0, v1.17.1, v1.18.0, v1.19.0, v1.19.1, v1.19.2)
  visible in the recent tail. For a dependency that parses the one file every user edits by hand,
  in a project that expects outside contributions, a parser with no release in four years is a
  liability.
- **Dependency purity.** Both are dependency-free. Neither costs a transitive tree.
- **Size.** `goccy` is 14.5k non-test lines against `yaml.v3`'s 11.3k. A real cost, and the
  smallest of the three factors.

`Strict()` is what gets used for the unknown-field half. Duplicate-key rejection is **not** part
of `Strict()`: in both `goccy` (`option.go:55`) and `yaml.v3`, duplicate-key rejection is the
decoder's *default* behavior, gated by a separate flag (`allowDuplicateMapKey`, default `false`,
in goccy) that only an explicit opt-out disables. The probe's measured outputs above are correct
either way — both libraries reject the duplicate-key case out of the box — but the two properties
come from two independent mechanisms, not one option covering both.

### D2 — `.env`: a hand-rolled strict parser, ~40 lines, in `internal/config`

`joho/godotenv` would work and its `Load` already has R4.3's precedence semantics (it does not
overwrite an existing variable). It is rejected anyway, for one reason: it is permissive. The
`.env` format has no specification, and every library invents its own tolerance — `export`
prefixes, multi-line values, interpolation, unquoted spaces, lines it simply skips.

This project's dominant defect family, found nine times during `complete-harness`, is a component
that silently discards part of its input. A permissive `.env` parser is that failure by
construction: a malformed line becomes a missing credential, which becomes a provider error three
layers away.

So M0 defines the subset it accepts and rejects everything else by name:

- `KEY=VALUE`, where `KEY` matches `[A-Za-z_][A-Za-z0-9_]*`
- optional single or double quotes around `VALUE`, stripped
- a line whose first non-space character is `#` is a comment
- a blank line is skipped
- **the same `KEY` appearing twice inside one `.env` is an error** naming the file and both line
  numbers — the same exactly-one-or-error stance §6 takes on vault discovery and D1 takes on
  duplicate YAML keys, applied consistently to the third format this project parses by hand
- **a bare, unquoted `#` after a value is rejected as ambiguous**, not absorbed into the value.
  `KEY=value # comment` has no defined meaning in this grammar: the naive read yields
  `"value # comment"`, which is exactly the kind of unrecognized shape D2's "reject what it does
  not understand" stance exists to catch rather than silently accept
- **anything else is an error naming the file and line number** — no `export`, no interpolation,
  no multi-line values, no silent skip

The subset is documented in the generated `.env` skeleton so a user sees the rules where they
edit. Growing it later is additive and safe; starting permissive is not reversible.

### D3 — CLI: stdlib `flag` and a dispatch table, no framework

`cmd/nooma/main.go` keeps `run(args []string, out io.Writer) error` — it is already testable
without touching the process streams, which is exactly the property a framework would take away.
It gains a table mapping name to handler, and each handler owns a `flag.FlagSet`.

Five commands, no shell completion, no nested subcommands, no generated help beyond one usage
line. `cobra` brings a transitive tree and a global-state style for a problem this size does not
have. The project's identity is a self-contained binary; the CLI layer is the last place to spend
a dependency.

`run` gains a second writer for stderr so R10.5 (errors to stderr, success to stdout) is testable
in L1 rather than only in L4.

### D4 — The lock: an OS advisory lock through `golang.org/x/sys`, PID in the file

Three candidates existed. The decision is driven by `spec.md` R8.3: after a `SIGKILL`, a later
writer must be able to acquire the lock without the user deleting a file by hand.

- **`O_EXCL` create plus a PID file** fails R8.3 outright. The file survives its process. Adding
  staleness detection means asking "is PID 1234 alive?", which is `kill(pid, 0)` on unix and
  `OpenProcess` on Windows — the same amount of OS-specific code, plus a genuine race (the PID
  may have been recycled) and a wrong answer that silently permits two writers. The mechanism
  built to prevent corruption becomes the thing that permits it.
- **An OS advisory lock** is released by the kernel when the process dies, however it dies. R8.3
  is satisfied for free, with no liveness check and no race.

So: `flock(fd, LOCK_EX|LOCK_NB)` on unix, `LockFileEx(handle, LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY, ...)`
on Windows, in two build-tagged files behind one interface:

```
internal/store/vaultlock/
├── lock.go           // Acquire(vaultDir) (*Lock, error), (*Lock).Release, ReadHolder(vaultDir)
├── lock_unix.go      //go:build !windows
└── lock_windows.go   //go:build windows
```

**The PID and the lock byte do not overlap**, and that detail is load-bearing. `flock` is
whole-file and advisory: it blocks no read. `LockFileEx` is a *byte-range* lock and an exclusive
range genuinely blocks other processes from reading those bytes. If the PID lived where the lock
byte is, `status` and `doctor` on Windows could not read the holder — and R8.4 requires exactly
that. So:

- the PID text is written as the first bytes of the file, bounded to the first 1024
- the lock is taken on the single byte at **offset 1024**, beyond that region and legally beyond
  EOF on Windows
- `ReadHolder` reads the first 1024 bytes and takes no lock at all

**The order between the PID write and the lock acquisition is load-bearing, and it runs opposite
to how it reads at first glance.** Neither `flock` nor `LockFileEx` protects the PID region: `flock`
is advisory and whole-file, so it gates no read or write anywhere in the file, and `LockFileEx`'s
exclusive range sits at offset 1024, not at offset 0. If the PID were written *before* the lock is
contested, the process that loses the race (R8.2's everyday scenario — a second `nooma serve` on a
held vault) would overwrite the real holder's PID with its own before discovering it lost, and
`status`/`doctor` would then report a dead process as the holder. That breaks R8.2 and R8.4 inside
the exact mechanism built to report the holder truthfully.

So `Acquire` runs in this order:

1. attempt `flock(fd, LOCK_EX|LOCK_NB)` / `LockFileEx(..., LOCKFILE_FAIL_IMMEDIATELY)` on the
   byte at offset 1024 **first**, before touching the PID region at all
2. on success: zero/truncate the first 1024 bytes, then write the current PID — zeroing first so a
   shorter PID (`"7"` after `"123456"`) can never leave stale trailing bytes from the previous
   holder
3. on failure (lock already held): read the existing, untouched PID region and return it as the
   holder — nothing in the file is written, because nothing about the losing process is true yet

The PID is truncated on release, after the lock is dropped. A stale PID with no lock held is not
authoritative and is reported as such — the lock, not the file's existence, is the truth. The
"acquire, then write" ordering is the whole reason a losing `Acquire` never corrupts the winner's
PID.

The PID write is a single small write (well under any platform's atomic-write guarantee for a
short buffer), because `ReadHolder` takes no lock and could otherwise observe a torn write on a
filesystem where a small write is not atomic — see risk #1.

### D5 — `golang.org/x/sys` is promoted from indirect to direct

The lock is the only new import, and it costs **no new module**: `x/sys v0.46.0` is already in
`go.sum`, arriving indirect through the SQLite driver. `go mod tidy` moves it into the direct
`require` block. The dependency count goes from one to two, and the second one was already being
compiled.

### D6 — Windows gets a real e2e runner and a real integration runner; macOS does not

`spec.md` R2.1 puts cross-compilation on every PR, which proves the lock *builds* on
`windows/amd64`. It proves nothing about `LockFileEx`'s behavior, and D4's byte-offset scheme is
precisely the kind of thing that compiles perfectly and is wrong.

The lock's actual behavioral tests — contention (R8.2) and `SIGKILL` recovery (R8.3) — are **L3**
by the taxonomy (§8), not L4: `docs/06-harness.md` §3 puts "a real second process" at L3, and §10
of this design does not move it. Every job in today's `.github/workflows/ci.yml`, including
`integration`, is `runs-on: ubuntu-latest` with no matrix. If only the L4 e2e job gained a Windows
runner, the tests most likely to catch a byte-offset bug would never run on Windows at all — the
e2e job's own scope (§8: "every command's contract, §10-§13") excludes the lock's contention and
crash-recovery tests by construction.

So **both** jobs gain a Windows leg, in the same PR that adds the lock:

- the e2e (L4) job runs a matrix of `ubuntu-latest` and `windows-latest`, as before — this is
  D6's original scope, unchanged
- the `integration` (L3) job also gains a matrix of `ubuntu-latest` and `windows-latest`, so
  R8.2's and R8.3's tests actually execute `LockFileEx` rather than only cross-compiling it

macOS is deliberately left out of both: `darwin` uses the same `unix.Flock` code path as Linux, so
a macOS runner would re-verify an already-verified path, and GitHub bills macOS minutes at 10x
against Windows's 2x for private repositories. `darwin` keeps cross-compile coverage plus the
shared code path.

This is a recorded, revisitable tradeoff, not an oversight. When the repository goes public
(runner minutes become free) or when the first `darwin`-specific code appears, it changes.

### D7 — Vault resolution is a pure function over an injected environment

Resolution needs `os.Getenv`, `os.Getwd`, `os.UserHomeDir` and a directory listing. Reaching for
those directly would make R6.1's precedence tests L3-or-worse: they would have to mutate the real
`$HOME` and `chdir` the test process, which is global state no parallel test can share.

Following the precedent the project already set twice — the injected `pathStyle` in
`buildDSN`, and the `Clock` port — resolution takes its environment as a value:

```go
type environment struct {
    getenv  func(string) string
    getwd   func() (string, error)
    homeDir func() (string, error)
    readDir func(string) ([]os.DirEntry, error)
}
```

Production builds one from `os`; tests build one from a map and a temp dir. All of §6 becomes L1.

**There is deliberately no `executable` member.** `docs/01-architecture.md`'s original step 3 was
"vault next to the executable"; R6.6 removes it and states the three reasons. The one that belongs
here, because it is a fact about the standard library rather than about convention: `os.Executable`
returns the *resolved* path, so a symlinked install (`~/.local/bin/nooma -> /opt/nooma/nooma`)
would search `/opt/nooma/` — verified by probe, not assumed. A resolution step whose search
directory the user cannot predict from the command they typed is a step that will one day open the
wrong brain. The cwd is predictable by construction: the user is standing in it.

### D8 — "Is a vault" means "contains a readable `nooma.yml`"

`nooma.db`'s location is configurable (`database.path`), so it cannot be the marker — a vault
whose database sits in a subdirectory is still a vault. The config file is at a fixed path by the
layout decision, which makes it the only stable marker.

Consequence, and it is the desirable one: a directory with a `nooma.yml` and no database is a
*vault with a problem*, which `doctor` reports precisely, rather than "not a vault", which sends
the user hunting for the wrong thing.

### D9 — Loopback detection parses, and an unknown hostname is treated as exposed

ADR-0007's rule is a security boundary, so the decision is made by `net.ParseIP` plus
`IP.IsLoopback`, never by string comparison. Three cases:

1. the value parses as an IP → `IsLoopback` decides (covers `127.0.0.1`, all of `127.0.0.0/8`,
   `::1`, and rejects `0.0.0.0` and `::`)
2. the value is the literal `localhost` → loopback, **without a DNS lookup**
3. anything else that is not an IP → **treated as non-loopback**, so the token becomes mandatory

Case 2 is a deliberate special case rather than a resolution: a DNS lookup to decide a security
property is both slow at startup and untestable without the network, which non-negotiable #5
forbids. Case 3 fails safe — an unresolvable or unrecognized hostname demands the token rather
than assuming safety. `127.0.0.1.evil` is a hostname, not an IP, and therefore exposed.

### D10 — `doctor`'s checks are data, so "report everything" is structural

R13.2 requires `doctor` to run every check even when one fails. Written as sequential `if err !=
nil { return err }`, that requirement is discipline, and discipline decays.

So a check is a value: `{name string, run func(vaultContext) checkResult}`. `doctor` iterates the
slice, collects results, prints all of them, and derives its exit code from whether any failed. A
future check is appended to a slice; there is no control flow to get wrong.

Config validation (R5.4) uses the same shape for the same reason, and returns an aggregate error.

### D11 — ADR-0007's refusal is decided before a socket exists

The check is a pure function `func decideBinding(cfg) (addr string, err error)`, called before
`net.Listen`. R11.2 requires that nothing ever listens when the refusal fires; making the decision
a pure function called first means the listener cannot be created by any path that skips it, and
the whole truth table is L1-testable.

### D12 — `init` writes into a temporary directory, then clears an empty target before renaming

R7.3 (never overwrite) and R7.4 (no half-vault) are one problem. `init` creates
`<target>.tmp-<n>`, builds the complete vault inside it — migrations included — and only then
places it at the target. A failure at any point removes the temporary directory and leaves the
filesystem as it was.

**The original plan — "the rename is both the atomic step and the existence check" — is wrong,
and it was wrong in the everyday case, not an edge case.** Verified locally on ext4: `os.Rename`
onto an existing directory returns `file exists` whether that directory is empty or non-empty.
Go's `os.Rename` performs an `Lstat` on the target and returns `EEXIST` for *any* existing
directory without ever calling `rename(2)` (`$(go env GOROOT)/src/os/file_unix.go`), even though
raw POSIX `rename(2)` permits replacing an empty directory. `nooma init` against a pre-existing
*empty* directory is exactly R7.1's scenario ("GIVEN an empty directory / WHEN `nooma init` runs
against it / THEN a vault exists") and exactly what `t.TempDir()` hands the L4 test — so the naive
mechanism would have failed the primary happy-path test on day one.

The corrected mechanism:

1. build the complete vault in `<target>.tmp-<n>`, a sibling of the target (not in `$TMPDIR`, so
   the eventual placement stays within one filesystem)
2. if the target does not exist → `os.Rename(tmp, target)`
3. if the target exists and is **empty** → `os.Remove(target)` (which succeeds only when the
   directory is empty), then `os.Rename(tmp, target)`. The window between the two calls is safe,
   not just small: if a concurrent `init` creates something at `target` in that window, the rename
   fails rather than clobbering it — the race resolves to "lost the race", never to data loss
4. if the target exists and is **non-empty** → refuse without touching it, satisfying R7.3
5. on any failure at any step, remove the temporary directory (R7.4)

`os.Rename` is therefore not a single atomic check-and-act step; it is an `Lstat` guard followed
by a kernel-atomic rename. The loser of a concurrent-`init` race may see `EEXIST` (target reappeared
between steps 2/3's check and the rename) or `ENOTEMPTY` (target was non-empty by the time the
kernel looked). Both mean "lost the race / a vault already exists here", and the caller treats them
identically — neither is special-cased into a different message.

### D13 — A new ADR sets the cross-compile matrix at six targets, superseding ADR-0001's criterion 5

Verified: `.github/workflows/main.yml` has exactly 4 matrix entries (`linux/amd64`, `linux/arm64`,
`darwin/arm64`, `windows/amd64`). `docs/adr/0001-sqlite-driver.md:47` lists those same 4 as
acceptance criterion 5, while line 106 of the *same file* reports "**PASS** — 6/6 targets" for
that criterion — an inconsistency inside one `Accepted` ADR, not between the ADR and the code.

`ADR-0001` is `Accepted` and is never edited (`CLAUDE.md` non-negotiable #2). This is an owner
decision, already taken: a new ADR (next free number **0013**, per `docs/adr/`'s current
contents) supersedes ADR-0001's acceptance criterion 5, sets the matrix at six targets (`linux`,
`darwin`, `windows` × `amd64`, `arm64`), and records the line-47-versus-line-106 inconsistency as
the reason a new ADR is needed rather than a silent correction of the old one. Writing that ADR is
part of the PR chain (proposal.md §5), not this design.

Reasoning for six, not four: M0's own demo claims "Linux/macOS/Windows/ARM" (proposal.md §2);
`darwin/amd64` is Intel Macs, still a real target this project claims to support, and
`windows/arm64` is real hardware (Windows on ARM devices ship today) — neither is a target this
project should silently drop. Cross-compilation is build-only and cheap (no C toolchain, per
ADR-0001's own criterion 5), so the two extra targets cost a few seconds of CI time, not a new
dependency. The spike's own results line already reported 6/6 — the new ADR is catching the
acceptance criterion up to what was already measured and true, not inventing new scope.

### D14 — The store-API golden is blind to exported `var` and `const`

Verified: `test/conformance/store_api_test.go:158-160` — `case *ast.GenDecl: if d.Tok != token.TYPE
{ return nil }`. Exported `var` and `const` declarations are silently dropped from the golden;
only funcs, methods and types are captured. Spec R8.5 and R13.5 lean on this golden as the
guarantee that widening the store surface produces a reviewable diff, but the lock (D4) needs an
exported sentinel error — `ErrVaultInUse`, so `cmd/nooma` can distinguish "held" from other I/O
failures — and a sentinel error is a `var`. As written, the golden would not see it appear: a gate
that only appears to guarantee what it claims.

The remedy: extend `renderExportedDecl` to render exported `var` and `const` declarations, and
regenerate `testdata/schema/store_api.golden`, in the same PR that adds the lock (proposal.md §5,
PR 6). `ErrVaultInUse`'s addition is then visible in that PR's golden diff like every other
surface widening, closing the blind spot at the exact moment it would otherwise have gone
unnoticed.

### D15 — `init`'s non-interactive path and its wizard share one input struct

R7.2 requires the interactive wizard to be a thin layer over the non-interactive path, verified by
an L1 test asserting the wizard collects "the same input type the non-interactive path takes." The
structure that makes that true: both paths funnel through one struct (`initInput` or equivalent) —
the non-interactive path builds it from flags, the wizard builds it from prompts — and vault
creation (D12's mechanism) accepts only that struct, never flags or prompts directly. Vault
creation therefore cannot observe which path produced its input, which is what makes "the wizard
cannot produce a vault the non-interactive path cannot" true by construction rather than by
convention. See §8's test matrix for the corresponding L1 row.

---

## 3. Package layout and dependency map

```
cmd/nooma/                        main, dispatch table, one file per command
  └── imports: internal/config, internal/store/{sqlite,vaultlock}, internal/httpapi

internal/config/                  NEW
  ├── config.go                   the schema types
  ├── load.go                     strict decode + validate
  ├── dotenv.go                   D2's strict subset parser
  ├── resolve.go                  D7's resolution over an injected environment
  └── imports: goccy/go-yaml, stdlib

internal/store/vaultlock/         NEW
  ├── lock.go, lock_unix.go, lock_windows.go
  └── imports: golang.org/x/sys/{unix,windows}, stdlib

internal/store/sqlite/            gains (*Vault).IntegrityCheck(ctx) error
internal/httpapi/                 NEW: a minimal mux, API hello + /ui placeholder
```

The new method is named `IntegrityCheck` explicitly, and not left implicit, because
`internal/store/sqlite/open.go:192` already defines `func (v *Vault) Check(ctx context.Context)
error` — an unrelated FTS5-registration probe, already present in
`testdata/schema/store_api.golden`. Naming the new method distinctly avoids both a collision and an
implementer wiring `doctor`'s `PRAGMA integrity_check` to the existing, unrelated `Check`.

Dependency rule check: nothing here imports `internal/core`, and `internal/core` gains no import.
`sqlite-containment` is satisfied — `database/sql` stays inside `internal/store/**`, which is why
`doctor`'s `integrity_check` must be a store method and not a query in `cmd/`. `forbidigo` scopes
to `internal/core/` only, so `os.Getenv` and `time.Now` are legal in these packages, and D7's
injection is for testability, not for lint.

Both new declarations under `internal/store/**` widen `testdata/schema/store_api.golden`, which
is regenerated in the PRs that add them (spec R8.5, R13.5) — see D14 for a blind spot in that
golden's coverage that the same PR must also close.

---

## 4. Configuration

The schema mirrors `docs/01-architecture.md` exactly, which is what makes the §6 gate possible:

```go
type Config struct {
    Server    Server                    `yaml:"server"`
    Database  Database                  `yaml:"database"`
    Providers map[string]Provider       `yaml:"providers"`
    Tasks     map[string]TaskBinding    `yaml:"tasks"`
    Channels  Channels                  `yaml:"channels"`
    Schedules Schedules                 `yaml:"schedules"`
}
```

Three notes on shapes that could have gone otherwise:

- `Providers` and `Tasks` are maps because the document's keys are user-chosen names
  (`claude_cloud`, `local_llama`) and task names (`chat`, `capture_processing`). A map means
  `Strict()` cannot police the *keys*, only each value's fields — so task-name validity is checked
  by M0's validator against the seven task names doc 01 lists, and provider `type` against the
  four documented types. Unknown names are errors, consistent with R3.2's spirit where the parser
  cannot reach.
- Secrets are `*_env` string fields holding variable names. There is no field anywhere that can
  hold a credential, which is what makes R4.1 structural rather than a review rule.
- Defaults are applied after decoding, not by pre-populating the struct, so "absent" and
  "explicitly set to the default" stay distinguishable — `status` can report which values the user
  chose.

Load order, fixed by R4.3: resolve the vault → read `<vault>/.env` without overwriting the process
environment → decode `<vault>/nooma.yml` strictly → apply defaults → validate → resolve
`database.path` to an absolute path inside the vault.

---

## 5. The lock, `serve`, and the read-only commands

```
serve:   resolve → load config → decideBinding (D11) → vaultlock.Acquire → sqlite.Open
         → listen → wait for signal → close → Release
status:  resolve → load config → vaultlock.ReadHolder → stat the db file → print
doctor:  resolve → load config (collecting errors) → run every check (D10) → print → exit code
```

`status` and `doctor` never call `Acquire`. That is the whole of R8.4, and it is why `ReadHolder`
is a separate function that takes no lock: an API where reading the holder required acquiring
anything would make the requirement impossible to satisfy without a comment asking people to be
careful.

`serve` releases the lock in a `defer` **and** on `SIGINT`/`SIGTERM` through a signal handler that
cancels the server's context. The kernel would release it anyway on exit (D4) — the explicit
release exists so that R8.1's L4 test observes a released lock rather than a released-by-luck one.

---

## 6. The config↔doc gate

An L2 test in `test/conformance/`:

1. read `docs/01-architecture.md`
2. extract the fenced `yaml` block inside §"Configuration — `nooma.yml`" using a **new**,
   section-scoped, language-tagged, exactly-one-or-error extractor
3. decode it into `config.Config` with the loader's own `Strict()` options
4. compare the struct's field-name schema, obtained by **reflection over the `yaml` struct
   tags**, against the set of keys present in the decoded document

**Correction on step 2's citation.** An earlier draft of this design cited
`test/support/schema/markdown.go`'s extractor as already satisfying R9.2. That is wrong on every
clause: that file's extractor is hardcoded to ```` ```sql ```` fences (`fenceStartPattern`,
`markdown.go:19`), is deliberately built to *collect every* such fence across the whole document
(doc 03 legitimately has many `CREATE` blocks), and has no section scoping and no arity check —
the opposite of what R9.2 requires. The real "exactly one, else error" precedent in this codebase
is a different file in a different package: `test/support/goldenset/markdown.go`'s
`ExtractJSONFence`, which returns exactly this shape of error (0 fences, or more than 1, both
named) but is JSON-specific and also has no section scoping. Neither extractor can be reused as
written. This step is **new code**, modeled on `ExtractJSONFence`'s "exactly one or error" shape
extended with section scoping and a `yaml` tag instead of JSON, not on the SQL collector's
whole-document scan. Without the section scope, the extractor would currently pass by luck: doc 01
happens to contain exactly one ```` ```yaml ```` fence today, so a whole-document scan finds one
and passes — until the day a second ```` ```yaml ```` fence appears anywhere in the document, at
which point the scan silently picks whichever one comes first. Proposal.md PR 4's line estimate is
re-budgeted upward accordingly (§5), since this is new extractor code, not reuse.

**Correction on step 4's mechanism.** An earlier draft compared key sets by re-encoding the
*decoded value* and comparing the resulting YAML's keys. That comparison is vacuous the moment any
field carries `yaml:"...,omitempty"` — idiomatic Go, and one that R3.4/§5 explicitly invite for
several "legitimately optional" sections. A zero-valued `omitempty` field vanishes from a
value-driven re-encode, so an undocumented field with a zero value would pass the gate silently —
exactly the false assurance R9.1 exists to prevent. The gate instead walks `config.Config`'s Go
type via `reflect.Type` and its struct tags to obtain the field-name schema directly from the
type, independent of any particular value's zero-ness, and compares that schema's key paths
against the document's key paths.

The comparison is on key paths, not values, because the document's values are illustrative
(`llama3.1:70b`, `123456789`) and pinning them would make the gate fail on every example edit —
teaching contributors to weaken it, which is the failure mode `docs/06-harness.md` §4 warns about.

The document's example must therefore be *complete*: every field the struct has appears in it.
That is a constraint on doc 01, and it is the point.

**The gate decodes and compares schemas; it MUST NOT validate.** `docs/01-architecture.md`'s
`tasks` block contains the literal placeholder `embedding: { provider: ... }`, and `...` decodes
cleanly to the Go string `"..."` under `Strict()` — the documented example decodes without error,
which is all this gate checks. It would fail any validator that checked a task's `provider` value
against the declared `providers:` map, because `...` is not a real provider name. That failure
would be the gate wrongly blaming doc 01 for not being a validator. The first person to "improve"
this gate by adding value validation breaks it; this note exists so that improvement gets rejected
in review instead of merged.

---

## 7. CI changes

| Job | Today | After |
|---|---|---|
| e2e (L4) | `push: main` | `pull_request` + `push: main`, matrix `ubuntu-latest` + `windows-latest` (D6) |
| cross-compile | `push: main`, 4 targets (`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`) | `pull_request` + `push: main`, **6 targets** — gains `darwin/amd64` and `windows/arm64` (D13) |
| `integration` (L3) | `push`/`pull_request`, `ubuntu-latest` only | same triggers, matrix `ubuntu-latest` + `windows-latest` (D6), so R8.2/R8.3 actually run `LockFileEx` |

The cross-compile matrix's expansion to 6 targets is not a pre-existing plan being restated: today
it is 4 targets, verified against `.github/workflows/main.yml`, and D13 records the new ADR that
authorizes the expansion (ADR-0001's acceptance criterion 5 said 4; its own results table on the
same page already reported 6/6 — see §1's ground-truth table). This PR moves the trigger **and**
expands the matrix **and** adds the new ADR, all together, because the matrix count and the ADR
that governs it cannot be split across PRs without one contradicting the other in the interim.

`make cross-compile` is added as a Makefile target covering the same six `GOOS`/`GOARCH` pairs
(`GOOS=x GOARCH=y go build ./...`, no PR metadata required) and is added to `check-all`, per
`CLAUDE.md`'s Workflow section: "if you add a blocking CI job, add it to `check-all` too — unless
it needs PR metadata a Makefile cannot produce." Cross-compilation needs none, unlike e2e (which
needs `test-e2e`, already excluded from `check-all` for the same documented reason) and unlike
`docs-sync.yml` (which needs PR labels).

The `main.yml` header comment, which currently explains why these two live outside `ci.yml`, gets
rewritten rather than left contradicting the triggers below it. **`ci.yml`'s own comment** (around
line 124, "cross-compilation matrix -> main.yml, on push to main only") goes stale for the same
reason and is corrected in the same PR — it was found stale by the same review that caught the
matrix-count and the `make cross-compile` gaps, not a separate follow-up. `CLAUDE.md`'s Workflow
section and the `Makefile` header both describe which gates `check-all` covers; `make test-e2e`
already exists and `check-all` does not include it. That stays true — e2e now blocks the PR through
CI, and `check-all` keeps its documented meaning of "every gate CI blocks on that a Makefile can
run locally" only if e2e is added. It is added, and all three comments (`main.yml`'s header,
`ci.yml`'s line-124 comment, and `check-all`'s own doc comments) are updated in the same PR.

The ruleset gains the two PR-blocking contexts (spec R2.2), which is a GitHub-side change, not a
repository one.

---

## 8. Test matrix

| Requirement group | Level | Where | Note |
|---|---|---|---|
| Config decode, unknown/duplicate/type errors (§3) | L1 | `internal/config/` | table-driven, one case per nesting level |
| `.env` subset and precedence (§4) | L1 | `internal/config/` | includes malformed-line rejection |
| Validation, aggregate errors (§5) | L1 | `internal/config/` | |
| Vault resolution: four steps, the two cwd sub-steps, three candidate counts, relative vs absolute argument (§6) | L1 | `internal/config/` | possible only because of D7 |
| Loopback truth table (R11.3) | L1 | `internal/config/` | includes `127.0.0.1.evil`, `0127.0.0.1` |
| Binding refusal decision (R11.2) | L1 | `internal/httpapi/` | pure function, D11 |
| Config↔doc gate (§9) | L2 | `test/conformance/` | untagged, runs in `make test` |
| `init`'s wizard collects the same input struct as the non-interactive path (R7.2) | L1 | `cmd/nooma/` | asserts on the shared struct, per D15 |
| Lock contention, real second process (R8.2) | L3 | `test/integration/` | precedent: `migrate_race_integration_test.go`; runs on Linux and Windows (D6) |
| Lock survives `SIGKILL` (R8.3) | L3 | `test/integration/` | runs on Linux and Windows (D6) |
| `integrity_check` (R13.1) | L3 | `test/integration/` | |
| `init` completeness, migration version (R7.1) | L3 + L4 | both | |
| Every command's contract (§10-§13) | L4 | `test/e2e/` | runs on Linux and Windows (D6) — excludes the lock's own contention/crash tests, which stay at L3 |
| No domain symbol created (R14.1) | gate | `make pending-red` | must stay green untouched |

`serve`'s L4 tests bind an ephemeral loopback port. `docs/06-harness.md` §3's "no test touches the
network" means no external service; a loopback listener in-process is not that, and the test files
say so where a future reader would otherwise wonder.

---

## 9. Risks this design accepts

| # | Risk | Position |
|---|---|---|
| 1 | `flock` and `LockFileEx` are unreliable over NFS and SMB; the same caveat applies to a torn read of the PID region, since `ReadHolder` takes no lock | Accepted and recorded. M0's single-writer guarantee holds for a vault on a local filesystem. A vault on a network share is outside the guarantee; a `doctor` check that detects a network filesystem is a candidate for a later milestone, not M0. The torn-read case is bounded the same way `ReadHolder` bounds the lock itself: the PID is written in a single small write (D4), so on any filesystem where that write is atomic — every local filesystem this guarantee targets — there is nothing to tear. |
| 2 | D4's byte-offset scheme is only truly exercised on Windows | This is exactly why D6 adds a Windows e2e runner **and** a Windows leg to the L3 `integration` job — the lock's actual contention (R8.2) and crash-recovery (R8.3) tests are L3, not L4, so the e2e runner alone would not have exercised them. Without both, the scheme would be a compiling assumption on the platform it most needs verifying. |
| 3 | `goccy` is a 14.5k-line dependency in a project that had one dependency | Accepted for D1's reasons. It is dependency-free and released this year; the alternative has not shipped since 2022-05-27. |
| 4 | The schema decodes `providers` and `tasks` whose semantics arrive in M1 | Shape-checked, never interpreted. The §6 gate makes any later change to those shapes visible as one diff against doc 01. |
| 5 | Map-keyed sections escape `Strict()`'s unknown-key checking | Compensated by validating task and provider-type names against doc 01's lists. A new task name is therefore a two-place change — struct-adjacent validator plus doc — which is the same coupling the §6 gate enforces everywhere else. |
| 6 | `localhost` is special-cased without resolution (D9) | Accepted. The alternative is a DNS lookup inside a security decision, at startup, untestable offline. The fail-safe direction (unknown host ⇒ exposed) bounds the damage. |

---

## 10. What this design does not decide

- The HTTP API's shape beyond a hello endpoint (M1).
- The `/ui` page's content beyond a placeholder (M4), and ADR-0007's session-cookie handshake,
  which is M4's half of that decision.
- Whether `nooma.yml` gains a `vault` display name. Doc 01 uses `pablo.nooma` as the vault
  directory name and never names the vault inside the config; M0 does not invent one.
- `nooma export`'s `VACUUM INTO` and the `units_fts` rowid exposure — a pending ADR, and `export`
  is not an M0 command.
