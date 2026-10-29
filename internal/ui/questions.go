package ui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
)

// Questions answers the pending question Today shows, by its id (doc 02 §4,
// I09): the relation it is about is read from the question store, never named
// by the page. *brain.CurationService satisfies it.
type Questions interface {
	ConfirmQuestion(ctx context.Context, questionID string) error
	RejectQuestion(ctx context.Context, questionID string) error
}

// serveQuestionAnswer answers POST /ui/questions/{id}/confirm and /reject:
// the answer, then Today read again with a notice. A question no longer open
// is refused with a notice, and nothing is written.
func (h *Handler) serveQuestionAnswer(w http.ResponseWriter, r *http.Request, a curationAction) {
	if h.deps.Questions == nil || h.deps.Today == nil {
		http.Error(w, "today: answering is not wired in this build", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	act, verb := h.deps.Questions.ConfirmQuestion, "Link confirmed"
	if a == curationReject {
		act, verb = h.deps.Questions.RejectQuestion, "Link deleted"
	}
	status := http.StatusOK
	var notice graphNotice
	var landed *brain.WriteLandedError
	switch err := act(r.Context(), id); {
	case err == nil:
		notice = graphNotice{Outcome: outcomeSaved, Message: verb + "."}
	case errors.Is(err, brain.ErrQuestionNotOpen), errors.Is(err, ports.ErrRelationNotFound):
		status, notice = http.StatusConflict, graphNotice{Outcome: outcomeConflict, Message: "That question is no longer open: it was answered, or its link is gone."}
	case errors.As(err, &landed):
		slog.Warn("today: the answer landed but a follow-up write failed", "question", id, "err", err)
		notice = graphNotice{Outcome: outcomeSavedWithNotice, Message: landedNoticeFor(landed, verb)}
	default:
		slog.Error("today: answering failed", "question", id, "err", err)
		http.Error(w, "today: internal error", http.StatusInternalServerError)
		return
	}

	today, err := h.deps.Today.Today(r.Context())
	if err != nil {
		slog.Error("today: rendering failed", "err", err)
		http.Error(w, "today: internal error", http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Boosted") == "true" {
		w.Header().Set("HX-Replace-Url", "/ui")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = TodayAnswered(today, h.deps.Serving, notice).Render(r.Context(), w)
}
