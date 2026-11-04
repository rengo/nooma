# Spec — warm-replies

> **PM-owned.** Drafted by nooma-dev from the client's ruling of 2026-11-04 because the PM asked
> for it; the PM owns this file and may rewrite it. Exact texts live in design.md §6 and in the
> conformance tests; this file states what must be true.

Zone in every scenario: America/Argentina/Buenos_Aires (-03:00). "Now" is Wed 2026-11-04 10:00
unless stated.

## R1 — No model words a reply that carries a fact

A recall answer, a reminder push, a ringing timer, the digest, a capture confirmation and a
correction's answer are built by code from what the code found or did. No provider call is made to word them, and
none of them waits for one.

- **Given** a vault whose language provider answers only the tasks `capture_processing` and
  `relation_evaluation` (the two the brain sends for classification and the relation judge) and
  fails any other task (`chat` outside a chitchat, `timer_rephrase`, a wording task), with the
  embedding provider untouched since it is a separate port, **when** a recall, a capture, a
  correction, a check pass that rings a timer and a digest run, **then** each reply is sent with
  the exact pinned text of R7, the classification of the capture and of the correction was
  answered (the unit is stored as the model classified it, not degraded), and no other task was
  called. A reply sent from a degraded capture does not satisfy this.

## R2 — Days and times read as a person says them

An instant in a reply is said with a relative day when it is yesterday, today, tomorrow or the
day after tomorrow, else with the weekday, the day of the month and the month's full name, and the
year only when it is not the current one. A time has no leading zero; Spanish says "a la" for one
o'clock. A day alone has no time.

- "mañana a las 10:00" / "tomorrow at 10:00"
- "el jueves 26 de noviembre a las 8:30" / "on Thursday 26 November at 8:30"
- "hoy a la 1:00" / "today at 1:00"
- "el martes 5 de enero de 2027" / "on Tuesday 5 January 2027"

## R3 — A recall answer is a sentence, honest about what it holds

- One item with an instant: one sentence naming it and when it is, in the past tense when that
  instant is behind now.
- One item with no instant: one sentence quoting what is noted, with no day.
- Several items: one line saying how many, then one line per item with its day and time.
- Nothing admitted: one sentence saying nothing is noted about that. It never names a topic, an
  item or a count.

## R4 — A reminder push says what to remember and when, in one sentence

The unit's title (else its content) and its instant now, as R2 says it. A late push says so with
an apology. A push whose unit cannot be read still says what it was armed with, or that the user
asked to be reminded of something.

## R4b — A ringing timer is one fixed sentence

A timer that rings says its words in a fixed sentence: "Es la hora: sacar la ropa." / "Time's up:
Take the clothes out." A late ring adds the lateness note. A timer with no words keeps its
generic nudge. No model rewords it.

## R5 — The digest greets and lists

The digest opens with a morning greeting, says how many things it has, and lists them with R2's
day and time; a task due names its due day as due ("para hoy", "era para ayer"). Lines left for
tomorrow are said as such. The check-ins asked again and the goal check-in keep the client's
wording of 2026-10-16. The relation question names both endpoints.

## R6 — Confirmations and corrections say what was done, warmly

A capture's answer confirms it was noted and, when armed, when the reminders ring, with R2's day
and time. A correction's answer says what changed ("lo pasé para mañana a las 11:00"). A refusal
still gives its reason. A timer's confirmation says its time as R2 says it.

## R7 — Deterministic and in the reply's language

The same input produces the same text, byte for byte, in Spanish (voseo) or English as the
existing language rules decide (ADR-0022, ADR-0035). Every surface above is pinned by an exact
text in a conformance test, in both languages.

## R8 — No article guessed before an item's words

A frame never puts a gendered article or adjective right before an item's own words. In Spanish,
an item's words inside a sentence start lower-case unless the first word is an acronym or is written capitalised elsewhere in the item's own title or content (a name the code can see, such as "Pedro" in "Regalo para Pedro"). A name the code cannot see is lowered: "ana cumple años".
