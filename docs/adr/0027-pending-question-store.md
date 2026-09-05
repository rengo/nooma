# ADR-0027 — Where a question the brain asked lives while it waits for an answer

- **Status**: Accepted
- **Date**: 2026-09-05
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3e

## Context

An Uncertain-band relation (doc 02 §4, `[min_confidence_to_persist, min_confidence_to_surface)`)
is stored and, per doc 02 §4's own wording, is supposed to be **asked about** in the digest: "I
linked X with Y, are they related?" Nothing in the tree has ever asked that question — the band
has been storable since M1 and unaskable ever since (`m3e-pending-question/proposal.md` §1).

Building the ask requires somewhere for the question to live between the moment consolidation
decides to ask and the moment an inbound answer resolves it. Two tables in this schema already
hold state that looks adjacent — `triggers` and `decision_log` — and both were considered before a
third, dedicated table.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| **Extend `triggers`** | `triggers.resolution` is a closed `engaged\|declined\|self_healed` vocabulary — recording a relation confirmation as `engaged` produces an audit row that misdescribes what happened, which doc 02 §11 forbids by name. `triggers.unit_id` is one FK; a relation has **two** endpoints, and a relation question would arrive at `LiveFocusCandidates` with no honest single unit to rank |
| **Derive from `decision_log`** | Cheapest — no migration, and precedented (`heldCounts`, `lastDigestAt` are already derived from it). But `decision_log` is the glass box, an append-only audit trail, not a state store: it has no natural place to hold "is this still open," and it gives M4 no queryable worklist to render a pending-questions view from |
| **A dedicated `pending_questions` table** ✅ | One migration, one new port. "Which relation is this question about" and "has it been answered" are both a column rather than an inference — the property neither of the other two options gives without repurposing a table for a second use it was not designed for |

## Decision

**A question the brain asked lives in a dedicated `pending_questions` store while it waits for an
answer — not `triggers`, and not derived from `decision_log`.**

The table is `(id, kind, relation_id, created_at, asked_at, resolved_at, resolution)`. `kind`
holds one member today (`relation`) and exists from the first row so a second question kind costs
a row later, not a migration. Three states are each a column rather than an inference: `asked_at`
and `resolved_at` both NULL is queued; `asked_at` set and `resolved_at` NULL is open; both set is
closed, with `resolution` one of `confirmed | rejected | expired`.

**`relation_id` carries no foreign key.** Every FK behaviour available loses something the table
exists to keep:

| FK behaviour | Why it was rejected |
|---|---|
| `ON DELETE CASCADE` | Rejecting a relation deletes it (I10). A CASCADE would delete the question row that records the rejection **with the thing it recorded** — deleting the audit of a deletion, the exact silent data loss non-negotiable #6 forbids |
| `ON DELETE RESTRICT` | I10 emits `relation_reject` **before** the delete. A RESTRICT would fail that delete and strand an emitted signal for a rejection that did not happen |
| `ON DELETE SET NULL` | Requires `relation_id` nullable and keeps the row, but destroys the one column the table exists for — "which relation was this about" becomes unanswerable at exactly the moment it matters |

The referential check moves to INSERT time instead, where it costs nothing and cannot cascade:
`PendingQuestionRepo.Create` inserts with `... WHERE EXISTS (SELECT 1 FROM relations WHERE id =
?)` and returns `ErrRelationNotFound` on zero rows affected. `decision_log` already carries unit
and trigger ids with no FK, for the identical reason — an audit row must outlive its subject.

**No `Delete`-prefixed method.** The port joins every other repository interface in
`test/conformance/i03_units_never_deleted_test.go`'s reflection sweep, with no carve-out — a
pending question is a state machine, never a removal.

### Related decision — where `confirmed_floor` comes from

Doc 02 §4 says confirming raises confidence via `GREATEST(current, confirmed_floor)`, and until
this change `confirmed_floor` existed in exactly one line of prose with no producer.
**`confirmed_floor` IS the relation type's own resolved `min_confidence_to_surface`**
(`relation.Thresholds.Surface`) — no new constant, no new column, no new §13 calibration row. A
confirmed relation lands at or above the edge of the band it was in, and `relation.Decide`'s
boundaries are inclusive toward the higher band, so it leaves the band by construction. This
travels inside this ADR as a related decision rather than a separate `0028`, because it is not an
architectural fork with two live alternatives — it is doc 02 §4 naming a number the tree already
had, and doc 02 §4's own amendment is what carries it forward.

## Consequences

### What it enables

- The Uncertain band's asking half becomes buildable: the digest reads `pending_questions` as a
  second item source, disambiguation reads it to resolve an inbound answer, and the confirm/reject
  paths close it as a state transition.
- A queryable worklist for M4's eventual "pending questions" view, with no schema change needed to
  support it.
- `kind` costs a second question producer one row's worth of vocabulary, not a migration.

### What it costs

- A migration — the first one since M2 (`0004_pending_questions.sql`), forward-only like every
  migration in this tree.
- No `CHECK` constraint on `kind` or `resolution` — no table in this schema carries one, so the
  vocabulary is pinned by an `AllX()` function, an L2 test against the migration's own column
  comments, and an L3 `SELECT DISTINCT` after a real pass, the same three-layer posture `m3b`
  already took for its own two vocabularies.
- A relation deleted by a rejection leaves its question row pointing at an id nothing satisfies
  anymore. Both reads (`Unasked`, `Open`) inner-join `relations`, so such a row is skipped rather
  than surfaced or failed on — it is not orphaned, the digest's own expiry sweep eventually closes
  it as `expired`.

### Reversal criteria

A second question kind whose natural home is genuinely not this table — one with no relation to
name, or with a resolution vocabulary this table's three-member set cannot express — would be
evidence to supersede this ADR with one describing a differently-shaped store, rather than
stretching `kind` to cover a shape it was never designed for.
