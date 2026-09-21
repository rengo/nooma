package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ui"
)

// Deps is everything Handler needs to build nooma's HTTP surface — ADR-0017's
// own struct literal (design D10). Token is "" when no token is configured;
// that state is reachable only on a loopback bind (TestRequireTokenNoOpOnlyOnLoopback
// pins this against DecideBinding's own truth table), so an empty Token here
// is never itself the vulnerability — only reaching it on a non-loopback bind
// would be, and DecideBinding is what prevents that.
type Deps struct {
	// Version is reported by the open API root — the same string M0 already
	// reported, unchanged by this PR.
	Version string
	// Capture is the one entry point POST /capture calls, unchanged
	// (spec R2.1's own MUST: "internal/httpapi... calls brain.CaptureService.Capture
	// unchanged"). Nil in a caller that has not wired production dependencies
	// yet (cmd/nooma/serve.go's own transitional state until 13d's full
	// wiring lands) — captureHandler checks for nil before every call and
	// answers 503 rather than reaching it; a nil Capture is never a crash,
	// on this build or any future one that leaves it unwired.
	Capture *brain.CaptureService
	// Recall is what POST /recall, GET /units/{id} and GET /units call
	// (spec R2.4/R2.6, design D10) — nil in a caller that has not wired
	// production dependencies yet (cmd/nooma/serve.go's own transitional
	// state until 13d's full wiring lands), the identical shape Capture's
	// own nil-dependency window has; every route below checks for nil
	// before every call and answers 503 rather than reaching it.
	Recall *brain.RecallService
	// Token is the bearer token requireToken checks against, or "" for "no
	// token configured" — see ResolveToken (auth.go), the one function that
	// produces this value from server.auth_token_env.
	Token string
	// UI is the mirror's own handler. Nil means the /ui subtree is not
	// mounted at all: neither "/ui" nor "/ui/" are registered on the open
	// mux, and a request falls through to the guarded mux exactly as any
	// other unknown path does — 404 with no token, 401 with one (design
	// m4a §3.9's --no-ui state, PR 3). cmd/nooma/serve.go wires
	// ui.New(ui.Deps{}) into this field unconditionally from this PR
	// onward (§3.9's "landing this across PR 2 and PR 3" correction); a
	// caller that constructs Deps directly — this package's own tests — is
	// free to leave it nil.
	UI *ui.Handler
}

// apiRoute is one entry of the guarded API surface — pattern and handler
// together, so this type is the "one slice" design D10 asks for: the same
// slice Handler registers from is the same slice TestGuardedRoutesRequireToken
// (server_test.go) iterates. A route added here is guarded by construction;
// there is no other way to reach the mux it is registered on.
type apiRoute struct {
	pattern string
	handler http.HandlerFunc
}

// apiRoutes is the guarded API surface's one declaration — every route this
// package mounts is registered here, and nowhere else (design D10's "one
// slice, two consumers" shape: the same slice Handler registers from is the
// same slice TestGuardedRoutesRequireToken iterates).
func apiRoutes(d Deps) []apiRoute {
	return []apiRoute{
		{pattern: "POST /capture", handler: captureHandler(d)},
		{pattern: "POST /recall", handler: recallHandler(d)},
		{pattern: "GET /units/{id}", handler: unitByIDHandler(d)},
		{pattern: "GET /units", handler: unitsListHandler(d)},
	}
}

// Handler builds nooma's HTTP surface: an open mux for the root and the UI
// subtree, and a guarded mux for every API route — design D10's "two
// muxes" shape. There is no exported way to reach the inner, guarded mux
// directly: every request that is not GET /{$} or under /ui falls through
// to it wrapped in requireToken(d.Token), so a route registered in
// apiRoutes cannot be reached without the check already having run.
//
// docs/01-architecture.md's Layer 2 promises the binary serves the user's
// complete frontend from the same process. M0 served an API root and a
// placeholder that said the interface would arrive in M4; design m4a §3.1
// replaces it with the mirror's real shell — the layout, no Today content
// yet, the paragraph that says why.
//
// Neither open route is guarded by requireToken (ADR-0017, spec R2.12):
// the UI's own authentication is ADR-0007's cookie handshake (PR 4a), not
// implemented here.
func Handler(d Deps) http.Handler {
	guardedMux := http.NewServeMux()
	for _, rt := range apiRoutes(d) {
		guardedMux.HandleFunc(rt.pattern, rt.handler)
	}
	guarded := requireToken(d.Token)(guardedMux)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "{\"name\":\"nooma\",\"version\":%q,\"status\":\"ok\"}\n", d.Version)
	})

	// The UI subtree mounts only when a handler was built for it. Both
	// forms of the path are registered on THIS open mux so that neither
	// triggers ServeMux's own subtree-root redirect: the exact "/ui"
	// pattern answers GET /ui directly, and the "/ui/" subtree forwards
	// everything below it — /ui/static/*, and from PR 4b /ui/login — to
	// the same handler, which does its own leaf-level routing inside
	// newUIMux (design m4a §3.2). Headers wrap outermost, then
	// cross-origin, then (from PR 4a) the cookie check.
	if d.UI != nil {
		xo := http.NewCrossOriginProtection()
		uiSubtree := securityHeaders(xo.Handler(newUIMux(d)))
		mux.Handle("/ui", uiSubtree)
		mux.Handle("/ui/", uiSubtree)
	}

	// Every path that is neither the open root nor the open UI subtree
	// falls through to the guarded mux — including a path the guarded mux
	// itself does not recognize, which the guarded mux answers with its own
	// 404 (TestUnknownPathIs404 pins this: an unmatched path is still a 404,
	// never a catch-all).
	mux.Handle("/", guarded)

	return mux
}

// newUIMux builds the UI subtree's own inner mux — one http.ServeMux whose
// every route is an explicit method+path leaf, never a bare-trailing-slash
// or "..." subtree, so none of them can trigger ServeMux's own subtree-root
// redirect (design m4a §3.2: the class that let GET /ui 307 to /ui/ from
// the mux itself, before any handler this package writes ever ran). PR 2
// registers the three static leaves and the two guarded leaves; the two
// /ui/login leaves land in PR 4b, once loginPage/loginSubmit exist to back
// them, and the two guarded leaves gain requireCookie's wrap in PR 4a —
// until then they dispatch straight to d.UI, which renders PR 2's shell
// (§3.1, §7.2's PR 2 tip row).
func newUIMux(d Deps) *http.ServeMux {
	mux := http.NewServeMux()

	assets := ui.Assets()
	mux.Handle("GET /ui/static/app.css", assets)
	mux.Handle("GET /ui/static/htmx.min.js", assets)
	mux.Handle("GET /ui/static/htmx.LICENSE", assets)

	mux.Handle("GET /ui", d.UI)
	mux.Handle("GET /ui/{$}", d.UI)

	return mux
}

// writeJSON is internal/httpapi's one JSON response writer — every handler
// in this package uses it, so a response's Content-Type header and encoding
// error handling are decided once, not per route.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
