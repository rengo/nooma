# Spec — M4c: focus hysteresis and adjacency

Specification for `m4c-focus-hysteresis`, third of six slices sharing
`openspec/changes/m4-mirror-ui/proposal.md`. States what MUST be true after this change, in
testable form; not how (`sdd-design`'s job).

Sources: umbrella §3.3, §5 (`m4c` paragraph, lines 281-283), §5.1 rows `feat/brain-focus-incumbent`
and `feat/brain-focus-adjacency`, §5.2 test row 6, R6; `docs/02-cognitive-core.md` §3 (lines
218-339: priority, restart effects, m4a amendment); `internal/core/focus` (`Rank`, `Select`,
`ResolveMargin`, `AdjacencyStrengths`); `internal/brain/today.go`, `digest.go:112-116`.

## Scope boundary (binding)

`m4c` is two links. **Link 1** (`feat/brain-focus-incumbent`): `focus.Select` gets its first
production callers, per `focus.Kind`, over an incumbent held in process memory, written by
**both** Today and the digest (no table, no repo, no port change: I01); the margin is read from
config; `RelationRepo.ByUnit` over the incumbent's members flows through `weight.Edge` into
`focus.AdjacencyStrengths`, and Today's own per-Kind ranking is fed that adjacency (R8, its
first paragraph). Link 1 may itself be cut in two (design §7: **1b** carries the digest as a
writer). **Link 2** (`feat/brain-focus-adjacency`): the digest's `Carry` reads the union
adjacency against the incumbent it loaded (R9's adjacency half), re-keyed to trigger ids, and
Today's pending-digest mirror reads it against the incumbent the request just selected, P'
(R8's second paragraph), ending the empty-map "Adjacency is M4's" input. Doc 02's m4a
amendment is removed in link 1 (non-negotiable 1).

**Not this change**: the graph island, persistence of the incumbent, any change to
`Select`/`Rank`/`Priority`/`AdjacencyStrengths` behavior, any new port method.

## Requirements

| ID | Requirement | Invariants |
|----|-------------|-----------|
| R1 | Today computes each Kind's focus with `focus.Select` over that Kind's incumbent | I19 |
| R2 | The margin is `focus.ResolveMargin(config.HysteresisMargin)`, read per request | I19 |
| R3 | The incumbent lives only in brain memory: survives requests in one process, absent in a fresh service | I01 |
| R4 | Incumbents are per Kind and independent | I19 |
| R5 | Today and the digest both write the in-memory incumbent and never the vault; memory is race-safe for both writers | I01, I27 |
| R6 | Doc 02 loses only the m4a amendment sentence block and gains text naming who writes the incumbent | non-neg. 1 |
| R7 | Displayed `Score` is the literal `Rank` value, adjacency included | I18 |
| R8 | Today's ranking is fed adjacency computed from the incumbent's relations | — |
| R9 | The digest Selects per Kind over the incumbent and reads adjacency against the pre-Select incumbent | I19 |
| R10 | With no incumbent, adjacency is empty and hysteresis is off, for each consumer's own focus ranking and for the digest; the pending-digest mirror is carved out (it reads the incumbent its request just selected) | I01 |

### R1 — Select over an incumbent

**MUST**: a challenger displaces an incumbent member only when its score exceeds
`incumbent x (1 + margin)`; the resulting membership becomes the next request's incumbent.

- GIVEN a first request holds A in the task focus, and B's score then rises above A's by less
  than the margin
- WHEN Today is requested again in the same process
- THEN A keeps its slot and B does not displace it
- GIVEN B's score exceeds `A x (1 + margin)`
- WHEN Today is requested
- THEN B displaces A

Edge: an incumbent id absent from candidates (archived, retyped) blocks nobody and is dropped;
fewer candidates than `DefaultSize` are all members; an empty pool yields an empty focus and an
empty incumbent.

### R2 — Margin from config

**MUST**: nil resolves to the default; negative or non-finite resolves to 0; a configured value
changes the outcome.

- GIVEN `hysteresis_margin` is 0 and a challenger is 1% above the incumbent
- WHEN Today is requested
- THEN the challenger displaces the incumbent

### R3 — In process only

**MUST**: no schema, repo method or file holds the incumbent; a freshly constructed service has
none.

- GIVEN a service holding incumbent A and a challenger inside the margin
- WHEN a new `TodayService` is built over the same vault and requested
- THEN the challenger is ranked by plain score (the accepted restart cost)

### R4 — Per Kind

- GIVEN incumbents for both Kinds
- WHEN only one Kind's candidates change
- THEN the other Kind's membership is unchanged

### R5 — Read-only vault, race-safe memory

**MUST**: a Today request and a digest run each update the in-memory incumbent and neither
writes the incumbent to the vault; a Today request leaves every vault table unchanged
(existing I27 write guard passes; I27's "writes nothing" is read as **writes nothing to the
vault**, since Today does write the in-memory incumbent); concurrent Today requests and digest runs are
`go test -race` clean.

- GIVEN concurrent Today requests and a digest run
- WHEN they execute under the race detector
- THEN no race is reported and the incumbent afterwards is one writer's complete selection,
  never a mix of two

- GIVEN a digest run
- WHEN it completes
- THEN the incumbent equals that run's selection and no incumbent row exists in the vault

**Stated plainly (P' semantics, link 2):** because Today publishes the focus it selected and the
digest reads adjacency against the focus it loaded, **viewing `/ui` can change a later
low-energy digest's order and which items it carries**. It changes nothing a digest owes
(which triggers are due, what is surfaced). Doc 02 §7 and `docs/06-harness.md` §4 (I27 row)
say so.

### R6 — Doc 02 amendment

**MUST**: remove only the sentence "Until `m4c` gives `focus.Select` its first caller, `/ui`'s
Today mirror computes each focus with `focus.Rank` alone ... rather than a transient one"
(lines ~336-339), since m4c ends exactly that. **Keep** lines ~330-335 and **append one clarifying clause**
(link 2) saying the restart effects describe each focus's own ranking and the digest, and that
`/ui`'s mirror is the exception because it reads the focus its own request just selected. The
restart cost (one un-damped transition; `relation_to_active_focus` 0 for every unit in a focus's
own ranking) stays true of a fresh process and is what R10 requires. **Keep** the §3 clamp guarantee (~266-270) as is.
**Add** text in §3 stating that the incumbent is written by both `/ui`'s Today computation and
the digest's, each over its own `Select`, and by nothing else; and that a process with no Today
view (headless, Telegram-only) still holds an incumbent once a digest has run. **Reword**
doc 02 §7 "Viewing is not delivering" (~:1116) and `docs/06-harness.md` §4's I27 row from
"writes nothing" to "writes nothing **to the vault**", naming the in-memory incumbent and the
P' consequence of R5.

- GIVEN the change is applied
- WHEN `scripts/docs-sync.sh` runs and doc 02 is searched
- THEN the sync check passes, the amendment is absent, the restart paragraph is intact, and the
  two-writer sentence is present

### R7 — Displayed score

**MUST**: a member's `Score` is the value `Rank` produced for it with that request's adjacency
(NaN included, never coerced). Because adjacency now lifts scores, a member kept by hysteresis
may show a lower score than a non-member; no field claims otherwise.

- GIVEN A is held over B by hysteresis and A has adjacency to the incumbent set
- WHEN Today renders
- THEN A shows its adjacency-inclusive rank score and B is absent

### R8 — Adjacency reaches Today

**MUST**: for each Kind, the adjacency passed to `Rank` is `focus.AdjacencyStrengths` over the
`weight.Edge`s built from `RelationRepo.ByUnit` of that Kind's incumbent members, computed
against the incumbent as it stood before this request's `Select`.

- GIVEN incumbent A, and candidates B (relation strength 0.9 to A) and C (none), equal otherwise
- WHEN Today is requested
- THEN B ranks above C
- GIVEN a relation stored as `A -> B` or `B -> A`
- WHEN Today is requested
- THEN B receives the same adjacency either way (undirected)

**MUST** (the pending-digest mirror): Today's pending-digest mirror ranks `Carry` with the
union of both Kinds' adjacency read against the incumbent **this request just selected** (the
one it publishes), not the one it loaded, because that is the incumbent a digest run next
would load. Today's own focus ranking above still reads the incumbent as it stood before this
request's `Select`.

- GIVEN incumbent A displaced by B in this request's `Select`, and pending items X (related to
  A) and Y (related to B), otherwise equal, on a low-energy morning with more items than
  `LowEnergyDigestSize`
- WHEN Today is requested and a digest is assembled next
- THEN the mirror orders Y before X, and so does the digest

### R9 — Adjacency reaches the digest

**MUST**: the digest, at `brain/digest.go:112-116`, computes adjacency exactly as R8 does,
against the incumbent as it stood before its own `Select`, and passes it to `prospection.Carry`
instead of an empty map. It then calls `focus.Select` per Kind with the resolved margin
(R2) and stores the result as the new incumbent **only after the digest is sent**; an empty
digest, a dry run, a missing conversation and a failed send store nothing, and a digest that
carries only a question stores its selection once sent. A failure to compute the focus does
not stop the digest: it is recorded in the decision log as `check.focus.unavailable`, the
digest is sent with empty adjacency, and nothing is stored. The row is written only on a
committed run (a dry run computes nothing and writes nothing), and it repeats on each retry
scan while the digest stays unsent. With no incumbent, adjacency is empty.

- GIVEN a live incumbent and two otherwise equal digest items, one adjacent to it
- WHEN the digest is assembled
- THEN the adjacent item is carried ahead of the other
- GIVEN a digest run whose Select keeps A against a challenger inside the margin
- WHEN Today is requested next
- THEN A is still held (the digest's incumbent is Today's)
- GIVEN a process where Today is never requested
- WHEN two digests run in sequence
- THEN the second digest sees adjacency and hysteresis from the first

### R10 — Restart behavior

**MUST**: with no incumbent (fresh process, before any Today view or digest run), **each
consumer's own focus ranking and the digest** rank with empty adjacency and no hysteresis; the
first computation by either writer after start is the un-damped transition doc 02 already
states. **Carve-out, the pending-digest mirror**: it reads adjacency against the incumbent the
request just selected (R8), which is non-empty on a fresh process, so the first `/ui` view's
mirror can already order a low-energy digest by adjacency. Doc 02's restart text gains a
clause saying so.

- GIVEN a fresh service and units with relations among them
- WHEN the first request is served
- THEN `relation_to_active_focus` is 0 for every unit in Today's own focus ranking and
  membership is the plain top-N
- GIVEN a fresh service, a low-energy morning with more pending items than
  `LowEnergyDigestSize`, and a pending item related to a unit in the first request's focus
- WHEN the first request is served
- THEN the mirror carries that item ahead of an otherwise equal unrelated one (P' is read)
- GIVEN a fresh service and no Today view
- WHEN the first digest is assembled
- THEN its adjacency is empty

### Out-of-domain adjacency — no new requirement

Doc 02 (~266-270) guarantees a corrupt or out-of-domain adjacency cannot break
`priority >= effective_weight`. This is already covered by
`TestPriority_AdjacencyClampedToUnitInterval` and `TestAdjacencyStrengths_UnclampedBoundaryStrengths`.
Both new production paths route the map through `Rank`/`Priority` (digest via `Carry`'s `Rank`
call), so no raw reader appears. **Constraint**: any consumer reading the map outside
`Priority` would reopen C19/C31 and needs its own requirement.

## Verified by

L1 brain tests with fakes and a fake clock: R1-R5, R7-R10 (R3's fresh-service test watched
failing first, umbrella §5.2 row 6). Hysteresis fixtures follow design §6's **FX-H** (at least
`DefaultSize+1` candidates of the Kind, contesting slot 7 against slot 8, seeded by a first
request); low-energy `Carry` fixtures follow **FX-L** (at least `LowEnergyDigestSize+1` pending
items plus filler triggers, trigger ids distinct from unit ids); `go test -race` over both writers for R5; existing
`i19_hysteresis_margin_test.go` gains a production caller; `scripts/docs-sync.sh` for R6. No
test touches the network or a real LLM.

## Open questions

**OQ1 (closed)**: adjacency is in scope as R8-R10.

**OQ2 (closed, owner ruling 2026-10-07)**: both Today requests and the digest write the
incumbent; the digest calls `focus.Select` too.

**OQ3 (closed by design §3.6) — Expiry.** Default: none; hysteresis applies against the last writer's selection however
old.

**OQ4 (closed by design §3.3) — Which incumbent feeds adjacency?** Doc 02 says "active focus", singular; it does not
say per Kind. Today uses each Kind's own incumbent (matches `AdjacencyStrengths(previous
Selection)`). Now that the digest Selects per Kind, the per-Kind answer for the digest may fall
out naturally, with `Carry`'s adjacency being the union or the per-item Kind's. Left open for
design.

**OQ5 (closed, owner ruling 2026-10-07)**: a headless or Telegram-only process gets hysteresis
and adjacency in its digest, because the digest is itself a writer (R9).

**OQ6 (closed by design §3.4) — Digest candidate pool.** The digest's items are trigger-linked units, while `Select`
ranks a Kind's live candidates. The spec requires the digest to Select per Kind but does not
say whether over the full live pool (as Today) or over the digest items. Default: the same
live pool as Today, so both writers produce comparable incumbents. Left open for design.

## Exit criterion

Today's focuses come from `Select` over per-Kind in-process incumbents with the configured
margin and relation-fed adjacency; the digest Selects over and writes the same incumbent,
reading adjacency against the pre-Select one; a fresh service has neither; no table, repo, port method or vault write is added; doc 02's
amendment is gone and its restart text intact; `make check-all` is green.
