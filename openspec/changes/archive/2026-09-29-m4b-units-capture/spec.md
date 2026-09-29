# Spec — M4b: units and capture

Specification for `m4b-units-capture`, the second of six slices sharing
`openspec/changes/m4-mirror-ui/proposal.md`. States what MUST be true of the repository after
this change is applied, in testable form. It does not prescribe how (`sdd-design`'s job).

Sources: umbrella proposal §3.2, §3.4 (m4b-relevant non-goals), §3.5 (I22, I02, I18), §4, §5's
`m4b` paragraph, §5.1's six `m4b` rows, §6 order 5, Q2's ruling; `docs/02-cognitive-core.md` §1,
§5 step 4; `docs/06-harness.md` §3 ("The UI is L1"); archived `m4a-ui-foundation` spec (R1-R7) —
this change builds on its handshake, its `ui-boundary` gate and its cross-origin middleware,
none of which it re-specifies.

## Scope boundary (binding)

> `m4b` is `/ui/units` (a live-unit browse read with type filters and keyset pagination, a
> search entrance through `brain.RecallService`, and a unit detail page) and `/ui/capture` (a
> capture form calling `brain.CaptureService.Capture`, and a correction form carrying an
> explicit referent from the detail page). It adds one `ports.UnitRepo` browse method. Depends
> on `m4a`: the cookie handshake, the open/guarded mux split, and the cross-origin middleware
> already wrap every route this change registers inside `newUIMux` — none of that is rebuilt
> here.

**Not this change**: `focus.Select`/the hysteresis incumbent, `AdjacencyStrengths`, the graph
island, belief edit/delete, activity, admin, timer list/cancel, and any new authentication
mechanism. The browse read never returns `archived` or `superseded` units — a future slice's
job, not this one's, per the umbrella's non-goal on undo/second surfaces (§3.4).

## R1 — `/ui/units` lists live units through a new, bounded, filterable, paginated read

**MUST**: `ports.UnitRepo` gains one browse method returning `unit.StatusPool` units only —
excluding `archived`, `superseded` and `incomplete` (I02), matching `LiveDecayStates` and
`LiveFocusCandidatesByType`'s existing "Live" naming convention. It accepts a `unit.Type` filter
set (empty set means all types) and is bounded: a page size limit and a stable ordering that
supports requesting the next page without re-scanning what a prior page already returned.

**MUST**: `/ui/units` renders one page of results as an htmx fragment, each row identified by
its unit id, filterable by `unit.Type`, and pageable via the read's own boundedness — no
unbounded "load everything" path exists at any layer between the SQL and the response.

**Scenario: an archived unit never appears in browse**
- GIVEN a vault with one `pool` unit and one `archived` unit of the same type
- WHEN `/ui/units` is requested with no filter
- THEN only the `pool` unit appears in the response

**Scenario: a type filter narrows the page without changing total live count**
- GIVEN a vault with `task` and `knowledge` pool units
- WHEN `/ui/units` is requested with `type=task`
- THEN only `task` units appear, and a request with `type=knowledge` returns only those

**Scenario: a second page never repeats a row from the first**
- GIVEN more pool units of one type than the page size
- WHEN the first page is requested, then the next page using the value the first page returned
- THEN no unit id appears on both pages

**Verified by**: L1/L2 — `repocontract` case for the new read excluding `archived`,
`superseded` and `incomplete` before the SQL exists; L1 fragment assertions on unit ids and
`hx-get` pagination links (`docs/06-harness.md` §3).

## R2 — Browse search is one recall mechanism, never a second lexical entrance (I22)

**MUST**: `/ui/units`'s search box reaches `brain.RecallService` — the same entrance `/recall`
and chat's `recall` classification use — and never calls `ports.LexicalSearch` or
`ports.EmbeddingProvider` directly from `internal/ui` or from a browse-specific service path.
The query text is embedded and compared exactly as any other recall query is (ADR-0020's
admission floor applies identically); a browse search is not a permissive, un-admitted variant
of recall.

**Scenario: a browse search below the admission floor returns no results, not a raw net**
- GIVEN a search query with no live unit close enough to clear `recall_min_similarity`
- WHEN `/ui/units` is searched with that query
- THEN the response shows no results — the same outcome `/recall` gives the identical query

**Verified by**: a `go/ast` or fake-counting test that fails if `ports.LexicalSearch` is called
from the browse path outside `RecallService` (umbrella §6 order 5, strict TDD — this test is
watched failing before the browse search handler exists).

## R3 — The unit detail page renders one unit, its relations, and its three dates distinctly (I18)

**MUST**: `/ui/units/{id}` (or equivalent single-unit path) renders one live unit's content,
type, its stored weight — labelled as "stored weight", never an effective or decayed weight,
since no reusable effective-weight value exists for an arbitrary unit outside a focus list (Today
exposes only a composite focus `Score`, not a per-unit effective weight) — its relations via
`ports.RelationRepo.ByUnit`, and `CreatedAt`, `EventAt` and `DueAt` each under its own distinct
label — never merged into one "date" field and never one substituted for another when a value
is `nil`. A `nil` `EventAt`/`DueAt` renders as explicit absence, not an empty or zero-valued
date.

**MUST**: a non-live or unknown id (archived, superseded, incomplete, or absent) answers the
same 404-class response `GET /units/{id}` already gives, through `ports.UnitRepo.LiveByIDs`'s
existing positive live filter — the same filter `RecallService.LiveByIDs` passes through to
unchanged — I02 enforced once, not reimplemented in `internal/ui`. The detail read calls
`UnitRepo.LiveByIDs` directly rather than going through `RecallService`, so the detail page
still works on a providerless vault, where `wireBrain` returns a nil `RecallService` and browsing
needs no provider.

**Scenario: a unit with only a due date never shows an event label**
- GIVEN a `task` unit with `DueAt` set and `EventAt` nil
- WHEN its detail page renders
- THEN the due date appears under its own label and no value appears under the event label

**Scenario: an archived unit's detail page is not reachable**
- GIVEN a unit whose status is `archived`
- WHEN its id is requested at the detail path
- THEN the response is the same not-found response class the browse list and `GET /units/{id}`
  already give a non-live id

**Scenario: a relation to a non-live unit is omitted from a live unit's detail page**
- GIVEN a live unit related to one live unit and one `archived` unit
- WHEN its detail page renders
- THEN only the relation to the live unit appears, filtered by the same positive live filter

**Verified by**: L1 — a test asserting the three date labels never swap when only one field is
present (I18's own UI failure mode, `today_test.go`'s `TestTodayView_I18DatesLabelled` precedent
applied to a single unit instead of a focus list).

## R4 — `/ui/capture` calls `CaptureService.Capture` unchanged, with the UI as one more caller

**MUST**: the capture form's `POST` builds a `brain.CaptureInput` from the submitted text and
calls `CaptureService.Capture` exactly as `POST /capture` already does — no reimplementation of
classification, dedup, or hook-arming logic inside `internal/ui` or a UI-specific service. A
successful capture's response reflects what `CaptureResult` already reports (the unit created,
or the correction applied); the UI invents no field `CaptureResult` does not carry.

**Scenario: a capture through the UI produces the same result shape the API produces**
- GIVEN identical input text submitted once through `POST /capture` and once through
  `/ui/capture`
- WHEN both requests complete
- THEN both persist a unit via the same `CaptureService.Capture` call path, with no UI-side
  branch that classification, dedup or hooks did not already decide

**Verified by**: L1 — a fake-counting or call-path test proving `/ui/capture`'s handler invokes
`CaptureService.Capture` and nothing else that could produce a persisted effect.

## R5 — A correction submitted from the unit detail page carries an explicit referent

**MUST**: the correction form reachable from `/ui/units/{id}` sets `CaptureInput.ReferentID` to
that unit's id before calling `Capture` — the explicit-referent path doc 02 §5 step 4 already
defines for "a caller holding an identifier": `resolveReferent`'s `in.ReferentID != ""` branch,
never the hybrid-recall/ambiguity-gate branch chat uses when it holds no identifier. The
pre-image `correction.applied` row this produces is unchanged by this change — its shape and
`referent.source: "explicit"` come from existing `internal/brain/correction.go` code, not new
code this slice writes.

**Scenario: a correction from the detail page never triggers the recall-ambiguity ask**
- GIVEN a unit open on its detail page and a correction submitted from that page
- WHEN the correction is classified and captured
- THEN `ReferentID` is non-empty on the call, and the correction never reaches the
  scored-candidate margin gate chat's referent-less path uses

**Verified by**: L1/L2 — asserts `ReferentID` is populated on every call originating from the
detail page's correction form; existing `correction_test.go` coverage of the explicit-referent
branch is unchanged, not re-derived.

## R6 — Cross-origin protection, wired generically by `m4a`, now protects a real mutating surface

**MUST**: `/ui/capture`'s `POST` and the correction form's `POST` are registered inside the same
`newUIMux` the handshake's `POST` already registers inside, so `httpapi.Handler`'s existing
`http.CrossOriginProtection` wrap — applied once, outside any per-route code — refuses a
cross-origin request to either route before its handler runs, with no new middleware written
for this change. This is the first slice exercising that inherited protection against a route
that can persist vault data (umbrella §9 risk R4; Q2's ruling).

**Scenario: a foreign-origin POST to the capture route never reaches CaptureService**
- GIVEN a `POST` to `/ui/capture` carrying a foreign `Origin`/`Sec-Fetch-Site`
- WHEN the request reaches `httpapi.Handler`
- THEN it is refused before `CaptureService.Capture` is called, and no unit is persisted

**Scenario: a foreign-origin POST to the correction route is refused identically**
- GIVEN a `POST` to the correction route on `/ui/units/{id}` carrying a foreign origin
- WHEN the request reaches `httpapi.Handler`
- THEN it is refused before `resolveReferent` runs, and the referenced unit is unchanged

**Verified by**: L1 — same-origin succeeds, foreign-origin is refused, for both new mutating
routes specifically (m4a's own test covered only the handshake `POST`; this is the first test
of the inherited wrap against a route with a persistence effect).

## What this spec does not require

Matching the umbrella's `m4b` row (§5) and §3.4: `focus.Select`, the hysteresis incumbent,
`AdjacencyStrengths`, the graph island and its vendored bundle, belief edit/delete, activity,
admin, timer list/cancel, undo of a correction, and any second authentication mechanism. No new
cross-origin middleware is written — R6 verifies inherited behavior, it does not add code to
produce it. `relation_to_active_focus` and focus jitter remain `m4c`'s to close.

## Open questions

**OQ1 — Does the browse read need a "status" filter beyond type?** The umbrella's PR table
(§5.1, `feat/ports-store-units-browse`) says "type/status filters", but `unit.Status` has one
live value (`pool`) — R1 already excludes the other three by construction (I02), so a status
axis on top of that would only ever be "pool", a no-op filter. **Default: type filter only; no
status parameter.** If a future slice needs to browse non-live units (e.g. an archived-units
view), that is a new, explicitly-scoped read, not a widening of this one — matching
`ports.UnitRepo`'s own "no `List(status)` parameterized read" rule (`unitrepo.go`'s package
doc comment).

**OQ2 — Is `feat/httpapi-cross-origin` (umbrella §5.1, listed under `m4b`) still a real PR?**
Verified against `internal/httpapi/server.go:116-121`: `m4a` already wraps the entire `/ui`
subtree in `http.CrossOriginProtection`, generically, before any leaf route exists — R6 above
reflects that. **Default: no new middleware PR; `m4b`'s budget for that row becomes the tests
R6 requires**, folded into the PRs that add the capture and correction routes rather than a
standalone PR. This is a discrepancy between the proposal's PR-table guess (written before
`m4a`'s design finalized the wrap's scope) and what `m4a` actually shipped, not a scope
decision this spec is making unilaterally — `sdd-tasks`/`sdd-design` should confirm the PR
count accordingly.

## Exit criterion

`/ui/units` lists only live units, filterable by type and paginated with no unbounded read at
any layer; its search reaches no mechanism other than `RecallService`; a unit's detail page
renders its relations and its three dates under distinct, never-swapped labels, and 404s on a
non-live id; `/ui/capture` and the detail page's correction form both call
`CaptureService.Capture` unchanged, the latter with an explicit `ReferentID`; a foreign-origin
`POST` to either mutating route is refused before any vault effect occurs; `make check-all` is
green; no test opens a browser or touches the network.
