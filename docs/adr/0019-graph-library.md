# ADR-0019 — Graph rendering: Cytoscape.js over a server-bounded neighbourhood

- **Status**: Proposed
- **Date**: 2026-08-11
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M4

## Context

[ADR-0008](0008-ui-stack.md) decided that htmx does not solve an interactive graph and that a
small JS bundle would be vendored for it — **and then never chose the library.** It named the
slot and left it open. `/ui/graph` cannot be built until it is filled, and this is the only
place in the whole product where third-party JavaScript enters the binary.

The constraints ADR-0008 set on that bundle are strict: **small, free of transitive
dependencies, vendored by hand and audited** — it is foreign code shipping inside a binary that
holds one person's entire memory — embedded with `go:embed`, and functional with no network.

Two facts about the view itself narrow the choice further.

**The job is edge curation, not display.**
[`01-architecture.md`](../01-architecture.md) Layer 2 defines `/ui/graph` as *"graph of units and
relations; edge-level curation (split/confirm connections)"*. Edges are the interactive target,
not decoration between nodes. A library with rich node interaction and edges as passive lines
does not solve this view.

**A mature vault holds thousands of units.** That is a fact about the data, and the tempting
inference — that the renderer must therefore draw thousands of nodes at once — is the one this
decision rejects. Above a few thousand nodes a force-directed layout collapses into a
*hairball*: a dense clump that no renderer makes legible, because the illegibility is
topological, not a frame-rate problem. And an edge that cannot be seen cannot be curated. The
volume is real; drawing it all at once is not the way to serve it.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| **Cytoscape.js** (chosen) | MIT, **zero dependencies**, edges are first-class event targets, `neighborhood()` is built-in traversal | Largest single asset the UI ships (435 KB raw / 136 KB gzip) |
| **Sigma.js v3** | MIT, WebGL, built for very large graphs | **Requires `graphology`** — two packages to vendor and hand-audit instead of one, doubling the third-party surface in the one place it exists |
| **Hand-written canvas/SVG** | No third-party code at all; smallest possible payload | Re-implements hit-testing, panning, zoom, layout and edge picking. ADR-0008 already rejected this reasoning by choosing to vendor rather than hand-roll |
| **vis-network** | Mature, canvas-based, familiar API | Larger dependency surface than Cytoscape with no compensating advantage against these criteria |

## Decision

**Cytoscape.js is the graph library, rendering a subgraph the server has already bounded.**

The two halves are one decision and neither works alone.

**The server bounds the view.** Go computes the subgraph — the neighbourhood of one unit,
capped by a render budget — and the JS island renders exactly what it is given. The client never
queries for "the whole graph", because no endpoint offers it. This is the same shape as the rest
of the UI: the brain decides, the view renders. The budget is a calibratable constant in
`internal/core`, not a magic number in a template, so it is tuned the way every other threshold
in this project is tuned.

**Cytoscape.js renders it.** Its dependency list is empty — `dependencies: {}` and
`peerDependencies: {}`, verified against the published package, not inferred from documentation.
Under ADR-0008's audit requirement that is the criterion that decides: Sigma.js is equally MIT
and equally capable, but arrives as two artifacts to audit instead of one.

`node.neighborhood()` being first-class API is what makes the two halves fit: the interaction
model this view was designed around is a built-in traversal rather than something built on top
of a library that assumes a whole-graph render.

### The detail that was not obvious

**The published comparison literature is wrong for the current version, and the spike is what
caught it.** Sources through 2026 state that canvas-based Cytoscape.js degrades at roughly
3,000–5,000 nodes and that Sigma.js is the answer when WebGL is required. That framing made
Sigma look like the price one pays for scale, and the bounded-neighbourhood model look like a
workaround for Cytoscape's ceiling.

It is not. `cytoscape@3.34.0` ships a **WebGL2 renderer** — confirmed by inspecting the exact
artifact that would be vendored, not by reading a changelog. So the reason to tolerate Sigma's
second package evaporated: **Cytoscape offers the WebGL path without `graphology`.**

The bounded neighbourhood stays regardless, and this is the point worth keeping: it was never a
performance workaround. It is what the curation task requires. WebGL availability is headroom,
not permission to render the whole vault.

### The consequence that lands on another document

Bounding the graph to a neighbourhood means the graph stops being the way a user browses their
vault — and Layer 2 currently offers no other way. **A browsable, searchable, filterable list of
units is required by this decision and does not exist in
[`01-architecture.md`](../01-architecture.md).** It is added there in this same PR rather than
discovered during M4.

That list is server-rendered with htmx and needs **no JavaScript at all**, and it reuses search
that already exists rather than inventing any: FTS5 is already registered and synchronised in
the vault, and [ADR-0010](0010-hybrid-recall-fusion.md) already decided how results fuse. The
thousands of units are served where serving them is cheap.

### What this ADR does not decide

The render budget's **value**. This ADR fixes that a budget exists, is server-side, and is
calibratable; the number comes from measuring a real vault, and the spike below deliberately did
not invent one.

## Consequences

### What it enables

- `/ui/graph` can be built: the library is chosen, and the interaction model it must support is
  a traversal that library provides natively.
- The audit ADR-0008 requires covers **one** artifact with an empty dependency list, which is
  the difference between an audit that gets done and an audit that gets deferred.
- Graph rendering cost stops scaling with vault size. A vault ten times larger renders the same
  neighbourhood in the same time, because the budget — not the data — sets the payload.

### What it costs

- 435 KB raw / 136 KB gzip embedded in the binary. Negligible against the binary's size
  (~16 MB, [ADR-0001](0001-sqlite-driver.md)), but it is the UI's largest single asset and
  should be restated whenever it is updated.
- A vendored dependency to track by hand: no automated update, no dependabot. Updating it means
  re-auditing it.
- No whole-vault overview. A user who wants to see the entire graph at once cannot, by design.
  If that turns out to be a real need rather than an assumed one, it is a new decision.

### Reversal criteria

Evidence that neighbourhood exploration plus the list view does not cover how users actually
navigate their vault — concretely, users repeatedly widening the neighbourhood to its cap trying
to reach an overview. That would mean the browse model was wrong, and would reopen the
whole-vault view and with it the renderer question. Cytoscape.js's own WebGL2 mode is the first
thing to try at that point, not a different library.

## Spike results — 2026-08-11

Measurements taken against the exact artifact that would be vendored,
`cytoscape@3.34.0/dist/cytoscape.min.js` fetched from unpkg, plus the package metadata from the
npm registry. No browser was run; see the limits at the end.

**Decision: Cytoscape.js is accepted at 3.34.0.**

| # | Criterion | Result |
|---|---|---|
| 1 | No transitive dependencies | **PASS** — `dependencies: {}`, `peerDependencies: {}` |
| 2 | Licence compatible with AGPL-3.0 distribution | **PASS** — MIT |
| 3 | Bundle small enough to embed | **PASS** — 435,328 B raw / 136,438 B gzip |
| 4 | Cannot reach the network (ADR-0008 offline requirement) | **PASS** — see audit below |
| 5 | Edge-level interaction for split/confirm | **PASS** — edges are first-class selector targets |
| 6 | Bounded-neighbourhood traversal available | **PASS** — `node.neighborhood()` is core API |

### The self-containment audit

The offline requirement was verified by scanning the shipped bundle rather than by trusting the
project's description of itself:

| Primitive | Occurrences |
|---|---|
| `fetch(` | 0 |
| `XMLHttpRequest` | 0 |
| `WebSocket` | 0 |
| `importScripts` | 0 |
| `eval(` | 0 |

The only module markers present are `module.exports` and `define.amd` — an ordinary UMD
wrapper. **The bundle cannot phone home**, established by inspection of the artifact.

### A premise in the Options table was wrong

The comparison literature's canvas ceiling does not apply to this version, and finding that out
is the most valuable thing this spike produced. `cytoscape@3.34.0`'s shipped bundle contains a
WebGL2 renderer:

| Marker | Occurrences |
|---|---|
| `webgl` (lowercase) | 45 |
| `getContext` | 12 |
| `createShader` | 4 |
| `drawArraysInstanced` | 1 |
| `vertexAttribDivisor` | 2 |
| literal `webgl rendering enabled` | present |

`drawArraysInstanced` together with `vertexAttribDivisor` is instanced drawing — the actual
mechanism, not a stub. It is documented as a WebGL2 mode *inside* the canvas renderer, enabled
with `webgl: true`, batching through a single shader program to hold down draw calls, and
falling back to canvas automatically at extreme zoom where WebGL textures degrade.

This did not change the decision. It removed the only reason to have hesitated about it.

### The API facts the criteria did not ask for

- **Event binding is selector-scoped**: `cy.on('tap', 'edge', handler)` treats edges as an
  ordinary target rather than a special case, and `.data()` carries a relation id onto the edge
  and back out on the event. Curation does not fight the library's grain.
- **`:selected` styles edges directly** (`line-color`, arrow colours), so "this connection is
  being judged right now" is a style state, not a bespoke overlay.

### What this spike did NOT establish

- **No browser was run.** Every finding is static inspection plus documentation. That
  `webgl: true` behaves as documented **in 3.34.0 specifically** is unverified — the
  documentation consulted is from the project's unstable branch, though the option is read and
  the log string is present in the 3.34.0 artifact.
- **No frame-rate measurement at any node count.** The render budget must come from measuring a
  real vault. This ADR deliberately does not name a number.
- **The primitive scan is not the hand audit** ADR-0008 requires. It is a targeted scan for
  network and dynamic-execution primitives, and it is a precondition for that audit, not a
  substitute.
