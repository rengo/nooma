package ui

import (
	"embed"
	"net/http"
)

// staticFS embeds internal/ui's three static files — the token-layer
// stylesheet (ADR-0018) and the vendored htmx bundle with its licence
// (ADR-0008). No network is read at render time; everything Assets serves
// is compiled into the binary.
//
//go:embed static/app.css static/htmx.min.js static/htmx.LICENSE
var staticFS embed.FS

// Assets serves exactly the three files staticFS embeds, each as an exact
// leaf pattern over http.ServeFileFS — never a wildcard subtree (design m4a
// §3.2, correcting an earlier draft). That draft mounted
// "/ui/static/{file...}" over http.FileServerFS and called the result
// audited; verified by probe, http.FileServerFS reads a request's file path
// from r.URL.Path rather than the wildcard's PathValue, so every lookup
// still carried the "/ui/static/" prefix the embedded sub-tree does not
// have — a 404 on every real request, not the 200 listing the earlier
// prose described (that 200 belongs to a different construction,
// http.StripPrefix over http.FileServerFS, which the "..." wildcard is
// itself the same subtree-redirect class as /ui's own corrected mount).
// Three known files named explicitly costs nothing and keeps the "no
// bare-trailing-slash, no ..." property every other /ui leaf already holds.
func Assets() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /ui/static/app.css", serveAsset("static/app.css"))
	mux.Handle("GET /ui/static/htmx.min.js", serveAsset("static/htmx.min.js"))
	mux.Handle("GET /ui/static/htmx.LICENSE", serveAsset("static/htmx.LICENSE"))
	return mux
}

// serveAsset answers one embedded file by its path inside staticFS.
// http.ServeFileFS sets Content-Type from the file's extension, falling
// back to content sniffing for htmx.LICENSE, which has none.
func serveAsset(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, name)
	})
}
