package httpapi

import (
	"net/http"
	"strings"
)

// uiCSP is the fixed Content-Security-Policy every /ui response carries
// (design m4a §3.5): no inline script or style, no eval anywhere. htmx 2
// runs under this with its own documented CSP mode — layout.templ's
// <meta name="htmx-config"> sets allowEval:false, the one thing in htmx
// that would otherwise need 'unsafe-eval' — and the stylesheet is served
// from its own route (/ui/static/app.css) rather than inlined, so
// style-src needs no exception either (ADR-0018).
const uiCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'"

// securityHeaders sets design m4a §3.5's fixed header set on every response
// under /ui, including a refusal or a 404 — it is the outermost wrap in
// the chain (§3.2: "headers, then cross-origin, then cookie"), so nothing
// under /ui can answer without them. A view is vault data and must not
// survive in a shared browser's cache or back-forward store
// (Cache-Control: no-store); the static assets change only with the binary
// and carry no version in their URL, so they revalidate instead
// (Cache-Control: no-cache).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", uiCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/ui/static/") {
			h.Set("Cache-Control", "no-cache")
		} else {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
