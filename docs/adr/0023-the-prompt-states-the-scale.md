# ADR-0023 — The prompt states the scale: the model still decides, it stops deciding blind

- **Status**: Accepted
- **Date**: 2026-08-29
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3e

## Context

A maintainer sent one message to their own brain and the memory was gone in about a minute:

```
22:59:54Z  capture.classify              unit 35d6b94f…
23:00:56Z  consolidate.archive.archived  35d6b94f…  below_weight_threshold
```

The unit was stored with `weight = 0.5` and `weight_decay_rate = 0.3`, both supplied by the
classifier. §6 archives when `effective_weight < weight_threshold`, whose default is **0.5**.
Sixty-two seconds of decay is arithmetically nothing —

```
0.5 · exp(−0.3 · 62/86400) ≈ 0.49989
```

— and nothing is enough, because the comparison is strict and the starting value was the
boundary itself.

**Nothing here is a bug.** `weight.Effective` is correct. `Archive` is correct. §6's strict
inequality is implemented exactly as written. The model's answers are reasonable readings of
what §5 asked it:

```
weight        0-1, how much this matters to the user
decay_rate    per-day forgetting rate; low for emotional or identity-shaped
              content, high for routine tasks
```

For "buy coffee", `0.5` is a fair answer to *how much does this matter*, and `0.3` is literal
obedience to *high for routine tasks*. **The model was never told that the low end of one scale
is a delete, or that a rate of 0.3 empties a memory in two days.**

Three facts sharpen it:

1. **Answering was worse than not answering.** §13 sets the base prior at `weight = 1.0` for a
   classification that supplies none. A model that omitted the field produced a unit that
   lives; one that answered `0.5` produced a unit that dies. The system punished the model for
   responding.
2. **§6's boundary promise is unkeepable.** "A unit sitting at exactly `weight_threshold` is
   not archived" holds for zero seconds: effective weight moves continuously, so the next
   instant is strictly below.
3. **§2's mechanism is running on one leg.** The design is *type orients the direction, the
   self-model's beliefs personalize the value*. Nothing injects beliefs into a classification
   yet — `internal/brain/capture.go` passes `nil` — so the model was asked to personalize with
   nothing to personalize from, and then to do it blind.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| A grace period before archiving | Small, surgical, and fixes the observed symptom exactly | Does not touch the cause. At the rate the model chose, a unit born at **full** weight still archives in `ln2 / 0.3 = 2.31` days — the grace period only moves the funeral |
| Nooma overrides what the model returns | Coherent with §13: if omitting the field yields 1.0, an informed 0.5 should not yield *less* than silence | Half-ignores the model's answer without saying so. That silence is the thing doc 02 does not tolerate, and writing it down honestly turns it into the option below |
| A per-type table in Go, replacing the model's judgment | Nooma owns every behavioural number, which is §13's whole premise | **Contradicts doc 02 §2 head-on** — "the LLM assigns `weight` and λ" — and §5.1 already rejected exactly this: "inventing nine of those would be inventing calibration this document never stated, in the one place it says the model decides". A real option, but it needs an ADR superseding §2, not this one |
| **Tell the model what the numbers do** | Stays inside §2's decision — the model still assigns — and removes the actual defect, which is blindness rather than authority. No supersede, no new vocabulary, no per-vault behaviour change | The prompt now depends on a §6 value, coupling capture to consolidation. And it is a prompt fix: it improves the odds of a good number without guaranteeing one |

## Decision

**`classify.BuildPrompt` states the operational meaning of both numbers. The model keeps the
decision.**

1. `weight` is asked for alongside §6's `weight_threshold` — **named with the value the vault
   actually uses**, not a constant — and alongside what crossing it costs: archived on the next
   nightly pass, out of active memory.
2. `decay_rate` is asked for alongside its half-lives (0.01 ≈ 70 days, 0.1 ≈ 7, 0.3 ≈ 2). A
   per-day exponential rate is not calibratable by anyone, model or human, without them.
3. The threshold reaches `BuildPrompt` as a **parameter**, the way `now` already does —
   `internal/core` reads no configuration — and is resolved **once at vault open**, not per
   capture. That mirrors ADR-0012's resident index: a database round trip per capture to render
   a prompt is a cost with no reader. A vault whose threshold changes while the server runs
   keeps the old value until restart; stated, not hidden.
4. `nooma doctor`'s quality gate builds the prompt with the **calibrated default** rather than a
   vault's configured value. The gate judges whether a provider can answer this prompt's shape,
   and a per-vault number would make two vaults score the same provider differently on a
   question that is not about them.

## Consequences

### What it enables

- A model choosing `weight` knows where the delete line is, and a model choosing λ knows what a
  rate costs in days. Both were unanswerable before, and both are answerable now with the one
  signal the model does have — the type.
- The failure becomes legible if it recurs: the prompt names the boundary, so a bad number is
  now a model ignoring an explicit instruction rather than a model guessing.

### What it costs

- **Capture is coupled to consolidation.** §5's prompt now depends on a §6 number. The coupling
  is real and one-directional, and it is the honest shape: the number was *already* deciding
  what §5 produced, invisibly. Naming it does not create the dependency, it discloses one.
- **A prompt fix improves odds, it does not guarantee outcomes.** A model can still answer 0.4
  having been told 0.5 deletes. What changes is that this would now be disobedience rather than
  ignorance — a different, and much cheaper, thing to diagnose.
- One more argument on a `BuildPrompt` signature, and a sixteenth on `NewCaptureService`. That
  constructor is past the size where positional arguments read well, and this ADR notes it
  without fixing it.

### Reversal criteria

Live captures still landing in the archive band with the scale stated — the model told where the
line is and choosing below it anyway. That is disobedience, not blindness, and it points at the
per-type table: the option this decision deliberately did not take, which would need its own ADR
superseding §2. Symmetrically, if beliefs ever do reach the classification and the model starts
personalizing with real context, the scale paragraph may be redundant rather than wrong — worth
re-reading then, not deleting now.
