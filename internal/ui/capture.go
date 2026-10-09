package ui

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/rengo/nooma/internal/brain"
)

// captureFormMaxBytes bounds a POST /ui/capture or correction submission's
// body (design §3.5) — http.MaxBytesReader's own limit, loginSubmit's own
// precedent (internal/httpapi/cookie.go) sized for a capture's longer text
// rather than a login form's short token.
const captureFormMaxBytes = 64 * 1024

// serveCaptureForm answers GET /ui/capture: the capture form alone, no
// result yet. Unlike serveCapture/serveCorrect, this needs no Capturer —
// rendering the form does not call it — so there is no nil check here.
func (h *Handler) serveCaptureForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = CapturePage(nil).Render(r.Context(), w)
}

// parseCaptureForm reads and bounds a capture or correction submission's
// body: MaxBytesReader at captureFormMaxBytes, then the submitted text —
// empty is a 400. Shared by serveCapture and serveCorrect; the
// brain.CaptureInput composite literal itself is deliberately NOT built
// here, so each handler's own literal stays the one place
// ui_entrances_test.go's AST gate (design §3.7, spec R5) inspects — a
// shared literal built from a parameter would hide the gate's own
// r.PathValue("id") pin behind an identifier.
func parseCaptureForm(w http.ResponseWriter, r *http.Request) (string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, captureFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return "", false
	}
	text := r.PostFormValue("text")
	if text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return "", false
	}
	return text, true
}

// serveCapture answers POST /ui/capture: builds a brain.CaptureInput from
// the submitted text and calls Capturer.Capture exactly as POST /capture
// already does (spec R4) — a submitted unit_id is never read, so it can
// never reach ReferentID; that field is the correction route's own job
// (serveCorrect, below).
func (h *Handler) serveCapture(w http.ResponseWriter, r *http.Request) {
	if h.deps.Capture == nil {
		http.Error(w, "capture: not wired in this build", http.StatusServiceUnavailable)
		return
	}

	text, ok := parseCaptureForm(w, r)
	if !ok {
		return
	}

	result, err := h.deps.Capture.Capture(r.Context(), brain.CaptureInput{Text: text, Channel: "ui"})
	if err != nil {
		slog.Error("capture: failed", "err", err)
		http.Error(w, "capture: internal error", http.StatusInternalServerError)
		return
	}

	renderCaptureResponse(w, r, result)
}

// serveCorrect answers POST /ui/units/{id}/correct: identical to
// serveCapture except ReferentID is set to the path's own id — the one
// place this package sets it (spec R5, design §3.7's gate c) — so this
// call always resolves through resolveReferent's explicit branch (doc 02
// §5 step 4) rather than chat's own hybrid-recall/ambiguity-gate path.
func (h *Handler) serveCorrect(w http.ResponseWriter, r *http.Request) {
	if h.deps.Capture == nil {
		http.Error(w, "capture: not wired in this build", http.StatusServiceUnavailable)
		return
	}

	text, ok := parseCaptureForm(w, r)
	if !ok {
		return
	}

	result, err := h.deps.Capture.Capture(r.Context(), brain.CaptureInput{
		Text:       text,
		Channel:    "ui",
		ReferentID: r.PathValue("id"),
	})
	if errors.Is(err, brain.ErrUnknownReferent) {
		// The page's own unit is gone, or the URL was typed: the user's
		// situation, not the server's (fix-unit-correction-form R4).
		http.Error(w, "No unit has that id.", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("correct: failed", "err", err)
		http.Error(w, "correct: internal error", http.StatusInternalServerError)
		return
	}

	renderCaptureResponse(w, r, result)
}

// renderCaptureResponse writes the capture result fragment alone on
// HX-Request, or the full capture page otherwise — renderUnits' own split
// (units.go, design §3.5).
func renderCaptureResponse(w http.ResponseWriter, r *http.Request, result brain.CaptureResult) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Header.Get("HX-Request") == "true" {
		_ = captureOutcome(result).Render(r.Context(), w)
		return
	}
	_ = CapturePage(&result).Render(r.Context(), w)
}
