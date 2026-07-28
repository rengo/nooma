# ADR-0001 spike — results

Run on 2026-07-28. **This branch is never merged.** Its output is the decision recorded in
`docs/adr/0001-sqlite-driver.md`; the code here exists only to produce these numbers and to
let anyone reproduce them.

The spike ran **twice**, because the first run changed what was worth measuring:

| | Configuration | Outcome |
|---|---|---|
| **Run 1** | `ncruces` v0.21.3 + sqlite-vec | Works, but only pinned to late 2024 |
| **Run 2** | `ncruces` v0.35.2, no sqlite-vec, brute-force vectors in Go | Better on every axis, on current software |

**Run 2 is the configuration being accepted.** Run 1 is kept because it is the evidence for
why sqlite-vec was dropped, not just an earlier draft.

Reproduce with:

```bash
go run ./spike/sqlitedriver   # the seven criteria, run 2 configuration
go run ./spike/bruteforce     # brute-force scaling curve
```

## Hardware

| | |
|---|---|
| CPU | Intel i7-11700F @ 2.50 GHz, 16 threads |
| RAM | 15 GiB |
| OS | Linux 6.6 (WSL2) |
| Go | 1.26.4 |

**This is not the target hardware.** ADR-0001 criterion 6 specifies a Raspberry Pi 4 or
equivalent. See the caveat under criterion 6.

## Versions — and the first finding

The combination only works pinned to late 2024:

| Component | Version | Released |
|---|---|---|
| `ncruces/go-sqlite3` | **v0.21.3** | 2024-12-19 |
| `asg017/sqlite-vec-go-bindings` | v0.1.6 | 2024-11-20 |
| SQLite (embedded) | 3.47.0 | 2024-10-21 |
| sqlite-vec | v0.1.6 | — |

`ncruces` v0.24.0, v0.27.1 and v0.35.2 **do not compile** against the sqlite-vec binding.

The reason is structural, not a missing patch. Up to ~v0.21 the driver ran SQLite as a
WebAssembly blob exposed through an exported `sqlite3.Binary` variable, so sqlite-vec could
ship its own SQLite-plus-extension `.wasm` and swap it in. From v0.35 the driver no longer
runs wasm at all: SQLite is machine-translated to Go with
[`wasm2go`](https://github.com/ncruces/wasm2go) and shipped as a generated Go module
(`go-sqlite3-wasm/v3`). There is no binary to swap.

Getting sqlite-vec onto a current `ncruces` therefore means reproducing that pipeline —
compiling SQLite + sqlite-vec with wasi-sdk, optimizing with binaryen, translating with
`wasm2go` — and maintaining the result. The tooling is public (`tools.sh` in the wasm module)
but it is a C toolchain at build time and an ongoing maintenance commitment.

## The seven criteria

| # | Criterion | Result | Measurement |
|---|---|---|---|
| 1 | sqlite-vec loads, `vec0` answers KNN | **PASS** | 10,000 vectors of dim 768; KNN returned 20 neighbours |
| 2 | FTS5 stays in sync through triggers | **PASS** | 1,000 mixed insert/update/delete ops; `units`=10,001, `units_fts`=10,001; `integrity-check` clean |
| 3 | Operational PRAGMAs | **PASS** | `journal_mode=wal`, `foreign_keys=1`, `busy_timeout=5s` |
| 4 | `VACUUM INTO` + `integrity_check` with WAL open | **PASS** | `integrity_check=ok`; 33 MB backup reopened and readable |
| 5 | Cross-compilation without a C toolchain | **PASS** | 6/6 targets, statically linked |
| 6 | Hybrid recall p95 < 100 ms over 10k units | **PASS\*** | p50 18.72 ms, p95 21.72 ms, p99 26.30 ms (200 queries) |
| 7 | Write throughput ≥ 50 units/s | **PASS** | 1,296 units/s (500 captures, one transaction each, unit + vector + FTS) |

### Criterion 5 detail

`CGO_ENABLED=0`, no C toolchain present:

```
linux/amd64      OK   8.9M      darwin/amd64     OK   9.0M
linux/arm64      OK   8.6M      windows/amd64    OK   9.2M
darwin/arm64     OK   8.7M      linux/arm        OK   7.0M
```

`file` reports `statically linked`. The distribution promise survives SQLite.

### Criterion 6 — why the asterisk

**Measured on an i7-11700F, not on the specified target.** A Raspberry Pi 4 (Cortex-A72 @
1.5 GHz, far lower memory bandwidth) is roughly 5–8× slower for this workload. Extrapolated,
p95 lands somewhere around **110–175 ms**, which would **fail** the 100 ms threshold.

Criterion 6 is therefore **not met**. It is unverified on target hardware, and the honest
projection is that it fails. Declaring it passed on the basis of a desktop measurement would
be exactly the theatre the criterion was written to prevent.

Resolving it requires either a measurement on real hardware, or a conscious decision to
relax the threshold for low-end targets.

## Side question: is sqlite-vec needed at all?

Every risk above traces back to one dependency. Nooma is a personal brain — one human, one
vault — so the obvious question is whether a plain linear scan in Go suffices at realistic
vault sizes. Vectors are unit-normalized, so cosine similarity is a dot product and top-K is
a partial sort. No index, no dependency, ~40 lines.

| Units | Vector memory | p50 | p95 | p99 |
|---|---|---|---|---|
| 1,000 | 3 MB | 0.74 ms | 0.88 ms | 0.99 ms |
| 10,000 | 29 MB | 7.50 ms | 8.30 ms | 8.96 ms |
| 50,000 | 146 MB | 39.06 ms | 43.12 ms | 44.45 ms |
| 100,000 | 293 MB | 78.26 ms | 84.74 ms | 91.77 ms |
| 500,000 | 1.4 GB | 401.95 ms | 434.57 ms | 455.15 ms |

At 10,000 units brute force is **faster than the sqlite-vec path measured above** (8.3 ms vs
21.7 ms p95 — not a like-for-like comparison, since the sqlite-vec figure includes FTS5 and
RRF fusion, but the vector half is clearly not the bottleneck).

The real difference is not speed, it is **residency**. sqlite-vec keeps vectors on disk and
streams them; brute force needs them all in RAM. On a Raspberry Pi 4 with 4 GB, 100k units
means 293 MB resident just for vectors — possible but uncomfortable, and the latency
extrapolation puts it far past acceptable.

Brute force is comfortable to roughly **50k units**, which is years of a single person's
capture at any plausible rate.

---

# Run 2 — `ncruces` v0.35.2, no sqlite-vec

The configuration being accepted. Vectors are stored as `BLOB` in an ordinary table and
proximity search is a dot product over an in-memory index.

| Component | Version |
|---|---|
| `ncruces/go-sqlite3` | **v0.35.2** (2026-07-06) |
| SQLite (translated) | **3.53.3** |
| sqlite-vec | not used |

## Results

| # | Criterion | Result | Measurement |
|---|---|---|---|
| 1 | Vectors stored, KNN correct | **PASS** | 10,000 vectors dim 768; index loads in **41.98 ms** (29 MB RAM); self-match correct |
| 2 | FTS5 in sync through triggers | **PASS** | 1,000 mixed ops; `units`=10,001, `units_fts`=10,001; `integrity-check` clean |
| 3 | Operational PRAGMAs | **PASS** | `journal_mode=wal`, `foreign_keys=1`, `busy_timeout=5s` |
| 4 | `VACUUM INTO` + `integrity_check` | **PASS** | `integrity_check=ok`; 42 MB backup reopened and readable |
| 5 | Cross-compilation, no C toolchain | **PASS** | 6/6 targets, `statically linked` |
| 6 | Hybrid recall p95 < 100 ms | **PASS** | p50 13.61 ms, **p95 17.85 ms**, p99 21.64 ms |
| 7 | Write throughput ≥ 50 units/s | **PASS** | **2,817 units/s** |

## Run 1 vs run 2

| | Run 1 (pinned + sqlite-vec) | Run 2 (current + brute force) |
|---|---|---|
| SQLite | 3.47.0 (Oct 2024) | **3.53.3** |
| Driver age | 20 months stale | current |
| Recall p95 | 21.72 ms | **17.85 ms** |
| Write throughput | 1,296 units/s | **2,817 units/s** |
| Binary size | ~9 MB | 16 MB |
| Extra dependency | sqlite-vec | none |

Dropping sqlite-vec made the system **faster, more current, and simpler**, at the cost of a
larger binary and vectors resident in RAM.

## Two findings that matter for implementation

**FTS5 is opt-in and per connection.** In v0.35 it is not compiled into the SQLite build; it
is registered as an extension:

```go
import "github.com/ncruces/go-sqlite3/ext/fts5"
must(fts5.Register(db))
```

The store must register it on **every connection it opens**, not once at startup. A
connection that skips it fails with `no such module: fts5` only when an FTS query runs —
late, and far from the cause.

**Loadable extensions are not dead, they moved.** v0.35 exposes
`sqlite3.ExtensionInit(db, mod.New, mod.DylinkInfo)`, which is how its own FTS5 package
works. sqlite-vec is therefore not permanently out of reach — it would need porting to that
mechanism rather than shipping a `.wasm`. That door stays open if brute force is ever
outgrown.

## Binary size

9 MB → 16 MB. `wasm2go` emits substantially more Go than the previous wasm runtime carried.
Still a single static file, still no toolchain, but worth recording rather than discovering
at release time.

## What this leaves open

1. **Minimum supported hardware is undecided.** The Raspberry Pi 4 target was dropped as a
   product decision; the replacement floor is due before M6. Until then criterion 6 is a
   regression detector against a recorded machine, not an absolute guarantee.
2. **Brute force has a ceiling.** Comfortable to roughly 50k units; beyond that, the index
   both slows down and stays resident. The recall interface must hide the implementation so
   this can be replaced without touching the core.

The decisions belong in the ADRs, not here.
