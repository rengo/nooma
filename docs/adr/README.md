# Architecture Decision Records

Each ADR records **one** architectural decision with its context, the options evaluated, and
the consequences accepted. It does not document the system's current state: it documents the
moment the choice was made, and with what information.

## Immutability rule

**An ADR in `Accepted` status is never edited.** If the decision changes, a new ADR is written
that supersedes it, and the old one gets `Superseded by: ADR-NNNN`. The value of an ADR is
being able to read what was known at that moment; editing it destroys exactly that.

The only fields that may be touched on an accepted ADR are `Status` and `Superseded by` in the
header.

### Amendments

An accepted ADR may receive an `## Amendment` section: dated, additive, appended at the end.
It exists for exactly two cases:

- Correcting a **factual error** in the original text.
- Recording **information that arrived later and does not change the decision**.

An amendment never rewrites the original text — the original stays readable as written, wrong
parts included, with the correction below it. If the decision itself changes, supersede
instead.

Without this mechanism the immutability rule has no way to fix a factual error, which forces
either leaving the error in place or inflating a supersede. Both are worse.

## Statuses

| Status | Meaning |
|---|---|
| `Proposed` | Written and argued, awaiting validation (a spike, a business decision) |
| `Accepted` | In force. The code must comply |
| `Rejected` | Evaluated and discarded. Kept so it is not re-litigated |
| `Superseded` | Another ADR replaces it |

## Numbering

ADRs 0001–0010 correspond 1:1 with decisions D1–D10 in
[`../04-decisions.md`](../04-decisions.md). Later ones continue from 0011 in chronological
order, with no correspondence to anything.

## Index

| ADR | Decision | Status | Blocks |
|---|---|---|---|
| [0001](0001-sqlite-driver.md) | SQLite driver and cross-compilation | Proposed | M0 |
| [0002](0002-default-llm-preset.md) | Default LLM preset | Accepted | M1 |
| [0003](0003-embeddings.md) | Embedding generation | Accepted | M1 |
| [0004](0004-license.md) | Project license (AGPL-3.0) | Accepted (amended 2026-07-28) | Public release |
| [0005](0005-v1-scope.md) | v1 scope | Accepted | M1 |
| [0006](0006-v1-channel-telegram.md) | v1 channel: Telegram | Accepted | M3 |
| [0007](0007-http-auth.md) | HTTP API and UI auth | Accepted | M4 |
| [0008](0008-ui-stack.md) | Concrete UI stack | Accepted | M4 |
| [0009](0009-scheduler-downtime.md) | Scheduler semantics under downtime | Accepted | M2 |
| [0010](0010-hybrid-recall-fusion.md) | Hybrid recall fusion | Accepted | M1 |
| [0011](0011-contributor-licensing.md) | Contributor licensing: no CLA, with a deadline | Accepted | Public release |

## Template

```markdown
# ADR-NNNN — One-line title

- **Status**: Proposed | Accepted | Rejected | Superseded
- **Date**: YYYY-MM-DD
- **Supersedes**: — | ADR-NNNN
- **Superseded by**: — | ADR-NNNN
- **Enables**: milestone(s) from `05-build-plan.md`

## Context

What problem forces the decision and what constraints existed at the time.

## Options evaluated

A table or list with the real tradeoff of each.

## Decision

What is chosen, in the imperative. No conditionals, no "probably".

## Consequences

### What it enables
### What it costs
### Reversal criteria

What concrete evidence would force writing the ADR that supersedes this one.
```
