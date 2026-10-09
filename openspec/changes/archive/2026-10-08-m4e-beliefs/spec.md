# Spec — M4e: beliefs

Specification for `m4e-beliefs-activity-admin`, fifth of six slices sharing
`openspec/changes/m4-mirror-ui/proposal.md`. States what MUST be true after this change, in
testable form; not how (`sdd-design`'s job).

> **Split on 2026-10-08 (umbrella rule, owner decision).** This change was specified as
> beliefs + activity + admin. Splitting PR 5 into "newest-first `DecisionLog` read" and
> "activity view" (the umbrella's own estimate: ~200 + ~300) makes eight implementation PRs, so
> the umbrella rule (proposal.md ~:301, "more than seven PRs or more than 2,400 budgeted lines")
> fires on measurement. **Admin is now its own slice, `m4e2-admin`**
> ([`../m4e2-admin/spec.md`](../../m4e2-admin/spec.md)), after m4e. R7, R8 and R10, the Q5/OQ5/OQ6
> rulings, and the admin part of R9 and R11 moved there with their text, scenarios and rulings
> intact. The directory name `m4e-beliefs-activity-admin` is kept because other artifacts
> reference it; **its scope was beliefs + activity and is now beliefs only (see the next
> note)**. R-numbers are kept (R5, R6, R7, R8, R10 are absent here by design) so a citation
> stays valid across the files.
>
> **Planning-PR task (not done by this spec):** the planning PR updates the umbrella proposal's
> slicing paragraph (`m4e` narrowed, `m4e2-admin` added, OQ7 closed by the split), its §5.1
> `m4e`/`m4e2` rows and totals, and its dependency rows. m4f depends on m4e for "the view shell"
> (layout, nav); that dependency stays on m4e and does not pick up m4e2. **The full list of
> umbrella spots that still put admin inside m4e is in [`design.md`](design.md)'s header** and
> includes the ones easy to miss: the §2 acceptance line (~:75), the §5.2 test rows 9-10
> ("m4e #2/#4", renumbered), the Q5 header ("blocking m4e #5", ~:462), and the independence
> paragraph that lists `ConfigRepo` under m4e (~:261-262).

> **Split again on 2026-10-08 (the owner's 7-PR rule, after PR 2 measured 2.06x).** The
> pre-agreed rule was: if m4e passes seven PRs, split beliefs from activity. PR 1 measured 1.2x
> its forecast and PR 2 measured 2.06x (557 changed lines against ~270; it was cut into 2a,
> #291, and 2b, #292). At that multiplier PRs 3, 4 and 6 would each need a cut too. **Activity
> is now its own change, [`m4e-activity`](../../archive/2026-10-08-m4e-activity/spec.md)**: R5, R6, the activity
> part of R9 and R11, and OQ4 moved there with their text, scenarios and rulings intact.
> **This change's scope is now beliefs only** (R1-R4, R9 for the beliefs routes, R11's beliefs
> part, R12, R13). R-numbers are kept (R5 and R6 are absent here by design, as R7, R8 and R10
> are) so a citation stays valid across the files. The directory name is kept because other
> artifacts reference it.
>
> **Who depends on what now.** `m4e-activity` depends on this change for PR 4's `ui.Deps` /
> `uiDeps` / layout-nav pattern and I22 whitelist, and starts after PR 4 merges. `m4e2-admin`
> depends on `m4e-activity` for `DecisionLog.Before`, the shape-driven change decoder and the
> `config.updated`-shaped fixture, and on this change for `WriteLandedError` (PR 3), the I22
> whitelist pattern, the `uiDeps` pattern and the cross-origin body table (PR 4). `m4f` keeps
> depending on this change for the view shell (layout, nav). The umbrella proposal records the
> split in the same planning PR that carries this note.

Sources: umbrella §2 (acceptance lines 73-75), §3.2 items 3-4, §3.4 (no learning, no undo), §4.2
rows "What a belief delete is" and "Newest-first activity", §5 (`m4e` paragraph, lines 292-294,
the `m4e2-admin` rule at 301), §5.1 `m4e` rows (334-339), §5.2 test rows 9-10, R10, Q4;
`docs/02-cognitive-core.md` §5 step 4 (lines 644-664), §6 item 5, §9, §10 (1217-1228), §11;
`docs/03-data-model.md` (`self_beliefs`, `decision_log`); `internal/ports` (`SelfModelRepo`,
`DecisionLog`, `SignalRepo`); `internal/brain/consolidate.go`; `internal/httpapi/server.go`
(cookie guard, `CrossOriginProtection`).

## Binding rulings

- **Q4 (exercised 2026-10-07)**: `m4e` runs ahead of `m4d`; ADR-0019 is still `Proposed`.
- **Planning-PR obligation**: m4e's planning PR MUST mark the umbrella proposal's Q4 and Q5 rows
  (lines ~483-484, "Provisional") as **Ruled**, citing 2026-10-07. Q5's text and consequences
  live in `m4e2-admin`; the row is still marked by this planning PR. This spec does not edit the
  umbrella.

## Scope boundary

**In**: `/ui/beliefs` (list, edit, retire); the brain services behind it; the derive shield (R12,
R13); new methods on `SelfModelRepo` (no new port, no migration; each widens
`testdata/schema/store_api.golden`).

**Not this change**: `/ui/activity`, `DecisionLog.Before` and the pre-image rendering
([`m4e-activity`](../../archive/2026-10-08-m4e-activity/spec.md)); `/ui/admin`, any `ConfigRepo` write, `RelationRepo.LearnedThresholds`
(all `m4e2-admin`); any reader of `learning_signals` (M5); an undo of a correction (umbrella
§3.4); timers (m4f); the graph (m4d); `/ui/tracking`; a new migration. Doc 02 lines 663-664 are
corrected in the activity-view PR of `m4e-activity` (non-negotiable 1).

## Requirements

| ID | Requirement | Invariants |
|----|-------------|-----------|
| R1 | `/ui/beliefs` lists beliefs grouped by facet, read-only until an action is taken | I22-free; I27-style read |
| R2 | A belief edit changes only `content` (normalised, bounded) and emits `belief_edit` plus a log row | I12 |
| R3 | A belief "delete" is a status transition guarded by a `from`; never a `DELETE` | I03, I12 |
| R4 | `ActiveBeliefs` excludes a retired belief; derive and stagnation never see it, and injection will not | I03 |
| R5, R6 | **Moved to `m4e-activity`** | — |
| R7, R8 | **Moved to `m4e2-admin`** | — |
| R9 | Every mutating beliefs route sits behind the cookie guard and cross-origin protection, proven with a valid body per route | m4a/m4b gates |
| R10 | **Moved to `m4e2-admin`** | — |
| R11 | Doc and golden sync: doc 02 §10 (lines 663-664: `m4e-activity`), doc 03 `status` vocabulary, store API golden, umbrella Q4/Q5 rows | non-neg. 1 |
| R12 | Nightly derive never re-derives a retired belief; the skip is logged | I03, I12 |
| R13 | An edit marks the belief `user_stated`; the night may reinforce it but never overwrites its text | I12 |

### R1 — Beliefs by facet

**MUST**: `GET /ui/beliefs` renders active beliefs grouped under the five facets (`identity`,
`value`, `goal`, `social`, `preference`), each with content, confidence, origin and
`last_reinforced_at`; an empty facet renders as empty, not absent. **Within a facet the order is
total and deterministic: `confidence` descending, then `last_reinforced_at` descending, then `id`
ascending**, independent of the store (the SQLite read has no `ORDER BY` and the in-memory fake
iterates a map). A GET writes nothing.

- GIVEN beliefs in three facets
- WHEN `/ui/beliefs` is requested with a valid cookie
- THEN all five facet groups render, the three populated ones list their beliefs, and no table
  changes
- GIVEN one facet holding beliefs that tie on confidence, and some that tie on both confidence and
  `last_reinforced_at`, returned by the store in the reverse of the expected order
- WHEN the facet renders
- THEN confidence descends, ties break on `last_reinforced_at` descending, and remaining ties on
  `id` ascending

### R2 — Edit

**MUST**: an edit of `content` (the only editable field; closed by owner ruling 2026-10-07; facet,
confidence, topic_key and origin are not form inputs) persists, sets `origin` to `user_stated`
(existing vocabulary, doc 03 `self_beliefs.origin`: `seed|derived|user_stated`), bumps `updated_at`
from one clock read, records a `learning_signals` row `belief_edit` (target_kind `belief`, target_id the belief id, no
FK) and a `decision_log` row naming the belief and the change. An edit naming no existing belief
returns `ErrBeliefNotFound`-shaped not-found and writes no signal and no log row. An edit that
changes nothing writes nothing, **except on a belief whose `origin` is not `user_stated`**
(owner ruling 2026-10-08): saving a derived or seed belief unchanged **claims** it. `origin`
becomes `user_stated`, the stored text is kept byte for byte, `updated_at` is bumped, a
`belief.edited` row with `claimed: true` (next equal to previous) and a **positive** `belief_edit` signal (the
system derived it right; a real edit stays negative) are written. This is how a user protects a derived belief from derive without rewording it.

**Content rules (added 2026-10-08, JD round 1)**: submitted content is **normalised once**:
every `\r\n` becomes `\n`, then surrounding whitespace is trimmed. The normalised value is what is
validated, compared, stored and logged. A normalised value that is empty is rejected. A
normalised value longer than the bound (a named constant, design §3.4; counted in runes) is
rejected. "Changes nothing" is judged on the normalised value, so a browser's CRLF resubmission of
an unchanged textarea is a no-op. The **stored** text is normalised for that comparison by the
same CRLF and trim rules but **without** the empty and length checks: derived content is not
bounded, so validating it would make an over-bound (or blank) derived belief impossible to edit.

- GIVEN an active belief with content "A"
- WHEN the user submits content "B"
- THEN the belief reads "B", one `belief_edit` signal and one log row exist
- GIVEN a derived belief edited by the user
- WHEN the row is read
- THEN `origin` is `user_stated`
- GIVEN a POST that also carries `facet` or `confidence`
- WHEN submitted
- THEN those fields are ignored or rejected and stay unchanged
- GIVEN an unknown belief id
- WHEN an edit is submitted
- THEN a not-found response is returned and no signal or log row is written
- GIVEN a `user_stated` belief with content "a\nb"
- WHEN the form resubmits "a\r\nb" unchanged
- THEN nothing is written (no signal, no log row, `origin` and `updated_at` unchanged)
- GIVEN a derived belief with content "a\nb"
- WHEN the form resubmits "a\r\nb" or "a\nb" unchanged
- THEN the belief is claimed: `origin` is `user_stated`, content reads exactly "a\nb", one
  `belief.edited` row with `claimed: true` and one `belief_edit` signal exist, and a later derive
  proposal under its topic_key cannot overwrite it
- GIVEN a derived belief whose stored content is longer than the bound
- WHEN the user submits valid new content
- THEN the edit lands (`origin` becomes `user_stated`), with one log row and one signal
- GIVEN content of exactly the bound, and content one rune over it
- WHEN each is submitted
- THEN the first is stored and the second is rejected with nothing written

### R3 — Retire ("delete")

**MUST**: the "delete" moves `status` from `active` to the value `retired` (closed by owner ruling 2026-10-07; doc 03 gains the `status` vocabulary line
`active|retired`) through a
`SelfModelRepo` method carrying a `from` precondition (`UnitRepo.SetStatus`'s shape,
`internal/store/sqlite/unitrepo.go:164-185`); it emits `belief_delete` and a log row. No row is
removed. A belief not in `from` is a conflict, with no signal and no log row. Retiring twice does
not emit twice.

- GIVEN an active belief
- WHEN the user retires it
- THEN the row still exists with the retired status, and one `belief_delete` signal and one log row
  exist
- GIVEN an already-retired belief
- WHEN retire is submitted again
- THEN a conflict is reported and the signal and log counts do not change

### R4 — Retired beliefs vanish from consumers

**MUST**: `ActiveBeliefs` returns no retired belief. Its two production callers today, derive's
dedup (`consolidate.go:618`) and `EvaluateStagnation`'s read (`consolidate.go:1192`), therefore
never read one. Classify injection does not read beliefs today (`capture.go:209` passes `nil`);
when it does, `ActiveBeliefs` is its read and already excludes retired. Re-derivation is R12.

- GIVEN a retired belief
- WHEN `ActiveBeliefs` is called
- THEN it is absent, and a repo-level reflection/AST gate finds no `DELETE FROM self_beliefs` in
  production code

### R5 — Newest-first activity

**Moved to [`m4e-activity`](../../archive/2026-10-08-m4e-activity/spec.md) (R5).**

### R6 — Pre-image rendering

**Moved to [`m4e-activity`](../../archive/2026-10-08-m4e-activity/spec.md) (R6).**

### R9 — Mutation gates (beliefs routes)

**MUST**: every non-GET m4e route (`POST /ui/beliefs/{id}/edit`, `POST /ui/beliefs/{id}/retire`)
is behind the cookie guard and `http.CrossOriginProtection`, like m4b's. An unauthenticated or
cross-origin POST changes nothing. The cross-origin conformance gate proves "same-origin reaches
the target exactly once" with a **per-route valid form body**: a table from route pattern to a body
that passes the handler's parsing, and a failing test when a non-GET route has no entry. A fixed
body shared by every route would make a new route's same-origin case fail on a parse error rather
than prove the guard.

- GIVEN a cross-origin POST to a belief edit
- WHEN it arrives
- THEN it is refused and the belief is unchanged
- GIVEN a non-GET route in the wiring table with no entry in the body table
- WHEN the conformance suite runs
- THEN it fails naming the route

### R11 — Sync (m4e's part)

**MUST**: doc 02 §10 states the retire/edit semantics (design §3.11); doc 02 §6 item 5 is amended
(retired shield and conditional embed, PR 2, which licenses the D15/D17 test rewrite); doc 02 §11
gains the belief clause (PR 3); doc 03 gains the `status`
vocabulary; `store_api.golden` regenerated; `scripts/docs-sync.sh` passes; umbrella Q4/Q5 rows
marked Ruled in the planning PR. Activity's doc edits (doc 02 lines 663-664, the `Before` golden widening) belong to
`m4e-activity`; admin's (doc 02 §11 config clause, `configrepo.go:17`) to `m4e2-admin`.

### R12 — A retired belief is never re-derived

**MUST**: the user's word wins. When a derive proposal matches a retired belief, derive creates
nothing, reinforces nothing, revives nothing, and records a skip row in `decision_log` (new action,
name left to design) naming the retired belief and the proposal. Matching, grounded in today's
code (`consolidate.go` `derive`/`persistMergeDecisions`): derive matches only against
`ActiveBeliefs` by embedding cosine (`MergeProposals`, `BeliefMergeCosine` 0.85), and a create is
`UpsertByTopicKey`, which would silently overwrite a retired row sharing the proposal's
`topic_key` (`derived/{facet}/{key}`). Therefore:

- **MUST** (deterministic): a proposal whose computed `topic_key` equals a retired belief's is a
  match.
- **Semantic matching** (a proposal near a retired belief by cosine without sharing a key) has no
  rule in the code today. Whether and how retired beliefs are embedded is left to design (it adds
  provider calls per night, doc 02 §5 step 5 cost note). **Added 2026-10-08:** when a proposal is
  equally near a retired and an active belief, the retired one wins (the user's word wins on a
  tie). When no proposal needs a semantic comparison (no proposals, or every proposal already
  decided by key), derive makes **no** embedding call.
- **Failure policy (added 2026-10-08, extended in round 2):** a retired belief whose embedding
  fails, or whose vector would make the comparison itself fail (non-finite, zero-magnitude, or of
  a different dimension from the others), is treated as matching nothing semantically; the failure
  is logged with its cause; the pass does not fail and the belief is still matched by key. A
  retired belief must never become re-derivable by key because its embedding failed. The same
  screen applies to a proposal's vector: an unusable one gets no semantic match, falls to the key
  rules, and says so in its row's rationale (a zero vector no longer aborts the phase).

- GIVEN a retired belief with `topic_key` K and a proposal deriving K
- WHEN the nightly derive runs
- THEN the belief stays `retired` with unchanged content, no create or reinforce row is written,
  and one skip row is logged
- GIVEN a retired belief and a proposal with a different key
- WHEN derive runs
- THEN the proposal follows the existing create or merge path
- GIVEN a proposal equally near a retired and an active belief
- WHEN derive runs
- THEN it is skipped, not reinforced into the active one
- GIVEN a retired belief whose embedding fails, a proposal sharing that belief's key, and **a
  second proposal with a new key** (so that a semantic comparison is needed and an embedding is
  actually attempted; with only the key-sharing proposal derive makes no embedding call and there
  is no failure to log)
- WHEN derive runs
- THEN the pass completes, one failure row is logged, the key-sharing proposal is still skipped,
  and the second proposal follows the existing create path
- GIVEN a retired belief whose vector is zero, non-finite or of a different dimension (same two
  proposals)
- WHEN derive runs
- THEN the same holds, with the cause recorded in the failure row

### R13 — An edited belief keeps the user's text

**MUST**: a belief with `origin = user_stated` set by an edit never has `content` overwritten by
derive. Derive MAY reinforce it (`ReinforceByID` changes only confidence and `last_reinforced_at`;
this is the existing merge path). The create path MUST NOT overwrite it: a proposal whose
`topic_key` equals an edited belief's is treated as a merge into it (reinforce), not an upsert of
new content. The edited-key rule outranks the semantic-active rule: a same-key proposal merges
into the edited belief **even when another active belief is semantically nearer**. Semantic
matches against active beliefs reach the merge path via `MergeProposals` only when no edited
belief shares the proposal's key. How the topic_key collision is detected (read before write or a guarded upsert) is
design's.

- GIVEN a user-edited belief, and a proposal deriving the same `topic_key` with different content
- WHEN derive runs
- THEN the content is unchanged, confidence is reinforced at most, and the result is logged
- GIVEN a user-edited belief and a semantically matching proposal
- WHEN derive runs
- THEN it is reinforced and its content is unchanged
- GIVEN a user-edited belief U, an active belief A, and a proposal whose `topic_key` equals U's
  and whose vector is nearer to A (cosine 0.95) than to U
- WHEN derive runs
- THEN U is reinforced, A is untouched, and U's content is unchanged

## Verified by

L1 brain and `internal/ui` tests over fakes and a fake clock (R1-R4); L3 for the SQL (R3 `from`
guard); umbrella §5.2 row 9 (R3, R4, signal and log row both exist). Each transition test is
watched failing first. No test opens a browser, the network or a real LLM. (R5, R6, umbrella row 10
and the whole-second activity fixtures are `m4e-activity`'s.)

## Open questions (genuine product decisions, not decided here)

Open, for design and tasks:

- **OQ4 — Activity page size and filters.** Moved to [`m4e-activity`](../../archive/2026-10-08-m4e-activity/spec.md).
- **OQ8 — Semantic match to a retired belief** (R12): embed retired beliefs each night, or key
  match only. For design.

Closed:

- **OQ1** — closed by owner ruling 2026-10-07: only `content` is editable (R2).
- **OQ2** — closed by owner ruling 2026-10-07: the status is `retired`; doc 03 gets the line (R3, R11).
- **OQ3** — closed by owner ruling 2026-10-07: the user's word wins (R12, R13).
- **OQ7** — closed by the owner decision of 2026-10-08: `/ui/admin` splits off as `m4e2-admin`
  (the umbrella rule fires on measurement; see the header).
- **OQ5, OQ6** — closed by owner ruling 2026-10-07, now in `m4e2-admin` (R7).

## Exit criterion

Beliefs can be edited and retired from the UI, each emitting its signal and log row, with no row
deleted; mutations are gated with a valid body per route; docs, golden and umbrella rows are in
sync; `make check-all` is green. Activity is `m4e-activity`'s exit; admin is `m4e2-admin`'s exit.
