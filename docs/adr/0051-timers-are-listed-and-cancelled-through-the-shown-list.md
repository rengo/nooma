# ADR-0051 — Timers are listed and cancelled from chat through the list the code shows

- **Status**: Proposed
- **Date**: 2026-10-26
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M4 (`m4f`, the chat half)

## Context

Live, on a copy of the client's vault (main at 4fdd004), with two timers set:

```
← ¿qué temporizadores tengo?
→ Encontré 3 cosas: • Tengo que llamar a Juan … • Cita con el dentista …
← cancelá el del horno
→ Necesito una cosa más antes de cambiarlo: ¿a cuál te referías?
```

Doc 02 §8 promises that a timer is "listable and cancellable from chat", and nothing did either.
The model read the question as a `recall`, which searches memory, and a timer is not memory
(I04). The cancel was read as a `correction`, which binds a unit, and a timer is not a unit.
`ports.TimerRepo` had no list read, and its `Cancel` only served the due scan.

A chat follow-up already binds what it is about through a list the code built and showed
(ADR-0036, ADR-0044), and the model reads what a message means into typed fields that the code
acts on (ADR-0033). Timers need the same two things, and there is no unit for them to hang off.

## Decision

1. **Every classify prompt shows the live timers.** That is every `pending` timer
   (`TimerRepo.Pending`), ordered by `fire_at` then id. The first `max_reminder_choices` are
   numbered, with the instant in the prompt's own date form and the timer's text. When none is
   set, the prompt says so, so the model is not left to notice that the list is missing. The
   prompt also states that timers are kept apart from memory, so a recall never finds one.
2. **Two typed fields carry the reading.** `timer_request` is `none | list | cancel`.
   `timer_target` is, for a cancel, the listed position the user means (named by what the timer
   says or when it rings) or `unclear`. Both are optional on the wire, and an unknown value
   degrades to absent (I14).
3. **A request about the timers is answered first, whatever the reading's type.** This happens
   before any follow-up, check-in or recall fork, and never on a correction that names its unit.
   It stores nothing.
4. **A list** answers with the shown timers, numbered as the prompt numbered them. Each is named
   by its hour on the day it was set, and by its day and hour otherwise (`phrase.TimerLine`). The
   reply counts every live timer and says how many are past the shown ones. When none is set,
   the reply says so.
5. **A cancel binds only a position the prompt showed.**
   - That timer moves `pending → cancelled`. This is a state transition, never a delete
     (non-negotiable 6), and it writes one `capture.timer.cancelled` row naming the timer, its
     instant and its position.
   - Anything else (`unclear`, no target, a position past the list) asks "¿Cuál cancelo?" with
     the same numbered list and cancels nothing. The answer is read with that question in the
     chat (ADR-0044) and the live list shown again.
   - A timer that rang or was cancelled between the list and the cancel is answered as no
     longer set, and nothing is logged as cancelled.
   - With no live timer, the reply says none is set.
6. **The fixed sentences take no side in voseo or tuteo** ("Hay 2 temporizadores puestos:",
   "¿Cuál cancelo?"), because they are not the model's words.

## Alternatives considered

| Option | Why not |
|---|---|
| Read the question as a `recall` and add timers to its answer | A recall's scope filters units (ADR-0048), and a timer is not one. It would mix infrastructure into memory's answer, and a cancel would still have nothing to bind |
| A new `pending_questions` kind for "which timer" | It needs a migration and a store write for a question the chat already carries. The list is shown again on every prompt, so the answer binds without stored state |
| Let the model name the timer by its text, and match it in Go | That is a word list in Go (ADR-0033). The position the code showed is the only thing the code binds |
| Show the timers only beside a chat | A cancel from the API or the CLI would have nothing to bind. The list is short and bounded |

## Consequences

- Every prompt changed, so every phrasing in `testdata/llm/reminders/` is recorded again. The
  case format gains `timers_list`, and `expect` gains `timer_request` and `timer_target`, both
  strict and both counted in the reminder reading rate.
- A new outcome, `timers`, carries what was listed or cancelled. Every renderer handles it: the
  channel, the API (`timers.listed`, `timers.cancelled`) and the UI capture form.
- Listing and cancelling from the UI is still open, and so is `m4f`'s answer to a pending
  question from the UI.
- **Known limit, accepted:** with more than `max_reminder_choices` live timers, the ones past the
  list are counted in the reply but cannot be cancelled until earlier ones ring or are
  cancelled. A cancel that names one of them asks which, with the shown list.
