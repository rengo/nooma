# 07 — What Nooma does (functional overview)

This document tells you what Nooma **does**, as a user would experience it. It does not say how
it is built: for that read [`01-architecture.md`](01-architecture.md). It does not define
behavior either. [`02-cognitive-core.md`](02-cognitive-core.md) ("doc 02") governs, and this page
only describes and links. Where a number matters, it points at doc 02 §13 instead of repeating
it, so the two cannot drift.

## What it is

Nooma is a personal digital brain: one self-contained Go binary (`nooma`) running over a
**vault**, a folder holding one person's whole memory in a SQLite file. You tell it things; it
classifies them, weighs them, connects them, lets the cold ones go quiet while you sleep, and
speaks first when something is due. You use it through the command line, an HTTP API, a Telegram
chat, and a web UI served by the same process at `/ui`. Everything it decides on its own is
written to a queryable `decision_log`. See [`00-vision.md`](00-vision.md) for the principles and
positioning.

## At a glance

| You want to… | Surface | Flow below |
|---|---|---|
| Tell it something, ask it something, fix something | CLI `nooma capture`, `POST /capture`, `POST /recall`, Telegram, `/ui/capture` | [A](#a-capture-classification-recall-and-correction) |
| Know what it did while you slept | `decision_log` in the vault | [B](#b-the-night) |
| Hear from it without asking | Telegram | [C](#c-the-morning-digest-and-the-question-it-asks), [D](#d-an-ephemeral-timer) |
| Look at your brain | `/ui` | [E](#e-the-ui-mirror) |

Day one looks like this: `nooma init` creates the vault and a wizard writes your provider
configuration; `nooma serve` starts the API, the UI, the scheduler and (if enabled) the Telegram
channel in one process; `nooma doctor` checks the vault, the configuration and that your provider
returns well-formed JSON; `nooma status` reports on a vault without starting it.

---

## A. Capture, classification, recall and correction

**Scenario.** On Monday you type: *"Dentist on the 14th at 4pm, and I owe Laura the contract by
Friday."* Three weeks later you ask: *"What did I say about Laura?"* Then you notice the dentist
is on the 15th.

**What Nooma does**

1. **Takes it in from any surface.** From a terminal, `nooma capture "<text>"` talks to a
   running `nooma serve` over HTTP. The same text can arrive through `POST /capture`, a Telegram
   message, or the `/ui/capture` form. All of them run the same pipeline
   ([doc 02 §5](02-cognitive-core.md#5-capture)).
2. **Classifies it with one provider call.** The message becomes one or more units, each with a
   type (`task`, `event`, `mental_load`, `knowledge`, …), normalized text, dates resolved against
   your local day ("Friday", "the 14th"), and an initial weight. The dentist visit gets an
   `event_at`; the contract gets a `due_at`. The dentist visit is reminded 24 hours and 2 hours
   before; a date with no time, the day before at 09:00; a reminder whose time has already
   passed is skipped ([doc 02 §7](02-cognitive-core.md#7-prospection--the-proactive-lobe), ADR-0029),
   and so is one that would ring the same day you said it, for an event on a later day, unless it
   is the only one left (ADR-0037). Told after every reminder time has passed, an event on a
   later day is reminded on its own day at 09:00, never at once (ADR-0039); one today gets only
   the reminders still ahead, and none at all when every one has passed: "hoy a las 21", told at
   17:14, is never answered "Te aviso ahora mismo." (ADR-0047).
   Nothing is set for the night: a reminder that would fall before 07:00 is moved to 07:00, or
   to 22:00 the evening before when the event itself is that early (the leads, the 09:00 and
   both hours are your preferences, changed on `/ui/preferences`), so a 05:00 flight is
   reminded at 07:00 and 22:00 the day before. When no time before the event is left outside
   the night, none is set, and the trail says why (ADR-0030). The full taxonomy is in
   [doc 02 §5 step 1](02-cognitive-core.md#5-capture); the units and their statuses in
   [doc 02 §1](02-cognitive-core.md#1-the-unit--the-atom).
3. **Degrades instead of refusing.** If a field comes back malformed, or a provider is down at a
   later step (embedding, the relation judge), the unit is still stored and the missing field is
   left empty. The two failures that leave nothing to store, the classify call itself failing and
   an answer with no usable field, are the next paragraph's. Nooma is cautious to *capture*
   ([doc 02 §5.1](02-cognitive-core.md#51-what-degrades-to-null-means-field-by-field)).
4. **Asks only when it has to.** A vague reference to a person can leave a unit `incomplete`
   until the ambiguity resolves. After a day the nightly pass promotes it with what it has
   ([doc 02 §1](02-cognitive-core.md#1-the-unit--the-atom), [§6 item 1](02-cognitive-core.md#6-nightly-consolidation-sleep)).
5. **Looks for company.** Hybrid recall (vector and full-text legs, fused) finds related
   units, and a judge decides *new*, *duplicate* or *related*. Related ones become
   [relations](02-cognitive-core.md#4-relations) with a confidence.
6. **Answers a question instead of filing it.** When you ask "what did I say about Laura?", the
   message is classified as a `recall`: nothing is stored, and the units that clear the
   admission floor come back, ranked. In a chat each one is named by its title and when it is
   ("Turno con el oculista — mar 20 oct, 10:00"). In a chat the question's own scope narrows
   the answer (ADR-0048): "¿qué te conté anteayer?" lists what was told that day, "¿qué tengo
   esta semana?" what is ahead from now to Sunday, and "lo del oculista" only what names it, or
   a synonym the model wrote ("odontólogo" finds the dentist). Over HTTP this is `POST /recall`;
   in `/ui/units` it is the `?q=` search, neither narrowed
   ([doc 02 §5 step 2](02-cognitive-core.md#5-capture), ADR-0010, ADR-0020).
7. **Corrects in place.** "Actually the dentist is on the 15th" is a `correction`. Where the
   caller holds a unit id (the UI and the API do), that id wins. In chat there is none, so a
   message is read in the conversation's last twenty turns, however old, kept in the vault across
   a restart (ADR-0041): "Es a las 08:00." right after
   a meeting, or "no, el jueves" after a reminder Nooma pushed, moves that item at once, and when
   the model cannot tell which item you mean Nooma asks "¿Te referís a…?" right then and stores
   nothing (ADR-0036, I40). With nothing in the conversation to name, Nooma finds the referent
   by recall and **asks** when it cannot tell which unit you mean rather than guessing. A correction is a patch: from a unit page, "it's at 8am" is read
   against that unit, so the time moves and the rest of the text stays; a change that would
   replace the text with the correction itself, or leave text and date disagreeing, asks instead
   (I30). The edit changes the one field you corrected, with its pre-image
   recorded; a moved date also rewrites the date the text states, and moves, arms or cancels the
   unit's reminders as a fresh capture of the new date would, and the answer names the new
   instant ("Movido al mar 20 oct, 11:00."). Right after an event, "avisame solo 1 hora antes"
   gives that event its own reminders, one hour before, and the answer says so ("Te aviso 1 hora
   antes."); they stay when it moves or your preferences change. Asked of something with no time
   of day, Nooma says it did not change the reminders (ADR-0038). "también 1 hora antes" adds
   that reminder to the ones the event has, and a chat message that tells an event and names
   its leads ("dentista mañana a las 10, avisame 1 hora antes") is reminded at those alone
   (ADR-0045); one already behind never rings at once, but on the event's day at 09:00 ("Te
   aviso ese día a las 09:00."). A chat's first message, the CLI and the API read them too. A
   task keeps its reminder 15 minutes before whatever leads it names. A learning signal is emitted ([doc 02 §5 step 4](02-cognitive-core.md#5-capture), ADR-0016).

**When the capture itself cannot proceed**, Nooma says why in plain words and saves nothing
([doc 02 §5.1](02-cognitive-core.md#51-what-degrades-to-null-means-field-by-field), I31). The CLI
and `/ui/capture` show the sentence; `POST /capture` answers it with a stable `code` next to the
`error` text, and `nooma serve` writes one log line (class, provider, request path), never the key
or what you typed. A missing key names the variable (for example `OPENAI_API_KEY`), says Nooma looks
in the vault's `.env` and the environment, and asks for a restart of `serve`, which reads both once.
`nooma doctor` flags the same missing key under `provider keys`.

| `code` | HTTP | Meaning |
|---|---|---|
| `provider_key_missing` | 503 | The provider's `api_key_env` holds nothing |
| `provider_unreachable` | 502 | No answer from the provider |
| `provider_key_rejected` | 502 | The provider answered 401 or 403 |
| `provider_rate_limited` | 503 | The provider answered 429 |
| `provider_timeout` | 504 | The provider outlived its deadline: 60 s per call unless its `timeout:` in `nooma.yml` says otherwise |
| `canceled` | 499 | The caller hung up mid-capture; logged, not a provider fault |
| `provider_failed` | 502 | Any other non-success answer from the provider |
| `model_output_unusable` | 502 | The answer had no usable field or no type; nothing was saved |
| `invalid_request` | 400 | The body is not JSON, or `text`, `query` or `ids` is missing |
| `unit_not_found` | 404 | No unit has that id |
| `not_wired` | 503 | This build has no capture or recall pipeline (no provider bound) |
| `internal` | 500 | Anything else; the cause is in the `serve` log |

**You end up with** units that carry what you said in your own words (ADR-0024), related to one
another, findable by meaning or by keyword, and editable.

---

## B. The night

**Scenario.** You do nothing. Overnight, Nooma tidies up. In the morning you want to know what it
did and why.

**What Nooma does**

1. **Wakes at 03:00 local time.** The time is a constant, not a setting
   ([ADR-0025](adr/0025-the-schedule-is-not-a-setting.md)). If the process was down at 03:00, a
   boot catch-up runs the missed pass once ([ADR-0009](adr/0009-scheduler-downtime.md)). The
   scheduler lives inside `nooma serve`. You can also run `nooma consolidate`, optionally with
   `--phase=<name>`, by hand.
2. **Runs eight phases, always in this order:** `expire_incomplete`, `archive`, `strengthen`,
   `connect`, `derive`, `reweight`, `pattern_eval`, `learn`
   ([doc 02 §6](02-cognitive-core.md#6-nightly-consolidation-sleep)).
3. **Archives what went cold.** Weight decays with time and is restored by use
   ([doc 02 §2](02-cognitive-core.md#2-weight-decay-temperature-lazy-write-model)). A unit whose
   effective weight has fallen below `weight_threshold` becomes `archived`. It is not deleted:
   archiving is a state transition and the unit can come back
   ([§1](02-cognitive-core.md#1-the-unit--the-atom)).
4. **Connects what belongs together.** `connect` picks the units you touched recently and runs
   the same hybrid recall as capture to propose relations, each judged by the provider and kept
   or dropped by its confidence ([§4](02-cognitive-core.md#4-relations)). Two things are
   connected by what they are about, never only because they fall on the same day. A relation
   the judge is only moderately sure of is stored **and** queued as a question (see flow C).
5. **Derives beliefs.** `derive` proposes statements about *you* (a value, a goal, a
   preference), merges them with existing beliefs instead of duplicating them, and reinforces
   the ones that keep recurring ([doc 02 §10](02-cognitive-core.md#10-the-self-model)).
6. **Looks for patterns.** `pattern_eval` watches for a goal that has gone quiet and for an
   accumulation of open mental-load items ([doc 02 §7](02-cognitive-core.md#7-prospection--the-proactive-lobe)).
7. **Learns.** `learn` moves a kind of link's bar one step when you rejected, or confirmed, 6 of
   the last 8 links of that kind Nooma made, at most once a week per kind. A link of that kind
   now below the bar goes quiet: it is not drawn, listed, asked about or used to rank focus, and
   it is kept, so it comes back if the bar falls (see [M5](#the-learner-m5-in-progress)).
8. **Tells the story.** Every decision with an effect writes a row to the `decision_log` with a
   rationale naming the specific unit, relation or belief ([doc 02 §11](02-cognitive-core.md#11-the-glass-box)).
   `/ui/activity` tells it newest first, and `sqlite3 <vault>` can read it too (see the
   [status table](#status)).

**You end up with** a vault a little more organized than you left it, and an audit trail instead
of a black box.

---

## C. The morning digest, and the question it asks

**Scenario.** At 07:00 your phone buzzes with the day's digest. Among the items is a question:
*"I linked 'Laura's contract' with 'renewal terms' — are they related?"* You answer *"yes"*.

**What Nooma does**

1. **Only speaks to a chat you allowed.** The Telegram channel will not start without
   `allowed_chat_ids`; messages from any other chat are refused. It uses long polling, so it
   opens no inbound port ([ADR-0014](adr/0014-telegram-transport.md)). `/start` gets a short
   greeting in your Telegram language (Spanish when it has none Nooma speaks), and any other
   `/command` a line saying it is not one Nooma knows. Neither goes to the model.
2. **Splits what is urgent from what can wait.** A reminder of an event always goes out on its
   own when due, whatever its urgency, and never after the event has started; the digest drops
   one whose event has passed rather than listing it. Another trigger
   above the push threshold goes out immediately too. Either waits for you to wake during
   quiet hours. Everything else accumulates for the digest ([doc 02 §7, delivery](02-cognitive-core.md#7-prospection--the-proactive-lobe)).
   Hours and thresholds are in [§13](02-cognitive-core.md#13-calibration--numbers-vs-mechanisms).
3. **Sends one digest a day.** It carries the top items by priority. A vault that was off for
   three days owes one digest, not three. If your last energy reading is low, it holds back
   the non-urgent items and lets only a few through, and it softens its wording. This is the
   product rule in [doc 00](00-vision.md): Nooma looks after your *load*, not your feelings.
   A task with a due date ("mañana renuevo el pasaporte") rings no reminder: the reply says so
   ("Anotado para el mar 20 oct. Te lo recuerdo en el resumen de esa mañana.", with its hour,
   "mar 20 oct, 10:00", when you told one), and the digest names it
   first on its due day, and on the next two mornings while it is still open, even on a
   low-energy morning, and even when it is the only thing to say. Answer it "ya lo hice" and the
   task is closed (archived, its reminders dropped) and named no more: "Listo, saqué «Renovar el
   pasaporte»."; when the digest named two, Nooma asks which (ADR-0040, I42). An event works
   the same way: "listo, ya fui" after "hoy a las 21 tengo turno con el kinesiólogo" archives it
   and drops its remaining rings, "Listo, saqué «Turno con el kinesiólogo»." (ADR-0046).
4. **Asks about what it is unsure of.** A relation the nightly job judged into the uncertain
   band was stored but not asserted. The digest carries at most one such question, naming both
   endpoints, oldest first, and never on a low-energy morning
   ([doc 02 §4](02-cognitive-core.md#4-relations), [ADR-0027](adr/0027-pending-question-store.md)).
5. **Understands your answer from the store, not from the model.** Your reply is classified
   like any message, but *which* question it answers is decided by Nooma's own record of what
   it asked, because a model asked to name an id can name a plausible wrong one
   ([doc 02 §5 step 1](02-cognitive-core.md#5-capture)). *Yes* raises the relation's
   confidence. *No* deletes the relation, after recording the rejection. An answer with
   nothing open to answer changes nothing and is logged.
6. **Lets a question lapse.** One that goes unanswered through enough digests is marked
   expired, never deleted, and is not asked again.
7. **Asks its own check-ins.** Goal and task nudges are answered the same way: *engaged* or
   *declined* resolves the most recent open one, or, in a chat, the one of the pushed item the
   answer is about (a message about something told since answers none); for a task check-in, *done* and *drop* resolve
   it and *snooze* ("not yet", a bare "no" included) leaves it open. A check-in about something still
   to do (a task or an open loop) that you put off is asked again from the next day's morning digest ("Te pregunto de nuevo: • ¿Ya está «Comprar café»?"), at most twice, and then closes by itself; one never
   answered, or about an event, is not asked again and closes after the same three mornings, so an old check-in
   stops collecting answers ([doc 02 §7](02-cognitive-core.md#7-prospection--the-proactive-lobe)).

**You end up with** a graph that you correct in one word from your phone, and a nightly job that
learns what you consider a real connection (the learning itself is [M5](#the-learner-m5-in-progress)).

---

## D. An ephemeral timer

**Scenario.** You message the bot: *"remind me in 15 minutes to take the bread out."*

**What Nooma does**

1. **Recognizes a timer, not a memory.** Classification yields type `timer`. A timer is
   infrastructure and is **never stored as a unit**: it lives in its own table and does not
   decay, connect or appear in recall ([doc 02 §8](02-cognitive-core.md#8-ephemeral-timers--infrastructure-not-memory)).
   What makes it a timer is a span counted from now, in minutes or hours ("en 20 minutos",
   "in an hour"); a day or a time of day ("mañana a las 9") is a task or an event with a
   reminder instead.
2. **Arms it at capture** at now plus that span, resolved against your clock, and names that
   moment in the reply ("Te aviso a las 10:28."). It rings once, at that moment: a timer has no
   lead before it.
3. **Fires it when due.** A background check scans every few minutes. At fire time the provider
   rewords the reminder, with a generic nudge as the fallback if it cannot.
4. **Honors an explicit instruction over quiet hours.** You asked for that instant, so a timer
   is delivered even during quiet hours, unlike an inferred trigger
   ([doc 02 §7](02-cognitive-core.md#7-prospection--the-proactive-lobe)).
5. **Says so when it is late.** If the process was down when the timer came due, the message
   carries a note that it is later than intended ([ADR-0009](adr/0009-scheduler-downtime.md)).

6. **Lists and cancels from chat.** "¿qué temporizadores tengo?" answers with the live timers,
   numbered ("Hay 2 temporizadores puestos:\n1) 10:28 — Sacar la pizza del horno …"), or "No hay
   temporizadores puestos.". "cancelá el del horno" cancels the timer it names in that list:
   a state change logged to the activity page, never a delete. When it cannot tell which,
   Nooma asks "¿Cuál cancelo?" with the list, and "el 2" answers it
   ([ADR-0051](adr/0051-timers-are-listed-and-cancelled-through-the-shown-list.md)).

**Not yet:** listing and cancelling from the UI is owned by the last slice of M4 (see the
[status table](#status)).

---

## E. The UI mirror

**Scenario.** On the weekend you open `http://localhost:<port>/ui` in a browser.

**What Nooma does**

1. **Opens straight in on loopback, asks for the token anywhere else.** With a token configured,
   `/ui` shows a login screen and keeps a cookie ([ADR-0028](adr/0028-ui-cookie-handshake.md)).
   A non-loopback bind without a token makes the server refuse to start
   ([ADR-0007](adr/0007-http-auth.md)). `nooma serve --no-ui` serves the API only.
2. **Today** (`/ui`) shows three things:
   - **Focus.** Two lists. The **task focus** is the top items among tasks *and events*. The
     **load focus** is the top items among `mental_load`, bounded to a human-sized number
     (`focus_size`, [§13](02-cognitive-core.md#13-calibration--numbers-vs-mechanisms)). Focus is
     computed on demand and never stored ([doc 02 §3](02-cognitive-core.md#3-focus--computed-never-persisted)).
   - **Pending digest.** What the next digest would carry, including any relation question
     waiting to be asked, worded from each unit as it is now. Today is a page, so it lists every
     reminder waiting; a digest that reaches Telegram's message bound sends fewer and says "and N more". Looking at it changes nothing in the vault: **viewing is not
     delivering** ([doc 02 §7](02-cognitive-core.md#7-prospection--the-proactive-lobe)).
   - **System.** Last consolidation, the latest energy reading and where it came from, counts of
     undelivered items and open questions, and the bind address.
3. **Keeps the focus steady.** A challenger must beat the item currently in focus by a margin
   before it displaces it, so the list does not flicker between near-ties. The "current" list
   is remembered in memory, shared by Today and the digest, and starts fresh after a restart
   ([doc 02 §3, anti-jitter hysteresis](02-cognitive-core.md#3-focus--computed-never-persisted)).
4. **Browses units** (`/ui/units`): live units, newest first, 50 per page, filterable by type,
   and searchable with `?q=` through the same recall mechanism as the API.
5. **Opens a unit** (`/ui/units/{id}`): its content, stored weight, its dates under separate
   labels (event, due), and its relations with direction and confidence.
6. **Captures and corrects.** `/ui/capture` is the same pipeline as every other surface. The
   unit page has a correction form whose target is the unit you are looking at, so the UI never
   has to guess a referent. Whatever you type there corrects that unit, never a new one: write
   the new value ("it is on the 9th at 10") and that field changes, with the date the text
   states and the unit's reminder following it. You stay on that unit: the page comes back with
   its fields as they are now and a line naming what changed. When the text does not
   say what to change, nothing changes and the page says so.
7. **Reviews its beliefs** (`/ui/beliefs`). The active beliefs are listed by facet, each with its
   confidence, origin and last reinforcement. You can edit one: saving it unchanged claims a
   derived belief as yours, and the nightly derive then never overwrites it. You can retire one,
   after a confirm step, and the nightly derive never brings it back.
8. **Shows what it did** (`/ui/activity`). The `decision_log` newest first, 50 rows a page, with
   an "older" link and a `?kind=` filter by family. Each row leads with what happened in plain
   words ("Reminder set", "Corrected", "New belief") and, when it concerns a unit that is still
   live, links to it; Nooma's own reason and the action code follow, secondary. A row that
   records an edit shows what it was before and what it became. An empty page says what will
   appear there. The page is read-only: nothing on it writes or undoes.
9. **Shows every time in your zone.** Each date and time the UI displays, each activity before/after
   value that is a timestamp, and each instant inside Nooma's own sentences (an activity
   rationale, a refusal, a chat reply) is in the zone the server process runs in (the one that travels
   with the clock, [doc 02 §5](02-cognitive-core.md#5-capture)), as
   `2026-10-15 11:00`, with a one-line note naming the zone. Nothing stored changes: the vault and
   the API stay UTC. Text you wrote yourself (a unit's content, a digest line) is shown as written,
   even if it contains an instant. A vault viewed from a browser in another zone still reads in the server's.
10. **Lets you change when it reminds you** (`/ui/preferences`). The page shows the leads before
    a timed event (one row each, up to five, in minutes, hours or days, with Remove and Add), the
    time for an event with no time of day, the end of quiet hours and the evening time, defaults
    included. Saving checks every field with the rules capture uses and, when one is wrong, names
    it and saves nothing. A save applies to every reminder set from then on, also moves the
    reminders already set for upcoming events, and shows up in Activity with the values it
    replaced. A later end of quiet hours also delays the morning digest, which never goes out
    twice in one day.
11. **Lets you tune five settings and see how the nightly job is doing** (`/ui/admin`). The page
    shows, and saves one at a time, the archive threshold, the focus margin, the days before a
    goal counts as stagnant, the mental-load threshold and whether the nightly consolidation runs.
    A value outside what the brain accepts is refused with a message and saves nothing; a saved
    one takes effect on the next request and shows up in Activity with the value it replaced.
    It also shows, read-only, when the last full consolidation ran (or that none has), the
    relation thresholds learned so far, and the latest effects of the nightly pass, labelled as
    effects: a night that decides nothing leaves none.
12. **Draws a unit's connections** (`/ui/graph?unit=<id>`, reached from a unit page that has
    relations). The unit sits in the middle and up to 12 of its live
    related units on a ring around it, strongest first from the top, with every relation among
    them as a line (a curve between two neighbours, so it never passes through the middle). Each
    label is the unit's title, or its text, shortened; the full text is its name for a screen
    reader and its tooltip. Clicking a neighbour, or reaching it with Tab and pressing Enter,
    redraws the graph around it; the middle one opens its unit page. When more are related than
    fit, a line says how many were left out. It is drawn on the server: no dragging, no zoom
    ([ADR-0050](adr/0050-the-graph-is-drawn-on-the-server.md)). Under the drawing each relation
    is listed on its own line, two of different types between the same pair included, each with
    Delete behind a confirm step. Only a link Nooma is still unsure of (below its type's
    threshold) also offers Confirm; one above it is already in force and needs nothing, as a
    line above the list says. Confirming is what answering "yes" to the
    digest's question does: the link stops being uncertain and the learner hears it. Deleting is
    what "no" does: the link is gone and the learner hears that too. Either one closes a question
    still waiting about that link, shows up in Activity with what the link was, and redraws the
    graph with a line saying what was done.

An empty Today, Units or Beliefs page says in one line what will appear there and how, as
Activity does, rather than rendering nothing; so does a unit page's relations section when the
unit is connected to nothing, and the graph of such a unit. A digest with nothing to carry but items held back says how many
it holds. Beliefs says it once when every facet is empty, and under each empty facet otherwise.

**You end up with** a product you can use without a terminal, for what exists today, the
links between your entries included.

---

## Status

As of 2026-10-14. "Shipped" means closed and archived, or recorded as closed in the build plan.

| Capability | Status | Where it is specified |
|---|---|---|
| Vault, `init`, `serve`, `status`, `doctor`, `version` | Shipped in M0 | [05 §M0](05-build-plan.md#m0--skeleton-binary--vault) |
| Capture, classification, hybrid recall, correction; HTTP API and `nooma capture`; provider wizard and `doctor` quality gate | Shipped in M1 | [05 §M1](05-build-plan.md#m1--capture-and-recall), doc 02 §5 |
| Weight, decay, the two focuses (as pure functions) | Shipped in M2 | doc 02 §2, §3 |
| Nightly consolidation at 03:00, boot catch-up, `nooma consolidate`, `decision_log` | Shipped in M2; `learn` runs since M5 | [05 §M2](05-build-plan.md#m2--sleep-and-weight), doc 02 §6, §11 |
| `/start` greets in the sender's Telegram language, Spanish as the fallback, and an unknown `/command` says so, both without the model | Shipped | doc 02 §5 |
| Telegram channel, triggers, push, morning digest, check-ins, ephemeral timers | Shipped in M3 | [05 §M3](05-build-plan.md#m3--the-mouth-telegram--prospection), doc 02 §7, §8 |
| Uncertain relation asked in the digest and answered in chat | Shipped in `m3e` | doc 02 §4; ADR-0027 |
| Event reminders at the user's leads (24 h and 2 h before; a date, the day before at 09:00), stored as vault preferences | Shipped; edited on `/ui/preferences`, which moves the reminders already set; editing them by chat is pending | doc 02 §7; ADR-0029 |
| "Recordame comprar café mañana a las 10" reminds 15 minutes before (a day alone, that day at 09:00), rings even at night for a named time, says so in the reply, or says why nothing was set; an asked "todos los días a las 9pm" is a recurring reminder. The lead is stored but not yet editable; a correction does not yet move an asked reminder | Shipped (one-shot); following a correction and editing the lead are pending | doc 02 §7; ADR-0031, ADR-0033 |
| Event reminders pushed by day, moved out of the night, never sent after the event | Shipped; quiet end (07:00) and evening time (22:00) are vault preferences edited on `/ui/preferences`; editing them by chat is pending | doc 02 §7; ADR-0030 |
| A reminder is never sent once its event is past, by the push, the digest or Today | Shipped | doc 02 §7; ADR-0032 |
| An event whose every reminder lapsed gets one catch-up at once (or at quiet end); a vault with no channel keeps its reminders (an overdue one-shot, a unit-less one included, stays listed on Today) | Shipped | doc 02 §7; ADR-0032 |
| A reminder that could not be pushed is pushed again on a later pass while its event is ahead, once per pass, never twice for one outage report; the digest carries only recurring reminders | Shipped | doc 02 §7; ADR-0032 |
| A reminder, pushed or in the digest, says its unit's current text and instant ("Reminder: Flight to Lisbon, 08:00 — Thu 15 Oct, 08:00"), one message per unit; a bare date has no clock time | Shipped | doc 02 §7; ADR-0032 |
| A task with a due date is named by the morning digest on its day, and two more mornings while open, never held by low energy; Today does not list it | Shipped | doc 02 §7 |
| A chat answer that a task shown is done, or an event shown already happened, closes it (archived, reminders dropped), asks which when unclear, and says so when nothing closed | Shipped | doc 02 §5 step 4; ADR-0040, ADR-0046 |
| A digest that would exceed one Telegram message drops its last lines, says "and N more", and the dropped reminders come in tomorrow's | Shipped | doc 02 §7; ADR-0032 |
| What Nooma says first speaks the user's language: a pushed reminder, in its unit's; the digest (header, lines, question, lateness note, re-asks), in the language the user usually writes in (the most frequent among their 20 newest units); English when none is known; a timer's wording and Today follow the same | Shipped | doc 02 §5, §7; ADR-0032, ADR-0035 |
| UI Today, cookie login, `--no-ui` | Shipped in `m4a` | [05 §M4](05-build-plan.md#m4--the-mirror-complete-ui), ADR-0007, ADR-0028 |
| UI units browse, search, detail, capture and correct | Shipped in `m4b` | 05 §M4; doc 02 §5 |
| Focus held by hysteresis across Today and the digest | Shipped in `m4c` | doc 02 §3 |
| Graph view with edge curation | Shipped in `m4d` | ADR-0050; doc 02 §4; 05 §M4 |
| Beliefs view: list by facet, edit, claim on save, retire; the nightly derive respects both | Shipped in `m4e` | 05 §M4; doc 02 §6, §10 |
| Activity view (the `decision_log` told as a story, newest first, read-only) | Shipped in `m4e-activity` | 05 §M4; doc 02 §11 |
| Admin view (a small set of settings, job status) | Shipped in `m4e2` | 05 §M4; doc 02 §11 |
| Timer list and cancel from chat: "¿qué temporizadores tengo?" lists the live timers, "cancelá el del horno" cancels the one it names through that list, asks which when it cannot tell | Shipped | doc 02 §8; ADR-0051 |
| Timer list and cancel from the UI; answering a pending question from the UI | Pending: `m4f` | doc 02 §8; 05 §M3, §M4 |
| The learner: signals from all surfaces, the `learn` pass, a correctable summary of what was learned | In progress: M5. Signals from every surface (ADR-0052) and the relation `learn` pass (ADR-0053) shipped; goal cadence and the summary planned | doc 02 §9; 05 §M5 |
| `nooma backup`: a verified snapshot taken while serve runs, pruned with `--keep`; a Docker deployment with a daily encrypted upload via rclone | Shipped ahead of M6 | ADR-0049; [08](08-deploy.md) |
| `nooma export`/`import`, full `doctor`, reindex, release builds | Planned: M6 | 05 §M6 |
| Tracking UI, multi-format perception, voice, multi-tenant mode, extra channels | After v1 | 05 "After v1"; doc 02 §12 |

Platform note: Linux and Windows are exercised in CI. `darwin` and ARM targets are build-checked
only ([ADR-0013](adr/0013-cross-compile-targets.md)).

---

## What it deliberately does not do

- **It cares for your load, not your emotions.** It watches open loops and accumulated mental
  load, which are observable and practical. It does not infer or comment on how you feel
  ([00-vision](00-vision.md)).
- **It is not multi-tenant.** One vault belongs to one person, and isolation is the file
  itself. A hosted multi-tenant mode is a possible future, not v1 ([00-vision](00-vision.md)).
- **It never deletes.** Archiving is a state transition and an archived unit can come back.
  Even an unanswered question or an unresolved capture is archived or expired, not removed
  (non-negotiable 6 in [`CLAUDE.md`](../CLAUDE.md)).
- **It is not a GPT wrapper, a wiki, or passive agent memory.** The provider is a replaceable
  part; weights, decay, relations and the nightly job are the product ([00-vision](00-vision.md)).

Safe defaults are structural: the code cannot start in the unsafe state, so there is nothing to
forget to configure.

| Default | What it means |
|---|---|
| No `allowed_chat_ids` | The Telegram channel does not start |
| Non-loopback bind and no token | The server does not start |
| Loopback bind and no token | Open, as a local tool should be |
| Not exactly one allowed chat | There is no push destination, so a trigger is recorded as undeliverable instead of being sent |
| Credentials | The config holds the *name* of an environment variable, never a secret |

---

## The learner (M5, in progress)

> **In progress.** Steps 1 and 2 run today: each verdict is recorded as a typed learning signal,
> from the graph, the chat and the morning digest alike ([ADR-0052](adr/0052-every-verdict-is-a-typed-signal.md)),
> and the nightly `learn` phase moves a kind of link's bar from them ([ADR-0053](adr/0053-a-relation-bar-moves-one-step-on-fresh-evidence.md)).
> Steps 3 and 4 are planned. The story below
> is the design target in [doc 02 §9](02-cognitive-core.md#9-learning--the-prediction-error-loop).
> Every threshold, window and interval M5 uses is **decided by M5**; none is fixed here.

**María** tells Nooma things for months. It connects her notes, but it is eager: it links
two items just because they share a date.

1. **She says no, quietly.** She removes the wrong links from the graph or answers "no" to the
   digest's question. Each rejection is recorded as a learning signal, from chat or from the UI
   alike, because the signal layer does not care which surface it came from.
2. **Nooma gets more careful with her.** After enough rejections of one kind of connection
   (say `same_topic`), the nightly `learn` pass raises *her* bar for that kind. Another user who
   accepts everything would see the bar go the other way. Same brain, personalized.
3. **She sets a goal and drifts from it.** Nooma checks in. She ignores the check-ins. It
   judges by whether she *responded*, not by whether the message was delivered, and it lengthens
   the interval so it bothers her less. Had she engaged, it would shorten it. After adjusting,
   it waits long enough to see the effect before judging again.
4. **She can read what it learned.** A plain-language summary, in the UI and the API, says what
   changed and why ("I raised the `same_topic` bar because you rejected most of the last
   links"), and she can correct it.

What this demonstrates is the line between a wrapper and a brain: the same engine adapts to each
person's actual behavior, visibly and auditably. How the thresholds move, over what window, and
with what cooldown is M5's design work.

---

## Where to go next

| If you want to… | Read |
|---|---|
| Understand the behavior precisely | [`02-cognitive-core.md`](02-cognitive-core.md) |
| See the layers, CLI, config and channels | [`01-architecture.md`](01-architecture.md) |
| Know what was built, in what order | [`05-build-plan.md`](05-build-plan.md) |
| Contribute | [`06-harness.md`](06-harness.md) and [`CLAUDE.md`](../CLAUDE.md) |
