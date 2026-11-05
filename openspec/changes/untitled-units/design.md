# Design — untitled-units

> **nooma-dev-owned.** Base: `origin/main` at dd6b6a8e (2026-11-05), warm-replies merged and
> archived. The client's rulings of 2026-11-05 (ADR-0065 accepted; Q1, Q2) are folded in; proposal.md
> and spec.md are PM-owned drafts from them. Doc 02 governs; every doc 02 change below lands in
> the same PR as its code.

## 1. The defect

A warm reply says an item's words and then the day the code renders (ADR-0064). The words are
`shownText(u)`: the title, else the content. The content carries the resolved instant as
`YYYY-MM-DD` / `HH:MM` (the capture prompt asks for that form, `internal/core/classify/prompt.go:131-134`, so
`correction.FollowDate` can move it), and an older unit may still carry a relative word
("mañana"). So a title-less dated unit says its date twice, once in ISO form, and can say a day
that is no longer true (ADR-0032):

```
Acordate: tengo reunión con Juan el 2026-11-04 a las 22:30, hoy a las 22:30.
```

A dated unit is title-less when:

| Source | Today |
|---|---|
| It predates migration 0009 | Never named; the migration says "no backfill" |
| The capture model gave no title | Degraded field (I14), left NULL |
| `ResolveTitle` dropped it (too long, ISO date, `HH:MM`, invisible rune) | Degraded field, left NULL |
| A correction moved the date or rewrote the body with no fresh title | **Cleared on purpose** (doc 02 §5, "the title follows the body") |

The fourth row matters: every date-only correction where the model omits a fresh title produces
one of these units today, so this is not only legacy data.

Surfaces that put an instant beside `shownText`: the push (`render.go` `pushItem`, #440), the
recall answer (`channels/reply.go` `recallItem`), the digest due lines (`digest.go` `dueLines`)
and Today (reuses `saidNow`).

## 2. Decisions

### D1 — One naming step, three call sites

A new model task, `unit_title`, asks for the title alone: shown the unit's content and language,
it answers `{"title": "..."}` under the **same rule text** the capture prompt uses for `title`
(extracted from `BuildPrompt` into one function both prompts call, so the rule cannot drift).
The answer goes through `classify.ResolveTitle`, unchanged. This is the model naming the unit, as
at capture, not wording a reply: ADR-0064 point 1 is not touched (a title is a stored field the
reply later reads; no reply waits for this call).

Pieces:

- `internal/core/classify`: `TitleRule() string`, `BuildTitlePrompt(u unit.Unit) string`,
  `DecodeTitle(raw []byte) (*string, Reason)` (strict JSON, `ResolveTitle`, a dropped title is
  `ReasonBadFormat` as in `assignTitle`).
- `internal/ports.UnitRepo`: `NameIfUntitled(ctx, id, title string, at time.Time) (bool, error)`,
  an `UPDATE … WHERE id = ? AND title IS NULL`. It never overwrites a title a correction or the
  user wrote in the meantime; `false` means someone else named it first. `UntitledDated(ctx,
  limit int) ([]unit.Unit, error)` returns non-archived units with `title IS NULL` and an
  `event_at` or `due_at`. Both get a `repocontract` case (memrepo and SQLite).
- `internal/brain`: `nameUnit(ctx, u, source) (named bool, err error)`: call, decode, write,
  log, then `retextReminders` so an armed trigger's `action_text` stops carrying the content.
  It routes through the provider configured for `capture_processing`.

It runs at three points:

1. **Capture, on the degraded path only**: a unit with its own `event_at` or `due_at` (ADR-0065
   point 1; an undated reminder is not named here) that came
   back with no usable title is named once, after the unit is written and **before** its
   triggers are armed, so `action_text` is the title from the start. One extra call, only when
   the title is missing; on any failure the capture proceeds untouched (the night retries).
2. **Correction, where it clears the title**: right after a plan that clears the title of a unit
   that is still dated, the same step names it against the new body. The clearing rule in doc 02
   stays (a title that names a gone day must go); a fresh name replaces the gap.
3. **The night**: a new consolidation phase, `title`, placed between `pattern_eval` and `learn`
   (D3); `learn` stays last (doc 02 §6, I11).

Not "on first need" at push time: ADR-0064 point 1 says no reply waits for a provider call, and
the push runs on the check tick. Naming where the gap appears (1, 2) plus the night (3) leaves
only the provider-failure residue, which D2 covers.

### D2 — The fallback names no words and no date twice

When an item has an instant and no title, the reply does not use the content. `phrase` gets one
constructor, `ItemOf(title, content string, when *Moment, dated bool) Item`, used by every caller in §1 (it
also builds `Names`, replacing `NamesOf`). With a title it is as today; with
no title and an instant it returns an item marked `Unnamed`; with no title and no instant it
keeps the content (an undated unit states no instant, so nothing repeats).

"Dated" is a property of the unit, not of the reply: `dated` is true only when the unit itself
carries an `event_at` or a `due_at` (the case where its normalized content embeds a date). The
`when` a reply computes is not enough. Callers pass `u.EventAt != nil || u.DueAt != nil`.

The common case is therefore dated: a recurring reminder normally carries an `event_at`, its first
occurrence and the anchor of its rule (doc 02 §545-548, §1895). A title-less "Tomar las pastillas"
with an `event_at` is said "Acordate: tenés algo anotado hoy a las 21:00." until slice 3's night
(or slice 2's retry) names it; this is the main case the fallback serves. The rare legacy shape,
a recurring reminder with neither `event_at` nor `due_at`, is pushed with a day the pass computes
but its words are all it has, so it keeps them ("Acordate: tomar las pastillas, hoy.").

An unnamed item is said with these frames (new `Set` fields, pinned by the completeness test and
by exact texts in I59):

| Surface | Spanish | English |
|---|---|---|
| Push (`PushUnnamed`) | `Acordate: tenés algo anotado hoy a las 22:30.` | `Don't forget: you have something noted today at 22:30.` |
| Recall, one, ahead (`RecallOneUnnamed`) | `Tenés algo anotado el jueves 26 de noviembre a las 8:30.` | `You have something noted on Thursday 26 November at 8:30.` |
| Recall, one, past (`RecallOnePastUnnamed`) | `Tenías algo anotado ayer a las 9:00.` | `You had something noted yesterday at 9:00.` |
| List line: recall many, digest, Today (`LineUnnamed`) | `• Algo anotado, hoy a las 22:30` | `• Something noted, today at 22:30` |

The day and time come from the same `When`/`said` the titled frames use, so due days read
`para mañana` / `due tomorrow` as D3 of warm-replies says. The late note still appends.

Ruled (client, 2026-11-05): the plain sentence alone, no content quoted, texts exactly as in the
table. Trade-off accepted: the user learns that something is due and when, not what. The rejected alternatives were
the sentence followed by the content quoted (repeats the date, can quote a stale "mañana") and
cutting the content at its first date token (a free-text parser of the kind ADR-0064's context
rejects); D1 makes the case rare.

Out of scope, recorded: a closed task's confirmation (`task_close.go`) and a correction's answer
say `shownText` with no instant beside it, so they do not repeat a date; they can still state a
content's stale relative word. They are not touched by slice 1.

### D3 — The night's `title` phase

- Core (`internal/core/consolidation`): `SelectToName(cands []Untitled, now time.Time, limit
  int) []string`, pure. Past instants are named too (ruled, Q2). Order: units with a pending trigger, soonest fire first; then future
  instants, nearest first; then past instants, most recent first. Ties by id. Cut at `limit`.
- Calibration: `title_backfill_per_pass` = 50 (doc 02 §13). It bounds the night's provider cost;
  a production vault with hundreds of legacy units drains over a few nights, the units that will
  ring first named first. A unit the model cannot name is retried each night and keeps its slot;
  that costs one call per such unit per night, accepted over storing an attempt counter (a new
  column, a migration) for a case not yet seen.
- Slot: the phase list becomes `expire_incomplete → archive → strengthen → connect → derive →
  reweight → pattern_eval → title → learn`. `learn` stays last: it reads what the others wrote,
  and I11 (doc 02 §6) with its compile-time proof in `internal/core/consolidation/phase.go` hold.
  `PhaseTitle` is declared before `PhaseLearn`; the count goes from 8 to 9.
- Brain: reads `UntitledDated`, runs the selection, `nameUnit` each, persists through the
  phase's `record`. With no provider configured the phase writes nothing (as `connect` does).
  A provider error on one unit is logged and the pass moves on; it does not abort the night.

### D4 — Decision log

A title written by Nooma is a state change and is logged, with the same `previous` / `next` shape
a correction's title edit uses, so `/ui/activity` shows it and the user can correct it:

| Action | When | Detail |
|---|---|---|
| `consolidate.title.named` | the night named a unit | `unit_id`, `previous: null`, `next` |
| `consolidate.title.unnamed` | the night asked and got nothing usable, or the provider failed | `unit_id`, `reason` |
| `capture.title.named` / `correction.title.named` | capture or correction named a unit | `unit_id`, `previous: null`, `next` |

A `false` from `NameIfUntitled` writes nothing and logs nothing. Nothing is deleted: the column
goes from NULL to a value; content, instants and triggers' instants are untouched. The four join
`AllDecisionActions`; `source` is implied by the prefix.

### D5 — ADR

**ADR-0065, Accepted by the client on 2026-11-05**, "A dated unit is named", extending ADR-0064, ADR-0032 and ADR-0033 and
superseding none. It records what no Accepted ADR says: the night makes an unattended model call
that writes a field of the user's units (a new kind of nightly write, beside relations and
beliefs); a dated unit is not said by its content beside its instant; and the fallback frames.
It also states that migration 0009's "no backfill" comment is overtaken (the file is published
and stays as it is; doc 03's column comment is updated).

## 3. Doc changes

- Doc 02 "read by its content" lines (§5 step 1 at `:677`, the recall answer at `:855`, the
  reminder at `:1989`, the digest at `:2085`): **slice 1 (#446)** changes their fallback half,
  "a dated unit with no title is said as something noted, an undated one by its content". Slice 1's
  PR is amended for it. Slice 2 adds only the retry (a dropped title on a dated unit is named once).
- Doc 02 §5 corrections ("the title follows the body"): a cleared title on a dated unit is
  named afresh.
- Doc 02 §5 recall, §8 push, the digest and Today paragraphs: the unnamed frames.
- Doc 02 §6: the pipeline line and the phase item for `title` (before `learn`); §13:
  `title_backfill_per_pass`. Doc 06 §4: the I11 row ("The 9 consolidation phases") and I60 (doc 02
  has no I11 row).
- Doc 03 `units.title` comment; doc 07: the status row and the "Runs eight phases" item
  (`docs/07-functional.md:156-157`), now nine; the type comment at
  `internal/core/consolidation/phase.go:8` ("eight nightly phases").
- ADR-0065 adds a ninth phase to the eight ADR-0005's v1 scope names. That is a scope statement,
  not a decision: ADR-0005 is not superseded, and slice 3's docs say so in one line.

## 4. Test plan (no network, no real LLM)

| Level | What | Where |
|---|---|---|
| L1 | `ItemOf` (titled, unnamed, undated content), every unnamed frame by exact text in both languages, ahead/past/due, late note | `internal/core/phrase` |
| L1 | `TitleRule` is the text `BuildPrompt` carries; `BuildTitlePrompt` holds content and language; `DecodeTitle` keeps/drops as `ResolveTitle` | `internal/core/classify` |
| L1 | `SelectToName` order and cut, ties, empty input | `internal/core/consolidation` |
| L2 | I59 extended: an untitled dated unit's push, recall and digest line never contain its content nor an ISO date | `test/conformance/i59_…` |
| L2 | I60: capture with a dropped title is named before arming (`action_text` is the title); a provider failure leaves NULL and still captures; a date-only correction with no fresh title ends titled; the night names up to the cap in order, logs `previous`/`next`, never touches a titled unit, content or instants; a concurrent title is not overwritten | `test/conformance/i60_…`, scripted fake provider per task |
| L3 | `NameIfUntitled`, `UntitledDated` repo contract on SQLite | `test/support/repocontract` |

Every L2 test is written first and watched red. A mutation probe per gate: drop the `title IS
NULL` guard, reverse the selection order, route an unnamed item back to its content; each must
turn a named test red.

## 5. Slices (each under 400 implementation+doc lines)

| # | Branch | Content | Est. impl+doc / tests | Depends on |
|---|---|---|---|---|
| 1 | `feat/untitled-fallback` | D2: `ItemOf`, unnamed frames, the four callers, I59 extension, doc 02 frame lines | ~170 / ~180 | main |
| 2 | `feat/untitled-naming` | D1 + D4 for capture and correction, doc 02 §5 retry and clearing | ~300 / ~250 | 1 |
| 3 | `feat/untitled-night` | D3 + its D4 rows, repo `UntitledDated`, `PhaseTitle` and the 9-phase proof, §6/§13, doc 03, I60 night cases | ~260 / ~220 | 2 |

**Release gate.** Slices merge to main in order, but v0.7.0 is tagged only after slices 1-3 have all
merged (PM decision 2026-11-05). Slice 1 alone stops the replies repeating dates, but at a cost
larger than vaguer items: a recurring reminder normally carries an `event_at` (D2), so its push
reads "tenés algo anotado hoy" until slice 3's night names it. No release tag between slice 1 and
slice 3.

## 6. Rulings (client, 2026-11-05)

- **Q1** — The fallback for a dated unit with no title is the plain sentence only, no content
  quoted (D2).
- **Q2** — The night names past dated units too, future first, under the cap (D3).

No open questions remain.
