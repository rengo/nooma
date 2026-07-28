# ADR-0002 — Default LLM preset

- **Status**: Accepted
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M1

## Context

The original design proposed a "local embedded" default: llama.cpp with a 3B model inside the
binary, to deliver "zero dependencies, zero cloud". Three objections refute it:

1. **llama.cpp requires cgo** — it directly contradicts the cross-compilation promise
   ([ADR-0001](0001-sqlite-driver.md)).
2. **A 3B model does structured JSON badly.** `classify` and the relation judge are exactly the
   tasks that cannot fail: if `classify` returns broken JSON, the capture is lost or corrupted.
   Putting the weakest model at the most critical point is backwards.
3. **It bloats the binary by ~2 GB.** "Download the binary" stops being a reasonable sentence.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| Cloud by default (the wizard asks for an API key) | Guaranteed quality on the critical tasks; zero local setup | "Zero cloud" stops being the default; requires an account and a card |
| Ollama by default | Genuinely local, decent models, no cgo (it speaks HTTP) | External dependency: "install Ollama first" |
| Embedded llama.cpp | Zero dependencies | Refuted above |

## Decision

**The `nooma init` wizard offers two first-class paths: Cloud (recommended) and Ollama. The
embedded option is discarded.**

Additionally, `nooma doctor` gains a **structured-JSON quality gate**: it sends a fixed set of
`classify` and judge prompts to the provider configured for each task, and verifies the
returned JSON validates against the expected schema. On failure, `doctor` reports the provider
as unsuitable **for that specific task**, not in general — a model can be excellent at chat and
bad at JSON, and the user has to see that distinction.

Honesty replaces the false promise: *"a powerful local model requires hardware"* instead of
*"local is trivial and good"*.

## Consequences

### What it enables

- The lowest-friction path (Cloud) has the best quality on the tasks that cannot fail. The user
  who wants total privacy picks Ollama knowing what they are paying.
- The `doctor` gate turns "pick the right model" into something verifiable instead of folklore.
- The gate's prompt corpus is the same one that feeds the test golden files: written once, used
  in two places (see [`../06-harness.md`](../06-harness.md) §5).

### What it costs

- Nooma stops being "zero cloud by default". That is a real positioning cost, accepted
  consciously: the product's differentiator is the dynamic model, not where the LLM runs. The
  vision doc already says it — "an LLM is a replaceable part of the system, not the system".
- The wizard gets longer: a tradeoff has to be explained in the first minute of use.

### Reversal criteria

A small local model (≤ 4B) appearing with reliable structured JSON and no cgo — for example
via constrained decoding exposed by Ollama. That would put the embedded option back on the
table.
