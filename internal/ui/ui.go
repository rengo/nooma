package ui

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
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

// UnitsReader is /ui/units' own entry point into the mirror's second read
// model (design §3.3): Browse for one page of live units, Detail for one
// unit and its live relations. TodayReader's own split applies here too:
// Detail returns brain.UnitDetail, an OUTPUT type, never brain.UnitsService
// itself — the same "narrow behavioural interface" choice (design m4a
// §3.1). *brain.UnitsService satisfies it.
type UnitsReader interface {
	Browse(ctx context.Context, types []unit.Type, after *ports.BrowseCursor) (ports.BrowsePage, error)
	Detail(ctx context.Context, id string) (brain.UnitDetail, bool, error)
}

// Searcher is /ui/units' search entrance, and the only brain method this
// package may call to answer a query — RecallService.ForText, never
// ScoredFor (I22, spec R2): declaring no ScoredFor here keeps the
// non-admitting net structurally unreachable from internal/ui.
type Searcher interface {
	ForText(ctx context.Context, text string) ([]unit.Unit, bool, error)
}

// Capturer is /ui/capture's and the correction form's one entrance into the
// brain — brain.CaptureService.Capture unchanged (spec R4, R5), reached
// through this one-method interface exactly as TodayReader, UnitsReader and
// Searcher each narrow their own brain service to only what this package
// calls. *brain.CaptureService satisfies it.
type Capturer interface {
	Capture(ctx context.Context, in brain.CaptureInput) (brain.CaptureResult, error)
}

// Deps is what New needs to build the mirror's handler. Today, Units,
// Search and Capture are nil until wired (cmd/nooma's own transitional
// state, and every test fixture that does not need a real view); each nil
// dependency answers 503 for the routes that need it, rather than
// panicking on a nil receiver (design m4a §3.1, §3.4's typed-nil gotcha).
type Deps struct {
	Today   TodayReader
	Units   UnitsReader
	Search  Searcher
	Capture Capturer
	Beliefs Beliefs
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

// ServeHTTP switches on r.Pattern (design §3.1) — the same guardedUI
// target serves every leaf newUIMux registers, so this switch, not the
// mux, is where each pattern's own view is decided. Go's ServeMux sets
// r.Pattern to the exact pattern it matched before this handler ever runs;
// a request driven directly in a test sets it by hand
// (today_test.go:159). A pattern with no case here 404s — this package's
// own drift guard against a leaf newUIMux wires but this switch forgot
// (design N2, TestUIGuardedLeavesEachReachAView).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Pattern {
	case "GET /ui", "GET /ui/{$}":
		h.serveToday(w, r)
	case "GET /ui/units":
		h.serveUnits(w, r)
	case "GET /ui/units/{id}":
		h.serveUnit(w, r)
	case "GET /ui/capture":
		h.serveCaptureForm(w, r)
	case "POST /ui/capture":
		h.serveCapture(w, r)
	case "POST /ui/units/{id}/correct":
		h.serveCorrect(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveToday renders Today unconditionally: it decides nothing else —
// internal/ui's own charter (doc.go) — and does not itself set any
// security header; internal/httpapi's securityHeaders wraps the whole /ui
// subtree from the outside (design m4a §3.2). A nil TodayReader —
// ui.New(ui.Deps{}), PR 2's own shell-era construction — answers 503
// instead of reaching a view that no longer exists, captureHandler's own
// nil posture (internal/httpapi/capture.go).
func (h *Handler) serveToday(w http.ResponseWriter, r *http.Request) {
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
