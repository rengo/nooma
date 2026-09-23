package ui

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/rengo/nooma/internal/brain"
)

// TodayReader is Today's own entry point, declared here rather than
// imported from brain: ui owns a narrow behavioural interface, and the
// view-model type (brain.Today) lives where it is produced (design m4a
// §3.1's "narrow behavioural interface" choice). *brain.TodayService
// satisfies it; a ui test stubs it with a fixed brain.Today instead of
// constructing a real service over seven fakes to render one page.
type TodayReader interface {
	Today(ctx context.Context) (brain.Today, error)
}

// Deps is what New needs to build the mirror's handler. Today is nil until
// wired (cmd/nooma's own transitional state before wireToday's result
// reaches this field, and every internal/httpapi test fixture that does
// not need a real view); ServeHTTP answers 503 for that state rather than
// PR 2 through PR 6's retired shell (design m4a §3.1, §7.2's PR 7 tip row).
type Deps struct {
	Today   TodayReader
	Serving Serving
}

// Serving carries the two process facts SYSTEM needs that are not the
// brain's: the listener's effective bind address and whether /ui sits
// behind a cookie. Both are known to cmd/nooma/serve.go at wiring time and
// to no repository, so they reach this package as a plain struct rather
// than through brain — which the ui-boundary depguard rule denies transport
// for a reason (design m4a §3.1).
type Serving struct {
	Bind       string
	CookieAuth bool
}

// Handler renders the mirror: Today unconditionally once a TodayReader is
// wired, 503 otherwise (design m4a §3.2, §7.2's PR 7 tip row).
type Handler struct {
	deps Deps
}

// New builds the mirror's handler.
func New(deps Deps) *Handler {
	return &Handler{deps: deps}
}

// ServeHTTP renders Today unconditionally: it decides nothing else —
// internal/ui's own charter (doc.go) — and does not itself set any
// security header; internal/httpapi's securityHeaders wraps the whole /ui
// subtree from the outside (design m4a §3.2). A nil TodayReader —
// ui.New(ui.Deps{}), PR 2's own shell-era construction — answers 503
// instead of reaching a view that no longer exists, captureHandler's own
// nil posture (internal/httpapi/capture.go).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.deps.Today == nil {
		http.Error(w, "today: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	today, err := h.deps.Today.Today(r.Context())
	if err != nil {
		// The reader is already the authenticated owner (requireCookie has
		// run by the time this handler is reached), so this crosses no
		// trust boundary today — but err may wrap a raw SQL error or a
		// filesystem path, and reflecting it verbatim would still leak
		// implementation detail to the browser for no reader-facing
		// benefit. The detail goes to the server-side log instead;
		// log/slog is part of $gostd, so this reaches a logger without
		// widening ui-boundary's allow-list (.golangci.yml).
		slog.Error("today: rendering failed", "err", err)
		http.Error(w, "today: internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = Today(today, h.deps.Serving).Render(r.Context(), w)
}
