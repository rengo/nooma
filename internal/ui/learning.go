package ui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
)

// learningAct is one of the summary's two POSTs.
type learningAct string

const (
	learningUndo    learningAct = "undo"
	learningRelease learningAct = "release"
)

// learningPage is /ui/learning as rendered.
type learningPage struct {
	View    brain.LearningView
	Outcome *beliefResult
}

// nothingLearned is a summary with no change behind any value: no sentence
// has an instant.
func (p learningPage) nothingLearned() bool {
	for _, v := range p.View.Values {
		if v.At != nil || v.Pinned {
			return false
		}
	}
	return true
}

// serveLearning answers GET /ui/learning: the summary. It writes nothing.
func (h *Handler) serveLearning(w http.ResponseWriter, r *http.Request) {
	if h.deps.Learning == nil {
		http.Error(w, "learning: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	h.writeLearning(w, r, http.StatusOK, nil)
}

// serveLearningAct answers POST /ui/learning/undo and /release: the value is
// named by kind and key. A refusal is a notice beside the page and writes
// nothing (brain refuses before any write).
func (h *Handler) serveLearningAct(w http.ResponseWriter, r *http.Request, act learningAct) {
	if h.deps.Learning == nil {
		http.Error(w, "learning: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, captureFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	kind, key := brain.LearnedKind(r.PostFormValue("kind")), r.PostFormValue("key")
	do := h.deps.Learning.Undo
	if act == learningRelease {
		do = h.deps.Learning.Release
	}
	err := do(r.Context(), kind, key)
	status, outcome := http.StatusOK, beliefResult{Outcome: outcomeSaved, Message: "Undone: the value before Nooma's change is back, and Nooma leaves it alone."}
	if act == learningRelease {
		outcome.Message = "Nooma learns this value again, from what you do from now on."
	}
	var landed *brain.WriteLandedError
	switch {
	case err == nil:
	case errors.Is(err, brain.ErrNothingToUndo):
		status, outcome = http.StatusConflict, beliefResult{Outcome: outcomeConflict, Message: "Nothing was changed: there is no change of Nooma's to undo here any more."}
	case errors.Is(err, ports.ErrNotPinned):
		status, outcome = http.StatusConflict, beliefResult{Outcome: outcomeConflict, Message: "Nothing was changed: Nooma is already learning this value."}
	case errors.Is(err, brain.ErrUnknownLearnedValue):
		status, outcome = http.StatusBadRequest, beliefResult{Outcome: outcomeInvalid, Message: "Nothing was changed: Nooma learns no such value."}
	case errors.As(err, &landed):
		slog.Warn("learning: the act landed but its record failed", "act", act, "err", err)
		outcome = beliefResult{Outcome: outcomeSavedWithNotice, Message: landedNotice(landed, false)}
	default:
		slog.Error("learning: act failed", "act", act, "err", err)
		http.Error(w, "learning: internal error", http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Boosted") == "true" {
		w.Header().Set("HX-Replace-Url", "/ui/learning")
	}
	h.writeLearning(w, r, status, &outcome)
}

// writeLearning renders the page with status and an optional outcome.
func (h *Handler) writeLearning(w http.ResponseWriter, r *http.Request, status int, outcome *beliefResult) {
	v, err := h.deps.Learning.View(r.Context())
	if err != nil {
		slog.Error("learning: reading failed", "err", err)
		http.Error(w, "learning: internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = LearningPage(learningPage{View: v, Outcome: outcome}).Render(r.Context(), w)
}

var _ = context.Background
