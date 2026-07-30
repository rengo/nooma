# ADR-0013 — The cross-compilation matrix: seven targets, on every PR

- **Status**: Accepted
- **Date**: 2026-07-30
- **Supersedes**: [ADR-0001](0001-sqlite-driver.md) acceptance criterion 5 only
- **Superseded by**: —
- **Enables**: M0

## Context

[ADR-0001](0001-sqlite-driver.md) accepted `ncruces/go-sqlite3` because it is pure Go/wasm and
therefore cross-compiles with no C toolchain. Criterion 5 was where that claim got tested. The ADR
records it twice, and the two records disagree:

| Where | What it says |
|---|---|
| Criterion table, row 5 | `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64` — **four** |
| Results table, row 5 | **PASS — 6/6 targets**, without naming which six |

`.github/workflows/main.yml` implements the four. The spike branch's own measurements name the six:

```
$ git show spike/adr-0001-sqlite-driver:spike/RESULTS.md
linux/amd64      OK   8.9M      darwin/amd64     OK   9.0M
linux/arm64      OK   8.6M      windows/amd64    OK   9.2M
darwin/arm64     OK   8.7M      linux/arm        OK   7.0M
```

So the spike built `linux/arm` — 32-bit ARM — and **never built `windows/arm64`**. Criterion 5's
four-target list is a subset of what was measured, and the "6/6" is accurate about the count while
naming nothing.

Two things force a decision now rather than later. M0's demo criterion is
`nooma init && nooma serve` on **Linux/macOS/Windows/ARM** (`docs/05-build-plan.md`), which four
targets do not cover: `darwin/amd64` is every Intel Mac and `windows/arm64` is current Windows-on-ARM
hardware. And M0 introduces the first genuinely OS-dependent code in the tree — the single-writer
lockfile, `flock` on unix and `LockFileEx` on Windows behind build tags — so the matrix stops being a
formality the moment that lands.

ADR-0001 is `Accepted`, and an `Accepted` ADR is never edited (`CLAUDE.md` non-negotiable #2). Hence
this ADR rather than a correction in place.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| **Seven targets: the cartesian six plus `linux/arm`** | Covers every platform M0's demo claims; keeps the 32-bit ARM target the spike actually verified; build-only, so the cost is minutes of CI | Two of the seven have no runtime coverage, only build coverage |
| Keep criterion 5's four | No decision needed, no ADR needed | Silently drops a target the spike verified, and leaves Intel Macs and Windows-on-ARM unbuilt while the demo claims them |
| Six: the cartesian product only | Symmetrical and easy to state | Drops `linux/arm`, which the spike verified and which is a Raspberry Pi — a stated target device for a self-hosted personal brain |
| Every target Go supports | Maximal | Meaningless breadth; nobody runs a personal brain on `plan9/386` |

## Decision

**The cross-compilation matrix is seven targets, and it runs on every pull request.**

```
linux/amd64    linux/arm64    linux/arm
darwin/amd64   darwin/arm64
windows/amd64  windows/arm64
```

`linux/arm` is retained because the spike verified it and 32-bit Raspberry Pi hardware is squarely
this project's own stated target. `windows/arm64` is added on the strength of direct measurement, not
inferred from criterion 5's unnamed "6/6": all seven were built from a clean checkout before this ADR
was written.

```
$ for t in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
    GOOS=${t%/*} GOARCH=${t#*/} go build -o /dev/null ./... && echo "$t OK"
  done
linux/amd64 OK      darwin/amd64 OK      windows/amd64 OK
linux/arm64 OK      darwin/arm64 OK      windows/arm64 OK
linux/arm   OK
```

**The trigger moves from `push: main` to `pull_request` as well.** A job that only runs after a merge
cannot block the merge that broke it — `main.yml`'s own header comment says so, and accepted that
cost while the tree had no platform-specific code. M0 removes the premise. A `windows/arm64` build
break must fail on the PR that introduces it.

The same reasoning applies to the L4 e2e job that shares this workflow, so it moves too. Its Windows
runtime leg arrives with the lockfile, not here.

`make cross-compile` builds the same seven pairs locally and joins `make check-all`, because
`CLAUDE.md`'s Workflow section admits exactly one exception — a gate needing PR metadata a Makefile
cannot produce — and `GOOS=x GOARCH=y go build ./...` needs none.

## Consequences

- **Seven build jobs per PR instead of four after a merge.** Each is a `go build` with no test run
  and no C toolchain; the wall-clock cost is small and it buys pre-merge signal on every platform the
  product claims.
- **Seven required status checks, not one.** The job names its checks per matrix leg
  (`cross-compile linux/amd64` and so on), so the branch ruleset must register all seven. A required
  context that never posts is never satisfied and permanently blocks every merge — the registration
  is verified against the names the workflow actually posts, not against names assumed from this
  table.
- **Build coverage is not runtime coverage, and this ADR does not pretend otherwise.** The matrix
  proves the code compiles for a target; it proves nothing about behavior there. `main.yml`'s existing
  comment records a real bug that a full matrix would have compiled without complaint — a path-shape
  check that accepted a Windows-style path on POSIX and silently rewrote it. Platform behavior needs a
  test that names the platform, which is why M0 adds a `windows-latest` leg to the e2e and integration
  jobs alongside the lockfile.
- **`darwin` has no runtime leg and is not getting one in M0.** It shares the `unix.Flock` code path
  with Linux, and GitHub bills macOS minutes at 10x against Windows's 2x for private repositories.
  Revisit when the repository goes public or the first `darwin`-specific code appears.
- **ADR-0001 stays as written**, including the inconsistency between its criterion table and its
  results table. That inconsistency is described here, in the ADR that supersedes the criterion,
  which is where the record of a superseded decision belongs.

## Verification

- `make cross-compile` exits zero on a clean checkout for all seven pairs.
- The matrix job appears as seven separate checks on a pull request, each named for its leg.
- `make check-all` includes `cross-compile`, so the local pre-PR command and CI agree.
