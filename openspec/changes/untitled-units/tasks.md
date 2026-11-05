# Tasks — untitled-units

Stacked to main, one link at a time (design §5). Strict TDD: every RED test is written and watched
failing for the stated reason before its GREEN code. `make check-all` before every push. A link
measured over 400 impl+docs is cut before it opens.

## Review Workload Forecast

| Link | Impl+docs | Tests | Risk |
|---|---|---|---|
| 0 | plan only | none | None |
| 1 | ~170 | ~180 | Low: pure phrase plus four callers |
| 2 | ~300 | ~250 | Medium: a new model task and a decision-log action |
| 3 | ~260 | ~220 | Medium: an unattended nightly write |

Chained PRs: yes (3 links + plan). Decision needed before apply: none; Q1-Q2 are ruled (design §6).

## Link 0 — `plan/untitled-units`

- [x] 0.1 proposal.md, spec.md (PM-owned drafts), design.md, tasks.md.
- [x] 0.2 ADR-0065 `Accepted`, README row; client rulings of 2026-11-05 folded into design.

## Link 1 — `feat/untitled-fallback` (D2)

- [ ] 1.1 RED L1 `phrase.ItemOf`: titled as today; no title + instant is `Unnamed`; no title and
      no instant of its own keeps the content (also when the reply computes a day for it); `Names` built by it (replaces `NamesOf`).
- [ ] 1.2 RED L1 exact text of every unnamed frame in ES and EN (push, recall one ahead, recall
      one past, list line), today / tomorrow / due / past, the late note.
- [ ] 1.3 RED L2 I59 extension: an untitled dated unit's push, recall and digest line contain
      neither its content nor an ISO date; a title-less recurring reminder with an `event_at` is
      said "something noted" (spec R1), one with neither instant keeps its words.
- [ ] 1.4 GREEN `Set` fields and `ItemOf`; the four callers (`pushItem`, `recallItem`,
      `dueLines`, Today). Completeness test covers the new fields.
- [ ] 1.5 Doc 02 §5 recall, §8 push, digest and Today: the unnamed frames, and the fallback half of
      the "read by its content" lines (`:677`, `:855`, `:1989`, `:2085`): a dated unit with no title
      is said as something noted. PR #446 (this link) is amended for it. `make check-all`.
- [ ] 1.6 Mutation probe: route an unnamed item back to its content; a named test goes red.

## Link 2 — `feat/untitled-naming` (D1, D4 capture and correction; ADR-0065 already Accepted by link 0)

- [ ] 2.1 RED L1 `classify.TitleRule`, `BuildTitlePrompt`, `DecodeTitle`.
- [ ] 2.2 RED L3 `UnitRepo.NameIfUntitled` on memrepo and SQLite (title still NULL only).
- [ ] 2.3 RED L2 I60: capture with a dropped title is named before arming; provider failure
      leaves NULL and captures; a date-only correction ends titled; a concurrent title survives.
- [ ] 2.4 GREEN `nameUnit`, the two call sites, decision actions `capture.title.named`, `correction.title.named`.
- [ ] 2.5 Doc 02 §5 (title paragraph retry sentence, "the title follows the body"). Mutation: drop the NULL guard.

## Link 3 — `feat/untitled-night` (D3, D4 night)

- [ ] 3.1 RED L1 `consolidation.SelectToName` order, cut, ties, empty.
- [ ] 3.2 RED L3 `UnitRepo.UntitledDated`.
- [ ] 3.3 RED L2 I60 night: cap and order, `previous`/`next` logged, titled units, content and
      instants untouched, a provider error does not abort, no provider writes nothing.
- [ ] 3.4 RED then GREEN `PhaseTitle` declared between `pattern_eval` and `learn`: update
      `TestOrder_HasExactlyEightPhasesAscendingWithLearnLast` (renamed for nine; still asserts
      `learn` is the last index), the `phase.go` proof comment and the I11 conformance's arrow-line
      parse; the `title` phase; `consolidate.title.*` actions.
- [ ] 3.5 Doc 02 §6 (pipeline line, phase item) and §13 (`title_backfill_per_pass` = 50), doc 03
      `units.title`, doc 06 §4 I11 ("9"; doc 02 has no I11 row) and I60, doc 07 (status row and
      `docs/07-functional.md:156-157`, "eight phases" to nine), the `phase.go:8` type comment, and
      a line that ADR-0065 adds a ninth phase to ADR-0005's eight (no supersession). Mutation:
      reverse the order.
