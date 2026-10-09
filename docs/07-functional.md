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
   `event_at`; the contract gets a `due_at`. The full taxonomy is in
   [doc 02 §5 step 1](02-cognitive-core.md#5-capture); the units and their statuses in
   [doc 02 §1](02-cognitive-core.md#1-the-unit--the-atom).
3. **Degrades instead of refusing.** If a provider is down, or a field comes back malformed, the
   unit is still stored and the missing field is left empty. Nooma is cautious to *capture*
   ([doc 02 §5.1](02-cognitive-core.md#51-what-degrades-to-null-means-field-by-field)).
4. **Asks only when it has to.** A vague reference to a person can leave a unit `incomplete`
   until the ambiguity resolves. After a day the nightly pass promotes it with what it has
   ([doc 02 §1](02-cognitive-core.md#1-the-unit--the-atom), [§6 item 1](02-cognitive-core.md#6-nightly-consolidation-sleep)).
5. **Looks for company.** Hybrid recall (vector and full-text legs, fused) finds related
   units, and a judge decides *new*, *duplicate* or *related*. Related ones become
   [relations](02-cognitive-core.md#4-relations) with a confidence.
6. **Answers a question instead of filing it.** When you ask "what did I say about Laura?", the
   message is classified as a `recall`: nothing is stored, and the units that clear the
   admission floor come back, ranked. Over HTTP this is `POST /recall`; in `/ui/units` it is the
   `?q=` search ([doc 02 §5 step 2](02-cognitive-core.md#5-capture), ADR-0010, ADR-0020).
7. **Corrects in place.** "Actually the dentist is on the 15th" is a `correction`. Where the
   caller holds a unit id (the UI and the API do), that id wins. In chat there is none, so
   Nooma finds the referent by recall and **asks** when it cannot tell which unit you mean
   rather than guessing. The edit changes the one field you corrected, with its pre-image
   recorded; a moved date also rewrites the date the text states. A learning signal is emitted ([doc 02 §5 step 4](02-cognitive-core.md#5-capture), ADR-0016).

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
   or dropped by its confidence ([§4](02-cognitive-core.md#4-relations)). A relation the judge
   is only moderately sure of is stored **and** queued as a question (see flow C).
5. **Derives beliefs.** `derive` proposes statements about *you* (a value, a goal, a
   preference), merges them with existing beliefs instead of duplicating them, and reinforces
   the ones that keep recurring ([doc 02 §10](02-cognitive-core.md#10-the-self-model)).
6. **Looks for patterns.** `pattern_eval` watches for a goal that has gone quiet and for an
   accumulation of open mental-load items ([doc 02 §7](02-cognitive-core.md#7-prospection--the-proactive-lobe)).
7. **Leaves `learn` empty.** The slot exists and does nothing yet; it belongs to the learner
   (see [M5](#the-learner-m5-planned)).
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
   opens no inbound port ([ADR-0014](adr/0014-telegram-transport.md)).
2. **Splits what is urgent from what can wait.** A trigger above the push threshold goes out
   immediately, except during quiet hours, when it waits for you to wake. Everything else
   accumulates for the digest ([doc 02 §7, delivery](02-cognitive-core.md#7-prospection--the-proactive-lobe)).
   Hours and thresholds are in [§13](02-cognitive-core.md#13-calibration--numbers-vs-mechanisms).
3. **Sends one digest a day.** It carries the top items by priority. A vault that was off for
   three days owes one digest, not three. If your last energy reading is low, it holds back
   the non-urgent items and lets only a few through, and it softens its wording. This is the
   product rule in [doc 00](00-vision.md): Nooma looks after your *load*, not your feelings.
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
   *declined* resolves the most recent open one; for a task check-in, *done* and *drop* resolve
   it and *snooze* leaves it open ([doc 02 §7](02-cognitive-core.md#7-prospection--the-proactive-lobe)).

**You end up with** a graph that you correct in one word from your phone, and a nightly job that
learns what you consider a real connection (the learning itself is [M5](#the-learner-m5-planned)).

---

## D. An ephemeral timer

**Scenario.** You message the bot: *"remind me in 15 minutes to take the bread out."*

**What Nooma does**

1. **Recognizes a timer, not a memory.** Classification yields type `timer`. A timer is
   infrastructure and is **never stored as a unit**: it lives in its own table and does not
   decay, connect or appear in recall ([doc 02 §8](02-cognitive-core.md#8-ephemeral-timers--infrastructure-not-memory)).
2. **Arms it at capture** with the instant you named, resolved against your clock.
3. **Fires it when due.** A background check scans every few minutes. At fire time the provider
   rewords the reminder, with a generic nudge as the fallback if it cannot.
4. **Honors an explicit instruction over quiet hours.** You asked for that instant, so a timer
   is delivered even during quiet hours, unlike an inferred trigger
   ([doc 02 §7](02-cognitive-core.md#7-prospection--the-proactive-lobe)).
5. **Says so when it is late.** If the process was down when the timer came due, the message
   carries a note that it is later than intended ([ADR-0009](adr/0009-scheduler-downtime.md)).

**Not yet:** listing pending timers and cancelling one, from chat or from the UI, is promised by
doc 02 §8 and is not built. It is owned by the last slice of M4 (see the [status table](#status)).

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
     waiting to be asked. Looking at it changes nothing in the vault: **viewing is not
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
   states following it. When the text does not
   say what to change, nothing changes and the page says so.
7. **Reviews its beliefs** (`/ui/beliefs`). The active beliefs are listed by facet, each with its
   confidence, origin and last reinforcement. You can edit one: saving it unchanged claims a
   derived belief as yours, and the nightly derive then never overwrites it. You can retire one,
   after a confirm step, and the nightly derive never brings it back.
8. **Shows what it did** (`/ui/activity`). The `decision_log` newest first, 50 rows a page, with
   an "older" link and a `?kind=` filter by family. A row that records an edit shows what it was
   before and what it became. The page is read-only: nothing on it writes or undoes.

**You end up with** a product you can use without a terminal, for what exists today. The graph
and admin screens are not built (see below).

---

## Status

As of 2026-10-08. "Shipped" means closed and archived, or recorded as closed in the build plan.

| Capability | Status | Where it is specified |
|---|---|---|
| Vault, `init`, `serve`, `status`, `doctor`, `version` | Shipped in M0 | [05 §M0](05-build-plan.md#m0--skeleton-binary--vault) |
| Capture, classification, hybrid recall, correction; HTTP API and `nooma capture`; provider wizard and `doctor` quality gate | Shipped in M1 | [05 §M1](05-build-plan.md#m1--capture-and-recall), doc 02 §5 |
| Weight, decay, the two focuses (as pure functions) | Shipped in M2 | doc 02 §2, §3 |
| Nightly consolidation at 03:00, boot catch-up, `nooma consolidate`, `decision_log` | Shipped in M2 (`learn` is an empty slot) | [05 §M2](05-build-plan.md#m2--sleep-and-weight), doc 02 §6, §11 |
| Telegram channel, triggers, push, morning digest, check-ins, ephemeral timers | Shipped in M3 | [05 §M3](05-build-plan.md#m3--the-mouth-telegram--prospection), doc 02 §7, §8 |
| Uncertain relation asked in the digest and answered in chat | Shipped in `m3e` | doc 02 §4; ADR-0027 |
| UI Today, cookie login, `--no-ui` | Shipped in `m4a` | [05 §M4](05-build-plan.md#m4--the-mirror-complete-ui), ADR-0007, ADR-0028 |
| UI units browse, search, detail, capture and correct | Shipped in `m4b` | 05 §M4; doc 02 §5 |
| Focus held by hysteresis across Today and the digest | Shipped in `m4c` | doc 02 §3 |
| Graph view with edge curation | Blocked: `m4d` waits on ADR-0019 (still `Proposed`) | ADR-0019; 05 §M4 |
| Beliefs view: list by facet, edit, claim on save, retire; the nightly derive respects both | Shipped in `m4e` | 05 §M4; doc 02 §6, §10 |
| Activity view (the `decision_log` told as a story, newest first, read-only) | Shipped in `m4e-activity` | 05 §M4; doc 02 §11 |
| Admin view (a small set of settings, job status) | In progress: `m4e2`, planned, nothing built | 05 §M4 |
| Timer list and cancel from chat and UI; answering a pending question from the UI | Pending: `m4f` | doc 02 §8; 05 §M3, §M4 |
| The learner: signals from all surfaces, the `learn` pass, a correctable summary of what was learned | Planned: M5 | doc 02 §9; 05 §M5 |
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

## The learner (M5, planned)

> **Planned, not shipped.** Nothing in this section runs today. The `learn` phase is an empty
> slot ([doc 02 §6 item 8](02-cognitive-core.md#6-nightly-consolidation-sleep)). The story below
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
