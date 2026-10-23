# ADR-0050 — The graph is drawn on the server: one neighbourhood, as SVG, no JavaScript

- **Status**: Proposed
- **Date**: 2026-10-23
- **Supersedes**: [ADR-0019](0019-graph-library.md) (which was never accepted)
- **Superseded by**: —
- **Enables**: M4 (`m4d`, `/ui/graph`)

## Context

[ADR-0019](0019-graph-library.md) proposed vendoring Cytoscape.js (435 KB, the UI's only
third-party JavaScript) to draw a neighbourhood the server had already bounded. It stayed
`Proposed`, waiting on a hand audit and a measurement, and `m4d` waited with it.

On 2026-10-23 the client rejected it. What `/ui/graph` has to do is small: show one unit, what it
is related to, and let the user confirm or delete a relation ([doc 01](../01-architecture.md)
Layer 2). Every other view does that kind of thing as server-rendered HTML with htmx
([ADR-0008](0008-ui-stack.md)) and no JavaScript of its own.

## Options evaluated

| Option | Tradeoff |
|---|---|
| Cytoscape.js island (ADR-0019) | Drag, zoom and a force layout; a vendored bundle to audit by hand and update by hand, and a surface [doc 06](../06-harness.md) could not test without a browser |
| **SVG rendered by Go** | No drag, no zoom; nothing to vendor, and the drawing is a pure function of the vault, golden-tested like any other output |
| A hand-written JS renderer | The cost of an island with none of a library's features |

## Decision

1. **Go lays out and renders the graph as SVG.** `/ui/graph?unit=<id>` draws one live unit at
   the centre and its related live units on a ring around it, with every relation among those
   units. No JavaScript is added.
2. **The view is one bounded neighbourhood, never the whole vault.** The centre's neighbours are
   ranked by their strongest relation to it (strength, then confidence, then id) and at most
   `graph_max_neighbours` (12, doc 02 §13) are drawn; the rest are counted and the count is shown.
   This is ADR-0019's render budget, now with a value: twelve labels fit on a ring at phone width.
3. **The layout is deterministic and lives in `internal/core/graph`.** Neighbours go clockwise
   from the top in rank order, at equal angles, on whole-number coordinates. No time, no
   randomness, no force pass: the same vault draws the same picture on every platform. The
   viewBox and ring radius place pixels, not decisions, so they are named constants in that
   package and not §13 rows.
4. **Navigation is a link.** Every node is a plain `<a>` to `/ui/graph?unit=<its id>`, reachable
   by keyboard, which redraws the graph around it. Every edge is a link or an htmx action like the
   rest of the UI. There is no drag and no zoom.
5. **Curating an edge reuses the existing paths.** Confirming a relation from the graph raises
   its confidence and emits `relation_confirm`, deleting one emits `relation_reject` before the
   delete (I10): the same code the chat answer to a relation question runs (doc 02 §4), not a
   second implementation.
6. **Colours come from the stylesheet's tokens** ([ADR-0018](0018-css-approach.md)), so light and
   dark both work, and the SVG scales to its container's width.

## Consequences

- Nothing third-party enters the binary for the graph, and ADR-0019's hand audit is not needed.
- [doc 06](../06-harness.md)'s "one surface with no automated test" is gone: the SVG is an L1
  golden, and the neighbourhood and layout are L1 tests in `internal/core/graph`.
- A unit is seen with its direct relations only. Reaching a unit two steps away takes a click,
  and a whole-vault overview does not exist, as under ADR-0019.
- If the client misses drag or zoom, a JS renderer can be added later over the same
  server-side neighbourhood. That would be a new ADR.
