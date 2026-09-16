# Design — m3e: the pending question (the digest asks, and the answer lands)

Technical design for `m3e-pending-question`, closing `m3d` finding **J24** and the two gaps
underneath it that J24's wording does not reach
([`proposal.md`](proposal.md) §1). Scope is that document's §3.2 in full: the pending-question
store, the digest's second item source, the disambiguation, the confirm path, and the four
governing documents.

`m3e` is the first change since M2 to need a migration, and the first to make **I09** true.

It does not restate requirements — that is `spec.md`, written concurrently and never read or
edited here. It does not edit `docs/`; it *describes* the doc deltas each PR carries, because
`CLAUDE.md` non-negotiable #1 requires the doc delta to ship in the same PR as the code.

> **Five things this design decides that the proposal did not anticipate**, each flagged for owner
> review rather than applied silently:
> 1. **`pending_questions.relation_id` carries no foreign key at all** (§3.1). Every one of the
>    three FK behaviours loses something the table exists to keep; the referential check moves to
>    INSERT time instead, where it cannot cascade and cannot block a delete.
> 2. **`consolidation.ProposedRelation` gains a `Band` field** (§3.4). Without it, `brain` would
>    recompute a decision `ProposeRelation` already made — "one rule in two languages".
> 3. **`rel.ID` is not necessarily the stored relation's id** (§3.4, N1). `Upsert`'s own contract
>    keeps the FIRST id on an existing triple. A question created with the losing id points at
>    nothing, and the guard is structural rather than a precondition somebody remembers.
> 4. **The confirm path emits its signal AFTER the raise — the opposite of I10's order** (§3.6),
>    for the same underlying reason, pointed the other way.
> 5. **`capture.go`'s own relation judge still creates no question** (§3.4, N2). The proposal
>    names `consolidate.go` only; widening it silently is how scope creeps, so it is an
>    owner-review item with its cost stated.

---

## 1. Ground truth this design was verified against

Every row was read at the named file and line in this session, at `988daae`.

| Claim | Verified at |
|---|---|
| `relations` FKs both endpoints to `units(id) ON DELETE CASCADE`, and is `UNIQUE (from_unit_id, to_unit_id, type)` | `internal/store/sqlite/migrations/0001_core_tables.sql:30-40` |
| `relation_thresholds` carries exactly `min_confidence_to_persist` (DEFAULT 0.3) and `min_confidence_to_surface` (DEFAULT 0.5) — **no third column** | `0002_learning_and_search.sql:30-35` |
| Migrations on disk are `0001`, `0002`, `0003`. The next is `0004` | `internal/store/sqlite/migrations/` |
| `units.content` is the only text column; there is no title | `0001_core_tables.sql:6-21` |
| A partial index is already house practice (`WHERE status = 'pool' AND type = 'insight'`) | `0001_core_tables.sql:25-28` |
| No table in the schema carries a `CHECK` constraint on a vocabulary column | `0001`, `0002`, `0003` |
| `relation.Decide` partitions `[Persist, Surface)` as `Uncertain`, with **both boundaries inclusive toward the higher band** | `internal/core/relation/verdict.go:28-37` |
| `relation.Resolve(nil)` falls back to the two package defaults; core never reads `relation_thresholds` itself | `internal/core/relation/thresholds.go:26-38` |
| `ProposeRelation` returns `(ProposedRelation, bool)` and **discards the band it computed** | `internal/core/consolidation/connect.go:164-186` |
| `connect.go:150-151` still says *"the Uncertain band is stored AND asked about (I09), and the asking is M3's"* | that file, verbatim |
| `RelationRepo` declares `Upsert, ByUnit, ThresholdsFor, Evidence, ExistingPairs, Delete` — **no read by id** | `internal/ports/relationrepo.go:54-131` |
| `Upsert` on an existing triple keeps the FIRST row's `ID`, `CreatedBy` and `CreatedAt`; only `Strength` and `Confidence` change | `relationrepo.go:56-65` |
| `RelationRepo.Delete` is the one carve-out from the I03 port sweep, by the 2026-08-24 owner ruling recorded at the sweep | `relationrepo.go:109-130` |
| `RelationRepo.Evidence`'s doc comment states the house reason for declaring a join in a port: *"relations, then units, then a zip in brain is two round trips and a correctness hazard if a unit moves between them"* | `relationrepo.go:84-97` |
| `resolveRelationCheckIn` writes one `ActionCaptureCheckInUnmatched` row naming m3e and returns | `internal/brain/checkin.go:108-122` |
| `RejectRelation` takes `rel ports.Relation` and reads **only `rel.ID`** | `checkin.go:130-148` |
| `resolveCheckIn` takes the most recent from `triggers.Delivered`, records `open_check_ins: N`, and records an unmatched row when nothing is open | `checkin.go:22-54` |
| `recordCheckIn`'s Context is `{trigger_id, resolution, open_check_ins}` | `checkin.go:151-160` |
| `classify.AllRelationOutcomes()` is exactly `{confirmed, rejected}` | `internal/core/classify/outcomes.go:34-42` |
| `ports.SignalRelationConfirm` exists, is in `AllSignalTypes()`, is exercised by `repocontract`, and has **zero production call sites** | `internal/ports/signalrepo.go:29`, `:45`; `test/support/repocontract/signalrepo.go:39` |
| `assembleDigest` returns early on `len(pending) == 0` and sources items **only** from `triggers.Undelivered` | `internal/brain/digest.go:53-64` |
| `digestHistoryDays = prospection.MaxDigestDeferrals + 2`, and the history slice is already read before the due check | `digest.go:24`, `:44-51` |
| `heldCounts` derives the deferral counter from `ActionCheckDigestHeld` rows — *"the audit trail IS the counter"* | `digest.go:186-211`, `:116-120` |
| `assembleDigest` returns 0 immediately when `r.channel == nil` | `digest.go:33-42` |
| `renderDigest` takes `(carry, pending)` and emits a header plus one bullet per item | `digest.go:219-235` |
| `checkRunner` holds `triggers, timers, ids, log, channel, conversation, units, state` — **no `rels`** | `internal/brain/check.go:248-270` |
| `checkDetail` is a fixed four-field struct `{id, fire_at, verdict, table}` | `check.go:281-293` |
| `captureRunner` holds `ids, units, embeds, lex, rels, log, llm, judge, chat, embed, index, recall, correction` | `internal/brain/capture.go:117-139` |
| `judgeAndPersistPair` already performs the `ThresholdsFor` → `relation.Resolve` pair before `ProposeRelation` | `internal/brain/consolidate.go:493-498` |
| `docs/02-cognitive-core.md:350-353` is the whole `confirmed_floor` surface: *"Confirming raises confidence (`GREATEST(current, confirmed_floor)`)"* | that file |
| The calibration gate matches only `internal/core/<pkg>.<Symbol>`, so a constant in `internal/brain` needs no §13 row — `digestHistoryDays` is the shipped precedent | `test/conformance/calibration_doc_test.go:35`; `digest.go:24` |

**`m3e` names zero new calibratable constants**, and that is Q1's whole point: the floor already
exists, in `relation_thresholds.min_confidence_to_surface`. §13 is untouched.

---

## 2. What `m3e` decides, in one paragraph

`m3e` owns **where a question the brain asked lives while it waits for an answer** (a dedicated
table, §3.1, and ADR-0027), **what the digest does with one** (at most one, appended after the
ranked items, never on a depleted morning, §3.5), **when an unanswered one stops being open**
(counted in digests, not in wall time, §3.5), **which relation an inbound answer is about**
(`resolveCheckIn`'s shipped shape, reused rather than reinvented, §3.6), and **what a confirmation
does to a confidence** (one pure comparison in `internal/core/relation`, against a number doc 02
§4 already names, §3.3). It decides no second question kind, no classify-prompt widening, no
learning consumption, and no re-ask.

---

## 3. Decisions

### 3.1 The table, and the foreign key that is deliberately absent

```sql
-- 0004_pending_questions.sql
--
-- A question the brain asked and is waiting on. See docs/03-data-model.md
-- and docs/adr/0027-pending-question-store.md.
CREATE TABLE pending_questions (
  id           TEXT PRIMARY KEY,
  kind         TEXT NOT NULL,   -- relation (the only member m3e writes)
  relation_id  TEXT NOT NULL,   -- relations(id); deliberately NOT a foreign key
  created_at   TEXT NOT NULL,   -- when consolidation decided to ask
  asked_at     TEXT,            -- NULL = not yet surfaced in a digest
  resolved_at  TEXT,            -- NULL = still open
  resolution   TEXT             -- confirmed|rejected|expired; NULL while open
);

-- The digest's queue: unasked, oldest first.
CREATE INDEX idx_pending_questions_unasked ON pending_questions(created_at)
  WHERE asked_at IS NULL AND resolved_at IS NULL;

-- The disambiguation pool and the expiry sweep: asked, unanswered.
CREATE INDEX idx_pending_questions_open ON pending_questions(asked_at)
  WHERE asked_at IS NOT NULL AND resolved_at IS NULL;
```

**Three states, and each is a column rather than an inference** — the property that made the
dedicated table win over `triggers` and `decision_log` (proposal §4):

| `asked_at` | `resolved_at` | State | Who reads it |
|---|---|---|---|
| NULL | NULL | **queued** — created, not yet asked | the digest's `Unasked` |
| set | NULL | **open** — asked, unanswered | disambiguation, expiry |
| set | set | **closed** — `confirmed` \| `rejected` \| `expired` | the glass box, and M4 |

`kind` holds one member and exists from the first row. Proposal §4 already paid for that argument.

**No `CHECK` constraint on `kind` or `resolution`**, and this is a decision rather than an
omission: no table in this schema carries one (§1), a published migration is never modified, and
`kind` is designed to grow. The vocabularies are pinned the way `m3b` pinned its three — an
`AllX()` in `internal/ports`, an L2 test against this migration's own column comments, and an L3
`SELECT DISTINCT resolution` after a real pass. §3.2's port shape is what makes that sufficient:
**no method takes a `resolution` parameter**, so there is no channel through which an unvetted
string could reach the column.

**`relation_id` carries no foreign key.** Every FK behaviour loses something:

| Option | Verdict |
|---|---|
| `REFERENCES relations(id) ON DELETE CASCADE` | **Rejected.** `RejectRelation` deletes the relation, so the question row that records the rejection would vanish *with the thing it recorded* — deleting the audit of a deletion. Non-negotiable #6, and the exact silent data loss this design was asked to avoid |
| `ON DELETE RESTRICT` | **Rejected.** I10 emits the `relation_reject` signal **before** the delete. A RESTRICT would fail that delete and strand an emitted signal for a rejection that did not happen — precisely the hazard `checkin.go:98-104` exists to prevent |
| `ON DELETE SET NULL` | **Rejected.** Requires `relation_id` nullable, keeps the row, and destroys the one column the table exists for. "Which relation was this about" would become unanswerable at exactly the moment it matters |
| **No FK, plain `TEXT NOT NULL`** — chosen | `decision_log` already carries unit and trigger ids with no FK, for the identical reason: an audit row must outlive its subject. A dangling id is the honest historical record of a relation that was rejected |

**The referential check moves to INSERT time instead**, where it costs nothing and cannot
cascade:

> `PendingQuestionRepo.Create` inserts with
> `... WHERE EXISTS (SELECT 1 FROM relations WHERE id = ?)` and returns
> `ports.ErrRelationNotFound` on zero rows affected.

That is a guard that can fail on its own violation — the property `m3b` §3.6 paid to learn — and
it is what closes **N1** (§3.4) structurally rather than by precondition. It constrains only the
insert; it places no obligation on a later delete, which is the whole reason the FK was refused.

**Doc 03 and the schema goldens.** `docs/03-data-model.md` gains the table in the section
`0004`'s header points at, `schema_doc_test.go`'s anchor list gains its entry, and the schema
golden is regenerated by its make target, never by hand. The regeneration-diff gate lives in
`make check-all`.

### 3.2 The port

```go
// internal/ports/pendingquestionrepo.go

// QuestionKind is what a pending question is about. One member today; the
// column exists so a second costs a row rather than a migration (ADR-0027).
type QuestionKind string
const QuestionKindRelation QuestionKind = "relation"
func AllQuestionKinds() []QuestionKind

// QuestionResolution is how a question stopped being open. Deliberately not
// ports.TriggerResolution: engaged|declined|self_healed would record a
// relation confirmation as "engaged", an audit row that misdescribes what
// happened (doc 02 §11, and proposal §4's own argument).
type QuestionResolution string
const (
	QuestionConfirmed QuestionResolution = "confirmed"
	QuestionRejected  QuestionResolution = "rejected"
	QuestionExpired   QuestionResolution = "expired"
)
func AllQuestionResolutions() []QuestionResolution

// PendingQuestion is the WRITE shape. A question is always created queued
// and open: asked_at, resolved_at and resolution have no field here, so a
// row born already-answered is unrepresentable rather than merely refused.
type PendingQuestion struct {
	ID         string
	Kind       QuestionKind
	RelationID string
	CreatedAt  time.Time
}

// RelationQuestion is the READ shape: one row joined to the relation it is
// about and to both of that relation's endpoints, in one read.
// RelationRepo.Evidence's own stated reason applies unchanged — the
// alternative is three round trips and a hazard if a row moves between them.
//
// Both reads return it, though only the digest renders the two contents:
// the expiry row's rationale names both endpoints too ("the question about
// X and Y expired unanswered"), so the join is load-bearing on both.
type RelationQuestion struct {
	ID           string
	RelationID   string
	RelationType string
	FromUnitID   string
	ToUnitID     string
	FromContent  string
	ToContent    string
	CreatedAt    time.Time
	AskedAt      *time.Time // nil on every row Unasked returns, by construction
}

type PendingQuestionRepo interface {
	// Create persists q. It returns ErrRelationNotFound when q.RelationID
	// names no relation — §3.1's INSERT-time check, which is what a
	// foreign key was refused in favour of.
	Create(ctx context.Context, q PendingQuestion) error

	// Unasked returns every queued question whose relation still exists,
	// oldest created_at first, then id. Named for what it returns
	// (UnitRepo's own rule): there is no Questions(state) read.
	Unasked(ctx context.Context) ([]RelationQuestion, error)

	// Open returns every asked, unanswered question whose relation still
	// exists, most recent asked_at FIRST, then id — Delivered's own order,
	// because §3.6 makes the same "most recent" choice resolveCheckIn does.
	Open(ctx context.Context) ([]RelationQuestion, error)

	// MarkAsked stamps asked_at. Precondition asked_at IS NULL, in the
	// UPDATE's WHERE clause and nowhere else (m3b §3.1/§3.6).
	MarkAsked(ctx context.Context, id string, at time.Time) error

	// Confirm, Reject and Expire each set resolved_at and their own
	// resolution literal, in ONE statement, with resolved_at IS NULL as
	// the precondition. No method takes a resolution parameter: that is
	// the channel through which an unvetted string would reach a column
	// with no CHECK, and m3b §3.1 already closed it for two other tables.
	Confirm(ctx context.Context, id string, at time.Time) error
	Reject(ctx context.Context, id string, at time.Time) error
	Expire(ctx context.Context, id string, at time.Time) error
}

var ErrQuestionNotFound = errors.New("pending question not found")
var ErrQuestionStatusConflict = errors.New("pending question is no longer in the expected state")
```

**Seven methods, seven callers** — `Create` (consolidate), `Unasked` (digest), `Open`
(disambiguation *and* the expiry sweep), `MarkAsked` (digest), `Confirm`/`Reject` (the check-in
path), `Expire` (the sweep). No method with no caller, and **no `Delete`-prefixed method**, so
`i03_units_never_deleted_test.go`'s port sweep stays satisfied over every repository interface
with no widening and no second carve-out.

**Both reads filter on the relation still existing** (an inner join on `relations`), which is
R8's mitigation as the proposal stated it: a question whose relation is gone is skipped, not
failed on. It is not orphaned — §3.5's sweep closes it as `expired`.

**`RelationRepo` gains exactly one method:**

```go
// ByID returns the relation with id, or ErrRelationNotFound. The confirm
// path needs the WHOLE row: Upsert revises confidence in place (I07) and
// takes a complete Relation, so strength, created_by and created_at must
// travel back unchanged. A narrow read would force the caller to invent
// values for three columns it must not change.
ByID(ctx context.Context, id string) (Relation, error)
```

### 3.3 The confirm arithmetic — Q1, and where it lives

Owner ruling Q1: **`confirmed_floor` IS the relation type's own `min_confidence_to_surface`.** No
constant, no column, no §13 row.

```go
// internal/core/relation/confirm.go

// ConfirmedConfidence is doc 02 §4's GREATEST(current, confirmed_floor) with
// owner ruling Q1's answer substituted: the floor is the type's own
// Surface threshold. A confirmed relation lands at or above the edge of the
// band it was in, and Decide's boundaries are inclusive toward the higher
// band (verdict.go:20-27), so it leaves the band by construction.
//
// Written as !(current > t.Surface) rather than math.Max, and that is a
// decision: relations.confidence is a REAL column with no CHECK, so a NaN
// can be read back from it. math.Max would propagate the NaN into the row,
// and Decide would then read NaN as Asserted by ACCIDENT — both comparisons
// fail, so it falls through to the default arm. This form lands NaN on the
// floor, where the row is honest and the property below holds for a reason
// rather than by luck. Same posture ResolveInterrupt takes for the same
// class of column (prospection/delivery.go:34-40).
func ConfirmedConfidence(current float64, t Thresholds) float64 {
	if !(current > t.Surface) {
		return t.Surface
	}
	return current
}
```

**Purity, per non-negotiable #3.** It imports nothing but its own package, takes no `time.Time`
(there is no instant in a comparison), reads no `relation_thresholds` row — `brain` performs that
lookup with the shipped `ThresholdsFor` → `relation.Resolve` pair, exactly as
`judgeAndPersistPair` already does at `consolidate.go:493-498`. `depguard`'s `core-purity` rule
enforces the first; `thresholds.go:31-32`'s own recorded rule is the second.

**Three properties, asserted as L1 tests rather than trusted:**

1. `Decide(ConfirmedConfidence(c, t), t) == Asserted` for every `c` and every `t` — *confirming
   always leaves the band*, which is the whole point and the thing a future edit could break.
2. `ConfirmedConfidence(ConfirmedConfidence(c, t), t) == ConfirmedConfidence(c, t)` — idempotent,
   which is what makes §3.6's retry-safe ordering safe.
3. `ConfirmedConfidence(c, t) >= c` for every non-NaN `c` — confirming never *lowers* a
   confidence. A relation already above the floor keeps its own number.

Q1's accepted consequence, recorded here so it is not rediscovered: a later raise of
`min_confidence_to_surface` puts a confirmed relation back inside the band. It is **not** re-asked
— §3.5 asks only queued questions, and the confirmed one is closed. It would be re-asked only if
some future pass created a *new* question for it, which nothing in `m3e` does.

### 3.4 Creating the question

`ProposeRelation` computes `relation.Decide` and throws the answer away
(`connect.go:174-176`). The caller needs it.

| Option | Verdict |
|---|---|
| `brain` recomputes `relation.Decide(proposed.Confidence, thresholds)` after the Upsert | One pure call with data already in hand and zero core change — but the same decision computed twice in two packages, and a future edit to `Decide`'s boundary would have to find both. That is the "one rule in two languages" drift `LiveFocusCandidates`' design refused |
| **`ProposedRelation` gains `Band relation.Verdict`** — chosen | The function already knows the answer. One field, one line, and the caller's branch reads `proposed.Band == relation.Uncertain` rather than re-deriving it. `ProposeRelation` still returns `(_, false)` for `Discard`, so `Band` is only ever `Uncertain` or `Asserted` on a returned plan — stated in its doc comment and asserted |

In `judgeAndPersistPair`, after the existing `Upsert` and its `ActionConnectRelationPersisted` row:

```go
if proposed.Band != relation.Uncertain {
	return nil
}
q := ports.PendingQuestion{ID: r.ids.New(), Kind: ports.QuestionKindRelation, RelationID: rel.ID, CreatedAt: now}
if err := r.questions.Create(ctx, q); err != nil { … }
return r.record(ctx, now, ports.ActionConnectQuestionCreated, …)
```

**N1 — `rel.ID` is not necessarily the stored relation's id, and this design depends on it being
one.** `Upsert`'s own contract (`relationrepo.go:60-64`) keeps the FIRST row's id when a row
already exists for the `(from, to, type)` triple. A question created with the freshly generated
`rel.ID` would then reference a row that does not exist. On connect's path it cannot happen —
`consolidation.ExistingPairs` excludes any pair that already carries a relation before the judge
is ever called — but that is a precondition three files away from the line that depends on it,
which is exactly the shape this repository has repeatedly paid to learn. §3.1's INSERT-time
`WHERE EXISTS` turns it into a loud failure at the only place it can occur, and an L3 test
inserts a question for an unknown relation id and asserts `ErrRelationNotFound`.

**N2 — `capture.go`'s own relation judge creates no question, and that is the proposal's scope
rather than this design's preference.** Proposal §3.2 item 1 names `consolidate.go` and only it.
Capture's judge is the dedup path, and asking about its output would interrupt the very capture
the user just made. Recorded as **owner-review item R2** with the cost stated: until it is widened,
an uncertain relation born at capture is stored and never asked about, so I09 is true of
consolidation's relations and silent about capture's.

`consolidateRunner` gains one field, `questions ports.PendingQuestionRepo`. It reads no clock —
`now` is the instant `ConsolidateService` already handed down, so
`brain_single_clock_read_test.go` is satisfied by construction.

### 3.5 The digest — one question, appended, and when it stops being open

Owner ruling Q2: **at most one relation question per digest, appended after `prospection.Carry`'s
ranked items, competing with nothing.** A relation has two endpoints and `focus.Priority` ranks
one unit; inventing a candidate to make it rankable would be the invented number this repository
refuses (`prior.go:9-19`'s rule, applied).

`assembleDigest` is restructured. Today it returns early on `len(pending) == 0`
(`digest.go:57-64`), which would keep a vault with an uncertain relation and no triggers silent
forever — the demo the proposal's §2 describes.

```
if r.channel == nil                            → 0            (unchanged)
history  := log.Since(now - digestHistoryDays)                (unchanged)
if !DigestDue(lastDigestAt(history), now)      → 0            (unchanged)
energy   := state.LatestEnergy(); low := prospection.LowEnergy(energy, now)
expired  := r.expireStaleQuestions(ctx, history, now, commit) ← BEFORE anything is sent
pending  := triggers.Undelivered(ctx)
question := nil; if !low { question = r.nextQuestion(ctx) }
if len(pending) == 0 && question == nil        → 0
items/Carry over `pending` only                               (unchanged)
if len(carry) == 0 && question == nil          → 0
channel.Send(renderDigest(carry, pending, question))
for each carried: triggers.Surface(…)          ; if question != nil: questions.MarkAsked(…)
record ActionCheckDigestSent, ActionCheckDigestHeld×n, ActionCheckDigestQuestionAsked
```

**Three rules inside that, each argued:**

1. **A question alone is enough to send a digest.** The "an empty digest is not sent" rule
   (`digest.go:58-63`) exists because a message saying *nothing happened* trains the eye to skip
   the shape the message that matters arrives in. A digest that asks *"I linked X with Y, are they
   related?"* is not empty — it has content and it wants something. This is also what makes I09
   reachable on a vault with no triggers, which is the proposal's own demo.
2. **No question on a low-energy morning.** Doc 02 §7's care gate holds back non-urgent items, and
   a graph question is the most non-urgent thing this system produces. Expressed as a rule rather
   than a rank, which is the same trade Q2 already took, and it needs no number:
   `prospection.LowEnergy` is already computed in this function.
3. **The tie-break is `created_at`, then `id` — FIFO, not confidence.** Ranking by confidence
   would ask first about the relation *closest to asserting itself anyway*, which is the question
   that matters least. FIFO also drains the queue in order, so a question created today cannot
   wait behind one created tomorrow.

**Expiry — Q4, and who runs it.**

```go
// expireStaleQuestions closes every open question that MaxDigestDeferrals
// digests have gone out on without an answer, as a state transition
// (resolution = expired). Nothing is deleted — non-negotiable #6.
//
// It runs HERE and not in nightly consolidation, because the bound is
// counted in DIGESTS and the digest path is the only one that reads
// decision_log's ActionCheckDigestSent rows. Consolidation would need a
// second reader of the same audit rows to answer a question it does not
// otherwise ask — heldCounts' own argument, applied to a second counter.
//
// digestHistoryDays is already MaxDigestDeferrals + 2, so the window this
// pass has ALREADY read is exactly wide enough to count them, and this
// costs no additional read.
func (r checkRunner) expireStaleQuestions(ctx context.Context, history []ports.Decision, now time.Time, commit bool) (int, error)
```

The count is `len(rows where Action == ActionCheckDigestSent && OccurredAt.After(*q.AskedAt))`,
and `>= prospection.MaxDigestDeferrals` expires. It runs **before** the send, so the digest going
out now is not counted against the question it may be carrying. `!commit` (`nooma check
--dry-run`) counts and writes nothing, matching every other arm of this pass.

**Two properties fall out, and both are worth writing down.** A question is asked **once**
(`MarkAsked`'s `asked_at IS NULL` precondition, and `Unasked` never returns an asked row), so the
open pool is bounded at `MaxDigestDeferrals` — Q4's stated motivation ("the disambiguation pool
grows without bound") is closed by arithmetic rather than by a second rule. And a vault whose
digests stop going out expires nothing, which is correct: it also asked nothing.

**Rendering.** `renderDigest(carry, pending, question)` keeps its header and bullets and appends
one paragraph:

```
Here are 3 things for today:
• …
• …
• …

One more — I linked "<from>" with "<to>". Are they related?
```

`units.content` is unbounded (§1) and a digest line is not, so both are truncated to
`questionSnippetRunes` (rune-aware, ellipsis on truncation). That constant lives in
`internal/brain`, **not** `internal/core`, so `calibration_doc_test.go` requires no §13 row — the
gate matches `internal/core/<pkg>.<Symbol>` and `digestHistoryDays` is the shipped precedent for a
brain-side bound. The justification is not the gate, though: it is a rendering bound with no
decision behind it. Nothing branches on it.

`checkRunner` gains one field, `questions ports.PendingQuestionRepo`, **nil-tolerant** in exactly
the way `channel`, `units` and `state` already are (`check.go:253-270`): a pass without it still
fires and expires, it simply asks nothing.

### 3.6 Disambiguation and the two resolutions

`resolveRelationCheckIn` mirrors `resolveCheckIn` (`checkin.go:22-54`) shape for shape, because
that pattern is shipped, tested and accepted — proposal §1's own reading.

```go
func (r captureRunner) resolveRelationCheckIn(ctx context.Context, c classify.Classification, now time.Time) error {
	if c.RelationOutcome == nil {
		return nil
	}

	open, err := r.questions.Open(ctx)
	if err != nil { … }

	if len(open) == 0 {
		// Q5: an answer with nothing open resolves nothing and deletes
		// nothing — INCLUDING a rejection. Deletion is the one
		// irreversible act in the vault and disambiguation is a
		// heuristic; refusing to guess which relation to delete is the
		// only safe direction. resolveCheckIn's identical treatment.
		return r.recordRelationCheckIn(ctx, now, ports.ActionCaptureRelationCheckInUnmatched, …, "", "", *c.RelationOutcome, 0)
	}

	target := open[0] // most recent asked_at; Open orders that way
	switch *c.RelationOutcome {
	case classify.RelationOutcomeConfirmed:
		err = r.ConfirmRelation(ctx, target, now)
	case classify.RelationOutcomeRejected:
		err = r.RejectRelation(ctx, target.RelationID, now)
		// resolveRelationCheckIn then closes the question as rejected.
	}
	if err != nil { … }

	return r.recordRelationCheckIn(ctx, now, ports.ActionCaptureRelationCheckInResolved, …,
		target.ID, target.RelationID, *c.RelationOutcome, len(open))
}
```

`open_relation_questions: N` is the field that makes "we chose the most recent" auditable rather
than invisible, verbatim from `recordCheckIn`'s own doc comment — proposal §2's third success
criterion.

**`RejectRelation` narrows to `(ctx, relationID string, now)`.** It reads only `rel.ID` today
(`checkin.go:130-148`), and a parameter with no reader is what `UpdateEventAt`'s doc comment
refuses (`unitrepo.go:57-65`). `checkin_test.go:221` stays as a test — its call expression
changes by one argument, and the path it exercises gains its first real caller. **I10's ordering
is untouched**: signal first, then delete.

**`ConfirmRelation` is `RejectRelation`'s sibling, and its ordering is deliberately the opposite:**

```go
// ConfirmRelation raises one relation's confidence out of the uncertain
// band and emits relation_confirm — doc 02 §4, and that signal's first
// call site anywhere in this tree.
//
// **The signal is emitted AFTER the raise, which is the OPPOSITE of I10's
// order, for the same underlying reason pointed the other way.** I10 emits
// first because its effect is a DELETE: irreversible, so the recoverable
// error (a signal for a relation that survived) is the one to prefer. Here
// the effect is idempotent — ConfirmedConfidence applied twice is
// ConfirmedConfidence applied once — so re-running the raise costs nothing,
// while a signal emitted for a raise that then failed is evidence the
// learning module would tune on forever. Same principle (never emit
// evidence for an effect that may not have happened), opposite direction,
// and the two are stated together so the next reader harmonises neither
// into the other.
func (r captureRunner) ConfirmRelation(ctx context.Context, q ports.RelationQuestion, now time.Time) error {
	rel, err := r.rels.ByID(ctx, q.RelationID)          // §3.2
	row, err := r.rels.ThresholdsFor(ctx, rel.Type)     // brain reads, core does not
	rel.Confidence = relation.ConfirmedConfidence(rel.Confidence, relation.Resolve(row))
	err = r.rels.Upsert(ctx, rel)                       // I07: revises in place
	err = r.signals.Record(ctx, ports.Signal{
		ID: r.ids.New(), Type: ports.SignalRelationConfirm, Valence: ports.ValencePositive,
		TargetKind: &relationTarget, TargetID: &rel.ID, OccurredAt: now})
	return err
}
```

`Upsert` is handed the row `ByID` returned with only `Confidence` replaced, so `Strength`,
`CreatedBy` and `CreatedAt` travel back unchanged — belt and braces beside `Upsert`'s own
contract, which already refuses to rewrite them.

**Failure posture, stated per step** rather than left to be discovered:

| Step fails | Result | Why acceptable |
|---|---|---|
| the raise | nothing written, error returned | The question stays open; the next answer retries |
| the confirm signal | the raise stands, no signal | The relation left the band, which is the user-visible half. The learning module missed one datum |
| `questions.Confirm` | the raise and signal stand, the question stays open | A second "yes" re-confirms idempotently (§3.3 property 2) |
| `questions.Reject` after the delete | the relation is gone, the question stays open | Both reads inner-join `relations`, so it is invisible; §3.5's sweep closes it as `expired` |

`captureRunner` gains one field, `questions ports.PendingQuestionRepo`, and `signals` is already
there (`RejectRelation` uses it).

### 3.7 What `decision_log` records

**Five `DecisionAction` members are added**, and none is reused:

| Member | Value | When |
|---|---|---|
| `ActionConnectQuestionCreated` | `consolidate.connect.question_created` | §3.4 — an Uncertain relation was stored and a question queued |
| `ActionCheckDigestQuestionAsked` | `check.digest.question_asked` | §3.5 — a question was surfaced |
| `ActionCheckQuestionExpired` | `check.question.expired` | §3.5 — `MaxDigestDeferrals` digests, no answer |
| `ActionCaptureRelationCheckInResolved` | `capture.relation_checkin.resolved` | §3.6 |
| `ActionCaptureRelationCheckInUnmatched` | `capture.relation_checkin.unmatched` | §3.6, Q5 |

**Why the last two do not reuse `ActionCaptureCheckInResolved`/`Unmatched`.** `recordCheckIn`'s
Context is `{trigger_id, resolution, open_check_ins}` (`checkin.go:151-160`). A relation answer's
context is `{question_id, relation_id, resolution, open_relation_questions}` — different shape,
and m2c §7.5's rule (quoted verbatim in m3b §3.5) splits effects when their Context shapes differ.
Putting a question id into a field named `trigger_id` is the audit row that misdescribes what
happened, which doc 02 §11 forbids by name — and it is the proposal's own argument against
reusing the `triggers` table, applied one level down.

So `checkin.go` gains a `recordRelationCheckIn` beside `recordCheckIn`, and `check.go` gains a
`questionDetail{question_id, relation_id, resolution}` beside `checkDetail` — for the same reason,
since `checkDetail`'s four fields are `{id, fire_at, verdict, table}` and three of them would be
empty on every question row.

**I12 as arithmetic, not a promise:** queueing, asking, expiring, confirming and rejecting are the
five effects, and each writes exactly one row. A pass that queued nothing, asked nothing and
expired nothing writes none — `TestConsolidate_NoEffects`' own MUST, extended.

### 3.8 ADR-0027

`docs/adr/0027-pending-question-store.md`, `Accepted`, with `docs/adr/README.md`'s index row.

**Decision**: *a question the brain asked lives in a dedicated `pending_questions` store while it
waits for an answer — not in `triggers`, and not derived from `decision_log`.*

**Alternatives, with the reasons proposal §4 already recorded**: `triggers` (its resolution
vocabulary `engaged|declined|self_healed` would misdescribe a relation confirmation, and its
single `unit_id` cannot hold a relation's two endpoints); `decision_log` (the glass box is not a
state store, and it gives M4 no queryable worklist).

**Consequences**: migration `0004`, forward-only; a `kind` column carrying one member so a second
question kind costs a row rather than a migration; no FK on `relation_id`, with §3.1's three
rejected behaviours named; and no `Delete`-prefixed method, so the I03 sweep needs no second
carve-out.

**Q1's ruling ships inside this ADR as a `Related decision` section, not as `0028`**, and that
deviates mildly from proposal §5's "it decides one thing". The reason: `confirmed_floor := the
type's own min_confidence_to_surface` is not an architectural fork with two live alternatives — it
is doc 02 §4 naming a number it already had, and doc 02 §4's own amendment is what carries it. An
ADR per prose clarification would dilute the bar `0020`–`0026` were held to. Recorded as
**owner-review item R1**; splitting it into `0028` costs one file and no code.

---

## 4. Package layout and dependency map

```
internal/store/sqlite/migrations/
└── 0004_pending_questions.sql          the table + two partial indexes        PR 1

internal/core/relation/
└── confirm.go                          ConfirmedConfidence                    PR 2

internal/core/consolidation/
└── connect.go                          + ProposedRelation.Band; the stale
                                        "the asking is M3's" comment corrected PR 2

internal/ports/
├── pendingquestionrepo.go              QuestionKind, QuestionResolution,
│                                       AllX(), PendingQuestion,
│                                       RelationQuestion, PendingQuestionRepo  PR 3
├── relationrepo.go                     + ByID                                 PR 3
└── decisionlog.go                      + 5 members                      PR 4/5/6

internal/store/sqlite/
├── pendingquestionrepo.go              the seven statements, both joins       PR 3
└── relationrepo.go                     + ByID                                 PR 3

internal/brain/
├── consolidate.go                      + questions field; the Uncertain branch PR 4
├── digest.go                           second source, expiry sweep,
│                                       renderDigest's question paragraph      PR 5
├── check.go                            + questions field, questionDetail      PR 5
├── checkin.go                          resolveRelationCheckIn, ConfirmRelation,
│                                       recordRelationCheckIn                  PR 6
└── capture.go                          + questions field                      PR 6

cmd/nooma/wiring.go                     three constructors                  PR 4/5/6
```

**Edges.** `ports` imports `internal/core/relation` already (`relationrepo.go:9`) and gains
nothing new. `internal/core/relation` imports the standard library only — `ConfirmedConfidence`
does not even need `math`. `brain` imports `ports`, `core/relation`, `core/consolidation` and
`core/prospection`, all already. **`internal/core` imports nothing new in either direction**, and
`depguard`'s `core-purity` rule is what makes that free rather than reviewed.

**`store_api.golden` widens** — one new file under `internal/store/sqlite` adds exported `type`
and `func` lines. Regenerated by its make target, never by hand; the diff is the review artifact.

---

## 5. Data flow

```
 ══ nightly, 03:00 ═══════════════════════════════════════════════════════════
  connect ─► judge ─► ProposeRelation(…) ─► (ProposedRelation{…, Band}, ok)
                                                   │
                            Band == Asserted ──────┤────── Band == Uncertain
                                   │                              │
                            rels.Upsert                    rels.Upsert
                                   │                              │
                                   │                    questions.Create(relation_id)
                                   │                       │ WHERE EXISTS(relations)
                                   └──── decision_log ─────┴─► consolidate.connect.
                                                                 question_created
 ══ the 07:00 digest ═════════════════════════════════════════════════════════
  DigestDue ─► LowEnergy ─► expireStaleQuestions(history)  ─► check.question.expired
                   │                                            (resolution=expired)
                   │ low? no question at all
                   ▼
       triggers.Undelivered ──► Carry ──► carry / held
                   │                        │
       questions.Unasked ──► [0] ───────────┤
                                            ▼
                          renderDigest(carry, pending, question)
                                            │
                                    channel.Send
                                            │
                  triggers.Surface ×n ──────┴────── questions.MarkAsked
                                            │
                                     decision_log: check.digest.sent,
                                     check.digest.held ×n,
                                     check.digest.question_asked
 ══ the answer arrives ═══════════════════════════════════════════════════════
  inbound text ─► classify ─► RelationOutcome{confirmed|rejected}
                                     │
                          questions.Open()  ── empty ─► capture.relation_checkin.
                                     │                    unmatched   (Q5: nothing
                                     │                    resolved, nothing deleted)
                              open[0] (most recent)
                    ┌────────────────┴────────────────┐
              confirmed                           rejected
                    │                                 │
        rels.ByID + ThresholdsFor              signals.Record(relation_reject)
        relation.ConfirmedConfidence                   │            ← I10, first
                    │                          rels.Delete(id)
             rels.Upsert  (I07)                        │
                    │                          questions.Reject
        signals.Record(relation_confirm)               │
                    │        ← §3.6, after            │
             questions.Confirm                         │
                    └──── decision_log: capture.relation_checkin.resolved ────┘
```

---

## 6. File changes

| File | Action | What |
|---|---|---|
| `internal/store/sqlite/migrations/0004_pending_questions.sql` | Create | §3.1 |
| `docs/03-data-model.md` | Modify | The table, matching `0004` exactly |
| `internal/store/sqlite/testdata/schema/*.golden` | Modify | Regenerated |
| `test/conformance/schema_doc_test.go` | Modify | The anchor list |
| `docs/adr/0027-pending-question-store.md` + `docs/adr/README.md` | Create / Modify | §3.8 |
| `internal/core/relation/confirm.go` | Create | §3.3 |
| `internal/core/consolidation/connect.go` | Modify | `Band`; `:150-151`'s stale comment |
| `docs/02-cognitive-core.md` §4, §5 | Modify | The asking mechanism; `confirmed_floor`'s producer; `relation_outcome`'s resolution semantics. **No §13 row** |
| `docs/06-harness.md` | Modify | I09's row, if it names the mechanism |
| `internal/ports/pendingquestionrepo.go` | Create | §3.2 |
| `internal/ports/relationrepo.go` | Modify | `ByID` |
| `internal/ports/decisionlog.go` | Modify | Five members |
| `internal/store/sqlite/pendingquestionrepo.go` | Create | §3.2 |
| `internal/store/sqlite/relationrepo.go` | Modify | `ByID` |
| `test/support/memrepo/pendingquestions.go` | Create | The fake |
| `test/support/repocontract/pendingquestionrepo.go` | Create | The shared contract |
| `internal/brain/consolidate.go` | Modify | §3.4 |
| `internal/brain/digest.go`, `check.go` | Modify | §3.5 |
| `internal/brain/checkin.go`, `capture.go` | Modify | §3.6 |
| `cmd/nooma/wiring.go` | Modify | Three constructors |
| `test/conformance/i09_uncertain_band_asked_test.go` | Create | §8 — the invariant's first real test |

---

## 7. The PR chain

**Cached `delivery_strategy` is `single-pr`, and this is not one PR** — proposal R5 already said
so. The forecast belongs to `sdd-tasks`; the slices are named here so the cut is a measurement and
not a mood. Chain `stacked-to-main`. Every PR: **the conformance test is its own commit ahead of
the implementation commit** (proposal §6; `sdd-verify` reads the PR's `git log`).

| # | Branch | Content | Impl+docs |
|---|---|---|---|
| 1 | `feat/store-pending-questions-migration` | `0004`, doc 03, schema goldens, anchors, ADR-0027, README row. **Forward-only and inert** — nothing reads the table yet | ~180 |
| 2 | `feat/core-relation-confirmed-confidence` | `ConfirmedConfidence` + its three L1 properties, `ProposedRelation.Band`, `connect.go:151`'s correction, doc 02 §4's `confirmed_floor` producer. **Triggers `docs-sync`** (R7) | ~120 |
| 3 | `feat/ports-store-pending-questions` | The port, both vocabularies + `AllX()`, `RelationRepo.ByID`, the SQLite implementation with both joins and the `WHERE EXISTS` insert, memrepo + repocontract, `store_api.golden`. **L3 owns N1** | ~300 |
| 4 | `feat/brain-queue-relation-question` | The Uncertain branch in `judgeAndPersistPair`, one action, wiring | ~110 |
| 5 | `feat/brain-digest-asks` | The second source, the one-question cap, the low-energy rule, the FIFO tie-break, `renderDigest`'s paragraph, the expiry sweep, two actions. **I09's asking half** | ~300 |
| 6 | `feat/brain-relation-checkin` | `resolveRelationCheckIn`, `ConfirmRelation`, `RejectRelation`'s narrowing, `recordRelationCheckIn`, two actions, doc 02 §5. **I10 reachable, `relation_confirm`'s first emission** | ~250 |

Order 1 → 2 → 3 → 4 → 5 → 6. PR 1 must land first and alone: proposal §9 states the asymmetry —
the migration does not roll back, and a revert of everything after it leaves an inert empty table
read by nothing.

**The two at genuine risk are 3 and 5**, both budgeted at ~300 against a 400 ceiling that this
project's own measured 1.3×–4.3× multipliers (proposal R11 lineage) routinely clears. Their cuts
are named now: **3** splits into port-plus-fakes and SQLite-plus-golden; **5** splits into the
second source (the asking) and the expiry sweep, which is an autonomous property with its own
rollback. `sdd-tasks` should apply either cut if its own forecast exceeds 400.

---

## 8. Testing strategy

| Layer | What | Where |
|---|---|---|
| **L1** | `Decide(ConfirmedConfidence(c, t), t) == Asserted` for a swept `c` × `t`, including NaN — *confirming always leaves the band* | `internal/core/relation/confirm_test.go` (PR 2) |
| **L1** | `ConfirmedConfidence` is idempotent, and never lowers a non-NaN confidence | same (PR 2) |
| **L1** | `ProposeRelation` returns `Band ∈ {Uncertain, Asserted}` and never `Discard` on `ok == true`, table-driven over the band boundaries | `connect_test.go` (PR 2) |
| **L2** | **I09, both halves, and its first real conformance file.** A judgment landing in `[persist, surface)` (a) stores a relation and (b) produces exactly one question, whose next due digest's **rendered text names both endpoints**. Boundaries expressed as multiples of `relation.Default*`, never literals | `test/conformance/i09_uncertain_band_asked_test.go` (PR 4 red, PR 5 green) |
| **L2** | Both port vocabularies pinned to `0004`'s own column comments — `relation.AllCreatedBy`'s shape | `test/conformance/` (PR 3) |
| **L2** | The one-question cap: a digest with five queued questions carries exactly one, after every ranked item | `test/conformance/` (PR 5) |
| **L2** | The low-energy rule: a low-energy digest carries zero questions, and the queue is untouched | (PR 5) |
| **L2** | Expiry: at `MaxDigestDeferrals - 1` digests the question is still open; at `MaxDigestDeferrals` it is `expired`, and **the row still exists** with its `relation_id` intact | (PR 5) |
| **L2** | **I10 with a real relation on the end**: `checkin_test.go:221`'s ordering assertion, now reached through `resolveRelationCheckIn` | `checkin_test.go` (PR 6) |
| **L2** | **Q5**: a `rejected` answer with nothing open writes one unmatched row, deletes zero relations, resolves zero questions | (PR 6) |
| **L2** | Disambiguation: three open questions, one answer, `open_relation_questions: 3` in the row and the **most recently asked** one resolved | (PR 6) |
| **L2** | **I12 both directions**: five effects, five rows; a pass that decided nothing writes none | (PR 4, 5, 6) |
| **L2** | **I03**: the port sweep still finds no `Delete`-prefixed method on `PendingQuestionRepo`, with `RelationRepo.Delete` the only carve-out — asserted by the existing reflection sweep, unmodified | (PR 3) |
| **L3** | `repocontract` over a real migrated vault for both implementations | (PR 3) |
| **L3** | **N1**: `Create` with an unknown `relation_id` returns `ErrRelationNotFound` and inserts nothing | (PR 3) |
| **L3** | **The FK's absence, asserted rather than assumed**: `RejectRelation` deletes the relation and the question row **survives**, resolved, with `relation_id` still readable. This is the test that fails if a future migration adds a CASCADE | (PR 3 for the raw delete, PR 6 for the path) |
| **L3** | `SELECT DISTINCT resolution FROM pending_questions` after a real pass yields only `AllQuestionResolutions()` members — the constraint the schema does not carry (m3b Risk A's posture) | (PR 5) |
| **L3** | Both partial indexes are used: `EXPLAIN QUERY PLAN` on `Unasked` and `Open` names them | (PR 3) |
| **L4** | The proposal's demo: an uncertain relation reaches the digest; "yes, they're related" raises its confidence out of the band; "no" deletes it, signal first | `test/e2e/` (PR 6) |

**Two things no test is allowed to assume.** That confirming leaves the band — it is asserted
against `Decide`, not against a number. And that the question row survives a relation delete — it
is asserted by reading the row back after the delete, not by reading the migration.

**Core coverage.** `ConfirmedConfidence` is four lines with three property tests, and `Band` is a
field. The ≥90 % floor on `internal/core/` is not stressed, but `make check-all` before every PR
remains mandatory — `scripts/core-coverage.sh`, L3, L4 and the schema-golden regeneration diff
only run there (R6).

---

## 9. Threat matrix

No routing, shell, subprocess, VCS/PR automation, executable-file classification or
process-integration boundary is opened or changed. **One row is genuinely applicable** and is
answered rather than omitted:

| Boundary | Status |
|---|---|
| Routing | N/A — no port, no route, no HTTP surface |
| Shell / subprocess | N/A — no `os/exec`; the `brain-boundary` depguard rule already denies the neighbourhood |
| VCS/PR automation | N/A |
| Executable-file classification | N/A |
| Process integration | N/A — no new binary, no new subcommand |
| **Untrusted inbound text → an irreversible delete** | **Applicable, and this change makes it reachable for the first time.** Before `m3e`, `RejectRelation` had no caller; after it, an inbound Telegram message can delete a relation. Four structural defences, none of them a warning: (1) the channel refuses to start without `allowed_chat_ids` (non-negotiable #7, shipped in `m3c`), so no unknown sender reaches classify at all; (2) `classify.AllRelationOutcomes()` is a closed two-member vocabulary and `decodeEnum` degrades anything else to `nil`, so no free text reaches the store; (3) **Q5** — an answer with no open question deletes nothing, so a delete requires a question the system itself asked first; (4) exactly one relation is reachable per answer, `open[0]`. Worst case for a hostile or mistaken message: one inference the system made, which it asked about in a digest first, is deleted. **Expected failure behaviour, asserted in PR 6**: an unknown outcome string resolves nothing and deletes nothing; a `rejected` with nothing open writes one row and deletes nothing; a `rejected` with three open deletes exactly one and records `open_relation_questions: 3` |

---

## 10. Migration / rollout

**Migration `0004`, forward-only**, like every migration here. It only adds a table and two
partial indexes; it modifies no published file and no existing column. `make check-all` runs the
schema-golden regeneration diff and `schema_doc_test.go`'s anchors.

**No feature flag.** Two behaviours become user-visible when their PRs merge: a morning digest may
carry a question (PR 5), and an inbound "yes"/"no" may change or delete a relation (PR 6). Neither
can be staged behind a flag without shipping a branch nothing takes.

**Rollback**, per PR:

- PR 6 reverts to today's recorded-and-ignored row. Questions asked before the revert stay open
  and expire on schedule.
- PR 5 removes an additive branch in `assembleDigest`; the trigger-only digest is restored
  exactly. Queued questions stop being asked and are never expired — inert, not corrupt.
- PR 4 stops queueing. Existing rows are read by nothing.
- PR 2's `ConfirmedConfidence` has one caller; deleting both reverts it.
- **PR 1 does not roll back.** A revert of everything after it leaves an empty, unread table.
  That asymmetry is why the migration lands in its own PR, first.

---

## 11. Owner-review items and open questions

Numbered so `sdd-tasks` and the owner can answer by reference. **None blocks `sdd-tasks`**; each
has a decided default that ships if the owner is silent.

| # | Item | Decided default | What a different answer costs |
|---|---|---|---|
| **R1** | **Q1's ruling ships inside ADR-0027** as a `Related decision`, not as `0028` (§3.8), deviating from proposal §5's "it decides one thing" | Inside `0027` | A separate `0028` is one file and no code. It also dilutes the "architectural fork with two live alternatives" bar `0020`–`0026` were held to |
| **R2** | **`capture.go`'s relation judge creates no question** (§3.4). Uncertain relations born at capture are stored and never asked about | Consolidate's path only, per proposal §3.2 | Widening it is ~40 lines in `capture.go` plus one field, and it makes I09 true of both producers. It also puts a graph question into the queue during the capture the user just made |
| **R3** | **`ProposedRelation` gains `Band`** (§3.4), a core struct widening the proposal did not name | The field | Recomputing `Decide` in `brain` costs zero core change and buys one rule in two languages |
| **R4** | **The confirm path's signal is emitted after the raise** (§3.6), the opposite of I10 | After | Emitting first mirrors I10 visually and teaches the learning module about a raise that may not have happened |
| **R5** | **`RejectRelation` narrows to `(ctx, relationID, now)`** (§3.6), touching `checkin_test.go:221`'s call expression | Narrowed | Keeping `ports.Relation` means every caller assembles a struct with six unread fields |
| **Q1** | **The queued (unasked) pool is unbounded** (§3.5, N3). One question per digest, and consolidation can queue several a night | **Not decided.** No second bound in `m3e`; recorded so it is not improvised at apply time | A `created_at`-based queue expiry is one constant and one sweep arm — and a second §13-adjacent number for a pressure nothing has measured |
| **Q2** | Does a **re-ask** ever happen — a question expired unanswered, and the relation still uncertain a month later? | **No.** Q4 says "ask once", and nothing creates a second question for a relation | A re-ask needs a rule for how long is long enough, which is the number Q4 was written to avoid |

---

## 12. Risks this design adds or sharpens

Proposal §8's R1–R8 stand. R1 is closed by Q1's ruling and §3.3; R2 is closed by §8's I09 file;
R8's mitigation is §3.2's inner join plus §3.5's sweep. New or sharpened:

| # | Risk | Mitigation |
|---|---|---|
| **N1** | **`rel.ID` is not necessarily the stored relation's id** (§3.4). `Upsert` keeps the FIRST id on an existing triple, so a question could reference a row that does not exist | §3.1's INSERT-time `WHERE EXISTS`, which fails loudly at the one place it can occur, plus an L3 test. Not a precondition three files away |
| **N2** | **Capture's judge still queues nothing** (§3.4), so I09 is true of consolidation's relations and silent about capture's | Owner-review item R2, with its cost stated. Not inherited silently |
| **N3** | **The queued pool is unbounded** (§3.5). One question asked per digest against an unbounded producer means a question created today could be asked in months | Named, not mitigated. FIFO at least drains it in order, and M4's worklist view is its real drain. Q1 records the question rather than inventing a bound |
| **N4** | **`ConfirmRelation` is a read-modify-write** (`ByID` → compute → `Upsert`), racing the nightly connect pass inside one `nooma serve` process | Bounded: `vaultlock` serialises across processes, and the worst in-process case is one confidence revision losing to another. No deletion, no lost question. `ConfirmedConfidence`'s idempotence means a repeat answer corrects it. An atomic `UPDATE … SET confidence = MAX(confidence, ?)` was rejected: it puts doc 02 §4's arithmetic in SQL, which proposal §2's own success criterion forbids |
| **N5** | **`docs-sync` fires on PRs 2 and 4** — both touch `internal/core/**` | Both carry a genuine doc 02 §4 delta (`confirmed_floor`'s producer; the asking mechanism), so no `no-spec-change` label is needed. R7 discharged |
| **N6** | **A question is asked exactly once, so a missed digest is a missed ask.** If the send fails after `renderDigest`, `MarkAsked` is never reached and the question is re-queued — correct — but if the send succeeds and `MarkAsked` fails, the pass returns a hard error and the question is asked again tomorrow | The second case is deliberate and matches `triggers.Surface`'s own posture on the same line (`digest.go:106-110`): a duplicate ask is recoverable, a silent loss is not |
| **N7** | **`resolution` has no `CHECK`** (§3.1), so nothing in the schema stops an unvetted string | Three layers: no port method takes a `resolution` parameter (§3.2), the L2 vocabulary pin, and the L3 `SELECT DISTINCT`. m3b Risk A's accepted posture, applied to a new table for the same reason |
