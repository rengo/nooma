# Design — m4e: beliefs

Technical design for `m4e-beliefs-activity-admin`, the fifth of six slices sharing
[`m4-mirror-ui/proposal.md`](../m4-mirror-ui/proposal.md). Requirements are `spec.md` (R1-R4, R9,
R11-R13, owner rulings 2026-10-07). This document decides HOW and closes OQ8 (OQ4 moved to `m4e-activity`). Shape
follows the archived [`m4c` design](../archive/2026-10-07-m4c-focus-hysteresis/design.md) and
[`m4b` design](../archive/2026-09-29-m4b-units-capture/design.md).

> **Split on 2026-10-08 (umbrella rule, owner decision).** The first version of this design
> covered beliefs, activity and admin in seven PRs (~1,890 lines, thin margins). Splitting the
> activity PR into "newest-first read" and "activity view" makes eight implementation PRs, so
> the umbrella rule (proposal.md ~:301, more than seven PRs or more than 2,400 budgeted lines)
> fires on measurement. **Admin is now [`m4e2-admin`](../m4e2-admin/design.md)**: §3.7, §3.8, the
> admin rows of §3.9, gate G5 and G4, the admin mutants (C-series), the admin PRs and their
> findings moved there. The directory name is kept because other artifacts reference it; the
> scope was beliefs + activity and **is now beliefs only** (see the next note). **Depends on
> nothing from m4e2.** m4e2 depends on this slice for `brain.ErrWriteLanded` /
> `*WriteLandedError` (PR 3), the cross-origin body table (§3.12 G6), the I22 whitelist test (G12)
> and the `ui.Deps` / `uiDeps` / `wiring.go` pattern with the layout nav (PR 4), and the
> action-vocabulary file; it depends on [`m4e-activity`](../m4e-activity/design.md) for
> `DecisionLog.Before`, the change decoder (§3.6 there) and the `config.updated`-shaped fixture
> (A11), matching `../m4e2-admin/design.md`'s header.
>
> **Planning-PR task list (recorded here, not done by this document; the umbrella is not edited
> now).** Every spot of `m4-mirror-ui/proposal.md` that still puts admin inside m4e, found by
> `rg 'm4e|admin'` at `dc22762`, is updated by the planning PR (m4e narrowed, `m4e2-admin` added
> after m4e, OQ7 closed):
> 1. slicing paragraph (~:292-294): `/ui/admin` leaves m4e; the "split off as `m4e2-admin`" rule
>    (~:301) is recorded as exercised;
> 2. §5.1 `m4e` rows (~:334-339) and totals: six m4e rows and the `m4e2-admin` rows, with the
>    budgets of design §7 here and m4e2's §7;
> 3. §2 items 3-4 (~:104-111): "a config write (m4e)" becomes m4e2;
> 4. **§2 acceptance line (~:73-75)**: "`/ui/admin` shows and edits what §Q5 rules" is attributed to
>    `m4e2-admin`, and "lines 647-648 corrected" becomes `663-664` (finding 5 below);
> 5. §4.2 row "Newest-first activity" (~:235): the same `647-648` -> `663-664` correction;
> 6. **§5.2 test rows 9-10 (~:373-374)**: "m4e #2" and "m4e #4" are renumbered to this design's
>    PR 3 (belief edit/retire, I03/I12) and PR 6 (activity view, I23 read-only);
> 7. **Q5 header (~:462)**: "blocking `m4e` #5" becomes "blocking `m4e2-admin`";
> 8. **independence paragraph (~:261-262)**: it lists `ConfigRepo` under m4e. m4e touches
>    `SelfModelRepo` and `DecisionLog`; `m4e2-admin` touches `ConfigRepo` and `RelationRepo`;
> 9. dependency rows (~:258, ~:520-521): `m4e2-admin` after m4e, and m4f stays on m4e only;
> 10. risk row R10 (~:505): "m4e #3" becomes PR 5 of this design;
> 11. Q4 and Q5 rows (~:483-484) marked Ruled, 2026-10-07.

> **Split again on 2026-10-08 (the owner's 7-PR rule, after PR 2 measured 2.06x).** The
> pre-agreed rule (this document's own §7, "the next remedy"): if growth forces cuts and the PR
> count passes seven, split beliefs (PRs 1-4) from activity (PRs 5-6) into two changes. PR 1
> measured 1.2x (318 changed lines against ~265) and PR 2 measured 2.06x (557 against ~270); PR 2
> was cut into 2a (#291, the pure shield) and 2b (#292, the derive wiring, `size:exception`). At
> 2.06x, PRs 3, 4 and 6 are ~536, ~567 and ~597 and would each need a cut. **Activity is now
> [`m4e-activity`](../m4e-activity/design.md)**: §3.5, §3.6, the activity row of §3.9, the
> activity rows of §3.11 and §3.12 (G7, G8, G11, G12), FX-A, the A-series mutants, the old PRs 5-6,
> RK-7's decoder half, RK-8, RK-9 and the activity items of §10 **moved there with their text**.
> Each place leaves a one-line pointer, and **section, finding, gate, mutant, risk and PR numbers
> are kept** (3.5 and 3.6 exist here only as pointers; PR 5 and PR 6 belong to `m4e-activity`).
> **This design's scope is beliefs only**: PRs 1, 2 (as 2a and 2b), 3 and 4. The measurements and
> the PR 2 cut are in [`tasks.md`](tasks.md)'s forecast section.

> **Findings this design made that the spec did not anticipate**, named up front:
> 1. **Today's create path would revive a retired belief AND overwrite an edited one.**
>    `UpsertByTopicKey` sets `status = excluded.status` and `origin = excluded.origin` on conflict
>    (`internal/store/sqlite/selfmodelrepo.go:68-76`), so a proposal sharing a retired belief's
>    `topic_key` flips it back to `active`. R12 and R13 therefore need a **store-level guard**,
>    not only a brain-side check (§3.2).
> 2. **Key-only matching (OQ8) cannot honour the ruling.** The derive prompt shows the judge only
>    *active* beliefs (`consolidate.go:618-625`), and the key is the judge's free text. A retired
>    belief is invisible to the judge, so it is re-proposed under whatever key it picks that
>    night. That is the "re-retire every week" failure the ruling exists to prevent (§3.3).
> 3. **The I23 gate matches by selector name, not receiver.**
>    `test/conformance/i23_correction_audit_precedes_edit_test.go:17-19` forbids any call named
>    `UpdateContent`/`UpdateEventAt`/`UpdateDueAt` outside `dispatchEdits`. A belief method named
>    `UpdateContent` would turn R6's "untouched and green" red. The method is `EditContent`.
> 4. **One `Now()` per file in `internal/brain`** (`brain_single_clock_read_test.go:23-29`). Edit
>    and retire cannot share one file with one clock read each. Each operation gets its own file
>    (§3.4).
> 5. *Moved to [`m4e-activity`](../m4e-activity/design.md) (finding 5 there; the number is kept
>    so a citation stays valid).*
> 6. **Migration 0001 has no `self_beliefs.status` vocabulary comment** (`0001:81`), and a
>    published migration is never modified. The Go↔text vocabulary pin
>    (`unit_status_ddl_test.go`'s precedent) reads doc 03 instead. Doc 03's DDL comparator strips
>    comments (`test/support/schema/markdown.go:256`), so the new comment does not break the
>    schema golden.
> 7. **The cross-origin gate counts `Capturer` calls only and posts one fixed body.** Its
>    "same-origin with the right cookie reaches the target exactly once" subtest
>    (`test/conformance/ui_cross_origin_test.go:116-127`) fails for every new POST row whose
>    target is not `Capture`, and it sends `text=hello` to every row, so a belief edit (which
>    reads `content`) would fail on a parse error and prove nothing about the guard. It needs a
>    counting stub per mutating entrance **and a per-row valid body table** (§3.12, G6).
> 8. *Moved to [`m4e-activity`](../m4e-activity/design.md) (finding 8 there; the number is kept
>    so a citation stays valid).*
> 9. **`MergeProposals` fails the whole call on a non-finite vector on the existing side**
>    (`internal/core/consolidation/derive.go:91-110`, `:140`), and it returns only the single nearest
>    neighbour, so it cannot express a tie between an active and a retired neighbour. Both shape
>    the shield (§3.3).
> 10. **Nothing injects beliefs into classify today.** `capture.go:209` calls
>     `classify.BuildPrompt(in.Text, nil, …)`. Doc 02 §10's "Injection into classify" is a design
>     statement, not shipped behaviour; the doc edit is worded as forward-looking (§3.11).

---

## 1. Ground truth (read at `dc22762`)

| Claim | Verified at |
|---|---|
| `SelfModelRepo` has three methods: `ActiveBeliefs` (`status = 'active'`), `UpsertByTopicKey` (conflict on UNIQUE `topic_key`, overwrites content/origin/status), `ReinforceByID` (`WHERE id = ?` only) | `internal/ports/selfmodelrepo.go:41-69`; `internal/store/sqlite/selfmodelrepo.go:37-102`; `0001:75` |
| The only producer of beliefs is derive (`Origin: "derived"`, `Status: "active"`). No seed or user-stated row exists in any vault today | `internal/brain/consolidate.go:694-705`; `rg 'Origin:'` (no other non-test producer) |
| `ActiveBeliefs` has two production callers: derive's dedup and stagnation's read | `consolidate.go:618`, `:1192` |
| Classify receives `nil` beliefs; nothing reads `self_beliefs` for injection | `internal/brain/capture.go:209` |
| Derive embeds every active belief per pass **even when there are no proposals**, runs `MergeProposals` (nearest at cosine ≥ `BeliefMergeCosine` 0.85), and routes `MergeInto == ""` to create and otherwise to reinforce. An embed failure of any belief or proposal aborts the phase. **Pinned by `TestConsolidateRunner_Derive_EmbedsExactlyOncePerActiveBelief` (`consolidate_test.go:1824-1871`) and `embedForMerge`'s comment (`:839-852`)** | `consolidate.go:618-678`, `:853-876`; `internal/core/consolidation/derive.go:21`, `:140-193` |
| `MergeProposals` returns the single nearest neighbour per proposal (`scored[0]`); a non-finite existing vector returns an error | `derive.go:91-110`, `:140-193` |
| `decision_log`'s layout and `occurred_at` resolution, `DecisionLog`'s `Record` and `Since`, `memrepo.DecisionLog`'s ordering | Moved to [`m4e-activity` design §1](../m4e-activity/design.md) |
| The vocabulary is forty-eight members, hand-mirrored in `repocontract` (count in a doc comment and in a subtest title) | `decisionlog.go:169-204`; `test/support/repocontract/decisionlog.go:133` |
| Consolidation writes no run-level `decision_log` row: every `consolidate.*` action is a per-effect row | `internal/ports/decisionlog.go:109-149`; `consolidate.go:1107` writes only `config` |
| `BrowsePageSize = 50` is a **transport constant**, explicitly not a §13 row | `internal/ports/unitrepo.go:254-257` |
| UI leaves are listed twice: `newUIMux` registrations and `Handler.ServeHTTP`'s `r.Pattern` switch, pinned by `wantUIMuxWiring` and `TestUIGuardedLeavesEachReachAView` | `internal/httpapi/server.go:150-173`; `internal/ui/ui.go:94-111`; `test/conformance/httpapi_ui_wiring_test.go:158-174` |
| The I22 reflection gate whitelists exactly `{Today, Browse, Detail, ForText, Capture}` as methods any `ui.Deps` interface field may expose | `test/conformance/ui_entrances_test.go:32` |
| `uiDeps` takes `(today, units, recall, capture, serving)` and exists to keep typed-nil services out of interfaces; `wireUnits`/`wireToday` are the constructor precedent, `wiring_units_test.go` the test precedent | `cmd/nooma/serve.go:285-300`; `cmd/nooma/wiring.go:310-352`; `cmd/nooma/wiring_units_test.go:14` |
| Mutating UI handlers: `MaxBytesReader` at 64 KiB, an HTMX fragment on `HX-Request`, the full page otherwise | `internal/ui/capture.go:14`, `:32-44`, `:105-112` |
| `ADR-0016`'s ordering (pre-image row first, then the write, then the signal) is the correction path's | `docs/adr/0016-correction-pre-image.md:63-65`; `internal/brain/correction.go:193-202` |
| `internal/brain` has no logger; `internal/ui` uses `log/slog` | `rg 'log/slog'`: `internal/ui/{ui,units,capture}.go` only |

**New calibratable constants: none** (§3.4 rules the content bound an input bound, not a §13
row). **New migration: none.** **New ADR: none.** Doc 02 gains text in §6 item 5, §10 and §11
(§3.11); §5 step 4 is `m4e-activity`'s.

---

## 2. What m4e decides, in one paragraph

The user's word is protected at the **store**, not just in the brain. Retire is a `from`-guarded
status write. An edit is a compare-and-swap that marks `origin = user_stated` inside its own SQL,
on content normalised once and bounded by a named constant. The create path overwrites a row only
while that row is `active` and `derived`, and reinforce touches only `active` rows. So no brain
bug can revive a retired belief or rewrite the user's text. On top of that guard, one pure core
function, `consolidation.RouteProposals`, decides each derive proposal: skip (it hits a retired
belief by key, or a retired belief is at least as near as any active one at the existing 0.85),
reinforce (nearest active, or the key of a user-stated belief), or create. Retired beliefs are
embedded each night alongside active ones, but only when a proposal still needs a semantic
comparison, and a retired belief that fails to embed degrades to key-only matching with a logged
row, never to a failed pass and never to re-derivability (OQ8). Activity is `m4e-activity`
(the newest-first `DecisionLog.Before` read and the view). Admin is `m4e2-admin`.

---

## 3. Decisions

### 3.1 Vocabulary: `selfmodel.Status` and `selfmodel.Origin`

`internal/core/selfmodel/status.go` adds `Status` (`active|retired`) and `Origin`
(`seed|derived|user_stated`) as defined string types, with `AllStatuses()`/`AllOrigins()`
functions (the `Facet` house pattern, `facet.go:10-43`). `ports.Belief.Status` and `.Origin`
take those types. Untyped literals at existing call sites still compile. `scanBelief` scans both
through a local string, as it already does for `facet`.

| Option | Verdict |
|---|---|
| **Typed core vocabulary + doc 03 comment pinned by a test** — chosen | A status added in Go and not in doc 03 (or the reverse) fails `TestBeliefStatusDocMatchesAllStatuses` (G3) |
| Keep `string` with literals | Rejected. `"retired"` would live as a bare literal in SQL, memrepo, brain and ui, and nothing ties them together |
| `archived` as the name | Ruled out (OQ2). `archived` means *cold* for units |

### 3.2 `SelfModelRepo`: four new methods, two guarded contracts

```go
BeliefByID(ctx context.Context, id string) (Belief, error)          // ErrBeliefNotFound
RetiredBeliefs(ctx context.Context) ([]Belief, error)                // status = 'retired', a name, never a status param
SetStatus(ctx context.Context, id string, from, to selfmodel.Status, at time.Time) error
EditContent(ctx context.Context, id, from, to string, at time.Time) error

var ErrBeliefStatusConflict = errors.New("belief is not in the expected state")
var ErrBeliefProtected      = errors.New("belief is not derived-and-active; derive may not overwrite it")
```

- **`SetStatus`** is `UnitRepo.SetStatus`'s shape (`internal/store/sqlite/unitrepo.go:164-185`):
  a `SELECT status … WHERE id = ?` **first** (no row → `ErrBeliefNotFound`; `status != from` →
  `ErrBeliefStatusConflict`), then `UPDATE … SET status = ?, updated_at = ? WHERE id = ? AND
  status = ?` whose result goes through `requireRowAffected(res, ErrBeliefStatusConflict)`, which
  covers a writer landing between the SELECT and the UPDATE. No legality table in core: one transition exists and has one caller, and a `ValidateTransition`
  with a single row would be a function with nothing to decide.
- **`EditContent`**: `UPDATE … SET content = ?, origin = 'user_stated', updated_at = ? WHERE id =
  ? AND status = 'active' AND content = ?`. The origin is a **literal in the statement**, and the
  signature has no origin parameter, so an edit that does not mark the row is not expressible
  (R2, R13). `content = from` is a compare-and-swap, so the pre-image row (§3.4) names exactly
  what was overwritten. The same SELECT-first shape: not found, then status or content mismatch
  (`ErrBeliefStatusConflict`), then the UPDATE with `requireRowAffected` for the race.
- **`UpsertByTopicKey`** (changed contract): `ON CONFLICT (topic_key) DO UPDATE SET … WHERE
  self_beliefs.status = 'active' AND self_beliefs.origin = 'derived'`. Zero rows affected →
  `ErrBeliefProtected`. An insert, or an overwrite of an active derived row, behaves exactly as in
  m2c R2.1. **Probe at L3 before relying on it:** SQLite must report `changes() = 0` when the
  `DO UPDATE … WHERE` is false. Tasks write that L3 case first.
- **`ReinforceByID`** (changed contract): adds `AND status = 'active'`. A retired id →
  `ErrBeliefStatusConflict`, a missing id stays `ErrBeliefNotFound` (the same disambiguating
  SELECT as `SetStatus`).

| Option for "derive never touches a retired or edited row" | Verdict |
|---|---|
| **Read-then-route in brain AND a guarded write in the store** — chosen | Routing gives the right *decision* (skip vs reinforce, each logged). The guard makes the wrong *write* unexpressible, including under a UI race between derive's read and its write |
| Brain read-then-route only | Rejected. A retire landing between `ActiveBeliefs` and `UpsertByTopicKey` revives the row: an invisible property with no gate |
| Guarded write only (no routing) | Rejected. A key collision with a user-stated belief must *reinforce* it (R13), which a refused write cannot express |
| `INSERT … DO NOTHING` (never overwrite) | Rejected. It silently changes m2c R2.1 for active derived rows, which is out of scope |

### 3.3 OQ8 and the derive shield (R12, R13)

**OQ8 — chosen: embed retired beliefs and match them semantically, in addition to the
mandatory key match.**

| Option | Cost | Honours "the user's word wins"? | Verdict |
|---|---|---|---|
| Key match only | 0 calls | Only when the judge happens to reuse the same free-text key. The judge never sees retired beliefs (finding 2), so the likely outcome is a new key every night | Rejected. It is the failure the ruling names |
| **Key match + embed retired, compared at the existing `BeliefMergeCosine`, retired winning a tie** — chosen | + one embed call per retired belief per pass that still has a proposal needing a comparison. Retired beliefs only grow by an explicit user click. This is the same growth argument doc 02 §6 item 5 already accepts for active beliefs (`:965-972`) | Yes. A proposal that would have *merged into* the retired belief **is** that belief by the project's own definition of "same belief" | **Chosen** |
| Put retired beliefs in the derive prompt ("do not propose") | 0 calls, more tokens | Probabilistic; changes `BuildDerivePrompt` and its LLM fixtures | Rejected. Not deterministic, and a gate cannot verify a model's obedience |
| A separate, lower "retired shield" cosine | — | — | Rejected. It is a new §13 number with no data behind it |

**Routing is a core decision** (nooma-core: "decides from input data"):

```go
// internal/core/consolidation/shield.go
type RouteKind string
const (
	RouteCreate      RouteKind = "create"
	RouteReinforce   RouteKind = "reinforce"
	RouteSkipRetired RouteKind = "skip_retired"
)
type KeyedBelief struct { ID, TopicKey string; Origin selfmodel.Origin }
type Route struct {
	ProposedIndex int
	Kind          RouteKind
	BeliefID      string  // reinforce target or the retired belief; "" for create
	Reason        string  // "retired_topic_key" | "retired_similar" | "" otherwise
	Similarity    float64 // set only for a semantic match
}
// RetiredKeyHits returns, per proposal index, the id of the retired belief whose key the
// proposal derives. Rule 1 only; needs no vectors.
func RetiredKeyHits(proposedKeys []string, retired []KeyedBelief) map[int]string
// activeMerges and retiredMerges are two separate MergeProposals results, each indexed by the
// ORIGINAL ProposedIndex; an index decided by rule 1 has no entry in either.
func RouteProposals(activeMerges, retiredMerges []MergeDecision, proposedKeys []string, active, retired []KeyedBelief) []Route
```

**Two `MergeProposals` calls, not one over a union.** `MergeProposals` keeps only the nearest
neighbour (finding 9), so over `active ∪ retired` a tie at equal cosine is resolved by
`recall.Search`'s incidental ordering, not by a rule. Running it once over the active vectors and
once over the retired vectors hands `RouteProposals` both nearest neighbours *with their
similarities*, so the tie is broken by an explicit comparison, in the one pure function the rule
lives in. Two in-memory index builds are cheap beside the provider calls.

Precedence, per proposal *i*, first match wins:

1. `proposedKeys[i]` equals a retired belief's key → **skip**, `retired_topic_key` (R12 MUST).
2. `retiredMerges` qualifies for *i* (a retired belief is its nearest retired neighbour at ≥ 0.85)
   **and** (`activeMerges` does not qualify, **or** `retired.Similarity >= active.Similarity`) →
   **skip**, `retired_similar`. **A tie goes to retired**: the user's word wins.
3. `proposedKeys[i]` equals an active belief whose origin is not `derived` → **reinforce** that
   belief (R13: a key collision with the user's text is a merge, never an upsert, and it wins
   even when another active belief is semantically nearer).
4. `activeMerges` qualifies → **reinforce** it (today's path).
5. Otherwise → **create** (an active *derived* key collision still overwrites in place, as m2c
   R2.1, and the store guard permits exactly that).

Nearest-wins at steps 2/4 rather than "any retired neighbour ≥ 0.85 skips": if an active belief
is strictly nearer, the proposal is that active belief, and reinforcing it revives nothing. Step
3 sits above step 4 on purpose: an edited belief's key is the user's word, so it is not
overridden by a nearer active neighbour.

**Brain** (`consolidate.go` `derive`):

1. Read `ActiveBeliefs` then `RetiredBeliefs` (an error aborts the phase, as every other read does).
2. Judge, decode proposals, compute each proposal's key (`consolidation.DeriveTopicKey`).
3. `hits := RetiredKeyHits(...)`. `pending` = the proposal indices not in `hits`.
4. **If `pending` is empty, make no embedding call at all** (this covers "no proposals", the
   common quiet night, and "every proposal is already decided by key"). Go to step 7 with empty
   merges. The vectors were only ever an input to `MergeProposals`, which has nothing to compare
   when no proposal is pending. **This changes pinned behaviour, and the pins are rewritten
   explicitly in PR 2** (an earlier draft of this design wrongly said no test pins the
   unconditional embed):
   - `TestConsolidateRunner_Derive_EmbedsExactlyOncePerActiveBelief`
     (`internal/brain/consolidate_test.go:1824-1871`; fixture `{"beliefs":[]}`, two active
     beliefs, asserts `EmbedCalls() == 2` with the message "spec R5.7") is **rewritten, not
     deleted or weakened**: same fixture, renamed `TestDerive_NoProposalsMakesNoEmbedCalls` (D15),
     asserting `EmbedCalls() == 0`. A sibling case is added (D17): the same two active beliefs,
     one retired belief and **one pending proposal** (its key decided by no rule), asserting
     `EmbedCalls() == active + retired + pending == 4`.
   - `embedForMerge`'s doc comment (`consolidate.go:839-852`: "every entry in active is embedded
     exactly once, unconditionally ... regardless of whether proposals is empty") is rewritten to
     the new rule, and the function takes the retired beliefs and only the `pending` proposals.
   - **Justification (nooma-testing rule 4, non-negotiable 4).** The old assertion encodes m2c
     R5.7 (`archive/2026-08-11-m2c-consolidation-runtime/spec.md:573`) and doc 02 §6 item 5
     ("embeds every active belief in memory at the start of the phase"). The two legitimate exits
     are to fix the code or to change doc 02 and its ADR in the same PR; this is the second. No
     ADR governs belief embedding (`rg -il belief docs/adr` names none that does), so the doc 02
     §6 item 5 amendment (§3.11, row PR 2) is the whole governing change, and it lands **in PR 2
     with the test rewrite**. Both appear in PR 2's file table (§5).
5. Otherwise embed the active beliefs (a failure aborts, as today), then the **retired**
   beliefs under the failure policy below, then only the `pending` proposals.
6. `MergeProposals(model, activeVecs, pendingVecs)` and `MergeProposals(model, retiredVecs,
   pendingVecs)`, results remapped from the compact `pending` position back to the original
   proposal index.
7. `RouteProposals`; persist by `Kind`.

**Retired-embedding failure policy (decided, not left open).** `MergeProposals` fails the **whole
call** on any vector that `recall.Normalize` or `recall.NewVectorIndex` rejects (finding 9). So
a retired belief is **dropped from the retired vectors** for that pass, matching nothing
semantically, when any of these holds:

| Cause | What it is | Would otherwise |
|---|---|---|
| `embed_error` | its `Embed` call returned an error | abort the phase |
| `non_finite` | a NaN or ±Inf component (`recall.ErrNonFiniteVector`) | `Normalize` error: `MergeProposals` fails (`derive.go:144-150`) |
| `zero_vector` | zero magnitude, or an empty vector (`recall.ErrZeroVector`, `recall/vector.go:60`; `Normalize` returns it at `:148-150`) | `Normalize` error: `MergeProposals` fails |
| `dimension_mismatch` | its length differs from the reference dimension (the first active belief's vector when active beliefs exist, otherwise the first usable pending proposal's vector). Within the retired set this is `recall.ErrRaggedVectors` (`vector.go:79-91`, from `NewVectorIndex`); against the query it is `ErrDimMismatch` (`vector.go:104-106`, from `Search`) | `NewVectorIndex`/`Search` error: `MergeProposals` fails |

One brain helper, `usableVector(v, dim) (cause string)`, screens each retired vector (and each
pending proposal, below) before `MergeProposals`; it is the single place the list lives. For each
dropped retired belief one row, `ActionDeriveRetiredEmbedFailed`
(`consolidate.derive.retired_embed_failed`), is recorded with context `{belief_id, topic_key,
cause}`. The pass continues. The belief is **still matched
by key** (rule 1 needs no vector), and the store guard still refuses a write over it, so a failed
embedding can never make a retired belief re-derivable by key. The honest residue: on a night its
embedding fails, a *semantically* similar proposal under a *new* key is not caught and is created;
that is logged, bounded by the 1,000-rune content bound (§3.4) which makes the failure unlikely
for any belief the user edited, and it ends when the provider recovers. Aborting instead would
make one unembeddable retired belief fail derive every night until someone intervened, from a
UI that has no un-retire. A context cancellation (`ctx.Err() != nil`) is **not** this policy:
it aborts the phase like any other cancelled read. An **active** belief or a proposal that
fails to **embed** keeps today's behaviour (abort).

*The proposed-side residue, verified against `derive.go:161-170`, comment `:104-120` (not as first assumed).*
`MergeProposals` treats only `ErrNonFiniteVector` on a **proposed** vector as "resolve to create
and continue" (a possible duplicate belief, never a wrong merge; today **unlogged**). A **zero**
proposed vector is *not* recognised there: `Normalize` returns `ErrZeroVector`, `MergeProposals`
returns it, and the derive phase aborts; a dimension mismatch against the index aborts the same
way (`Search`'s `ErrDimMismatch`). Decision: the same `usableVector` screen runs on every pending
proposal vector. A proposal whose vector is unusable (`non_finite`, `zero_vector`,
`dimension_mismatch`) is excluded from **both** `MergeProposals` calls, so it has no semantic
match on either side, and it falls to rules 3 and 5: reinforce a user-stated key, or create. This
turns the zero and dimension cases from a phase abort into a create, deliberately and only for
proposals. It is **logged**: the `belief_created` (or `belief_reinforced`) row's rationale
states "semantic comparison skipped: proposal vector unusable (<cause>)", so no new action and no
context-shape change is needed. A proposal's `embed_error` still aborts (above).

A skip records one row, `ActionDeriveBeliefSkipped` (`consolidate.derive.belief_skipped`), with
context `{topic_key, belief_id?, reason, similarity?, proposed_content}`, keyed by what is true:
`topic_key` is always the **proposal's** computed key; `belief_id` is the belief that decided the
skip, **omitted** when the store refused without naming one; `similarity` is **omitted** for a
key match (§5 step 4's "an absent key is the truth" rule). The `retired_*` reasons are reserved
for retired beliefs: `retired_topic_key`, `retired_similar`; the two race arms use
`changed_since_read` and do not abort the pass, per doc 02 §11's "a scan-time conflict is
recorded and skipped": `ErrBeliefProtected` from the create write (no `belief_id`), and
`ErrBeliefStatusConflict` from the reinforce write (`belief_id` = the target).
`BuildDerivePrompt` is unchanged: the prompt still shows active beliefs only.

One skip action, not three: the reasons share one context shape (m2c §7.5's split rule applies
only when shapes differ); the embed-failure row has a different shape (it concerns a belief, not
a proposal), so it is its own action. A retired belief re-proposed every night writes one skip
row per night. That is the trace the owner asked for (R12).

### 3.4 Belief edit and retire (R2, R3)

`brain.BeliefsService{clock, ids, beliefs SelfModelRepo, signals SignalRepo, log DecisionLog}`,
one file per operation (finding 4): `beliefs.go` (`ByFacet`, no clock), `belief_edit.go`,
`belief_retire.go`.

**Content: one normalisation, one bound.** `internal/core/selfmodel/content.go` (pure, L1):

```go
const MaxBeliefContentRunes = 1000
func NormalizeText(raw string) string             // total: \r\n -> \n, TrimSpace; never errors
func NormalizeContent(raw string) (string, error) // NormalizeText + ErrEmptyContent, ErrContentTooLong
```

`NormalizeText` replaces `\r\n` with `\n` and applies `strings.TrimSpace`. `NormalizeContent` is
`NormalizeText` followed by rejecting an empty result and a result of more than
`MaxBeliefContentRunes` runes (`utf8.RuneCountInString`, so a multibyte rune counts once).
`NormalizeContent` runs **once**, on the *submitted* text at the top of `Edit`; the normalised
value is the one compared, stored, logged and echoed back. A browser submits a textarea's newlines
as CRLF, so without it an unchanged resubmission of a multi-line belief would never be a no-op.
**The stored text is normalised with `NormalizeText` only, never with `NormalizeContent`**:
derived content is unbounded and may be blank-looking, so validating it would return
`ErrContentTooLong` or `ErrEmptyContent` for a belief the user is trying to fix, and an over-bound
derived belief could never be edited.

| Question | Decision |
|---|---|
| Is `MaxBeliefContentRunes` a §13 row? | **No. Ruled an input bound, the `ports.BrowsePageSize` class** (`unitrepo.go:254-257`): it validates what a form may carry and decides nothing doc 02 governs (no relevance, weight or lambda moves with it). Its doc comment cites that ruling. If the owner prefers a §13 row, it is one table row and a doc 02 edit in PR 1; this is flagged for the owner, not a blocker |
| Why 1,000? | A belief is a sentence or a short paragraph, not a document; the bound exists so one form field cannot make the nightly embed cost unbounded (§3.3) and the activity row legible. It is chosen, not calibrated, which is exactly why it is not a §13 row |
| Why runes, not bytes? | The ruling is about what a person can write; bytes would make the same sentence legal in English and illegal in Japanese |
| Does it bound derived beliefs? | No. The judge's text is unbounded today and unchanged; an over-bound derived belief renders fine, and any edit of it is bound-checked like any other |

| Operation | Order | Why this order |
|---|---|---|
| `Edit(ctx, id, raw)` | `NormalizeContent(raw)` (→ `ErrEmptyContent`, `ErrContentTooLong`, no I/O) → `BeliefByID` (not found → `ErrBeliefNotFound`; retired → `ErrBeliefStatusConflict`) → normalised equals `NormalizeText(current.Content)` (**non-validating**, see above) → **no-op, writes nothing** → `Record(belief.edited)` **pre-image** → `EditContent(id, current.Content, normalised, now)` → signal `belief_edit` | An edit **overwrites** the user-visible text and a belief has no history table. ADR-0016's reasoning (record first; a failed record means no write) applies, by analogy rather than by its scope. The read first gives R2 its "unknown id writes nothing" without an orphan row. The no-op compares against the stored text normalised too (by `NormalizeText`), so a derived belief whose stored text has a trailing space is not "edited" by resubmitting its visible text, and an over-bound derived belief can still be edited |
| `Retire(ctx, id)` | `BeliefByID` (not found / not active → conflict before any write) → `SetStatus(id, active, retired, now)` **first** → `Record(belief.retired)` → signal `belief_delete` | A transition destroys nothing: `from`/`to` *are* the pre-image, and the content stays in the row. See the failure windows below |

**Failure windows, stated for each ordering.**

*Edit (record-first, ADR-0016).* (a) `Record` fails: nothing is written, the error is returned.
(b) `Record` lands, `EditContent` fails (a concurrent retire or derive between the read and the
CAS): a row exists describing an edit that did not land, no signal is written; the rationale says
"about to replace". **This window is the accepted ADR-0016 class, stated as such, not a defect to
close**: ADR-0016 (`docs/adr/0016-correction-pre-image.md:63-65`) orders pre-image, write, signal
and accepts a pre-image row whose write then fails; conflicts here are user-versus-nightly-pass
only. B5 pins the half that matters (no signal follows a failed write); no test asserts the
orphan row away. (c) `EditContent` lands, the signal fails: the edit and its row stand, the
signal is missing; the error is `ErrWriteLanded` with `Signal` set (B15).

*Retire (write-first).* The choice is write-first with a **non-fatal** follow-up, not
record-first. The pre-image of a retire is trivial (`active` and the untouched row), so
record-first buys nothing; it would cost R3's "a conflict writes no log row" under a race (the
read passes, `SetStatus` loses, and a row asserts a retirement that did not happen). A false row
is an assertion in the audit trail; a missing row is an absence, and an absence is the lesser
lie. (a) `SetStatus` fails or conflicts: nothing else happens. (b) `SetStatus` lands, `Record`
fails: the belief is retired with no row; the signal is still written (without `decision_id`),
because M5 should learn the user's act. (c) Row lands, signal fails: row without signal; the
error is `ErrWriteLanded` with `Signal` set, pinned by B17
(`TestBeliefRetire_SignalFailureAfterWriteIsNonFatal`), because B14 covers window (b) only and
without B17 the plain-error mutant of (c) survives.

*Follow-up failures are non-fatal and visible, and the notice names what actually failed.* In (c)
for edit and in (b)/(c) for retire the user's act **did land**, so reporting an error would invite
a retry that then conflicts. The service returns `*brain.WriteLandedError{Record, Signal bool,
Err error}` (`Record`: the log row is missing; `Signal`: the signal is missing), with
`Is(ErrWriteLanded)` true so `errors.Is` works, and `As` gives the UI the parts. The UI treats it
as success, `slog.Warn`s the cause (`internal/brain` has no logger; `internal/ui` does), and
chooses the notice **per window** so it never claims the record failed when only the signal did:

| Missing | Windows | Notice |
|---|---|---|
| `Record` only | retire (b) | "Saved, but the activity record of it could not be written." |
| `Signal` only | edit (c), retire (c) | "Saved and recorded, but the learning signal could not be written." |
| both | retire (b) with the signal also failing | "Saved, but neither the activity record nor the learning signal could be written." |

A test pins each window (§6, B-series; U2 pins the notice per variant). `WriteLandedError` and
`ErrWriteLanded` live in `internal/brain/write_landed.go` and are reused by `m4e2-admin`
(`Record` only: "saved, but not logged").

**Signals** (doc 02 §9): `Type` `belief_edit` / `belief_delete`; `Valence` **negative** (the
belief was wrong, as for `correction`, `correction.go:336-345`); `TargetKind` `belief`;
`TargetID` the id; `DecisionAction` = `ActionDeriveBeliefCreated` **when the belief's origin at
that moment was `derived`** (the bucket that produced it), otherwise nil. Nil is the D6 rule:
leave a field nil rather than guess. `Magnitude` is nil. `Context` is `{belief_id, topic_key,
decision_id}`, linking the log row (`decision_id` omitted when the row failed).

**Log contexts**, keyed by column name (§5 step 4's rule):
`belief.edited` → `{belief_id, topic_key, fields:["content","origin"], previous:{content, origin},
next:{content, origin:"user_stated"}}` (`content` is the normalised value); `belief.retired` →
`{belief_id, topic_key, content, from:"active", to:"retired"}`. One clock read per operation feeds
`updated_at`, the row and the signal.

**`ByFacet` order is total.** Within a facet, beliefs are ordered by `confidence` DESC, then
`last_reinforced_at` DESC, then `id` ASC. The sort is done **in brain**, not in the store: the
SQLite `ActiveBeliefs` has no `ORDER BY` (`selfmodelrepo.go:37-39`, it is `… WHERE status =
'active'`) and `memrepo.SelfModel.ActiveBeliefs` iterates a Go map (`memrepo/selfmodel.go:46`), so
without the sort the page order is storage-dependent and, in tests, random from run to run.
`sort.SliceStable` over a comparator ending in `id` is a total order, so stability adds nothing but
is harmless.

The brain signature `Edit(ctx, id, content string)` has **no facet or confidence parameter**, so
R2's "a POST carrying `facet` is ignored" is structural at the brain boundary, not a handler
discipline.

### 3.5 OQ4 — the newest-first read and its page

**Moved to [`m4e-activity` design §3.5](../m4e-activity/design.md).**

### 3.6 Activity rendering (R6)

**Moved to [`m4e-activity` design §3.6](../m4e-activity/design.md).**

### 3.7 Admin writes, 3.8 admin read

**Moved to [`m4e2-admin` design](../m4e2-admin/design.md) §3.1 and §3.2.**

### 3.9 UI routes

| Pattern | Guarded | Brain call | Errors |
|---|---|---|---|
| `GET /ui/beliefs` | yes | `ByFacet` | 503 nil dep; 500 |
| `POST /ui/beliefs/{id}/edit` | yes | `Edit(id, content)` | 400 empty/too long; 404 not found; 409 conflict (incl. retired); success on `ErrWriteLanded` with the notice for the missing part (§3.4 table) |
| `POST /ui/beliefs/{id}/retire` | yes | `Retire(id)` | 404; 409; success on `ErrWriteLanded` with the notice for the missing part |

(`/ui/activity`: `m4e-activity`; `/ui/admin` routes: `m4e2-admin`.) Every POST answers with an HTMX fragment on `HX-Request` and
the full page otherwise (m4b's split). Bodies are bounded by `MaxBytesReader` at the existing
64 KiB. `ui.Deps` gains `Beliefs`, a narrow interface (m4a §3.1). The layout nav gains the
beliefs link in PR 4 (`m4e-activity` adds the activity link in its own PR 6).

**Wiring (`cmd/nooma`).** `wiring.go` gains `wireBeliefs(db) *brain.BeliefsService`
(`systemClock{}`, `uuidGen{}`, `sqlite.NewSelfModelRepo`, `NewSignalRepo`, `NewDecisionLog`),
provider-free and wired unconditionally at vault open, `wireUnits`'s precedent. `uiDeps` gains a
`beliefs *brain.BeliefsService` parameter with the same typed-nil guard, and its one call site in
`serve.go` passes it. The constructor has a test over a real empty migrated vault
(`wiring_units_test.go`'s precedent), and `TestUIDeps_NilServicesStayNilInterfaces`
(`serve_test.go`) gains a case for it. `wireBeliefs` and the first `uiDeps` change land in PR 4;
the activity wiring and the second signature change are `m4e-activity`'s PR 6.

### 3.10 Vocabulary edits

Four actions are added: `ActionDeriveBeliefSkipped` (`consolidate.derive.belief_skipped`) and
`ActionDeriveRetiredEmbedFailed` (`consolidate.derive.retired_embed_failed`), both after
`ActionDeriveBeliefReinforced` (PR 2), then `ActionBeliefEdited` (`belief.edited`) and
`ActionBeliefRetired` (`belief.retired`) appended last (PR 3). Each lands in the PR that first
records it, with **two hand edits per PR**: the `AllDecisionActions` comment count in
`internal/ports/decisionlog.go:169` (forty-eight → fifty → fifty-two) and
`test/support/repocontract/decisionlog.go:133`'s hard-coded `want` map plus its subtest title.
Neither is derived from the list. `m4e2-admin` adds `config.updated` (fifty-three).

### 3.11 Doc edits (R11)

| Doc | Edit | PR |
|---|---|---|
| `docs/03-data-model.md` `self_beliefs.status` | add the column comment `-- active|retired`, and a prose line: retiring is a transition; nothing is removed | 1 |
| doc 02 §10 (`:1217-1228`) | `status` vocabulary; "deleting a belief **retires** it (`active → retired`), never removes a row; a retired belief is excluded from every read of active beliefs, which today means derive's dedup and stagnation, and will mean classify injection when that exists (`capture.go:209` passes no beliefs yet)"; "an edit changes `content` only and marks `origin = user_stated`". **No sentence claims injection exists today** | 1 |
| doc 02 §6 item 5 (`:953-972`) | a third dedup rule: the retired shield (key, then the nearest at 0.85, a tie going to retired); "derive may reinforce a user-stated belief, never rewrite its text"; the cost note amended: embedding happens "when a proposal still needs a semantic comparison" (replacing "at the start of the phase"), and covers "every **retired** belief, which grows only by explicit user action"; the retired-embed-failure policy in one sentence (embed error, non-finite, zero and wrong-dimension vectors all drop the retired belief for the pass); and one clause: "a proposal whose vector is unusable is created, or reinforces a user-stated key, without a semantic comparison, and its rationale says so". **The same PR rewrites the test that pins the old sentence** (`consolidate_test.go:1824-1871`, §3.3 step 4) | 2 |
| doc 02 §11 (`:1237-1240`) | "A user's write through the mirror (a belief edit or retirement) is recorded too, with the value it replaced." §11 today speaks of automatic decisions only. (`m4e2-admin` adds the config clause.) | 3 |
| doc 02 §5 step 4 (`:663-664`) | Moved to [`m4e-activity` design §3.11](../m4e-activity/design.md) | its PR 6 |
| `docs/06-harness.md` §4 | I03 row names `self_beliefs`; I12 row names the new actions and two signals; the I22 row names the whitelist additions (§3.12 G12) | 1, 2, 3, 4 (the I23 and `Page` rows: `m4e-activity`) |
| umbrella `proposal.md:483-484` | Q4 and Q5 → **Ruled 2026-10-07**; slicing, §5.1 rows/totals and dependency rows for the m4e/m4e2 split (planning-PR task) | planning PR |

`docs-sync.sh`: PR 1 (`core/selfmodel`) and PR 2 (`core/consolidation`) each carry their doc 02
text in the same PR.

### 3.12 Structural gates for invisible properties

| # | Property | Gate | Fires on (probe) | Silent on |
|---|---|---|---|---|
| G1 | A retired belief is unreachable by derive; edited text is never overwritten | Store guards (§3.2) proven by `repocontract` on **both** implementations: upsert over a retired key and over a user-stated key → `ErrBeliefProtected`, row byte-identical; reinforce of a retired id → conflict, confidence unchanged | removing either `WHERE` clause; `changes()==0` mapped to nil | upsert over an active derived row |
| G2 | Nothing deletes a belief (R4, I03) | `TestI03_UnitsAreNeverDeleted`'s tree scan **strengthened** with a `DELETE FROM self_beliefs` marker (same identifier-tail rule). `SelfModelRepo` is already in the reflection sweep | a probe file emitting `DELETE FROM self_beliefs` | `self_beliefs_x` |
| G3 | Status vocabulary has one truth | `TestBeliefStatusDocMatchesAllStatuses`: doc 03's `self_beliefs.status` comment ↔ `selfmodel.AllStatuses()`, in order | a member added on one side | the pair |
| G6 | Every new mutating route is guarded and cross-origin protected (R9), **proven with a valid body** | New rows in `wantUIMuxWiring`; `TestUINonGETLeavesRefuseCrossOrigin`'s `build()` gives **every** mutating entrance (`Capture`, `Beliefs`; `Admin` in m4e2) a counting stub on one shared counter (finding 7). **A body table** `uiCrossOriginBodies map[string]string` (pattern → a form body that passes that handler's parsing, e.g. `"POST /ui/beliefs/{id}/edit": "content=new+text"`, `"POST /ui/beliefs/{id}/retire": ""`), used by all three subtests in place of the fixed `text=hello`, and **also carrying the two existing POST rows** (`POST /ui/capture` and `POST /ui/units/{id}/correct`, both `text=hello`, the body the fixed subtests send today); **`TestUICrossOriginBodiesCoverEveryPOSTRow` fails for a non-GET row with no entry, and for an entry with no row**. **One explicit exemption, with its reason:** `POST /ui/login` is listed in `uiCrossOriginBodyExempt = map[string]string{"POST /ui/login": "unguarded by design: it mints the cookie, so the same-origin-with-cookie subtest cannot apply; the row keeps a fixed placeholder body for the two refusal subtests, and only the same-origin subtest is skipped (wantUIMuxWiring has it as guarded:false, httpapi_ui_wiring_test.go:166; the existing subtest skips it at ui_cross_origin_test.go:112)"}`. The coverage test treats a row as covered by exactly one of the body table or the exemption map, and fails for an exemption with an empty reason, an exemption with no row, and a pattern in both. Without it the new test would fail on the login row it was never meant to cover. The `{id}` replacement already covers `/ui/beliefs/{id}/…`. `TestUIGuardedLeavesEachReachAView` (`internal/httpapi/server_test.go:117`, a hard-coded leaf list over a stub `ui.Deps`) gains the `/ui/beliefs` leaf and a `BELIEFS` marker in PR 4 (`m4e-activity` adds the `/ui/activity` leaf and an `ACTIVITY` marker in its PR 6; activity has no POST, so it adds no body-table entry) | a POST row unguarded; a POST left out of the wiring table; a POST row with neither a body entry nor an exemption; an exemption with an empty reason (U4b); a body that fails the handler's parse (the stub reads 0 calls); a new entrance not counted | the wiring in §3.9 |
| G7 | GET views write nothing (R1) | `TestUIReadViewsWriteNothing` (conformance, I27's shape): the beliefs GET over write-counting decorators of every repo the service holds → zero calls to any write method (`m4e-activity` adds the activity GET and `m4e2-admin` the admin GET) | a GET that records, signals or seeds | — |
| G8 | | Moved to `m4e-activity` (design §3.12 G8; the number is kept) | | |
| G9 | I23 stays untouched | Naming: `EditContent`, never `UpdateContent` (finding 3). The existing gate is the check | — | — |
| G10 | One instant per operation | The existing `brain_single_clock_read` gate, satisfied by one file per operation (finding 4) | two `Now()`s in `belief_edit.go` | — |
| G11 | Store surface widening is reviewed | `testdata/schema/store_api.golden` regenerated in PR 1 (`make store-api-golden`); `m4e-activity` regenerates it in its PR 5 | an unreviewed method | — |
| G12 | The I22 entrance whitelist names exactly what each slice adds | `ui_entrances_test.go:32`'s `allowedMethods` (a flat name set over every interface field of `ui.Deps`) is edited **per PR, by exact name**: PR 4 adds `ByFacet`, `Edit`, `Retire` (the `Beliefs` interface); `m4e-activity`'s PR 6 adds `Page` (the `Activity` interface); `m4e2-admin` adds `View`, `Update` (the `Admin` interface). Decision: keep the flat set (the gate's declared semantics, no scope creep); the tighter per-field map is a possible later hardening, not this change's | a `ui.Deps` interface exposing any method not on the list (the existing test fires; each PR's RED commit shows it red before the whitelist edit) | the five existing names |

Each GREEN commit body records its probe (mutation applied, red output, reverted), following
m4b and m4c.

---

## 4. Data flow

```
nightly derive ─▶ ActiveBeliefs + RetiredBeliefs ─▶ BuildDerivePrompt(active only) ─▶ judge
   ─▶ RetiredKeyHits (rule 1, no vectors) ─▶ pending = proposals not decided by key
        pending empty ─▶ NO embedding calls
        else ─▶ embed(active) + embed(retired; error / NaN / zero / wrong dimension ─▶ drop it, log retired_embed_failed) + embed(pending)
              ─▶ MergeProposals over active, MergeProposals over retired (remap to original index)
   ─▶ RouteProposals: retired key │ retired nearest (tie ─▶ retired) ─▶ log belief_skipped
                      user-stated key │ active nearest ─▶ ReinforceByID (active only) ─▶ log reinforced
                      otherwise ─▶ UpsertByTopicKey (overwrites active+derived only) ─▶ log created
                      store refusal (race) ─▶ log belief_skipped{changed_since_read}, continue

POST /ui/beliefs/{id}/edit ─▶ xo ─▶ requireCookie ─▶ Edit: normalise ─▶ read ─▶ no-op? ─▶ log(pre-image) ─▶ EditContent(CAS, origin:=user_stated) ─▶ signal
POST /ui/beliefs/{id}/retire ─▶ … ─▶ Retire: read ─▶ SetStatus(active→retired) ─▶ log ─▶ signal   (follow-up failure: ErrWriteLanded)
(`GET /ui/activity`: see `m4e-activity` design §4.)
```

## 5. File changes

| File | Action | PR |
|---|---|---|
| `internal/core/selfmodel/status.go`, `content.go` (+tests) | Create: `Status`, `Origin`, `All*`; `NormalizeText`, `NormalizeContent`, `MaxBeliefContentRunes` | 1 |
| `internal/ports/selfmodelrepo.go` | 4 methods, 2 errors, changed contracts, typed fields; doc comment "Three methods" rewritten | 1 |
| `internal/store/sqlite/selfmodelrepo.go` | the methods and guards (§3.2) | 1 |
| `test/support/memrepo/selfmodel.go`, `test/support/repocontract/selfmodelrepo.go` | fake + contract (G1). **Ripple in the contract:** `repocontract/selfmodelrepo.go:43` seeds `inactive.Status = "merged"` as its non-active belief. `"merged"` is outside the closed `active|retired` vocabulary that G3 pins, so it **must change in PR 1** to `selfmodel.StatusRetired` (the filter case then also covers S1 with a real status) | 1 |
| `internal/brain/consolidate_test.go` (seeds at `:1772`, `:1840-1845` (b-1 and b-2 at `:1844-1845`), `:1930`, `:2161`) | **Ripple of the new `origin = 'derived'` upsert guard.** These brain fixtures seed beliefs through `UpsertByTopicKey` with an **empty `Origin`**; an empty value is neither `derived` nor `user_stated`, so once the guard lands (PR 1, in both `memrepo` and SQLite) any derive test that upserts over such a seeded key gets `ErrBeliefProtected` (at least `:1930`, the overwrite-in-place case). Every seed gets `Origin: "derived"` in PR 1 **to avoid rule 3 (the edited-key rule) treating an empty-origin seed as non-derived**, which would reinforce instead of overwriting in place; the `ErrBeliefProtected` effect of the store guard is the second, not the primary, reason. So the ripple is closed at once and not found one failing test at a time. SQL's `DEFAULT 'user_stated'` (`0001:79`) does not apply: an explicit empty string is stored as empty | 1 |
| `test/conformance/i03_units_never_deleted_test.go`, `belief_status_doc_test.go` | G2 strengthening; G3 | 1 |
| `internal/core/consolidation/shield.go` (+test) | `RetiredKeyHits`, `RouteProposals` | 2 |
| `internal/brain/consolidate.go` | retired read, conditional embed, `usableVector` screen, two merges, route, skip/race/embed-failure arms; `embedForMerge` (`:839-876`) takes the retired beliefs and the `pending` proposals, and **its doc comment (`:839-852`) is rewritten** | 2 |
| `internal/brain/scripted_embedder_test.go` | Create: a test-local `ports.EmbeddingProvider` with per-text vectors, per-text failures, NaN/zero/wrong-dimension vectors and a call counter (the FX-D/FX-D2 embedder) | 2 |
| `internal/brain/consolidate_test.go` `:1824-1871` | **Explicit rewrite** of `TestConsolidateRunner_Derive_EmbedsExactlyOncePerActiveBelief` (asserts 2 embeds on `{"beliefs":[]}`, "spec R5.7"): same fixture, renamed `TestDerive_NoProposalsMakesNoEmbedCalls`, asserts 0 (D15); plus the sibling case with one pending proposal asserting active + retired + pending embeds (D17). Justified against m2c R5.7 and the doc 02 §6 item 5 amendment **in the same PR** (§3.3 step 4; nooma-testing rule 4, non-negotiable 4) | 2 |
| `internal/ports/decisionlog.go` + repocontract map | +2 actions (PR 2), +2 (PR 3) (§3.10) | 2, 3 |
| `internal/brain/beliefs.go`, `belief_edit.go`, `belief_retire.go`, `write_landed.go` (+tests) | Create | 3 |
| `internal/ui/beliefs.go`, `beliefs.templ`, `ui.go`, `layout.templ`; `internal/httpapi/server.go`; `cmd/nooma/serve.go`, `cmd/nooma/wiring.go` (`wireBeliefs`), `cmd/nooma/wiring_beliefs_test.go`, `serve_test.go` | routes, view, deps, wiring, `uiDeps` signature | 4 |
| `DecisionLog.Before` and its port/store/memrepo/repocontract changes, the `recordingLog` ripple, `ActivityService`, `/ui/activity` and `wireActivity` | Moved to [`m4e-activity` design §5](../m4e-activity/design.md) | 5, 6 there |
| `internal/httpapi/server_test.go:117` (`TestUIGuardedLeavesEachReachAView`) | PR 4: `/ui/beliefs` leaf, `BELIEFS` marker, stub `Beliefs` (activity's leaf is `m4e-activity`'s PR 6) | 4 |
| `test/conformance/ui_entrances_test.go` | whitelist additions (G12) | 4 |
| `test/conformance/httpapi_ui_wiring_test.go`, `ui_cross_origin_test.go`, `ui_read_views_write_nothing_test.go` | G6 (body table), G7 | 4 |
| docs | §3.11 | 1, 2, 3, 4 |

## 6. Testing strategy and mutation targets

Levels: L1 core (`RouteProposals`, `RetiredKeyHits`, `NormalizeContent`), L1 brain over memrepo
with a fixed clock, a scripted judge and a **text-keyed fake embedder** (hand-built vectors, so
every cosine is exact, with a call counter), L1 `ui` over stubs, L2 conformance for G2-G12, and L3
for every SQL guard, ordering and cursor claim. No browser, network or real LLM.

### Fixture rules (each test below cites them by name)

**FX-B, beliefs.** At least two beliefs in each touched facet, one untouched facet populated, one
empty facet, and **one retired belief in a touched facet** (so a missing status filter shows).
Before any "nothing changed" assertion, **seed** `decision_log` with two rows and
`learning_signals` with one, and compare full snapshots (rows and every belief column), not
counts from zero. Rows must carry non-default `confidence`, `updated_at` and
`last_reinforced_at`, so a column written by mistake is visible. One belief has multi-line content
(`"a\nb"`) for the CRLF case. **Order fixture (B11b-d):** one touched facet holds at least five
beliefs forming a tie at each level of the order: two with different `confidence`; two with equal
`confidence` and different `last_reinforced_at`; two with equal both and different `id`. The
`ByFacet` test runs over a **stub `SelfModelRepo` that returns them in the exact reverse of the
expected order**, plus a second permutation, so a missing sort fails every time. A `memrepo` run
of the same fixture, repeated 20 times, is the flake detector: `memrepo` iterates a Go map
(`memrepo/selfmodel.go:46`), so without the sort it fails with high probability, not certainty,
and the stub is what makes the kill deterministic.

**FX-D, derive shield.** Proposals in this order (the retired-key one is **not first**, so an
index misalignment between `proposedKeys`, the compact `pending` list and the merges dies):
p0 fresh key, far from everything → create (control);
p1 key == retired R1's, cosine to R1 **below** 0.85 → skip `retired_topic_key` (only rule 1
catches it);
p2 new key, cosine 0.95 to retired R2 and 0.90 to active A → skip `retired_similar`;
p3 new key, cosine 0.95 to active A and 0.90 to retired R2 → reinforce A (kills "any retired
neighbour skips");
p4 key == user-stated U's, far content → reinforce U, content unchanged;
p5 key == active derived D's, far content → D overwritten in place (m2c control);
**p6 new key, exactly equal cosine (≥ 0.85) to retired R3 and to active A2 → skip
`retired_similar`** (the tie; built from mirror vectors such as A2 = (a,b,0), R3 = (a,0,b),
proposal (1,0,0) so both scores are bitwise equal after normalisation, not merely close).
**p7 key == U's, cosine 0.95 to active A → reinforce U, not A** (kills D23).
R1, R2, R3 and U carry distinct content, confidence and timestamps that the test snapshots.

**FX-D2, retired-vector failure (D18-D20).** Retired R4 whose vector is unusable, in four
variants (embed error, NaN, zero, wrong dimension), with **two proposals**: pA shares R4's key
(rule 1, skip) and **pB has a new key, far from everything**, so it stays `pending`. Without pB
the pending list is empty, derive makes no embedding call at all (D16), R4's embedding is never
attempted and no failure row can exist, so the fixture would assert a row that cannot be
written. With pB an embed is attempted, the failure row exists, pA is still skipped by key and pB
is created. The
fake embedder counts calls: the expected count is asserted for every scenario below, and p1 sits
before pending proposals so the remap from compact to original index is exercised.

**FX-A, activity.** Moved to [`m4e-activity` design §6](../m4e-activity/design.md).

**FX-N, content.** Bound-exact (`MaxBeliefContentRunes` ASCII runes), bound+1, a multibyte string
of `MaxBeliefContentRunes` runes (> that many bytes), `"  a\r\nb  "`, whitespace only, empty. For
the no-op comparison: a **derived** belief whose stored content is 1,500 runes with a trailing
space, and one whose stored content is whitespace only.

**FX-H** (R10) moved to `m4e2-admin`.

### Mutation targets, enumerated from the production diff

| # | PR | Production branch | Mutant | Killed by |
|---|---|---|---|---|
| S1 | 1 | `ActiveBeliefs` `status='active'` | drop the filter | `RunActiveBeliefs` retired-excluded case (FX-B) |
| S2 | 1 | `SetStatus … AND status = from` | drop | contract "retire twice → conflict, row unchanged" |
| S3 | 1 | `SetStatus` zero rows → not-found vs conflict | swap / always one | contract missing-id and retired-id cases |
| S4 | 1 | `SetStatus` writes `updated_at = at` | omit | contract (FX-B timestamps) |
| S5 | 1 | `EditContent` literal `origin='user_stated'` | omit | contract: derived → edited → `user_stated` |
| S6 | 1 | `EditContent … AND content = from` | drop | contract stale-`from` → conflict, content unchanged |
| S7 | 1 | `EditContent … AND status='active'` | drop | contract edit-retired → conflict |
| S8 | 1 | `EditContent` touches only content/origin/updated_at | also sets confidence or `last_reinforced_at` | contract full-row snapshot (FX-B) |
| S9 | 1 | upsert guard `status='active'` | drop | G1 retired-key case |
| S10 | 1 | upsert guard `origin='derived'` | drop; `= 'user_stated'` | G1 user-stated case; m2c's existing second-write case |
| S11 | 1 | `changes()==0` → `ErrBeliefProtected` | return nil | G1 cases assert the error |
| S12 | 1 | `ReinforceByID … AND status='active'` | drop | G1 reinforce-retired case |
| S13 | 1 | `RetiredBeliefs` filter | return all / active | contract over active + retired |
| S14 | 1 | vocabulary pin | member added one side | G3 |
| S15 | 1 | `ReinforceByID` zero rows → not-found vs conflict | swap / always `ErrBeliefNotFound` / always conflict | contract: missing id → `ErrBeliefNotFound`, retired id → `ErrBeliefStatusConflict` (two distinct assertions) |
| S16 | 1 | `BeliefByID` unknown id → `ErrBeliefNotFound` | return a zero `Belief` and nil | contract unknown-id case on both implementations; and a found id returns every column (FX-B) |
| S17 | 1 | `NormalizeText` CRLF → LF (reached through `NormalizeContent`) | omit | `"a\r\nb"` == `"a\nb"` (FX-N) |
| S18 | 1 | `NormalizeText` `TrimSpace` | omit | `"  a  "` → `"a"` |
| S19 | 1 | empty-after-normalise rejected | accept whitespace-only | FX-N whitespace-only |
| S20 | 1 | bound check `> Max` in runes | `>= Max`; bytes instead of runes; check before normalising | FX-N bound-exact accepted, bound+1 rejected, multibyte case, `"  " + Max runes + "  "` accepted |
| D1 | 2 | rule 1 (retired key) | remove | `TestRouteProposals_RetiredKeySkipsEvenWhenFar` (p1) |
| D2 | 2 | rule 2 (retired nearest) | remove | p2 |
| D3 | 2 | nearest-wins | "any qualifying retired skips" | p3 |
| D4 | 2 | rule 3 (user-stated key → reinforce) | remove (falls to create) | p4 + G1 at the store (the upsert would be refused, and a brain test asserts **reinforced**, not skipped) |
| D5 | 2 | rule 3 scoped to `origin != derived` | "any active key reinforces" | p5 |
| D6 | 2 | rule order 1 before 4 | 4 first | p1 variant where R1's key matches *and* an active belief is nearest |
| D7 | 2 | retired vectors compared at all | active only | brain FX-D p2 (becomes reinforce or create) |
| D8 | 2 | `RetiredBeliefs` error aborts | swallow | `TestDerive_RetiredReadErrorFailsPhase` |
| D9 | 2 | skip records one row with `{topic_key, belief_id?, reason, similarity?, proposed_content}`; `similarity` omitted on key match; `topic_key` is the proposal's; `proposed_content` is the proposal's text | no row; wrong reason; zero similarity written; `retired_topic_key` key name; `proposed_content` omitted | FX-D asserts one row per skip, by key, and the exact key set per reason |
| D10 | 2 | skip writes no belief | fall through to create | FX-D snapshot of R1/R2 unchanged |
| D11 | 2 | `ErrBeliefProtected` → skip row (`changed_since_read`, no `belief_id`), continue | return err | `TestDerive_ProtectedRaceSkipsAndContinues` (decorator flips D to user-stated between read and write; p0 still created) |
| D12 | 2 | `ErrBeliefStatusConflict` on reinforce → skip | return err | `TestDerive_RetiredDuringReinforceSkips` |
| D13 | 2 | tie goes to retired (`>=`) | `>` | p6 (equal cosine) |
| D14 | 2 | two `MergeProposals` calls, remapped to original index | one call over a union; no remap | p6 (tie by Search order is not a rule), and p1-before-pending remap case |
| D15 | 2 | no proposals ⇒ zero embed calls | embed anyway | `TestDerive_NoProposalsMakesNoEmbedCalls`: **the rewrite of `TestConsolidateRunner_Derive_EmbedsExactlyOncePerActiveBelief`** (`consolidate_test.go:1824-1871`; same `{"beliefs":[]}` fixture, 2 active; asserts 0 where it asserted 2). Plus a variant with M retired seeded; counter == 0 |
| D16 | 2 | every proposal key-decided ⇒ zero embed calls | embed pending-less | `…AllKeyDecidedMakesNoEmbedCalls` (counter == 0) |
| D17 | 2 | only `pending` proposals embedded | embed all | FX-D counter == active + retired + pending; and the **rewrite's sibling case** (2 active, 1 retired, one pending proposal; `EmbedCalls() == 4`, where the old test's fixture could only ever see 2) |
| D18 | 2 | retired embed error → drop, log `retired_embed_failed`, continue | abort; swallow silently | `TestDerive_RetiredEmbedFailureDegradesToKeyOnly` over **FX-D2** (pA by key plus a key-undecided pB, so an embed is attempted): pass completes, one row with `{belief_id, topic_key, cause: "embed_error"}`, pB created |
| D19 | 2 | retired non-finite vector → same policy | pass it to `MergeProposals` (whole call errors) | `…RetiredNonFiniteVectorDegrades` (FX-D2, `cause: "non_finite"`) |
| D19b | 2 | retired **zero** vector → same policy (`recall.ErrZeroVector`) | pass it to `MergeProposals` (`Normalize` errors, whole call fails); or conflate the cause with `non_finite` | `…RetiredZeroVectorDegrades` (FX-D2, `cause: "zero_vector"`; an empty vector too) |
| D19c | 2 | retired **ragged/mismatched dimension** → same policy (`ErrRaggedVectors`, `ErrDimMismatch`) | pass it to `MergeProposals` (`NewVectorIndex` or `Search` errors) | `…RetiredRaggedVectorDegrades` (FX-D2, `cause: "dimension_mismatch"`: one retired vector one component short, and a second case where every retired vector is consistently the wrong length) |
| D19d | 2 | **proposed**-side unusable vector (non-finite, zero, dimension) → excluded from both merges, falls to rules 3/5, rationale says "semantic comparison skipped" | pass it to `MergeProposals` (a zero vector aborts the phase); skip the rationale | `…ProposedUnusableVectorCreatesAndSaysSo` (zero and non-finite cases: pass completes, proposal created, rationale names the cause; a user-stated-key case reinforces; **odd-first case:** the wrong-dimension proposal comes first in the list and must be the one excluded, not the one that sets the dimension; **no-active case:** no active beliefs and a zero-vector proposal first) |
| D20 | 2 | failed-embed retired belief still key-matched | key rule needs the vector | `…EmbedFailureStillSkipsByKey` (FX-D2: pA sharing the key → skip, no create; asserts the row exists, so pB's pending embed really ran) |
| D21 | 2 | `ctx` cancelled is not the degrade policy | degrade on cancel | `…CancelledEmbedAbortsPhase` |
| D22 | 2 | active-belief embed failure still aborts | degrade | existing behaviour pinned |
| D23 | 2 | rule order 3 (user-stated key) before 4 (active nearest) | swap the two rules | p7 |
| B1 | 3 | edit unknown id → nothing | log first | `TestBeliefEdit_UnknownIDWritesNothing` (FX-B seeded) |
| B2 | 3 | equal normalised content → no-op | write anyway; compare raw | `…SameContentWritesNothing`; `…CRLFResubmissionWritesNothing` (`"a\r\nb"` vs stored `"a\nb"`, origin stays `derived`, `updated_at` unchanged) |
| B3 | 3 | retired → conflict before any write | skip the status check (CAS still refuses, but the log row exists) | `…RetiredBeliefWritesNothing` (log count unchanged) |
| B4 | 3 | pre-image before `EditContent` | swap | `…LogFailureLeavesBeliefUntouched` |
| B5 | 3 | signal only after a landed write | signal on failed write | `…WriteFailureEmitsNoSignal` |
| B6 | 3 | edit signal fields (type, valence negative, target, `DecisionAction` by prior origin, `decision_id`) | each field wrong | `…SignalNamesBeliefAndLogRow`, two cases: prior `derived`, prior `user_stated` (nil action) |
| B7 | 3 | context keyed by column | positional / missing origin | JSON key assertions |
| B8 | 3 | empty / over-bound → typed errors, nothing written | accept | table test (FX-N) |
| B9 | 3 | retire writes first | log first | `TestBeliefRetire_TwiceLogsOnce` |
| B10 | 3 | `SetStatus(active, retired)` args | swapped | retire-active succeeds and reads `retired` |
| B11 | 3 | `ByFacet` all five facets, empty as empty | omit empty; mis-group | FX-B |
| B11b | 3 | `ByFacet` orders by `confidence` DESC | no sort (storage order); ASC | order fixture over the reverse-order stub |
| B11c | 3 | tie → `last_reinforced_at` DESC | drop the second key; ASC | order fixture (equal confidence) |
| B11d | 3 | tie → `id` ASC (total order) | drop the third key (left to map iteration or insertion) | order fixture (equal confidence and timestamp) and the 20-run `memrepo` flake detector |
| B12 | 3 | `Retire` on an already-retired belief → conflict **before any write** | skip the pre-check and rely on the CAS | `TestBeliefRetire_AlreadyRetiredConflictsBeforeAnyWrite`: counting decorators show `SetStatus`, `Record` and `Record(signal)` called **zero** times, and the error is `ErrBeliefStatusConflict` |
| B13 | 3 | retire signal fields: type `belief_delete`, valence negative, `TargetKind` `belief`, `TargetID`, `Magnitude` nil, `DecisionAction` by origin (`derived` → created bucket; `user_stated`/`seed` → nil), `Context` `{belief_id, topic_key, decision_id}` | each field wrong, one mutant per field | `…RetireSignalFields`, three origin cases |
| B14 | 3 | retire `Record` fails after the write (window b) → belief retired, signal still written without `decision_id`, error wraps `ErrWriteLanded` with `Record` set | return a plain error; skip the signal; set `Signal` instead | `…RecordFailureAfterWriteIsNonFatal` |
| B15 | 3 | edit signal fails after the write → `ErrWriteLanded` with `Signal` set | plain error | `…SignalFailureAfterWriteIsNonFatal` |
| B17 | 3 | **retire window (c)**: `Record` lands, the signal fails → belief retired, log row exists, error wraps `ErrWriteLanded` with `Signal` set (not `Record`) | return the signal's plain error (the mutant B14 and B15 leave alive: B14 fails the record, B15 is the edit) | `TestBeliefRetire_SignalFailureAfterWriteIsNonFatal`: a failing `SignalRepo` double; asserts `errors.Is(err, ErrWriteLanded)`, `errors.As` gives `Signal && !Record`, the belief reads `retired`, and the `belief.retired` row exists |
| B18 | 3 | no-op compares against `NormalizeText(stored)`, non-validating | compare against `NormalizeContent(stored)` (errors on an over-bound or blank derived belief) | `TestBeliefEdit_OverBoundDerivedBeliefIsEditable`: derived belief of 1,500 runes edited to a valid short text lands (origin `user_stated`, one log row, one signal); a whitespace-only stored belief likewise; and resubmitting an over-bound derived text is rejected by the *submitted*-side `NormalizeContent` with nothing written |
| B19 | 3 | retire's `Record` **and** the signal both fail → error wraps `ErrWriteLanded`, `errors.As` gives `Record && Signal` | report only one of the two; plain error | `TestBeliefRetire_RecordAndSignalFailBothReported`; the notice is the "neither" row of the §3.4 table |
| B16 | 3 | stored value and logged `next.content` are the normalised value | store raw | FX-N `"  a\r\nb  "` stored as `"a\nb"` |
| U1 | 4 | edit reads `content` and `r.PathValue("id")` only | constant id; reads another field | two-belief test; POST with `facet`/`confidence` leaves them unchanged |
| U2 | 4 | error → status map (404/409/400, `ErrWriteLanded` → success, `slog.Warn`ed, **with the notice for the missing part**) | any swap; one generic notice that says "the record failed" when only the signal did | table test over the three `WriteLandedError` variants (§3.4) asserting the notice text and a recording `slog` handler |
| U3 | 4 | retire route → `Retire` | → `Edit` | G6 markers and per-route stub counters |
| U4 | 4 | each POST route has a body-table entry | a route with none | `TestUICrossOriginBodiesCoverEveryPOSTRow` (and a stale entry) |
| U4b | 4 | `POST /ui/login` is covered by the explicit exemption map, with a non-empty reason; no pattern is in both maps | drop the exemption (the test fails on the login row); an empty reason; login also given a body entry | the same test, three probe rows |
| A1-A12 | | **Moved to `m4e-activity` design §6** (numbers kept) | | |

Equivalent mutant named so nobody spends time on it: `SetStatus`'s disambiguating SELECT run
**after** a zero-row UPDATE instead of before it (same observable result, one fewer read on the
happy path). (The `substr` one moved with A3 to `m4e-activity`.)

Order per PR, as in m4c: a scaffold commit (signatures with zero-value bodies, compiles), a RED
commit (tests failing on assertions, never on `undefined`), then GREEN. Umbrella §5.2 row 9 (I03
beliefs, I12) lands in PR 3. Row 10 (I23 read-only) is `m4e-activity`'s PR 6.

## 7. The PR chain (stacked-to-main) and the split forecast

Budgets are impl+docs. They exclude tests, `test/support/**`, the regenerated golden and
`*_templ.go` (`linguist-generated`, `.gitattributes:31`). Each PR runs `make check-all` and `go
vet -tags integration,e2e ./...`.

Columns after the point estimate are the estimate times 1.3 (the project's lowest measured
overrun, umbrella §5.1), 1.8 and 2.2 (the high end of its six measured overruns, "1.3x-2.2x";
a 4.3x outlier is recorded in the umbrella and not modelled here).

| # | Branch | Content | Impl+docs | x 1.3 | x 1.8 | x 2.2 |
|---|---|---|---|---|---|---|
| 0 | `plan/m4e-beliefs-activity-admin` | spec, design, tasks for m4e; spec and design for `m4e2-admin`; the umbrella edits listed in the header | (planning, **not counted**) | | | |
| 1 | `feat/ports-store-belief-status` | §3.1, §3.2, `NormalizeText`/`NormalizeContent` + bound, G1-G3, doc 03, doc 02 §10 | ~265 | ~345 | ~477 | ~583 |
| 2 | `feat/brain-derive-shield` | §3.3, `RouteProposals`/`RetiredKeyHits`, conditional embed, `usableVector` screen and failure policy, actions +2, doc 02 §6 item 5 | ~270 | ~351 | ~486 | ~594 |
| 3 | `feat/brain-belief-edit-retire` | §3.4, `WriteLandedError`/`ErrWriteLanded`, `ByFacet` order, actions +2, doc 02 §11 | ~260 | ~338 | ~468 | ~572 |
| 4 | `feat/ui-beliefs` | `/ui/beliefs` and two POSTs, G6 body table + rows + login exemption, G12 (`ByFacet`, `Edit`, `Retire`), `wireBeliefs`, `uiDeps`, nav | ~275 | ~358 | ~495 | ~605 |
| 5, 6 | `feat/ports-store-decisionlog-before`, `feat/ui-activity` | **Moved to [`m4e-activity`](../m4e-activity/design.md)** (design §7 there) | ~420 | ~546 | ~756 | ~924 |
| | **Total (4 PRs; 5 after PR 2's cut)** | | **~1,070** | **~1,390** | **~1,930** | **~2,350** |

Arithmetic: 265 + 270 + 260 + 275 = 1,070. x 1.3: 345 + 351 + 338 + 358 = 1,392. x 1.8: 477 + 486
+ 468 + 495 = 1,926. x 2.2: 583 + 594 + 572 + 605 = 2,354. With the moved pointer row (420, 546,
756, 924) the six-PR totals are the old ones: 1,490, 1,938, 2,682, 3,278.

**The 400-line soft ceiling.** At 1.3x every PR is under 400 (largest: PR 4 at ~358). At 1.8x
every PR is over it, and at 2.2x all are well over. So the ceiling holds only
at the lowest multiplier. **Cut rule for `sdd-tasks` (measured, not a mood):** after PR 1 merges,
compute actual-over-estimate for it; if `estimate x measured multiplier > 400` for any later PR,
cut that PR at its nearest layer seam (port/store | brain | ui) before `sdd-apply`, and re-run the
umbrella rule on the new PR count. Named candidates if the cut fires (PR 2's was exercised: #291 and #292): PR 2 into "pure `shield.go`
+ its doc 02 text" and "`consolidate.go` wiring". PR 6's candidate moved with PR 6 to `m4e-activity`.

PR 2 precedes the UI so that, at every tip, no belief can be retired or edited before the night
knows how to skip it. At PR 1's tip a `ErrBeliefProtected` from derive would abort the pass, but
no surface can yet create a retired or user-stated row, so that path is unreachable in
production.

**Does planning PR 0 count toward the seven-PR rule? No.** The umbrella rule (proposal.md ~:301)
is about *implementation* PRs, and the umbrella's own §5.1 table lists only implementation
branches (its thirty-one PRs exclude every `plan/` branch). PR 0 carries no code and no budgeted
lines. Counting it would make every slice one PR heavier by construction.

**Forecast, recomputed on 2026-10-08 (round 2), shown at 1.3x, 1.8x and 2.2x** (historical,
before the activity split: the current figures are the next table and `tasks.md`):

| Scope | PRs | Point | x 1.3 | x 1.8 | x 2.2 | Rule (> 7 PRs or > 2,400 lines) at point (sensitivity: 1.3x / 1.8x / 2.2x) |
|---|---|---|---|---|---|---|
| **m4e (beliefs + activity), this change** | **6** | **~1,490** | **~1,940** | **~2,680** | **~3,280** | **does not fire at point** (sensitivity: does not fire / fires on lines / fires on lines) |
| `m4e2-admin` (three PRs after its PR 1 is cut, see its §7) | 3 | ~605 | ~790 | ~1,090 | ~1,330 | does not fire at point (nor at any of the three multipliers) |
| m4e + m4e2 as one slice (the old shape) | 9 | ~2,095 | ~2,720 | ~3,770 | ~4,610 | fires on the PR-count clause only at point (lines clause needs 1.3x: ~2,720) |

The umbrella rule (proposal.md ~:301) reads **budgeted, i.e. point, lines**; the 1.3x, 1.8x and
2.2x columns are sensitivity, not the rule's reading, and they say plainly that **if this
slice runs at 1.8x or worse, m4e alone crosses 2,400 budgeted lines**. If 1.8x growth forces cuts
and the PR count exceeds seven, the next remedy is splitting beliefs (PRs 1-4) from activity (PRs
5-6) into two changes, not further cuts inside this one. That is not a reason to
split further now (the cut rule above reacts to the first measurement), but it is why the
re-measurement point is PR 1, not PR 4. (The "old shape" row counts m4e2's three PRs, so it is 9,
not the 8 of the first split; with the original two m4e2 PRs it is 8 and ~2,095 points.)

**Forecast after the activity split (2026-10-08, owner ruling), with the measured multiplier.**
The umbrella rule reads point lines; the other columns are sensitivity. 2.06x is what PR 2
measured (557 changed lines against ~270).

| Scope | PRs | Point | x 1.3 | x 1.8 | x 2.06 | Rule (> 7 PRs or > 2,400 lines) at point |
|---|---|---|---|---|---|---|
| **m4e (beliefs), this change** | **4 forecast; 5 after PR 2's cut** (7 if PRs 3 and 4 are also cut at 2.06x) | **~1,070** | **~1,390** | **~1,930** | **~2,200** | does not fire |
| `m4e-activity` | 2 (3 if PR 6 is cut) | ~420 | ~546 | ~756 | ~865 | does not fire |
| `m4e2-admin` | 3 | ~605 | ~790 | ~1,090 | ~1,250 | does not fire |
| m4e as it stood (beliefs + activity, before the split) | 7 after PR 2's cut; 10 once PRs 3, 4 and 6 are cut at 2.06x | ~1,490 | ~1,940 | ~2,680 | ~3,070 | would fire on the PR-count clause (10 > 7) once the predicted cuts are counted |

Arithmetic: 1,070 x 2.06 = 2,204; 420 x 2.06 = 865; 605 x 2.06 = 1,246; 1,490 x 2.06 = 3,069.
PR 3 at 2.06x is 260 x 2.06 = ~536 and PR 4 is 275 x 2.06 = ~567, both over 400, so their
pre-defined seams (`ByFacet` + `Edit` | `Retire` + `write_landed.go`; GET view + wiring | the
two POSTs + G6 body table) stay available: 5 + 2 = 7 PRs for this change at most, which does not
pass seven.

**Reconciling the earlier "~1,890" figure.** The previous text said the old design was "7 PRs and
~1,890 lines: ~1,460 here plus ~430 admin" and also that the findings had added code, which cannot
both be true of the same 1,460. The arithmetic that is verifiable: old 1,890 - old admin 430 =
**1,460 non-admin**, the figure this design carried through round 1. Admin is now **605** (330 +
275), so 1,460 + 605 = **2,065**, i.e. **+175 over the old total, all of it admin** (the
`LearnedThresholds` read, the `AdminService` door, effective-value "unchanged", honest job
status, wiring). This round adds about **+30 to m4e** (the `usableVector` screen +10 in PR 2;
`WriteLandedError`, `NormalizeText` and the `ByFacet` sort +20 in PR 3), giving 1,490 + 605 =
**2,095**. The split of the old PR 5 into two changes the PR count and the shape, not the sum.
Re-measure after PR 1 (not PR 4): if it ran above 1.3x, `sdd-tasks` applies the cut rule before
PR 2.

## 8. Risks

| # | Risk | Mitigation |
|---|---|---|
| OR-1 | **Owner review**: the semantic shield can suppress a genuinely new belief whose nearest neighbour is a retired one (e.g. "run a marathon" retired, "run a half marathon" proposed). M4 has no un-retire and no create, so the user cannot recover it from the UI | Each suppression is a visible `belief_skipped` row with similarity. Un-retire is one `SetStatus(retired, active)` caller away. The owner confirms that the ruling's "user's word wins" covers near-duplicates |
| OR-2 | **Owner review**: `MaxBeliefContentRunes` is ruled an input bound (not a §13 row) and its value (1,000) is chosen, not calibrated | Flagged in §3.4; promoting it to a §13 row is a one-line doc 02 edit in PR 1 |
| RK-1 | SQLite `changes()` for `DO UPDATE … WHERE false` is assumed 0 | The L3 probe is written first (§3.2). If it is not 0, fall back to a read-then-`INSERT … DO NOTHING` + guarded `UPDATE` pair |
| RK-2 | An edit's CAS can fail after the pre-image row is written (a concurrent retire), leaving a row describing an edit that did not land | Same class ADR-0016 accepts. The rationale says "about to replace". Conflicts are user-vs-nightly-pass only |
| RK-3 | Nightly embed cost grows with retired beliefs | Grows only by explicit clicks, and the shield now embeds nothing on a night with no pending proposal. Doc 02 §6 item 5's cost note is extended, and option B (`belief_embeddings`) remains the named escape |
| RK-4 | One skip row per night per re-proposed retired belief | Requested by R12. Filterable under `consolidate` in activity |
| RK-5 | A retired belief that fails to embed degrades to key-only matching for that night, so a semantically similar proposal under a new key is created | Decided policy (§3.3), logged as `retired_embed_failed`, ends when the provider recovers; aborting would fail derive nightly with no UI recovery |
| RK-6 | Typing `Belief.Status`/`Origin` ripples into fixtures that hold typed string variables | Compile errors only, caught by `make check` and `go vet -tags integration,e2e` |
| RK-7 | The cross-slice contract with `m4e2-admin` (vocabulary file; body table; `ErrWriteLanded`; the decoder half moved to `m4e-activity`, its RK-7) | m4e2 adds the integration test that the row it writes decodes (its C15) through `m4e-activity`'s service; the dependency list is in the headers of all three changes |
| RK-8, RK-9 | Moved to [`m4e-activity` design §8](../m4e-activity/design.md) (numbers kept) | |
| RK-10 | The `usableVector` screen turns a **zero or wrong-dimension proposed vector** from a phase abort (verified: `MergeProposals` continues only on `ErrNonFiniteVector`, `derive.go:163`) into a create | Deliberate and logged in the row's rationale (§3.3); D19d pins it. It only ever runs on pending proposals, never on active beliefs, whose corruption keeps aborting. **Proposal-side residue (cf. RK-5):** a proposal with an unusable vector, under a new key, near a retired belief is **created**, because it has no semantic match, just as RK-5's retired side has none. A zero vector degrades while an embed error aborts: an unusable vector is a property of one input the provider returned successfully, deterministic per input, so aborting would fail derive every night with no UI recovery (RK-5's argument); an embed **error** is a provider fault that affects the pass as a whole and clears on retry, and aborting loses nothing. The doc 02 §6 item 5 amendment (PR 2) gains the clause "a proposal whose vector is unusable is created, or reinforces a user-stated key, without a semantic comparison, and its rationale says so" |

No open question blocks `sdd-tasks`. OQ8 is closed above (OQ4 is closed in `m4e-activity`). OQ7 is closed by the admin split.
OR-1 and OR-2 are for the owner, not blockers.

---

## 10. Judgment Day round 3 carry-overs (2026-10-08)

Binding corrections from round 3. They are **appended, not rewritten into** §1-§8: where one
conflicts with an earlier section, this section wins, and `tasks.md` pre-task 0 applies the
in-place wording changes in the planning PR. Numbers are the round-3 correction numbers.

1. **No dead nav link.** PR 4 adds only the beliefs link to `layout.templ`. PR 6 adds the
   activity link, so `internal/ui/layout.templ` joins PR 6's files (§5 row for PR 6). §3.9's
   "the layout nav gains two links" reads "one link per PR".
2. **`usableVector` reference dimension (§3.3).** The reference is **the first active belief's
   vector when active beliefs exist** (an active belief that fails to embed or is non-finite
   already aborts, so active vectors are the trusted set), **falling back to the first usable
   pending proposal's vector** only when there are no active beliefs. D19d gains a case where the
   odd (wrong-dimension) proposal comes **first** in the list: it must be the one excluded, not
   the one that sets the dimension. A second case covers no active beliefs with a zero-vector
   proposal first.
3. **Forecast basis (§7).** The umbrella rule (proposal.md ~:301) reads **budgeted, i.e. point,
   lines**; 1.3x/1.8x/2.2x are sensitivity, not the umbrella's convention. §7's sentence "The
   rule is evaluated at 1.3x because that is the umbrella's own convention" is superseded. At
   point: m4e is 6 PRs and ~1,490 lines (does not fire); m4e2 is 3 PRs and ~605 (does not fire);
   m4e + m4e2 as one slice is 9 PRs and ~2,095, which fires on the **PR-count clause only** (the
   lines clause needs 1.3x: ~2,720). The "fires on both clauses at every multiplier" cell in the
   §7 table is corrected accordingly. **If 1.8x growth forces cuts and the PR count exceeds
   seven, the next remedy is splitting beliefs (PRs 1-4) from activity (PRs 5-6) into two
   changes**, not further cuts inside this one.
4. **Spec R11** lists doc 02 §6 item 5 (retired shield and conditional embed, PR 2, which
   licenses the D15/D17 test rewrite) and the §11 belief clause (PR 3). Pre-task 0 spec edit.
5. **RK-10 and RK-5.** RK-10 cross-references RK-5 and names the proposal-side residue: a
   proposal with an unusable vector, under a new key, near a retired belief gets **created**
   (it has no semantic match, as RK-5's retired side has none). Why a zero vector degrades while
   an embed error aborts: an unusable vector is a property of one input the provider returned
   successfully, deterministic per input, so aborting would fail derive every night with no UI
   recovery (RK-5's argument); an embed **error** is a provider fault that affects the pass as a
   whole and clears on retry, and aborting loses nothing. The doc 02 §6 item 5 amendment (PR 2)
   gains one clause: "a proposal whose vector is unusable is created, or reinforces a
   user-stated key, without a semantic comparison, and its rationale says so".
6. **Umbrella list.** The header's planning-PR list gains `proposal.md:150` ("No undo ... lines
   647-648" becomes 663-664) and `:337` (`feat/ui-activity` row: "doc 02 lines 647-648
   corrected" becomes 663-664) as explicit items 12 and 13.
8. **B19 (PR 3).** A brain test where retire's `Record` **and** the signal both fail: the error
   wraps `ErrWriteLanded` and `errors.As` gives `Record && Signal`; the notice is the "neither"
   row of the §3.4 table.
9. **Precedence (§3.3), R13 vs rules 3/4.** The edited-key rule moves above the semantic-active
   rule so a same-key proposal merges into the edited belief even when a different active belief
   is semantically nearer. New order: 1 retired key, 2 retired nearest (tie to retired), **3
   user-stated key, 4 active nearest**, 5 create. §3.3's list, the "nearest-wins" paragraph and
   the §4 data-flow lines follow. New fixture **p7** (key == U's, cosine 0.95 to active A: reinforce
   U, not A) and mutant **D23** (§6): swap the two rules; killed by p7.
10. **G6 body table (§3.12).** `uiCrossOriginBodies` also carries the two **existing** POST rows
    (`POST /ui/capture` and `POST /ui/units/{id}/correct`, both `text=hello`, the body the fixed
    subtests send today). Exempt rows (`POST /ui/login`) keep a **fixed placeholder body** for the
    two refusal subtests; only the same-origin subtest is skipped for them.
11. **`TestUIGuardedLeavesEachReachAView`** lives in `internal/httpapi/server_test.go:117`
    (hard-coded leaf list over a stub `ui.Deps`), not in `test/conformance`. PR 4 adds the
    `/ui/beliefs` leaf and a `BELIEFS` marker with a stub `Beliefs`; PR 6 adds `/ui/activity` and
    `ACTIVITY` with a stub `Activity`. The file joins both PRs' tables (§5).
12. **Scripted test embedder (PR 2).** `internal/brain/scripted_embedder_test.go`: a test-local
    `ports.EmbeddingProvider` with per-text vectors, per-text failures, NaN/zero/wrong-dimension
    vectors and a call counter. It is the FX-D/FX-D2 embedder named in §6.
13. **Header alignment.** The header sentence "m4e2 depends on this slice for `DecisionLog.Before`,
    the action-vocabulary file, the change decoder (§3.6) and the cross-origin body table
    (§3.12 G6)" gains `brain.ErrWriteLanded` / `*WriteLandedError` (PR 3), the I22 whitelist test
    (G12) and the `ui.Deps` / `uiDeps` / `wiring.go` pattern with the layout nav, matching
    `../m4e2-admin/design.md` lines 18-28.
14. **`:1930` ripple justification (§5).** The `consolidate_test.go` seed rows are justified as:
    every seed gets `Origin: "derived"` **to avoid rule 4 treating an empty-origin seed as
    non-derived** (it would reinforce instead of overwriting in place). "Rule 4" is the
    edited-key rule under §3.3's original numbering; it is rule 3 once item 9 reorders. The
    `ErrBeliefProtected` effect of the store guard is the second, not the primary, reason.

Addendum (2026-10-08, activity split): items 1 and 11 each have a PR 6 half, and items 3 and 6
mention PR 5 or PR 6; those PRs are now `m4e-activity`'s. The activity halves of items 1 and 11
and the tasks-phase note on mutants A4 and A7 are repeated in
[`m4e-activity` design §10](../m4e-activity/design.md). Item 3's remedy (split beliefs from
activity) is the one carried out; the text above is left as the record of what was decided
then.
