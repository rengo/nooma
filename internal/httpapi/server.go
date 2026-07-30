package httpapi

import (
	"fmt"
	"net/http"
)

// Handler builds nooma's HTTP surface.
//
// M0 serves two things and admits to being early about both: an API root that
// reports what is running, and a UI placeholder. docs/01-architecture.md's Layer
// 2 promises the binary serves the user's complete frontend from the same
// process — the real views arrive in M4 (ADR-0008), but the route exists now
// because a /ui that 404s would make that promise untrue on day one.
//
// The mux is deliberately not a catch-all: an unknown path is a 404. A handler
// that answered everything would make the first genuinely missing route
// impossible to notice.
func Handler(version string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "{\"name\":\"nooma\",\"version\":%q,\"status\":\"ok\"}\n", version)
	})

	mux.HandleFunc("GET /ui", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, uiPlaceholder)
	})

	return mux
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
