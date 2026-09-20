# Spec — M4a: UI foundation

Specification for `m4a-ui-foundation`, the first of six slices sharing
`openspec/changes/m4-mirror-ui/proposal.md`. States what MUST be true of the repository after
this change is applied, in testable form. It does not prescribe how (`sdd-design`'s job).

Sources: umbrella proposal §3.2, §3.3, §3.4 (m4a scope only), §4, §5's `m4a` paragraph, §5.1's
seven `m4a` rows, §6, owner rulings Q1–Q3 (2026-09-16); `docs/02-cognitive-core.md` §3, §7;
`docs/01-architecture.md` Layer 2, `## Configuration`; `docs/06-harness.md` §3 ("The UI is L1"),
§4, §6; ADR-0007, ADR-0008, ADR-0018.

## Scope boundary (binding)

> `m4a` is the toolchain, the CI gates, the `ui-boundary` allow-list, the ADR-0007 cookie
> handshake, `--no-ui`/`Config.Server.UI`, and a Today view over eight existing reads plus one new
> `UnitRepo` read. Focus is Priority-only: `focus.Rank` with an empty adjacency map, top-N per
> Kind, no `focus.Select`, no incumbent. Nothing in `m4b`–`m4f` is specified here.

**Not this change**: `/ui/units`, `/ui/capture`, `focus.Select`/hysteresis incumbent, the graph
island, belief edit/delete, activity, admin, timers, cross-origin protection (no non-GET UI
route beyond the handshake POST exists yet — R3 below states the rule for later slices to
inherit, not to exercise it now).

## R1 — `GET /ui` is unauthenticated only on loopback with no token; a token gates every route but the handshake

**MUST**: with `Deps.Token != ""`, every `/ui` and `/ui/*` route except the handshake screen and
the static asset route requires a valid session cookie — a request with no cookie or a wrong one
never reaches vault data. With `Deps.Token == ""` (reachable only on loopback, per
`DecideBinding`), `/ui` stays open exactly as today, with no cookie issued.

**MUST**: `TestOpenRoutesStayOpenRegardlessOfToken`'s `/ui` leg is inverted in the test commit
(strict TDD order 2, umbrella §6): with a token configured and no cookie, `GET /ui` no longer
returns `200` with the placeholder body — it reaches the handshake instead. The `/` leg (API
root) is untouched.

**Scenario: a token is configured, no cookie is presented**
- GIVEN `server.auth_token_env` resolves to a non-empty token
- WHEN `GET /ui` arrives with no `Cookie` header
- THEN the response is the handshake screen, never Today's markup, and no `Set-Cookie` header is
  present on this response

**Scenario: loopback with no token behaves exactly as before**
- GIVEN `Deps.Token == ""`
- WHEN `GET /ui` arrives
- THEN the response is `200` and no cookie is issued or required

**Verified by**: L1 (`httptest`), L4 (`docs/06-harness.md` §3's "L4 gains the UI's own path").

## R2 — The cookie carries the token itself, compared in constant time (Q1)

**MUST**: the handshake's `POST` sets a cookie whose value is `server.auth_token_env`'s token
verbatim — no session id, no session table. `HttpOnly`, `SameSite=Strict`, `Secure` when TLS is
present, no `Max-Age`/`Expires` (session cookie). Verifying a request's cookie against the
configured token uses `subtle.ConstantTimeCompare`, mirroring `requireToken`'s own comparison.
Rotating `server.token`/`auth_token_env` invalidates every existing cookie with no other code
path involved.

**Scenario: a cookie carrying a stale token is rejected after rotation**
- GIVEN a cookie issued against a previously configured token value
- WHEN the configured token changes and a request presents the old cookie
- THEN the request is treated as unauthenticated, identically to a missing cookie

**Verified by**: L1 — cookie flags asserted on the `Set-Cookie` header; constant-time comparison
exercised with a same-length wrong value and a different-length value, both rejected.

## R3 — Cross-origin protection wraps every non-GET UI route, from the handshake onward (Q2)

**MUST**: `internal/httpapi` gains one middleware applying stdlib `http.CrossOriginProtection` to
every non-GET route under `/ui`. In `m4a` this covers exactly the handshake's `POST`; later
slices' mutating routes inherit the same middleware without adding a second mechanism. It applies
identically on loopback with no token — Q2's finding that a foreign origin can still `POST` to a
loopback bind with no cookie in play.

**Scenario: a cross-origin POST to the handshake is refused**
- GIVEN a `POST` to the handshake route carrying a foreign `Origin`/`Sec-Fetch-Site`
- WHEN the request reaches the middleware
- THEN it is refused before the handshake handler runs, on loopback and on a bound host alike

**Verified by**: L1 — a same-origin POST succeeds, a foreign-origin POST is refused, asserted
before any other `/ui/capture`-class route exists (R4 of umbrella §9 risk R4).

## R4 — `--no-ui` and `server.ui: false` leave `/ui` unmounted; the API is unaffected

**MUST**: when the effective `Config.Server.UI` is `false` (flag or config, flag wins when both
are set), `/ui` and every `/ui/*` path are not registered on any mux and fall through to the same
status an unmatched path produces for that token state (404 without a token, 401 with one —
`server.go`'s existing behavior). No API route's registration or behavior changes.

**Scenario: `--no-ui` unmounts the UI without touching the API**
- GIVEN `nooma serve --no-ui`
- WHEN a request reaches `GET /ui`
- THEN the response is the same status an unmatched path produces for that token state (404
  without a token, 401 with one), and `POST /capture` behaves exactly as with the UI mounted

**Verified by**: L1, L4 (`feat/serve-no-ui`, umbrella §5.1).

## R5 — Today shows exactly Q3's three sections, computed from the eight existing reads plus one new one

**MUST**: `/ui` (Today) renders, in one request:
- **FOCUS** — the task focus and the load focus, each `focus.Rank` over `focus.Types(kind)`
  candidates with an **empty** adjacency map, top `focus.DefaultSize` per Kind. No call to
  `focus.Select`.
- **PENDING DIGEST** — `TriggerRepo.Undelivered` items plus the unasked/open `PendingQuestionRepo`
  question, in `prospection.Carry`'s ranked order, read-only.
- **SYSTEM** — `ConfigRepo.Load().ConsolidationLastRunAt` (nil → "never"), `StateRepo.LatestEnergy`
  with its source and instant (nil → "no reading"), `TriggerRepo.Undelivered` count,
  `PendingQuestionRepo` open-question count, the effective bind, and whether `/ui` is behind a
  cookie. No other content.

**MUST**: the one new `UnitRepo` read is a live-by-type-set method returning `[]focus.Candidate`,
bounded by status (excludes `superseded`/`incomplete`, I02) and type rather than by count — the
same unbounded posture `LiveDecayStates` already has. It is the only port change this slice
makes.

**MUST**: all nine reads and every core computation for one Today request use a single
`ports.Clock.Now()` call, passed to every `focus.Rank` and `prospection.Carry` invocation —
umbrella §4.3's rule, so the two focuses agree with each other within one response.

**Scenario: the load focus never contains a task-focus member**
- GIVEN a pool with both `task` and `mental_load` units
- WHEN Today ranks each focus
- THEN the load focus contains only `mental_load` candidates and the task focus only
  `task`/`event` candidates

**Scenario: an empty vault renders SYSTEM with explicit absence, not zeros disguised as data**
- GIVEN a vault with no consolidation run and no energy reading
- WHEN Today renders SYSTEM
- THEN consolidation shows "never" and energy shows "no reading" — not a zero-valued instant

**Verified by**: L1/L2 — `repocontract` case for the new read excluding `superseded`/`incomplete`
before the SQL exists (strict TDD order 3); a fake-clock test proving one `Now()` call feeds every
computation in one request.

## R6 — Viewing Today is not delivering

**MUST**: rendering `/ui` never sets `triggers.surfaced_at` or `pending_questions.asked_at`. The
morning delivery (`prospection.Carry` through the digest channel) is unaffected by any number of
Today requests, including zero.

**Scenario: repeated Today requests leave delivery state untouched**
- GIVEN one undelivered trigger and one open pending question
- WHEN `/ui` is requested three times with no morning digest run in between
- THEN `surfaced_at` and `asked_at` remain `NULL` after all three requests

**Verified by**: L1/L2, asserted before the Today service exists (strict TDD order 4).

## R7 — the templ clean-tree gate and the `ui-boundary` depguard rule are real, structural gates

**MUST**: `templ generate` on a clean checkout leaves a clean tree; `ci.yml` and `make check-all`
both fail when it does not. Generated `*_templ.go` files are committed and marked
`linguist-generated` in `.gitattributes`. `go build ./...` needs no `templ` binary installed.

**MUST**: `.golangci.yml` gains a `ui-boundary` `depguard` rule, an **allow-list** (not a
deny-list, matching `core-purity`/`ports-purity`'s shape): `internal/ui` may import `$gostd`,
`internal/core`, `internal/brain`, `internal/ports`, and the `templ` runtime — nothing else.
`internal/store`, `internal/providers`, `internal/channels`, `internal/scheduler`,
`internal/httpapi`, `internal/config`, and `database/sql` are unreachable from `internal/ui` by
construction. The rule is watched failing (a scratch file importing `internal/store` from
`internal/ui`) before the first template exists — strict TDD order 1, umbrella §6.

**MUST**: security headers (`Content-Security-Policy: default-src 'self'` with no inline
script/style, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`,
`X-Frame-Options: DENY`) are set on every `/ui` response from the first PR that mounts a real
handler, before any view exists.

**Scenario: a deliberate `internal/store` import from `internal/ui` fails lint, not review**
- GIVEN a scratch file in `internal/ui` importing `internal/store`
- WHEN `golangci-lint` runs
- THEN it fails on the `ui-boundary` rule

**Verified by**: L2 (CI job on Linux, per umbrella §9 risk R5 — Windows jobs need no `templ`
because `_templ.go` is committed); the lint rule itself, watched red then green.

## What this spec does not require

Matching the umbrella's `m4a` row (§5) and §3.4: `/ui/units`, `/ui/capture`, `focus.Select` and
the in-process incumbent, `AdjacencyStrengths`, the graph island, belief edit/delete, activity,
admin, timer list/cancel, and any UI mutation beyond the handshake. `relation_to_active_focus`
contributes nothing this slice — the accepted cost of §3.3's ruling, closed by `m4c`.

## Exit criterion

`nooma serve` on a non-loopback bind with a token shows the handshake, then Today lists real
FOCUS/PENDING DIGEST/SYSTEM content behind a cookie with the stated flags; on loopback with no
token there is no screen and no cookie; `--no-ui` unmounts `/ui` with the API unaffected; repeated
Today requests never touch `surfaced_at`/`asked_at`; `templ generate` and `ui-boundary` are CI
gates, both watched failing before passing; `make check-all` is green; no test opens a browser or
touches the network.
