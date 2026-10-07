# Design — m4c: focus hysteresis and adjacency

Technical design for `m4c-focus-hysteresis`, the third of six slices sharing
[`m4-mirror-ui/proposal.md`](../m4-mirror-ui/proposal.md). Requirements are `spec.md` (R1–R10);
this document decides HOW, and closes OQ3, OQ4 and OQ6. Shape follows the archived
[`m4b` design](../archive/2026-09-29-m4b-units-capture/design.md).

> **Four things this design found that the spec did not anticipate**, named up front:
> 1. **`prospection.Carry` ranks by trigger id, not by unit id.** It overwrites
>    `c.ID = item.ID` before calling `focus.Rank` (`internal/core/prospection/digest.go:212-215`).
>    `AdjacencyStrengths` returns a map keyed by unit id, so passing it to `Carry` as it is
>    would compile, pass every test whose trigger ids equal unit ids, and change nothing. The
>    brain must re-key it (§3.5). This is the highest-risk line in the change.
> 2. **`Carry` ignores adjacency on a normal day.** When energy is not low it returns every item
>    unranked (`digest.go:176-178`). Adjacency changes the digest only on a low-energy morning
>    with more than `LowEnergyDigestSize` items. R9's tests must build exactly that fixture.
> 3. **`CheckService` has no `ConfigRepo`** (`internal/brain/check.go:34`), yet R2 needs the
>    margin in the digest. The margin read moves into the shared focus object (§3.1), so neither
>    service reads it itself.
> 4. **Today's pending-digest mirror and the digest can diverge if the mirror reads the wrong
>    snapshot.** Today publishes its own `Select` (P') before the digest ever runs, so a digest
>    run next loads P', not the P Today loaded. The mirror must read adjacency against P' (§3.5).

---

## 1. Ground truth (read at `6babc38`)

| Claim | Verified at |
|---|---|
| `Select(k, ranked, previous, margin, size)` filters by `Types(k)` and drops incumbents absent from `ranked`. It does not mutate `previous.Members` | `internal/core/focus/select.go:113-157` |
| `AdjacencyStrengths(previous, edges)` uses MAX and is undirected. An empty `previous` gives an empty map. It does not mutate its inputs | `internal/core/focus/adjacency.go:119-145` |
| `weight.Edge{From, To string; Strength float64}` | `internal/core/weight/spread.go:14-17` |
| `ByUnit(id)` returns relations at either endpoint. It is unbounded | `internal/ports/relationrepo.go:67-74` |
| Today ranks with `map[string]float64{}`, truncates to `DefaultSize`, and never calls `Select`. Its pending digest also calls `Carry` with an empty map | `internal/brain/today.go:154`, `:194-231` |
| The digest calls `Carry` with an empty map. `!commit` returns before the send. The post-send effects (`Surface`, `MarkAsked`, log rows) all run only after `Send` succeeds | `internal/brain/digest.go:116`, `:121-123`, `:136-179` |
| `ConfigRepo.HysteresisMargin *float64` is commented "NO reader in m2c" | `internal/ports/configrepo.go:16` |
| The digest's unsent paths, in order: `r.channel == nil` returns at `:33`; `!DigestDue` returns at `:49`; an **empty digest** (no pending triggers, no question) returns at `:93-105` (an existing guard, before `digestItems` and `Carry`); an **empty carry** with no question returns at `:118-120`; a dry run returns at `:121-123`; no conversation records `check.delivery_failed` and returns at `:125-134`; a failed `Send` records the same and returns at `:136-140` | `internal/brain/digest.go:33`, `:49`, `:93-140` |
| `DigestDue` stays true until a `check.digest.sent` row exists, so every scan tick after the due hour re-enters `assembleDigest` while the digest is unsent (no conversation, failing channel) | `internal/core/prospection/digest.go:35-42`, `internal/brain/digest.go:44-51` |
| `Carry`'s `carry` length is exempt items plus `min(LowEnergyDigestSize, rest)`: adjacency changes which items and in what order, never how many. `LowEnergyDigestSize` = `DefaultSize/2` = 3 | `internal/core/prospection/digest.go:118`, `:175-237` |
| `Select` keeps the top `DefaultSize` = 7 of a Kind's ranked candidates. With fewer than 8 candidates of a Kind nobody can be displaced, so no hysteresis fixture can be built from fewer | `internal/core/focus/select.go:26`, `:143-154` |
| `AdjacencyWeight` = 0.25: a unit at adjacency `a` is lifted by at most `AdjacencyWeight·a` in the additive nudge envelope | `internal/core/focus/priority.go:151`, `:175-179` |
| `checkRunner.at`: `commit` gates the writes and nothing else; a dry run writes no row and no column | `internal/brain/check.go:100-103`, `internal/brain/check_test.go:385-403` |
| `NewTodayService` takes no optional ports today; `NewCheckService` is the one that tolerates a nil collaborator by convention | `internal/brain/today.go:32`, `internal/brain/check.go` |
| `wireToday` builds `systemClock{}` (real time); a digest wiring test fixes its own `now` through the `wireProactive` clock seam | `cmd/nooma/wiring.go:313`, `:430` |
| `Select` returns `Selection{Kind, Members []string}`: ids only, no score | `internal/core/focus/select.go:32-35`, `:113-157` |
| `wireProactive` does not need an LLM (it degrades to `llm = nil`) but hard-codes `systemClock{}`, and `DigestDue` reads `now.Hour()`, so a wiring test cannot drive a digest through it today | `cmd/nooma/wiring.go:420-445`, `internal/core/prospection/digest.go:35-42` |
| `wireToday` is unconditional (`serve.go:144`). `wireProactive` runs only inside `wireScheduler`, which is LLM-gated (`serve.go:130`). `wireCheck` (`nooma check`) has a nil channel, so its digest returns at line 33 | `cmd/nooma/wiring.go:270-321`, `:420-446` |
| `internal/brain` declares **no** package-level `var` in any non-test file; the gate in §3.8 is default-deny, so this stays true by construction | `rg '^var ' internal/brain` (no non-test match) |
| `today_test.go`'s `wantTopIDs` compares against plain `Rank`+`[:DefaultSize]`. This still holds on a fresh incumbent, where `Select` reduces to plain top-N | `internal/brain/today_test.go:36-56` |

**New calibratable constants: none.** **New port methods: none.** **New ADR: none.** Doc 02 §3
gains text in both links (§3.7).

---

## 2. What m4c decides, in one paragraph

One brain-owned object, **`*brain.FocusKeeper`**, holds the incumbent as an
`atomic.Pointer` to an immutable two-Kind snapshot. It runs **the one focus computation** that
both writers call. `cmd/nooma` constructs it exactly once per `serve` process and injects it
into `TodayService` and the proactive `CheckService`. A computation loads the snapshot once.
That one snapshot is the "pre-Select incumbent" for every adjacency read and every `Select` in
that round, so R8/R9's "before this request's Select" is a property of the data flow, not a
rule a writer must remember. A writer publishes by swapping in the whole new snapshot: Today
when a request succeeds, the digest after its `Send` succeeds. Adjacency is per Kind for each
focus's own ranking, read against the loaded snapshot P. For `Carry` it is the union of both
Kinds, re-keyed from unit id to trigger id. The digest reads it against P. Today's
pending-digest mirror reads it against P', the snapshot Today itself is about to publish,
because P' is what a digest run next would load. A consequence, stated here so R10 is read
correctly: on a fresh keeper P is empty but P' is not, so the **first** `GET /ui`'s mirror
already orders a low-energy digest by adjacency. R10's "no incumbent, no adjacency" scopes to
each consumer's own focus ranking and to the digest, and carves the mirror out (§3.5).

---

## 3. Decisions

### 3.1 Where the incumbent lives: `brain.FocusKeeper`

```go
// internal/brain/focuskeeper.go
type FocusKeeper struct {
	units ports.UnitRepo
	cfg   ports.ConfigRepo
	rels  ports.RelationRepo         // read from link 1: per-Kind adjacency (R8)
	held  atomic.Pointer[incumbent]  // nil = no incumbent: a fresh process (R10)
}

// The constructor takes all three ports in link 1, so link 2 changes no signature.
func NewFocusKeeper(units ports.UnitRepo, cfg ports.ConfigRepo, rels ports.RelationRepo) *FocusKeeper

// incumbent is never mutated after publish; a new round builds a new one.
type incumbent struct{ byKind map[focus.Kind]focus.Selection }

// focusRound is one computation's whole result.
type focusRound struct {
	members map[focus.Kind][]focus.Ranked // Select's order; Score is Rank's literal value (R7)
	next    *incumbent                    // P': both Kinds, always
	// Link 2 adds these two fields, and only then, so link 1 has no field written and never read:
	//   adjacent     map[string]float64 // unit-keyed union adjacency vs the loaded P (the digest's Carry)
	//   nextAdjacent map[string]float64 // unit-keyed union adjacency vs next, P' (Today's mirror)
}

func (k *FocusKeeper) compute(ctx context.Context, now time.Time) (focusRound, error)
func (k *FocusKeeper) publish(r focusRound) // k.held.Store(r.next)
```

`compute` does the following, in order: `held.Load()` once; `cfg.Load` → `focus.ResolveMargin`
(R2, per computation); one `ByUnit` sweep over the members of P (both Kinds) → `[]weight.Edge`
(link 1); for each `focus.AllKinds()` Kind it calls `LiveFocusCandidatesByType`, then `Rank`
(with `AdjacencyStrengths(prev[k], edges)`, R8), then `Select(k, ranked, prev[k], margin,
focus.DefaultSize)`. It holds no clock: `now` is a parameter, so
`brain_single_clock_read_test.go` is unaffected.

**Where `Score` comes from.** `Select` returns ids only (§1), so `compute` builds
`scoreByID := map[string]focus.Ranked` from that Kind's Rank slice and, for each id in
`Select(...).Members`, in Select's order, looks up its `Ranked` by id. Joining by index would
silently hand a held incumbent the score of whoever sat at its slot in Rank's order (R7).
`compute` indexes `scoreByID[id]` directly, with no presence arm, exactly as `Carry` indexes
its queue map straight off `focus.Rank`'s result (`internal/core/prospection/digest.go:220-230`,
"no unreachable arm"): `Select` only returns ids it was given, so a missing id would mean
`Select` invented one, and an arm for it would be dead code no test can reach.

| Option | Verdict |
|---|---|
| **Brain struct injected into both services, built once in `cmd/nooma`** — chosen | A fresh `FocusKeeper` is a fresh incumbent by construction (R3/I01). Two services share memory only because `cmd/nooma` handed them the same pointer (gated, §3.8) |
| Package-level `var` in `internal/brain` | Rejected. Every service in the process, including every test, would share it, so "a fresh service has none" would be false. Made unexpressible by a gate (§3.8) |
| A field on `TodayService` alone | Rejected by the owner ruling (OQ2): the digest is also a writer, and OQ5 needs it to work headless |
| State in `internal/core/focus` | Rejected. Core holds no state between calls (non-negotiable 3, R4.6's no-package-var rule) |
| A table, a repo, or a `config` row | Rejected. I01, and spec R3 |

**Why the keeper owns ports and not only the state:** the margin read and the per-Kind `Select`
then live in one place. The two writers cannot rank differently (OQ6), and `CheckService`
gets the margin without a `ConfigRepo` of its own. Each constructor gains exactly one
parameter.

**Nil policy differs per constructor.** `NewTodayService` **requires a non-nil keeper**: Today
is the always-wired writer (`serve.go:144`, unconditional), calls `compute` and `publish`
unconditionally, and has no nil path to test. `NewCheckService` **allows nil** (`nooma check`
and every existing digest test pass `nil`): a nil keeper means an empty adjacency map, no
`Select` and no publish (§3.5).

### 3.2 Concurrency: whole-snapshot swap, `atomic.Pointer`, no mutex

| Option | Verdict |
|---|---|
| **`atomic.Pointer[incumbent]`; `Load` once per round; `Store` of a freshly built whole snapshot** — chosen | "Never a mix of two" is structural: the only write is one pointer swap, and no API sets one Kind. There is no lock to forget and no `Unlock` to drop, so there are no deadlock mutants. Readers never block |
| `sync.Mutex` around a `map[Kind]Selection` | Viable, but the obvious later edit (lock, set one Kind, unlock) is exactly the "mix" R5 forbids, and the type does nothing to prevent it |
| Per-Kind atomic pointers | Rejected. That expresses the mix directly |

**Lost update is accepted and documented.** Today and the digest can both load snapshot P,
compute, and store. The last store wins, and each candidate result is a valid selection from
P. Hysteresis is a smoothing heuristic, not a ledger. The next computation re-converges.

**P' is read, never written, after `compute` returns.** Today's mirror reads adjacency against
`round.next` (P') before publishing it. That is safe for the same reason: `next` is built
fresh and is immutable once built, whether or not it is ever stored.

**No defensive copies.** `Select` and `AdjacencyStrengths` only read `previous.Members`
(§1), and `next` is built fresh. The snapshot is immutable by convention, and the `-race` test
is the check (§6).

### 3.3 OQ4 — which incumbent feeds adjacency

| Consumer | Option | Verdict |
|---|---|---|
| Each focus's ranking (Today and the digest's own `Select`) | **Per Kind: `AdjacencyStrengths(prev[k], edges)`** — chosen | Matches `AdjacencyStrengths(previous Selection)`'s single-Kind signature and Select's per-Kind contest. A task does not get lifted for being related to a worry |
| | Union of both Kinds | Rejected. It couples two contests doc 02 keeps separate ("two queries with different criteria") |
| `Carry` (digest: vs P; Today's pending-digest mirror: vs P') | **Union: `AdjacencyStrengths(Selection{Members: task ∪ load}, edges)`** — chosen | Digest items are triggers on units of **any** type, including types in neither focus. "Related to what I am focused on" has one honest reading here: anything currently in focus. Under MAX, the union equals the per-unit max of the two per-Kind maps, so no new rule is introduced |
| | The item's own Kind | Rejected. An item on a `knowledge` unit has no Kind and would always read 0. That is a rule invented by omission |

One `ByUnit` sweep, memoized by unit id within a round, covers the members of P (link 1: the
per-Kind maps, at most `2 × DefaultSize` = 14 reads) and of P' (link 2: the mirror's map, at
most 14 more), so at most `2 × 2 × DefaultSize` = 28 reads. No unit's relations are read twice
in one round.

### 3.4 OQ6 — the digest's `Select` candidate pool

| Option | Verdict |
|---|---|
| **The same live pool per Kind as Today, via the shared `compute`** — chosen | Both writers produce the same kind of incumbent from the same function. R9's "the digest's incumbent is Today's" is then meaningful, and drift between them cannot be expressed |
| The digest items' units | Rejected. That incumbent would be "units with a pending trigger". Today would then apply hysteresis against a set no focus ever selected. Items also arrive re-keyed by trigger id (§3.5), which is a different id space |

Cost: two `LiveFocusCandidatesByType` reads plus at most 28 memoized `ByUnit` reads (§3.3) per
`compute`. `compute` runs only on a committed digest that is due, has content and has a
conversation (§3.5), but **not once a day**: `DigestDue` stays true until `check.digest.sent`
(§1), so a failing channel re-runs `compute` on every scan tick until a `Send` succeeds. A
vault with no conversation never reaches it.

### 3.5 Edges, re-keying, and where each writer computes and publishes

**Relation → edge:** `weight.Edge{From: r.FromUnitID, To: r.ToUnitID, Strength: r.Strength}`.
`Strength`, not `Confidence` and not their product. Doc 02 §4 defines strength as relevance,
confidence as certainty. A product would be an invented combination with no §13 row. Type is
ignored. Duplicate edges (a relation between two members, fetched twice) are harmless under
MAX.

**Re-keying for `Carry`** is one package function, used by both Today and the digest:

```go
// carryAdjacency maps unit-keyed adjacency onto the trigger ids Carry ranks by
// (prospection/digest.go:212-215). A trigger with no unit gets no entry.
func carryAdjacency(byUnit map[string]float64, pending []ports.DueTrigger) map[string]float64
```

**Which snapshot each consumer reads (the preview must match the digest).** Doc 02 §7 says
`/ui`'s Today mirrors what the digest would carry if it ran right now. A digest run right now
loads the snapshot Today has just published, P', not the P Today loaded. So:

| Consumer | Adjacency read against | Why |
|---|---|---|
| Today's own focus ranking (R8) | P, the loaded snapshot | The `Select` it feeds is the one that produces P' |
| Today's pending-digest mirror `Carry` | **P'** (`round.next`) | What the digest would see next. This is the only reading under which "mirrors what the digest would carry" is true |
| The digest's `Carry` | P, the snapshot it loaded | Its own `Select` has not run yet |

**Today** (`todayRunner.at`): it calls `compute` first, then
`Carry(items, carryAdjacency(round.nextAdjacent, pending), …)` (link 1: the empty map, as now),
then builds each `Focus` from `round.members[k]` joined through `LiveByIDs` (the N7 drop is
unchanged). It **publishes only if the whole request succeeds.** A failed request returns an
error and leaves no trace.

**Digest** (`assembleDigest`), in this order:

1. The existing early returns (`:33`, `DigestDue` `:49`) run first, then the existing
   **empty-digest guard at `:93-105`** (no pending triggers and no question), then
   `digestItems`. That guard already sits before every new read, so an empty digest costs no
   `compute` and an error in `compute` cannot touch one. Nothing is added for it; it is
   presented as the guard it already is (L1-12e pins its position).
2. The existing `Carry(items, map[string]float64{}, low, now)` at `:116` stays where it is and
   keeps its empty map in **both** links. It is the **verdict** for the next two returns:
   `len(carry) == 0 && question == nil` (`:118-120`) and `!commit` (`:121-123`, which returns
   `len(carry)`). That is sound because `Carry`'s `carry` length does not depend on
   adjacency (§1): adjacency changes which items and in what order, never how many.
3. The existing no-conversation return (`:125-134`) runs.
4. **Only now, and only when `r.focus != nil`**, `round, err := r.focus.compute(ctx, now)`.
   It sits after the conversation check and after the `!commit` return, and still before
   `Send`. A dry run therefore never computes, and records nothing, which is `check.go:100-103`'s
   rule (`commit` gates the writes) kept: `check.focus.unavailable` is a `decision_log` write,
   so it is written only on a committed pass.
   **A compute error is non-fatal.** It is recorded as one `decision_log` row,
   `ActionCheckFocusUnavailable` (`check.focus.unavailable`, naming the error), and the digest
   continues: the `Send` renders the step-2 `carry`, and nothing is published afterwards. A
   focus that cannot be computed must not stop a digest the user is owed, and the digest
   worked without a focus before this change. It is a deliberate exception to §8's C5
   reasoning, which covers repos the digest already depends on. The new action costs one
   constant, one `AllDecisionActions` entry and its count comment
   (`internal/ports/decisionlog.go:165`: "forty-seven" becomes "forty-eight"), and two explicit
   edits in `test/support/repocontract/decisionlog.go:133-198`: the hard-coded 47-entry `want`
   map gains `ports.ActionCheckFocusUnavailable: true`, and the subtest title
   "forty-seven" becomes "forty-eight". The vocabulary is not derived from the list; both files are
   edited by hand (§5).
   **Repeat behaviour across retry scans.** `DigestDue` stays true until `check.digest.sent`
   (§1), so with a persistently failing focus and a failing `Send`, **each scan tick records one
   `check.focus.unavailable` row** beside that tick's `check.delivery_failed`: the same
   cadence the failing channel already writes at, at most doubling it. When the `Send` then
   succeeds, the digest is sent with the empty-adjacency split and the rows stop. Nothing
   de-duplicates the row: a de-duplication rule would be a new behaviour with no requirement.
5. Link 2 only: `carry, held = prospection.Carry(items, carryAdjacency(round.adjacent,
   pending), low, now)` replaces the step-2 split before `Send`. Link 1 has no second call.
6. **Publish happens immediately after `Send` succeeds**, with the other post-send effects.

A nil keeper keeps today's behaviour: an empty map, no `Select`, no publish. That follows
`checkRunner`'s "nil is legal" convention.

**R10 on a fresh keeper, and the mirror carve-out.** On a fresh keeper P is empty, so Today's
own focus ranking and the digest's `Carry` read empty adjacency: that is R10, unchanged. The
mirror is the one consumer that does **not**: it reads P', which is never empty once the pool
has a unit, so the first `GET /ui` on a fresh process can already order the mirror's
low-energy `Carry` by adjacency. That is the correct reading of "mirrors what the digest would
carry if it ran right now", because a digest run right after that request would load P'. R10 is
therefore scoped to **each consumer's own focus ranking and the digest**, and doc 02's
restart text gets a clarifying clause (§3.7), not a removal. A test pins it (§6,
`TestToday_FirstRequestMirrorUsesNextAdjacencyOnFreshKeeper`).

Paths that publish nothing, each with its own killer (§6): an empty digest, no conversation
(`digest.go:125`), a dry run, a failed `Send`, a compute error. A **question-only digest**
(zero carry, a question present) **does** publish if it is sent: the user saw a digest, and
the carry count is not the criterion.

**Why publish after `Send`, and what a retry sees.** A failed send is retried on the next scan,
and the retry **recomputes** against whatever the snapshot is then. Today may have published in
between and the vault may have changed, so the retried digest can differ from the failed one.
That is accepted. The reason for publishing after `Send` is not that retries are identical.
It is that an unsent digest must not move the incumbent: the user never saw that selection, so
the next computation must not be damped toward it.

### 3.6 OQ3 — expiry

**None.** An old incumbent costs at most a 5% relative bias toward units that are still live.
A member that left the pool drops out in `Select`. A TTL would be a new behavioural number,
needing a §13 row and a clock read in the keeper, for a problem nobody has observed. A restart
already resets the incumbent. Revisit only on evidence.

### 3.7 Doc 02 §3 (R6)

- **Link 1:** delete lines 336-339 ("Until `m4c` gives …"). Keep 330-335 and 266-270
  unchanged. Add: *"The previous focus is held per focus, in process, and written by exactly
  two computations, each running its own `focus.Select`: `/ui`'s Today view on every
  successful request, and the morning digest each time one is sent. Nothing else writes it,
  nothing persists it, and it does not expire. A process with no Today view (headless, or
  reached only through Telegram) still holds one once a digest has gone out. A writer replaces
  both focuses at once, so two concurrent writers leave one writer's whole selection, never a
  mix."* Also adds the per-focus adjacency sentence below, and an interim sentence that link 2
  deletes: *"Until `m4c`'s second link, the digest and `/ui`'s pending-digest mirror read
  `relation_to_active_focus` as 0 even while an incumbent is held."*
- **Link 2:** delete the interim sentence. Add: *"The digest, whose items span both focuses
  and neither, reads `relation_to_active_focus` against the union of the two. `/ui`'s mirror
  reads it against the focus its own request just selected, because that is the focus the
  digest would load if it ran next."* Also a **clarifying clause appended** to the restart text
  (`docs/02-cognitive-core.md:330-335`, kept, not removed), after "Two effects from one restart,
  not one.": *"This describes each focus's own ranking and the digest. `/ui`'s pending-digest
  mirror is the exception: it reads the focus its own request just selected, which a fresh
  process already has, so the first view after a restart can already order the mirror's
  low-energy digest by adjacency."*
- **§7 "Viewing is not delivering"** (`docs/02-cognitive-core.md:1116`, link 1 for the first
  sentence, link 2 for the second). "...but rendering it writes nothing" becomes "...but
  rendering it **writes nothing to the vault**: `surfaced_at` and `asked_at` are set only by
  the digest pass above, never by a `GET`". Link 1 adds: *"It does hold one thing in process
  memory: the focus it selected (§3), which the next digest then reads."* Link 2 adds the
  consequence plainly: *"So viewing `/ui` can change a later low-energy digest's order, and
  which items it carries, because the digest reads adjacency against the focus the last
  writer published. It changes nothing a digest owes: which triggers are due, and what is
  surfaced, are decided without it."*
- `docs/06-harness.md` §4 **I27 row** (`:269`): "Rendering `/ui`'s Today writes nothing"
  becomes "Rendering `/ui`'s Today writes nothing **to the vault**: `surfaced_at` and
  `asked_at` are set only by the digest pass, never by a GET. Its in-memory incumbent is the
  one thing it does write". Link 1.
- **Link 1 per-focus sentence:** *"Each focus reads `relation_to_active_focus` against its own
  previous focus, as the strongest relation `strength` joining a unit to a member."*
- `docs/06-harness.md` §4: the I01 row names the new behavioural test, and the I19 row names
  its production callers. `internal/ports/configrepo.go:16`'s comment changes to name
  `brain.FocusKeeper` as the reader. The new `check.focus.unavailable` action is added wherever
  doc 02 or doc 03 enumerates `check.*` actions (link 1, or 1b if cut; `rg 'check\.digest\.held' docs/`
  found no enumerating doc line at `6babc38`, so the edit may be empty). The ports vocabulary
is edited by hand in two files (§3.5 step 4, §5), not derived.

No `internal/core` file changes, so `docs-sync.sh` needs no label. It still runs and passes.

### 3.8 Structural gates for invisible properties

| Property | Gate | Fires on (probe) | Silent on |
|---|---|---|---|
| A fresh service has no incumbent (I01, R3) | **`TestBrain_DeclaresNoPackageLevelVar`** (conformance, `go/ast` over non-test `internal/brain/*.go`). **Default-deny, syntactic only, no type inference**: every package-level `var` spec fails, except exactly two shapes. (1) A blank assertion: every name is `_` (`var _ T = …`). (2) An error sentinel: every value is a call expression whose callee is the selector `errors.New` or `fmt.Errorf`. Anything else fails, whatever its type, because "what is this variable's type?" is the question a syntactic gate cannot answer and must not guess. The gate has no allow-list of types and so cannot be dodged by a type it did not think of. Its allow set is two syntactic shapes that cannot hold state a service could share, which is what keeps it from firing on correct code. | `var defaultKeeper = NewFocusKeeper(...)`; `var held atomic.Pointer[incumbent]`; `var cache = map[string]int{}`; `var n = 0`; `var mu sync.Mutex`; `var x *T` | The current tree; `var ErrX = errors.New("…")`; `var _ ports.Clock = fixedClock{}`; a grouped `var ( … )` block of only those shapes (a test case with an allowed sentinel, a blank assertion and a grouped block, so the gate is itself proved not to over-fire) |
| Today and the digest share one incumbent in `serve` and down the whole chain | **`TestServe_OneFocusKeeperSharedByTodayAndDigest`** (cmd L1, AST over the non-test files of `cmd/nooma`). (a) Exactly one `wireFocus(` call, in `serve.go`, assigned to an identifier X; (b) `wireToday(…, X)` and `wireScheduler(…, X)` take X; (c) **`NewFocusKeeper(` and `wireFocus(` appear nowhere else outside `serve.go` and their own definition in `wiring.go`**; (d) `wireScheduler`'s call to `wireProactive` passes its own focus parameter, and `wireProactive`'s `NewCheckService` call passes its own focus parameter as the last argument, never `nil` or a call. The keeper flows `serve.go` → `wireScheduler` (`wiring.go:448`, whose call to `wireProactive` is at `:459`) → `wireProactive` (`:420`) → `NewCheckService`; parsing `serve.go` alone cannot see the last two links | Two `wireFocus` calls; `wireToday(db, wireFocus(db))` inline; `NewFocusKeeper` called inside `wireProactive`; `wireProactive` passing `nil`; `wireScheduler` passing `nil` or a fresh keeper | The wiring in §6 |
| The keeper really reaches the digest | **`TestWireProactive_DigestSharesTodaysKeeper`** (cmd, over a real migrated vault, a fake channel, and a fixed clock through the §5 `wireProactive` clock seam). One `wireFocus`, then `wireToday(db, k)` and `wireProactive(…, k)`. Digest → Today: on an **FX-H** fixture (§6), a digest is sent and holds A over B (inside margin); weights then flip to favour B; the first-ever `Today` still shows A. `wireToday` runs on `systemClock{}` (real time, `wiring.go:313`) while the digest runs on the fixed clock, so the fixture is **date-insensitive**: every candidate carries identical timestamps, so a different `now` shifts all their scores together and cannot reorder them. A `nil` or a second keeper in `wireProactive` shows B. Link 2 adds the Today → digest direction (low-energy order follows Today's published P') | `wireProactive` passes `nil`; `wireProactive` builds its own keeper | Shared keeper |
| No incumbent reaches a port | Existing `testdata/schema/store_api.golden` diff: any port signature change regenerates the golden | A `FocusRepo`, or `ConfigRepo.SaveIncumbent` | Unchanged ports |
| Never a mix of two writers (R5) | The type itself: one `Store` of a whole `*incumbent`, and no per-Kind setter. Behavioural pin: `TestFocusKeeper_PublishReplacesBothKinds` | A "merge" publish that keeps the old load focus when the new one is empty | `held.Store(r.next)` |
| Race safety (R5) | `TestFocusKeeper_ConcurrentTodayAndDigestAreRaceFree` under `-race` (CI already runs `-race`) | `held` changed to a plain `*incumbent` field | `atomic.Pointer` |
| Adjacency actually reaches `Carry` | `TestDigest_AdjacentItemCarriedAhead` on fixture rule **FX-L** (§6): low energy, ≥ 4 pending items, **trigger ids ≠ unit ids** | Passing `round.adjacent` unre-keyed | `carryAdjacency` |
| The preview equals the digest | `TestToday_PendingDigestMatchesDigestOrder`, on a fixture where **P ≠ P'** (§6) | Today's mirror reading P (or the empty map) | The mirror reading P' |

Each GREEN commit body records its probe (mutation applied, red output, reverted). That is the
m4b convention: a probe that never applied is itself a finding.

---

## 4. Data flow

```
GET /ui ─▶ TodayService.Today(now)
             └─ keeper.compute(now): P := held.Load()
                  cfg.Load → ResolveMargin
                  ByUnit(each member of P.task ∪ P.load) → []weight.Edge
                  per Kind: candidates → Rank(adj_k = AdjacencyStrengths(P[k], edges))
                                       → Select(k, ranked, P[k], margin, DefaultSize)
                                       → members (Score joined by id)
                  next := P'  (both Kinds)
                  [L2] adjacent     := AdjacencyStrengths(P.task ∪ P.load, edges)
                  [L2] nextAdjacent := AdjacencyStrengths(P'.task ∪ P'.load, edges ∪ ByUnit(new members))
             ├─ Carry(items, carryAdjacency(nextAdjacent, pending))  mirror: what a digest run NEXT sees
             ├─ Focuses from round.members (+ LiveByIDs)
             └─ on success: keeper.publish(round)   → held = P'

scheduler tick ─▶ CheckService.Check ─▶ assembleDigest (due)
             ├─ no items and no question (:93-105) ─▶ return (no compute, no publish)
             ├─ Carry(items, empty map)                      verdict only (count is adjacency-independent)
             ├─ empty carry+no question / dry run / no conversation ─▶ return (no compute, no publish)
             ├─ round := keeper.compute(now)   error ─▶ log check.focus.unavailable, no publish, send the verdict split
             ├─ [L2] Carry(items, carryAdjacency(round.adjacent, pending))   vs the loaded P; this split is sent
             └─ Send ok ─▶ keeper.publish(round) ─▶ Surface / MarkAsked / log rows
                Send fails ─▶ record delivery_failed, no publish
```

## 5. File changes

| File | Action | Link |
|---|---|---|
| `internal/brain/focuskeeper.go` (+`_test.go`) | Create: `FocusKeeper` (`units`, `cfg`, `rels`), `incumbent`, `focusRound`, `compute`, `publish`; edges + per-Kind adjacency | 1; `adjacent`/`nextAdjacent` fields in 2 |
| `internal/brain/today.go` | `NewTodayService(…, focus *FocusKeeper)`; compute first; members from the round; publish on success; doc comments for `Today`/`Focus` stop saying "no Select" | 1 (mirror `Carry` adjacency in 2) |
| `internal/brain/check.go`, `digest.go` | `NewCheckService(…, focus *FocusKeeper)` appended last (m3e precedent); non-fatal compute placed after the conversation check and the `!commit` return, publish after `Send` in `assembleDigest` (§3.5) | 1b (link 1 if the overflow cut in §7 is not taken) |
| `internal/brain/digest.go` | `carryAdjacency`; delete the "Adjacency is M4's" comment; the second `Carry` gets `round.adjacent` | 2 |
| `internal/ports/decisionlog.go` | `ActionCheckFocusUnavailable`; `AllDecisionActions` entry; the count comment at `:165` ("forty-seven" becomes "forty-eight") | 1b (link 1 if no cut) |
| `test/support/repocontract/decisionlog.go` (`:133-198`) | Hand edit, not derived: add `ports.ActionCheckFocusUnavailable: true` to the hard-coded `want` map (47 entries become 48) and change the subtest title "forty-seven" to "forty-eight" | 1b (link 1 if no cut) |
| `cmd/nooma/wiring.go`, `serve.go` | Link 1: `wireFocus(db)` (builds `NewFocusKeeper(units, cfg, rels)`); `wireToday(db, k)`; `wireCheck` passes `nil`. **1b:** `wireScheduler(…, k)` → `wireProactive(clock, …, k)` (call at `wiring.go:459`) | 1, 1b |
| `internal/ports/configrepo.go:16` | Comment only | 1 |
| `docs/02-cognitive-core.md` §3 and §7 (`:1116`), `docs/06-harness.md` §4 (I01, I19, I27 rows) | §3.7 | 1, 2 |
| `test/conformance/i01_incumbent_fresh_service_test.go`, `brain_no_package_var_test.go` | Create | 1 |
| `test/conformance/i19_hysteresis_margin_test.go` | Add `TestI19_TodayHoldsIncumbentInsideMargin` (the production caller) | 1 |
| `test/conformance/i27_viewing_is_not_delivering_test.go` | Pass a keeper at `:71`; add a `RelationRepo` write guard (Today now reads relations: `Upsert` and `Delete` are its write methods; `ByUnit`, `ThresholdsFor`, `Evidence`, `ExistingPairs` and `ByID` are reads, `ByID` already counted under `UnitRepo`), and **recount the "sixteen distinct method names" comment (`:34`, repeated at `:112-113`) from the guard in the same edit**, never by hand; **update the comment at `:84`**, "TodayService is stateless", to "TodayService keeps only the in-memory incumbent and writes nothing to the vault, so a single call proves the same postcondition" (it is no longer stateless). I27 now reads "writes nothing **to the vault**" everywhere (§3.7) | 1 |
| `cmd/nooma/serve_gate_test.go` | Create: the extended AST gate and `TestWireProactive_DigestSharesTodaysKeeper` (§3.8). Link 1 carries clauses (a) and the `wireToday` half of (b), plus (c) for `wireFocus`/`NewFocusKeeper`; **1b carries (b)'s `wireScheduler` half, (c)'s remainder and (d)**, since `wireScheduler` and `wireProactive` gain their keeper parameter there | 1, 1b (+ Today → digest direction in 2) |

**Call-site table.** Every constructor and wiring signature change, with its edit. Several of
these files are build-tagged, so `make check` does not compile them: **each link must run
`go vet -tags integration,e2e ./...` and it must pass** before the link's PR opens. A missed
tagged site fails CI only after the PR is open.

| Site | Symbol | Edit |
|---|---|---|
| `cmd/nooma/serve.go:130` | `wireScheduler` | add the shared keeper argument |
| `cmd/nooma/serve.go:144` | `wireToday` | pass the same keeper X |
| `cmd/nooma/wiring.go:271` (`wireCheck`) | `NewCheckService` | **append `nil`**. `nooma check` has a nil channel, so its digest returns at `digest.go:33`, before `compute`. A keeper here would be built, never read, and a second keeper beside serve's |
| `cmd/nooma/wiring.go:312` (`wireToday`) | `NewTodayService` | take and pass the keeper |
| `cmd/nooma/wiring.go:429` (`wireProactive`) | `NewCheckService` | pass the keeper (last argument) |
| `cmd/nooma/wiring.go:448` (`wireScheduler`; its `wireProactive` call is at `:459`) | `wireProactive` | pass the keeper, and `systemClock{}` for the new `clock` parameter. `wireProactive` takes a `ports.Clock` so `TestWireProactive_DigestSharesTodaysKeeper` can fix `now` inside `DigestDue`'s window; `wireProactive` hard-codes `systemClock{}` today |
| `cmd/nooma/wiring_today_test.go:35` | `wireToday(db)` | `wireToday(db, k)`, with `k := wireFocus(db)` |
| `internal/brain/today_test.go:23`, `:364` | `NewTodayService` | pass `NewFocusKeeper(units, cfg, rels)` |
| `test/conformance/i27_viewing_is_not_delivering_test.go:71` | `NewTodayService` | pass a keeper |
| `test/conformance/check_effect_completeness_test.go:139`, `i15_trigger_expires_not_fires_test.go:168`, `i16_quiet_hours_test.go:148`, `i09_uncertain_band_asked_test.go:173` | `NewCheckService` | append `nil` |
| `test/integration/due_scan_status_vocabulary_test.go:81`, `due_scan_concurrent_test.go:89` and `:93`, `pending_question_vocabulary_test.go:118` (**`integration` tag**) | `NewCheckService` | append `nil` |
| `test/e2e/m3_demo_test.go:103`, `m3e_relation_answer_demo_test.go:114` (**`e2e` tag**) | `NewCheckService` | append `nil` |

If the overflow cut (§7) is taken, every `NewCheckService`, `wireScheduler` and `wireProactive`
row above moves to 1b with the signature change that forces it; link 1 keeps `NewTodayService`,
`wireToday` and `wireFocus`, and compiles and passes on its own.

**Recount of link 1's test churn:** 4 conformance + 4 integration + 2 e2e `NewCheckService`
sites (10, one line each), 3 `NewTodayService` sites (`today_test.go` ×2, i27), 1 `wireToday`
test site with a 2-line keeper, plus the new gate and wiring tests. That is 14 mechanical edits
(~20 lines), not the "~12" the earlier table claimed, and 5 of the 14 are build-tagged.

## 6. Testing strategy and mutation targets

Level: L1 brain tests with memrepo fakes and `fixedClock` (cheapest level that proves it);
L2 conformance for I01/I19/I27 and the AST gates; one cmd-level wiring test over a real
migrated vault (not L3: it asserts wiring, not SQLite behaviour). Hysteresis fixtures change a
unit's stored weight in memrepo between two calls on the same keeper.

### Fixture constraints (stated once; every test below cites them by name)

**FX-H, hysteresis fixtures.** `Select` keeps the top `DefaultSize` = 7 (`select.go:26`,
`:143-154`), so with fewer than 8 candidates of a Kind nobody is displaced and a fixture
asserting a displacement or a hold proves nothing. Every hysteresis fixture therefore:
(1) puts **at least `DefaultSize+1` = 8 candidates of the Kind** in the pool: six fillers
`F1…F6` that clearly outrank everything contested, plus the contested units, so the
contest is **slot 7 versus slot 8**; (2) **seeds** the incumbent with a first request, in
which A holds slot 7 and B sits at slot 8 (P = `{F1…F6, A}`); (3) only afterwards changes
B's stored weight, past `A·(1+margin)` to displace, or short of it to hold; (4) stamps
every candidate with identical timestamps, so a different `now` scales all scores together and
cannot reorder them (the fixture is **date-insensitive**; `wireToday` runs on real time).
Tests using it: R1 (both halves), R2, R4's per-Kind pair, R7 (which additionally needs Select's
order to differ from Rank's), the R8 pair, the mirror fixture, and the wiring test's
Digest → Today direction.

**FX-L, low-energy `Carry` fixtures.** `Carry` returns every item unranked unless energy is
low (`prospection/digest.go:175-178`) and then carries only the top `LowEnergyDigestSize` = 3
(`:118`, `:232`), so a fixture asserting adjacency reordering needs: (1) a fresh low-energy
reading; (2) **at least `LowEnergyDigestSize+1` = 4 pending items**; (3) two contested items
`X` and `Y`, equal in every base term, plus **filler triggers**: one ranking above both and
one ranking below both on base weight, so that `X` and `Y` both land in the top 3 and the
assertion is on their relative order in `carry`; (4) **trigger ids ≠ unit ids**, with ids
chosen so Rank's lexicographic tie-break (`rank.go`, third level) puts the item the
*mutant* would favour first; (5) one pending trigger with a nil `UnitID` where a test covers
L2-9.

**The R8 fixture** (`B ranks above C`) is an FX-H fixture where B and C carry equal weight,
each below A's, B has a relation of strength 0.9 to A, and B's weight is chosen so that
B lifted by `AdjacencyWeight·0.9` (`priority.go:151`) clears `A·(1+margin)` while C alone
does not. The test's own comment carries the arithmetic. B displaces A and C does not, which
is how "B ranks above C" is observable in membership.

**The P ≠ P' fixture** (`TestToday_PendingDigestMatchesDigestOrder`), run as **two table
cases**, each an FX-H plus FX-L fixture, so that neither Kind alone can satisfy it:

| Case | Kind contested | P → P' | `X` related to | `Y` related to |
|---|---|---|---|---|
| task | task | `{F1…F6, A}` → `{F1…F6, B}` | A (strength 0.9) | B (0.9) |
| load | load | `{G1…G6, C}` → `{G1…G6, D}` | C (0.9) | D (0.9) |

In each case the first request seeds P; then the contested challenger's weight is raised past
`incumbent·(1+margin)`, so the second request's `Select` displaces the incumbent and publishes
P'. Against P, `X` outranks `Y`; against P', `Y` outranks `X`. **The load case exists so a
mutant that builds `nextAdjacent` from `P'.task` alone dies** (it sees no relation for `Y`
and falls back to the tie-break, arranged to put `X` first), and the task case kills the mirror
image, a mutant using `P'.load` alone. Each case asserts three things: (1) the mirror's carry
order is `Y, X`; (2) a `CheckService` over the same stores, **sharing the same keeper**, run
next in the same low-energy conditions, carries `Y, X`, equal to the mirror; (3) a control
digest over a fresh keeper seeded with P by the first request only carries `X, Y`, so the
fixture really has P ≠ P'.

**The fresh-keeper mirror pin** (`TestToday_FirstRequestMirrorUsesNextAdjacencyOnFreshKeeper`).
An FX-L fixture over a fresh keeper, **no seed request**; units `A` in the task top 7 and a pending trigger `X` on a
unit related to A, plus a twin `Y` with no relation, tie-break arranged to put `Y` first. The
first `GET /ui` is served. Assert: Today's own focus members carry the plain-`Rank` score with
empty adjacency (R10 holds for each focus's own ranking) **and** the mirror's carry puts `X`
before `Y` (P' is read, and it is non-empty). A mutant that reads P, or the empty map, puts `Y`
first. This pins R10's carve-out.

**Mutation targets, enumerated from the production diff.** Each needs a test that kills it.
Equivalent mutants are named so nobody burns time on them. "Link" is the PR that ships it.

| # | Link | Production branch / expression | Mutant | Killed by |
|---|---|---|---|---|
| L1-1 | 1 | `snap := held.Load()` used as `prev` | ignore it (always empty) | `TestToday_IncumbentHeldInsideMargin` (R1, FX-H) |
| L1-2 | 1 | `ResolveMargin(cfg.HysteresisMargin)` | `DefaultHysteresisMargin`; `0` | `TestToday_ConfiguredZeroMarginDisplaces` (R2, FX-H); R1 test |
| L1-3 | 1 | `cfg.Load` error → return | swallow | `TestFocusKeeper_ConfigErrorPropagates` |
| L1-4 | 1 | candidates error → return | swallow | `TestFocusKeeper_CandidatesErrorPropagates` |
| L1-5 | 1 | `Select(…, prev[k], …)` | another Kind's prev / `focus.Selection{}` | R1 test; `TestToday_KindsAreIndependent` (R4, FX-H in each Kind) |
| L1-6 | 1 | `Select(…, focus.DefaultSize)` | `DefaultSize-1` | existing `wantTopIDs` tests (>7 candidates) |
| L1-7 | 1 | `publish`: `held.Store(r.next)` whole | merge per Kind; no-op | `TestFocusKeeper_PublishReplacesBothKinds`; R1 test |
| L1-8 | 1 | Today publishes only on success | publish before a later error return | `TestToday_FailedRequestPublishesNothing` (fail `questions.Open`) |
| L1-9 | 1 | `Score` joined by id: `scoreByID[id]` over Select's ids, indexed directly with no presence arm | join by index into Rank's slice; look up the wrong Kind's map | `TestToday_HeldMemberShowsItsOwnRankScore` (R7, FX-H) |
| L1-10 | 1 | member order = Select's order | re-sort by score | same R7 test (asserts order) |
| L1-11 | 1 | digest `if r.focus != nil` | remove guard | existing i09/i15/i16 conformance (nil keeper) panic |
| L1-12 | 1 | digest publish after `Send` ok | before Send; on failed Send; in dry run; removed | `TestDigest_FailedSendPublishesNothing`, `…_DryRunPublishesNothing`, `TestDigest_TodaySeesDigestIncumbent` (R9b, FX-H), `…_SecondDigestSeesFirst` (R9c, FX-H) |
| L1-12b | 1 | publish only after the empty-digest return | publish before it | `TestDigest_EmptyDigestPublishesNothing` |
| L1-12c | 1 | publish only after the no-conversation return (`digest.go:125`) | publish before it | `TestDigest_NoConversationPublishesNothing` |
| L1-12d | 1 | publish keyed on "was sent", not on `len(carry) > 0` | gate it on `len(carry) > 0` | `TestDigest_QuestionOnlyDigestPublishesWhenSent` (zero carry, a question, send ok) |
| L1-12e | 1 | the existing empty-digest return (`digest.go:93-105`) sits above every `compute` | **move `compute` above `:93`** | `TestDigest_EmptyDigestDoesNotCompute` (a keeper whose `cfg.Load` fails: no `check.focus.unavailable` row on an empty digest) |
| L1-12f | 1 | `compute` runs after the conversation check and the `!commit` return | move it above either | `TestDigest_DryRunFocusErrorWritesNothing` (below), `TestDigest_NoConversationDoesNotCompute` (failing keeper, no conversation: only the `check.delivery_failed` row) |
| L1-13 | 1 | digest `compute` error is non-fatal: record `check.focus.unavailable`, continue | return the error (**the old `FocusErrorAbortsDigest` mutant, now the mutant, not the behaviour**) | `TestDigest_FocusErrorStillSendsDigest` (digest is sent, items surfaced, one `check.focus.unavailable` row); `TestDigest_FocusErrorRepeatsPerRetryScan` (failing keeper and failing `Send`, two scans: one focus row per scan) |
| L1-13a | 1 | `check.focus.unavailable` is recorded only when `commit` (`check.go:100-103`) | record it in a dry run | `TestDigest_DryRunFocusErrorWritesNothing`: a failing keeper, `commit=false`, a digest with content and a conversation; the log is empty and the returned count equals the wet run's |
| L1-13b | 1 | no publish after a compute error | publish a zero round | same test, then a fresh `Today` with flipped weights shows plain top-N (nothing was held; FX-H) |
| L1-14 | 1 / 1b | `cmd/nooma` passes one keeper to both | two keepers; `wireProactive` builds its own | `TestServe_OneFocusKeeperSharedByTodayAndDigest` (c), (d) (1b carries the clauses that need `wireScheduler`/`wireProactive`) |
| L1-14b | 1 / 1b | `wireProactive` forwards its keeper to `NewCheckService` | passes `nil` | `TestWireProactive_DigestSharesTodaysKeeper`; gate (d) |
| L1-14c | 1 / 1b | `wireScheduler` forwards its keeper to `wireProactive` | passes `nil` | gate (d). `wireScheduler` is LLM-gated, so no behavioural test reaches it |
| L1-15 | 1 | `Strength: r.Strength` | `r.Confidence` | `TestToday_AdjacencyUsesStrengthNotConfidence` (R8 fixture; strength 0.9/conf 0.1 vs 0.1/0.9) |
| L1-16 | 1 | `From/To` mapping | swap | **Equivalent** (undirected); R8's direction test pins it anyway |
| L1-17 | 1 | `ByUnit` over every member of both Kinds | first member only; task only | `TestToday_AdjacencyFromSecondMember`, load-Kind adjacency test (both on the R8 fixture shape, FX-H) |
| L1-18 | 1 | `ByUnit` error → return | swallow | `TestFocusKeeper_RelationsErrorPropagates` |
| L1-19 | 1 | per-Kind `AdjacencyStrengths(snap[k], …)` for Rank | union | `TestToday_TaskNotLiftedByLoadIncumbent` (OQ4, FX-H) |
| L1-20 | 1 | `Rank(cands, adj_k, now)` | empty map | R8 `B ranks above C` (the R8 fixture, FX-H) |
| L2-7 | 2 | `adjacent` = union vs P | task only; vs `next` | `TestDigest_ItemAdjacentToLoadFocusCarried` (FX-L); `TestDigest_FreshKeeperHasNoAdjacency` (R10, the digest half: `next` is non-empty, the digest's adjacency must still be empty) |
| L2-8 | 2 | `carryAdjacency` re-key | pass unit-keyed map | `TestDigest_AdjacentItemCarriedAhead` (FX-L) |
| L2-9 | 2 | `carryAdjacency` skips nil `UnitID` | dereference | same test with one nil-`UnitID` trigger (FX-L item 5; panics) |
| L2-10 | 2 | Today's mirror `Carry` gets `carryAdjacency(round.nextAdjacent, …)` | empty map; `round.adjacent` (vs P) | `TestToday_PendingDigestMatchesDigestOrder` (P ≠ P' fixture, both cases; assertions 1 and 2 kill the empty map and the P-reading) |
| L2-11 | 2 | `nextAdjacent` is read against `next` (P') | against P | same test, assertion 1; the control in assertion 3 proves the fixture discriminates |
| L2-11b | 2 | `nextAdjacent` is the union of both Kinds' P' | `P'.task` alone; `P'.load` alone | the load case and the task case of the P ≠ P' fixture respectively (FX-H, FX-L) |
| L2-12 | 2 | the digest's `Carry` reads `adjacent`, not `nextAdjacent` | swap | `TestDigest_FreshKeeperHasNoAdjacency` and `TestDigest_AdjacentItemCarriedAhead` |
| L2-12b | 2 | the mirror reads P' even when P is empty (R10 carve-out) | mirror reads P or the empty map | `TestToday_FirstRequestMirrorUsesNextAdjacencyOnFreshKeeper` (§6; FX-L) |
| L2-13 | 2 | `ByUnit` memo covers P' members not in P | memo only P's members | `TestToday_PendingDigestMatchesDigestOrder` (P ≠ P' fixture: Y's neighbour is only in P') |
| L2-14 | 2 | wiring: Today's publish reaches the digest | (covered by L1-14b) | `TestWireProactive_DigestSharesTodaysKeeper`, Today → digest direction (low-energy order follows Today's P'; FX-L, date-insensitive) |
| L2-15 | 2 | the digest's second `Carry` result is the one sent | send the step-2 (empty-adjacency) split | `TestDigest_AdjacentItemCarriedAhead` (the rendered digest, not only `Carry`'s return, orders the adjacent item first) |

Conformance: `TestI01_IncumbentDoesNotSurviveAFreshService` is written and watched red
before any behaviour exists (umbrella §5.2 row 6), **for the right reason**. Red is a failed
assertion, not "undefined: brain.FocusKeeper". The order is therefore:

1. **Scaffold commit** (compiles, no behaviour): `FocusKeeper`, `NewFocusKeeper(units, cfg,
   rels)`, `incumbent`, `focusRound`, and `compute`/`publish` signatures with zero-value bodies
   (`compute` returns an empty round, `publish` is a no-op), the new `NewTodayService`
   parameter accepted and ignored, `wireFocus` and the wiring. `make check` passes. It also
   makes the §5 call-site churn land in a commit with no behaviour to review.
2. **RED commit**: I01 (`TestI01_…`), I19's production caller, the gates and the L1 tests. They
   fail on their assertions: the I01 test builds two keepers, requests Today on the first with
   B inside A's margin, expects A held, and gets B; the fresh second keeper's first request is
   asserted to be plain top-N and passes already, which is fine, since the failing half is the
   first keeper's.
3. **GREEN commit**: behaviour.

`TestI19_TodayHoldsIncumbentInsideMargin`. I27's write guard passes over both links.

## 7. The PR chain (stacked-to-main)

Each PR puts a scaffold commit, then the RED test commit, ahead of GREEN. No tip is red. Budgets
are impl+docs; tests are reported beside them. Each link runs `make check-all` **and**
`go vet -tags integration,e2e ./...` (§5).

| # | Branch | Scaffold + RED | GREEN | Impl+docs | Tests |
|---|---|---|---|---|---|
| 1 | `feat/brain-focus-incumbent` | I01 fresh-service, I19 production caller, no-package-var gate, serve gate (extended), wiring test, L1-1…L1-14c (less the 1b rows), L1-15…L1-20 | `focuskeeper.go` (units, cfg, **rels**; edges and per-Kind adjacency), Today compute+publish, digest compute+publish with non-fatal error, `ActionCheckFocusUnavailable`, constructors, `wireFocus` + wiring, configrepo comment, doc 02 link-1 text, doc 06 rows | ~330. **Overflow cut**: the digest as writer splits into **1b**, leaving ~230. 1b takes L1-11…L1-14c and L1-12f/L1-13a, serve-gate clauses (b)'s `wireScheduler` half, (c)'s remainder and (d), the `wireScheduler`/`wireProactive` keeper parameter (and `wireProactive`'s `clock` parameter), `decisionlog.go`, `test/support/repocontract/decisionlog.go`, `check.go`/`digest.go`, and every `NewCheckService` call site. Link 1 keeps Today, `wireFocus`, `wireToday` and gate clauses (a), (b) `wireToday` half and (c) for `wireFocus`, so it compiles and passes alone | ~480 incl. 14 call-site edits |
| 2 | `feat/brain-focus-adjacency` | L2-7…L2-15 tests (L2-11b, L2-12b included), I27 relation guard (already in 1; extended to the mirror), Today → digest wiring direction | `adjacent`/`nextAdjacent` fields, P' sweep, union adjacency, `carryAdjacency`, Today mirror + digest `Carry`, doc 02 link-2 text | ~130 | ~320 |

`rels` is a `NewFocusKeeper` parameter from link 1 and is **read** in link 1 (the edge sweep
feeding per-Kind adjacency), so link 2 changes no constructor signature and link 1 has no
written-but-unread field. Link 2's two new `focusRound` fields are added in link 2.

Tip behaviour: after link 1, `/ui`'s two focuses apply hysteresis and read adjacency against
their own incumbent; the digest applies hysteresis, and both `Carry` calls still read an empty
map (doc 02's interim sentence says so). After link 2, the full behaviour is in place. No shipped
route changes shape. `FocusMember` keeps its fields.

## 8. Risks

| # | Risk | Mitigation |
|---|---|---|
| C1 | A unit-keyed map reaches `Carry` and silently does nothing | §3.5 helper; L2-8 with trigger ids ≠ unit ids |
| C2 | Low-confidence (m3e uncertain-band) relations lift adjacency at full strength | Accepted: strength is relevance and `Priority` clamps it. **Owner-review OR1**: filter by confidence later, with a §13 row |
| C3 | Lost update between concurrent writers | Accepted (§3.2); re-converges on the next computation |
| C4 | `ByUnit` N+1 (≤28 reads per computation once link 2 covers P and P', **each read unbounded per unit**), now on every `GET /ui` **and on every scan tick on which a committed, due digest with content and a conversation has not yet been sent** (`DigestDue` holds until `check.digest.sent`, so a failing channel repeats it per tick; it is not "once a day") | Named, not mitigated: no port method is allowed by the spec, and a hot unit's relation list is read in full per request. m4d's neighbourhood bound is the real cap |
| C5 | A relation or config read error inside the keeper does **not** abort a digest | Deliberate (§3.5): logged as `check.focus.unavailable`, the digest is sent with empty adjacency. Today, which has no send to protect, still returns the error |
| C6 | Today loads config twice (its own status read plus the keeper's margin read) | Accepted: one margin read site is worth one extra singleton read |
| C7 | A headless `serve` with no LLM binding has no scheduler, so no digest writer | Unchanged scope: Today is then the only writer, as OQ5 already allows |
| C8 | INFO: an archived or ghost incumbent member keeps lifting its neighbours' adjacency until the next publish replaces the snapshot. `Select` drops it from membership, but `AdjacencyStrengths` still counts it as a member of P | Accepted, no design change. Bounded by one publish: the next successful Today or sent digest replaces P |
| C9 | INFO: unbounded `ByUnit` per `GET` (C4), restated as a request-cost risk: a vault with a very connected unit makes every page load, and every retried digest scan, read that unit's whole relation list | Named only |
| OR2 | INFO, owner review: Today's `GET /ui` now **fails** on a `ByUnit` read error (and on a config read error in the keeper), because Today has no send to protect and returns errors (§3.5, C5). The digest, which owes the user a message, treats the same errors as non-fatal. The asymmetry is deliberate and is recorded for the owner to confirm, not a defect found | Owner decides whether Today should degrade to empty adjacency instead; either answer changes one `if` and two tests |

No open question blocks `sdd-tasks`. OQ3, OQ4 and OQ6 are closed above.

## 10. Judgment Day round 3 carry-overs

Binding corrections from round 3. They refine the sections above without rewriting them; where
one conflicts with an earlier line, this section wins and `tasks.md` implements it.

1. **`TestToday_RepeatedRequestsLeaveTheMorningDigestByteIdentical`**
   (`internal/brain/today_test.go:323-400`) builds its digest through a keeper-less runner
   (`r.focus == nil`). It can no longer observe what its name claims and it contradicts spec R5
   (viewing `/ui` can change a later low-energy digest). It is narrowed in name and comment to
   "delivery bookkeeping (surfaced, asked and held rows) is unchanged", and the comment states
   that the keeper-free runner is why. A shared-keeper variant is optional.
2. **Mutant L1-13b ("publish a zero round" on a digest focus error).** The killer first SEEDS
   the keeper (FX-H: Today holds `{F1…F6, A}`), then a toggleable failing `ConfigRepo` fails
   only the digest's `compute`; afterwards A must still be held. The old "fresh Today shows plain
   top-N" assertion cannot discriminate and is dropped.
3. **Spec wording.** R9 reads "the union of both Kinds' members of the loaded incumbent,
   re-keyed to trigger ids (§3.3)"; OQ4's body no longer says "left open for design"; R3 says
   "a freshly constructed keeper".
4. **Under the §7 overflow cut (1b)**, the doc 02 two-writer sentence, the §7 "digest then
   reads" sentence and the R9/R10 digest wording move to 1b. Link 1's doc text says the
   incumbent is "written by `/ui`'s Today view; the digest becomes the second writer in 1b".
5. **FX-L** trigger units use a type in neither Kind (for example `knowledge`), so they never
   enter the focus pool and cannot perturb the incumbent under test.
6. **Naming and edit counts.** The constructor parameter is `keeper`, not `focus`, which would
   shadow `internal/core/focus`. The serve gate is a NEW test file (`cmd/nooma/serve_gate_test.go`
   does not exist). The build-tagged call-site edits are 6 edits in 5 files (`integration`:
   `due_scan_status_vocabulary_test.go`, `due_scan_concurrent_test.go` ×2, `pending_question_vocabulary_test.go`;
   `e2e`: `m3_demo_test.go`, `m3e_relation_answer_demo_test.go`).
