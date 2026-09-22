# Tasks — m4a: the UI foundation (toolchain, boundary, handshake, Today)

Implementation task list for `m4a-ui-foundation`, derived from `spec.md` (R1–R7, read in full)
and `design.md` (§1–§12, read in full, verified against the tree at `4e3115c` — design's own §1
ground-truth table), the first of the six chained changes `openspec/changes/m4-mirror-ui/proposal.md`
splits M4 into. Design §7 fixes the slicing — **eight PRs** (PR 4 splits into 4a/4b per design's
own overflow rule, item 4 of its opening list), ~1,705 budgeted impl+docs lines — treated as
authoritative over any disagreement with spec's own wording, per `m3e`'s own precedent ("design
owns the slicing"). Design.md is **APPROVED after seven Judgment Day rounds**; it is not
re-derived here, only sliced into checkable tasks.

**Inputs**: `spec.md` (R1–R7, this change's own scope boundary); `design.md` §1–§12 (verified
`4e3115c`); `openspec/changes/m4-mirror-ui/proposal.md` §5–§6, §8 (owner rulings Q1–Q3, Q7
ruled 2026-09-16).

**Delivery parameters** (cached this session): delivery `auto-chain` (design §7's own "Chain
`stacked-to-main`, delivery `auto-chain` (proposal §5)"), chain strategy **`stacked-to-main`** —
every branch targets `main` directly and merges in order; the next branch rebases on `main` after
the previous merge. Soft ceiling 400 impl+docs lines per PR (tests, generated `_templ.go`,
`go.sum` and `htmx.min.js` counted and reported separately, never against the ceiling —
`docs/06-harness.md` §7, design §7's own header). Strict TDD is active: every behavioral task
states its RED commit strictly ahead of its GREEN commit inside the same PR; no PR tip is
deliberately red — `main`'s ruleset has no bypass. `make check` between commits; `make check-all`
before opening each PR, run **at that PR's own commit in an isolated worktree**
(`git worktree add`) — a branch tip being green says nothing about whether the combined tree up to
that point still builds (m3e's own "verify the link, not the tip" lesson). Branch names are
design's exact `feat/...` names. Conventional commits, no AI-attribution trailer of any kind.

**The eight branches, order 1 → 2 → 3 → 4a → 4b → 5 → 6 → 7** (design §7's own fixed order):

| # | Branch | Depends on |
|---|---|---|
| 1 | `feat/ui-toolchain-gates` | nothing |
| 2 | `feat/ui-base-layout` | 1 |
| 3 | `feat/serve-no-ui` | 2 |
| 4a | `feat/httpapi-ui-cookie-middleware` | 2 (registers over PR 2's leaves) |
| 4b | `feat/httpapi-ui-login-screen` | 4a |
| 5 | `feat/ports-store-live-focus-by-type` | nothing beyond `main` (touches `ports`/`store`/`core` only) |
| 6 | `feat/brain-today` | 5 |
| 7 | `feat/ui-today-view` | 6, and 4b (renders behind the cookie) |

**PR 4a must land before 5, 6 and 7** — spec R1's own MUST: no vault data reaches `/ui` before the
cookie check exists. PR 3 could land anywhere after 2 and is placed early so `--no-ui` exists
before the first data route. Against this project's own measured range (1.3×–2.2× six times,
4.3× once, proposal §5.1), PR 2 and PR 6 are the ones to watch most closely — though every PR
below carries a named overflow cut, not only those two.

---

## Cross-PR items (named once, tasked at their PR)

- **`ui-boundary` depguard rule watched failing before passing** — PR 1, tasks 1.1–1.2.
- **`templ` clean-tree gate watched failing before passing** — PR 1, tasks 1.3–1.4.
- **`docs-sync` fires on PR 5** (it touches `internal/core/prospection/digest.go`), and PR 5
  carries its one doc 02 §7 sentence in the same commit as the code — task 5.5, no
  `no-spec-change` label claimed or needed.
- **I27** (`viewing is not delivering`), the invariant this milestone is named for, gets its
  conformance test and its `docs/06-harness.md` §4 row in PR 6 — tasks 6.1, 6.9 (OR7's decided
  default).

---

## PR 1 — `feat/ui-toolchain-gates` (~190 impl+docs; risk: Low)

**Overflow cut** (if over 400): trim `layout.templ`'s nav markup to one `<main>` slot — nothing
exists yet for a nav to link to; the gate needs one committed template, not a finished shell.

- [x] **1.1** RED — `.golangci.yml`: add the `ui-boundary` `depguard` rule, an **allow-list**
      (design §3.1's exact block: `$gostd`, `internal/core`, `internal/brain`, `internal/ports`,
      `internal/ui`, `github.com/a-h/templ`; four named `deny` entries whose message a
      contributor needs first). Add a scratch `internal/ui/probe.go` importing `internal/store`.
      Run `golangci-lint run` and record the failure in the PR body — the rule has a file to fail
      on before the first template exists (design §3.8 "watching the boundary rule fail",
      proposal §6 order 1).
      Requirement: R7.
- [x] **1.2** GREEN — delete `internal/ui/probe.go`. `golangci-lint run` passes with
      `internal/ui/doc.go` as the package's only file.
      Verify: `golangci-lint run ./internal/ui/...`.
      Requirement: R7; design §3.1, §3.8.
- [x] **1.3** RED — add `internal/ui/layout.templ` (`Page(title, body)`, `<meta name="htmx-config"
      content='{"allowEval":false,"includeIndicatorStyles":false}'>` per §3.5, stylesheet
      `<link>`, `<script>` tag); `Makefile` gains `templ` (`go tool templ generate -path
      ./internal/ui`) and `templ-clean` (`templ` then `git diff --exit-code -- 'internal/ui/*_templ.go'`,
      `schema-golden-clean`'s own shape) targets joining `check-all`, not `check`;
      `.github/workflows/ci.yml`'s `build` job gains the clean-tree step, `ci.yml:125-127`'s
      "not enforced" comment about to be deleted. Hand-edit the generated `layout_templ.go` and
      run `make templ-clean`; record the failure in the PR body (design §3.8 "watching the gate
      fail", proposal §6 order 1).
      Requirement: R7.
- [x] **1.4** GREEN — regenerate `layout_templ.go` via `make templ`; `make templ-clean` passes;
      delete `ci.yml:125-127`'s "not enforced" line for real. `go.mod`/`go.sum`: `require
      github.com/a-h/templ`; `tool github.com/a-h/templ/cmd/templ` (design §3.8's pinning
      choice — one file, one version, for the binary and the runtime). `.gitattributes`:
      `internal/ui/*_templ.go linguist-generated=true`, `internal/ui/static/htmx.min.js
      linguist-vendored=true` (the second file lands PR 2; the attribute line can precede it).
      Verify: `go build ./...` needs no `templ` binary; the seven-target cross-compile matrix is
      untouched (generated `_templ.go` is plain Go, `templ`'s own runtime has no cgo).
      Requirement: R7; design §3.8.
- [x] Verify (PR-level, local part) — confirmed at tip `5e2aab1` (the last code commit; its tree hash `c242e65` is what `make check-all` ran against): `GET /ui` is untouched, no
      `internal/httpapi`/`test/e2e` file changed (`git diff --stat 8a7232e..HEAD -- internal/httpapi
      test/e2e` is empty); no test modified (no `_test.go` in the diff);
      `TestHandlerServesBothSurfaces`, `TestOpenRoutesStayOpenRegardlessOfToken` and
      `TestServeAnswersBothSurfaces` all stay as they are. `make check-all` green in an isolated
      worktree at this tip (lint, L1/L2, build, L3, `schema-golden-clean`, coverage 99%, the
      seven-target matrix, L4, `templ-clean`). Impl+docs measured at 97 lines (well under ≤190;
      `go.sum` 41 and `layout_templ.go` 78 reported beside it, not inside).
      **Still open** (out of this apply batch's scope, per the executing agent's own
      instructions): opening `feat/ui-toolchain-gates` against `main`, waiting for the 17
      required contexts, merging only on `mergeStateStatus: CLEAN`, and confirming
      `git ls-remote --heads origin feat/ui-toolchain-gates` returns nothing before branching
      PR 2.

**Deviations** (recorded, not silent):
- Task 1.2's GREEN commit also updated `internal/ui/doc.go`'s charter sentence to "it renders
  view models, it decides nothing" (design §3.1's tree; §7.1's landing-order table attributes it
  to PR 1, "inferred — §3.1's tree carries no explicit PR tag for this line"). Not itemized as
  its own bullet in this file's 1.x list; folded into 1.2 since it touches the same file the task
  already names as "the package's only file" and carries zero risk.
- Task 1.3's RED commit, as first committed, left out `go.mod`'s `tool`/`require` directive
  (design's own table places that in the GREEN commit, task 1.4), reproducing the recorded
  `templ-clean` diff failure by staging the tool directive locally — never committed at that
  point — to make `go tool templ` resolvable, then reverting `go.mod`/`go.sum` before committing.
  A judgment-day review of the committed tree (not the staged state that produced the evidence)
  found that tree fails earlier, at `go: no such tool "templ"`, because the pin does not exist in
  that commit — not at the `templ-clean` gate's own `git diff --exit-code` step the RED commit's
  body claims to have watched. Strict TDD (nooma-testing skill, rule 3) requires watching red for
  the right reason from the committed tree, not from an uncommitted staged state, so PR 1's task
  1.3/1.4 commits were rebuilt: the `tool`/`require` pin (and `.gitattributes`'s
  generated/vendored lines, which describe the same toolchain) now land in the RED commit
  alongside the stale hand-edited `layout_templ.go`, deviating from design's task split (pin in
  GREEN, task 1.4) for that reason. The rebuilt RED commit was verified failing at the
  `templ-clean` diff step from an isolated worktree at that exact commit, with `go build ./...`
  succeeding there since the stale file is still valid Go; the GREEN commit is now a pure
  regeneration of `layout_templ.go` with no toolchain change. The resulting tree at PR 1's tip is
  byte-identical to the tree this deviation note originally described (verified by tree hash).
- Design §3.1's prose and task 1.1 both say the `ui-boundary` `depguard` rule has "four" named
  `deny` entries; the YAML block in both design §3.1 and the implemented `.golangci.yml` has six
  (`internal/store`, `internal/httpapi`, `internal/config`, `database/sql`, `os`, `os/exec`).
  Implemented the YAML, which is the executable artifact and predates this apply; the prose count
  is stale in both documents and is left for correction at the next docs touch rather than edited
  here, out of scope for a PR 1 apply.

---

## PR 2 — `feat/ui-base-layout` (~350 impl+docs; risk: High — closest PR to the ceiling, watch closely)

**Overflow cut** (if over 400): `app.css` beyond the token layer moves to PR 7, landing beside the
first view that actually uses it.

- [x] **2.1** RED — `internal/httpapi`: `TestUISubtreeSetsSecurityHeaders`,
      `TestUICrossOriginPostIsRefused`, `TestUIStaticServesStylesheetAndHtmx`,
      `TestUIRootIsNeverRedirectedByTheMux` (new, §8).
      Mutations these catch: a header dropped from one response arm, or a handler writing its own
      `Cache-Control`, or the chain wrapped in the wrong order so a `403` escapes with no headers
      (`TestUISubtreeSetsSecurityHeaders`); the cross-origin wrap removed, placed inside
      `requireCookie` instead of outside it, or a bypass pattern added
      (`TestUICrossOriginPostIsRefused`); wrong content-type or truncated/swapped embedded bytes,
      caught by driving `newUIMux`'s three registered leaves through `Handler(d)`
      (`TestUIStaticServesStylesheetAndHtmx`) — this test does **not** catch a directory listing
      served by `Assets()` itself, because `newUIMux`'s leaves never route a bare `/ui/static` or
      `/ui/static/` into `Assets()` at all; that construction is pinned separately, directly
      against `Assets()`, by `internal/ui/assets_test.go`'s
      `TestAssetsServesTheThreeEmbeddedLeaves` (mutation: reverting `Assets()` to
      `http.StripPrefix("/ui/static/", http.FileServerFS(sub))` — the `{file...}`/`FileServerFS`
      defect §3.2 corrected — stays green here but fails there, confirmed by mutation); registering
      `/ui/` as a subtree pattern instead of the exact leaf `GET /ui/{$}` — the class that let `GET
      /ui` `307`-redirect from the mux itself, before `requireCookie` or any handler ran
      (`TestUIRootIsNeverRedirectedByTheMux`). Two cases added at review: a same-origin `POST /ui`
      answers `405` with `Allow: GET, HEAD` and every security header — catches a header
      middleware placed inside the mux instead of around it (`TestUISubtreeSetsSecurityHeaders`);
      and `Handler(d)` end-to-end serves `GET /ui` and `GET /ui/` with no `Location` — catches
      the outer mount registering only `/ui/` (`TestUIRootIsNeverRedirectedByTheMux`, second
      subtest; confirmed by mutation, `307 Location: /ui/`).
      Requirement: R1, R3 (spec's cross-origin/handshake scope), R7.
- [x] **2.2** RED — rename `TestHandlerServesBothSurfaces` → `TestHandlerServesAPIRootAndUIShell`,
      given a `Deps.UI` fixture: `GET /` and `GET /ui` both `200`; the `/ui` body carries the
      layout (headers) and PR 2's own shell paragraph — *"Today arrives in a
      later PR"* — never a FOCUS/PENDING DIGEST/SYSTEM section or any other vault-shaped content
      (§3.1's PR 2 shell state; this test's own scope is PR 2 through PR 6 only, per §7.2 — it is
      superseded at PR 7 by `TestTodayView_NilTodayReaderAnswers503`, task 7.3).
      Mutation: a shell that leaks Today's own markup before `TodayReader` exists; a shell missing
      the layout or the five security headers.
      Requirement: R5 (the shell is not Today yet); design §3.1, §7.2, §8.
- [x] **2.3** GREEN — `internal/ui/ui.go` (`Deps{}`, `Serving{}`, `New`, `(*Handler).ServeHTTP`
      rendering `layout.Page` with an empty `<main>` and the shell paragraph, no SYSTEM section —
      §3.1's PR 2 shell state); `internal/ui/assets.go` (`Assets()` — a small `*http.ServeMux`
      carrying exactly three exact leaf patterns over `http.ServeFileFS`, no wildcard, §3.2's
      correction); `internal/ui/static/app.css` (ADR-0018's six layers, `light-dark()`, system
      font stack); vendor `htmx.min.js` + `htmx.LICENSE` beside it, with the version + SHA-256 of
      the release asset recorded in a comment at the top of `assets.go` (§3.8's vendoring note;
      no `fetch`/`XMLHttpRequest`/`WebSocket`/`eval` scan recorded, not asserted, since htmx uses
      `XMLHttpRequest` by design and is not the graph island).
      Verify: `go test ./internal/ui/...`.
      Requirement: R5, R7; design §3.1, §3.8.
- [x] **2.4** GREEN — `internal/httpapi/headers.go` (`securityHeaders`: the CSP string, `nosniff`,
      `same-origin` referrer, `X-Frame-Options: DENY`, `Cache-Control: no-store` on views /
      `no-cache` on `/ui/static/*`, §3.5's exact table); the cross-origin wrap
      (`http.NewCrossOriginProtection()`, constructed once, no trusted origins, no bypass
      patterns) inside `Handler`, wrapping the whole `/ui` subtree — headers outermost, then
      cross-origin, then (from PR 4a) the cookie check (§3.2's ordering rule).
      Verify: `go test ./internal/httpapi/...`.
      Requirement: R3, R7; design §3.4, §3.5.
- [x] **2.5** GREEN — `internal/httpapi/server.go`: `Deps.UI *ui.Handler`; the `uiMux` with its
      five leaf patterns from this PR (`GET /ui`, `GET /ui/{$}`, `GET /ui/static/app.css`, `GET
      /ui/static/htmx.min.js`, `GET /ui/static/htmx.LICENSE` — none subtree-shaped; the two
      `/ui/login` leaves land PR 4b, once their handlers exist); mount the subtree on the open mux
      only when `d.UI != nil`; delete `uiPlaceholder`.
      Verify: `go test ./internal/httpapi/...`.
      Requirement: R1, R4; design §3.2.
- [x] **2.6** GREEN — `cmd/nooma/serve.go`: wire `ui.New(ui.Deps{})` into `Deps.UI`
      **unconditionally**, no flag and no conditional around it yet (§3.9's "landing this across
      PR 2 and PR 3" correction — this is the call PR 3 later wraps, not a call PR 3
      introduces). Without this, `d.UI` is nil in the compiled binary and `TestServeAnswersBothSurfaces`
      (e2e) fails.
      Verify: `go test ./test/e2e/... -run TestServeAnswersBothSurfaces`.
      Requirement: R4; design §3.9.
- [x] **2.7** GREEN, fixture-only, no assertion change — `TestHandlerServesDistinctSurfaces` and
      `TestOpenRoutesStayOpenRegardlessOfToken`'s existing `Deps` fixtures gain `UI` so `/ui`
      stays reachable once `d.UI != nil` gates the mount (§7.2's own note that this is plumbing,
      not a new claim).
      Requirement: design §7's PR 2 row.
- [x] Verify (PR-level) — confirmed at tip `25cfd5b` (RED `9987de5`, GREEN `25cfd5b`, branched
      from `feat/ui-toolchain-gates`'s merged tip `79d05e8`): `GET /ui` at this tip is `200`, PR
      2's shell (*"Today arrives in a later PR"*, five security headers, no FOCUS/PENDING
      DIGEST/SYSTEM), **with or without a token** — `requireCookie` does not exist until 4a, so a
      configured token changes nothing here (§7.2's PR 2 row). Tests modified for the tip to stay
      green: `TestHandlerServesBothSurfaces` renamed and rewritten (task 2.2);
      `TestHandlerServesDistinctSurfaces` and `TestOpenRoutesStayOpenRegardlessOfToken` gain a
      `UI` fixture field (task 2.7) with no assertion change. `test/e2e/serve_test.go`'s
      `TestServeAnswersBothSurfaces` is **not** modified — task 2.6's wiring alone keeps it green
      (confirmed green at this tip). `make check-all` green in an isolated worktree at this tip
      (lint 0 issues, `go vet`, L1/L2, build, L3, `schema-golden-clean`, `internal/core` coverage
      99% (990/992, unchanged — PR 2 touches no `internal/core` file), the seven-target
      cross-compile matrix all OK, L4 e2e 141s, `templ-clean` clean). Impl+docs measured at 286
      lines added / 32 deleted (well under ≤400, and under design's own ~350 estimate — no
      overflow cut needed); `shell_templ.go` (45), `htmx.min.js` (1, ~51 KB) and `htmx.LICENSE`
      (13) reported beside it, not inside; `internal/httpapi/server_test.go` (287 added / 35
      deleted) reported separately as test lines; no `go.sum` change (no new Go module — htmx is
      vendored as a static asset, not a dependency). htmx vendored at v2.0.10 (current 2.x stable
      tag), fetched once from the tagged GitHub raw content, byte sizes (51238, 642) verified
      against the tagged release's own asset sizes; SHA-256 recorded in the GREEN commit body.
      **Still open** (out of this apply batch's scope, per the executing agent's own
      instructions): opening `feat/ui-base-layout` against `main` (rebased on PR 1's merged tip),
      waiting for required contexts, merging only on `mergeStateStatus: CLEAN`, and confirming
      `git ls-remote --heads origin feat/ui-base-layout` returns nothing before branching PR 3.

**Deviations** (recorded, not silent):
- Task 2.1's mutation description and design §8's own test-catalogue entry for
  `TestUICrossOriginPostIsRefused` are both written against `POST /ui/login` — a route that does
  not exist until PR 4b (`loginPage`/`loginSubmit` are PR 4b's GREEN, design §3.2, §7's PR 4b
  row). Implemented the test against `POST /ui` instead: `http.CrossOriginProtection` wraps the
  *whole* `/ui` subtree before any inner-mux routing happens (design §3.4 — "wraps the whole UI
  subtree ... a route added later cannot forget it"), so the refusal fires identically for any
  method+path pair under `/ui`, whether or not that specific pattern is registered yet. Using
  `/ui` — a leaf that does exist at this tip — proves the same middleware behavior design §8
  describes without waiting for PR 4b's routes; the design's own route name is stale for this
  PR's own RED commit, corrected here rather than left unimplementable.
- Task 2.3's own text says "no `fetch`/`XMLHttpRequest`/`WebSocket`/`eval` scan recorded, not
  asserted" (matching design §3.8's audit note), but design §3.8's own prose the task paraphrases
  actually says the scan result *is* "recorded, not asserted" — i.e., the scan is run and its
  result written down, not skipped. Ran the scan (`rg` over the vendored `htmx.min.js`) and
  recorded its result in the GREEN commit body: `XMLHttpRequest` (2 matches) and `eval(` (1
  match) present, by design (htmx uses `XMLHttpRequest`; `allowEval:false` in
  `layout.templ`'s own `htmx-config` meta neutralizes the `eval` path); no `fetch(` or
  `WebSocket` found. Implemented per design §3.8's actual meaning, not per the task's own
  slightly compressed restatement of it.
- Task 2.5 names `uiMux` as the identifier holding the five leaf patterns; implemented as
  `newUIMux(d Deps) *http.ServeMux`, a constructor function rather than a bare variable, so
  `TestUIRootIsNeverRedirectedByTheMux` (task 2.1) can call it directly and inspect
  `mux.Handler(r)` without needing package-private access to a value scoped inside `Handler`'s
  own body. Same mux, same five patterns, same behavior — a naming/shape choice for
  testability, not a deviation from §3.2's routing design.
- Design §7's PR 1 row and §8's testing-strategy row describe PR 2's shell body as carrying "the
  layout (nav, headers)". `layout.templ` ships no `<nav>` in this PR, and neither task 2.2's own
  test nor any other PR 2 test asserts one. Deferred `<nav>` to PR 7 (`feat/ui-today-view`):
  design §7's PR 1 row already conditions it on "once a second route exists", and PR 2 through
  PR 6 render only the one shell route (§7.2's tip table) — Today, PR 7's own page, is the first
  route a nav would actually link to. Added a note to PR 7's own task list (below) so the nav
  lands there instead of silently staying missing.

---

## PR 3 — `feat/serve-no-ui` (~110 impl+docs; risk: Low)

**Overflow cut**: none named — this PR wraps an existing call rather than adding one, and design's
own count is unchanged from the proposal's estimate.

- [x] **3.1** RED — `internal/httpapi`: `TestNoUIUnmountsTheSubtree` — `Deps.UI == nil`: `/ui` and
      `/ui/login` answer exactly as `/does-not-exist` does under the same token state (`404`
      without a token, `401` with one).
      Mutation: a stub "UI is off" page mounted instead of leaving the pattern unregistered; a
      `404` returned when the API's own posture for that token state would be `401`.
      Requirement: R4.
      **Reframed at apply time (recorded under Deviations below): this test is already true on
      PR 2's tree — probed before any PR 3 code change (`go test -run
      TestNoUIUnmountsTheSubtreeProbe`), confirmed 404/404/404 with no token and
      401/401/401 with one, matching `/does-not-exist` exactly. Committed as a pinning test in
      the GREEN commit, not as its own RED — writing it as RED here would have meant claiming a
      failure that does not exist, which this project's own strict-TDD rule forbids.**
- [x] **3.2** RED — `test/e2e/serve_test.go`: `TestServeNoUI` (L4) — the `--no-ui` flag and
      `server.ui: false` alone, each unmounting `/ui` with `POST /capture` unaffected.
      Requirement: R4.
- [x] **3.3** GREEN — `cmd/nooma/serve.go`: `noUI := fs.Bool("no-ui", false, ...)`; `uiEnabled :=
      *cfg.Server.UI && !*noUI`; wrap PR 2's already-wired `ui.New(ui.Deps{})` call in that
      conditional (§3.9's exact correction — the call is not introduced here, only guarded);
      `Deps.UI`'s nil path falls through to `requireToken(guardedMux)`.
      Verify: `go test ./cmd/nooma/... -run NoUI`, `go test -tags=e2e ./test/e2e/... -run
      TestServeNoUI`.
      Requirement: R4; design §3.9.
- [x] **3.4** `docs/01-architecture.md` — the `--no-ui` sentence gains "or `server.ui: false`".
      Requirement: R4 (doc parity with the flag's actual reach).
- [x] Verify (PR-level): default (`--no-ui` absent) tip is unchanged from PR 2's own row — `200`,
      shell, with or without a token. With `--no-ui` or `server.ui: false`: `404` without a token,
      `401` with one (§7.2's PR 3 row). Tests modified: none besides the two new ones — the
      default-flag state needs no change to any existing test. `make check-all` in an isolated
      worktree at this branch's tip. Open `feat/serve-no-ui` against `main`; merge on
      `mergeStateStatus: CLEAN`; confirm branch deletion before branching PR 4a. Target ≤110
      impl+docs lines.
      **Confirmed** at tip `78d85ab` (RED `46d3fdc`, GREEN `78d85ab`, branched from
      `feat/ui-base-layout`'s merged tip `42f2644`): default `GET /ui` unchanged from PR 2's row
      on both arms (`TestHandlerServesAPIRootAndUIShell`, `TestOpenRoutesStayOpenRegardlessOfToken`
      untouched, unmodified). With `--no-ui` or `server.ui: false`, confirmed by `TestServeNoUI`
      (e2e): `404` without a token; `POST /capture` unaffected (`503`, no providers configured,
      identical to the UI-mounted case). Impl+docs measured at 35 lines (`cmd/nooma/serve.go`
      33, `docs/01-architecture.md` 2 — `git diff --numstat` on the GREEN commit, well under
      ≤110, no overflow cut needed); test lines reported separately:
      `internal/httpapi/server_test.go` +44 (GREEN commit); `test/e2e/serve_test.go` +60/-2,
      `cmd/nooma/serve_test.go` +30 new file (RED commit — corrected here; the figures originally
      recorded in this line, `test/e2e/serve_test.go` +88/-2 and `cmd/nooma/serve_test.go` +28,
      were wrong: `git diff --numstat 42f2644..46d3fdc` on the actual RED commit gives +60/-2 and
      +30/-0). `make check` green after the GREEN commit; `make check-all` green in an isolated
      worktree at tip `78d85ab` (lint 0 issues, go vet, L1/L2 race+shuffle, build, L3 integration,
      `schema-golden-clean`, `internal/core` coverage 99% unchanged, seven-target cross-compile
      matrix all OK, L4 e2e green, `templ-clean` clean). **Still open** (out of this apply batch's
      scope): opening `feat/serve-no-ui` against `main`, waiting for required contexts, merging
      only on `mergeStateStatus: CLEAN`, confirming branch deletion before branching PR 4a.

      **Judgment-day fixes, post-GREEN** (recorded, not silent): `1fd11f9` adds
      `fs.PrintDefaults()` to `runServe`'s `fs.Usage` so `nooma serve -h` actually shows
      `--no-ui` and its precedence wording, plus `TestServeUsageShowsNoUIPrecedence`
      (`cmd/nooma/serve.go` +8/-1, `cmd/nooma/serve_test.go` +33/-1). `2ddb1ea` adds a third
      `TestServeNoUI` case composing `--no-ui` with a configured token end to end — R4's second
      arm, until now only verified by hand (`test/e2e/serve_test.go` +46/-4). Final split at this
      branch's tip against `main` (`42f2644`), `git diff --numstat 42f2644..HEAD`: impl+docs 42
      lines (`cmd/nooma/serve.go` 40, `docs/01-architecture.md` 2 — churn, added+deleted per
      file, the same convention this block already used), well under ≤110, no overflow cut
      needed; test lines 210 (churn too, `cmd/nooma/serve_test.go` 62, `test/e2e/serve_test.go` 104,
      `internal/httpapi/server_test.go` 44).

**Deviations** (recorded, not silent):
- Task 3.1's RED commit was not written as RED. Probed against PR 2's merged tree before writing
  any PR 3 code (`Handler(Deps{Token: token})` with `UI` left nil, for both `token == ""` and a
  configured token): `/ui`, `/ui/login` and `/does-not-exist` already answered identically in
  every case (404/404/404, then 401/401/401) — PR 2's task 2.5 `if d.UI != nil` mux gate already
  implements the whole of this behavior; nothing in `internal/httpapi` needed to change for PR 3.
  The genuine gap PR 3 closes is entirely in `cmd/nooma/serve.go`, which wired `ui.New(ui.Deps{})`
  into `Deps.UI` **unconditionally** until this PR (PR 2's own task 2.6) — that gap is real and is
  what `TestServeNoUI` (task 3.2, e2e) and a new `cmd/nooma` unit test genuinely fail against
  before this PR's GREEN commit. `TestNoUIUnmountsTheSubtree` was therefore committed as a
  pinning/completeness test inside the GREEN commit (`78d85ab`) rather than as its own RED commit,
  with the probe result stated in both the commit body and here — this project's own strict-TDD
  rule ("watching red for the right reason from the committed tree, not from an uncommitted staged
  state") forbids claiming a RED that does not exist, the same lesson PR 1's task 1.3/1.4 rebuild
  recorded for the opposite mistake (a RED that failed for the wrong reason).
- Added `cmd/nooma/serve_test.go`'s `TestResolveUIEnabled_NoUIFlagOverridesConfig`, not named in
  design or this task list, because the orchestrator's own hard rule for this PR ("an explicit
  `--no-ui` flag beats `ui: true` in the config... state the rule in the flag's help text and test
  it") asks for the precedence rule to be tested directly. Design §3.9's own code block computes
  `uiEnabled` as an inline local (`*cfg.Server.UI && !*noUI`) inside `runServe`, which cannot be
  unit-tested without starting a real server and delivering it an OS signal to stop — expensive and
  fragile for a boolean AND-NOT. Pulled the expression into a small package-level pure function,
  `resolveUIEnabled(serverUI, noUIFlag bool) bool`, with an identical implementation and identical
  call-site behavior: the same naming/shape-for-testability choice PR 2's `newUIMux` extraction
  already set precedent for (design §3.2's own note on that deviation). This function was
  genuinely RED (compile-red, `cmd/nooma/serve_test.go:25:14: undefined: resolveUIEnabled`,
  confirmed in commit `46d3fdc`'s own isolated-worktree check) before the GREEN commit introduced
  it.

---

## PR 4a — `feat/httpapi-ui-cookie-middleware` (~220 impl+docs; risk: Medium)

**In-between state on `main` after this PR merges**: a token-configured server's UI is
unreachable from a browser until 4b lands (N2, design §12) — one PR apart, named in this PR's
body, not hidden.

**Overflow cut**: ADR-0028's Alternatives section shortens to a one-line pointer per alternative
(proposal §8 already argues each in full) rather than restating the reasoning; `requireCookie`,
`uiCookieName` and the wrap do not move — 4b's handlers depend on all three landing whole here.

- [x] **4a.1** RED (strict TDD order 2, proposal §6) — `internal/httpapi/server_test.go`:
      **invert** `TestOpenRoutesStayOpenRegardlessOfToken`'s `/ui` leg — with a token configured
      and no cookie, `GET /ui` now answers `303 See Other, Location: /ui/login`, carries no vault
      data, sets no cookie; the `/` leg (API root) is untouched at `200`. Rename the test
      `TestOpenRoutesAndUIRoutesUnderAToken`.
      Requirement: R1's own MUST — "a request with no cookie or a wrong one never reaches vault
      data."
- [x] **4a.2** RED — `TestUIViewsRequireCookie` — missing cookie, a wrong cookie, and a cookie
      that fails `base64.RawURLEncoding` decoding all give byte-identical `GET` responses (`303
      /ui/login`); the right cookie reaches the view; `POST /ui` against `Handler(d)` answers
      `405 Method Not Allowed`, `Allow: GET, HEAD`, no `Set-Cookie`, no body (the mux's own answer
      for a method-specific pattern with no match, asserted rather than assumed, §3.2's "method
      posture" correction).
      Mutation: a branch that distinguishes missing from wrong; a method-agnostic pattern that
      would route `POST` into the view instead of the mux's `405`. **Not** `==` instead of
      `subtle.ConstantTimeCompare`, nor an early `return unauthorized` on the decode error
      (§3.3's own timing-oracle argument): both produce the identical byte-for-byte response this
      response-level test observes, and only their timing differs — caught instead by
      `test/conformance/httpapi_secret_compare_test.go`, added later this same PR (correction
      recorded here after judgment-day found the original claim did not hold). That gate itself
      was rewritten a further **four** rounds after that. Round 2: a version that inspected only
      each target function's own body missed the same bug once moved into a same-package helper
      (false negative) and separately flagged a same-package helper that still called
      `subtle.ConstantTimeCompare` (false positive) — closed by reshaping `cookie.go` so the bug
      has no signature to be written in (`presentedSecret` returns `[]byte`, no `error`, no
      `http.ResponseWriter`, making the decode-error branch unexpressible **inside
      `presentedSecret` itself**) and by rewriting the gate to check that signature plus a
      transitive, same-package call closure instead of one function's own statements. Round 3 (two
      independently blind reviewers, same PR) found that signature check said nothing about
      `requireCookie`'s or `requireToken`'s own body: a caller can re-derive the same
      missing/malformed fact itself and branch on it with an early `return` ahead of
      `presentedSecret`, compiling, gate-green, response-test-green, and only timing differing —
      closed by a third check on the same gate, a control-flow rule: the only `return` statement
      permitted inside the per-request `http.HandlerFunc` literal either function returns had to be
      guarded by an `if` that REACHES the `subtle.ConstantTimeCompare` comparison. Round 4's blind
      judges broke that rule three further ways, all compiling, all gate-green: a `||`-joined
      condition (`cookieLooksBad(r) || subtle.ConstantTimeCompare(...) != 1`) reaches the compare
      without ever running it, since Go short-circuits `||`; the handler literal wrapped by another
      same-package function one call away from the `return` the rule inspected; and a conditional
      busy loop with **no** `return` at all ahead of the compare — a real timing oracle a
      return-only rule cannot see in the first place. The ruling after round 4: stop blacklisting
      bug shapes (a property that does not terminate) and whitelist the one permitted GOOD shape
      instead — a literal template, declared in the gate's own test file, that `requireCookie`'s
      and `requireToken`'s handler bodies must match exactly, statement by statement, with the
      compare `if`'s condition required to BE (not merely reach) the comparison. This inverts
      round 2's own requirement: staying sound under in-package helper extraction "in either
      direction" was the fix then; under the template it is now the failure — moving the compare or
      the decode into a helper changes the pinned statement sequence and is expected to fail,
      deliberately.
      Requirement: R1, R2.
- [x] **4a.3** RED — `TestRequireCookieNoOpOnlyOnLoopback` over `bindTokenTruthTable`
      (`TestRequireTokenNoOpOnlyOnLoopback`'s own shape).
      Mutation: a cookie check that fires with no token, or does not fire with one.
      Requirement: R1's `Token == ""` case; design §3.2.
- [x] **4a.4** GREEN — `internal/httpapi/cookie.go`: `uiCookieName = "nooma_token"`;
      `requireCookie(token string) func(http.Handler) http.Handler` — decode the cookie's value,
      compare against `token` with `subtle.ConstantTimeCompare` even on a decode error (never an
      early return), no-op when `token == ""`; wrap the two guarded leaves (`GET /ui`, `GET
      /ui/{$}`) in `server.go`, leaving `/ui/login` and `/ui/static/*` unwrapped (§3.2's
      open/guarded split).
      Verify: `go test ./internal/httpapi/...`.
      Requirement: R1, R2; design §3.3.
- [x] **4a.5** `docs/adr/0028-ui-cookie-handshake.md` (new, `Accepted`) — the cookie's value is the
      configured token (base64url), compared in constant time; a session cookie, `Path=/ui`,
      `HttpOnly`, `SameSite=Strict`, `Secure` from `r.TLS`; no session table, no logout; every
      non-safe UI request passes `net/http.CrossOriginProtection` with no trusted origins.
      Alternatives (§3.10): an opaque session id (Q1-B), a synchronizer token (Q2-B),
      `SameSite=Strict` alone (Q2-C). `docs/adr/README.md` gains the `0028` index row.
      Requirement: R2; design §3.10 (OR6's decided default — a new ADR, not a note inside
      `Accepted` ADR-0007 or ADR-0017).
- [x] Verify (PR-level): `GET /ui` at this tip with no token is unchanged, `200` shell — `Token ==
      ""` keeps `requireCookie` a no-op. With a token configured and no cookie: **`303 Location:
      /ui/login`** (inverted); `/ui/login` itself still `404`s until 4b (N2) — this is the stated,
      accepted gap, not a bug found late. Tests modified for the tip to stay green:
      `TestOpenRoutesStayOpenRegardlessOfToken` renamed and its `/ui` leg inverted (task 4a.1).
      `TestHandlerServesAPIRootAndUIShell`/`TestHandlerServesDistinctSurfaces` (both token-less)
      stay green, unmodified. `make check-all` in an isolated worktree at this branch's tip. Open
      `feat/httpapi-ui-cookie-middleware` against `main`; the PR body names N2 explicitly; merge
      only on `mergeStateStatus: CLEAN`; confirm branch deletion before branching PR 4b. Target
      ≤220 impl+docs lines.
      **Confirmed** at tip `7cb1de1` (RED `591de90`, GREEN `7cb1de1`, branched from
      `feat/serve-no-ui`'s merged tip `75b0175`): `GET /ui` with no token unchanged (`200`, PR 2's
      shell); with a token configured and no cookie, `303 Location: /ui/login`, no `Set-Cookie`,
      no vault-shaped body; the right cookie reaches the shell (`200`); `POST /ui` (no route
      declares it) `405 Method Not Allowed`, `Allow: GET, HEAD`, no `Set-Cookie`, before
      `requireCookie` ever runs. `TestRequireCookieNoOpOnlyOnLoopback` confirms the no-op holds
      exactly on the loopback rows of `bindTokenTruthTable`. Tests modified for the tip to stay
      green: `TestOpenRoutesStayOpenRegardlessOfToken` renamed to
      `TestOpenRoutesAndUIRoutesUnderAToken` with its `/ui` leg inverted (task 4a.1);
      `TestHandlerServesAPIRootAndUIShell`/`TestHandlerServesDistinctSurfaces` (both token-less)
      stay green, unmodified. Impl+docs measured at 185 lines (`git diff --numstat` on the GREEN
      commit against `origin/main`, excluding `server_test.go`: `cookie.go` 60, `server.go`
      +14/-9, `docs/adr/0028-ui-cookie-handshake.md` 101, `docs/adr/README.md` 1 — well under the
      ~220 budget and the 400-line ceiling; no overflow cut needed). Test lines reported
      separately: `server_test.go` 191 (RED 183 + an 8-line fix landed in the GREEN commit, see
      Deviations). `make check` green after each commit; `make check-all` green in an isolated
      worktree at tip `7cb1de1` (lint 0 issues, go vet, L1/L2 race+shuffle, build, L3 integration,
      `schema-golden-clean`, `internal/core` coverage 99% unchanged — this PR touches no
      `internal/core` file — seven-target cross-compile matrix all OK, L4 e2e 141.8s,
      `templ-clean` clean). **Final counts after five Judgment Day rounds** (`git diff --numstat
      origin/main..HEAD`, churn = added+deleted, the chain's own rule: `_test.go` and
      `openspec/` excluded from impl+docs). Recomputed here at tip `ad6b8eb` — the commit
      immediately before this bookkeeping edit, to break the self-reference an edit to this same
      file would otherwise introduce into its own diffstat: impl+docs **253 lines (as of
      `ad6b8eb`, not this commit's own edit)** (`cookie.go` 107, `server.go` +14/-9 = 23,
      `docs/adr/0028-ui-cookie-handshake.md` 89, `docs/adr/README.md` 1, `docs/06-harness.md` 25,
      `test/conformance/doc.go` +7/-1 = 8) — **over the ~220 budget** (by 33 lines, ~15%), still
      well under the 400-line ceiling; the overage over round 4's own **238** is round 5's own
      correction pass in `docs/06-harness.md` (10 → 25 lines of churn), restating the gate's
      vacuity claim precisely now that the duplicate-declaration guard and the sibling UI-wiring
      gate exist to describe — `cookie.go` and `server.go` are unchanged by round 5, no new
      production behaviour. Tests **1220 lines (as of `ad6b8eb`, not this commit's own edit)**
      (`cookie_test.go` 69, `server_test.go` 217/12 = 229 — the table-driven rewrite now driving
      every guarded leaf, not only `/ui`; `httpapi_secret_compare_test.go` 646 — this file itself
      **grew**, 584 → 646, the duplicate-declaration guard's own cost; `httpapi_ui_wiring_test.go`
      276 — new this round, the structural sibling gate judge B's finding required); `openspec/`
      bookkeeping **169 lines (as of `ad6b8eb`, not this commit's own edit)** (`design.md`
      48/10 = 58, `tasks.md` 101/10 = 111), reported separately as in every earlier link. Four
      earlier reports in this same paragraph (321, then 219 corrected from 102, then
      238/863/160 at round 4's tip `fe5a9d3`) each undercounted, miscategorized, or went stale
      against a later round's edits; this one is the fifth, taken at tip `ad6b8eb` after round 5's
      two fixes (the duplicate-declaration guard in `httpapi_secret_compare_test.go`; the
      `httpapi_ui_wiring_test.go` gate plus `TestUIViewsRequireCookie`'s table-driven rewrite), and
      is expected to need recomputing again if `openspec/` bookkeeping is edited after it, for the
      same self-reference reason stated above. **Still open** (out of this apply batch's scope, per the executing
      agent's own instructions): opening `feat/httpapi-ui-cookie-middleware` against `main`,
      waiting for required contexts, merging only on `mergeStateStatus: CLEAN`, confirming branch
      deletion before branching PR 4b.

      **Deviations** (recorded, not silent): design m4a §3.2 and §5 both claim `POST /ui`'s `405`
      carries "no body". Probed against this tree: `net/http.ServeMux`'s own default
      method-not-allowed handler is `http.Error`, which writes `"Method Not Allowed\n"` —
      confirmed by running `TestUIViewsRequireCookie`'s own subtest before adjusting it.
      `requireCookie` never runs on this path (the mux answers before it), so nothing this PR
      wrote produced the body — it is `net/http`'s own stdlib behavior design's prose did not
      probe before asserting. What actually matters for R1 — that nothing vault-shaped leaks
      through an unauthenticated method mismatch — still holds and is what the test now asserts
      (the literal generic stdlib message), landed as an 8-line fix inside the GREEN commit
      (`7cb1de1`) rather than a second RED/GREEN pair, since the RED commit's own claim (405,
      `Allow: GET, HEAD`, no cookie) was otherwise correct and already red for the right reason —
      only the "no body" sub-assertion needed correcting once the real response was observed.
      Disclosed for the next reader rather than left implied: a stricter reading of strict TDD
      order would have wanted this correction as its own RED/GREEN pair, since the fix changed
      what the test asserts, not merely the code under it; landing it inside the GREEN commit is
      the precedent this note records, not a claim that no stricter option existed.

---

## PR 4b — `feat/httpapi-ui-login-screen` (~210 impl+docs; risk: Medium)

- [ ] **4b.1** RED — `TestOpenRoutesAndUIRoutesUnderAToken` gains its **third leg** in this PR's
      RED commit: `GET /ui/login` is `200` with no `Set-Cookie` — this leg cannot be asserted in
      4a because `loginPage` is 4b's own GREEN and 4a's tip must stay green (§3.2).
      Requirement: R1's "except the handshake screen" clause.
- [ ] **4b.2** RED — `TestLoginIssuesTheCookieOnlyOnTheRightToken` — one `Set-Cookie`; name,
      decoded value, `Path=/ui`, `HttpOnly`, `SameSite=Strict` asserted one by one; `Secure`
      absent under `httptest.NewServer`, present under `httptest.NewTLSServer`; `303 Location:
      /ui` on success.
      Mutation: a flag dropped; `Secure` hard-coded either way; `Path=/`; a `Max-Age` added; the
      raw token stored as the cookie value instead of base64url-encoded (breaks a token containing
      `;`, `,`, `"`, `\` or a control byte).
      Requirement: R2.
- [ ] **4b.3** RED — `TestLoginRejectionIsByteIdentical` (an empty field and a wrong token produce
      the same `401` body and headers); `TestLoginRoutesAbsentWithoutAToken` (`Token == ""` → `GET
      /ui/login` is `404`, not a screen shown on loopback).
      Mutation: an oracle distinguishing "empty" from "wrong"; a screen shown when `Token == ""`.
      Requirement: R1, R2.
- [ ] **4b.4** RED — `test/e2e/serve_test.go`: the L4 handshake walk (`docs/06-harness.md:181-183`)
      — token on loopback: `GET /ui` → `303`, `POST /ui/login` → cookie, `GET /ui` with the
      cookie → `200`.
      Requirement: Exit criterion.
- [ ] **4b.5** GREEN — `internal/ui/login.templ`/`login_templ.go` (`Login(LoginView)`),
      `internal/ui/login.go` (`LoginView{Rejected bool}`, `RenderLogin`); `internal/httpapi/cookie.go`
      gains `setUICookie` (base64url-encodes the token, sets the four flags), `loginPage` (renders
      `ui.Login(LoginView{Rejected: false})` at `200`), `loginSubmit` (`http.MaxBytesReader(w,
      r.Body, 4096)` before `ParseForm`, compares via `requireCookie`'s own comparison, on success
      `setUICookie` + `303 /ui`, on failure re-renders at `401` with `Rejected: true`); register
      the two `GET`/`POST /ui/login` leaf patterns on `uiMux`.
      Verify: `go test ./internal/httpapi/... ./internal/ui/...`.
      Requirement: R1, R2; design §3.3.
- [ ] Verify (PR-level): `GET /ui` with no token is unchanged, `200` shell. With a token
      configured and no cookie: `303` unchanged; `GET /ui/login` is now `200`; `POST /ui/login`
      with the right token sets the cookie, after which `GET /ui` with the cookie is `200` —
      **still shell**, `TodayReader` is PR 7's (§7.2's PR 4b row). Test modified:
      `TestOpenRoutesAndUIRoutesUnderAToken` gains its third leg (task 4b.1). `make check-all` in
      an isolated worktree at this branch's tip. Open `feat/httpapi-ui-login-screen` against
      `main`; merge only on `mergeStateStatus: CLEAN`; confirm branch deletion before branching
      PR 5 (PR 5 does not depend on 4b, but the chain order is fixed, §7). Target ≤210 impl+docs
      lines.

---

## PR 5 — `feat/ports-store-live-focus-by-type` (~170 impl+docs; risk: Medium)

Touches only `internal/ports`, `internal/store`, `internal/core/prospection` — never
`internal/httpapi`/`internal/ui`. **`docs-sync` fires on this PR** because it edits
`internal/core/prospection/digest.go`; task 5.5 carries the doc 02 §7 sentence in the same commit,
no `no-spec-change` label claimed.

**Overflow cut**: `LiveFocusCandidatesByType`'s doc comment trims to the two paragraphs a caller
needs first (positive filter, id order); the SQL-vs-`ORDER BY` rationale stays here as its only
copy.

- [ ] **5.1** RED — `test/support/repocontract/unitrepo.go`: `RunLiveFocusCandidatesByType` — a
      `pool` unit of each wanted type returns; a `pool` unit of an unwanted type, and an
      `archived`, a `superseded` and an `incomplete` unit of a wanted type, do not; id order;
      empty `types` → empty slice, never an error. Run first against `memrepo` (compile-red, then
      a stub returning nothing).
      Mutation: a negative status filter (`status != 'archived' AND ...`) instead of the positive
      `status = 'pool'` I02 requires — fails the day a fifth status arrives; a `LIMIT` added; the
      type filter applied to the wrong column.
      Requirement: R5 — "excludes `superseded`/`incomplete` (I02) ... rather than by count."
- [ ] **5.2** RED — `test/support/repocontract/staterepo.go`: `RunLatestEnergy` (new, alongside
      `RunOpenHypothesis`/`RunLastHypothesisAt`) — pins `Source` beside `Level`/`RecordedAt` for a
      `user`-sourced and a `consolidation`-sourced row.
      **Red**: `undefined: prospection.EnergyReading.Source` — the field does not exist yet.
      Requirement: R5's SYSTEM energy line, "carries its source" (design §3.6, owner ruling
      2026-09-16).
- [ ] **5.3** GREEN — `internal/ports/unitrepo.go`: `LiveFocusCandidatesByType(ctx
      context.Context, types []unit.Type) ([]focus.Candidate, error)` (§3.7's exact doc comment:
      bounded by status and type, never by count; `ORDER BY id`; the type filter lives in SQL, not
      re-derived in `brain`). `internal/store/sqlite/unitrepo.go`: the SQL (`WHERE status = ? AND
      type IN (?, …) ORDER BY id`), `scanCandidate` factored out and shared with
      `LiveFocusCandidates`'s own scan. `test/support/memrepo/units.go`: the fake.
      Verify: `go test ./test/support/repocontract/... ./test/support/memrepo/...`.
      Requirement: R5; design §3.7. This is the **one port change** this slice makes.
- [ ] **5.4** GREEN — `internal/core/prospection/digest.go`: `EnergyReading` gains `Source
      string` (the raw `current_state.source` value, never the `ports` constant — `prospection`
      stays pure). `internal/store/sqlite/staterepo.go`'s `LatestEnergy` SELECT and `Scan` gain
      `source`; the method's signature is unchanged, so this is a struct widening, not a second
      port change (§3.6's own ruling).
      Verify: `go test ./internal/core/prospection/... ./test/support/repocontract/...`.
      Requirement: R5; design §3.6.
- [ ] **5.5** `docs/02-cognitive-core.md` §7 — **one sentence, same commit as 5.4**: the energy
      reading now carries its source (`user` or `consolidation`) for display; the low-energy gate
      (`LowEnergy`) is itself unchanged; cross-reference §10's `current_state` column list, where
      `source` is already named.
      Requirement: R10-class doc parity (non-negotiable #1); `docs-sync` fires on this PR (design
      §4's correction to an earlier claim that no PR touched `internal/core`).
- [ ] **5.6** `testdata/schema/store_api.golden` — regenerate; diff limited to
      `LiveFocusCandidatesByType`'s one new line.
      Verify: `make store-api-golden && git diff --stat testdata/schema/store_api.golden`.
      Requirement: R5.
- [ ] **5.7** L3: `EXPLAIN QUERY PLAN` on `LiveFocusCandidatesByType` names the status index if
      one exists.
      Requirement: design §8's L3 row.
- [ ] Verify (PR-level): `GET /ui` at this tip is unchanged from PR 4b's own row on both arms —
      PR 5 touches no `internal/httpapi`/`internal/ui` file (§7.2's PR 5 row: "None new for `/ui`'s
      own HTTP state"). No existing HTTP test is modified; `RunLiveFocusCandidatesByType` and
      `RunLatestEnergy` cover the new read, not the route. `make check-all` in an isolated
      worktree at this branch's tip — confirm `docs-sync` (a PR-metadata check, not a Makefile
      target) has the doc 02 §7 delta to find once the PR is open. Open
      `feat/ports-store-live-focus-by-type` against `main`; merge only on `mergeStateStatus:
      CLEAN`; confirm branch deletion before branching PR 6. Target ≤170 impl+docs lines.

---

## PR 6 — `feat/brain-today` (~260 impl+docs; risk: High — second-closest PR to the ceiling)

**Overflow cut** (if over 400): split `digestItems`' package-function refactor and its one
call-site update in `brain/digest.go` off into `feat/brain-digest-items-refactor` (a new PR 5b,
landing after 5 and ahead of 6), leaving `today.go`, `wireToday` and the doc amendments in PR 6.

- [ ] **6.1** RED (strict TDD order 4, proposal §6) — `test/conformance/i27_viewing_is_not_delivering_test.go`
      (new): `TodayService` over `memrepo` fakes wrapped in a `writeGuard` that fails the test on
      any call to `Surface`, `Fire`, `Expire`, `Resolve`, `Create`, `MarkAsked`, `Confirm`,
      `Reject`, `Record`, `RecordConsolidationRun`, `OpenHypothesis`, `SetStatus`, `ApplyBoosts`,
      `UpdateContent`, `UpdateEventAt`, `UpdateDueAt`; `Undelivered()` and `Unasked()` compared
      before and after the call. Written against a `TodayService` that does not compile yet.
      Mutation: the one mutation the milestone is named for — a view that delivers.
      Requirement: R6; design §3.6, §8 — I27 (OR7's decided default).
- [ ] **6.2** RED — `TestToday_PriorityOnlyTopNPerKind` — `focus.DefaultSize + 2` task units and
      `focus.DefaultSize + 1` load units with distinct weights; each focus holds exactly
      `focus.DefaultSize` in `focus.Rank`'s order; the task focus contains no `mental_load`, the
      load focus contains no `task`/`event`; adjacency is never non-zero.
      Mutation: a `focus.Select` call with a real margin instead of bare `focus.Rank` +
      `[:DefaultSize]`; a type leak between focuses; a truncation at the wrong N.
      Requirement: R5's FOCUS section — "Priority-only ... no call to `focus.Select`."
- [ ] **6.3** RED — `TestToday_DigestMirrorsCarry` — with a low-energy reading, `Items` equals
      `Carry`'s carry slice joined to `pending` by ID for the same inputs, `Held` equals
      `len(held)`, `Question` is nil; without one, `Items` is every undelivered trigger in
      `(fired_at, id)` order and `Question` is `Unasked()[0]`.
      Mutation: a Today that lists held items (violates §3.6's "counted, never listed", OR2's
      decided default); a question shown on a low-energy day; a second, independent sort; a join
      that drops a trigger `Carry` still names.
      Requirement: R5 — PENDING DIGEST section; design §3.6 (OR2, OR3's decided defaults).
- [ ] **6.4** RED — extend the existing `test/conformance/brain_single_clock_read_test.go` to scan
      `today.go` as a second file.
      Mutation: a second `clock.Now()` call inside `todayRunner.at`, or a `Now()` call inside a
      function that already takes a `now time.Time` parameter.
      Requirement: R5's single-clock-read MUST — "all nine reads ... use a single
      `ports.Clock.Now()` call."
- [ ] **6.5** GREEN — `internal/brain/today.go`: `TodayService`, `NewTodayService`, `Today`,
      `Focus`, `FocusMember`, `PendingDigest`, `DigestLine`, `VaultStatus`, `todayRunner.at`
      implementing §3.6's exact 8-step read order (`cfg.Load` → `state.LatestEnergy` →
      `triggers.Undelivered` → `log.Since` → `digestItems` + `Carry` → `questions.Unasked` (if
      `!low`) → `questions.Open` → per-Kind `LiveFocusCandidatesByType` + `Rank` +
      `LiveByIDs`). Wired unconditionally at vault open, the same call site as `wireBrain`, never
      inside `wireScheduler`'s LLM-gated path (§3.6's "the service needs no provider").
      Verify: `go test ./internal/brain/... -run Today`.
      Requirement: R5, R6; design §3.6.
- [ ] **6.6** GREEN — `internal/brain/digest.go`: `digestItems` becomes a package function;
      `checkRunner.digestItems` deleted, its one call site updated to the package function.
      Verify: `go test ./internal/brain/...`.
      Requirement: design §3.6 — "the same rule in two places... is how Today's Carry order and
      the digest's Carry order would drift apart."
- [ ] **6.7** `cmd/nooma/wiring.go`: `wireToday`.
      Requirement: design §4.
- [ ] **6.8** `docs/02-cognitive-core.md` §3 — one sentence: Priority-only, no incumbent, until
      `m4c`. §7 — one sentence: viewing is not delivering.
      Requirement: R10-class doc parity; non-negotiable #1. (This PR's `internal/core` touch is
      none — `today.go` lives under `internal/brain` — so `docs-sync` does **not** fire on PR 6;
      these two sentences are still required by this design and checked by `sdd-verify`, not by
      the gate, per design §4's own correction.)
- [ ] **6.9** `docs/06-harness.md` §4 — add the **I27** row (next free invariant number, design
      §1's ground truth: I01–I26 run today).
      Requirement: OR7's decided default — a numbered invariant, not an unrowed conformance test.
- [ ] **6.10** `docs/01-architecture.md` — the `/ui` row names SYSTEM's six lines.
      Requirement: R5.
- [ ] Verify (PR-level): `GET /ui` at this tip is unchanged from PR 5's own row on both arms —
      `ui.TodayReader`/`Deps.Today` are PR 7's, so `ServeHTTP` still renders the PR 2 shell
      (§7.2's PR 6 row). No existing HTTP test is modified;
      `i27_viewing_is_not_delivering_test.go`, `TestToday_PriorityOnlyTopNPerKind` and
      `TestToday_DigestMirrorsCarry` exercise `brain.TodayService` directly, never the route.
      `make check-all` in an isolated worktree at this branch's tip. Open `feat/brain-today`
      against `main`; merge only on `mergeStateStatus: CLEAN`; confirm branch deletion before
      branching PR 7. Target ≤260 impl+docs lines — **measure before opening**.

---

## PR 7 — `feat/ui-today-view` (~195 impl+docs; risk: Medium)

**Overflow cut** (if over 400, no PR 8 to receive it): factor `today.templ`'s two `AllKinds()`
loops (FOCUS's task and load sections) into one shared partial before the tip, recovering their
near-duplicate markup from within this PR.

- [ ] **7.0** Deferred from PR 2 — add `<nav>` to `layout.templ` and assert it in the shell/Today
      tests; this is the first PR with a navigable view for a nav to link to (design §7 PR 1 row,
      §8 shell row and the §3.1 tree all say PR 7 now).

- [ ] **7.1** RED — `internal/ui`: `TestTodayView_RendersThreeSectionsFromTheModel` — a fixed
      `brain.Today` renders each focus member's id, content and `Score` (two decimals) in rank
      order; each digest line's text; `Held` as a count, never a list; the question's two
      endpoints; the six status lines with `never`/`no reading` for nils; a member with a `NaN`
      `Score` renders the literal `NaN`. Asserted on structure (`strings.Contains`, index order),
      never the whole document.
      Mutation: a template that re-sorts, filters or drops a member; a nil dereference on an empty
      focus; a held list rendered; `Score` dropped from the markup; `NaN` formatted as `0.00`
      instead of the literal string.
      Requirement: R5 (all three sections); Q7's ruling (Score rendered, never coerced).
- [ ] **7.2** RED — `TestTodayView_I18DatesLabelled` — a member with `DueAt` and one with
      `EventAt` render under different labels, never swapped.
      Mutation: I18's own UI failure mode.
      Requirement: R5 (`DueAt`/`EventAt` distinction).
- [ ] **7.3** RED — `TestTodayView_NilTodayReaderAnswers503` — from this PR, `ui.New(ui.Deps{})`'s
      no-`TodayReader` fixture (PR 2's own shell-era construction, task 2.3) answers `503`, not
      the retired PR 2–6 shell. Narrow `TestHandlerServesAPIRootAndUIShell`'s (task 2.2) own
      comment to state its scope is PR 2 through PR 6 only — this test takes over that fixture's
      PR 7+ behavior (§7.2, §8's own cross-reference).
      Mutation: `captureHandler`'s existing nil posture, kept — a regression here means a nil
      `TodayReader` silently falls back to the shell or crashes instead of `503`.
      Requirement: design §3.1, §7.2.
- [ ] **7.4** RED — `test/e2e/serve_test.go`: `GET /ui` after the handshake lists a real unit
      captured through the API (L4).
      Requirement: Exit criterion.
- [ ] **7.5** GREEN — `internal/ui/today.templ`/`today_templ.go`: `Today(brain.Today, Serving)` —
      FOCUS (two `AllKinds()` sections), PENDING DIGEST, SYSTEM; `FocusMember.Score` rendered two
      decimals, the literal `NaN` for a NaN value, never coerced (Q7, ruled).
      Verify: `go test ./internal/ui/...`.
      Requirement: R5; design §3.6, §3.1.
- [ ] **7.6** GREEN — `internal/ui/ui.go`: `TodayReader interface { Today(ctx) (brain.Today,
      error) }` (a narrow behavioral interface `ui` declares, satisfied by `*brain.TodayService`,
      §3.1's chosen option); `Deps.Today TodayReader`; `ServeHTTP` renders Today unconditionally
      when `TodayReader != nil`, `503` otherwise (`captureHandler`'s own nil posture).
      Verify: `go test ./internal/httpapi/... ./internal/ui/...`.
      Requirement: R5, R6; design §3.1, §3.2.
- [ ] **7.7** GREEN — `cmd/nooma/serve.go`: widen the existing `ui.New(...)` call (task 2.6) with
      `Today: today` and `Serving: ui.Serving{Bind: addr, CookieAuth: token != ""}` — the same
      call, not a new one, once `wireToday` (task 6.7) exists.
      Verify: `go test -tags=e2e ./test/e2e/...`.
      Requirement: design §3.9, §3.1.
- [ ] Verify (PR-level): `GET /ui` with no token is now the **real** Today page
      (FOCUS/PENDING DIGEST/SYSTEM) — `ServeHTTP` renders Today unconditionally once `TodayReader`
      is wired; with a token and no cookie, `303` unchanged; with the right cookie, `200` real
      Today (§7.2's PR 7 row). Tests modified: `TestHandlerServesAPIRootAndUIShell`'s own scope
      narrows to PR 2 through PR 6 (task 7.3's comment); its no-`TodayReader` fixture now falls
      into the `503` arm and `TestTodayView_NilTodayReaderAnswers503` takes over asserting that
      fixture's PR 7+ behavior — no other existing test changes. `make check-all` in an isolated
      worktree at this branch's tip. Open `feat/ui-today-view` against `main`; merge only on
      `mergeStateStatus: CLEAN`; confirm branch deletion. This is the chain's last link — confirm
      `main`'s tree equals this branch's tree after merge (m3e's own post-merge check). Target
      ≤195 impl+docs lines.

---

## Deviations from design

**None found.** Design's own §7.1 and §7.2 already located and fixed the tree's two internal
inconsistencies (the `TodayReader`/`brain.Today` PR attribution, and `setUICookie`'s PR
attribution in §10 vs. §7/§4) and stated "no other instance found" after walking every symbol,
route, test and doc amendment against every PR tip. Spec.md's own scope boundary already reflects
design's amended read count ("eight existing reads plus one new") and R1's MUST already names both
open routes ("except the handshake screen and the static asset route") — the two points where an
earlier spec/design mismatch could have existed are both already reconciled inside the artifacts
as read for this task breakdown. No new disagreement was found while slicing §3/§4/§6/§7/§8 into
the tasks above.

---

## Carried forward (design §11 — decided defaults, ship if the owner is silent; Q6 provisional)

| # | Item | Decided default | Where |
|---|---|---|---|
| OR1 | No logout in `m4a` | None — `POST /ui/logout` fits `m4e`'s admin view better | Not tasked; explicit non-goal |
| OR2 | Held digest items are counted, not listed | `Held int` rendered as one line | PR 6 (task 6.3), PR 7 (task 7.1) |
| OR3 | "The unasked or open question" reads as "what the next digest would carry" | `Unasked()[0]` when `!low`; open ones counted only | PR 6 (task 6.3) |
| OR4 | Cross-origin middleware ships in `m4a` PR 2, not `m4b` | Here — `m4b`'s own `feat/httpapi-cross-origin` row is discharged | PR 2 (task 2.4) |
| OR5 | The handshake splits into two PRs (4a, 4b), with an unreachable-UI gap on `main` between them | Split | PR 4a, PR 4b |
| OR6 | ADR-0028 rather than a note inside an `Accepted` ADR | New ADR | PR 4a (task 4a.5) |
| OR7 | I27 as a numbered invariant with a `docs/06-harness.md` §4 row | I27 | PR 6 (tasks 6.1, 6.9) |
| Q6 | Should the API's `POST` routes also sit behind `CrossOriginProtection`? | **Not decided; unchanged.** Recommendation: no in M4 | Not tasked — named, not built |
| Q7 | Does the focus render `Score`? | **Ruled: yes** — two decimals, `NaN` as `NaN`, never coerced | PR 7 (tasks 7.1, 7.5) |

**Rollback coupling (design §10, not a task but a constraint on how any revert must be sequenced)**:
reverting PR 4a reopens R1 (a token-configured UI would serve the shell with no cookie check) and
also breaks the build on its own — PR 4b's `loginPage`/`loginSubmit`/`setUICookie` live in the
same `cookie.go` and reference 4a's `uiCookieName` constant, and `server.go`'s wiring depends on
`requireCookie` directly — so **4a is reverted only together with 4b–7, never alone**. Reverting
PR 1 while PR 2 stands also breaks the build — PR 2's `ui.go` calls `layout.Page`, which only PR 1
defines — so **revert 2 before 1**, never the reverse.

---

## Traceability

| Spec requirement | Tasks |
|---|---|
| R1 — token gates every `/ui` route but the handshake and static assets | 4a.1–4a.4, 4b.1, 4b.3, 3.1–3.3 |
| R2 — the cookie carries the token, constant-time compared | 4a.2, 4a.4–4a.5, 4b.2–4b.3, 4b.5 |
| R3 — cross-origin protection on every non-GET UI route | 2.1, 2.4 |
| R4 — `--no-ui`/`server.ui: false` unmount `/ui`, API unaffected | 3.1–3.4 |
| R5 — Today's three sections over nine reads, one clock call | 5.1–5.7, 6.2–6.10, 7.1–7.7 |
| R6 — viewing is not delivering | 6.1, 6.5, 7.6 |
| R7 — the templ clean-tree gate and `ui-boundary` are real gates | 1.1–1.4, 2.1 |
| Exit criterion | 4b.4, 7.4 |

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines (impl+docs) | ~1,705 budgeted across 8 PRs (design §7: 190 + 350 + 110 + 220 + 210 + 170 + 260 + 195) |
| Estimated test lines | Not aggregately budgeted by design (tests are reported per PR, never against the 400-line ceiling). Estimated **~3,500–5,000** at this project's own measured 2×–3.9× impl-to-test ratio on comparably-sized PRs in `m3e` (e.g. PR 5: 293 impl / 772 test; PR 6: 262 impl / 1,020 test) |
| PR count | 8 (PR 4 splits into 4a/4b per design's own overflow rule) |
| Chained PRs recommended | Yes — already a chain by design; ~1,705 budgeted lines alone exceeds the 400-line-per-PR ceiling by ~4×, before this project's own historical 1.3×–4.3× realized-vs-budgeted multiplier |
| 400-line budget risk — per PR | PR 1 Low (~190); PR 2 **High** (~350, closest to the ceiling, watch closely per design §7); PR 3 Low (~110); PR 4a Medium (~220); PR 4b Medium (~210); PR 5 Medium (~170); PR 6 **High** (~260, second PR design names to watch closely); PR 7 Medium (~195) |
| 400-line budget risk — overall | Medium — every PR carries a named, pre-drawn overflow cut (design §7), so no PR is expected to blow the ceiling silently, but PR 2 and PR 6 are the two the multiplier has historically hit hardest in this project |
| Decision needed before apply | **No** — delivery strategy is `auto-chain` (design §7's own "Chain `stacked-to-main`, delivery `auto-chain` (proposal §5)"), which resolves the chain-strategy decision without a stop-and-ask; `sdd-apply` proceeds PR by PR in the fixed order (1 → 2 → 3 → 4a → 4b → 5 → 6 → 7), applying each PR's own named overflow cut only if its measured lines threaten 400 — reported before splitting, never split silently |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium
