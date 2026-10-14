# Tasks — remind-me-reminders

Nine links stacked to main (design §5), after this plan merges and ADR-0031 is accepted. Strict
TDD: each RED fails for the right reason first. A link measured over 400 is cut before it opens.

## Review Workload Forecast

| Link | Impl + docs (est.) | Tests (est.) | Risk |
|---|---|---|---|
| 1 `feat/remind-me-decision` | ~240 | ~380 | Medium: the planner every event reminder uses |
| 2 `feat/remind-me-one-shot` | ~350 | ~330 | Medium: migration, payload, scan, reply |
| 3 `feat/remind-me-follows` | ~170 | ~200 | Medium: I29, I35, #339's re-plan |
| 4 `feat/remind-me-preference-ui` | ~110 | ~120 | Low |
| 5 `feat/remind-me-recurring` | ~330 | ~330 | High: re-arm on fire is new for every recurring trigger |
| 6a `feat/question-store-widens` | ~180 | ~180 | Medium: table rebuild of published data |
| 6b `feat/remind-me-ask-back` | ~300 | ~260 | High: binding an answer without the model |
| 7a `feat/remind-me-stop` | ~280 | ~260 | High: dismissing on a chat phrase |
| 7b `feat/remind-me-done` | ~260 | ~240 | Medium: the keep-or-delete question and the mark |
| Total | ~2,220 | ~2,300 | Chained PRs: yes. Decision before apply: ADR-0031 |

PR 0a — `plan/remind-me-reminders`, and 0b — `plan/remind-me-reminders-design`
- [x] 0.1 0a: ADR-0031 (Proposed), its index row, proposal, spec. 0b: design and tasks.
- [x] 0.2 The client's 2026-10-14 answers, NQ1 included, folded in; aligned with #339 as merged.

PR 1 — `feat/remind-me-decision`
- [ ] 1.1 RED/GREEN L1: `AsksToBeReminded`, `RemindCues`, the language sweep, negation, accents (D1).
- [ ] 1.2 RED/GREEN L1: `RemindMeLead`, `ParseRemindMeLead`, `ResolveReminderPrefs` (D6).
- [ ] 1.3 RED/GREEN L1: `leadSet`, `reminders`, `Arm(c, asked, …)`. Cover each R2 case (named time, day only, event union, past lead at once, any kind, timer unchanged), with every existing `Arm`/`Follow` test green (D2, D3).
- [ ] 1.4 RED/GREEN L1: a named-time asked lead is not moved by the night (D4 core).
- [ ] 1.5 Doc 02 §5 step 1, §7, §13; `make check-all`.

PR 2 — `feat/remind-me-one-shot` (first usable link)
- [ ] 2.1 RED: the I36 row in doc 06 §4, and the I36 capture tests (design §3).
- [ ] 2.2 RED/GREEN L3: migration M1 (`units.remind_me`, `config.remind_me_lead_minutes`), the port fields, `TriggerPayload.Asked`, memrepo, contracts, goldens; doc 03 (D5).
- [ ] 2.3 RED/GREEN L1 brain: capture arms, records `asked`, refuses with a reason (D7).
- [ ] 2.4 RED/GREEN L1 phrase + channels: `Firings`, `LeadBefore`, `ThatDayAt`, `NotReminded`, both languages (D8).
- [ ] 2.5 RED/GREEN: the scan exemption for a named time (D4 scan, inside `ReminderVerdict` if #339 is in).
- [ ] 2.6 Corpus case and L4; doc 02 §5 step 5, §11; doc 07; `make check-all`.

PR 3 — `feat/remind-me-follows`
- [ ] 3.1 RED: `TestI36_AnAskedReminderFollowsItsInstant`, `TestI36_ALeadChangeMovesAskedReminders`.
- [ ] 3.2 RED/GREEN L1 brain: correction of an asked unit's instant moves it, arms it after a capture refusal, and expires it. A unit that did not ask moves nothing (D9).
- [ ] 3.3 RED/GREEN: the re-arm includes asked units, and a lead-only save re-arms. `CatchUp` uses `leadSet` (if #339 is in).
- [ ] 3.4 Doc 02 §5 step 4, §7; `make check-all`.

PR 4 — `feat/remind-me-preference-ui`
- [ ] 4.1 RED/GREEN L1 brain: `Save` validates and records the lead.
- [ ] 4.2 RED/GREEN L1 ui: the row, its error, the saved value; UI gates.
- [ ] 4.3 Doc 07; `make check-all`.

PR 5 — `feat/remind-me-recurring`
- [ ] 5.1 RED: `TestI36_AnAskedRecurrenceIsARecurringTask`, `TestI36_ARecurringTriggerReArmsWhenItFires`.
- [ ] 5.2 RED/GREEN L1 core: anchor time, `NextOccurrence` with a time, `recurringTrigger` asked, the verdict's `occurs_at` (D10).
- [ ] 5.3 RED/GREEN L1 brain: an asked recurrence makes a `task`; push routing; re-arm on fire for every recurring trigger, idempotent (D11).
- [ ] 5.4 RED/GREEN L1 phrase: the cadence words. Corpus case for the pills.
- [ ] 5.5 Doc 02 §7 "Recurrence" (re-arm made true); doc 07; `make check-all`.

PR 6a — `feat/question-store-widens`
- [ ] 6.1 RED/GREEN L3: migration M2 rebuilds `pending_questions` and keeps the rows and indexes. The new kinds, `CreateAbout`, `OpenOfKind`. The relation reads are unchanged (m3e tests green); goldens; doc 03 (D12).

PR 6b — `feat/remind-me-ask-back`
- [ ] 6.2 RED: the I38 row, and the ask-back tests.
- [ ] 6.3 RED/GREEN L1 classify: `IsNoAnswer`, `IsOnlyWhen` and their lexicons, swept.
- [ ] 6.4 RED/GREEN L1 brain: ask (D13); bind within the window. A time answer goes through the explicit correction path, "No" becomes daily at `DateOnlyAt`, and anything else is a capture (D14).
- [ ] 6.5 Doc 02, doc 07; `make check-all`.

PR 7a — `feat/remind-me-stop`
- [ ] 7.1 RED: the I38 stop tests (design §3).
- [ ] 7.2 RED/GREEN L1 classify: `StopsReminding`, swept, negation-aware; a stop wins over a done (D15).
- [ ] 7.3 RED/GREEN L1 brain: the binding order, `which_reminder` and its answer, `reminder.stopped` before dismissing, the reply (D16, D16b stop row).
- [ ] 7.4 Doc 02 §5, §11; doc 07; `make check-all`.

PR 7b — `feat/remind-me-done`
- [ ] 7.5 RED: the I38 done tests (design §3).
- [ ] 7.6 RED/GREEN L3: migration M3 `units.done_means_today`; port, memrepo, goldens; doc 03.
- [ ] 7.7 RED/GREEN L1 classify: `SaysDone`, `SaysKeep`, `SaysDelete`, swept (D15).
- [ ] 7.8 RED/GREEN L1 brain: D16b, one test per row. The question is asked once until it is answered, `reminder.kept` is written before the mark, and the daily made by a "No" follows the same rows.
- [ ] 7.9 Doc 02 §5, §11; doc 07; `make check-all`.
