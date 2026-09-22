# Design — m4a: the UI foundation (toolchain, boundary, handshake, Today)

Technical design for `m4a-ui-foundation`, the first of the six chained changes
[`m4-mirror-ui/proposal.md`](../m4-mirror-ui/proposal.md) §5 splits M4 into. Scope is that
document's **m4a block only**: §3.2's m4a items, §3.3's one new read, §4.1's five decisions with
their tests, §4.2's first three rows with their doc amendments, §5.1's seven m4a PRs, §6's
orders 1–4, and owner rulings Q1–Q3. `m4b`–`m4f` are out.

`m4a` is the first change since M1 to open an HTTP route that serves vault data, and the first
whose failure mode is a *misreported* decision rather than a wrong one (proposal §0). Every
decision below is therefore judged against one property first: **the mirror renders what the
brain decided and decides nothing itself.**

It does not restate requirements — that is `spec.md`, written concurrently and never read or
edited here. It does not edit `docs/`; it *describes* the doc deltas each PR carries, because
`CLAUDE.md` non-negotiable #1 requires the doc delta to ship in the same PR as the code.

> **Six things this design decides that the proposal did not anticipate**, named up front rather
> than applied silently. Items 1 and 2 are settled — by `spec.md`'s own amended count and by the
> owner's 2026-09-16 ruling, respectively; items 3–6 stay flagged for owner review:
> 1. **The Today service uses nine port methods, not seven** (§3.6). Reproducing
>    `prospection.Carry`'s order faithfully needs the digest's own `LiveFocusCandidates` read,
>    and rendering a focus member's text needs `LiveByIDs` — `focus.Candidate` carries no
>    content. One new method, as the proposal said; nine in use.
> 2. **SYSTEM's energy line now carries its source** (§3.6, owner ruling 2026-09-16: *keep the
>    ruling* — spec R5 and proposal Q3 both require it). `prospection.EnergyReading` widens by
>    one field, `Source string`; `StateRepo.LatestEnergy`'s signature is unchanged — this is not
>    a new port method, so §3.3's "the only port change this slice makes" still names exactly one:
>    `LiveFocusCandidatesByType`.
> 3. **The cross-origin middleware ships in m4a PR 2, not m4b** (§3.4). `POST /ui/login` is a
>    non-GET UI route, so m4a already has the mutation Q2 protects. m4b's
>    `feat/httpapi-cross-origin` row is discharged here.
> 4. **The handshake PR splits in two before it is written** (§7). With ADR-0028 inside it,
>    it lands above 400; the proposal's own overflow rule applies — middleware first, screen
>    second.
> 5. **A new ADR is needed** (§3.10): ADR-0028, *the UI cookie carries the token*. Q1 and Q2
>    are architectural forks with live alternatives, the same weight as ADR-0017's header
>    decision, and ADR-0007 is `Accepted` and cannot absorb them.
> 6. **`viewing is not delivering` becomes an invariant with a number**, I27 (§8). A rule with
>    a conformance test and no row in `docs/06-harness.md` §4 is what `nooma-testing`'s step 2
>    forbids.

---

## 1. Ground truth this design was verified against

Every row was read at the named file and line in this session, at `4e3115c`.

| Claim | Verified at |
|---|---|
| `Handler` builds an open mux (`GET /{$}`, `GET /ui`) and a guarded mux wrapped in `requireToken(d.Token)` mounted at `/`; `GET /ui` serves `uiPlaceholder` on the **open** mux | `internal/httpapi/server.go:81-108`, `:122-131` |
| `Deps` is `{Version, Capture, Recall, Token}`; a nil `Capture`/`Recall` answers 503, never a crash | `server.go:17-40` |
| `requireToken("")` is a no-op; non-empty compares `crypto/subtle.ConstantTimeCompare`; missing and wrong produce byte-identical 401s | `auth.go:54-79` |
| `ResolveToken` is the one source of "is a token configured", shared with `DecideBinding` | `auth.go:28-37`, `binding.go:41-49` |
| `TestOpenRoutesStayOpenRegardlessOfToken` pins `GET /` **and** `GET /ui` at 200 with a token and no header, and asserts no `Set-Cookie` on either | `server_test.go:163-184` |
| `TestHandlerServesBothSurfaces` and `TestServeAnswersBothSurfaces` (L4) both `GET /ui` and want 200 with `Deps{Version}` only | `server_test.go:15-40`, `test/e2e/serve_test.go:80-98` |
| `serve.go` declares a `FlagSet` with **no flags**; `Config.Server.UI` is never read outside `internal/config`; `cfg.Server.UI` defaults `true` via `ApplyDefaults` | `cmd/nooma/serve.go:43-51`, `internal/config/defaults.go:37-40` |
| `http.Server` is built with `Handler: httpapi.Handler(...)` and no TLS config; `ListenAndServe` only | `serve.go:124-128`, `:169` |
| `UnitRepo` has 12 methods; `LiveFocusCandidates(ctx, ids)` returns `focus.Candidate` ordered by id; `LiveByIDs(ctx, ids)` returns `unit.Unit` in the caller's order; **no read lists live units by type** | `internal/ports/unitrepo.go:48-51`, `:163-201` |
| `CountLiveByType(ctx, t unit.Type)` is on record as *not* reopening the "no `List(status)`" rule: `unit.Type` has no live/non-live axis | `unitrepo.go:114-125` |
| `focus.Candidate` is `{ID, Type, Weight, DecayRate, LastTouchedAt, CreatedAt, DueAt}` — **no `Content`** | `internal/core/focus/priority.go:158-166` |
| `focus.Rank(cs, adjacency, now) []Ranked` scores with `Priority` and sorts with a three-level tie-break; `focus.DefaultSize = 7` is §13's `focus_size`; `focus.Types(KindTask) = {task, event}`, `Types(KindLoad) = {mental_load}`; `AllKinds()` returns `{task, load}` | `rank.go:74-100`, `select.go:26`, `:40-76` |
| `focus.Select(k, ranked, previous, margin, size)` with empty `previous` degenerates to top-`size` by score — the post-restart state R4.5 pins | `select.go:78-157` |
| `brain/digest.go:116` calls `prospection.Carry(items, map[string]float64{}, low, now)` with the comment *"Adjacency is M4's"*; `digestItems` is a `checkRunner` method that reads `LiveFocusCandidates` and `heldCounts(history)`; `nextQuestion` is `Unasked()[0]` and is skipped when `low` | `internal/brain/digest.go:107-116`, `:184-218`, `:348-360` |
| `CheckService` holds the one `ports.Clock`; `Check` is the file's one `Now()` and hands the instant to `checkRunner.at` | `check.go:14-43`, `:86-89` |
| `brain_single_clock_read_test` fails a non-test file under `internal/brain` on a second `Now()` call expression, or on any `Now()` inside a function that already takes `time.Time` | `test/conformance/brain_single_clock_read_test.go:58-113` |
| `ConfigRepo.Load` returns `VaultConfig` with `ConsolidationLastRunAt *time.Time`; `StateRepo.LatestEnergy` returns `*prospection.EnergyReading` (`Level`, `RecordedAt`); its SQL drops `current_state.source` today even though the column exists (migration `0003_current_state_source.sql:8`, `StateSourceUser`/`StateSourceConsolidation` at `ports/staterepo.go:18-19`) — `staterepo.go:87-88`'s `SELECT` does not name it; `TriggerRepo.Undelivered`; `PendingQuestionRepo.Unasked`/`Open`; `DecisionLog.Since(t, limit)` forward-only | `configrepo.go:14-36`, `ports/staterepo.go:56-66`, `store/sqlite/staterepo.go:81-102`, `triggerrepo.go:244-250`, `pendingquestionrepo.go:117-124`, `decisionlog.go:241-255` |
| `.golangci.yml` has `core-purity`, `sqlite-containment`, `ports-purity`, `brain-boundary`, `scheduler-boundary`; **no rule's `files:` scope covers `internal/ui/**`** (`core-purity`'s own `deny` list does name `internal/ui` as an import a `core` file may not make, `.golangci.yml:67`); `exclusions.generated: lax` | `.golangci.yml:47-157`, `:176-177` |
| `Makefile`: `check = lint test build`; `check-all` adds `test-integration schema-golden-clean cover cross-compile test-e2e`; `schema-golden-clean` is `make schema-golden && git diff --exit-code -- testdata/schema` | `Makefile:46-50`, `:79-81` |
| `ci.yml`'s trailing comment lists *"templ generate leaves a clean tree"* as **not enforced**; `docs/06-harness.md:334` lists it as blocking | `ci.yml:125-127`, `docs/06-harness.md:334` |
| `.gitattributes` forces `* text=auto eol=lf`; no `linguist-*` line | `.gitattributes:15` |
| `go.mod` is `go 1.26.4`, three direct requires, **no templ, no `tool` directive** | `go.mod:1-9` |
| `net/http.CrossOriginProtection` exists in the installed Go 1.26.4: safe methods always pass; `Sec-Fetch-Site: same-origin\|none` passes; any other value → 403; **no `Sec-Fetch-Site` and no `Origin` → passes** (assumed non-browser); `Origin` host ≠ `Host` → 403; `.Handler(h)` wraps, `.Check(r)` decides | `$GOROOT/src/net/http/csrf.go:134-175`, `:206-218` |
| `docs/06-harness.md` §3: the UI is **L1** — `httptest`, assert structure, never a page golden; L4 gains `GET /ui` + the handshake; the graph island's two gates ship with the first island JS line (m4d) | `docs/06-harness.md:159-219` |
| Invariants run I01–I26; the next free number is **I27** | `docs/06-harness.md:241-266` |
| Doc 02 §3: two focuses are two queries over `units`; hysteresis needs the previous focus *in process*; the first ranking after a restart has `previous` empty for both hysteresis and adjacency | `docs/02-cognitive-core.md:312-335` |
| ADR-0007 fixes the cookie's flags (`HttpOnly`, `SameSite=Strict`, `Secure` when TLS) and says "session cookie" and "one secret"; it does not say what the value is | `docs/adr/0007-http-auth.md:41-53` |
| ADR-0008: `_templ.go` committed; CI runs `templ generate` and fails on a dirty tree; `go build` needs no `templ`; `linguist-generated` named as the mitigation | `docs/adr/0008-ui-stack.md:37-46`, `:59-61` |
| ADR-0018: one hand-written CSS file, `@layer reset, tokens, base, layout, components, utilities;`, `light-dark()`, system font stack, embedded | `docs/adr/0018-css-approach.md:47-66` |
| `internal/ui/doc.go` is four lines and the package's only file | `internal/ui/doc.go` |
| `test/support/memrepo` has a fake per port; **none records calls** | `test/support/memrepo/*.go` |
| `store_api.golden` lists one line per exported `internal/store/sqlite` method; regenerated by `make store-api-golden` | `testdata/schema/store_api.golden:1-2` |

**`m4a` names zero new calibratable constants.** N is `focus.DefaultSize`; the digest window is
`brain.digestHistoryDays`; every other number the view shows is a value read from the vault.
§13 is untouched.

---

## 2. What `m4a` decides, in one paragraph

`m4a` owns **how the UI is built and gated** (templ pinned by `go.mod`'s `tool` directive, a
clean-tree gate in CI and `check-all`, a `ui-boundary` allow-list, §3.8), **where the UI hangs
on the HTTP surface and in what order the middleware runs** (a UI subtree on the open mux,
wrapped headers → cross-origin → cookie, §3.2), **what the cookie is** (the token itself,
base64url-encoded, a session cookie scoped to `/ui`, constant-time compared, `Secure` from
`r.TLS`, §3.3), **what the browser may not do cross-origin** (`http.CrossOriginProtection` on
the whole subtree, §3.4), **what every UI response says about itself** (a fixed header set with
a `'self'`-only CSP and an external stylesheet, §3.5), **what Today is** (one `brain.Today`
struct from one clock read, Priority-only top-`DefaultSize` per Kind, the digest as `Carry`
would carry it, six status lines, read-only, §3.6), **the one new read** (§3.7), and **what
`--no-ui` removes** (§3.9). It decides no hysteresis, no adjacency, no htmx interaction, no
logout, no second auth path, and no view beyond Today.

---

## 3. Decisions

### 3.1 `internal/ui`: what it holds, what it may import, and who owns the read-model contract

```
internal/ui/
├── doc.go                 exists; charter sentence gains "renders view models, decides nothing"
├── ui.go                  Deps, Serving, New, (*Handler).ServeHTTP                    PR 2
│                          TodayReader, Deps.Today                                     PR 7
├── assets.go              //go:embed static/*; Assets() http.Handler                 PR 2
├── login.go               LoginView, RenderLogin                                     PR 4b
├── layout.templ           Page(title, body) — <html>, <head> with CSP-compatible     PR 1
│                          meta htmx-config, <script src=/ui/static/htmx.min.js>; nav lands in PR 7
├── layout_templ.go        generated, committed, linguist-generated                    PR 1
├── today.templ            Today(brain.Today, Serving) — FOCUS / PENDING DIGEST / SYSTEM PR 7
├── today_templ.go         generated                                                  PR 7
├── login.templ            Login(LoginView)                                           PR 4b
├── login_templ.go         generated                                                  PR 4b
└── static/
    ├── app.css            ADR-0018's six layers, tokens, light-dark()                PR 2
    ├── htmx.min.js        vendored htmx 2.x, reported not budgeted                   PR 2
    └── htmx.LICENSE       the release's licence text (0BSD)                          PR 2
```

**The `ui-boundary` depguard rule is an allow-list** (proposal §4.1, and `core-purity`'s and
`ports-purity`'s shape rather than `scheduler-boundary`'s deny-list, whose own comment at
`.golangci.yml:149-156` records what forgetting one entry costs):

```yaml
ui-boundary:
  files:
    - "**/internal/ui/**"
  allow:
    - $gostd
    - github.com/rengo/nooma/internal/core
    - github.com/rengo/nooma/internal/brain
    - github.com/rengo/nooma/internal/ports
    - github.com/rengo/nooma/internal/ui
    - github.com/a-h/templ
  deny:   # redundant under an allow-list; present so the failure names the rule
    - pkg: github.com/rengo/nooma/internal/store
      desc: "the UI never touches a port or a store; brain does — docs/06-harness.md §1"
    - pkg: github.com/rengo/nooma/internal/httpapi
      desc: "the UI is mounted by httpapi, never the reverse"
    - pkg: github.com/rengo/nooma/internal/config
      desc: "the UI receives facts, it does not read configuration"
    - pkg: database/sql
      desc: "only internal/store speaks SQL"
    - pkg: os
      desc: "the UI reads embedded assets, never the filesystem or the environment"
    - pkg: os/exec
      desc: "the UI runs no process"
```

`net/http` stays legal — handlers are what the package is. `providers`, `channels`, `scheduler`
are unreachable by omission from `allow`; the four explicit denials are the ones whose message
a contributor is most likely to need.

**Does `ui` import `brain` directly, or only an interface it declares?** Both, and the split is
the decision:

| Option | Verdict |
|---|---|
| `ui` imports `*brain.TodayService` and calls it | **Rejected.** Every `ui` test would construct a real service over seven fakes (a fake clock and six port fakes) to render one page; the view's tests would be the service's tests a second time |
| `ui` declares the view-model struct and `brain` imports `ui` to fill it | **Rejected.** Reverses the edge (`brain-boundary` should not know rendering), and `ui → brain → ui` is a cycle the compiler refuses |
| **`ui` owns a narrow behavioural interface; the view-model type lives in `brain`** — chosen | `type TodayReader interface { Today(ctx) (brain.Today, error) }` is declared in `ui`, satisfied by `*brain.TodayService`, and stubbed in `ui` tests with a fixed `brain.Today`. The struct is the service's *output*, exactly as `CheckReport` is `CheckService`'s; `ui` imports `brain` only for its output types, never its services |

**PR 2's shell state — what `ServeHTTP` renders before `TodayReader` exists.** `Deps`, `Serving`,
`New` and `(*Handler).ServeHTTP` land in PR 2; `TodayReader` and `Deps.Today` land in PR 7, the
same PR that creates `brain.Today` itself (§4's tree, §7's PR 6 and PR 7 rows) — PR 2 cannot hold
a field typed against a struct two PRs away. From PR 2 through PR 6, `(*Handler).ServeHTTP`
renders `layout.Page` with an empty `<main>` holding one paragraph, *"Today arrives in a later
PR"*, and no SYSTEM section: the shell, not a stub of Today's own markup. `TestHandlerServesAPIRootAndUIShell`
(§8) is this state's test: layout present, the five security headers present, no vault-shaped
content anywhere in the body. §3.2's "renders Today unconditionally" names the PR 7+ steady
state, not PR 2's.

**The two process facts SYSTEM needs are not the brain's.** "Effective bind" and "is the UI
behind a cookie" are properties of the listener and the token, known to `cmd/nooma/serve.go`
at wiring time and to no repository. They reach the view as a static `ui.Serving{Bind string,
CookieAuth bool}` handed to `ui.New` — never through `brain`, which would put transport facts
in a package whose depguard rule denies transport for a reason.

### 3.2 The mount and the middleware chain

```
mux (open)
├── GET /{$}                       API root, unchanged
├── /ui, /ui/                      → uiSubtree            (only when d.UI != nil)
└── /                              → requireToken(guardedMux), unchanged

uiSubtree = securityHeaders( crossOrigin.Handler( uiMux ) )

uiMux (http.ServeMux) — one mux; every route is an explicit method+path leaf,
                        never a bare-trailing-slash or "..." subtree, so none
                        of them can trigger ServeMux's own subtree-root redirect
├── GET  /ui/static/app.css        ui.Assets()                              open
├── GET  /ui/static/htmx.min.js    ui.Assets()                              open
├── GET  /ui/static/htmx.LICENSE   ui.Assets()                              open
├── GET  /ui/login                 loginPage        (only when Token != "")  open
├── POST /ui/login                 loginSubmit      (only when Token != "")  open
├── GET  /ui                       requireCookie(Token)( d.UI )              guarded
└── GET  /ui/{$}                   requireCookie(Token)( d.UI )              guarded — same
                                    handler as the line above, so a client that keeps
                                    the trailing slash also lands on Today, with no
                                    redirect either way
```

**Landing order.** This is the steady-state mux, mid-`m4a`: the three `/ui/static/*` leaves and
the two guarded `/ui`/`/ui/{$}` leaves land in PR 2; the two `/ui/login` leaves land in PR 4b,
registered in the same commit as `loginPage`/`loginSubmit` (§7) — PR 2 cannot register a pattern
whose handler does not exist yet. Between PR 4a (which wraps the guarded leaves in
`requireCookie`) and PR 4b, `requireCookie`'s redirect already points at `/ui/login`, which 404s
until 4b lands — one PR apart, named as N2 (§12).

`d.UI` (`*ui.Handler`) no longer runs its own routing: both guarded patterns above dispatch
straight to it, so its `ServeHTTP` renders Today unconditionally from PR 7 onward — through PR 6
it renders §3.1's shell state instead, `TodayReader` not existing yet. "Everything else 404" is
the mux's own default now, not a branch `ui.Handler` has to implement, one line closer to §3.1's
"decides nothing".

**Why every UI pattern is a leaf, never a subtree — the redirect `uiOpen` used to hide.** The
mount above replaces a design where the OUTER mux registered `/ui` and `/ui/` correctly (both
forms present, so neither redirects — `go doc net/http.ServeMux`'s own escape hatch: "This
behavior can be overridden with a separate registration for the path without the trailing
slash"), but forwarded the request UNCHANGED to a second, INNER `http.ServeMux` (`uiOpen`) that
registered the guarded route as bare `/ui/` — a subtree pattern, "registered using a trailing
slash" in the doc's own words — with no sibling `/ui` registration on that inner mux. Verified
by probe: that inner mux's own built-in trailing-slash redirect fired on `GET /ui`, answering a
raw `307 Location: /ui/` **before `requireCookie`, before any handler this design wrote, ever
ran** — the guarded view was reachable, unauthenticated, through a hop nothing here decided. The
fix collapses the two nested muxes into one and gives the guarded route both of its exact forms
(`GET /ui` and `GET /ui/{$}`) as leaf patterns — `{$}` is documented as matching only the exact
end of the URL, not as the trailing-slash/`...` form the redirect rule fires on — so neither
pattern is subtree-shaped and neither can trigger the redirect. This is the "explicit
method+path leaf patterns on one mux" option rather than patching the old inner mux with a
second, `/ui`-exact registration beside its `/ui/` subtree: collapsing removes the third layer
(`uiOpen` → `d.UI`'s own internal routing) entirely, rather than leaving two routers that both
have to agree on the same leaf/subtree distinction to stay correct.

**`/ui/static` is three exact leaf patterns, never a subtree — corrected, not audited.** An
earlier draft mounted `/ui/static/{file...}` with `http.FileServerFS` over the embedded tree and
called the result "audited, not a bug": a `"..."` wildcard is a subtree by §3.2's own rule, so
`GET /ui/static` (no trailing slash) redirects 307 to `/ui/static/` — the same subtree-redirect
class §3.2 already rejects for `/ui` itself. Verified by probe: that construction's real failure
is a 404 on every request, real files included, because `http.FileServerFS` reads a request's
file path from `r.URL.Path` and never from the `{file...}` wildcard's `PathValue`, so every
lookup still carries the `/ui/static/` prefix the embedded sub-tree does not have — the 200
directory listing the earlier draft's prose actually described belongs to a different
construction it conflated this one with, the classic `http.StripPrefix("/ui/static/",
http.FileServerFS(sub))` subtree, which does trim the path and, with `static/` holding no
`index.html`, answers 200 with an HTML listing of `app.css`, `htmx.min.js` and the licence text.
The assets are compiled into the binary, not vault data, so neither failure leaks a secret, but
neither is a response this design chose to serve. The fix drops `{file...}` and
`http.FileServerFS` entirely:
`ui.Assets()` returns a small `*http.ServeMux` carrying exactly the three known, embedded leaf
patterns above (`GET /ui/static/app.css`, `GET /ui/static/htmx.min.js`, `GET
/ui/static/htmx.LICENSE`), each an exact literal with `http.ServeFileFS` behind it, exactly like
`/ui/login`'s pattern shape. With no wildcard registered, neither `/ui/static` nor `/ui/static/`
is a pattern at all — both answer the mux's ordinary unmatched-path 404, verified by probe, with
no redirect and no listing. Three files is few enough that naming them costs nothing and keeps
the same "no bare-trailing-slash, no `...`" property §3.2 already holds every other leaf to.

**Why the UI subtree hangs on the open mux and not behind `requireToken`.** `requireToken`
reads the `Authorization` header and nothing else (`auth.go:60-63`); a browser navigation
cannot carry one (ADR-0007's own "detail that was not obvious"). Putting `/ui` behind it would
make the UI unreachable from the thing it exists for. The cookie check is the UI's
`requireToken`, and it sits at the same depth: `requireCookie` wraps only the two guarded
leaves (`GET /ui`, `GET /ui/{$}`) on `uiMux`, with the open routes (`login`, `static`)
registered beside them, unwrapped — the **same open/guarded split `Handler` already has at its
own top level**, applied to `/ui`'s leaves directly rather than through a second nested mux.
There is no exported way to reach `d.UI` without `requireCookie` having run, which is the
property `TestGuardedRoutesRequireToken` proves for the API and `TestUIViewsRequireCookie` (§8)
proves here.

**Why `login` and `static` are open.** `/ui/login` is where the cookie comes from; a guarded
login page is a locked door with the key inside. `/ui/static` serves the stylesheet the login
page needs, and the assets are compiled into the binary — they are not vault data. Both are
still under `securityHeaders` and `crossOrigin`: the chain is complete from the first
response (proposal §4.1). `spec.md` R1's MUST is amended alongside this design to name both open
routes — "except the handshake screen and the static asset route" — rather than the handshake
alone, so the requirement and this design agree on what a token gates.

**Order: headers, then cross-origin, then cookie.** Headers outermost so that every UI
response — a 403 from cross-origin, a 303 from the cookie check, a 404 from the mux — carries
them. Cross-origin before the cookie check so a foreign-origin `POST /ui/login` is refused
before the token in its body is ever compared; the cheapest refusal runs first.

**`TestOpenRoutesStayOpenRegardlessOfToken`'s `/ui` leg is inverted in PR 4a's RED commit**
(proposal §6 order 2): with a token configured and no cookie, `GET /ui` answers **303 to
`/ui/login`**, carries no vault data, and sets no cookie; `GET /` stays at 200 unchanged. The
test is renamed `TestOpenRoutesAndUIRoutesUnderAToken` in PR 4a. **Its third leg — `GET
/ui/login` is 200 with no `Set-Cookie` — is added in PR 4b's RED commit, not 4a's**: `loginPage`
is PR 4b's GREEN, so a third leg asserting a 200 from it would leave PR 4a's tip red, which
`main`'s required checks and zero-bypass ruleset forbid. PR 4a's own RED/GREEN pair proves only
the 303 and the no-cookie state the cookie middleware itself owns. The `no Set-Cookie on GET`
assertion is kept on every GET leg — only `POST /ui/login` with the right token may set one.

**Method posture on a guarded view — corrected.** An earlier draft of this design claimed "any
other method on a guarded view → 401, empty body" as a branch inside `requireCookie`. Verified by
probe: both guarded patterns (`GET /ui`, `GET /ui/{$}`) are registered as method-specific leaves,
so a `POST` to either never reaches `requireCookie` — `uiMux` itself answers **`405 Method Not
Allowed`, `Allow: GET, HEAD`**, before any handler this design writes ever runs, the same
mechanism `go doc net/http.ServeMux` documents for any method-specific pattern with no match on
method. That is the correct HTTP answer for a method no view route declares, not a case
`requireCookie` should intercept, so `requireCookie` carries no "any other method" arm — there is
nothing for it to delete because m4a never wrote it as reachable code, and this design's prose no
longer claims one. `GET`/`HEAD` on a guarded view with no cookie still answers `303 See Other,
Location: /ui/login` (a navigation is sent where the key is); missing cookie and wrong cookie stay
byte-identical in that one arm — `requireToken`'s own MUST NOT. m4a registers no non-GET pattern
under a guarded view, so this design states no 401 posture for `requireCookie` on a non-GET
request at all; the first guarded route in a later slice that accepts a method besides GET/HEAD
decides its own unauthenticated-method answer when it is written — 401 is one option there, not a
rule m4a established.

**When `Token == ""`** (loopback, no token — `DecideBinding` guarantees this state is
reachable nowhere else): `requireCookie` is a no-op exactly as `requireToken` is, **and the
two `/ui/login` routes are not registered at all**. "No screen, no cookie" is then a property
of the mux, not a branch inside a handler: `GET /ui/login` is a 404. `nooma status`'s
`ui:` line is unchanged; `Serving.CookieAuth` is `Token != ""`.

### 3.3 The cookie — Q1 ruled, and the four decisions the ruling leaves

```go
// internal/httpapi/cookie.go

// uiCookieName is the one cookie this binary sets. It says what it holds.
const uiCookieName = "nooma_token"

// setUICookie issues ADR-0007's session cookie. The value IS the token
// (owner ruling Q1; ADR-0028), base64url-encoded so that no byte a
// token may contain is one net/http's cookie sanitiser would silently
// drop. No Max-Age, no Expires: "session cookie" means what it says, and
// a lifetime would be a number nobody calibrated.
func setUICookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiCookieName,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(token)),
		Path:     "/ui",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	})
}

// requireCookie is requireToken's sibling for the UI: same no-op on an
// empty token, same constant-time comparison, same byte-identical refusal
// for "missing" and "wrong". It differs in what it reads (the cookie, never
// the header — one presentation per surface) and in how it refuses a
// GET/HEAD navigation with no cookie: 303 to the screen. m4a's guarded
// routes accept no other method — a non-GET request never reaches this
// middleware, the mux answers 405 first (§3.2) — so there is no second arm
// here to write.
func requireCookie(token string) func(http.Handler) http.Handler
```

| Decision | Choice | Alternatives rejected |
|---|---|---|
| **Value** | The token, `base64.RawURLEncoding` | *Raw token*: net/http drops `;`, `,`, `"`, `\`, space and control bytes from a cookie value with a log line nobody reads; a token containing one would fail every login with no error naming why. *Opaque session id*: Q1 rejected it — a table with no owner |
| **Name** | `nooma_token` | `__Host-`/`__Secure-` prefixes require `Secure`, which a plain-HTTP LAN bind cannot set; a name that lies about its contents is worse than a plain one |
| **`Path`** | `/ui` | `/` would present the secret to every API request too, where it is never read. RFC 6265 path-match is `/ui` and `/ui/…`, never `/uix` |
| **`Secure`** | `r.TLS != nil` — the request's own truth | A `server.tls`/`behind_tls` key: a knob whose wrong value either drops the flag silently or makes the cookie unusable over HTTP. `r.TLS` is structural. **Honest consequence**: `serve.go` never terminates TLS today (`ListenAndServe`), so `Secure` is `false` on every current deployment, which is ADR-0007's documented and accepted LAN posture; a TLS listener flips it with no code change here |
| **Lifetime** | Session cookie, no `Max-Age`/`Expires` | Proposal §4.1's own decision |
| **Comparison** | `subtle.ConstantTimeCompare(decoded, token)` | `==` leaks timing; comparing the encoded forms would be fine too but decoding first keeps `requireToken` and `requireCookie` comparing the same bytes |
| **A cookie value that fails to decode** | Treated exactly as a wrong token, never as a distinct case | An early `return unauthorized` on the `base64.RawURLEncoding.DecodeString` error would make "malformed" a third, faster-answering outcome — a timing and code-path oracle telling an attacker their cookie failed to decode rather than failed to match |
| **Logout** | **None in m4a** | A `POST /ui/logout` clearing the cookie is ~15 lines and a mutation with no vault effect. It has no user story until the UI has something to walk away from; closing the browser ends a session cookie, rotating `server.token` ends every cookie (Q1). Owner-review item OR1; the natural home is m4e's admin view |
| **Body bound on the login POST** | `http.MaxBytesReader(w, r.Body, 4096)` before `ParseForm` | A token is short; an unbounded form parse on an open route is the one denial-of-service this PR would otherwise introduce |

**A cookie that fails to decode is a wrong cookie, not a third outcome.** `requireCookie` reads
its cookie through `presentedSecret` (`cookie.go`), which decodes `base64.RawURLEncoding` and
always returns exactly `len(token)` bytes — the decoded value when it decodes to that length, a
fixed-length zero value otherwise — and returns no error and takes no `http.ResponseWriter`.
That signature is what makes an early return on the decode error **unexpressible inside
`presentedSecret` itself**, not merely avoided there: `presentedSecret` has nothing of its own to
answer an error with. It says nothing about `requireCookie`'s own body, though — a caller can
still re-derive the same fact (call `r.Cookie` or decode the cookie value a second time, ahead of
calling `presentedSecret`) and branch on it with an early exit of its own; two independent
Judgment Day reviewers each reproduced exactly that against an earlier version of this gate.
What forbids it is a second, control-flow rule the same conformance test also enforces: inside the
per-request `http.HandlerFunc` literal `requireCookie` (and `requireToken`) return, the only
`return` statement permitted is the one guarded by the `subtle.ConstantTimeCompare` comparison —
any other early exit fails the gate, whatever fact it branches on.
`TestUIViewsRequireCookie` (§8) gains an explicit case for a cookie value that is
not valid base64url, and a second explicit case for a wrong cookie the same decoded length as the
real token (exercising the full-length comparison rather than a length-mismatch short-circuit),
both asserting the same 303/401 arms as "wrong" — the byte-identical-over-HTTP half of the claim,
the only half a response recorder can observe. It does **not** by itself prove the comparison
still runs in constant time: a mutation confined to `presentedSecret`'s or a helper's own body, or
one that adds an early return elsewhere in `requireCookie`'s handler, produces that exact same
byte-identical response, and only timing (or a code path that never reaches the compare) would
differ, which no response-level test measures. The structural half is proven instead by
`test/conformance/httpapi_secret_compare_test.go` (§8), in three parts: `presentedSecret`'s
signature check makes the decode-error branch unexpressible inside `presentedSecret`; a transitive
walk over same-package calls from `requireCookie` and `requireToken` proves
`subtle.ConstantTimeCompare` is still reached — soundly under in-package helper extraction in
either direction, unlike an earlier version of this gate that inspected only each function's own
body (Judgment Day round 1/2: a helper hiding the bug escaped it, and a helper hiding a *correct*
comparison falsely failed it); and the control-flow rule proves the handler carries no other early
return (Judgment Day round 3: the shape above). None of the three measures real elapsed time —
they prove the structure timing-safety depends on, never the timing itself.

**The handshake.** `GET /ui/login` renders `ui.Login(LoginView{Rejected: false})`, status 200.
`POST /ui/login` reads `token` from the form, compares, and on success sets the cookie and
answers `303 See Other, Location: /ui`; on failure re-renders the form with `Rejected: true`
at **401** — the same body for an empty field and a wrong value, because both reach the same
comparison. The seam between `httpapi` and `ui` is exactly two calls:
`ui.RenderLogin(ctx, w, view)` and the mount of `ui.Assets()`. `httpapi` owns the route, the
comparison and the cookie; `ui` owns the markup. `httpapi` imports `ui`; `ui` cannot import
`httpapi` (§3.1's rule).

### 3.4 Cross-origin protection — Q2 ruled, and one correction to its wording

```go
// in Handler, once:
xo := http.NewCrossOriginProtection()
uiSubtree := securityHeaders(xo.Handler(uiMux))
```

Constructed once, no trusted origins, no bypass patterns, default deny handler (403). It wraps
the **whole** UI subtree, so `POST /ui/login` in m4a and every m4b form are covered by the same
line, and a route added later cannot forget it.

**Correction to proposal Q2's stated cost.** The stdlib implementation does **not** refuse a
browser that sends neither `Sec-Fetch-Site` nor `Origin`; it *allows* it, on the reasoning
that such a request is same-origin or not a browser (`csrf.go:154-159`). What it refuses is:
`Sec-Fetch-Site` present and not `same-origin`/`none`; or `Sec-Fetch-Site` absent and `Origin`
present with a host that differs from `Host`. The accepted residual is therefore a pre-2023
browser that sends `Origin` — which is refused when cross-site — and a client that sends
neither, which is not a browser. That is a narrower cost than the proposal wrote, not a wider
one, and the L1 test in §8 asserts all three arms so nobody re-derives them.

**Why not also on the API's `POST /capture`/`/recall`.** Proposal Q2 leaves it open, and this
design does not answer it: the API is ADR-0017's, its clients are `curl` and the CLI, which send
neither header and pass anyway, and a browser posting `text/plain` to it cross-site is the same
exposure it has today. Named, not changed.

### 3.5 Security headers and the CSP — where the stylesheet lives decides the policy

Set by `securityHeaders`, one function in `internal/httpapi/headers.go`, on every response
under `/ui` including redirects, refusals and static assets:

| Header | Value | Why this value |
|---|---|---|
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'` | No inline script, no inline style, no eval. htmx 2 runs under this with `allowEval` off; the m4d island must fit under it or the island is wrong (proposal §4.1) |
| `X-Content-Type-Options` | `nosniff` | The stylesheet and the script are served by content type; sniffing is what turns a vault string into a script |
| `Referrer-Policy` | `same-origin` | A unit's text can be a URL; the referrer must never carry `/ui` paths off-host |
| `X-Frame-Options` | `DENY` | `frame-ancestors 'none'` for CSP-aware browsers, this for the rest; both, because they are free |
| `Cache-Control` | `no-store` on views; `no-cache` on `/ui/static/*` | A view is vault data and must not survive in a shared browser's cache or back-forward store. Assets change only with the binary and carry no version in their URL, so they revalidate — `embed.FS` has no `ModTime`, so `http.ServeFileFS` sends no `Last-Modified` either (verified by probe); `no-cache` is what keeps a stale `app.css` from outliving an upgrade |

**`style-src` posture: external `/ui/static/app.css`, and nothing else.**

| Option | Verdict |
|---|---|
| `'unsafe-inline'` | **Rejected.** Reopens the one hole the header exists to close, for a stylesheet that is one file anyway |
| A per-request nonce | **Rejected.** Buys inline `<style>` at the cost of plumbing a nonce through every template and a `context.Context` value every component reads; ADR-0018 already put the CSS in one embedded file, so there is nothing inline to license |
| **External `<link rel=stylesheet href=/ui/static/app.css>` under `style-src 'self'`** — chosen | ADR-0018's decision, served at one route. "Embedded" in ADR-0018 means `go:embed`, and a static route over an `embed.FS` is exactly that |

**Two htmx details that the CSP forces, both in `layout.templ`'s `<head>`:**
`<meta name="htmx-config" content='{"allowEval":false,"includeIndicatorStyles":false}'>`.
`allowEval:false` disables `hx-on`/`hx-vars` evaluation, which is the only thing in htmx that
would need `'unsafe-eval'`; `includeIndicatorStyles:false` stops htmx injecting a `<style>` for
`.htmx-indicator` at boot, which `style-src 'self'` would block with a console error and which
`app.css` carries instead. Neither is a workaround; both are htmx's own documented CSP mode.

### 3.6 The Today read model — Q3 ruled, and what the mirror must not do

```go
// internal/brain/today.go

// TodayService assembles the Today view model — the mirror's first read
// model (proposal §3.1's boundary rule). CheckService's shell/worker shape:
// this struct holds the one ports.Clock; todayRunner never sees it.
type TodayService struct {
	clock ports.Clock
	run   todayRunner
}

func NewTodayService(clock ports.Clock, units ports.UnitRepo, cfg ports.ConfigRepo,
	state ports.StateRepo, triggers ports.TriggerRepo, questions ports.PendingQuestionRepo,
	log ports.DecisionLog) *TodayService

// Today builds the view model at one instant. This file's one Now() call.
func (s *TodayService) Today(ctx context.Context) (Today, error) {
	return s.run.at(ctx, s.clock.Now())
}

// Today is what GET /ui renders. Every field is a value the brain already
// decided or a row the vault already holds; nothing here is computed by
// the template, and nothing here is written back.
type Today struct {
	Now     time.Time
	Focuses []Focus          // focus.AllKinds() order: task, then load
	Digest  PendingDigest
	Status  VaultStatus
}

type Focus struct {
	Kind    focus.Kind
	Members []FocusMember    // rank order, at most focus.DefaultSize
}

// FocusMember is one ranked unit with the text a view needs and the
// dates I18 says must never be confused. Score is the literal value
// focus.Rank produced — NaN included — never coerced (doc 02 §3) — and
// today.templ renders it, two decimals, NaN as the literal "NaN" (Q7,
// ruled — §11).
type FocusMember struct {
	ID      string
	Type    unit.Type
	Content string
	DueAt   *time.Time
	EventAt *time.Time
	Score   float64
}

// PendingDigest is what the morning delivery would carry if it went out
// at Now — prospection.Carry's own output over the same inputs
// assembleDigest reads, and NOTHING is marked delivered or asked.
type PendingDigest struct {
	LowEnergy bool
	Items     []DigestLine            // Carry's carry slice, in its order, joined to pending by ID
	Held      int                     // len(held): counted, never listed
	Question  *ports.RelationQuestion // Unasked()[0] when !LowEnergy, else nil
}

type DigestLine struct {
	TriggerID string
	UnitID    *string
	Text      string    // TriggerPayload.ActionText
	FireAt    time.Time
	Deferrals int
}

// VaultStatus is the four SYSTEM lines the vault can answer. The other
// two (bind, cookie auth) are the listener's facts and reach the view
// from cmd/nooma through ui.Serving, never through this package.
type VaultStatus struct {
	LastConsolidationAt *time.Time                 // nil renders "never"
	Energy              *prospection.EnergyReading // nil renders "no reading"; now carries Source
	Undelivered         int
	OpenQuestions       int
}
```

**How the source reaches the view — R5 is ruled, not dropped.** `prospection.EnergyReading`
gains one field, `Source string`, holding the literal value `current_state.source` already
stores (`StateSourceUser`/`StateSourceConsolidation`, `ports/staterepo.go:18-19`); the field
carries the raw string, never the `ports` constant, so `internal/core/prospection` imports
nothing new and stays pure. `staterepo.go`'s `LatestEnergy` SQL adds `source` to its `SELECT`
and to its `Scan`; `LatestEnergy`'s signature — `(ctx) (*prospection.EnergyReading, error)` — is
untouched, so this is a core-struct widening, not a port change, and `§3.3`'s "the only port
change this slice makes" still names exactly one method. `VaultStatus.Energy`'s type is
unchanged; the view reads `.Source` off the same pointer it already had.

| Alternative | Verdict |
|---|---|
| **Widen `prospection.EnergyReading` with `Source string`** — chosen | The port's method signature never changes; every existing caller (`brain/digest.go:52`) compiles unmodified; `internal/core` takes no new import |
| A second return value, `LatestEnergy(ctx) (*prospection.EnergyReading, string, error)` | **Rejected.** A port signature change for a value only one caller (Today) reads; every other call site carries a parameter it discards |
| A new `StateRepo` method (e.g. `LatestEnergySource`) | **Rejected.** A second near-duplicate read over the same row, which is exactly the drift `§3.3`'s "no other port change" rule exists to prevent |

**`todayRunner.at(ctx, now)` — the read order, and why each read is there:**

```
1. cfg.Load                                  → Status.LastConsolidationAt
2. state.LatestEnergy                        → Status.Energy (Level, RecordedAt, Source);
                                                 low := prospection.LowEnergy(energy, now)
3. triggers.Undelivered                      → pending;  Status.Undelivered = len(pending)
4. log.Since(now - digestHistoryDays, -1)    → history (deferral counts, heldCounts' own source)
5. digestItems(ctx, units, pending, history) → items   (units.LiveFocusCandidates inside it)
   carry, held := prospection.Carry(items, map[string]float64{}, low, now)
   Items = joinDigestLines(carry, pending)      ← carry is []DigestItem{ID, Candidate,
                                                   Deferrals} (internal/core/prospection/
                                                   digest.go:131-135); it carries no
                                                   text, FireAt or UnitID. Joined back to pending
                                                   []ports.DueTrigger by ID, mirroring
                                                   renderDigest's own text[t.ID] lookup
                                                   (brain/digest.go:269-271): pending's Payload
                                                   .ActionText, FireAt and UnitID fill each
                                                   DigestLine; Deferrals is carry's own
6. questions.Unasked   (only if !low)        → Question = &unasked[0] or nil
7. questions.Open                            → Status.OpenQuestions = len(open)
8. for k in focus.AllKinds():
     cs := units.LiveFocusCandidatesByType(ctx, focus.Types(k))      ← the ONE new read
     ranked := focus.Rank(cs, map[string]float64{}, now)
     top := ranked[:min(focus.DefaultSize, len(ranked))]
     us  := units.LiveByIDs(ctx, ids(top))                            → content, in rank order
```

Nine port methods, eleven calls, one instant. **The proposal counted six existing reads plus
one; this is eight plus one** — `spec.md`'s scope boundary and R5 heading are amended to the
correct count — and the two it missed are the two that make the view a mirror rather than a
re-derivation: `LiveFocusCandidates` (through `digestItems`) is what gives `Carry` the same
candidates the digest would rank, and `LiveByIDs` is the only way to put a unit's text beside a
`focus.Candidate` that deliberately carries none.

**`digestItems` becomes a package function** `digestItems(ctx, units ports.UnitRepo, pending,
history)` with `checkRunner.digestItems` deleted and its one call site updated. The
alternative — a second `digestItems` on `todayRunner` — is the same rule in two places, which
is how the digest's Carry order and Today's Carry order would drift apart. `heldCounts` is
already a package function and needs nothing.

**Priority-only, and the ruling's own words.**

| Option | Verdict |
|---|---|
| `focus.Select(k, ranked, focus.Selection{Kind: k}, 0, focus.DefaultSize)` | Produces exactly the same members (`select.go:91-98`: empty `previous` and margin 0 degenerate to top-`size`) and would give `Select` its first production caller. **Rejected by ruling** (proposal §3.3: *"It does not call focus.Select"*), and for a reason the ruling did not need to state: I19's first production caller would be a call that passes margin 0 — an invariant's first load being the call that disables it is the wrong debut. m4c gives `Select` its first caller with `ResolveMargin(cfg.HysteresisMargin)` and a real incumbent |
| **`focus.Rank` + `[:focus.DefaultSize]`** — chosen | The ranking is core's; N is core's constant; the slice bound is not a decision. The port already filtered by `focus.Types(k)`, so no type predicate lives in `brain` either. Doc 02 §3's post-restart state as steady state for one slice, stated in the doc amendment below |

**Accepted and stated (proposal §3.3, R6):** between m4a and m4c, two requests can disagree on
membership on a priority gap smaller than `hysteresis_margin`, and `relation_to_active_focus`
is 0 for every unit. Nothing is persisted to hide it — that would be I01.

**Two things about "what the digest would carry" that are true and must be written down.**
`Carry` is evaluated at the request's `now`, not at `DigestHour`; a low-energy reading that
expires between the request and 07:00 changes the answer, and the view says "if it went out
now". And `DigestDue` is not consulted: after a morning digest has gone out, `Undelivered`
holds what it held back, and Today shows that — which is the honest state of the queue, not a
preview of tomorrow.

**Held items are counted, never listed.** `renderDigest`'s own rule (`digest.go:265-267`) — a
message that lists what it withheld defeats the gate that withheld it — applies to the mirror
too; but a mirror that hid the existence of held items would misreport the queue, which is the
milestone's named failure mode. `Held int` is the middle: the fact without the list. Owner-review
item OR2, with "do not mention held at all" as the alternative.

**The question slot follows the digest's rule exactly**: `Unasked()[0]` when `!low`, nothing
otherwise — `nextQuestion`'s own body. An already-asked, unanswered question is *not* listed
here: the next digest will not carry it, and listing it would be listing a thing the user cannot
yet act on from this surface (answering from the UI is m4f). It is counted in
`Status.OpenQuestions`. Owner-review item OR3: Q3's wording "the unasked or open question" is
read as "the question the next digest would carry"; the alternative is a second slot showing
`Open()[0]` labelled *awaiting your answer*.

**Viewing is not delivering — the guarantee, stated as the port methods the GET must not call.**
`todayRunner.at` calls no method whose name is `Surface`, `Fire`, `Expire`, `Resolve`,
`Create`, `MarkAsked`, `Confirm`, `Reject`, `Record`, `RecordConsolidationRun`,
`OpenHypothesis`, `SetStatus`, `ApplyBoosts`, `UpdateContent`, `UpdateEventAt`, `UpdateDueAt`.
A read-only view writes no `decision_log` row either: I12 covers *automatic decisions with an
effect*, and a GET has neither. The test that proves it is §8's `writeGuard` fake — a wrapper
that embeds each `memrepo` fake and fails the test on any of those names — plus a
state assertion that `Undelivered()` and `Unasked()` return the same rows after the call as
before. That is **I27** (§8), and the doc 02 §7 sentence that carries it.

**The service needs no provider.** It reads repositories and calls core; a vault with no LLM
configured renders Today completely. `wireBrain`'s nil-`Capture` state (`serve.go:92-105`) does
not reach here: `NewTodayService` is wired unconditionally at vault open, from the same
unconditional call site as `wireBrain` (`serve.go:102`) — never inside `wireScheduler`, whose
`resolveConsolidateProviders` gate skips `NewCheckService` on a vault with no LLM bindings
(`wiring.go:413-417`); `wireToday` must not copy that shape.

### 3.7 The one new read — §3.3's signature, decided

```go
// internal/ports/unitrepo.go — UnitRepo gains one method (13)

// LiveFocusCandidatesByType returns every unit.StatusPool unit whose Type
// is among types, as focus.Candidate, ordered by id. An empty types
// returns an empty slice, never an error — LiveFocusCandidates' own
// posture, and what focus.Types(k) returns for an unknown Kind.
//
// Bounded by status and type, never by count: focus.Priority cannot be
// expressed as an ORDER BY (LiveFocusCandidates' doc comment, above), so a
// LIMIT here would drop a high-urgency low-weight unit before the ranking
// ever saw it. The ranking stays in focus.Rank; ORDER BY id exists only so
// two runs over one vault agree.
//
// This is an unbounded read — O(live units of those types) per call, no
// paging — LiveDecayStates' own shape and its own named risk. Doc 02 §3's
// "two queries over one table" are two calls to this method with
// focus.Types(KindTask) and focus.Types(KindLoad); the type filter lives in
// SQL, so no type predicate is re-derived in brain.
//
// types is a []unit.Type, not a status: CountLiveByType's own ruling
// applies — unit.Type has no live/non-live axis, so this does not reopen
// the "no List(status)" rule. The name carries both halves, UnitRepo's own
// convention: Live is the status, FocusCandidates is the shape, ByType is
// the bound.
LiveFocusCandidatesByType(ctx context.Context, types []unit.Type) ([]focus.Candidate, error)
```

SQL: `WHERE status = ? AND type IN (?, …) ORDER BY id` — the **positive** `status = 'pool'`
filter I02 requires, never a negative exclusion. `RunLiveFocusCandidatesByType` in
`repocontract` seeds a `pool` unit of each wanted type, a `pool` unit of an unwanted type, and
an `archived`, a `superseded` and an `incomplete` unit of a wanted type; only the first group
returns. The scanning code is `LiveFocusCandidates`' own, factored into one `scanCandidate`
helper so the two reads cannot disagree about a column.

| Alternative | Verdict |
|---|---|
| Return a wider `{focus.Candidate; Content}` shape and skip `LiveByIDs` | **Rejected.** Ranks over the whole pool but renders at most 14; fetching every live unit's unbounded text to show 14 is the wrong trade, and m2a D9's rule (narrow read shape for the core decision that consumes it) holds one layer up. The two-read race — a unit archived between the reads — is absorbed by `LiveByIDs` omitting it: the view shows one member fewer, and misreports nothing |
| One call with the union of both Kinds' types, filtered per Kind in `brain` | **Rejected.** Puts `focus.Types` inside `brain` as a predicate; doc 02 §3 says *two queries*, and two calls are two queries |
| A `Kind`-typed parameter (`LiveFocusCandidates(ctx, k focus.Kind)`) | **Rejected.** Binds the port to a `focus` vocabulary the store would then have to map to types itself — the mapping is `focus.Types`', and moving it into SQL is one rule in two languages |

`store_api.golden` gains one line; `memrepo` gains one method; no migration.

### 3.8 The templ toolchain and its gates

| Decision | Choice | Why, and what was rejected |
|---|---|---|
| **How `templ` is pinned** | `go.mod`: `tool github.com/a-h/templ/cmd/templ` (Go ≥ 1.24's `tool` directive; `go.mod` is 1.26.4); invoked as `go tool templ generate` | The runtime `github.com/a-h/templ` must be a `require` anyway — generated code imports it. Generated files carry a version header, and a `templ` binary that does not match the runtime produces a **dirty tree by construction**. The `tool` directive makes the binary and the runtime one version in one file. *`go install …@vX` in the Makefile* (golangci-lint's shape) was rejected for exactly that drift; *a checked-in binary* never considered. Cost: `templ`'s own command dependencies land in `go.sum` (not linked into `nooma`; reported, not budgeted) |
| **`go build` on a clean machine** | Unchanged: `_templ.go` committed (ADR-0008) | `go tool` is never invoked by `go build ./...`; `make build` and the seven-target matrix need no `templ` |
| **`make templ`** | `go tool templ generate -path ./internal/ui` | One target, one path |
| **`make templ-clean`** | `templ` then `git diff --exit-code -- 'internal/ui/*_templ.go'` — `schema-golden-clean`'s shape verbatim | Joins `check-all`; **not** `check` (the fast loop stays lint + L1/L2 + build; the proposal's own split) |
| **CI** | One step in the existing `build` job on Linux: `make templ-clean`; `ci.yml:125-127`'s "not enforced" line deleted | A step, not a job: a new job renames nothing and adds a required context to a ruleset that would then wait on it. Linux only — R5: the Windows jobs run `go test` against committed `_templ.go` and need no `templ` |
| **Cross-compile impact** | None | Generated `_templ.go` is plain Go; the `templ` runtime is pure Go with no cgo. The matrix (`CGO_ENABLED=0`) builds it unchanged; the templ step itself never runs under a `GOOS` |
| **Line endings** | Already forced LF by `.gitattributes:15` | The gate is a byte diff and the schema golden already paid this lesson; nothing to add |
| **`.gitattributes`** | `internal/ui/*_templ.go linguist-generated=true`, `internal/ui/static/htmx.min.js linguist-vendored=true` | ADR-0008's own mitigation, one line each |
| **golangci-lint on generated files** | `exclusions.generated: lax` already skips files whose header says `Code generated … DO NOT EDIT`, which templ emits | **Verify in PR 1** that the `gofmt`/`goimports` formatters honour the same exclusion; if not, add a `path: internal/ui/.*_templ\.go` exclusion rule. Named as a risk (N5) rather than assumed |
| **Watching the gate fail** | PR 1 carries `layout.templ` so the gate has a file: hand-edit `layout_templ.go`, run `make templ-clean`, record the failure in the PR body — `brain_single_clock_read_test`'s own "temporary-break check, run and recorded in this PR" | A gate with no file is vacuous; a gate first exercised in PR 2 is a gate PR 1 claimed and did not prove |
| **Watching the boundary rule fail** | PR 1's RED commit adds the rule and a scratch `internal/ui/probe.go` importing `internal/store`; the GREEN commit deletes the probe | The lint failure is the proof; the probe never reaches the tip |

**htmx vendoring (PR 2).** htmx 2.x latest at implementation time, from the GitHub release
asset, with its `LICENSE` beside it and the version + SHA-256 of the asset in a comment at the
top of `assets.go`. ADR-0008's hand-audit obligation is discharged by that record plus the
same scan doc 06 §3 names for the island (no `fetch`/`XMLHttpRequest`/`WebSocket`/`eval` — htmx
uses `XMLHttpRequest` by design, so the scan result is *recorded*, not asserted; htmx is not the
island and is not under the island's gate). ~50 KB minified, reported not budgeted.

### 3.9 `--no-ui` and `server.ui`

```go
// cmd/nooma/serve.go — PR 3's shape (PR 2 wires the unconditional line below with
// no flag and no conditional around it — see "Landing this across PR 2 and PR 3")
noUI := fs.Bool("no-ui", false, "serve the API only; do not mount /ui")
…
uiEnabled := *cfg.Server.UI && !*noUI
var uiHandler *ui.Handler
if uiEnabled {
	uiHandler = ui.New(ui.Deps{Today: today, Serving: ui.Serving{Bind: addr, CookieAuth: token != ""}})
}
httpapi.Handler(httpapi.Deps{…, Token: token, UI: uiHandler})
```

`Deps.UI *ui.Handler`; nil means **unmounted**: `Handler` registers neither `/ui` nor `/ui/`,
so a request falls through to `requireToken(guardedMux)` exactly as `/does-not-exist` does
today (`server.go:100-105`): **404 with no token, 401 with one** — the same answer any unknown
path gets under the same conditions, and `TestUnknownPathIs404`'s own posture. The flag
overrides the key; `server.ui: false` alone has the same effect (`ApplyDefaults` makes the key
non-nil, so the deref is safe). Go's `flag` package stops at the first positional argument, so
the form is `nooma serve --no-ui [vault]`; the usage line says so.

**Landing this across PR 2 and PR 3 — corrected.** An earlier draft of this design put every
line above, including the unconditional `httpapi.Deps{UI: uiHandler}` wiring, at PR 3, with PR 2
building `ui.New` but never calling it from `cmd/nooma/serve.go`. That leaves `d.UI == nil` at
PR 2's tip in the *real binary* — `Handler`'s mux (§3.2) mounts `/ui` only when `d.UI != nil`,
so a compiled `nooma serve` at PR 2's tip would 404/401 on `/ui`, failing
`test/e2e/serve_test.go`'s `TestServeAnswersBothSurfaces` (§1's own ground-truth row for it),
and any `internal/httpapi` test whose `Deps{}` fixture has no `UI` would fail the same way for
the same reason (§7's PR 2 row, §7.2). The fix moves the wiring one PR earlier:

```go
// cmd/nooma/serve.go — PR 2's shape: unconditional, no flag yet
uiHandler := ui.New(ui.Deps{})
httpapi.Handler(httpapi.Deps{…, Token: token, UI: uiHandler})
```

PR 2 calls `ui.New(ui.Deps{})` with no `Today` — §3.1's shell state — and hands the result to
`Deps.UI` unconditionally, so `d.UI` is never nil in the compiled binary from PR 2's tip onward
(until PR 3's flag says otherwise). PR 3 does not introduce the call; it wraps the *existing*
call in the `--no-ui` conditional shown in the code block above — `uiEnabled`, `noUI` and
`Deps.UI`'s nil path are PR 3's whole contribution here, not the handler's construction. PR 7
widens the same call, still in `cmd/nooma/serve.go`, to `ui.New(ui.Deps{Today: today, Serving:
ui.Serving{Bind: addr, CookieAuth: token != ""}})` once `TodayReader` and `NewTodayService`'s
wiring (`wireToday`, PR 6) exist (§4, §7) — `Serving` is wired at the same time as `Today`
because nothing before PR 7 renders it (§3.1's shell has no SYSTEM section).

**Rejected:** a `Deps.UIEnabled bool` beside a always-built handler — a handler that exists and
is not mounted is a branch inside `Handler`; a nil pointer is the absence itself. And mounting a
"UI is off" page — a page is a UI.

### 3.10 ADR-0028 — *the UI cookie carries the token*

**New ADR needed**; not written here. Title: **`0028-ui-cookie-handshake.md` — The UI cookie
carries the token itself; cross-origin UI requests are refused structurally**. `Accepted`, with
`docs/adr/README.md`'s index row, in PR 4a.

Decision: the cookie's value is the configured token (base64url), compared in constant time as
the header is; a session cookie, `Path=/ui`, `HttpOnly`, `SameSite=Strict`, `Secure` from the
request's own TLS state; no session table, no logout in the first slice; every non-safe UI
request passes `net/http.CrossOriginProtection` with no trusted origins.

Alternatives with proposal §8's reasons: an opaque session id (Q1-B: a table with no owner,
"users + sessions" that ADR-0007 rejected); a synchronizer token per form (Q2-B: state per
session, a hidden field in every template, and no protection on loopback with no cookie to bind
to); `SameSite=Strict` alone (Q2-C: zero protection on the default configuration, which
non-negotiable #7 forbids).

Consequences: rotating `server.token` invalidates every cookie; the secret travels in every UI
request, which under the three flags is the exposure the header already has (ADR-0007's own
accepted LAN cost); a pre-2023 browser that sends `Origin` cross-site is refused, one that sends
neither header is admitted — `csrf.go`'s documented stance, recorded rather than assumed.

Why an ADR and not a "Related decision" inside an existing one: ADR-0007 and ADR-0017 are
`Accepted` and never edited; Q1 and Q2 each had two live alternatives; and ADR-0017 set the bar
— the API's header got its own ADR, the UI's cookie is its exact sibling.

---

## 4. Package layout and dependency map

```
internal/ui/                    §3.1 tree                                        PR 1, 2, 4b, 7
internal/httpapi/
├── server.go                   Deps.UI; the UI subtree mount; placeholder gone  PR 2, 3, 4a
│                              (PR 4a wraps the two guarded leaves in requireCookie;
│                              Today/Serving reach ui.New through cmd/nooma/serve.go
│                              — PR 3, 7 — and wiring.go — PR 6 — never through this file)
├── headers.go                  securityHeaders                                  PR 2
├── cookie.go                   uiCookieName, requireCookie                       PR 4a
│                              setUICookie, loginPage, loginSubmit                PR 4b
└── crossorigin_test.go …       (tests, §8)
internal/ports/unitrepo.go      + LiveFocusCandidatesByType                      PR 5
internal/store/sqlite/unitrepo.go  + the SQL; scanCandidate factored out         PR 5
test/support/memrepo/units.go   + the fake                                       PR 5
test/support/repocontract/unitrepo.go  + RunLiveFocusCandidatesByType            PR 5
internal/core/prospection/digest.go    EnergyReading gains Source string         PR 5
internal/brain/
├── today.go                    TodayService, Today, Focus, FocusMember,
│                               PendingDigest, DigestLine, VaultStatus, todayRunner PR 6
└── digest.go                   digestItems → package function                   PR 6
cmd/nooma/
├── serve.go                    ui.New(ui.Deps{}), Deps.UI wired unconditionally PR 2
│                              --no-ui, Server.UI consumed, Deps.UI's nil path   PR 3
│                              ui.New gains Today, Serving.CookieAuth            PR 7
└── wiring.go                   wireToday                                        PR 6
go.mod / go.sum                 require templ; tool templ                        PR 1
Makefile, .github/workflows/ci.yml, .gitattributes, .golangci.yml                PR 1
docs/01-architecture.md         Layer 2 row for /ui: SYSTEM's six lines; --no-ui PR 6, 3
docs/02-cognitive-core.md       §7: energy reading carries its source (PR 5);
                                 §3: Priority-only until m4c; §7: viewing is not delivering PR 5, 6
docs/06-harness.md              §4: I27 row; §6: the templ gate is now true      PR 6, 1
docs/adr/0028-…, README.md      §3.10                                            PR 4a
```

**Edges, all one-way.** `cmd/nooma → httpapi → ui → brain → ports → core`. `httpapi` already
imports `brain` and `config`; it gains `ui`. `ui` imports `brain` (the `Today` type), `core/focus`
and `core/unit` (types on that struct), `ports` (`RelationQuestion`, `EnergyReading` via
`prospection`), `templ`, `embed`, `net/http`. `brain` imports nothing new. `ports` imports
`core/unit` and `core/focus` already (`unitrepo.go:8-11`). **`internal/core` is not untouched by
every PR — corrected.** An earlier draft of this design claimed it was and that `docs-sync` fires
on none of them. PR 5 widens `prospection.EnergyReading` with `Source string` (§3.6), a change
under `internal/core/prospection/digest.go`; read (`scripts/docs-sync.sh:45`), `docs-sync` fires
on the path prefix `internal/core/` alone — it does not distinguish a struct-widening change from
a behavioral one, and does not need to: the script passes as soon as `docs/02-cognitive-core.md`
changes in the same diff. `docs-sync` therefore fires on PR 5, and PR 5 carries one doc 02
sentence in the same commit — in §7's low-energy-gate paragraph, the paragraph whose semantics
the widened field leaves unchanged, stating that the energy reading now carries its source
(`user` or `consolidation`) for display, and that the low-energy gate (`LowEnergy`) is itself
unchanged — with a cross-reference to §10's `current_state` column list, where `source` is
already named — rather than claiming the `no-spec-change` label PR 5 does not need. PR 6's own
two doc 02
sentences (§3 Priority-only, §7 viewing-is-not-delivering) touch no `internal/core` file, so
`docs-sync` does not fire on PR 6; those two are still carried by this design and checked by
`sdd-verify`, not by the gate, exactly as before.

---

## 5. Data flow

```
 ══ a browser, token configured, first visit ═════════════════════════════════════
  GET /ui ─► securityHeaders ─► crossOrigin(GET: pass) ─► requireCookie: no cookie
                                                              │
                                              303 Location: /ui/login  (no body, no data)
  GET /ui/login ─► … ─► uiMux: loginPage ─► ui.RenderLogin(Rejected:false)   200
  POST /ui/login (Sec-Fetch-Site: same-origin, token=…)
        ─► crossOrigin: pass ─► loginSubmit: MaxBytesReader, ParseForm,
           ConstantTimeCompare ── ok ──► Set-Cookie nooma_token=b64(token);
           │                              Path=/ui; HttpOnly; SameSite=Strict[; Secure]
           │                              303 Location: /ui
           └── not ok ──► ui.RenderLogin(Rejected:true)                        401
  GET /ui (cookie) ─► requireCookie: decode, compare ── ok ──► d.UI
                                                                 │
  POST /ui (no route declares it) ─► uiMux: 405 Method Not Allowed,
        Allow: GET, HEAD — before requireCookie ever runs; no Set-Cookie, no body
 ══ the mirror ═══════════════════════════════════════════════════│═══════════════
  ui.Handler ─► TodayReader.Today(ctx)            clock.Now() ← the one clock read
                     │
        cfg.Load ──────────────────────────────► Status.LastConsolidationAt
        state.LatestEnergy ─► LowEnergy(now) ──► Status.Energy, low
        triggers.Undelivered ──────────────────► pending, Status.Undelivered
        log.Since(now-digestHistoryDays) ──────► history ─► heldCounts
        digestItems(units, pending, history) ─► items ─► Carry(items,{},low,now) ─► carry, held
        carry joined back to pending by ID ──────────────────────────────────► Items; Held=len(held)
        questions.Unasked (if !low) ───────────► Question
        questions.Open ────────────────────────► Status.OpenQuestions
        ∀k ∈ AllKinds:
          units.LiveFocusCandidatesByType(Types(k)) ─► Rank({},now) ─► [:DefaultSize]
                                                   ─► units.LiveByIDs ─► Focuses[k].Members
                     │
                     ▼   (no Surface, no MarkAsked, no Record — I27)
        ui.Today(model, Serving{Bind, CookieAuth}) ─► HTML, Cache-Control: no-store  200

 ══ a foreign page on the LAN ════════════════════════════════════════════════════
  POST http://nooma:7777/ui/login  Origin: http://evil  Sec-Fetch-Site: cross-site
        ─► crossOrigin: 403 before any handler, before any comparison

 ══ --no-ui ══════════════════════════════════════════════════════════════════════
  GET /ui ─► mux has no /ui ─► requireToken(guarded) ─► 401 (token) | 404 (none)
```

---

## 6. File changes

| File | Action | What |
|---|---|---|
| `go.mod`, `go.sum` | Modify | `require github.com/a-h/templ`; `tool github.com/a-h/templ/cmd/templ` |
| `Makefile` | Modify | `templ`, `templ-clean`; `check-all` gains `templ-clean` |
| `.github/workflows/ci.yml` | Modify | `build` job gains the clean-tree step; lines 125-127 deleted |
| `.gitattributes` | Modify | `linguist-generated`, `linguist-vendored` |
| `.golangci.yml` | Modify | `ui-boundary` (§3.1) |
| `internal/ui/layout.templ`, `layout_templ.go` | Create | `Page` shell, `<meta htmx-config>`, stylesheet link, script tag |
| `internal/ui/ui.go`, `assets.go`, `static/*` | Create | §3.1 |
| `internal/ui/login.templ`, `login_templ.go`, `login.go` | Create | §3.3 |
| `internal/ui/today.templ`, `today_templ.go` | Create | §3.6's three sections |
| `internal/httpapi/server.go` | Modify | `Deps.UI`; UI subtree; `uiPlaceholder` deleted (PR 2, 3); the two guarded leaves wrapped in `requireCookie` (PR 4a) |
| `internal/httpapi/headers.go`, `cookie.go` | Create | §3.5, §3.3 |
| `internal/httpapi/server_test.go` | Modify | `TestHandlerServesBothSurfaces` renamed to `TestHandlerServesAPIRootAndUIShell` and given a `Deps.UI` (PR 2); `TestHandlerServesDistinctSurfaces` and `TestOpenRoutesStayOpenRegardlessOfToken`'s `Deps` fixtures also gain `UI` in PR 2 so `/ui` stays reachable once the mux's `d.UI != nil` gate lands (§7.2); `TestOpenRoutesStayOpenRegardlessOfToken` is later inverted on its `/ui` leg and renamed to `TestOpenRoutesAndUIRoutesUnderAToken` (PR 4a) |
| `internal/ports/unitrepo.go` | Modify | §3.7 |
| `internal/store/sqlite/unitrepo.go` | Modify | §3.7; `scanCandidate` |
| `testdata/schema/store_api.golden` | Modify | Regenerated, one line |
| `test/support/memrepo/units.go`, `test/support/repocontract/unitrepo.go` | Modify | The fake and the contract |
| `internal/core/prospection/digest.go` | Modify | `EnergyReading` gains `Source string` (§3.6) |
| `internal/store/sqlite/staterepo.go` | Modify | `LatestEnergy`'s `SELECT`/`Scan` gain `source` (§3.6) |
| `test/support/repocontract/staterepo.go` | Modify | `RunLatestEnergy` (§3.6, §7 PR 5) |
| `internal/brain/today.go` | Create | §3.6 |
| `internal/brain/digest.go` | Modify | `digestItems` becomes a function |
| `cmd/nooma/serve.go`, `wiring.go` | Modify | §3.9 — `ui.New(ui.Deps{})` wired unconditionally into `Deps.UI` (PR 2); `--no-ui`/`Server.UI`/`Deps.UI`'s nil path wrapping that call (PR 3); `wireToday` (PR 6); `ui.New` gains `Today`/`Serving.CookieAuth` (PR 7) |
| `test/conformance/i27_viewing_is_not_delivering_test.go` | Create | §8 |
| `test/e2e/serve_test.go` | Modify | `--no-ui` legs (`TestServeNoUI`, PR 3); the handshake walk (`TestServeHandshake`, PR 4b/7). `TestServeAnswersBothSurfaces`'s `/ui` leg is **not** modified: it configures no token, so `requireCookie` is a no-op at every tip (§3.2's "When `Token == \"\""` case) and the leg passes once PR 2's production wiring alone lands (§7.2) |
| `docs/01-architecture.md` | Modify | `/ui` row names SYSTEM's six lines; `--no-ui` sentence gains "or `server.ui: false`" |
| `docs/02-cognitive-core.md` | Modify | §7: one sentence (PR 5) — the energy reading carries its source, `user` or `consolidation`, cross-referencing §10's `current_state` columns; §3: one sentence (PR 6) — Priority-only, no incumbent, until m4c; §7: one sentence (PR 6) — viewing is not delivering |
| `docs/06-harness.md` | Modify | §4: I27; §6: no change to the row, which becomes true |
| `docs/adr/0028-ui-cookie-handshake.md`, `docs/adr/README.md` | Create / Modify | §3.10 |

---

## 7. The PR chain

Chain `stacked-to-main`, delivery `auto-chain` (proposal §5). Every PR: **the test commit ahead
of the implementation commit** (proposal §6; `sdd-verify` reads `git log`). No tip is red.
Budgets are implementation + docs; tests, generated `_templ.go`, `go.sum` and `htmx.min.js` are
reported beside them, not inside (proposal §4.1).

| # | Branch | RED commit | GREEN commit | Impl+docs |
|---|---|---|---|---|
| 1 | `feat/ui-toolchain-gates` | `ui-boundary` rule + scratch `probe.go` importing `store` (lint red); `layout.templ` + `Makefile`/CI gate + a hand-edited `layout_templ.go` (gate red, recorded) | probe deleted; `_templ.go` regenerated; `.gitattributes`; `go.mod` `tool`; `ci.yml:125-127` gone; doc 06 §6 unchanged and now true | ~190 (+ `go.sum`, `layout_templ.go` reported). **Overflow rule**: if above 400, `layout.templ`'s nav markup — nothing yet exists for it to link to — trims to one `<main>` slot; the gate only needs one committed template to prove `templ-clean`, not a finished shell. PR 7 adds the nav with the first navigable view (deferred from PR 2 at apply time) |
| 2 | `feat/ui-base-layout` | `TestUISubtreeSetsSecurityHeaders`, `TestUICrossOriginPostIsRefused`, `TestUIStaticServesStylesheetAndHtmx`, `TestHandlerServesBothSurfaces` → `TestHandlerServesAPIRootAndUIShell` with `Deps.UI`, `TestUIRootIsNeverRedirectedByTheMux` (new — §3.2, §8) | `ui.New`, `Assets`, `app.css` (six layers), htmx + licence + audit note, `headers.go`, the cross-origin wrap, `uiMux`'s five leaf patterns (`/ui`, `/ui/{$}`, `/ui/static/app.css`, `/ui/static/htmx.min.js`, `/ui/static/htmx.LICENSE` — none subtree-shaped; the two `/ui/login` leaves land in PR 4b with their handlers, §3.2), `uiPlaceholder` deleted; `cmd/nooma/serve.go` wires `ui.New(ui.Deps{})` into `Deps.UI` **unconditionally**, the shell state's production wiring (§3.9) — GREEN-side plumbing, not a new assertion; `TestHandlerServesDistinctSurfaces`'s and `TestOpenRoutesStayOpenRegardlessOfToken`'s existing `Deps` fixtures also gain `UI` here so `/ui` stays reachable once `d.UI != nil` gates the mount (§7.2) — also GREEN-side plumbing; neither test's assertions change | ~350 (+ `htmx.min.js` reported; +10 for the three explicit static leaves that replace one `{file...}` wildcard, §3.2; −10 for the two `/ui/login` leaf registrations moved to PR 4b, where `loginPage`/`loginSubmit` exist to back them; +5 for the shell paragraph `ServeHTTP` renders before `TodayReader` exists, §3.1; +5 for `cmd/nooma/serve.go`'s unconditional `ui.New(ui.Deps{})` wiring landing here instead of PR 3 (§3.9, §7.2) — all four counted inside the ~350, net +10 against this design's earlier count of ~340 for these items; `TestHandlerServesAPIRootAndUIShell` and the two fixture-only test changes are reported beside it, not inside, per this table's own rule). **Overflow rule**: if above 400, `app.css` beyond the token layer moves to PR 7 with the view that first uses it |
| 3 | `feat/serve-no-ui` | `TestNoUIUnmountsTheSubtree` (L1: nil `Deps.UI` → 404/401 like an unknown path); L4 `TestServeNoUI` with the flag and with `ui: false` | `--no-ui`, `Server.UI` consumed, `Deps.UI`'s nil path — wraps PR 2's already-wired `ui.New(ui.Deps{})` call in the `uiEnabled` conditional rather than introducing that call (§3.9), doc 01 sentence | ~110 (unchanged: PR 3 wraps an existing call instead of adding one) |
| 4a | `feat/httpapi-ui-cookie-middleware` | **`TestOpenRoutesStayOpenRegardlessOfToken` inverted** on `/ui` (303, no data, no cookie) and renamed to `TestOpenRoutesAndUIRoutesUnderAToken` (two legs: `/` at 200, `/ui` at 303); `TestUIViewsRequireCookie` (missing ≡ wrong; GET→303; POST→405 with `Allow: GET, HEAD`, asserted against `Handler(d)`, no `Set-Cookie`, no body); `TestRequireCookieNoOpOnlyOnLoopback` over `bindTokenTruthTable` | `requireCookie`, `uiCookieName`, the wrap; **ADR-0028** + README row | ~220. **In-between state on `main`**: a token-configured server's UI is unreachable from a browser until 4b — one PR, stated. **Overflow rule**: if above 400, ADR-0028's Alternatives section shortens to a one-line pointer per alternative (proposal §8 already argues each in full) instead of restating the reasoning; `requireCookie`, `uiCookieName` and the wrap do not move — 4b's handlers depend on all three landing whole |
| 4b | `feat/httpapi-ui-login-screen` | `TestOpenRoutesAndUIRoutesUnderAToken` gains its third leg — `GET /ui/login` is 200 with no `Set-Cookie` — in this PR's RED commit, ahead of `loginPage`'s GREEN (§3.2: PR 4a's tip stays green, the leg needing `loginPage` cannot be asserted there); `TestLoginIssuesTheCookieOnlyOnTheRightToken` (attributes asserted one by one; `Secure` absent under `httptest.NewServer`, present under `NewTLSServer`); `TestLoginRejectionIsByteIdentical`; `TestLoginRoutesAbsentWithoutAToken`; L4 handshake walk (`docs/06-harness.md:181-183`) | `loginPage`, `loginSubmit`, `setUICookie`, `login.templ`, `ui.RenderLogin`, the two `GET`/`POST /ui/login` leaf registrations on `uiMux` (§3.2) | ~210 (+10 against this design's earlier count for the two `/ui/login` leaf registrations moved here from PR 2, since their handlers do not exist before this PR, §7). **Overflow rule**: if above 400, `login.templ`'s markup trims to the minimal form (token input, submit button, the rejected-state paragraph); layout polish is a follow-up commit inside this same PR, not a second one — the handshake screen is one deliverable |
| 5 | `feat/ports-store-live-focus-by-type` | `RunLiveFocusCandidatesByType` in `repocontract` (I02 positive filter; type bound; id order; empty in → empty out) run against `memrepo` (L2, red: fake lacks the method — compile-red, then a stub returning nothing); `RunLatestEnergy` (new, `repocontract`, alongside `RunOpenHypothesis`/`RunLastHypothesisAt`) pinning `Source` beside `Level`/`RecordedAt` for a `user`- and a `consolidation`-sourced row (L2, red: `prospection.EnergyReading` has no `Source` field to assert on) | the port, the SQL, `scanCandidate`, the fake, `store_api.golden`; `prospection.EnergyReading` gains `Source string`; `staterepo.go`'s `LatestEnergy` SELECT and `Scan` gain `source`; **the doc 02 §7 energy-reading sentence** (§4) in the same commit — this PR touches `internal/core/prospection/digest.go`, so `docs-sync` fires on it, and it carries no `no-spec-change` label | ~170 (+15 against this design's earlier count of ~155, for the doc 02 sentence §4 now requires). Lower than proposal §5.1's ~250 for this slice because that estimate predates `scanCandidate`'s factoring, which reuses `LiveFocusCandidates`' own scanning code instead of writing a second SQL-to-struct mapping. **Overflow rule**: if above 400, `LiveFocusCandidatesByType`'s doc comment (§3.7) trims to the two paragraphs a caller needs first — the positive filter, the id order — with the SQL-vs-`ORDER BY` rationale staying here as its only copy, not duplicated in source |
| 6 | `feat/brain-today` | **`i27_viewing_is_not_delivering_test.go`** (L2, `writeGuard`); `TestToday_PriorityOnlyTopNPerKind` (fixtures around `focus.DefaultSize`, never the literal 7); `TestToday_DigestMirrorsCarry`; the existing AST gate `test/conformance/brain_single_clock_read_test.go`'s `TestBrainReadsTheClockAtMostOnceAndNeverAgainstAnInstantItAlreadyHas`, now scanning a second file | `today.go`, `digestItems` refactor, `wireToday`, doc 02 §3 + §7, doc 06 §4 I27, doc 01 `/ui` row | ~260. Lower than proposal §5.1's ~350 for `feat/brain-today` because that estimate predates this design's split: `todayRunner.at` reuses seven reads and `digestItems`/`heldCounts` largely as written, rather than the second `Carry`-shaped pass the higher figure assumed — the new lines are `today.go`'s types, `todayRunner.at`'s orchestration, and the two doc amendments. **Overflow rule**: if above 400, `digestItems`' package-function refactor and its one call-site update in `brain/digest.go` split off as `feat/brain-digest-items-refactor` (a new PR 5b, landing after 5 and ahead of 6), leaving `today.go`, `wireToday` and the doc 01/02/06 amendments in 6 |
| 7 | `feat/ui-today-view` | `TestTodayView_RendersThreeSectionsFromTheModel` (L1, structure); `TestTodayView_NilTodayReaderAnswers503` — takes over `TestHandlerServesAPIRootAndUIShell`'s no-`TodayReader` fixture, now that `ServeHTTP` renders Today unconditionally and that fixture falls into the 503 arm instead of the retired shell (§7.2, §8); `TestTodayView_I18DatesLabelled`; L4 `GET /ui` after the handshake lists a real unit | `today.templ`, `ui.Deps.Today`, `Serving` wired from `serve.go`; rendering `FocusMember.Score` (two decimals, `NaN` as `NaN`, never coerced — Q7, ruled, §11) | ~195 (+5 for `Score`'s rendering; + `today_templ.go` reported). Lower than proposal §5.1's ~300 for `feat/ui-today-view` because that estimate predates §3.1's ruling that `ui` renders `brain.Today` directly — no second, hand-mapped view-model type between the service and the template, so `today.templ` reads the struct's own fields rather than a translation layer the higher figure assumed. **Overflow rule**: if above 400, `today.templ`'s two `AllKinds()` loops (FOCUS's task and load sections) factor into one shared partial before the tip, recovering their near-duplicate markup — there is no PR 8 to receive an overflow, so 7's cut comes from within |

Eight PRs, ~1,705 budgeted lines — the sum of the eight rows above (190 + 350 + 110 + 220 + 210 + 170 + 260 + 195); the delta narrative that follows is history, not the derivation (PR 2 +10 for the three explicit `/ui/static/*` leaves that
replace one `{file...}` wildcard (§3.2), −10 for the two `/ui/login` leaf registrations moved to
PR 4b (§3.2, §7 — offset by the matching +10 already counted in PR 4b's own row below, net zero
on the total), +5 for the shell paragraph `ServeHTTP` renders before `TodayReader` exists (§3.1),
and +5 for `cmd/nooma/serve.go`'s unconditional `ui.New(ui.Deps{})` wiring landing in PR 2
instead of PR 3 (§3.9, §7.2) — net +10 for PR 2; PR 5 +15 for `prospection.EnergyReading`'s
`Source` field and `LatestEnergy`'s widened SQL, and +15 more for the doc 02 sentence §4 now
requires in the same commit — net +30 for PR 5 — all against this design's earlier count of
~1,650). The two `/ui/login` leaf registrations move from PR 2's budget
to PR 4b's — net zero on the total: PR 2's earlier count wrongly included two patterns whose
handlers (`loginPage`, `loginSubmit`) do not exist until PR 4b, corrected in both rows above.
Order 1 → 2 → 3 → 4a → 4b → 5 → 6 → 7. **PR 4a must land
before 5, 6 and 7** — proposal R1: no vault data reaches `/ui` before the cookie check exists.
PR 3 could land anywhere after 2 and is placed early so `--no-ui` exists before the first data
route. Against this project's full measured range — 1.3×–2.2× six times, 4.3× once (proposal
§5.1) — 2 and 6 are the ones to watch most closely, though every PR above now carries a named
overflow cut, not only those two.

### 7.1 Landing order

One row per Go symbol, file, route pattern, test and doc amendment this design names, the PR
whose GREEN commit creates it, and the PR(s) that later change it — built by walking §3, §4, §6,
§7 and §8, then checking every PR's tip against it: nothing below is used by a tip earlier than
the PR that creates it.

| Artifact | Created (PR, GREEN) | Later modified |
|---|---|---|
| `go.mod`/`go.sum`: `tool`/`require` templ | 1 | — |
| `Makefile`: `templ`, `templ-clean` targets | 1 | — |
| `.github/workflows/ci.yml`: clean-tree step | 1 | — |
| `.gitattributes`: `linguist-generated`/`linguist-vendored` | 1 | — |
| `.golangci.yml`: `ui-boundary` depguard | 1 | — |
| `internal/ui/layout.templ`, `layout_templ.go`, `Page(title, body)` | 1 | — |
| `internal/ui/doc.go` charter sentence | 1 (inferred — §3.1's tree carries no explicit PR tag for this line) | — |
| `ui.Deps`, `ui.Serving`, `ui.New`, `(*Handler).ServeHTTP`, PR 2's shell state (§3.1) | 2 | `ServeHTTP` starts rendering Today at 7 |
| `ui.Assets()`, `internal/ui/assets.go`, `static/*` | 2 | — |
| `internal/httpapi/headers.go`: `securityHeaders` | 2 | — |
| the cross-origin wrap (`http.NewCrossOriginProtection`) | 2 | — |
| `uiMux`'s five leaf patterns (`/ui`, `/ui/{$}`, `/ui/static/*` ×3) | 2 | the two guarded leaves wrapped in `requireCookie` at 4a |
| `internal/httpapi/server.go`: `Deps.UI`, the UI subtree mount, `uiPlaceholder` deleted | 2 | `--no-ui`'s nil path at 3; guarded leaves wrapped at 4a |
| `cmd/nooma/serve.go`: `ui.New(ui.Deps{})` wired unconditionally into `Deps.UI` (§3.9) | 2 | wrapped in the `--no-ui` conditional at 3 (same call, not a new one); gains `Today`/`Serving.CookieAuth` at 7 |
| `TestHandlerServesAPIRootAndUIShell` (renamed from `TestHandlerServesBothSurfaces`) | 2 | scope narrows to PR 2–PR 6; superseded by `TestTodayView_NilTodayReaderAnswers503` for the same fixture from 7 (§7.2, §8) |
| `TestUISubtreeSetsSecurityHeaders`, `TestUICrossOriginPostIsRefused`, `TestUIStaticServesStylesheetAndHtmx`, `TestUIRootIsNeverRedirectedByTheMux` | 2 | — |
| `TestHandlerServesDistinctSurfaces` and `TestOpenRoutesStayOpenRegardlessOfToken`: existing `Deps` fixtures gain `UI` (fixture plumbing, no assertion change, §7.2) | 2 | `TestOpenRoutesStayOpenRegardlessOfToken` renamed to `TestOpenRoutesAndUIRoutesUnderAToken` and its `/ui` leg inverted at 4a |
| `--no-ui`, `Server.UI` consumed, `Deps.UI`'s nil path (wraps 2's already-wired call) | 3 | `ui.New(...)` gains `Today`/`Serving.CookieAuth` at 7 (§3.9) |
| `TestNoUIUnmountsTheSubtree`, `TestServeNoUI` | 3 | — |
| `docs/01-architecture.md`: `--no-ui` sentence | 3 | `/ui` row's SYSTEM six lines at 6 |
| `uiCookieName`, `requireCookie` | 4a | — |
| ADR-0028, `docs/adr/README.md` row | 4a | — |
| `TestOpenRoutesAndUIRoutesUnderAToken` (renamed, inverted `/ui` leg), `TestUIViewsRequireCookie`, `TestRequireCookieNoOpOnlyOnLoopback` | 4a | `TestOpenRoutesAndUIRoutesUnderAToken` gains its third leg (`GET /ui/login`) at 4b |
| `loginPage`, `loginSubmit`, `setUICookie`, `login.templ`, `login_templ.go`, `ui.RenderLogin`, `internal/ui/login.go` (`LoginView`) | 4b | — |
| `GET`/`POST /ui/login` leaf registrations | 4b | — |
| `TestLoginIssuesTheCookieOnlyOnTheRightToken`, `TestLoginRejectionIsByteIdentical`, `TestLoginRoutesAbsentWithoutAToken`, the L4 handshake walk | 4b | — |
| `ports.UnitRepo.LiveFocusCandidatesByType` | 5 | — |
| `internal/store/sqlite/unitrepo.go` SQL, `scanCandidate` | 5 | — |
| `test/support/memrepo/units.go`, `test/support/repocontract/unitrepo.go` (`RunLiveFocusCandidatesByType`) | 5 | — |
| `prospection.EnergyReading.Source` | 5 | — |
| `staterepo.go`'s `LatestEnergy` SELECT/Scan (`source`), `test/support/repocontract/staterepo.go` (`RunLatestEnergy`) | 5 | — |
| `docs/02-cognitive-core.md` §7 energy-source sentence | 5 | — |
| `testdata/schema/store_api.golden` | 5 | — |
| `brain.TodayService`, `NewTodayService`, `brain.Today`, `Focus`, `FocusMember`, `PendingDigest`, `DigestLine`, `VaultStatus`, `todayRunner` | 6 | — |
| `digestItems` (package function) | 6 | — |
| `cmd/nooma/wiring.go`: `wireToday` | 6 | — |
| `i27_viewing_is_not_delivering_test.go` (I27), `TestToday_PriorityOnlyTopNPerKind`, `TestToday_DigestMirrorsCarry` | 6 | — |
| `test/conformance/brain_single_clock_read_test.go` scanning `today.go` | 6 (extends the existing test) | — |
| `docs/02-cognitive-core.md` §3 Priority-only sentence, §7 viewing-is-not-delivering sentence; `docs/06-harness.md` §4 I27 row; `docs/01-architecture.md` `/ui` row (SYSTEM's six lines) | 6 | — |
| `ui.TodayReader`, `ui.Deps.Today` | 7 | — |
| `internal/ui/today.templ`, `today_templ.go`, `Today(brain.Today, Serving)` | 7 | — |
| `TestTodayView_RendersThreeSectionsFromTheModel`, `TestTodayView_NilTodayReaderAnswers503`, `TestTodayView_I18DatesLabelled` | 7 | — |
| `cmd/nooma/serve.go`: `ui.New(...)` gains `Today: today` and `Serving{Bind, CookieAuth}` (widening the call PR 2 already wires, not creating it) | 7 | — |
| L4 `GET /ui` after the handshake lists a real unit | 7 | — |

**Two inconsistencies found and fixed, beyond the reported instance:**
1. §3.1's tree listed `TodayReader` at "PR 2, PR 7", though it is declared as
   `Today(ctx) (brain.Today, error)` and `brain.Today` is PR 6's (§4's tree, §7's PR 6 row) — the
   reported instance. Fixed by splitting the tree row (§3.1), defining PR 2's shell state (§3.1,
   §3.2), giving it its own test row (§8), and qualifying §3.2's "renders Today unconditionally"
   and §3.9's `ui.New(...)` snippet as the PR 7+ state.
2. §10's rollback paragraph attributed `setUICookie` to PR 4a ("call 4a's `setUICookie`/
   `requireCookie` directly"), while §7's PR 4a/4b rows and §4's tree both place `setUICookie` in
   PR 4b, beside `loginSubmit`. Fixed by correcting §10 to name what 4b actually depends on from
   4a (`uiCookieName`, `requireCookie`) and by splitting `cookie.go`'s tree row in §4 to attribute
   each symbol to its own PR.

No other instance found: every other symbol, route, test and doc amendment named in §3, §4, §6,
§7 and §8 already lands at or after the PR that creates what it depends on.

### 7.2 Tip behaviour

One row per PR tip, walking §3, §7 and §7.1 and the tests those sections and §8 name, so a reader
can check what `GET /ui` in the compiled binary answers **at that tip** without re-deriving it —
the CRITICAL finding that opened this section was exactly this state going unchecked between
PR 2 and PR 4a.

| PR tip | `GET /ui`, no token | `GET /ui`, token configured, no cookie | Tests asserting this state at this tip | Test modified in THIS PR for the tip to stay green |
|---|---|---|---|---|
| 1 | 200, `uiPlaceholder` — no `m4a` code has landed yet | 200, the same placeholder — no cookie concept exists | `TestHandlerServesBothSurfaces`, `TestOpenRoutesStayOpenRegardlessOfToken`, `TestServeAnswersBothSurfaces` (e2e) — all pre-existing | None — PR 1 touches the toolchain and gates only (§7's PR 1 row), never `internal/httpapi` |
| 2 | 200, PR 2's shell (§3.1: layout, empty `<main>`, "Today arrives in a later PR", five security headers, no SYSTEM section) | 200, the same shell — `requireCookie` does not exist yet, so a configured token changes nothing here | `TestHandlerServesAPIRootAndUIShell` (renamed), `TestUISubtreeSetsSecurityHeaders`, `TestUIRootIsNeverRedirectedByTheMux`, `TestHandlerServesDistinctSurfaces`, `TestOpenRoutesStayOpenRegardlessOfToken` | Yes — `TestHandlerServesBothSurfaces` renamed and rewritten; `TestHandlerServesDistinctSurfaces`'s and `TestOpenRoutesStayOpenRegardlessOfToken`'s `Deps` fixtures gain `UI` (fixture-only plumbing, §7's PR 2 row). `test/e2e/serve_test.go`'s `TestServeAnswersBothSurfaces` is **not** modified — `cmd/nooma/serve.go`'s unconditional `ui.New(ui.Deps{})` wiring (§3.9) is enough on its own |
| 3, default (`--no-ui` absent) | 200, shell — unchanged from PR 2 | 200, shell — unchanged from PR 2 | Same tests as PR 2's row | None |
| 3, `--no-ui` or `server.ui: false` | 404 — falls to `requireToken(guardedMux)` with no token (§3.9) | 401 — falls to `requireToken(guardedMux)` with a token (§3.9) | `TestNoUIUnmountsTheSubtree` (L1), `TestServeNoUI` (L4) | New tests only; the default-flag state above needs no change |
| 4a | 200, shell — `Token == ""` keeps `requireCookie` a no-op (§3.2) | **303** `Location: /ui/login` (inverted); `/ui/login` itself still 404s until 4b (N2) | `TestOpenRoutesAndUIRoutesUnderAToken` (renamed, inverted `/ui` leg), `TestUIViewsRequireCookie`, `TestRequireCookieNoOpOnlyOnLoopback` | Yes — `TestOpenRoutesStayOpenRegardlessOfToken` renamed and its `/ui` leg inverted (§3.2). `TestHandlerServesAPIRootAndUIShell`/`TestHandlerServesDistinctSurfaces` (both token-less) stay green unmodified |
| 4b | 200, shell — unchanged | 303 unchanged with no cookie; `GET /ui/login` now 200; `POST /ui/login` with the right token sets the cookie, after which `GET /ui` (cookie) is 200 — still shell, `TodayReader` is PR 7's | `TestOpenRoutesAndUIRoutesUnderAToken` (third leg), `TestLoginIssuesTheCookieOnlyOnTheRightToken`, `TestLoginRejectionIsByteIdentical`, `TestLoginRoutesAbsentWithoutAToken`, the L4 handshake walk | Yes — `TestOpenRoutesAndUIRoutesUnderAToken` gains its third leg in this PR's RED commit (§3.2) |
| 5 | 200, shell — unchanged (PR 5 touches only `internal/ports`/`internal/store`/`internal/core/prospection`, never `internal/httpapi`/`internal/ui`, §4, §6) | 303 unchanged | None new for `/ui`'s own HTTP state; `RunLiveFocusCandidatesByType` and `RunLatestEnergy` cover the new read, not the route | None |
| 6 | 200, shell — unchanged (`ui.TodayReader`/`Deps.Today` are PR 7's, §3.1, §3.2) | 303 unchanged | None new for `/ui`'s own HTTP state; `i27_viewing_is_not_delivering_test.go`, `TestToday_PriorityOnlyTopNPerKind`, `TestToday_DigestMirrorsCarry` exercise `brain.TodayService` directly, not the route | None |
| 7 | 200, the **real** Today page (FOCUS/PENDING DIGEST/SYSTEM) — `ServeHTTP` renders Today unconditionally once `TodayReader` is wired (§3.2); a `Deps.UI` built with no `TodayReader` — PR 2's own shell fixture, `ui.New(ui.Deps{})` — now answers **503**, `captureHandler`'s nil posture | 303 with no cookie; 200 real Today with the right cookie | `TestTodayView_RendersThreeSectionsFromTheModel`, `TestTodayView_NilTodayReaderAnswers503`, `TestTodayView_I18DatesLabelled`, L4 `GET /ui` after the handshake lists a real unit | Yes — `TestHandlerServesAPIRootAndUIShell`'s scope narrows to PR 2 through PR 6 (§8): its own fixture (`ui.New(ui.Deps{})`, no `TodayReader`) now falls into the 503 arm instead of the shell path it was written to test, and `TestTodayView_NilTodayReaderAnswers503` takes over asserting that fixture's PR 7+ behavior |

**Two inconsistencies found and fixed, beyond the reported instance this section exists to close:**
1. §6's file-change row for `test/e2e/serve_test.go` claimed `TestServeAnswersBothSurfaces`'s
   `/ui` leg needed modification. It does not: that test configures no token, so `requireCookie`
   is a no-op at every tip (§3.2's `Token == ""` case), and the leg passes as soon as PR 2's
   production wiring lands, with no test change at all. Fixed by rewording that row to name the
   tests actually added (`TestServeNoUI`, the L4 handshake walk) and stating why
   `TestServeAnswersBothSurfaces` itself needs no change.
2. §8's `TestHandlerServesAPIRootAndUIShell` row and §7.1's landing-order table both implied no
   later PR touches it, but its own fixture (`Deps.UI` built via `ui.New(ui.Deps{})`, no
   `TodayReader`) falls into PR 7's new nil-`TodayReader` arm once `ServeHTTP` starts rendering
   Today unconditionally (§3.2): the same construction PR 2 wrote to prove the shell now answers
   503, not 200-with-shell. Fixed by scoping the test's assertion to PR 2 through PR 6 in §8 and
   recording in §7.1 that `TestTodayView_NilTodayReaderAnswers503` (PR 7) takes over that
   fixture's PR 7+ behavior.

No other instance found: every other test named in §7's PR rows and §8 either names no PR-scoped
`/ui` assertion, or is already the test that a later PR is recorded as changing.

---

## 8. Testing strategy

Levels per `docs/06-harness.md` §3: `internal/httpapi` and `internal/ui` tests are **L1**
(adapters against `httptest` and fakes; the UI is L1 by that document's own section);
`repocontract` is L2 over `memrepo` and L3 over SQLite; the invariant is L2; the binary is L4.

| Layer | Test | The mutation it catches |
|---|---|---|
| L1 `httpapi` | `TestHandlerServesAPIRootAndUIShell` (renamed from `TestHandlerServesBothSurfaces`) — **PR 2 through PR 6 only (§7.2)**: `GET /` and `GET /ui` both 200 with `Deps.UI` set and no `TodayReader` wired; the `/ui` body carries the layout (headers, no nav until PR 7) and PR 2's shell paragraph, never a FOCUS/PENDING DIGEST/SYSTEM section or any other vault-shaped content (§3.1). From PR 7, the same `ui.New(ui.Deps{})` fixture falls into the nil-`TodayReader` arm instead and is covered by `TestTodayView_NilTodayReaderAnswers503` below, not by this test | A shell that leaks Today's own markup before `TodayReader` exists; a shell response missing the layout or the security headers |
| L1 `httpapi` | `TestUIRootIsNeverRedirectedByTheMux` — inspects the pattern `uiMux.Handler(r)` resolves for `GET /ui`: it must be the exact leaf pattern itself, never `ServeMux`'s own internal redirect handler, checked with a token configured and on loopback with none alike — a check on what the mux resolves the request to, not on the response `requireCookie` produces, since the legitimate 303 `requireCookie` answers under a token is not the mux's own redirect (§3.2) | Registering `/ui/` as a subtree instead of a leaf pattern — the class that let `GET /ui` 307 to `/ui/` from the mux itself, before `requireCookie` or any handler ran |
| L1 `httpapi` | `TestUISubtreeSetsSecurityHeaders` — every response under `/ui` (200, 303, 401, 403, 404, static) carries the five headers with the exact CSP string | A header dropped from one arm; a handler that writes its own `Cache-Control`; the chain wrapped in the wrong order so a 403 escapes without headers |
| L1 `httpapi` | `TestUICrossOriginPostIsRefused` — `POST /ui/login` with `Sec-Fetch-Site: cross-site` → 403; with `same-origin` → not 403; with no `Sec-Fetch-Site` and `Origin: http://evil` → 403; with neither header → not 403; `GET` with `cross-site` → not 403 | The wrap removed; the wrap placed inside `requireCookie` (a cross-site POST would 401 instead of 403); a bypass pattern added |
| L1 `httpapi` | `TestUIStaticServesStylesheetAndHtmx` — `GET /ui/static/app.css`, `/ui/static/htmx.min.js` and `/ui/static/htmx.LICENSE` each answer 200 with the right `Content-Type` and the embedded bytes; `GET /ui/static` and `GET /ui/static/` both answer 404, asserted against `Handler(d)`, not a 307 or a listing | Wrong content-type; truncated or swapped bytes; a directory listing served (the `{file...}`/`http.FileServerFS` defect §3.2 corrected); the licence file missing from the embed |
| L1 `httpapi` | `TestUIViewsRequireCookie` — token configured: no cookie, a wrong cookie, a wrong cookie the same decoded length as the real token (exercising the full-length arm of the comparison rather than a length-mismatch short-circuit), and a cookie that fails `base64.RawURLEncoding` decoding all give byte-identical `GET` responses (303 `/ui/login`); the right cookie reaches the view; `POST /ui` against `Handler(d)` answers `405 Method Not Allowed` with `Allow: GET, HEAD`, no `Set-Cookie`, no body — the mux's own answer, asserted rather than assumed | A branch that distinguishes missing from wrong; a view reachable by another path; registering a method-agnostic pattern (e.g. a bare `/ui` subtree) that would route `POST` into the view instead of a 405. **Not** an early return on decode error nor `==` instead of `ConstantTimeCompare` — both produce this same byte-identical response over HTTP; see the conformance row below |
| L2 `test/conformance` | `TestHTTPAPISecretCompareStructure` (`httpapi_secret_compare_test.go`) — an AST-level gate, not a response-level one, proving exactly two things, both robust to in-package helper extraction in either direction: (1) `cookie.go`'s `presentedSecret` — the decode step — returns a single `[]byte`, no `error`, no `http.ResponseWriter`, so an early return on a decode error has no signature left to be written in; (2) `requireCookie` and `requireToken` each transitively reach `subtle.ConstantTimeCompare` through same-package, unqualified function calls, not necessarily in their own function body | `presentedSecret` grows an `error` result or an `http.ResponseWriter` parameter; `subtle.ConstantTimeCompare` replaced by a plain byte-equality helper anywhere in either function's same-package call closure; `requireCookie` or `requireToken` renamed or removed (the gate is vacuity-guarded — it fails loudly rather than finding nothing to check). **Does not** flag a helper extraction that still calls `subtle.ConstantTimeCompare`, nor a decode step moved into a further in-package helper with `presentedSecret`'s own signature unchanged — both are correct refactors, proven by mutation (`sdd/m4a-ui-foundation/apply-progress`) |
| L1 `httpapi` | `TestRequireCookieNoOpOnlyOnLoopback` — `bindTokenTruthTable`'s rows, `TestRequireTokenNoOpOnlyOnLoopback`'s exact shape | A cookie check that fires with no token, or does not fire with one |
| L1 `httpapi` | `TestLoginIssuesTheCookieOnlyOnTheRightToken` — one `Set-Cookie`; name, decoded value, `Path=/ui`, `HttpOnly`, `SameSite=Strict`; `Secure` present only under `httptest.NewTLSServer`; 303 to `/ui` | A flag dropped; `Secure` hard-coded either way; `Path=/`; a `Max-Age` added; the raw token as the value (a token with a `;` breaks the round trip) |
| L1 `httpapi` | `TestLoginRejectionIsByteIdentical` — empty field and wrong token → same 401 body and headers; `TestLoginRoutesAbsentWithoutAToken` — `Token == ""` → `GET /ui/login` is 404 | An oracle; a screen shown on loopback |
| L1 `httpapi` | `TestNoUIUnmountsTheSubtree` — `Deps.UI == nil`: `/ui` and `/ui/login` answer exactly as `/does-not-exist` does under the same token state | A stub page mounted when off; a 404 when the API would say 401 |
| L1 `ui` | `TestTodayView_RendersThreeSectionsFromTheModel` — a fixed `brain.Today` renders each focus member's id, content and `Score` (two decimals) in rank order, each digest line's text, `Held` as a count and not a list, the question's two endpoints, and the six status lines with `never`/`no reading` for nils; a member with a `NaN` `Score` renders the literal `NaN`. Asserted on structure (`strings.Contains`, order of indexes), never on the whole document | A template that re-sorts, filters or drops a member; a nil dereference on an empty focus; a held list rendered; `Score` dropped from the markup; `NaN` formatted as `0.00` instead of rendered as `NaN` |
| L1 `ui` | `TestTodayView_I18DatesLabelled` — a member with `DueAt` and one with `EventAt` render under different labels, never swapped | I18's UI failure mode |
| L1 `ui` | `TestTodayView_NilTodayReaderAnswers503` — from PR 7, `ui.New(ui.Deps{})`'s no-`TodayReader` fixture (§3.1's own shell-era construction) answers 503, not the retired PR 2–6 shell (§7.2, §8's `TestHandlerServesAPIRootAndUIShell` row) | `captureHandler`'s nil posture, kept |
| L2 `repocontract` | `RunLiveFocusCandidatesByType` — wanted types in `pool` return; unwanted type in `pool` does not; `archived`/`superseded`/`incomplete` of a wanted type do not; id order; empty types → empty | A negative status filter (fails when a fifth status arrives — the same caveat `RunLiveFocusCandidates` records); a `LIMIT`; a type filter in the wrong column |
| L2 conformance | **I27** `i27_viewing_is_not_delivering_test.go` — `TodayService` over `memrepo` fakes wrapped in a `writeGuard` that fails on `Surface`, `Fire`, `Expire`, `Resolve`, `MarkAsked`, `Confirm`, `Reject`, `Create`, `Record`, `RecordConsolidationRun`, `OpenHypothesis`, `SetStatus`, `ApplyBoosts`, `Update*`; then `Undelivered()` and `Unasked()` compared before and after | The one mutation the milestone is named for: a view that delivers. Written in PR 6's RED commit against a `TodayService` that does not compile yet |
| L2 `brain` | `TestToday_PriorityOnlyTopNPerKind` — `DefaultSize + 2` task units and `DefaultSize + 1` load units with distinct weights; each focus holds exactly `DefaultSize`, in `focus.Rank`'s order, task focus contains no `mental_load`, load focus contains no `task`; adjacency is never non-zero (a fake `Rank` is not needed: compare against `focus.Rank` called directly) | A `Select` call with a margin; a type leak between focuses; a truncation at the wrong N |
| L2 `brain` | `TestToday_DigestMirrorsCarry` — with a low-energy reading, `Items` equals `Carry`'s carry slice joined to `pending` by ID for the same inputs, `Held` equals `len(held)`, `Question` is nil; without one, `Items` is every undelivered trigger in `(fired_at, id)` order and `Question` is `Unasked()[0]` | A Today that lists held items; a question shown on a low-energy day; a second sort; a join that drops a trigger `Carry` still names |
| L2 conformance (existing) | `brain_single_clock_read_test` now scans `today.go` | A second `Now()`; a `Now()` inside a `now time.Time` function |
| L3 | `RunLiveFocusCandidatesByType` against SQLite; `EXPLAIN QUERY PLAN` names the status index if one exists | The SQL, on the real engine |
| L4 | `TestServeNoUI` (flag; `ui: false`); `TestServeHandshake` — token configured on loopback: `GET /ui` → 303, `POST /ui/login` → cookie, `GET /ui` with the cookie → 200 and the body names a unit captured through the API | The binary's own wiring: `Deps.UI`, `Serving`, `wireToday` |

**Two things no test is allowed to assume.** That the cookie's `Secure` flag follows TLS — it
is asserted under both `httptest.NewServer` and `NewTLSServer`, not read from the code. And that
the view delivers nothing — `writeGuard` fails on the call, and the state comparison fails on
the effect, so a write that bypasses the guard (a new port method added later) still shows up
as a changed `Undelivered()`.

**Core coverage.** `internal/core` is touched only by PR 5's `EnergyReading` struct widening
(§4) — a new field, no new branch — so the ≥ 90 % floor is not meaningfully stressed; `docs-sync`
fires on PR 5 as §4 states, and PR 5 carries its doc 02 §7 sentence in the same commit.
`make check-all` before every PR is still the rule — `templ-clean`, L3, the matrix and L4 run
only there.

---

## 9. Threat matrix

| Boundary | Status |
|---|---|
| **Routing** | **Applicable.** Five new open routes (`/ui/login` ×2, `/ui/static/*` ×3) and one guarded subtree. The open ones serve embedded assets and a form; the guarded one is reachable only through `requireCookie` (§3.2), asserted by `TestUIViewsRequireCookie`. The three `/ui/static/*` leaves and the two guarded `/ui`/`/ui/{$}` leaves land in PR 2; the two `/ui/login` leaves land in PR 4b, with `loginPage`/`loginSubmit` (§7). `/ui/static`'s three routes are exact leaf patterns over `http.ServeFileFS` against an embedded `fs.FS`, each naming one known file — no wildcard, so no path can escape the embedded tree by construction, and no directory listing is reachable either (§3.2) |
| **Secret on the wire** | **Applicable, ruled.** The token travels in a `POST` body once and in a cookie thereafter; `HttpOnly` keeps it from script, `SameSite=Strict` from cross-site navigations, `Path=/ui` from the API, `Referrer-Policy` from off-host referrers; without TLS it is the exposure ADR-0007 documents and accepts |
| **Cross-site request forgery** | **Applicable, closed structurally** (§3.4); residual: a client sending neither `Sec-Fetch-Site` nor `Origin`, which is not a browser |
| **Brute force on `/ui/login`** | **Applicable, accepted.** Constant-time compare, no rate limit — `requireToken`'s own posture on the API; the same LAN, the same secret. Named, not mitigated |
| **Content injection into HTML** | **Applicable, closed by the stack.** templ escapes every `{ expr }` by default; vault content reaches a template only through typed fields; the CSP refuses inline script and style even if a template used `templ.Raw`, which none does — `TestTodayView_*` renders a unit whose content is `<script>alert(1)</script>` and asserts it is escaped |
| **Denial of service** | The login body is bounded (§3.3); the two unbounded reads are the vault's own size (LiveDecayStates' posture) |
| Shell / subprocess / VCS automation / executable classification | N/A |

---

## 10. Migration / rollout

**No migration** (proposal §1). No feature flag beyond the one the product already documents
(`--no-ui`, `server.ui`). **Rollback per PR** is a revert: 7 and 6 remove the view and the
service; 5 removes a method nothing then calls; 4b removes the screen (a token-configured UI
becomes unreachable, PR 4a's stated state); 4a removes the cookie check — **and reopens R1**, so
4a is reverted only together with 4b and 5–7, not 5–7 alone: 4b's `loginPage`/`loginSubmit`/
`setUICookie` live in the same `cookie.go` and reference 4a's `uiCookieName` constant, and
`server.go`'s wiring depends on 4a's `requireCookie` directly, so reverting 4a by itself breaks
the build, not only the guarantee; 3 removes a flag; 2 restores `uiPlaceholder`; 1 removes
gates that then protect nothing. One asymmetry: reverting 1 while 2 stands breaks the build —
PR 2's `ui.go` (`ServeHTTP`) calls `layout.Page`, which PR 1 alone defines, so removing PR 1
without also removing PR 2 leaves that call with nothing to resolve — revert 2 before 1.

---

## 11. Owner-review items and open questions

None blocks `sdd-tasks`; each has a decided default that ships if the owner is silent.

| # | Item | Decided default | What a different answer costs |
|---|---|---|---|
| **OR1** | No logout in m4a (§3.3) | None | `POST /ui/logout` is ~15 lines and a form on the layout; it fits m4e's admin view better than a foundation slice |
| **OR2** | Held digest items are counted, not listed (§3.6) | `Held int` rendered as one line | "Not mentioned at all" is `renderDigest`'s posture and hides part of the queue from the mirror; "listed" defeats the gate |
| **OR3** | "The unasked or open question" read as "the question the next digest would carry" (§3.6) | `Unasked()[0]` when `!low`; open ones counted | A second slot for `Open()[0]` is ~10 lines of model and template; it shows a thing the UI cannot answer until m4f |
| **OR4** | Cross-origin middleware lands in m4a PR 2; m4b's `feat/httpapi-cross-origin` row is discharged (§3.4) | Here | Deferring it ships `POST /ui/login` unprotected for a slice |
| **OR5** | The handshake is two PRs, 4a and 4b (§7), with an unreachable-UI state on `main` between them | Split | One ~420-line PR, over the ceiling |
| **OR6** | ADR-0028 rather than a Related-decision note (§3.10) | New ADR | A note has nowhere to live: ADR-0007 and ADR-0017 are both `Accepted` |
| **OR7** | I27 as a numbered invariant with a doc 06 §4 row (§8) | I27 | A conformance test with no row is what `nooma-testing` step 2 forbids; the alternative is an L2 brain test with no invariant name, which is how a rule becomes decoration |
| **Q6** | **Should the API's `POST` routes also sit behind `CrossOriginProtection`?** (§3.4) | **Not decided; unchanged.** Recommendation: no in M4 — ADR-0017's clients send neither header and pass, so the wrap changes nothing for them and nothing for a browser posting `text/plain`; if it is wanted, it is one line and its own ADR-0017 amendment, not a UI decision |
| **Q7** | **Does the focus render `Score`?** (§3.6) — **ruled, not open** (proposal §8, Q7): owner ruling, the approved Today wireframe shows each focus member with its priority number | **Ruled: yes** — rendered, two decimals, `NaN` as `NaN`, never coerced. The glass box (doc 02 §11) is the argument; a mirror that hides the number it ranked by is an advisor |

---

## 12. Risks this design adds or sharpens

Proposal §9's R1–R13 stand. R1 is closed by §7's order; R2 by §3.1's allow-list and I27; R4 by
§3.4; R5 by §3.8's Linux-only step; R9 by §3.5 landing in PR 2. New or sharpened:

| # | Risk | Mitigation |
|---|---|---|
| **N1** | **`Secure` is `false` on every current deployment** (§3.3): `serve.go` never terminates TLS, so `r.TLS` is always nil | Stated, not hidden; ADR-0007's accepted LAN posture. A TLS listener is a `serve.go` change and flips the flag with no cookie code change; `TestLoginIssuesTheCookie…` already asserts both arms |
| **N2** | **The in-between state after 4a**: a token-configured server's UI is unreachable from a browser until 4b lands | One PR apart, both small, named in 4a's PR body. The alternative is one over-ceiling PR |
| **N3** | **`go tool templ` pulls templ's command dependencies into `go.sum`** — a noisy first diff | Reported beside the budget in PR 1; none is linked into `nooma`; `go mod why` shows them as tool-only |
| **N4** | **htmx boots under the CSP only if the meta config is read before the script runs** | The `<meta>` precedes the `<script>` in `layout.templ`'s `<head>`, and the layout is one component; a browser check is the manual pass proposal §7 already owns |
| **N5** | **golangci's formatters may not honour `exclusions.generated`** for `_templ.go` | Verified in PR 1's gate run; the fallback is a path exclusion, one rule |
| **N6** | **`Cache-Control: no-store` on views and `no-cache` on assets are two arms** in one header function | Both asserted in `TestUISubtreeSetsSecurityHeaders` per route class |
| **N7** | **Two reads per Kind race with a nightly archive** (§3.7): a unit archived between `LiveFocusCandidatesByType` and `LiveByIDs` | `LiveByIDs` omits it; one fewer member, nothing misreported. Consolidation runs at 03:00 inside quiet hours; the window is microseconds |
| **N8** | **The proposal's Q2 cost statement was inverted** (§3.4): the stdlib admits, not refuses, a header-less client | Corrected here, asserted in `TestUICrossOriginPostIsRefused`'s fourth arm, and recorded in ADR-0028 so the next reader does not re-derive it from the proposal |
