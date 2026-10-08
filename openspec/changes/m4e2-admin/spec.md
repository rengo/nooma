# Spec — M4e2: admin

Specification for `m4e2-admin`, the slice split off `m4e-beliefs-activity-admin` on 2026-10-08.
States what MUST be true after this change, in testable form; not how (`design.md`).

> **Split from `m4e-beliefs-activity-admin` on 2026-10-08 by the umbrella rule** (proposal.md
> ~:301: more than seven PRs or more than 2,400 budgeted lines). Splitting m4e's PR 5 into "the
> newest-first read" and "the activity view" made eight implementation PRs in one slice, so the
> rule fired on measurement (owner decision). R7, R8 and R10, the Q5, OQ5 and OQ6 rulings, and the
> admin part of R9 and R11 were moved here with their text, scenarios and rulings intact (and the
> amendments of the 2026-10-08 judgment round, marked as such). R-numbers are kept.
> Source of truth for the split: [`../m4e-beliefs-activity-admin/spec.md`](../m4e-beliefs-activity-admin/spec.md).
>
> **Depends on `m4e-activity` and on m4e (this slice starts after `m4e-activity`'s last PR, PR 6,
> merges, which itself follows m4e's PR 4).** Re-pointed on 2026-10-08, when activity split off m4e (the owner's 7-PR rule, after m4e PR 2
> measured 2.06x): `m4e-activity` now owns what the old m4e PRs 5-6 built.
>
> From **`m4e-activity`** ([`../m4e-activity/spec.md`](../m4e-activity/spec.md)):
> - **the newest-first read** `DecisionLog.Before` and its page cursor (admin lists recent
>   consolidation effects through it);
> - **the change decoder** in `ActivityService` (R6 of m4e-activity), which must already accept
>   the `config.updated` context shape: a row with `fields`, `previous` and `next` objects. This
>   is a cross-slice dependency: m4e-activity's A11 test carries a `config.updated`-shaped
>   fixture, and this slice adds an integration test that the row it writes decodes through
>   m4e-activity's service.
>
> From **m4e** ([`../m4e-beliefs-activity-admin/spec.md`](../m4e-beliefs-activity-admin/spec.md)):
> - **the action vocabulary file** (`internal/ports/decisionlog.go`, its `repocontract` map) as
>   m4e leaves it after PR 3 (m4e-activity adds no action): this slice adds one action,
>   `config.updated`;
> - **`brain.ErrWriteLanded` / `*WriteLandedError`** (m4e PR 3): a config write whose log row
>   fails reports it;
> - the `ui.Deps` / `uiDeps` / `wiring.go` pattern, the layout nav, the cross-origin conformance
>   gate's **per-route body table** (m4e R9), and the I22 whitelist test (m4e design §3.12, G12),
>   all from m4e's PR 4.
>
> **Planning-PR task (not done by this spec):** the umbrella proposal's slicing and dependency rows
> gain `m4e2-admin` (after m4e) in the planning PR. This spec does not edit the umbrella.

Sources: umbrella §2, §3.2, §5 (`m4e2-admin` rule at 301), §5.1 `m4e` rows 338-339, R7, Q5;
`docs/01-architecture.md:126` (`/ui/admin`: system config, job status, thresholds, logs);
`docs/02-cognitive-core.md` §11; `docs/03-data-model.md` (`config`); `internal/ports`
(`ConfigRepo`, `RelationRepo`, `DecisionLog`); `internal/brain/focuskeeper.go:73-77`;
`internal/brain/consolidate.go:1107` (`RecordConsolidationRun`); `internal/core/{consolidation,focus}`
`Resolve*`.

## Binding rulings

- **Q5 (ruled 2026-10-07)**: `/ui/admin` WRITES only `weight_threshold`, `hysteresis_margin`,
  `goal_stagnation_days`, `mental_load_threshold`, `consolidation_enabled`, **one port method per
  field** (`m2c`'s discipline). `relation_thresholds` is SHOWN read-only with a note that it is
  learned and M5 owns it. Admin also shows job status.
- **OQ5 (closed 2026-10-07)**: out-of-range input is rejected in the form and stores nothing (R7).
- **OQ6 (closed 2026-10-07)**: admin config writes log to `decision_log` (R7).
- Q4 and the planning-PR obligation to mark the umbrella's Q4/Q5 rows Ruled stay in m4e's spec.

## Scope boundary

**In**: `/ui/admin` (config writes, job status, read-only thresholds); `AdminService`; five new
`ConfigRepo` methods and one new `RelationRepo` read (`LearnedThresholds`); one new action
`config.updated`. No new port, no migration; each port widening regenerates
`testdata/schema/store_api.golden`.

**Not this change**: editing `relation_thresholds` or `consolidation_last_run_at`; a generic
config writer; any reader of `learning_signals` (M5); beliefs (m4e) and activity (`m4e-activity`).

## Requirements

| ID | Requirement | Invariants |
|----|-------------|-----------|
| R7 | `/ui/admin` writes exactly Q5's five fields, one method per field, validated, logged | Q5 |
| R8 | `/ui/admin` shows `relation_thresholds` read-only with a "learned" note, and job status | Q5, I12 |
| R9 | The admin POST sits behind the cookie guard and cross-origin protection, proven with a valid body | m4a/m4b gates |
| R10 | A `hysteresis_margin` write is effective for the next focus computation | I19 |
| R11 | Doc and golden sync for admin: doc 02 §11 config clause, the stale `configrepo.go:17` comment, store API golden | non-neg. 1 |

### R7 — Admin writes

**MUST**: exactly five new `ConfigRepo` methods, one per Q5 field; each changes only its own
column, creates row `id=1` with the migration's SQL DEFAULTs for every other column when absent
(as `RecordConsolidationRun` does), and bumps `updated_at`. No generic `SetConfig`/map writer. A
POST naming any other key (including `relation_thresholds`, `consolidation_last_run_at`) is
rejected and writes nothing. Out-of-range or non-numeric input is rejected in the form with an
error message and stores nothing (closed by owner ruling 2026-10-07; per-field ranges are a design
decision grounded in the `Resolve*` functions). Each successful write records one `decision_log`
row naming the field, previous and new value (closed by owner ruling 2026-10-07); a rejected or
unchanged write logs nothing.

**Amendments (2026-10-08, JD round 1):**

- **Log shape.** The row's context is shaped like `belief.edited`:
  `{fields:[<column>], previous:{<column>:<v>}, next:{<column>:<v>}}`, so m4e-activity's change decoder
  renders it with no change. `previous` is the value that was **in force** (the effective value
  when the column was unset), and the rationale says so.
- **"Unchanged" is judged against the effective value.** A submission equal to the value in force
  is a no-op: on a vault with no config row, submitting a field's default writes nothing, creates
  no row and logs nothing. A stored value that is corrupt (non-nil but outside the accepted set)
  is not "in force": submitting the default repairs it and logs.
- **Write order.** The config write happens first and its log row second (the previous value was
  already read by `Load`, so nothing is lost by writing first), and a rejected or failed write
  logs nothing. If the log row fails after the write landed, the user is told the value was saved
  but not logged, and the failure is logged by the UI layer; the saved value is not rolled back.

- GIVEN `hysteresis_margin` is posted as a negative number or text
- WHEN submitted
- THEN the form shows an error, the stored value is unchanged and no log row is written
- GIVEN a valid `mental_load_threshold` change
- WHEN submitted
- THEN one log row records old and new values in the `fields/previous/next` shape

- GIVEN a vault with no config row
- WHEN `goal_stagnation_days` is set to 28
- THEN the row exists with 28 and the SQL defaults elsewhere
- GIVEN a vault with no config row
- WHEN `goal_stagnation_days` is submitted as the default (21)
- THEN no row is created and no log row is written
- GIVEN a POST for `relation_thresholds`
- WHEN submitted
- THEN a client error is returned and nothing is written, and the brain's `Update` is never called
- GIVEN an out-of-range value for a known field
- WHEN submitted
- THEN the response is a client error, the form is shown again with the message, and the stored
  value is unchanged
- GIVEN a config write that succeeds and a log write that fails
- WHEN the form is submitted
- THEN the value is stored, the response is a success carrying the "saved, but not logged"
  notice, and the failure is logged by the UI layer

### R8 — Admin read-only parts

**MUST**: `/ui/admin` shows `relation_thresholds` per type (persist/surface) with a note that they
are learned and not editable here, and job status: last full consolidation (nil as *never*),
`consolidation_enabled`, and recent consolidation activity from `decision_log` (the "logs" of
doc 01; scope OQ6). A GET writes nothing.

**Job status, defined honestly (2026-10-08).** No run-level `decision_log` row exists: a full
pass records `consolidation_last_run_at` in `config` (`consolidate.go:1107`), and every
`consolidate.*` action is a per-effect row (archive, strengthen, connect, derive and so on). A
pass that decides nothing writes none. So the page shows (1) the last full run from `config`,
which is the authoritative "did the job run", (2) the `consolidation_enabled` effective value, and
(3) the most recent consolidation **effects**, labelled as effects and not as runs, newest first,
bounded.

- GIVEN a learned threshold row
- WHEN `/ui/admin` renders
- THEN its values show with the learned note and no input for it
- GIVEN newer non-consolidation rows (`capture.*`) than the newest consolidation effect
- WHEN `/ui/admin` renders
- THEN none of them appears in the activity list

### R9 — Mutation gate (admin route)

**MUST**: `POST /ui/admin` is behind the cookie guard and `http.CrossOriginProtection`, like
m4b's. An unauthenticated or cross-origin POST changes nothing. The cross-origin gate's
per-route body table (m4e R9) gains an entry whose body reaches the brain stub (a valid `field`
and `value`); the gate fails if the entry is missing.

- GIVEN a cross-origin POST to `/ui/admin`
- WHEN it arrives
- THEN it is refused and the config is unchanged

### R10 — Margin edit reaches the keeper

**MUST**: `FocusKeeper.compute` reads `hysteresis_margin` from `ConfigRepo.Load` on every
computation (verified, `focuskeeper.go:73-77`); a write is therefore effective on the next Today
request or digest with no restart, and the in-memory incumbent is not reset.

- GIVEN margin 0.5 holding A over a challenger 10% above
- WHEN the margin is set to 0 and Today is requested
- THEN the challenger displaces A

### R11 — Sync (admin's part)

**MUST**: doc 02 §11 states that a user's write through the mirror, a config change included, is
recorded with the value it replaced (m4e adds the belief clause; this slice adds the config
clause); the stale `internal/ports/configrepo.go:17` comment ("no reader in m2c") names its real
reader, `internal/scheduler`; `store_api.golden` regenerated; `scripts/docs-sync.sh` passes.

## Verified by

L1 brain and `internal/ui` tests over fakes and a fake clock (R7-R10); L3 for the SQL (R7 lazy
row and defaults, `LearnedThresholds` order); the cross-origin gate (R9). `memrepo.Config` leaves
other fields nil on a lazily created row while SQLite fills SQL DEFAULTs, so "other columns take
SQL DEFAULTs" is L3-only. Each transition test is watched failing first. No test opens a browser,
the network or a real LLM.

## Open questions

None open. OQ5 and OQ6 are closed (above). OQ7 (this split) is closed by the owner decision.

## Exit criterion

Admin writes exactly Q5's five fields, each logged, and shows the rest read-only; the margin write
reaches the next Today; the route is gated with a valid body; docs and golden are in sync; `make
check-all` is green.
