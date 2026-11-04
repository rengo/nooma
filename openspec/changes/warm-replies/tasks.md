# Tasks — warm-replies

Stacked to main (design §7). Strict TDD: every RED test below is written and watched failing for
the stated reason before its link's GREEN code. Zone -03:00, now Wed 2026-11-04 10:00 unless
stated. A link measured over 400 impl+docs is cut before it opens. `make check-all` before every
push.

## Review Workload Forecast

| Link | Impl+docs | Tests | Risk |
|---|---|---|---|
| 1 | ~350 (312 pure deletion) | −218 | Low: no caller |
| 2 | ~260 | ~450 | Medium: the renderer every later link uses |
| 3 | ~150 (mostly deletion in `rephrase.go`) | ~550 | Medium: every push and the timer ring, I15/I27 pins |
| 4 | ~170 | ~400 | Medium: digest bound, Today |
| 5 | ~190 | ~700 | Medium: widest pin churn |

Chained PRs: yes (5 links + plan). Decision needed before apply: none; Q1-Q3 are ruled (design §8).

## Link 0 — `plan/warm-replies`

- [x] 0.1 proposal.md, spec.md (PM-owned drafts), design.md, tasks.md; ADR-0064 and
      its README row.
- [x] 0.2 ADR-0064 `Accepted`; client rulings Q1-Q3 (2026-11-04) folded into the ADR, spec, design
      and these tasks.

## Link 1 — `feat/replies-retire-voice` (D5)

- [ ] 1.1 Delete `internal/core/voice` (code and tests). `rg core/voice` finds nothing.
- [ ] 1.2 Doc 02 §5 step 2: replace the ADR-0061 paragraph with ADR-0064's rule; §13: delete the
      four `reply_*` rows. ADR-0061 and ADR-0032 `Superseded by` lines; README row 0061 →
      Superseded.
- [ ] 1.3 `scripts/docs-sync.sh` satisfied (core and doc 02 change together); `make check-all`.

## Link 2 — `feat/replies-days-and-recall` (D1-D4, D6)

- [ ] 2.1 RED L1 `TestDay_RelativeAndFull`: offsets -1, 0, 1, 2 → `ayer`/`hoy`/`mañana`/`pasado
      mañana` (EN too); 3 → `el sábado 7 de noviembre`; another year → `… de 2027`; an instant at
      23:30 -03:00 (02:30 UTC next day) is `hoy`.
- [ ] 2.2 RED L1 `TestClock`: 8:30 → `a las 8:30`; 1:00 → `a la 1:00`; 13:00 → `a las 13:00`;
      0:20 → `a las 0:20`; EN `at 8:30`.
- [ ] 2.3 RED L1 `TestWhen`: ES adverbial = day + clock; EN `on Thursday 26 November at 8:30`,
      `tomorrow at 10:00` (no `on`); day alone has no clock; due → `para mañana`, past due →
      `era para ayer`, EN `due tomorrow`, `was due yesterday`.
- [ ] 2.4 RED L1 `TestInline`: `Turno con el oculista` → `turno con el oculista`; `DNI vence` kept;
      `Pedro cumple` with Names `Regalo para Pedro` kept; trailing period dropped; EN unchanged.
- [ ] 2.5 RED L1 `TestRecall`: one ahead, one past, one due, one undated, several, none — design
      §6 recall rows, both languages, exact text.
- [ ] 2.6 Doc 06 §4: I59 row. RED L2 `TestI59_RecallAnswerIsBuiltByCode`: the §6 recall rows
      through `channels.RenderReply` from a `CaptureResult`; and
      `TestI59_RecallCallsNoWordingTask` (recording fake provider: only `classify`,
      `embedding`).
- [ ] 2.7 GREEN: `phrase` fields and functions (D1); `reply.go` builds `Item`s
      (`recalledLine` → `recallItem`); delete `FoundOne`/`FoundMany`/`List`. Update I22, I26,
      `channel_reply_totality` and integration pins to the new text.
- [ ] 2.8 Doc 02 §5 step 2 recall paragraph; doc 07 example; `make check-all`.

## Link 3 — `feat/replies-pushes`

- [ ] 3.1 RED L1 `TestPush`: design §6 push rows, both languages; a unit with no instant →
      `Acordate: llamar a mamá.`; late note appended.
- [ ] 3.2 RED L2 `TestI59_PushIsBuiltByCode`: a check pass with a fake clock pushes the §6 texts
      to a fake channel; `TestI59_CheckPassCallsNoWordingTask` (no timers due).
- [ ] 3.3 GREEN: `render.go` `unitWords` → `pushItem` (text, Moment from `Occasion`, Names);
      `pushText` uses `set.Push`; `Late`/`TimerLate` wording; delete `Reminder`/`Remind`. Update
      I15/I27/I39-I44 pins, `days-pass` transcripts.
- [ ] 3.4 RED L1 `TestRenderTimer`: a timer with words rings `Es la hora: sacar la ropa.` / `Time's
      up: Take the clothes out.`; late adds the note; no words → the nudge; the provider fake
      fails the test on any call. RED L2 `TestI59_TimerRingIsBuiltByCode` (a check pass that rings
      a timer calls no wording task).
- [ ] 3.5 GREEN: `renderTimer` builds the sentence (`Set.TimerRing`), drops `rephrasePrompt`, the
      `timer_rephrase` task and its tests; `rendered_text` written `NULL`; keep the
      `check.timer.rephrase_failed` constant for old rows. Update the timer pins.
- [ ] 3.6 Doc 02 §7 reminder frame and `phrase` field list; §8 timer ring, §5 language paragraph
      and order sentence; doc 07 line ~421; `make check-all`.

## Link 4 — `feat/replies-digest`

- [ ] 4.1 RED L1 `TestDigestLineOf`: event, day alone, due today, due past; bullets keep the
      item's capital.
- [ ] 4.2 RED L2 `TestI59_DigestIsBuiltByCode`: design §6 digest (ES and EN) byte for byte; one
      line; question only; over the bound → `Y 2 más que te cuento mañana.`; the bound still
      drops re-asks first (existing test unchanged in property).
- [ ] 4.3 GREEN: `saidNow` and `dueLines` render through `DigestLineOf`; `DigestHeader` with
      greeting; `AndMore`, `OneMore`, `RelationQuestion`; Today's preview follows (its pins
      updated).
- [ ] 4.4 Doc 02 §7 digest and tasks-due examples, §4 question wording; `make check-all`.

## Link 5 — `feat/replies-confirmations`

- [ ] 5.1 RED L1 confirmations: design §6 confirmation rows, both languages, through
      `phrase` (`NotedFor`, `NotedOn`, `NotedForDigest`, `Noted`, `NotedNotSet`, `NotScheduled`,
      refusal reasons, `Every`, `TimerAt`, `Moved`, `Corrected`, `Unchanged`); every lead phrase
      with a clock uses `Clock`.
- [ ] 5.2 RED L2 `TestI59_ConfirmationIsBuiltByCode` and `TestI59_CorrectionIsBuiltByCode`
      through `RenderReply`; the timers' list lines use `Clock` (`1) 10:20 — sacar la ropa`,
      structure unchanged, ADR-0051).
- [ ] 5.3 GREEN: `reply.go` passes `result.At` as now and builds `Moment`s; update the wide pin
      set (`Anotado`, `Te aviso`, `Movido al`, `Noted`, `I will remind`) in conformance,
      integration and `days-pass`.
- [ ] 5.4 Doc 02 §5 steps 4-5, §7 (capture reply of a task due), §8 (timer), recurring examples;
      doc 07 lines ~64, 106, 214, 265; `make check-all`.

## After the chain

- [ ] PM closes #435 unmerged (design D5); remove `../nooma-voice` when the PM says so.
- [ ] Optional, maintainer: re-record a few `testdata/llm/reminders/` follow-up cases with the new
      turn wording (measured, not required).
