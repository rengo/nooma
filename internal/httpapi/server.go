package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rengo/nooma/internal/brain"
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
	// wiring lands) — Handler itself never dereferences it; only a request
	// that reaches captureHandler does.
	Capture *brain.CaptureService
	// Recall is unused by this PR — POST /recall and the read-only unit
	// routes are 13c's — carried here now because design D10's own struct
	// literal declares it, and adding a field to a struct that already ships
	// is a second review, not a saved one.
	Recall *brain.RecallService
	// Token is the bearer token requireToken checks against, or "" for "no
	// token configured" — see ResolveToken (auth.go), the one function that
	// produces this value from server.auth_token_env.
	Token string
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

// apiRoutes is the guarded API surface's one declaration. 13c adds POST
// /recall, GET /units/{id} and GET /units — this function is where those
// entries land, not a second list somewhere else.
func apiRoutes(d Deps) []apiRoute {
	return []apiRoute{
		{pattern: "POST /capture", handler: captureHandler(d)},
	}
}

// Handler builds nooma's HTTP surface: an open mux for the root and the UI
// placeholder, and a guarded mux for every API route — design D10's "two
// muxes" shape. There is no exported way to reach the inner, guarded mux
// directly: every request that is not GET /{$} or GET /ui falls through to
// it wrapped in requireToken(d.Token), so a route registered in apiRoutes
// cannot be reached without the check already having run.
//
// M0 served two things and admitted to being early about both: an API root
// that reports what is running, and a UI placeholder. docs/01-architecture.md's
// Layer 2 promises the binary serves the user's complete frontend from the
// same process — the real views arrive in M4 (ADR-0008), but the route exists
// now because a /ui that 404s would make that promise untrue on day one.
//
// Neither open route is guarded (ADR-0017, spec R2.12): the UI's own
// authentication is ADR-0007's cookie handshake, which this PR does not
// implement.
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

	mux.HandleFunc("GET /ui", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, uiPlaceholder)
	})

	// Every path that is neither the open root nor the open UI placeholder
	// falls through to the guarded mux — including a path the guarded mux
	// itself does not recognize, which the guarded mux answers with its own
	// 404 (TestUnknownPathIs404 pins this: an unmatched path is still a 404,
	// never a catch-all).
	mux.Handle("/", guarded)

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

// uiPlaceholder says what it is. A blank page would leave the user wondering
// whether something failed; this one tells them the truth — the server is
// running and this surface is not built yet.
const uiPlaceholder = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>nooma</title></head>
<body>
<h1>nooma is running</h1>
<p>The interface arrives in M4. Until then, use the CLI:
<code>nooma status</code> and <code>nooma doctor</code>.</p>
</body>
</html>
`
