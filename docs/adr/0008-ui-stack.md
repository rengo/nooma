# ADR-0008 — Concrete UI stack

- **Status**: Accepted
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M4

## Context

The binary serves the user's complete frontend, not an admin panel: today/focus, capture,
graph, beliefs, activity, tracking, admin. And it has to work **offline**: no CDN, no fetching
from third parties. A personal brain that needs the internet to render its own interface is a
contradiction.

The base was already fixed in the design: SSR served by the binary + htmx, no heavy build
tooling. Three points were open: which template engine, what to do about the graph, and how
codegen is handled in the build.

## Options evaluated

**Templates:**

| Option | Pro | Con |
|---|---|---|
| `templ` | Compile-time typing, good DX, generates embeddable Go | External dependency + a codegen step in the build and in CI |
| `html/template` (stdlib) | Zero dependencies, zero codegen | Template errors at runtime, not at compile time; fragile refactors |

**Graph:** htmx does not solve an interactive graph. JS is required: hand-written canvas/SVG, or
a small library vendored as a static asset.

## Decision

**`templ` + htmx + a single small JS bundle, vendored and embedded with `go:embed`, for the
graph and (in v2) charts. No CDN.**

On codegen, which is the point that usually ruins this stack: **the generated `_templ.go` files
are committed to the repo.** `go build` must work on a clean machine without installing
`templ`. CI verifies they are current — it runs `templ generate` and fails if the working tree
comes out dirty.

The alternative (generate in CI and do not commit) is cleaner in the repo but breaks the "clone
and build" promise. For a project distributed as a self-hosted binary that expects third-party
contributions, that promise is worth more than a clean diff.

JS/CSS assets are embedded with `go:embed`. The binary remains a single file.

## Consequences

### What it enables

- A template error is caught at compile time, not when the user opens `/ui/beliefs`.
- The binary renders its complete UI with no network. It can run on an air-gapped machine.
- htmx covers all interactivity except the graph: the surface of custom JS stays minimal and
  auditable.

### What it costs

- One codegen step and one more tool in the development environment.
- Noisy diffs: committed `_templ.go` files clutter PRs. Mitigated by marking them
  `linguist-generated` in `.gitattributes` so GitHub collapses them.
- The vendored bundle must be updated by hand and audited: it is third-party code entering the
  binary. The graph library is chosen small and free of transitive dependencies precisely for
  this reason.

### Reversal criteria

`templ` being abandoned upstream, or the codegen step turning into a recurring source of
friction. The exit is `html/template`: the templates get rewritten, but the architecture
(SSR + htmx + embed) does not change. That containment is why this risk is acceptable.
