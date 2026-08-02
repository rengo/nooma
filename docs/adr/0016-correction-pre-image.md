# ADR-0016 — A correction records what it overwrote, before overwriting it

- **Status**: Accepted
- **Date**: 2026-08-02
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M1 Phase C (PR 12, `feat/corrections`)

## Context

Doc 02 §5 step 4 says a `correction` **edits the referenced unit in place**. That sentence, plus
§9's `correction` signal name, is the entire governing text for corrections in this project — no
ADR has ever covered them.

Doc 02 §4 says something else, about a different step of the same pipeline:

> **A duplicate is recorded, not merged.** [...] That is deliberate. Merging is a destructive
> decision made from one model call, and §1's rule that nothing is deleted applies to content the
> user actually wrote.

Those two paragraphs disagree. A correction resolved by a model and applied as an `UPDATE` is a
destructive decision made from one model call, over content the user actually wrote. Doc 02
argues against that at §4 and mandates it at §5.

The contradiction was **not** introduced by §4's paragraph, which landed in PR 11c. §5 step 4 has
said "in place" since doc 02 was written. What PR 11c did was state the principle plainly enough
that the conflict became visible — which is the same way most of this chain's conflicts surfaced.

**The distinction neither paragraph draws.** A duplicate is inferred *whole*: the model decides
two units are the same, and decides to merge them. Nothing about it was asked for. A correction
splits in two:

| | Decided by |
|---|---|
| **What** to change — "the 15th, not the 14th" | the **user**, explicitly |
| **Which** unit to change | the **model**, by inference |

So §4's argument does not condemn the edit. The user asked for the edit. It condemns **inferring
and destroying in the same act** — and a correction only does that because the referent is
inferred while the overwrite is irreversible.

Separating the two is what this ADR decides.

Two facts bound the options. `decision_log.context` and `learning_signals.context` are free-form
JSON columns that already exist (migration 0001), so recording something costs no migration. And
`docs/03-data-model.md` defines **no version or history table** for units — there is nowhere else
a previous value could go without new schema.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| **Overwrite, and let §4's argument stand unresolved** | No work | Doc 02 keeps contradicting itself, and the contradiction sits exactly where the damage is |
| **§4 cedes: its argument covers inference, not instruction** | No code; contradiction disappears from the doc | The referent is still inferred. A wrong one destroys the user's content with no trace, and the doc stops protecting precisely where the risk is highest |
| **§5 step 4 cedes: a correction becomes a new unit plus a `corrects` edge** | Nothing is ever overwritten; reuses all of PR 11c; a wrong referent is a rejectable edge | Contradicts `m1-capture-recall/proposal.md` §2, which pre-commits the in-place `UPDATE` as a success criterion; needs `superseded`'s gloss widened; the stale unit stays in recall |
| **Record the pre-image, then overwrite** ✅ | The instruction is honoured and the inference is reversible; no migration; I12 already requires the row | The pre-image lives in the audit trail rather than in a versions table, so undo is manual until a surface exists to drive it |

## Decision

**Before applying a correction, the values it is about to overwrite are written to
`decision_log.context`, in the same row that records the correction decision.**

The row is written **first**. If it fails, the `UPDATE` does not happen — the same ordering
`capture.embedding.failed` and `capture.dedup.failed` already use, where the audit write failing
propagates rather than being swallowed.

`context` carries at minimum the target unit's id, the previous values of every field the
correction is about to change, and the confidence that selected the referent. It is JSON and
free-form; this ADR does not fix its exact keys, because PR 12 will discover them and doc 02 §5
is where the settled shape belongs.

**What this resolves, and what it does not.** It answers *is a wrong referent recoverable?* — yes,
the previous value is retrievable from the glass box. It does **not** answer which unit a
correction refers to (Q3c proper), nor which columns a correction may write. Those remain open,
and this ADR deliberately makes the second one easier: a recoverable edit can be authorised by a
lower confidence than an irreversible one, so the threshold question is no longer entangled with
the destruction question.

## Consequences

Doc 02 §5 step 4 gains the pre-condition, and §4's paragraph gains the sentence distinguishing an
inferred merge from an instructed edit. The two stop disagreeing.

PR 12 gains a requirement it did not have: it cannot write the `UPDATE` without writing the
pre-image, and that ordering is testable — an L2 test can assert that a correction whose audit
write fails leaves the unit untouched.

Undo is **not** built by this decision. The pre-image is recorded, not offered: reversing a
correction means reading the glass box, and no surface does that until M4. That is an accepted
gap, named here rather than discovered later.

The pre-image makes `decision_log` rows carrying user content larger and more sensitive. The vault
is already the user's own file and nothing here leaves it, but a future export or sync feature
inherits this: **the audit trail now contains memory content, not only references to it.**

This does not extend to the relation judge. A duplicate still records an edge and changes nothing,
per §4 — there is no pre-image because there is no overwrite.
