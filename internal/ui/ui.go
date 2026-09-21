package ui

import "net/http"

// Deps is what New needs to build the mirror's handler. PR 2 lands the
// shell state — no TodayReader exists yet, because brain.Today and the
// service that produces it are PR 6's and PR 7's (design m4a §3.1, §4's
// tree) — so ServeHTTP renders layout.Page with an empty <main> holding one
// paragraph that says so, never a FOCUS/PENDING DIGEST/SYSTEM section.
type Deps struct{}

// Serving carries the two process facts SYSTEM needs that are not the
// brain's: the listener's effective bind address and whether /ui sits
// behind a cookie. Both are known to cmd/nooma/serve.go at wiring time and
// to no repository, so they reach this package as a plain struct rather
// than through brain — which the ui-boundary depguard rule denies transport
// for a reason (design m4a §3.1). Unused before PR 7's SYSTEM section
// exists.
type Serving struct {
	Bind       string
	CookieAuth bool
}

// Handler renders the mirror. From PR 2 through PR 6 it renders the shell
// state below — layout.Page with an empty <main> and one paragraph saying
// Today is not built yet — because TodayReader and Deps.Today do not exist
// until PR 7 (design m4a §3.1, §7.2's PR 2 tip row).
type Handler struct {
	deps Deps
}

// New builds the mirror's handler.
func New(deps Deps) *Handler {
	return &Handler{deps: deps}
}

// ServeHTTP renders PR 2's shell state unconditionally: layout.Page around
// shellBody(), nothing else. It decides nothing — internal/ui's own charter
// (doc.go) — and does not itself set any header; internal/httpapi's
// securityHeaders wraps the whole /ui subtree from the outside (design
// m4a §3.2).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = Page("nooma", shellBody()).Render(r.Context(), w)
}
