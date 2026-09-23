# Archive report — `m4a-ui-foundation`

**Closed**: 2026-09-23. **Verdict inherited from verify**: PASS WITH WARNINGS — 0 CRITICAL,
1 WARNING (closed before this archive, see below), 1 SUGGESTION.
**`main` at archive**: `0b55793`, the merge of PR #272.
**Milestone**: the first of M4's six slices. `openspec/changes/m4-mirror-ui/proposal.md` stays
active — it covers `m4b` through `m4f` and is not archived with this slice.

## What shipped

`/ui` stopped being a placeholder. The binary serves a real Today — FOCUS per `focus.Kind` with
each member's priority score, PENDING DIGEST as the raw items in `prospection.Carry`'s order with
held items counted rather than listed, and SYSTEM's six lines including the energy reading's
source — behind ADR-0007's cookie handshake when a token is configured, and open on loopback
without one, as it always was.

| PR | Branch | What |
|---|---|---|
| #265 | `feat/ui-toolchain-gates` | `ui-boundary` depguard, templ pinned by `go.mod`'s `tool` directive, the clean-tree gate `ci.yml` had listed as "not enforced yet" |
| #266 | `feat/ui-base-layout` | `internal/ui.Handler`, five exact leaf patterns, security headers, `http.CrossOriginProtection`, htmx vendored, `app.css` per ADR-0018 |
| #267 | `feat/serve-no-ui` | `--no-ui` and `server.ui: false`; `Config.Server.UI`, decoded since M1 and read nowhere, finally consumed |
| #268 | `feat/httpapi-ui-cookie-middleware` | `requireCookie`, the cookie carrying the token, ADR-0028 |
| #269 | `feat/httpapi-ui-login-screen` | the handshake screen, `setUICookie`, the 4 KB body bound |
| #270 | `feat/ports-store-live-focus-by-type` | `UnitRepo.LiveFocusCandidatesByType`; `prospection.EnergyReading` gains `Source` |
| #271 | `feat/brain-today` | `brain.TodayService`, invariant **I27** "viewing is not delivering" |
| #272 | `feat/ui-today-view` | the view, the `<nav>` deferred from PR 2, a nil reader answering 503 |

## What it leaves behind, beyond the feature

Five structural gates that did not exist before, each verified to still discriminate by applying a
mutation at `main` after the last merge:

| Gate | Mutation it was proven against |
|---|---|
| `ui-boundary` depguard | a file in `internal/ui` importing `internal/store` |
| templ clean-tree | a `.templ` edited without regenerating |
| `test/conformance/httpapi_secret_compare_test.go` | `subtle.ConstantTimeCompare` replaced by string equality |
| `test/conformance/httpapi_ui_wiring_test.go` | a guarded leaf registered unwrapped |
| `test/conformance/i27_viewing_is_not_delivering_test.go` | a `TriggerRepo.Surface` call inside the Today render path |

The secret-compare gate is the one worth reading before touching `internal/httpapi`: it does not
blacklist bug shapes, it **whitelists the permitted handler shape**. Changing `requireCookie` or
`requireToken` means changing that template in the same commit, deliberately. That inversion was
the outcome of five review rounds, and it is recorded in design §3.2–§3.3 with the four exploits
that defeated every earlier attempt.

## Cost, stated plainly

PR 4a took 18 commits and five Judgment Day rounds. The property it defends — that "missing",
"wrong" and "malformed" are indistinguishable in *time and code path*, not only in bytes — is
invisible in the HTTP response, so no response-level test can see a regression, and each AST gate
written to catch the shape it was shown missed the adjacent one. Rounds 1–4 each closed one shape
and left another open; round 5 inverted the approach. PRs 5, 6 and 7 each closed in about one and
a half rounds, on the gates 4a left standing.

Budgets: seven of eight links landed under their estimate. PR 4a came in at 253 impl+docs against
~220 and PR 6 at 303 against ~260 — both over, both under the 400 ceiling, both disclosed in their
PR bodies rather than folded away. The overage in each case is gate code and corrected doc
comments, not feature behaviour.

## The warning verify raised, and its fix

`docs/06-harness.md` still described `internal/ui/` as "a `doc.go` and nothing else" inside the
graph-island gates' precondition — true when written, false from PR 2 onward. Nothing caught it:
`docs-sync.yml` fires on `internal/core/**` only, so a doc describing any other package drifts
freely. The design named this as risk R12 and it landed exactly there. Closed by a separate
docs-only PR before this archive.

## Carried forward

Design §11's owner-review items **OR1–OR7** and the proposal's **Q4** (may `m4e` precede `m4d`),
**Q5** (`/ui/admin`'s writable set) and **Q6** (cross-origin on the API's own POST routes) all
remain open with a stated default, recorded in `design.md` §11 and `tasks.md`'s own carried-forward
table. None blocked this slice.

`m4d-graph` cannot start until **ADR-0019 moves from `Proposed` to `Accepted`** — it needs the
bundle's hand audit and a measured render budget first.
