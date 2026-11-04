# Design — warm-replies

> **nooma-dev-owned.** Base: `origin/main` at 1658c854 (2026-11-04). Governs with
> [ADR-0064](../../../docs/adr/0064-replies-are-built-by-code.md) (Accepted). The client's
> rulings of 2026-11-04 on Q1-Q3 are folded in; no open question remains. Doc 02 governs
> behavior; every doc 02 change below lands in the same PR as its code.

## 1. What is on main today

| Piece | Where | State |
|---|---|---|
| Every sentence, per language | `internal/core/phrase` (`Set`, `For`, 703 lines) | Live. Fixed frames: `Reminder: %s`, `%s — %s`, `Found %d things:`, `Noted for %s. …` |
| Day and time | `phrase.Set.Date` / `Time` | Abbreviated, absolute: `jue 26 nov, 08:30`. No relative day, no `now` |
| Lead phrases | `phrase.Set.Lead/Leads/DayLeads/NamedLeads/ExactLeads` | Live. Clocks as `%02d:%02d` (`ese día a las 09:00`) |
| Which sentence for a capture | `internal/channels/reply.go` `RenderReply` | Live. Routing switch per outcome; `recalledLine` joins text and when |
| Push text | `internal/brain/render.go` `unitWords`, `pushText`; `deliver.go` `frozenText`, `withDelayCaveat` | Live. `set.Remind(text, when)` |
| Fired timer | `internal/brain/rephrase.go` `renderTimer`, task `timer_rephrase` | Live. A model rewords the timer's text; the user's words on failure; `timers.rendered_text` stores it |
| Digest text | `internal/brain/digest.go` `dueLines`, `saidNow`, `digestMessageGoal`, `composeDigest`, `questionLine` | Live. Lines are pre-joined strings stored into `Payload.ActionText`; Today reuses `saidNow` |
| Model wording | `internal/core/voice` (#433, 312 impl + 218 test lines) | **No production caller** (verified: nothing imports it). Doc 02 §5 step 2 describes it as arriving; §13 carries four rows for it; no I56 row exists in doc 06 |
| Slots + check | #435, `feat/voice-facts`, ADR-0063 | Closed unmerged (2026-11-04), not on main. ADR-0063 and the I56 harness row exist only on that branch |

Findings worth carrying:

- `phrase`'s package comment already says it holds every sentence Nooma says to a person, and
  ADR-0022 puts the sentences in core so they are testable at L1. That is the right home; this
  change rewrites its frames and adds a renderer, it does not add a new package.
- #435's `voice.when` (`slots.go`) is the day/time rendering worth keeping: relative labels for
  -1..2 days, `el %s %d de %s`, a year suffix, `a las` / `a la` for one o'clock, `%d:%02d`. Its
  case rule (`caseFor`, `named`) is the other piece worth keeping, minus the user's message as a
  source (a push and the digest have no message).
- The classification prompt shows the model Nooma's recent turns (`Conversation.Add` in
  `deliver.go`, `digest.go`) as they were said. Nothing parses Nooma's own wording back, so a
  wording change breaks no binding; the follow-up still binds by listed letters (ADR-0036).
- `testdata/llm/reminders/` recordings carry old reply wording only inside fixture turns. Their
  `prompt_sha256` is built from the fixture, so **no recording becomes invalid**. None records a
  `voice` wording (there is no `reply` task on main). Nothing in `testdata/llm` is obsolete.

## 2. Decisions

### D1 — `phrase` stays the one home; frames take typed facts

New, in `internal/core/phrase` (pure: `now` is a parameter, as nooma-core requires):

```go
// Moment is an instant as a reply names it.
type Moment struct {
	At      time.Time // in the user's zone
	DayOnly bool      // a day alone: no time is said
	Due     bool      // a due day (a task's due_at), said as "para …" / "due …"
}

// Item is one thing a reply names: its title, else its content, and its
// instant when it has one. Names is the words of the unit's title and of
// its content, each text without its own first word, read only to tell a
// name from an ordinary first word (D4).
type Item struct {
	Text  string
	When  *Moment
	Names string
}

func (s Set) Day(m Moment, now time.Time) string  // nominal: "mañana", "el jueves 26 de noviembre" / "tomorrow", "Thursday 26 November"
func (s Set) When(m Moment, now time.Time) string // adverbial, with the time: ES = Day + " a las 8:30"; EN adds "on " before a date
func (s Set) Clock(t time.Time) string            // "a las 8:30", "a la 1:00" / "at 8:30"
func (s Set) Inline(it Item) string               // the item's words inside a sentence (D4)

func (s Set) Recall(items []Item, now time.Time) string
func (s Set) Push(it Item, now time.Time) string
func (s Set) DigestLineOf(it Item, now time.Time) string
```

New `Set` fields (each pinned by the existing completeness test, which fails on a zero field):
`DayNames` (singular weekday names; `WeekdayNames` stays plural for "todos los lunes"),
`Yesterday`, `TodayWord`, `Tomorrow`, `DayAfter`, `OnDate`, `YearSuffix`, `At`, `AtOne`,
`DatePrep` (EN `on `, ES empty), and the frames listed in §6. Old fields that no surface uses
after the last link (`FoundOne`, `FoundMany`, `Reminder`, `AtWhen` for chat) are deleted in the
link that stops using them; `Date`/`Time` stay for `/ui` and the timers' list.

Why not a new package: two packages holding sentences is how a surface ends up in the wrong one,
and `phrase` already has the language table, the completeness test and every caller.

Why not keep strings pre-joined (`unitWords` returning `text, when string`): a frame cannot pick
a tense, a "para" or a lower-case first letter from a string that already says "— jue 26 nov".
Callers build an `Item` from the unit, and `phrase` does the rest. `saidNow` keeps writing the
rendered line into `Payload.ActionText`, so the digest's message bound and Today keep working on
strings.

### D2 — Days and times (ADR-0064 point 2)

- Calendar offset from now, in now's zone (`dayOffset`, from #435): -1 `ayer`/`yesterday`, 0
  `hoy`/`today`, 1 `mañana`/`tomorrow`, 2 `pasado mañana`/`the day after tomorrow`.
- Else `el jueves 26 de noviembre` / `Thursday 26 November`; another year appends `de 2027` /
  ` 2027`.
- Time `%d:%02d`, 24-hour, no leading zero (the client's "8:30"); ES `a las 8:30`, `a la 1:00`
  for hour 1 only; EN `at 8:30`. `0:30` is said `a las 0:30` (rare; quiet hours keep most rings
  out of it).
- Every clock a chat reply says goes through `Clock`, lead phrases included (`ese día a las
  9:00`, `hoy a las 9:00`) and the timers' list lines (`1) 10:20 — sacar la ropa`), so one
  message never mixes `09:00` and `9:00`.

### D3 — Tense and due days (ADR-0064 point 4)

A recalled item is past when its timed instant is before now, or its day alone before now's day.
An instant read from `due_at` is a due day (`Moment.Due`): ES `para mañana`, `era para ayer`; EN
`due tomorrow`, `was due yesterday`. From `event_at`, it is said adverbially. The field the
instant came from decides, never the unit's type: a recurring reminder is stored as an `event`
whose title is a verb ("Tomar las pastillas"), so type says nothing about grammar.

### D4 — No gender guessed (ADR-0064 point 3, the client's accepted cost)

No frame puts an article or a gendered word right before an item's words. ES frames are built so
both a noun phrase and an infinitive read right: "Tenés anotado turno con el oculista…", "Tenés
anotado tomar las pastillas…", "Acordate: cena con Ana, …". `Inline` lowers the first letter in
Spanish unless the first word has a second upper-case letter (an acronym: "DNI") or appears
capitalised in `Names`. The caller builds `Names` from the title and the content separately,
each without its own first word (like #435's `slots.go:233-246`), so the ordinary case, where
the title and the content open with the same capitalised word ("Turno con el oculista" / "Turno
con el oculista el jueves"), is lowered, and a name later in either text ("Pedro" in "Regalo
para Pedro el jueves") keeps its capital. A capitalised word that opens a second sentence is
read as a name; that error is accepted.
English keeps the words as stored and quotes them mid-sentence (“Renew the passport”), since
English titles are stored capitalised and lowering one ("buy coffee") is no gain. A bullet line
starts with the item's words as stored in both languages. A trailing period of the item's words
is dropped before the frame continues.

### D4b — A ringing timer is a fixed sentence (ADR-0064 point 7)

`renderTimer` stops calling a model. A timer with words rings `Es la hora: <words>.` / `Time's up:
<words>.` (`Set.TimerRing`, the words through `Inline`, a trailing period dropped first), plus
`TimerLate` when `prospection.DelayCaveat` says so (its wording changes, its meaning "later than
you asked for" stays: "Perdón, te llega más tarde de lo que pediste." / "Sorry, this is later
than you asked for."; the push `Late` note keeps "previsto"/"intended"); a timer with none keeps `TimerNudge` (and the
late note). The `timer_rephrase` task, `rephrasePrompt`, the `llm` dependency of this path and
`withCaveat`'s "successful rephrasing already acknowledged" branch go. `timers.rendered_text`
stays in the schema (published migrations are never modified) and is written `NULL`; the
`check.timer.rephrase_failed` action constant stays defined so existing `decision_log` rows still
label in `/ui/activity`. Doc 02 §8's "the LLM rephrases the text" becomes the fixed sentence;
`rendered_text` is described as no longer written. ADR-0022's table is a measurement in an Accepted text and
is not edited; ADR-0064 point 7 says it is read without `timer_rephrase`.

### D5 — `internal/core/voice` and #433's path: retire

Retire it in link 1: delete the package, the four §13 rows and doc 02's paragraph about it.

- **No caller, no behavior.** Doc 02 describes a mechanism that does not run and lists four
  calibration constants nothing reads; that is the drift CLAUDE.md non-negotiable 1 forbids, kept
  alive only by the word "arrive".
- **Dormant code is not free**: it is linted, counted in the core coverage floor, and read by every
  reviewer who greps for how replies are made, and it invites a future change to wire it without
  re-deciding.
- **Its future is a different design.** The post-1.0 return is "measured on recordings, under a
  new decision"; #435 showed the next attempt starts from slots, not from #433's check. Both stay
  in history (b3af1e82; `feat/voice-facts`).
- What is kept from it moves to `phrase`: #435's day/time rendering and case rule (D2, D4).

#435 is closed unmerged (2026-11-04). ADR number 0063 is left
unused on main on purpose (the number #435 used), so a link to "ADR-0063" in that PR's history
never resolves to a different decision.

### D6 — I59, the conformance pin

New invariant **I59** (I56 was ADR-0061's and never got a row; I57 is unassigned and left alone):
*a reply that carries a fact is built by code (doc 02 §5 step 2, ADR-0064)*. Test file
`test/conformance/i59_replies_are_built_by_code_test.go`, grown one surface per link:

- an exact-text table per surface, both languages, fixed zone and now (§6's examples are its
  rows);
- a guard: a recording fake `LLMProvider` that fails the test on any task other than
  `capture_processing` and `relation_evaluation` (the two tasks `internal/brain` sends for the
  classification and the relation judge; `classify` is only a `testdata/llm` fixture label, and
  embedding goes through `EmbeddingProvider`, which the guard leaves alone) during a recall, a capture, a correction, a check pass
  that rings a timer, and a digest. Each case also asserts the exact pinned text and that the
  classification was answered (the stored unit carries the model's classification), so a capture
  degraded by the guard cannot pass on "a reply was sent".

L1 tests in `phrase_test.go` cover `Day`, `When`, `Clock`, `Inline` edge cases: every offset
-1..2 and 3, year change, hour 0, 1, 13, a day alone, a due day past and future, acronym, name
from `Names` (title and content each without its first word), trailing period, zone (an instant at 23:30 UTC-3 is "hoy", not "mañana").

Existing pins that change text (updated, never weakened: the property each pins stays, only its
expected words change): I22/I26 recall and language tests, `channel_reply_totality`, the
reminder/digest tests behind I15, I27 and I39–I44, `test/integration` reply pins and
`testdata/days-pass/*.txt` transcripts. Counted in §7 as test lines.

## 3. Doc 02 changes (land with their link)

| Link | Section | Change |
|---|---|---|
| 1 | §5 step 2 | Replace "What the code found is worded by the model…" (ADR-0061) with "A reply that carries a fact is built by code…" (ADR-0064): no model words recall answers, pushes, the digest, confirmations or corrections; days as D2; no gender guessed; deterministic |
| 1 | §13 | Delete `reply_phrasing_timeout`, `reply_max_sentences`, `reply_min_runes`, `reply_runes_factor` |
| 2 | §5 step 2 | "An answer names each unit the way the digest does…" becomes the recall frames (one, past, due, undated, many, none) with examples |
| 3 | §7, §8 | "A reminder says its unit as it is now (ADR-0032 point 4)…fixed frame" gets the new frame and examples; the `phrase` field list (line ~1974) is updated; the late note. §8: "On firing, the LLM rephrases the text" becomes the fixed ring sentence; `rendered_text` no longer written; §5's language paragraph and the order sentence drop the rephrasing |
| 4 | §7, §4 | Digest header, task-due line examples (`Renovar el pasaporte — mar 20 oct` → `, para hoy`), "and N more", the question's wording (§4's "I linked X with Y, are they related?" stays in meaning) |
| 5 | §5 steps 4-5, §7, §8 | Every confirmation and correction example (`Anotado para el …`, `Movido al …`, `Te aviso ese día a las 09:00`, the timer's `Te aviso el mar 27 oct a las 00:20.`, the recurring `todos los años el 23 de octubre a las 09:00`) |

Also, per link: `docs/07-functional.md`'s examples (lines ~64, 86, 106, 214, 265, 421, descriptive; link 3 also
267-268, "At fire time the provider rewords the reminder"), link 3 also `docs/03-data-model.md:93`
(`rendered_text`, "LLM rephrasing at fire time", described as no longer written),
`docs/06-harness.md` §4's I59 row (link 2, grown per link).

ADR bookkeeping landed with this plan (link 0), as precedent fe26b35c did: ADR-0061's `Status`
and `Superseded by`, ADR-0032's `Superseded by` line for point 4, and ADR-0061's README row. No
link edits an Accepted ADR.

## 4. Risks

- **Test churn is large** (rough count: `Recordatorio:` 19 files, `Anotado` 39, `Te aviso` 27,
  `Hoy hay` 19, mostly conformance and `days-pass`). Test lines are counted apart from the 400
  ceiling, but each link's test diff will be big. Mitigation: one link per surface, so each diff
  is one frame's churn; QA per link (these touch reminders and the digest).
- **Interim mixed voice on main** between links (recall warm, pushes still `Recordatorio:`).
  Accepted: each link is a complete surface, and the chain is short.
- **The model reads Nooma's new wording as conversation turns.** A follow-up after "Acordate:
  turno con el oculista, hoy a las 11:00." is read by the same prompt; no binding parses the
  wording. Re-recording a few `reminders/` cases with the new turns is a measurement the
  maintainer may choose; nothing requires it.
- **Names lowered in Spanish** when written capitalised only as an item's first word (D4, Q3).

## 5. What does not change

The timers' numbered list structure and `¿Cuál cancelo?`; the timer's generic nudge; the out-of-scope refusal, `NoAnswer`,
`AskWhichOne`, the greeting, unknown command and voice-note answers; the re-ask block and goal
check-in (client wording of 2026-10-16); `Closed`/`NothingClosed`; which language each surface
speaks; what is said (which items, which leads, which refusals); the glass box (English).

## 6. Before and after

Zone -03:00. Now is Wed 2026-11-04 10:00 unless stated. Every "after" is the exact text a
conformance row pins (the client accepted Q1 and Q3 on 2026-11-04).

### Recall

| Case | Before | After (ES) | After (EN) |
|---|---|---|---|
| One event, ahead (the client's) | `Encontré 1 cosa:`<br>`• Turno para estudios con el gastroenterólogo — jue 26 nov, 08:30` | `Tenés anotado turno para estudios con el gastroenterólogo el jueves 26 de noviembre a las 8:30.` | `You have “Appointment for tests with the gastroenterologist” on Thursday 26 November at 8:30.` |
| One event, past | `Encontré 1 cosa:`<br>`• Cena con Ana — mar 3 nov, 21:00` | `Tenías anotado cena con Ana ayer a las 21:00.` | `You had “Dinner with Ana” yesterday at 21:00.` |
| One task due | `Encontré 1 cosa:`<br>`• Renovar el pasaporte — jue 5 nov` | `Tenés anotado renovar el pasaporte para mañana.` | `You have “Renew the passport” due tomorrow.` |
| One undated | `Encontré 1 cosa:`<br>`• La clave del wifi es 1234.` | `Tenés anotado: la clave del wifi es 1234.` | `You have this noted: The wifi password is 1234.` |
| Several | `Encontré 3 cosas:`<br>`• Turno con el oculista — jue 5 nov, 11:00`<br>`• Cena con Ana — vie 13 nov, 21:00`<br>`• Renovar el pasaporte — lun 9 nov` | `Tenés 3 cosas anotadas:`<br>`• Turno con el oculista, mañana a las 11:00`<br>`• Cena con Ana, el viernes 13 de noviembre a las 21:00`<br>`• Renovar el pasaporte, para el lunes 9 de noviembre` | `You have 3 things noted:`<br>`• Appointment with the optician, tomorrow at 11:00`<br>`• Dinner with Ana, on Friday 13 November at 21:00`<br>`• Renew the passport, due Monday 9 November` |
| None | `No encontré nada sobre eso.` | `No tengo nada anotado sobre eso.` | `I don't have anything noted about that.` |

### Pushes

| Case | Before | After (ES) | After (EN) |
|---|---|---|---|
| Recurring, day alone (now Thu 29 Oct 09:00; the client's) | `Recordatorio: Tomar las pastillas — jue 29 oct` | `Acordate: tomar las pastillas, hoy.` | `Don't forget: Take the pills, today.` |
| Event lead, same day (now Tue 20 Oct 09:00) | `Recordatorio: Turno con el oculista — mar 20 oct, 11:00` | `Acordate: turno con el oculista, hoy a las 11:00.` | `Don't forget: Appointment with the optician, today at 11:00.` |
| Event, the day before | `Recordatorio: Cena con Ana — jue 5 nov, 21:00` | `Acordate: cena con Ana, mañana a las 21:00.` | `Don't forget: Dinner with Ana, tomorrow at 21:00.` |
| Late | `… (Esto llega más tarde de lo previsto.)` | `Acordate: turno con el oculista, hoy a las 11:00. Perdón, te llega más tarde de lo previsto.` | `… Sorry, this is later than intended.` |
| Unit unreadable, no text | `Me pediste que te recordara algo.` | unchanged | unchanged |
| Timer rings (the model's own sentence today) | the model's wording, or the user's words on failure | `Es la hora: sacar la ropa.` | `Time's up: Take the clothes out.` |
| Timer rings late | the words and `(Esto llega más tarde de lo que pediste.)` (`TimerLate`) | `Es la hora: sacar la ropa. Perdón, te llega más tarde de lo que pediste.` | `Time's up: Take the clothes out. Sorry, this is later than you asked for.` |

### Digest (now Wed 4 Nov 08:00)

Before:

```
Hoy hay 3 cosas:
• Renovar el pasaporte — mar 3 nov
• Turno con el oculista — mié 4 nov, 11:00
• Tomar las pastillas — mié 4 nov

Te pregunto de nuevo:
• ¿Ya está «Comprar café»?

Una más — Relacioné "Cena con Ana" con "Reservar restaurante". ¿Tienen relación?
```

After (ES):

```
Buen día. Tenés 3 cosas:
• Renovar el pasaporte, era para ayer
• Turno con el oculista, hoy a las 11:00
• Tomar las pastillas, hoy

Te pregunto de nuevo:
• ¿Ya está «Comprar café»?

Una pregunta más:
Relacioné «Cena con Ana» con «Reservar restaurante». ¿Tienen que ver?
```

After (EN):

```
Good morning. You have 3 things:
• Renew the passport, was due yesterday
• Appointment with the optician, today at 11:00
• Take the pills, today

Asking again:
• Is "Buy coffee" done?

One more question:
I linked “Dinner with Ana” with “Book a restaurant”. Are they related?
```

One line: `Buen día. Tenés una cosa:` / `Good morning. You have one thing:`. Over the message bound:
`…y 2 más.` → `Y 2 más que te cuento mañana.` / `And 2 more I'll tell you about tomorrow.` A
digest with only a question: `Buen día.` then the question, with no "una pregunta más".

### Confirmations and corrections

| Case | Before | After (ES) | After (EN) |
|---|---|---|---|
| Plain note | `Anotado.` | `Listo, anotado.` | `Got it, noted.` |
| Event armed | `Anotado para el jue 12 nov, 10:00. Te aviso el día anterior y unas horas antes.` | `Listo, anotado para el jueves 12 de noviembre a las 10:00. Te aviso el día anterior y unas horas antes.` | `Got it, noted for Thursday 12 November at 10:00. I'll remind you the day before and a few hours before.` |
| Day alone, asked | `Anotado para el vie 13 nov. Te aviso ese día a las 09:00.` | `Listo, anotado para el viernes 13 de noviembre. Te aviso ese día a las 9:00.` | `Got it, noted for Friday 13 November. I'll remind you that day at 9:00.` |
| Task to the digest | `Anotado para el lun 9 nov. Te lo recuerdo en el resumen de esa mañana.` | `Listo, anotado para el lunes 9 de noviembre. Te lo recuerdo en el resumen de esa mañana.` | `Got it, noted for Monday 9 November. I'll mention it in that morning's digest.` |
| Daily | `Te aviso todos los días a las 09:00.` | `Listo, te aviso todos los días a las 9:00.` | `Sure, I'll remind you every day at 9:00.` |
| Timer, tomorrow | `Te aviso el jue 5 nov a las 00:20.` | `Listo, te aviso mañana a las 0:20.` | `Sure, I'll remind you tomorrow at 0:20.` |
| Asked, past | `Anotado, pero no puse aviso: la hora indicada ya pasó.` | `Lo anoté, pero no te puse aviso: esa hora ya pasó.` | `I noted it, but I didn't set a reminder: that time has already passed.` |
| Moved, with leads | `Movido al jue 5 nov, 11:00. Te aviso unas horas antes y 15 minutos antes.` | `Listo, lo pasé para mañana a las 11:00. Te aviso unas horas antes y 15 minutos antes.` | `Done, I moved it to tomorrow at 11:00. I'll remind you a few hours before and 15 minutes before.` |
| Corrected | `Corregido.` | `Listo, lo corregí.` | `Done, I fixed it.` |
| Unchanged | `No cambió nada: ya dice eso.` | `Ya decía eso, así que no cambié nada.` | `It already said that, so I changed nothing.` |

## 7. Slices — stacked to main (`chained-pr`)

Each link is one surface, mergeable alone, with its doc 02 text, its I59 rows and its pin
updates. Estimates are implementation+docs (against 400) / tests; each is measured before opening,
and a link over 400 is cut before it opens.

| # | Branch | Content | Impl+docs | Tests | QA |
|---|---|---|---|---|---|
| 0 | `plan/warm-replies` | proposal, spec, design, tasks; ADR-0064 (Accepted) | planning | — | — |
| 1 | `feat/replies-retire-voice` | Delete `internal/core/voice`; doc 02 §5 step 2 rule, §13 rows | ~350 (312 pure deletion) | −218 | none (no behavior) |
| 2 | `feat/replies-days-and-recall` | `Moment`, `Item`, `Day`, `When`, `Clock`, `Inline`, `Recall`; `reply.go` recall branch; I59 row and recall rows | ~260 | ~450 | per link |
| 3 | `feat/replies-pushes` | `Push`, late note; `render.go` builds an `Item`; the timer's fixed ring (D4b: `rephrase.go` shrinks, `timer_rephrase` retired); doc 02 §7 frame, §8 | ~150 (the rephrase code is mostly deletion) | ~550 (churn) | per link |
| 4 | `feat/replies-digest` | greeting, header, `DigestLineOf`, due lines, "and N more", question; Today follows | ~170 | ~400 | per link |
| 5 | `feat/replies-confirmations` | Noted/NotedFor/NotedOn/NotedForDigest/NotedNotSet/refusals/Every/TimerAt/Moved/Corrected/Unchanged; every lead clock and the timers' list via `Clock` | ~190 | ~700 (churn) | per link |

Links 3-5 depend on link 2's renderer; link 1 is independent and can go first or in parallel.

## 8. Rulings (the client, 2026-11-04)

No open question remains.

- **Q1**: "Tenés anotado …" is accepted for a recalled item.
- **Q2**: a ringing timer is a fixed sentence, not a model rewording (D4b, link 3).
- **Q3**: an item's first letter is lowered mid-sentence in Spanish, accepting the rare "ana cumple
  años" (D4).
