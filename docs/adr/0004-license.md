# ADR-0004 — Project license

- **Status**: Accepted
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Amended**: 2026-07-28 (see the end of this document)
- **Enables**: Public release (M6). Does not block code.

## Context

Nooma ships open source, self-hosted, with a multi-tenant mode contemplated for the future.
The license decides what a third party can do with the code — in particular, whether they can
build a closed SaaS on top without giving anything back.

It is the only decision in this set that is business rather than technical, and the most
expensive to reverse: once external contributions exist under one license, relicensing requires
every contributor's consent.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| AGPL-3.0 | The individual user notices no difference from MIT; anyone running a service on top must open their changes | Many companies ban AGPL in internal policy, even for internal use; reduces corporate adoption and contributions from there |
| MIT / Apache-2.0 | Maximum adoption, zero friction | Zero protection: anyone can close a fork and sell it |
| Dual (AGPL + commercial) | Protects and leaves room to monetize | Requires a CLA from contributors, and a CLA scares off part of the community |

## Decision

**AGPL-3.0**, with the full text in `LICENSE` from the first public commit.

The reasoning: Nooma's target user runs the binary on their own machine. For them, AGPL and MIT
are indistinguishable — they never distribute anything, so no clause reaches them. The only
actor AGPL bites is exactly the one we want to deter: whoever takes the brain, hosts it closed,
and sells it.

The dual option is discarded for now: it requires a CLA, and a CLA on day one of a project with
no community is pure friction with no benefit.

## Consequences

### What it enables

- A third party hosting Nooma as a service must publish their modifications.
- It leaves the door open for the owner to offer their own hosting: they hold the copyright and
  do not restrict themselves.

### What it costs

- Adoption and contributions are lost from companies with an internal AGPL ban. That is a real
  and non-marginal cost in developer tooling.
- If relicensing is ever wanted, every contributor's consent is required. That is why this
  decision is made **before** the first public commit, not after.

### Reversal criteria

Evidence that AGPL is blocking real (not hypothetical) adoption in the target segment, before
significant external contributions exist. After that, reversal is practically impossible.

## Immediate operational consequence

`LICENSE` with the full AGPL-3.0 text goes into the **first public commit**, not later. While
the owner is the sole copyright holder they can relicense at will; the moment the first
external contribution is accepted, that door closes without every contributor's consent.

---

## Amendment — 2026-07-28

The decision stands: AGPL-3.0. This amendment corrects one factual error and records the
reasoning that was actually load-bearing.

### Correction: the reversal asymmetry

The Reversal criteria section above says reversal is "practically impossible" without
qualifying the direction. That is only true one way:

- **AGPL → MIT** requires every contributor's consent. Correct as written.
- **MIT → AGPL** is possible. MIT explicitly grants the right to *sublicense*, so contributions
  received under MIT can ship as part of a combined AGPL work going forward. Caveat: everything
  already released under MIT stays MIT forever and can be forked from the last permissive
  commit. You close the future, not the past.

Starting with AGPL is therefore the genuinely one-way choice. It is made knowing that.

### The reason that was missing

The original text argues that AGPL and MIT are indistinguishable for the individual user. True,
but not the load-bearing reason. The actual reason, confirmed by the owner on 2026-07-28:

**The multi-tenant hosted service is a real business plan, not the architectural note it reads
as in [`../00-vision.md`](../00-vision.md).** That is what makes the AGPL protection worth its
adoption cost.

### MIT evaluated and rejected (2026-07-28)

MIT was reconsidered against [Gentleman-Programming/engram](https://github.com/Gentleman-Programming/engram),
an MIT project by the same owner with Nooma's technical shape (single Go binary, SQLite + FTS5,
zero dependencies).

The analogy does not transfer. engram is a **developer tool** — MCP server, CLI, agent plugin —
installed on work machines inside companies, whose value *is* ubiquity; there a reflexive
corporate AGPL ban is fatal. Nooma is an **end-user product**: nobody imports a personal digital
brain as a dependency, so the corporate-ban cost barely applies. Same author and same technical
shape do not imply the same license — the deciding dimension is who installs it and why.

Recorded so the comparison is not re-litigated from scratch.
