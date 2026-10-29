package ui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rengo/nooma/internal/brain"
)

// Timers is /ui/timers' entrance (doc 02 §8): the live timers, and a cancel
// through the chat's own transition. *brain.TimersService satisfies it.
type Timers interface {
	Live(ctx context.Context) ([]brain.ListedTimer, error)
	Cancel(ctx context.Context, id string) (brain.ListedTimer, error)
}

// serveTimers answers GET /ui/timers: the live timers. It writes nothing.
func (h *Handler) serveTimers(w http.ResponseWriter, r *http.Request) {
	if h.deps.Timers == nil {
		http.Error(w, "timers: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	h.writeTimers(w, r, http.StatusOK, nil)
}

// serveTimerCancel answers POST /ui/timers/{id}/cancel: the cancel, then the
// list read again with a notice. A timer that rang or was cancelled since the
// page was drawn is said to be no longer set, and nothing is written.
func (h *Handler) serveTimerCancel(w http.ResponseWriter, r *http.Request) {
	if h.deps.Timers == nil {
		http.Error(w, "timers: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	cancelled, err := h.deps.Timers.Cancel(r.Context(), id)
	status := http.StatusOK
	var notice graphNotice
	var landed *brain.WriteLandedError
	switch {
	case err == nil:
		notice = graphNotice{Outcome: outcomeSaved, Message: "Timer cancelled: " + timerLine(r.Context(), cancelled) + "."}
	case errors.Is(err, brain.ErrTimerNoLongerSet):
		status, notice = http.StatusConflict, graphNotice{Outcome: outcomeConflict, Message: "That timer is no longer set: it rang or was cancelled."}
	case errors.As(err, &landed):
		slog.Warn("timers: the cancel landed but its record failed", "timer", id, "err", err)
		notice = graphNotice{Outcome: outcomeSavedWithNotice, Message: landedNoticeFor(landed, "Timer cancelled")}
	default:
		slog.Error("timers: cancel failed", "timer", id, "err", err)
		http.Error(w, "timers: internal error", http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Boosted") == "true" {
		w.Header().Set("HX-Replace-Url", "/ui/timers")
	}
	h.writeTimers(w, r, status, &notice)
}

// writeTimers renders the page with status and an optional notice.
func (h *Handler) writeTimers(w http.ResponseWriter, r *http.Request, status int, notice *graphNotice) {
	live, err := h.deps.Timers.Live(r.Context())
	if err != nil {
		slog.Error("timers: reading failed", "err", err)
		http.Error(w, "timers: internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = TimersPage(live, notice).Render(r.Context(), w)
}

// timerLine is one timer as the page names it: its instant in the user's
// zone, then what it says, or that it is a plain nudge.
func timerLine(ctx context.Context, t brain.ListedTimer) string {
	text := "a reminder with no text"
	if t.Text != nil && *t.Text != "" {
		text = *t.Text
	}
	return formatTime(ctx, t.FireAt) + " — " + text
}
