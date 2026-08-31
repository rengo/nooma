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
| The cross-compile matrix has 4 entries today, not 7 (the new target count) | `.github/workflows/main.yml` — exactly `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64` |
| ADR-0001 contradicts itself on the matrix size, and its "PASS" line does not name the 6 | `docs/adr/0001-sqlite-driver.md:47` lists 4 targets as acceptance criterion 5; line 106 of the same file reports "**PASS** — 6/6 targets" without naming which six |
| The spike branch's own results name six targets, and `windows/arm64` is not one of them | `git show spike/adr-0001-sqlite-driver:spike/RESULTS.md` — `linux/amd64 OK`, `linux/arm64 OK`, `linux/arm OK`, `darwin/amd64 OK`, `darwin/arm64 OK`, `windows/amd64 OK`; `windows/arm64` does not appear |
| All seven candidate targets (the cartesian six plus `linux/arm`) build on this checkout | `GOOS=… GOARCH=… go build ./...` run locally for each of `linux/amd64`, `linux/arm64`, `linux/arm`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64` — all seven succeed |
| `docs/01-architecture.md`'s four `providers:` entries use disjoint field subsets; no entry uses every `Provider` field | reading the document: `claude_cloud`/`claude_haiku` use `type`, `api_key_env`, `model`; `local_llama` uses `type`, `endpoint`, `model`; `whisper_local` uses `type`, `binary_path`, `model_path` |
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

- the PID region is the first 1024 bytes of the file
- the lock is taken on the single byte at **offset 1024**, beyond that region and legally beyond
  EOF on Windows
- `ReadHolder` reads the first 1024 bytes, parses only up to the first NUL (or a fixed
  terminator), and takes no lock at all

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
2. on success: allocate a fresh 1024-byte buffer, write the current PID's decimal digits followed
   by a NUL terminator into its front, and write **the whole 1024 bytes** in one `WriteAt(buf, 0)`
   call.

   The buffer is the full region, not a short prefix, and that is the point. A freshly allocated Go
   byte slice is zero-initialised, so every byte past the terminator is written as zero and no byte
   of a previous holder's PID can survive — `"7"` replacing `"123456"` leaves nothing behind. There
   is no separate zeroing or truncation pass because the single full-width write *is* the zeroing.

   (An earlier draft of this decision described the same write two incompatible ways — "the whole
   buffer" and, in the next sentence, stale bytes from the previous holder surviving past the
   terminator. Both cannot be true: a full-width write of a zero-initialised buffer leaves no stale
   bytes, and stale bytes only survive a *short* write. The full-width form is chosen because it is
   simpler and strictly safer, and because §8's regression test needs one unambiguous invariant to
   assert.)

   Collapsing this to one write closes a real gap the earlier two-step form had: `ReadHolder` takes
   no lock and runs at any instant, so a reader landing *between* a zeroing pass and the PID write
   would have observed a complete, self-consistent, wrong answer — "no holder" — while the lock was
   genuinely held, breaking R8.4 in ordinary use, not a contrived race. With one write, that
   intermediate state does not exist to be observed.

   `ReadHolder` still stops at the first NUL rather than trusting the region to be clean. That is
   defensive, not required by the above: it costs one loop and it means a truncated or
   foreign-written lock file degrades to "no holder" instead of to a garbage PID.
3. on failure (lock already held): read the existing, untouched PID region and return it as the
   holder — nothing in the file is written, because nothing about the losing process is true yet

The PID region is cleared on release, after the lock is dropped. A stale PID with no lock held is
not authoritative and is reported as such — the lock, not the file's existence, is the truth. The
"acquire, then write" ordering is the whole reason a losing `Acquire` never corrupts the winner's
PID.

The PID region is written by exactly one `WriteAt` of exactly 1024 bytes, because `ReadHolder` takes
no lock and could otherwise observe either an intermediate state between two writes or a torn write
on a filesystem where a small write is not atomic — see risk #1. An L3 test reads the PID region
concurrently with `Acquire` specifically to catch a regression back to a two-write form, and asserts
that every read returns either the empty holder or the complete winning PID, never a partial one
(§8).

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

**The upward search (R6.1) needs no new port member.** Walking from the cwd to the filesystem root
is pure string manipulation over the value `getwd()` already returns: each step computes
`filepath.Dir(dir)` and calls `readDir` on it, stopping when `filepath.Dir(dir) == dir` — the
portable, string-only signal that the root has been reached on both Unix (`/`) and Windows
(`C:\`). No new injected function is needed; the ascent is built entirely from `getwd` and
`readDir`, called repeatedly.

**There is deliberately no `executable` member.** `docs/01-architecture.md`'s original step 3 was
"vault next to the executable"; R6.6 removes it and states the three reasons. The one that belongs
here, because it is a fact about the standard library rather than about convention: `os.Executable`
returns the *resolved* path, so a symlinked install (`~/.local/bin/nooma -> /opt/nooma/nooma`)
would search `/opt/nooma/` — verified by probe, not assumed. A resolution step whose search
directory the user cannot predict from the command they typed is a step that will one day open the
wrong brain. The cwd is predictable by construction: the user is standing in it.

### D8 — "Is a vault" means "the directory contains a `nooma.yml` entry"; the partial-vault diagnostic probes only the default `./nooma.db`

`nooma.db`'s location is configurable (`database.path`), so it cannot be the marker — a vault
whose database sits in a subdirectory is still a vault. The config file is at a fixed path by the
layout decision, which makes it the only stable marker.

The predicate is existence, not readability: D7's injected `environment` exposes only `getwd`,
`getenv`, `homeDir` and `readDir` — a directory listing, which can prove `nooma.yml` is present or
absent but cannot exercise a permission-denied file at L1. Wording the predicate as "readable"
would state a property nothing in this design tests. A `nooma.yml` that exists but cannot be
opened surfaces instead as a config-load error (§3), which already has its own tests and error
path, not as a resolution-time distinction.

Consequence, and it is the desirable one: a directory with a `nooma.yml` and no database is a
*vault with a problem*, which `doctor` reports precisely, rather than "not a vault", which sends
the user hunting for the wrong thing.

**The partial-vault diagnostic (R6.5) has a narrower scope than the predicate.** R6.5 requires a
partial vault — its own example: `nooma.db` present, `nooma.yml` absent — to produce a specific
error naming what is missing. Finding the configured database requires reading `database.path`
from the very `nooma.yml` that is missing, which is circular, so the diagnostic probes exactly one
secondary artifact instead: the *default* path, `./nooma.db`, relative to the directory being
tested. If that default file is present alongside a missing `nooma.yml`, the error names `nooma.db`
as found and `nooma.yml` as missing. A vault whose `database.path` was customised away from the
default trades this specific diagnostic away — the directory is reported simply as not-a-vault
(missing `nooma.yml`), with no comment on a database resolution has no way to find without the
config it is missing. This is recorded as an acknowledged limitation, not fixed: an honest
limitation beats a requirement that cannot be met without reading the file that does not exist.

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
`<target>.tmp-<pid>-<rand>`, builds the complete vault inside it — migrations included — and only
then places it at the target. A failure at any point removes the temporary directory and leaves
the filesystem as it was.

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

1. build the complete vault in `<target>.tmp-<pid>-<rand>`, a sibling of the target (not in
   `$TMPDIR`, so the eventual placement stays within one filesystem). The suffix is the process PID
   **and** a random component, not the PID alone — a PID-only suffix is not collision-resistant
   (a recycled PID, or two invocations sharing one by coincidence, would let two racing `init`s
   build into the same temporary directory and corrupt each other's partial vault before either
   reaches the rename step).
2. `Lstat` the target first, before anything else touches it. If it exists and is **not a plain
   directory** — a regular file, a symlink to a file, a symlink to a directory, or any other
   non-directory type — refuse immediately, naming what exists there, without calling
   `os.ReadDir`, `os.Remove`, or `os.Rename` on it. This guard runs before the empty/non-empty
   branch below, so neither a plain file nor a symlink ever reaches it. Two failure modes this
   closes, both probed directly:
   - a plain file: `os.ReadDir` on a regular file returns an error ("not a directory"), not an
     error-free empty listing. An emptiness check written as `len(entries) == 0` without also
     requiring `err == nil` would misclassify an arbitrary stray file as "empty", and `os.Remove`
     deletes plain files — so `touch pablo.nooma` followed by `nooma init pablo.nooma` would
     **delete that file** and replace it with a vault, violating non-negotiable #6 one level up, at
     the vault's own root path.
   - a symlink to an empty directory: `os.ReadDir` follows the link and reports the target empty,
     while `os.Remove` unlinks only the symlink — the check and the mutation would act on two
     different objects, orphaning the real directory the symlink pointed to. `Lstat` (which does
     not follow the link) sees a symlink, not a directory, and refuses before either call runs.
3. if the target does not exist → `os.Rename(tmp, target)`
4. if the target exists, is a plain directory (per step 2's guard), and is **empty** →
   `os.Remove(target)` (which succeeds only when the directory is empty), then
   `os.Rename(tmp, target)`. Two race windows here are both safe, not just small, and both because
   the kernel re-checks the precondition rather than trusting the earlier check:
   - between the emptiness check (`os.ReadDir` returning zero entries) and the `os.Remove` call: if
     a concurrent process populates the directory in that window, `os.Remove` requires the
     directory be empty *at the moment it runs* and fails loudly instead of silently deleting
     whatever arrived between the check and the call
   - between `os.Remove` and `os.Rename`: if a concurrent `init` creates something at `target` in
     that window, the rename fails rather than clobbering it — the race resolves to "lost the
     race", never to data loss
5. if the target exists, is a plain directory, and is **non-empty** → refuse without touching it,
   satisfying R7.3
6. on any failure at any step, remove the temporary directory (R7.4)

`os.Rename` is therefore not a single atomic check-and-act step; it is an `Lstat` guard (step 2),
followed by an existence/emptiness check (steps 3/4), followed by a kernel-atomic rename. The
loser of a concurrent-`init` race may see `EEXIST` (target reappeared between the check and the
rename) or `ENOTEMPTY` (target was non-empty by the time the kernel looked). Both mean "lost the
race / a vault already exists here", and the caller treats them identically — neither is
special-cased into a different message.

### D13 — A new ADR sets the cross-compile matrix at **seven** targets, superseding ADR-0001's criterion 5

Verified: `.github/workflows/main.yml` has exactly 4 matrix entries (`linux/amd64`, `linux/arm64`,
`darwin/arm64`, `windows/amd64`). `docs/adr/0001-sqlite-driver.md:47` lists those same 4 as
acceptance criterion 5, while line 106 of the *same file* reports "**PASS** — 6/6 targets" for
that criterion — an inconsistency inside one `Accepted` ADR, not between the ADR and the code, and
the "PASS" line does not name which six.

The spike branch's own results file does name them: `git show
spike/adr-0001-sqlite-driver:spike/RESULTS.md` reports `linux/amd64 OK`, `linux/arm64 OK`,
`darwin/amd64 OK`, `darwin/arm64 OK`, `windows/amd64 OK`, `linux/arm OK` — six targets, and
`windows/arm64` is not among them. An earlier draft of this design proposed a "cartesian six"
(`linux`/`darwin`/`windows` × `amd64`/`arm64`) on the assumption that the ADR's unnamed "6/6" meant
that set. It does not: the spike tested `linux/arm` (32-bit), not `windows/arm64`, so the
cartesian-six proposal silently dropped the one target the spike actually verified while inventing
one it had never touched — the exact defect family this design fights everywhere else, now found
inside its own reasoning.

**Owner decision**: the matrix is **seven targets** — the cartesian six plus `linux/arm`. All seven
were verified by direct local measurement on this checkout (`GOOS=… GOARCH=… go build ./...` per
target, not inferred from the ADR's unnamed count): `linux/amd64`, `linux/arm64`, `linux/arm`,
`darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64` all build (§1's ground-truth
table).

`ADR-0001` is `Accepted` and is never edited (`CLAUDE.md` non-negotiable #2). A new ADR (next free
number **0013**, per `docs/adr/`'s current contents) supersedes ADR-0001's acceptance criterion 5,
sets the matrix at seven targets, and records: the line-47-versus-line-106 inconsistency as the
reason a new ADR is needed rather than a silent correction of the old one; that `windows/arm64` is
newly verified by this design's own local measurement, not carried over from the ADR's unnamed
"6/6"; and that `linux/arm` is retained because the spike actually verified it, and dropping a
verified target silently is the exact failure family this project keeps fighting elsewhere in this
design (D2, R6.2). Writing that ADR is part of the PR chain (proposal.md §5), not this design.

Reasoning for seven: M0's own demo claims "Linux/macOS/Windows/ARM" (proposal.md §2) without
specifying bitness, and a 32-bit Raspberry Pi is squarely the hardware that claim points at —
dropping `linux/arm` would silently narrow "ARM" to 64-bit only. `darwin/amd64` is Intel Macs,
still a real target this project claims to support, and `windows/arm64` is real hardware
(Windows-on-ARM devices ship today) — neither is a target this project should silently drop.
Cross-compilation is build-only and cheap (no C toolchain, per ADR-0001's own criterion 5), so the
three extra targets over today's four cost a few seconds of CI time, not a new dependency.

### D14 — The store-API golden is blind to exported `var` and `const`, and fixing it surfaces a second, pre-existing symbol

Verified: `test/conformance/store_api_test.go:158-160` — `case *ast.GenDecl: if d.Tok != token.TYPE
{ return nil }`. Exported `var` and `const` declarations are silently dropped from the golden;
only funcs, methods and types are captured. Spec R8.5 and R13.5 lean on this golden as the
guarantee that widening the store surface produces a reviewable diff, but the lock (D4) needs an
exported sentinel error — `ErrVaultInUse`, so `cmd/nooma` can distinguish "held" from other I/O
failures — and a sentinel error is a `var`. As written, the golden would not see it appear: a gate
that only appears to guarantee what it claims.

**The remedy cannot land bundled with the lock, because widening the renderer surfaces more than
the lock adds.** `internal/store/sqlite/dsn.go:15` already declares `var ErrRelativeDBPath` —
exported, predating this change, and invisible to the golden today for exactly the same reason.
The moment `renderExportedDecl` is widened to render `var`/`const`, regenerating the golden
surfaces **both** `ErrRelativeDBPath` and `ErrVaultInUse` in the same diff, and a reviewer looking
at "the lock's PR" would be asked to review a pre-existing symbol they did not touch, mixed
together with the one that is actually new — the opposite of the reviewable, single-purpose diff
this golden exists to produce.

So the renderer fix is split into its own PR, ahead of the lock: proposal.md §5, PR 6
(`fix/store-golden-var-const`), extends `renderExportedDecl` to render exported `var` and `const`
declarations and regenerates `testdata/schema/store_api.golden`. That PR's diff contains exactly
one thing — `ErrRelativeDBPath` appearing — reviewed there as the pre-existing symbol it is, with
nothing else bundled in. Only after PR 6 merges does PR 7 (`feat/vault-lock`) add `ErrVaultInUse`
and regenerate the golden again; because the renderer is already widened by then, that second
regeneration's diff contains only `ErrVaultInUse` — the property spec R8.5 actually needs, and
which one combined PR could not have produced.

### D15 — `init`'s non-interactive path and its wizard share one input struct

R7.2 requires the interactive wizard to be a thin layer over the non-interactive path, verified by
an L1 test asserting the wizard collects "the same input type the non-interactive path takes." The
structure that makes that true: both paths funnel through one struct (`initInput` or equivalent) —
the non-interactive path builds it from flags, the wizard builds it from prompts — and vault
creation (D12's mechanism) accepts only that struct, never flags or prompts directly. Vault
creation therefore cannot observe which path produced its input, which is what makes "the wizard
cannot produce a vault the non-interactive path cannot" true by construction rather than by
convention. See §8's test matrix for the corresponding L1 row.

### D16 — `os.Executable` is banned from `internal/config` by a tree-scan conformance test, **not** by `forbidigo`

R6.6's `Verified by` originally rested on "the absence of any `os.Executable` call in
`internal/config`" as unenforced prose: a behavioral test proves only that today's resolution logic
ignores the executable's directory in one scenario — it cannot prove no such call exists anywhere in
the package, so a future contributor adding an unrelated diagnostic could violate R6.6's letter with
nothing catching it. Per `CLAUDE.md`: "If a rule can be an automated gate, it is a gate — not a
skill." So it needs a gate. The question is which.

**The `forbidigo` route was tried, measured, and rejected.** An earlier draft of this decision
specified "a second, additive `forbidigo` pattern scoped to `internal/config/`, alongside the
existing `internal/core/` rule", asserting the two scopes were independent. That mechanism does not
exist, and the form described is actively destructive. Two facts, both measured against this repo's
pinned `golangci-lint v2.12.2`:

1. `forbidigo`'s settings schema has **no per-pattern `path` field** — only `pattern`, `pkg`, `msg`.
   A pattern cannot carry its own directory scope. Scoping exists only through
   `exclusions.rules`, which excludes *an entire linter's output* for a path, not one pattern's.
2. **`exclusions.rules` entries OR together.** With the existing rule (`linters: [forbidigo]`,
   `path-except: internal/core/`) plus a new one (`path-except: internal/config/`), a violation in
   either directory is excluded by the *other* rule, so neither is reported:

   ```
   two exclusion rules → 0 issues
     os.Getenv     in internal/core   → NOT reported   ← the gate that already worked
     os.Executable in internal/config → NOT reported
   control, today's single rule → 1 issue, os.Getenv correctly caught
   ```

Shipping that would have silently disabled the `core-purity` clock/environment enforcement that has
worked since the first PR — `CLAUDE.md` non-negotiable #3 — while appearing to add a gate. A
configuration that *does* work exists (a `text:` filter on every exclusion rule, matching each
pattern's `msg`), but it is rejected too: it requires rewriting the existing `internal/core` rule,
and it leaves the same trap latent, because a future `forbidigo` pattern added without its own
`text:` entry is silently excluded everywhere. A mechanism whose failure mode is "the gate quietly
stops gating" is the wrong mechanism for a project with eleven recorded instances of exactly that
defect.

**The decision: a tree-scan conformance test in the untagged L2 suite.** It parses every non-test
`.go` file under `internal/config/` and fails if any of them references `os.Executable`, naming the
file and line. This project already has the pattern —
`test/conformance/i01_focus_never_persisted_test.go` scans the tree for a forbidden literal, and
`docs/06-harness.md` §4 calls such tests "ugly and worth gold" for precisely this case. Three
properties decide it:

- It cannot silently stop gating. A missing scan is a visibly absent test, not a passing gate.
- It runs in `make test` and CI's test job with no new job, no new tag, and no lint-config surgery.
- Per D10's non-empty-corpus rule, it asserts it actually found `.go` files before asserting the
  property, so it cannot pass vacuously if the package is renamed or the walk breaks.

Recorded as part of PR 5 (`feat/vault-resolution`, proposal.md §5), the PR that introduces
`internal/config`'s resolution code. `.golangci.yml` is **not** modified by this change.

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
`doctor`'s `integrity_check` must be a store method and not a query in `cmd/`. `forbidigo`'s
existing rule scopes `os.Getenv`/`time.Now` to `internal/core/` only, so those calls are legal in
these packages, and D7's injection is for testability, not for lint. `.golangci.yml` is not modified
by this change at all: D16 bans `os.Executable` from `internal/config/` with a tree-scan conformance
test, because a second `forbidigo` exclusion rule was measured to disable the existing
`internal/core/` one rather than sit beside it.

Both new declarations under `internal/store/**` widen `testdata/schema/store_api.golden`, which
is regenerated in the PRs that add them (spec R8.5, R13.5) — see D14 for a blind spot in that
golden's coverage, closed by a dedicated PR that precedes the lock PR (proposal.md §5, PR 6)
rather than bundled into it.

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
  three documented types — doc 01 has four provider *entries* but three distinct `type` values,
  because `anthropic` appears twice. Unknown names are errors, consistent with R3.2's spirit where the parser
  cannot reach. The same map-ness affects the §6 gate's schema comparison, handled there rather
  than here — see §6.
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

**Map-typed fields: `providers` and `tasks`.** `Config.Providers` and `Config.Tasks` are
`map[string]Provider` / `map[string]TaskBinding`, and the map's own keys (`claude_cloud`,
`local_llama`, `chat`, ...) are user data, not schema — reflecting over a `map[string]X` does not
enumerate field names the way reflecting over a struct does. Treating a map as opaque here is the
easy path and the wrong one: it would make the gate silently stop checking anything inside
`providers.*`/`tasks.*`, the same "silently stops checking part of its input" defect family found
nine times during `complete-harness`, now inside the gate's own input filter. So for a map-typed
field, step 4's schema side recurses into the map's *value* type (`Provider`, `TaskBinding`) to
obtain that type's field names — the map's own keys are never compared against anything. On the
document side, the gate **unions** the field names observed across every entry present under that
map section before comparing: doc 01's `providers:` block has four entries with disjoint field
subsets (`claude_cloud`/`claude_haiku`: `type`, `api_key_env`, `model`; `local_llama`: `type`,
`endpoint`, `model`; `whisper_local`: `type`, `binary_path`, `model_path`), and no single entry uses
every `Provider` field — a per-entry completeness check is unsatisfiable on this document, or any
realistic one, since different provider types legitimately need different fields. The completeness
rule is therefore "the union across all entries of a map section", never "every entry
individually". A dedicated test case is built from this exact `providers:` block (spec R9.1).

The comparison is on key paths, not values, because the document's values are illustrative
(`llama3.1:70b`, `123456789`) and pinning them would make the gate fail on every example edit —
teaching contributors to weaken it, which is the failure mode `docs/06-harness.md` §4 warns about.

The document's example must therefore be *complete*: every field the struct has appears in it —
directly, for a struct-typed section, or via the union across its entries, for a map-typed section
(`providers`, `tasks`) per the note above. That is a constraint on doc 01, and it is the point.

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
| e2e (L4) | `push: main` | `pull_request` + `push: main`, matrix `ubuntu-latest` + `windows-latest` (D6); Windows leg installs `make` explicitly (D17) |
| cross-compile | `push: main`, 4 targets (`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`) | `pull_request` + `push: main`, **7 targets** — gains `darwin/amd64`, `windows/arm64` and `linux/arm` (D13) |
| `integration` (L3) | `push`/`pull_request`, `ubuntu-latest` only | same triggers, matrix `ubuntu-latest` + `windows-latest` (D6), so R8.2/R8.3 actually run `LockFileEx`; Windows leg installs `make` explicitly (D17) |

The cross-compile matrix's expansion to 7 targets is not a pre-existing plan being restated: today
it is 4 targets, verified against `.github/workflows/main.yml`, and D13 records the new ADR that
authorizes the expansion — the cartesian six plus `linux/arm`, all seven verified by direct local
measurement, not inferred from the ADR's unnamed "6/6" (see §1's ground-truth table). This PR moves
the trigger **and** expands the matrix **and** adds the new ADR, all together, because the matrix
count and the ADR that governs it cannot be split across PRs without one contradicting the other in
the interim.

`make cross-compile` is added as a Makefile target covering the same seven `GOOS`/`GOARCH` pairs
(`GOOS=x GOARCH=y go build ./...`, no PR metadata required) and is added to `check-all`, per
`CLAUDE.md`'s Workflow section: "if you add a blocking CI job, add it to `check-all` too — unless
it needs PR metadata a Makefile cannot produce."

**`make test-e2e` joins `check-all` in the same PR, and this needs stating because an earlier draft
of this section said both things.** It claimed e2e was "already excluded from `check-all` for the
same documented reason" and then, eleven lines later, that "it is added". Only one can be true, and
the exclusion story does not survive checking: `CLAUDE.md` names exactly one gate `check-all` cannot
cover, `docs-sync.yml`, and gives exactly one reason, PR metadata. No document in this repository —
not `CLAUDE.md`, not the `Makefile` header, not `docs/06-harness.md` — records any reason for
excluding e2e. It is absent from `check-all` today only because it was not a blocking gate today.

R2.1 makes it one. `make test-e2e` already exists, needs no PR metadata, and runs locally, so
`CLAUDE.md`'s rule applies to it literally: it joins `check-all`. The cost is real and accepted —
`check-all` now compiles the binary and runs L4, so it gets slower. That is what `check-all` is for.
`make check` stays the fast loop and is untouched.

`docs-sync.yml` remains the sole documented exception, because it genuinely needs a PR's base branch
and label list.

The `main.yml` header comment, which currently explains why these two live outside `ci.yml`, gets
rewritten rather than left contradicting the triggers below it. **`ci.yml`'s own comment** (around
line 124, "cross-compilation matrix -> main.yml, on push to main only") goes stale for the same
reason and is corrected in the same PR — it was found stale by the same review that caught the
matrix-count and the `make cross-compile` gaps, not a separate follow-up.

Two more stale claims live outside the workflows and are corrected in PR 1's docs sweep rather than
here, because they are prose in the docs the harness is described by:
`docs/06-harness.md` §6 states "what does **not** run on every PR: L4 (e2e), driver benchmarks, and
the cross-compilation matrix" — false after R2.1 — and the sentence beside it, "the full matrix
depends on ADR-0001 and cannot be designed until the spike closes", which stopped being true when
ADR-0001 closed two build-order steps ago. `docs/05-build-plan.md`'s M0 bullet still describes vault
resolution as "arg → env → portable → home", the executable-relative model R6.6 removes.

Four comments and doc passages are therefore updated across this change: `main.yml`'s header and
`ci.yml`'s line-124 comment (this PR), and doc 06 §6 plus doc 05's M0 bullet (PR 1). `CLAUDE.md`'s
Workflow section and the `Makefile` header, which both enumerate what `check-all` covers, are updated
in this PR too, since `check-all` gains two targets here.

**The ruleset gains every context each matrix leg posts, not one context per job (spec R2.2).**
`main.yml`'s cross-compile job templates its check name per matrix leg
(`name: cross-compile ${{ matrix.goos }}/${{ matrix.goarch }}`), so it posts one context per leg —
7, matching the matrix above — and e2e's `windows-latest` addition means it posts one context per
OS leg — 2. Registering only two job-level names when the workflows post 9 leg-specific names would
leave the un-registered legs' contexts absent from `required_status_checks` forever, which is not a
softer version of the gate — a required context that never posts never becomes satisfied, so it
**permanently blocks every future merge to `main`**, recoverable only by editing the ruleset. This
is a GitHub-side change, not a repository one, so it is applied directly, verified per R2.2's
`Verified by`.

### D17 — Windows CI legs install `make` explicitly, not by relying on what happens to be on PATH

D6 adds a `windows-latest` leg to both the e2e (`main.yml`) and `integration` (`ci.yml`) jobs, each
running `make test-e2e` / `make test-integration`. Verified against GitHub's `windows-latest`
runner image documentation: GNU Make is not in the installed-tools list and is not on PATH. MSYS2 is
present at `C:\msys64`, but the same documentation states it "is pre-installed on image but not
added to PATH" — and MSYS2's base install does not necessarily ship `make` in the first place, so
adding `usr\bin` to PATH is not a guaranteed fix, only a PATH hack that happens to work if some
other step already put `make` there. A step that runs by luck, in CI configuration rather than
application code, is this project's dominant defect family one layer up.

So each Windows leg gains one explicit step installing `make` via Chocolatey (already available on
the image) before its `make test-*` step runs. The tradeoff: an explicit install step costs a few
seconds of setup per leg, paid in exchange for both legs running the *identical* make target the
Linux legs run — preserving the CI/Makefile parity `CLAUDE.md`'s Workflow section already commits
`check-all` to. An explicit install is chosen over a PATH edit because a PATH edit depends on an
assumption about the image's contents this design has not verified, where an explicit install does
not. Recorded in PR 7 (`feat/vault-lock`, proposal.md §5) — the single PR that adds both jobs'
Windows legs together (D6), and therefore the one that adds both `make`-install steps. The
cross-compile job needs no such step: it cross-compiles from a single host runner
(`GOOS=x GOARCH=y go build ./...`, no C toolchain per ADR-0001 criterion 5) rather than running on
a native `windows-latest` runner, so it never invokes `make` on Windows in the first place.

---

## 8. Test matrix

| Requirement group | Level | Where | Note |
|---|---|---|---|
| Config decode, unknown/duplicate/type errors (§3) | L1 | `internal/config/` | table-driven, one case per nesting level |
| `.env` subset and precedence (§4) | L1 | `internal/config/` | includes malformed-line rejection |
| Validation, aggregate errors (§5) | L1 | `internal/config/` | |
| Vault resolution: four steps, the upward ascent's 3a/3b at each level, the `.nooma`-name exclusion, three candidate counts, relative vs absolute argument (§6) | L1 | `internal/config/` | possible only because of D7; includes a multi-level ascent and an unusable-candidate-not-silently-skipped case |
| Loopback truth table (R11.3) | L1 | `internal/config/` | includes `127.0.0.1.evil`, `0127.0.0.1` |
| Binding refusal decision (R11.2) | L1 | `internal/httpapi/` | pure function, D11 |
| Config↔doc gate, including the map-typed `providers`/`tasks` union case (§6, §9) | L2 | `test/conformance/` | untagged, runs in `make test`; uses doc 01's actual heterogeneous `providers:` block |
| `os.Executable` is referenced nowhere under `internal/config` (R6.6) | L2 | `test/conformance/` | tree scan per D16, **not** a lint rule; asserts a non-empty corpus first, per D10 |
| `init`'s target default and argument handling (R7.1b) | L4 | `test/e2e/` | four cases with `$HOME` pointed at a temp dir: bare `init` creates `~/.nooma/<user>.nooma` and prints its path, bare `init` refuses when a vault already exists there, relative argument, absolute argument |
| `init`'s wizard collects the same input struct as the non-interactive path (R7.2) | L1 | `cmd/nooma/` | asserts on the shared struct, per D15 |
| `init` refuses on a file or symlink target (R7.5) | L3 or L4 | `test/integration/` or `test/e2e/` | asserts the file/symlink is untouched; per D12's `Lstat` guard |
| Temp-dir name is collision-resistant across two racing `init`s (R7.6) | L1 | `cmd/nooma/` | asserts two generated names differ; per D12 |
| Lock contention, real second process (R8.2) | L3 | `test/integration/` | precedent: `migrate_race_integration_test.go`; runs on Linux and Windows (D6) |
| Lock survives `SIGKILL` (R8.3) | L3 | `test/integration/` | runs on Linux and Windows (D6) |
| PID region read concurrently with `Acquire` (R8.4) | L3 | `test/integration/` | regression guard against a two-write `ReadHolder` race, per D4 |
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
| 1 | `flock` and `LockFileEx` are unreliable over NFS and SMB; a torn read of the PID region is a separate, narrower risk since `ReadHolder` takes no lock | Accepted and recorded for the filesystem-unreliability half: M0's single-writer guarantee holds for a vault on a local filesystem, and a vault on a network share is outside it; a `doctor` check that detects a network filesystem is a candidate for a later milestone, not M0. For the torn-read half: D4 writes the entire PID region in a single `WriteAt` of a fixed-size, pre-built buffer — there is no separate zeroing pass, so there is no window in which a lock-free `ReadHolder` can observe an intermediate, self-consistent-but-wrong state ("no holder" while the lock is genuinely held). On any filesystem where that single write is atomic — every local filesystem this guarantee targets — there is nothing to tear; the L3 concurrent-read test (§8) exists to catch a regression to a two-write form, not to catch tearing itself, since a genuine tear on a non-atomic filesystem is outside this guarantee's scope, same as the network-filesystem risk above. |
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
