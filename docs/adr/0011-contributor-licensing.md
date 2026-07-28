# ADR-0011 — Contributor licensing: no CLA, with a deadline

- **Status**: Accepted
- **Date**: 2026-07-28
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: Public release (M6). Does not block code.

## Context

[ADR-0004](0004-license.md) settles the project license: AGPL-3.0, on the basis that the owner
intends to offer Nooma as a hosted multi-tenant service.

That intent raises a separate question the license decision does not answer: **what happens to
copyright on third-party contributions**, and whether a Contributor License Agreement is needed.

The two cases behave differently and are easy to conflate:

- **The owner hosting Nooma under AGPL needs no CLA.** They comply with the license by
  publishing the source of the deployed version — which they are doing anyway. Third-party AGPL
  contributions running inside that service are fine.
- **Selling a closed commercial license to a third party (dual licensing) does require a CLA.**
  You can only license proprietarily the code you own; a contributor's code stays AGPL-only
  without an assignment or a grant.

Dual licensing is how most AGPL projects monetize beyond hosting. It is not on the table today,
but the door to it closes at a specific moment.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| No CLA | Zero friction for the first contributors, which is exactly when a project has none to spare | Closes the dual-licensing door as soon as the first external PR merges |
| CLA from day one | Keeps every monetization path open | Real friction on a project with no community yet; a CLA on day one reads as distrust |
| Decide later | — | Not an option: "later" means going back to every past contributor |

## Decision

**No CLA is adopted.** Contributions arrive under AGPL-3.0, the project's license, with no
additional agreement.

**The deadline is recorded explicitly**: if dual licensing ever becomes plausible, the CLA must
be in place **before the first external contribution is merged** — the same deadline as the
license itself. After that, adopting one means obtaining consent from every past contributor
individually.

The reasoning: hosting — the actual plan — works fine without a CLA. Dual licensing is
hypothetical. Paying certain friction now for a hypothetical path, on a project that does not
yet have its first contributor, is a bad trade.

## Consequences

### What it enables

- Contributing to Nooma requires opening a PR and nothing else. No signature, no form, no legal
  reading before a first commit.
- The hosted service, which is the real plan, is unaffected.

### What it costs

- The moment the first external PR merges, closed dual licensing stops being available without
  going contributor by contributor.
- This is a decision made deliberately in the absence of information: there is no community yet,
  so there is no way to measure whether a CLA would actually have deterred anyone.

### Reversal criteria

A concrete commercial-licensing opportunity appearing **before** the first external
contribution. That is the only window in which this reverses cheaply. Afterwards, reversal means
a per-contributor consent campaign, and the new ADR must say so.
