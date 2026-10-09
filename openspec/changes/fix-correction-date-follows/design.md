# Design — fix-correction-date-follows

## 1. Root causes

- `internal/core/classify/prompt.go:66-67` tells the model to "answer with absolute dates" and
  says nothing about `normalized_content`, so the resolved date is baked into the body
  ("dentista el 2026-10-16 a las 10").
- `internal/core/correction/plan.go` (`PlanEdit`) writes exactly one field, and doc 02 §5 step 4
  accepted the stale body as a cost.
- `internal/brain/correction.go` (`correctionRunner`) holds no `TriggerRepo`, and
  `ports.TriggerRepo` has no read by unit and no way to move an armed trigger, so nothing on the
  correction path can reach the reminder.

## 2. The text: rewrite the instant the body already states, no model involved

Options weighed:

| Option | Verdict |
|---|---|
| Stop baking dates into the body at capture | Rejected. Push and digest render only `payload.action`, so the nudge would lose its date. Existing units keep theirs anyway |
| Ask a model to re-word the body on correction | Rejected. A second provider call, an outage mode, and the inference doc 02 refuses when it writes `content` from a model reading |
| **Rewrite, in the body, the previous instant in the form capture writes it** | Chosen. Pure, deterministic, no provider. Covers the client's shape: the body writes what the system itself resolved |

`correction.FollowDate(text, previous, next, zone)` replaces each standalone `YYYY-MM-DD` token
of `previous` (not digit-, path- or query-adjacent, not in a word with a scheme) and the `HH:MM`
time anchored to it by "T", a space, " at " or " a las ", with `next` rendered the same way. A
time with no date before it, or followed by a range dash, is never rewritten. The
frame the model wrote the body in is not stored (the column holds UTC), so it tries the user's
zone first and UTC second, and rewrites in the first frame where the old instant appears.
`correction.CarryText(plan, unit, zone)` appends the content edit to a date plan. `PlanEdit` is
unchanged: it still decides the one field the user corrected; the content edit is derived.

R2 states the form in the prompt so that new captures write it. The residual, stated: a body
naming the date another way ("on the 14th", "a las 10") is not recognized and stays as it was.

## 3. The reminder: the same functions a fresh capture calls

`prospection.Follow(eventAt, live, interruptLevel, now)` is pure. It calls `datedTrigger` or the
recurring branch Arm itself uses (factored to `recurringTrigger`), so the rule is the capture
rule by construction, not by copy. It returns the plan, which armed trigger carries it (the
recurring one if any, else the earliest), and which others expire. It refuses (expires all) only
when the plan arms nothing, i.e. a one-shot date already past.

Brain reads the unit's armed triggers only for an `event` unit whose plan writes `event_at`
(`TriggerRepo.ArmedForUnit`), then moves (`TriggerRepo.Reschedule`, armed precondition), creates
(`Create`), or expires (`Expire`). Cancelling reuses `expired`: the trigger's subject is behind
it and it will not fire (I15's meaning). No migration, no new status.

A new trigger takes the correction classification's interrupt level (a fresh capture's); a moved
one keeps its stored level. A move that changes nothing writes nothing.

## 4. Order and audit

`correction.applied` (now possibly two fields) → unit edits → per reminder: its row, then its
write → the learning signal. Every row precedes its write, ADR-0016's posture. The reminder rows
are change-shaped, so `/ui/activity` renders them with no template change.

## 5. Docs and invariants

Doc 02 §5 step 4 replaces the "accepted cost" paragraph with R1/R3. I28's "arms nothing" becomes
"arms nothing of its own" — the referent's reminder follows by the new I29. That is the doc-02
exit for I28's test, which asserted the old rule. No ADR governs the one-field rule (ADR-0016
leaves "which columns" open), so no ADR is superseded or added.
