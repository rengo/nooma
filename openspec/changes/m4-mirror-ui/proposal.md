# Proposal — M4: the mirror (complete UI)

Deliver M4 as laid out in [`docs/05-build-plan.md`](../../../docs/05-build-plan.md) §M4
(lines 201-212): every view [`docs/01-architecture.md`](../../../docs/01-architecture.md) Layer 2
names except `/ui/tracking`, over the stack [ADR-0008](../../../docs/adr/0008-ui-stack.md) and
[ADR-0018](../../../docs/adr/0018-css-approach.md) fixed, behind the UI half of
[ADR-0007](../../../docs/adr/0007-http-auth.md) that M1's [ADR-0017](../../../docs/adr/0017-http-request-auth.md)
deliberately left for M4.

M1 made the binary answer, M2 made it think while nobody looks, M3 made it speak first. **M4 is the
change where the user can see what it is doing without asking** — and therefore the first change
whose failure mode is not a wrong decision but a *misreported* one. Doc 01 states the principle in
five words: *a mirror, not an advisor*. A mirror that computes something the brain does not compute
is a second brain, and this proposal's boundary rule (§3.1) exists to keep that from happening.

This proposal covers the whole chain. `m4a-ui-foundation` is its first slice and the only one it
specifies to the PR; later slices are scoped here and specified when their turn comes, as M3's were.

---

## 1. Why now

M3 closed on 2026-08-24 and `m3e` closed on 2026-09-16 at `4e3115c`. What they left is a brain that
speaks over one channel and is otherwise visible only through `nooma status` and the CLI.

| Fact (verified 2026-09-16 at `4e3115c`) | Consequence |
|---|---|
| `internal/httpapi/server.go:95-98` serves `GET /ui` as a literal placeholder string (`uiPlaceholder`, lines 122-131) on the **open** mux; `server_test.go:163-184` pins it open with a token configured **and asserts no `Set-Cookie` header** (lines 179-181) | The route exists and is unauthenticated by test. The first PR that puts vault data behind it inverts that test on purpose, or ships real data on an open route |
| `internal/httpapi/auth.go:28-37` (`ResolveToken`) and `:54-79` (`requireToken`) implement the bearer header only; ADR-0017 lines 64-71 say the cookie handshake "remains M4's to build, unchanged and unsuperseded" | The UI's own auth is greenfield, with the token source already shared |
| `internal/config/config.go:35` declares `Server.UI *bool`; `defaults.go:17,37-39` defaults it `true`; **no non-test file outside `internal/config` reads it**. `cmd/nooma/serve.go:43-48` declares a `FlagSet` with no flags | `ui: false` and `--no-ui` (doc 01 line 134) are documented and do nothing |
| `internal/ui/doc.go` is four lines; `go.mod` names neither `templ` nor htmx; the `Makefile` has no codegen target; `.github/workflows/ci.yml:125-127` lists the "templ generate leaves a clean tree" gate as **not enforced**, while `docs/06-harness.md:334` lists it as blocking | The whole toolchain is greenfield, and one doc already describes a gate that does not run |
| `.gitattributes` exists (LF normalisation and binaries) and carries no `linguist-generated` line | ADR-0008's committed-`_templ.go` mitigation (lines 60-61) is one line away, not absent |
| `.golangci.yml:49-76` (`core-purity`) denies `core → ui`; `ports-purity` (101-118), `brain-boundary` (120-131) and `scheduler-boundary` (137-156) exist; **no rule names `internal/ui`** | Nothing stops `internal/ui` importing `internal/store` — the same class of gap `m2c` design §10.1 found three times |
| `internal/core/focus`: `Priority` (`priority.go:232`), `Rank` (`rank.go:74`), `AdjacencyStrengths` (`adjacency.go:119`), `Select` (`select.go:113`). **One** production caller: `focus.Rank` from `internal/core/prospection/digest.go:212`. `Select` and `AdjacencyStrengths` have none; `internal/brain/digest.go:112-116` passes an empty adjacency map — *"Adjacency is M4's"* | The exploration's "zero callers" was wrong for `Rank` and right for the two that matter. Hysteresis and adjacency are unexercised, and M4 is the milestone the comment names |
| `ports.UnitRepo` (`unitrepo.go:41-201`) has **no read that lists live units by type**. `LiveFocusCandidates(ctx, ids)` (line 201) takes an id set — `m3b` chose that shape over an unbounded read (its archive report, finding G9). `LiveDecayStates` (line 162) is the only unbounded live read, and it returns `consolidation.Cold` | The Today view cannot get its candidates from any existing port. §3.3 decides what m4a adds |
| The status data the Today view needs exists and is read only by `brain/digest.go` and the CLI: `ConfigRepo.Load().ConsolidationLastRunAt` (`configrepo.go:20,36`, written by `brain/consolidate.go:1107` on a full pass only), `StateRepo.LatestEnergy` (`staterepo.go:66`), `TriggerRepo.Undelivered` (`triggerrepo.go:250`), `PendingQuestionRepo.Unasked/Open` (`pendingquestionrepo.go:117,124`), `DecisionLog.Since` (`decisionlog.go:255`) | m4a's Today view is a new consumer of six existing reads plus one new one |
| `ports.TimerRepo` (`timerrepo.go:58-94`) declares `Create`, `Due`, `Fire`, `Cancel`. `Cancel` (line 93) has exactly one caller: the staleness path at `brain/check.go:216`. **No list read exists** | `CLAUDE.md`'s "declares neither method" is half right: the *read* is missing; the *cancel* has no user-initiated caller. `m4f` owns both and corrects the sentence |
| `ports.SelfModelRepo` (`selfmodelrepo.go:49,59,68`): `ActiveBeliefs`, `UpsertByTopicKey`, `ReinforceByID`. `self_beliefs.status` (`0001_core_tables.sql:81`) defaults to `active` with no `CHECK` | Doc 02 §10 says deleting a belief emits a signal; non-negotiable #6 says nothing is deleted. What a belief "delete" *is* has no definition (§4.2) |
| `DecisionLog.Since` reads **forward** only (`occurred_at > t`, ascending — `decisionlog.go:241-255`) | An activity view that shows the newest first has no read shape yet (§4.2) |
| ADR-0019 is **`Proposed`** (`0019-graph-library.md:3`), and its own §"What this ADR does not decide" leaves the render budget's value open | `m4d-graph` cannot start on it as written. §5 says what unblocks it |
| `docs/06-harness.md:159-183`: the UI is L1 — an htmx interaction is an HTTP request returning a fragment, asserted on structure, never on the whole document. Lines 185-209: the graph island carries no automated test, and its two structural gates ship "in the same PR as the first line of island JavaScript" | The test strategy is already decided. M4 writes no golden of a page and installs no browser |

**M4 needs no migration.** Every table it reads and writes has existed since M0/M2, and the one
persisted thing it might have wanted — a previous focus selection — doc 02 §3 forbids persisting
(§3.5, I01). Its real cost, as in M2 and M3, is the set of behaviours doc 01 names for the UI that
doc 02 never defined (§4.2).

---

## 2. Success criteria

The change is done when:

- [ ] `nooma serve` on a non-loopback bind with a token shows one screen asking for the token once,
      then serves `/ui` on a cookie with `HttpOnly`, `SameSite=Strict`, `Secure` when TLS; the API
      keeps accepting the bearer header; **`/ui` never serves vault data without one or the other**
      (ADR-0007). On loopback with no token there is no screen and no cookie, as today.
- [ ] `--no-ui`, or `server.ui: false`, leaves `/ui` unmounted; the API is unaffected.
- [ ] `templ generate` on a clean checkout leaves a clean tree, and CI fails when it does not.
      `go build ./...` needs no `templ` binary (ADR-0008).
- [ ] `internal/ui` cannot import `internal/store` — the lint fails, not the review.
- [ ] `/ui` (Today) shows the task focus and the load focus as doc 02 §3's two queries over one
      table, the digest items and question that would go out at the morning hour **without marking
      any of them delivered**, and the system status of §4.2.
- [ ] `/ui/units` browses every live unit with search, filters and pagination; its search is the
      recall mechanism, not a third entrance (**I22**). `/ui/capture` captures and corrects with an
      explicit referent (doc 02 §5 step 4, line 570).
- [ ] The focus is anti-jittered across requests by `focus.Select` and an in-process incumbent
      (**I19**), never by a stored flag (**I01**), and `relation_to_active_focus` is fed from real
      relations — `brain/digest.go:112`'s comment is gone.
- [ ] `/ui/graph` renders a server-bounded neighbourhood and lets the user confirm or split an edge;
      a split emits `relation_reject` **before** the delete (**I10**).
- [ ] `/ui/beliefs` edits and "deletes" beliefs and each emits its signal (doc 02 §10);
      `/ui/activity` shows `decision_log` newest-first including a correction's pre-image
      (doc 02 §5 step 4, lines 647-648 corrected); `/ui/admin` shows and edits what §Q5 rules.
- [ ] Timers are listed and cancelled from the UI **and** from chat (doc 02 §8, lines 1154-1155);
      `docs/05-build-plan.md:188-190` and `CLAUDE.md`'s open item close.
- [ ] Every mutation the UI performs goes through `internal/brain`, writes `decision_log` where an
      effect occurs (**I12**), and no view invents a number doc 02 §13 does not name.
- [ ] `make check-all` green and CI green on every PR in the chain; no test opens a browser.
- [ ] **Demo**: "a fully usable product without touching the terminal" (`docs/05-build-plan.md:212`).
      §7 states the part of it that cannot be automated.

---

## 3. Scope

### 3.1 The boundary rule

> **M4 renders what the brain already decides and asks what doc 02 says a surface may ask. It
> computes no new decision, persists no view state, and consumes no learning signal.**

Where a view needs a computation, the computation is `internal/core`'s and M4 gives it a caller.
Where a view needs a read, the read is a port method and `internal/brain` orchestrates it. Where a
user action has an effect, `internal/brain` performs it and logs it. M4 *emits* the signals doc 02
§9-§10 attach to UI actions, and **tunes nothing from them** — that is M5.

### 3.2 In scope

1. **`internal/ui`** — `templ` components, one embedded stylesheet with ADR-0018's six layers,
   vendored htmx under `go:embed`, and one handler per view. Rendering and request shaping only.
2. **`internal/httpapi`** — the ADR-0007 cookie handshake beside `requireToken`, sharing
   `ResolveToken`; security headers; cross-origin protection for UI mutations (Q2); the `/ui/` mount
   replacing the placeholder.
3. **`internal/brain`** — one read-model service per view (Today, browse, graph neighbourhood,
   beliefs, activity, admin, timers), each in `ConsolidateService`'s shape: one clock read, ports,
   core, `decision_log` where an effect occurs. The focus incumbent (m4c). Edge curation (m4d).
   Belief edit/"delete" (m4e). User-initiated timer cancel from UI and chat (m4f).
4. **`internal/ports`** — new **methods on existing ports**, never a new port unless a slice's design
   proves one necessary: a live-by-type unit read (m4a), a browse read (m4b), a neighbourhood read
   (m4d), a newest-first `decision_log` read and a config write (m4e), a pending-timer read (m4f).
   Each widens `testdata/schema/store_api.golden`; **no migration**.
5. **`internal/core`** — the graph neighbourhood bound and its §13 render-budget row (m4d). Nothing
   else: every other number the UI shows is already a §13 row.
6. **`cmd/nooma`** — `--no-ui`, `Config.Server.UI` consumed, UI wiring in `serve`.
7. **Harness** — the templ clean-tree gate (CI + `check-all`), the `ui-boundary` depguard rule,
   `linguist-generated` for `*_templ.go`, and — with the first line of island JavaScript — the two
   gates `docs/06-harness.md:199-209` already names.
8. **The doc 02 and doc 01 amendments of §4.2**, each in the PR that implements it.

### 3.3 The focus query — decided here, and what m4a does not do

The Today view needs the pool doc 02 §3 ranks: live units of `focus.Types(KindTask)` and
`focus.Types(KindLoad)`. No port returns it (§1). m4a adds **one method on `UnitRepo`** — a live read
by type set returning `focus.Candidate`, bounded by status and type rather than by count, the same
shape `LiveDecayStates` already has — and no other port change.

**m4a ranks by `focus.Rank` with an empty adjacency map and takes the top `focus.DefaultSize` per
Kind. It does not call `focus.Select`.** This is the owner's ruling (2026-09-16: *Priority only;
hysteresis as its own link*), and it is exactly the precedent `brain/digest.go:112-116` set: the
honest input until something computes the real one. The accepted cost, stated so nobody discovers
it: **between m4a and m4c the focus may change membership between two requests on a priority
difference smaller than `hysteresis_margin`, and `relation_to_active_focus` contributes nothing.**
Doc 02 §3 already describes this exact state as what every restart produces (lines 330-335); m4a
makes it the steady state for one slice, and `m4c` ends it.

One correction to the ruling's wording, from doc 02 rather than from preference: the previous
selection `focus.Select` compares against is held **in process** (§3, line 330: *"remembering the
previous focus — in process, at the cost of one un-damped transition immediately after every
restart"*). It is never persisted — that is **I01**. `m4c` therefore adds no table and no repo; it
adds an incumbent to the brain's focus service, and the restart cost doc 02 already accepts.

### 3.4 Explicit non-goals, each with its reason

- **No `/ui/tracking`.** Doc 01 line 125: *"once perception exists"*. Perception is v2
  (`docs/05-build-plan.md:231-233`), and §M4 (lines 208-210) does not list the view.
- **No learning.** Belief edit/delete, relation confirm/reject and nudge outcomes *emit* signals
  because doc 02 §9-§10 attach them to the surface. Nothing in M4 reads `learning_signals`, and
  `/ui/admin` shows `relation_thresholds` without editing them (Q5). Doc 05 gives that to M5.
- **No undo.** Doc 02 §5 step 4 (lines 647-648): *"Recording is not undoing. The previous value is
  retrievable; no surface offers it back until the UI exists."* M4 is that UI, and it **shows** the
  pre-image in `/ui/activity`. Offering it back is a new capture-path decision (ADR-0016's own
  scope) and stays out.
- **No second auth model.** One owner, one secret (ADR-0007). No users, no sessions table, no roles.
- **No whole-vault graph.** ADR-0019's decision, if accepted, is a bounded neighbourhood entered from
  `/ui/units`. A view that draws everything is its stated reversal criterion, not a feature.
- **No client-side state.** Every view except the graph island is htmx over server HTML with no
  JavaScript of its own (doc 01 lines 130-133). The island renders what it is given and reports
  through htmx (`docs/06-harness.md:199-202`).
- **No internationalisation.** `docs/06-harness.md:427-428`: deferred, not ruled out. UI copy is
  English.
- **No `nooma reindex`, export/import, or `doctor` widening.** M6.

### 3.5 Invariants in scope, traced

| # | Invariant | Doc 02 | M4 status |
|---|---|---|---|
| I01 | `status='focus'` does not exist; focus is a query | §3 | **Load-bearing in m4a and m4c.** m4a computes at request time; m4c's incumbent lives in the brain's memory, never in a repo. The structural test stands; m4c adds a behavioural one: the incumbent survives no restart |
| I19 | A challenger beats the incumbent by more than `hysteresis_margin` | §3 | **Existing test (`i19_hysteresis_margin_test.go`), first real load in m4c** — `focus.Select` gets its first production caller |
| I02 | Live reads exclude `superseded` and `incomplete` | §1 | In scope four times: Today (m4a), browse (m4b), neighbourhood (m4d), timers' source units (m4f). Each new read surface gets its own exclusion test |
| I22 | Capture's recall entrance and `/recall` are one mechanism | §5 | **In scope, m4b.** `/ui/units` search calls `brain.RecallService`, not `LexicalSearch` directly — a third entrance would be the exact drift I22 forbids |
| I10 | Rejecting a relation emits `relation_reject` **before** deleting | §4, §9 | In scope, m4d — the UI's split is the second rejection surface after m3e's |
| I03 | Nothing deleted; archiving is a transition | §1 | In scope with the I10 exception m3d already scoped to `units`. **m4e's belief "delete" is a status transition** (§4.2) |
| I23 | A correction's pre-image is recorded before the edit | §5 step 4 | In scope, read-only: m4e's activity view renders `context.previous`. Existing `go/ast` test untouched |
| I12 | Every automatic decision with an effect logs | §11 | In scope. UI actions are user-initiated, not automatic; the brain effects they cause (cancel, split, belief change) log as effects. A read-only view writes nothing |
| I04 | A timer is never a unit | §8 | In scope, m4f: timers are listed from `timers`, never through `/ui/units` |
| I09 | The `[persist, surface)` band is stored **and** asked | §4 | In scope: m4a shows the pending question read-only; m4f answers it from the UI through m3e's store |
| I18 | `event_at`, `created_at`, `due_at` never interchanged | §1 | In scope: three views render all three. A template that labels one as another is this invariant's UI failure |
| I13, I20 | — | §9, §12 | Out. Signal consumption is M5; insights are v2 |

---

## 4. Approach

### 4.1 Where the boundary falls

| Decision | Package | Why there |
|---|---|---|
| Which units are in the task focus and the load focus | `core/focus` | Already there; m4a gives `Rank` a second caller, m4c gives `Select` its first |
| Which nodes and edges are in a unit's neighbourhood, under what budget | `core` (m4d) | Data in, data out; the budget is a §13 row (ADR-0019) |
| Read ports, run core, build a view model, log effects | `brain/` | Orchestration, `ConsolidateService`'s shape. **The UI never touches a port** |
| Verify the token, issue and check the cookie | `httpapi/` | Beside `requireToken`; `ResolveToken` stays the one source ADR-0017 named |
| Render a view model to HTML; turn a form into a service call | `ui/` | Presentation. Imports `brain`, `core` types, `ports` value types, `templ`, stdlib — and nothing else |
| Mount, flags, `Config.Server.UI` | `cmd/nooma` | Wiring |

**The `ui-boundary` depguard rule is an allow-list, not a deny-list** — `core-purity` and
`ports-purity`'s shape, because a deny-list has to remember every adapter (`scheduler-boundary`'s
own comment at `.golangci.yml:149-156` shows what forgetting one costs): `$gostd`,
`internal/core`, `internal/brain`, `internal/ports`, `internal/ui`, and the `templ` runtime.
`internal/store`, `internal/providers`, `internal/channels`, `internal/scheduler`, `internal/httpapi`,
`internal/config` and `database/sql` are unreachable from `internal/ui` by construction.

**Decisions this proposal takes that no ADR covers** (each is a design obligation, not an open
question, because the ADRs' own reasoning determines the answer):

- **Loopback with no token: no screen, no cookie.** ADR-0007's own consequence — *"the local case
  pays no friction at all"* — and `requireToken`'s no-op state for the same fact (`auth.go:56-58`).
  The guarantee that this state is only reachable on loopback is `DecideBinding`'s, already pinned.
- **The cookie is a session cookie**: no `Max-Age`, no `Expires`. ADR-0007 says "session cookie"
  and means it; a lifetime would be a number nobody calibrated.
- **Security headers from the first response**: `Content-Security-Policy` with `'self'` only (no
  inline script, no inline style — htmx 2 runs under this with `allowEval` off, and the m4d island
  must fit under it or the island is wrong), `X-Content-Type-Options: nosniff`,
  `Referrer-Policy: same-origin`, `X-Frame-Options: DENY`. Adding them in m4a costs one function;
  adding them in m4d means auditing every template written without them.
- **`--no-ui` unmounts.** With the UI off, `/ui` and `/ui/*` are not registered and fall through
  exactly as any unknown path does today (`server.go:100-105`). The flag overrides `server.ui`;
  `server.ui: false` alone has the same effect.
- **Generated and vendored lines are reported, not budgeted.** `docs/06-harness.md:406-409` counts
  "the lines a reviewer must judge against the design"; a `_templ.go` file and `htmx.min.js` are
  not that. Every PR that carries them states their count beside the budgeted one.

### 4.2 The seven things doc 02 does not define

Doc 01 names the views; doc 02 defines the brain. Between them, seven behaviours the mirror needs
have no definition, and each is an `sdd-design` output subject to owner review, landing with its
doc amendment in the PR that implements it.

| Behaviour | What the docs say today | What M4 must supply | Slice |
|---|---|---|---|
| **"System status"** on Today | Doc 01 line 119: three words | Its contents. Proposed: last full consolidation (`ConsolidationLastRunAt`, nil = never), latest energy reading with its source, counts of undelivered items and open questions, effective bind and whether the UI is behind a cookie | m4a |
| **"Pending digest"** on Today | Doc 01 line 119. Doc 02 §7 defines the digest as a *delivery* | What the UI shows, and the rule: **viewing is not delivering.** The view lists `Undelivered` items and the unasked/open question, and touches neither `surfaced_at` nor `asked_at`; the morning delivery is unchanged | m4a |
| **The focus without an incumbent** | §3 lines 330-335 describe it as the post-restart state | m4a makes it steady state for one slice (§3.3); the amendment says so in one sentence and m4c removes it | m4a, m4c |
| **What a belief "delete" is** | §10: "deleting a belief emits `belief_delete`"; non-negotiable #6: nothing is deleted; `self_beliefs.status` has no vocabulary | A status transition and its name, plus a `SelfModelRepo` method with a `from` precondition (`UnitRepo.SetStatus`'s shape); `ActiveBeliefs` keeps excluding it | m4e |
| **Newest-first activity** | §11: "everything is recorded and explorable in the activity UI"; `Since` reads forward | A `Before(t, limit)` read, or `Since` with a descending flag; and the pre-image rendering (§5 step 4 lines 647-648 rewritten) | m4e |
| **The render budget's value** | ADR-0019: exists, is server-side, is calibratable; value undecided | The number, measured on a real vault, as a §13 row | m4d |
| **A chat message that cancels a timer** | §8: "cancellable from chat"; classify's outcome vocabulary has no cancel | How the intent is recognised and which timer it names — the same *which one* problem m3e solved for relations with a store rather than a model | m4f |

Implementing against prose that cannot produce a testable invariant is how a conformance suite
becomes decoration — M2's §4.2 and M3's, unchanged.

### 4.3 What a view request sees as "now"

Each brain read-model service reads `ports.Clock` **once** per request and every core call
receives that instant — `brain_single_clock_read_test.go`'s rule, and the reason the Today view's
two focuses agree with each other: `effective_weight` is computed on read (**I05**), and two
`Now()` calls inside one page would rank the task focus and the load focus at different instants.

---

## 5. The chain

Six phase changes, sharing this proposal. `m4a` and `m4b` are fixed by ruling. The rest is
argued:

```
m4a-ui-foundation ──> m4b-units-capture ──> m4c-focus-hysteresis ──┬──> m4d-graph (ADR-0019 gate)
                                                                   └──> m4e-beliefs-activity-admin ──> m4f-timer-chat
```

**`m4d` and `m4e` are independent** — `m4d` touches `core`, `RelationRepo` and the island; `m4e`
touches `SelfModelRepo`, `DecisionLog`, `ConfigRepo` — and `m4d` has an external gate `m4e` does
not: **ADR-0019 is `Proposed`.** It cannot start until the ADR is `Accepted` (or superseded), and
the hand audit ADR-0008 requires of the vendored bundle has been recorded — ADR-0019's own spike
says its primitive scan "is a precondition for that audit, not a substitute". The recommendation
(Q4) is that the ruling's order stands, and that `m4e` proceeds ahead of `m4d` if the ADR is not
accepted by the time `m4c` closes. `m4f` is last because it closes the chain's only cross-surface
promise (chat *and* UI) and needs the UI's timer view to exist.

**`m4a-ui-foundation`** — the toolchain, the gates, the boundary, the handshake, `--no-ui`, and a
Today view over the six existing reads plus §3.3's one new one. Priority-only focus.
*Exit*: `nooma serve` behind a token shows the handshake, then a Today page listing real focus
members, the pending digest and the status; `templ generate` and the lint are gates; `--no-ui`
unmounts. Owns I01 (m4a half), I02 (Today), I09 (read-only half), ADR-0007's structural claim.

**`m4b-units-capture`** — `/ui/units` (browse read with filters and pagination; search through
`RecallService`; a unit detail page) and `/ui/capture` (form → `CaptureService.Capture`; correction
with an explicit referent from the detail page). Cross-origin protection lands here: it is the first
slice with a mutating UI route beyond the handshake. Owns I22, I02 (browse), I18.

**`m4c-focus-hysteresis`** — `focus.Select` with an in-process incumbent per Kind, and
`AdjacencyStrengths` fed from `RelationRepo.ByUnit`; the same adjacency reaches
`brain/digest.go:112`. Owns I19's first real load and I01's behavioural half. Depends on m4a.

**`m4d-graph`** — the neighbourhood bound in `core` with its §13 row, its read, Cytoscape.js
vendored (a `size:exception` by construction: one 435 KB file), the island and its two structural
gates, edge confirm and split. Owns I10 (second surface), I03's scope discipline. Depends on
m4b (entered from `/ui/units`) and on ADR-0019 being `Accepted`.

**`m4e-beliefs-activity-admin`** — `/ui/beliefs` with edit and the §4.2 "delete"; `/ui/activity`
newest-first with pre-images; `/ui/admin` with the writable set Q5 rules, job status and
`consolidation_enabled`. Owns I23 (read-only), I03 (beliefs), I12 across the new effects.
Depends on m4a only.

**`m4f-timer-chat`** — a pending-timer read, the timers view with cancel, the chat cancel path
(§4.2), answering a pending question from the UI, and the doc 05 / `CLAUDE.md` sentences closed.
Owns I04's UI half, I09's answer half. Depends on m4b (capture path) and m4e (the view shell).

**If `sdd-tasks`'s forecast for `m4e` exceeds seven PRs or 2,400 budgeted lines, `/ui/admin` splits
off as `m4e2-admin`.** Named now so the split is a measurement, not a mood.

Chain strategy `stacked-to-main`, delivery `auto-chain` — M1's, M2's and M3's own.

### 5.1 Per-PR budgets

**These are guesses**, budgeted against the 400-line soft ceiling (implementation + docs,
excluding tests — `docs/06-harness.md:406-409`), not predictions. This project has measured its
predictions low six times in M0 (1.3x–2.2x) and once at 4.3x in M1 Phase B.

| Change | PR | Content | Est. |
|---|---|---|---|
| **m4a** | `feat/ui-toolchain-gates` | `templ` in `go.mod`; `make templ` + clean-tree target in `check-all`; the CI gate un-deferred (`ci.yml:125-127` rewritten); `linguist-generated`; the `ui-boundary` depguard rule; doc 06 §6 row now true | ~200 |
| | `feat/ui-base-layout` | `internal/ui.Handler`, base layout, `app.css` with the six layers, htmx vendored, security headers; `/ui/` mount replaces the placeholder; `TestHandlerServesBothSurfaces` renamed to say what changed | ~350 |
| | `feat/serve-no-ui` | `--no-ui`; `Config.Server.UI` consumed; unmount semantics; L4 | ~150 |
| | `feat/httpapi-ui-cookie-handshake` | cookie issue and check beside `requireToken`; the screen; `TestOpenRoutesStayOpenRegardlessOfToken`'s `/ui` leg inverted **in the test commit first**; the L4 path `docs/06-harness.md:181-183` names | ~400 |
| | `feat/ports-store-live-focus-by-type` | §3.3's one `UnitRepo` read, SQLite, `repocontract`, `store_api.golden` | ~250 |
| | `feat/brain-today` | the Today read-model service: one clock read, seven reads, `focus.Rank` top-N per Kind, empty adjacency; the "viewing is not delivering" rule with its test; doc 01/02 amendments | ~350 |
| | `feat/ui-today-view` | the Today view; L1 structure tests; the L4 `GET /ui` walk | ~300 |
| **m4b** | `feat/ports-store-units-browse` | the browse read with type/status filters and keyset pagination | ~350 |
| | `feat/brain-browse` | browse + search through `RecallService` (**I22**) | ~300 |
| | `feat/httpapi-cross-origin` | Q2's posture on every non-GET UI route | ~200 |
| | `feat/ui-units-list` | list, filters, pagination as htmx fragments | ~350 |
| | `feat/ui-unit-detail` | one unit, its relations, its dates (**I18**) | ~250 |
| | `feat/ui-capture` | the capture form; correction with an explicit referent | ~350 |
| **m4c** | `feat/brain-focus-incumbent` | `focus.Select` per Kind over an in-process incumbent (**I19**, **I01**) | ~300 |
| | `feat/brain-focus-adjacency` | `RelationRepo.ByUnit` → `weight.Edge` → `AdjacencyStrengths`; `digest.go:112` fed too | ~300 |
| **m4d** | `feat/core-graph-neighbourhood` | the bound and its §13 render-budget row | ~350 |
| | `feat/ports-store-neighbourhood` | the read | ~250 |
| | `feat/ui-vendor-cytoscape` | the bundle (`size:exception`), the recorded audit, the two island gates | ~150 |
| | `feat/ui-graph-view` | the view and the island JS under its line budget | ~400 |
| | `feat/brain-edge-curation` | confirm → `relation_confirm`; split → `relation_reject` then `Delete` (**I10**) | ~350 |
| **m4e** | `feat/ui-beliefs-list` | beliefs by facet | ~250 |
| | `feat/brain-belief-edit-retire` | §4.2's "delete" as a transition; both signals | ~350 |
| | `feat/ports-store-decisionlog-before` | the newest-first read | ~200 |
| | `feat/ui-activity` | the glass box, pre-images rendered; doc 02 lines 647-648 corrected | ~300 |
| | `feat/ports-store-config-write` | Q5's writable set, one method per field (`m2c`'s discipline) | ~250 |
| | `feat/ui-admin` | config, job status, `consolidation_enabled`, logs | ~400 |
| **m4f** | `feat/ports-store-timer-pending` | the list read | ~200 |
| | `feat/brain-timer-list-cancel` | the user-initiated cancel with its `decision_log` row | ~300 |
| | `feat/ui-timers-questions` | the timers view; answering a pending question | ~350 |
| | `feat/brain-chat-timer-cancel` | §4.2's chat path; doc 05 and `CLAUDE.md` sentences closed | ~400 |

Thirty-one PRs, ~8,900 budgeted lines. Read against the measured multipliers, realistically
**11,500–19,000 lines across 40–55 PRs** — M3's own order of magnitude (25 budgeted, 8,500 lines).

**m4a does not fit in four PRs.** Its list — toolchain, gates, boundary, layout, `--no-ui`,
handshake, a new read, a service, a view — is ~2,000 budgeted lines and seven PRs; folding it to
four means three PRs at ~500 each, over the ceiling before the multiplier. The split above is the
proposal's answer, and the handshake PR is the one most likely to run over: if its design lands
above 400, the screen and the middleware split into two PRs, middleware first.

---

## 6. Strict TDD ordering

Strict TDD is active and the mechanism is unchanged from M2 and M3: the test is its own commit
ahead of the implementation commit inside each PR; `sdd-tasks` orders the test task strictly
before the implementation task; `sdd-verify` reads `git log` and reports an inversion as CRITICAL.
**No PR in the chain is deliberately red at its tip** — `main` has required contexts and no bypass.

| Order | Invariant / gate | PR | Why here |
|---|---|---|---|
| 1 | **templ clean tree, `ui-boundary`** | m4a #1 | Both are structural; both are watched failing before the first template exists — the `.golangci.yml` rule against a deliberate `internal/store` import in a scratch file, the gate against a hand-edited `_templ.go` |
| 2 | **ADR-0007's structural claim** | m4a #4 | `TestOpenRoutesStayOpenRegardlessOfToken`'s `/ui` leg is *rewritten in the test commit*: with a token and no cookie, `/ui` serves no vault data. Then the handshake makes it pass. The `/` leg is untouched |
| 3 | **I02** (Today) | m4a #5 | The new read excludes `superseded` and `incomplete` — `repocontract` case before the SQL |
| 4 | **Viewing is not delivering** | m4a #6 | A Today request leaves `surfaced_at` and `asked_at` NULL — asserted before the service exists |
| 5 | **I22** | m4b #2 | Browse search reaches `RecallService`; a `go/ast` or fake-counting test that fails if `LexicalSearch` is called from the browse path |
| 6 | **I19**, **I01** | m4c #1 | Existing I19 test gets its first production caller; a new test proves the incumbent does not survive a fresh service |
| 7 | **Island gates** | m4d #3 | The no-`fetch`/`eval` scan and the line budget, in the PR with the first JS line — `docs/06-harness.md:206-209` |
| 8 | **I10, I03** | m4d #5 | `relation_reject` recorded before `Delete`; I03's `units` scope stays deliberate |
| 9 | **I03** (beliefs), **I12** | m4e #2 | The "delete" is a transition with a `from`; `ActiveBeliefs` excludes it; the signal and the log row both exist |
| 10 | **I23** (read-only) | m4e #4 | The activity view renders `context.previous`; the `go/ast` test is untouched and still green |
| 11 | **I04**, **I09** | m4f #2-#3 | Timers listed from `timers`; a UI answer resolves the same `pending_questions` row the digest asked |

---

## 7. The demo boundary, stated honestly

`docs/06-harness.md:159-183` already decided how the UI is tested: every htmx interaction is an
HTTP request returning a fragment, asserted on structure at L1 with `httptest`; L4 walks
`nooma serve` → `GET /ui` → the handshake through the compiled binary, as `test/e2e/serve_test.go`
already walks the placeholder. No test installs a browser, and lines 185-209 make the graph island
the one surface with no automated test, by decision.

That leaves two things CI cannot say, and they are the two the milestone is named for:

- **What a browser does with the markup.** Every `hx-*` attribute is asserted as text; whether
  htmx, under the CSP §4.1 sets, actually performs the swap is unverified until a human loads the
  page. The same is true of the island's WebGL render — ADR-0019's spike ran no browser either.
- **"Without touching the terminal."** The build plan's own demo is a walkthrough, not an
  assertion.

**`m4f` closes with one manual pass**: a vault served on a LAN bind with a token, opened from a
second machine's browser, the handshake completed once, and every view exercised — capture,
browse, graph curation, a belief edited, the activity trail read, a timer cancelled. It is the
milestone's exit gate, run by the maintainer, recorded in `m4f`'s archive report, exactly as M1's
live-provider pass (PRs #129-#132) and M3's real-bot pass were.

---

## 8. Open questions

Each is a decision the owner makes before `m4a`'s spec and design. The recommendation is this
proposal's reasoning, not a settled answer. **This section is the proposal question round.**

### Q1 — What does the cookie carry? *(blocking for `m4a` #4)*

ADR-0007 fixes the cookie's flags and says "two ways to present the same secret, one secret". It
does not say whether the cookie's *value* is the secret.

| Option | Pro | Con |
|---|---|---|
| **A. The token itself**, compared with `subtle.ConstantTimeCompare` exactly as the header is | One secret, one comparison, zero process state; revoking is what it already is for the header (rotate the variable, restart) | The secret travels in every browser request. Under `HttpOnly` + `SameSite=Strict` + `Secure`-when-TLS that is the same exposure the header already has on the same LAN — ADR-0007 names and accepts it |
| **B. An opaque session id** held in process, mapped to "authenticated" | The secret crosses the wire once | A session table with no owner: lost on restart, and "log out" becomes a feature with state to test. ADR-0007 rejected "users + sessions" for exactly this |

**Recommendation: A.** The ADR's own sentence is the argument; B builds the thing it declined.

### Q2 — Cross-origin posture for UI mutations, including on loopback *(blocking for `m4a` design; first exercised in `m4b`)*

`SameSite=Strict` protects a cookie. On loopback there is no cookie and no token, and a page on
any website can make the user's browser `POST` a form to `http://127.0.0.1:7777/ui/capture`. The
API has the same exposure today for `POST /capture` with a `text/plain` body; M4 is where a plain
HTML form makes it trivial.

| Option | Pro | Con |
|---|---|---|
| **A. Reject any non-GET UI request whose `Sec-Fetch-Site` / `Origin` is not same-origin** — the stdlib's `net/http.CrossOriginProtection` (Go ≥ 1.25; `go.mod` is 1.26.4) | ~20 lines, structural, no per-form token, covers loopback and LAN alike, and htmx requests are same-origin by construction | Refuses a request from a very old browser that sends neither header; accepted — such a browser cannot run the UI anyway |
| **B. A synchronizer token in every form** | Textbook | State per session, a hidden field in every template, and it protects nothing on loopback without a cookie to bind to |
| **C. Nothing; rely on `SameSite=Strict`** | Zero code | Zero protection on the default, loopback, no-token configuration — non-negotiable #7 says safe defaults are structural |

**Recommendation: A, as one middleware in `internal/httpapi` wrapping the UI mount, decided in
m4a's design so the layout PR ships with the middleware chain complete, and given its first
mutating route in m4b.** Whether the same middleware wraps the API's `POST` routes is a separate
question the API's own ADR-0017 does not ask; this proposal does not answer it.

### Q3 — What Today shows as "system status" and "pending digest" *(blocking for `m4a` #6)*

Doc 01 gives three words each (§4.2). Proposed:

- **Status**: last full consolidation (nil rendered as *never*), the latest energy reading with
  its source and instant (nil as *no reading*), the undelivered count, the open-question count,
  the effective bind and whether the UI is behind a cookie. Nothing else — a status line that
  lists every config key is `nooma status`, which already exists.
- **Pending digest**: the same `Undelivered` items and the unasked or open question the morning
  delivery would carry, in the same order `prospection.Carry` would give them, **read-only** —
  `surfaced_at` and `asked_at` untouched, the morning delivery unchanged. Not the digest's
  rendered text: that is a channel message, and the UI is not a channel.

**Recommendation: as proposed.** The alternative — show the *rendered* digest — would make
Today a preview of a Telegram message and tie the view to the channel's wording.

### Q4 — Does `m4e` go ahead of `m4d` when ADR-0019 is still `Proposed`? *(not blocking `m4a`)*

`m4d` cannot start on a `Proposed` ADR. The ruling's order puts it fourth. **Recommendation: keep
the order as the plan, and let `m4e` run first if ADR-0019 is not `Accepted` when `m4c` closes.**
Accepting ADR-0019 needs two things the spike named and did not do: the hand audit of the bundle,
and a render-budget measurement on a real vault. Both are owner work, and the chain should not
idle on them.

### Q5 — Which knobs does `/ui/admin` write? *(not blocking `m4a`; blocking `m4e` #5)*

Doc 01 line 126: "system config, job status, thresholds, logs". Doc 02 §9 gives M5's learner
`relation_thresholds` and the goal cadence. **If the user and the learner both write a field, they
fight, and the decision log cannot say who won.**

**Recommendation: `/ui/admin` writes the `config` singleton's hand-set calibratables —
`weight_threshold`, `hysteresis_margin`, `goal_stagnation_days`, `mental_load_threshold`,
`consolidation_enabled` — one port method per field (`m2c`'s discipline), and *shows*
`relation_thresholds` read-only with a note that they are learned.** The correctable surface for
what the learner did is M5's own deliverable ("natural-language summary … correctable",
`docs/05-build-plan.md:217`), not an M4 form. Cost: a user who wants to hand-set a relation
threshold in M4 cannot, and is told why.

### Owner rulings — 2026-09-16

| # | Ruling | Status |
|---|---|---|
| Q1 | **The cookie carries the token itself.** Compared in constant time exactly as the bearer header is; no session table, no session id, no expiry bookkeeping. Rotating `server.token` invalidates every cookie. | Ruled |
| Q2 | **Stdlib `http.CrossOriginProtection`**, as one `httpapi` middleware on every non-GET UI route. No per-form tokens. It also covers loopback with no token. | Ruled |
| Q3 | **Today as proposed**: FOCUS (Priority-only, top-N per Kind), PENDING DIGEST (the raw `Undelivered` items and the unasked or open question, in `prospection.Carry` order, read-only — viewing is not delivering), SYSTEM (exactly the six lines above). Never the rendered channel text. | Ruled |
| Q4 | `m4e` may run ahead of `m4d` if ADR-0019 is still `Proposed` when `m4c` closes. | Provisional — put to the owner before `m4d` starts |
| Q5 | `/ui/admin` writes only the hand-set calibratables listed above, one port method per field; `relation_thresholds` stays read-only. | Provisional — put to the owner before `m4e`'s spec |
| Q6 | Raised by `m4a`'s design: the API's `POST` routes do **not** sit behind `CrossOriginProtection` in M4; if wanted later, it is one line plus an ADR-0017 amendment, not a UI decision. | Provisional — design default, revisit if a browser client ever calls the API |
| Q7 | Raised by `m4a`'s design: **the focus renders each member's priority `Score`**, two decimals, `NaN` as the literal `NaN`. Ruled 2026-09-16 when the owner approved the Today wireframe showing `priority 0.82` beside each focus member. | Ruled |

Q1–Q3 unblock `m4a`'s spec and design; Q7 is the one design-raised question already ruled.

---

## 9. Risks

| # | Risk | Rank | Mitigation |
|---|---|---|---|
| R1 | **Real vault data on an open route.** `GET /ui` is open by test today; the first Today PR would ship data behind it if the handshake is not ahead of it | **1** | §5.1's order: handshake (m4a #4) lands before the read (#5), the service (#6) and the view (#7). §6 order 2 inverts the pinning test in the test commit |
| R2 | **The mirror computes.** A template that sorts, filters or sums is a decision outside `core` with no test | **2** | The `ui-boundary` allow-list keeps ports out of `ui`; the review property is "a template receives a finished view model". Named as a property, not a gate — a gate would need a template linter this repo does not have |
| R3 | **Seven undefined behaviours (§4.2)**, each an owner review | 3 | The six-way split asks about at most two per slice, at the point of need — M2's and M3's remedy |
| R4 | **Cross-origin writes on loopback** (Q2) | 4 | Decided in m4a's design; a test posts with a foreign `Origin` and expects a refusal before any form exists |
| R5 | **The templ toolchain on Windows CI.** `integration (windows)` and `e2e (windows)` run `go test` directly (`docs/06-harness.md:365-369`); the clean-tree gate must run where `templ` is installable, once, on Linux | 5 | The gate is one Linux job; committed `_templ.go` files mean the Windows jobs need no `templ` at all — ADR-0008's own reason for committing them |
| R6 | **Focus flaps between m4a and m4c** (§3.3) | 6 | Accepted by ruling and stated in doc 02's amendment; m4c is the third slice, not the last |
| R7 | **ADR-0019 stays `Proposed`** and m4d idles | 7 | Q4: m4e proceeds; the audit and the measurement are named as the two unblocking acts |
| R8 | **The vendored bundle is a 435 KB diff** nobody can review line by line | 8 | `size:exception` by construction; the recorded hand audit and the no-`fetch`/`eval` gate are what is reviewed |
| R9 | **CSP breaks htmx or the island** late | 9 | Headers ship in m4a #2 before any view; a view that needs an inline script is wrong, not the header |
| R10 | **`DecisionLog.Since` is forward-only** and the activity view paginates backwards | 10 | A new read (m4e #3), not a client-side reverse of a capped forward read |
| R11 | **Estimates run low** — 1.3x–2.2x six times, 4.3x once | 11 | §5.1 states the multiplier; m4a's split is decided here, with its own overflow rule for the handshake PR |
| R12 | **The UI slice hides from `docs-sync`.** Most m4a PRs touch no `internal/core/**`, so the sync gate never fires and doc 01/02 amendments could be skipped | 12 | Each §4.2 row names its PR; `sdd-verify` checks the amendment landed, since the gate will not |
| R13 | **The milestone cannot close inside CI.** §7's browser pass is unautomatable by decision | 13 | Named as an exit gate, recorded in m4f's archive report, as M1 and M3 did |

---

## 10. Next step

**`m4a-ui-foundation`**: `sdd-spec` and `sdd-design` run in parallel over this proposal once Q1,
Q2 and Q3 are ruled. Design owes §4.1's five decisions with their tests, §4.2's first three rows
with their doc amendments, the exact seam between `httpapi`'s cookie check and `ui`'s handshake
screen, and the signature of §3.3's one new read.

`m4b` can be specified as soon as m4a's design is reviewed; it needs the layout and the middleware
chain, not the Today view. `m4c` waits on m4a's service. `m4d` waits on ADR-0019. `m4e` waits on
m4a and Q5. `m4f` waits on m4b and m4e.
