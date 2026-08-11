# ADR-0018 — CSS: hand-written, embedded, no framework

- **Status**: Proposed
- **Date**: 2026-08-11
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M4

## Context

[ADR-0008](0008-ui-stack.md) fixed the UI stack — `templ` + htmx + a small vendored JS bundle,
all embedded with `go:embed` — and named CSS exactly once, in passing: *"JS/CSS assets are
embedded with `go:embed`."* **How that CSS is authored was never decided.**

That gap is now the blocking one. `internal/ui/` holds a single `doc.go`; not one line of
markup or style exists yet, so M4 starts by choosing a styling approach, and whichever one it
starts with is the one the seven views in
[`01-architecture.md`](../01-architecture.md) Layer 2 get written against.

The constraints that already apply are unusually strong for a front end:

- **No network at render time** (ADR-0008). No CDN, no web font fetched at load, no external
  stylesheet. The binary must render its complete UI on an air-gapped machine.
- **`go build` must work on a clean machine** with no extra toolchain installed. ADR-0008 chose
  to commit the generated `_templ.go` files specifically to keep that promise.
- **Third-party code entering the binary is audited by hand.** ADR-0008 imposes that on the
  graph bundle and picks the library small and free of transitive dependencies for that reason.
- **The stated visual direction is "dense, sober self-hosted-tool aesthetics"**
  ([`01-architecture.md`](../01-architecture.md) Layer 2) — the only direction written down
  anywhere.

The surface is also small and stays small: seven views, one owner per vault, no marketing
pages. `templ` components already scope markup, which removes most of the naming pressure a
utility framework exists to relieve.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| **Hand-written CSS, one embedded file** (chosen) | Zero toolchain, zero codegen, zero third-party code. Modern CSS — cascade layers, nesting, custom properties, `light-dark()` — covers what frameworks were invented to work around | No enforced consistency: the token layer has to be respected by discipline rather than by a compiler |
| **Tailwind (standalone CLI)** | Consistency by constraint; no class naming; well-understood by contributors | Asks for ADR-0008's entire codegen apparatus a second time: another binary in the dev environment, another generation step, another generated file to commit, another CI job to prove it is current — to solve a naming problem seven views do not have |
| **Classless base** (Pico, Water, Simple) | Semantic HTML with no classes at all; very little to write | Blog-shaped by design — generous whitespace, narrow reading measure, large type. Directly opposed to "dense and sober", so the work becomes overriding the base. Also third-party CSS in the binary, carrying ADR-0008's audit burden with none of the graph bundle's justification |
| **Per-component CSS inside `templ`** | Styles live next to the markup that uses them | Duplicated declarations across components, no cascade to rely on, and style shipped inline per render instead of cached once |

## Decision

**The UI's styling is hand-written CSS in a single file, embedded with `go:embed`. No CSS
framework, no CSS build step, no third-party stylesheet.**

The file is organised with explicit cascade layers, declared once at the top so precedence is a
property of the file's structure rather than of selector specificity:

```css
@layer reset, tokens, base, layout, components, utilities;
```

`tokens` carries the design system as custom properties — spacing scale, type scale, colour
roles — and is the only layer allowed to define a raw value. Every other layer references
tokens. Light and dark are expressed with `light-dark()` against those roles, so theming is one
declaration per role rather than a second stylesheet. Typography uses the **system font stack**:
no font file is downloaded at render time (forbidden) and none is embedded either (it would add
weight to the binary to look less native on every OS than the platform's own UI font).

`utilities` is a short list of helpers that earn their place, not a philosophy. If it starts
growing toward a utility framework, that is the evidence named under reversal criteria below,
not an invitation to keep going.

### The detail that was not obvious

**The argument against Tailwind here is not about Tailwind. It is that ADR-0008 already paid
this toll once.** Committing `_templ.go`, marking it `linguist-generated`, and running
`templ generate` in CI to fail on a dirty tree is a real, ongoing cost that ADR-0008 accepted
deliberately — because compile-time-checked templates were worth it and because the
clone-and-build promise had to survive. A CSS build step asks for the same apparatus a second
time, and the thing it buys — consistency by constraint across a large team, and relief from
naming things — is a problem this repository does not currently have.

**The second non-obvious point is that the exit is asymmetric.** Adding Tailwind on top of
hand-written CSS later is cheap: the token layer maps onto a theme config, and the two coexist
during migration. Removing Tailwind from a UI already written in utility classes means
rewriting every template. Choosing the reversible option first is not caution here — it is the
only ordering that keeps both options open.

**What this ADR does not decide.** It fixes how CSS is authored and delivered, nothing else. It
does not choose a colour palette, does not set an accessibility conformance target, and does not
select the graph JS library — that last one is still ADR-0008's open item and remains M4's to
resolve. Splitting the stylesheet into several embedded files if it grows is an implementation
detail, not a supersede: the decision is *hand-written and embedded*, not *exactly one file
forever*.

## Consequences

### What it enables

- A contributor clones the repository and builds the complete UI with the Go toolchain and
  `templ` alone. No Node, no second binary, no `npm install` — the ADR-0008 promise extends to
  the front end instead of being quietly qualified by it.
- The constraint and the intended aesthetic point the same way. Hand-written CSS with system
  fonts and inline SVG icons *produces* density and sobriety; there is no framework default to
  fight in order to reach the stated direction.
- Theming, including dark mode, costs one declaration per colour role rather than a parallel
  stylesheet or a class-toggling scheme.

### What it costs

- Consistency is a discipline, not a guarantee. Nothing stops a component from writing a raw
  `12px` instead of reaching for a token; only review catches it.
- Contributors who know Tailwind and not CSS cascade, layers, or custom properties face a real
  ramp. The layer declaration at the top of the file is the mitigation: it makes the intended
  structure readable before any rule is.
- No dead-code elimination. An unused rule stays in the embedded file until someone removes it
  by hand.

### Reversal criteria

The `utilities` layer growing into a de facto utility framework, or the stylesheet reaching a
size where a change to one view visibly risks another. Either is evidence that the naming and
consistency pressure a framework relieves has actually arrived — at which point Tailwind is
added on top of the existing token layer, which is the migration this decision deliberately kept
cheap. Contributor confusion alone is not that evidence: it argues for documenting the layers,
not for a build step.
