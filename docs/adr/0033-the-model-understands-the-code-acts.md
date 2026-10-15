# ADR-0033 — Reminders: the model understands, the code acts

- **Status**: Accepted (by the client, 2026-10-14, with option A of point 3)
- **Date**: 2026-10-14
- **Supersedes**: [ADR-0031](0031-reminders-the-user-asks-for.md) **in part**: only how a
  request, a stop, a done or an answer is *detected*. That is its point 1's "closed list per
  language", point 4's "made of date and time words, or a no", and point 5's "read from phrase
  lists". Everything ADR-0031 decides about what Nooma *does* stands.
- **Superseded by**: —
- **On acceptance**: ADR-0031's header names ADR-0033 in `Superseded by`, for detection only. The
  index allows that field and nothing else, as with ADR-0027.

## Context

ADR-0031 had Nooma recognise "recordame…", "no me lo recuerdes más", "listo" and the answers to
its own questions from closed phrase lists, so that the model's reading was never trusted for
what the user wrote literally. Three review rounds of the plan kept finding phrasings the lists
misread, and every fix grew them:

- "si no, avisame" and "no te olvides de recordarme" were read as negations;
- "recordame qué hablamos" was read as a request when it is a question;
- "my dentist sent a reminder to confirm" was read as a request;
- "listo, nos vemos mañana" was read as a done;
- reported speech had no answer at all.

The client ruled on 2026-10-14: "la IA entiende y el código actúa" ("no quiero caer en un gran
MAPING de textos… el LLM debería evaluar sobre la marcha").

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Keep the phrase lists and grow them | Deterministic, but each language and each turn of phrase is another entry, and the misreads above show the list never ends |
| **The model reads intent; the code binds and acts** ✅ | One classify call already reads every message. Its reading becomes structured fields the code checks. Misreads become possible, so binding, confirmation and degradation must bound them |
| The model reads intent *and* names the reminder (an id) | Fewer code paths, but a wrong id stops the wrong reminder with nothing to catch it |

## Decision

1. **The model understands.** The existing classify call (no second call) returns three new
   optional fields, each degrading independently (I14):
   - `remind_request` (bool): the user asks Nooma to remind them of what this message stores.
     Its time, recurrence and day-only-ness are the fields that already exist: `event_at` or
     `due_at`, `recurrence_rule`, and the decoder's day-only flag, which is read from the shape
     of the value, not from a model claim.
   - `reminder_intent` ∈ {`none`, `stop`, `done`, `keep`, `delete`}: what a chat message says
     about a reminder.
   - `question_answer` ∈ {`none`, `when`, `no`, `choice`}, with `choice_number` for `choice`:
     how the message answers the reminder question Nooma has open. The prompt shows that
     question, and its numbered options, only when one is open.
   - `reminder_target`, a position (1-5) or `unclear`: which reminder a stop or done is about.
     The prompt shows a numbered list of the candidates the code built (title or content, and
     date) only when there are any.

   **No phrase list in core.** The prompt explains the fields with examples in both languages.
2. **The code acts.** The model never names a unit, a trigger or a question.
   - The code builds the real candidates: the open question, asked reminders armed or delivered
     recently, armed recurring reminders, and the unit of the last delivery, asked or not. Units
     are deduplicated, so a tie inside one unit is one candidate, and there are at most 5.
   - A `choice_number` or `reminder_target` is a position in a list the code built. The code maps
     it to the unit it stored, and binds only on a valid position.
   - `unclear`, an out-of-range position, or a missing one means asking which. There is no word
     matching and no blind single-candidate bind.
3. **Durable actions are confirmed by the user's own explicit words, with an undo.** These are
   stopping for good and deleting.
   - An explicit stop ("no me lo recuerdes más") counts as the confirmation: Nooma stops at once
     and says which reminder it stopped.
   - The stop reply opens a pending undo for `stop_undo_window_minutes` (10). Only a `keep`
     answering that undo ("no, seguí") restores exactly the rows the stop retired, by id. A
     reminder that came due meanwhile is dropped and named in the reply. The same undo covers
     what an answer to Nooma's question armed.
   - A bare "listo" never stops anything by itself: it asks first (ADR-0031 point 5), and that
     answer is the confirmation turn for a delete.
   - Nothing in the vault is deleted in either case: a stop is a tombstone.
4. **A degraded reminder field means no reminder action.** The message takes the plain capture
   path, and the decision log says the reading was degraded. A **failed** classify call saves
   nothing, as for every capture (I31). Until m4f gives stopping a UI route, a stop cannot be
   made during a model outage: a known limit.
5. **Testing measures quality instead of asserting it.**
   - No test calls a real model (non-negotiable 5).
   - Existing recordings are not re-recorded, because conformance tests depend on their content.
     A new reminder phrasing set records about 44 phrasings, each with its expected fields, in
     one session against the final prompt. It includes every phrasing the reviewers raised, with
     the question or candidate list open when there is one. CI replays the set.
   - `nooma doctor --reading` (opt-in, about 44 calls) reports "reminder reading: k of n" and a
     separate hard line: stop or delete false positives on the negatives must be 0. A prompt
     change is judged by those numbers.

## Consequences

- No phrase list to maintain. A new phrasing or a new language is the model's job, and the
  doctor line says how well it does it.
- **Misreads become possible.** A wrong request costs a reminder the user can stop. A wrong
  stop is bounded three ways: it needs an unambiguous candidate, it is undoable for 10 minutes,
  and it deletes nothing.
- **Detection depends on prompt quality.** The prompt grows by the field descriptions and, when
  one is open, the reminder question. Any prompt change re-runs the doctor line and may need the
  recordings re-recorded.
- **Quoted speech** ("Ana me dijo 'avisame…'"), a known limit under ADR-0031, becomes a case the
  phrasing set measures rather than a pinned misread.

### Reversal criteria

A doctor reading rate below 90% on the phrasing set for the configured model, or wrong stops
reported in use, point back to a deterministic first pass in front of the model, narrower than
the list this ADR retires.
