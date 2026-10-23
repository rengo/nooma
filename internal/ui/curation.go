package ui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// Curator is the graph's one way to change a relation (ADR-0050): confirm
// it, or delete it through the chat's own reject (doc 02 §4, I10).
// *brain.CurationService satisfies it.
type Curator interface {
	Confirm(ctx context.Context, relationID string) error
	Reject(ctx context.Context, relationID string) error
}

// graphNotice is a curation's outcome as the graph page shows it: Outcome
// is the data-outcome the stylesheet colours by.
type graphNotice struct{ Outcome, Message string }

type curationAction int

const (
	curationConfirm curationAction = iota
	curationReject
)

// serveCuration answers POST /ui/graph/relations/{id}/confirm and /reject:
// the act, then the graph redrawn around the form's unit with a notice,
// which names what is missing when a follow-up write failed. A relation the
// graph does not draw is 404.
func (h *Handler) serveCuration(w http.ResponseWriter, r *http.Request, a curationAction) {
	if h.deps.Curation == nil || h.deps.Graph == nil {
		http.Error(w, "graph: curation is not wired in this build", http.StatusServiceUnavailable)
		return
	}
	centre := r.PostFormValue("unit")
	if centre == "" {
		http.Error(w, "graph: unit is required", http.StatusBadRequest)
		return
	}

	id := r.PathValue("id")
	act, notice := h.deps.Curation.Confirm, "Link confirmed"
	if a == curationReject {
		act, notice = h.deps.Curation.Reject, "Link deleted"
	}
	var landed *brain.WriteLandedError
	var shown graphNotice
	switch err := act(r.Context(), id); {
	case err == nil:
		shown = graphNotice{Outcome: outcomeSaved, Message: notice + "."}
	case errors.As(err, &landed):
		slog.Warn("graph: the curation landed but a follow-up write failed", "relation", id, "err", err)
		shown = graphNotice{Outcome: outcomeSavedWithNotice, Message: landedNoticeFor(landed, notice)}
	case errors.Is(err, ports.ErrRelationNotFound):
		http.NotFound(w, r)
		return
	default:
		slog.Error("graph: curation failed", "relation", id, "err", err)
		http.Error(w, "graph: internal error", http.StatusInternalServerError)
		return
	}

	n, found, err := h.deps.Graph.Neighbourhood(r.Context(), centre)
	if err != nil {
		slog.Error("graph: neighbourhood failed", "err", err)
		http.Error(w, "graph: internal error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = GraphPage(n, shown).Render(r.Context(), w)
}

// landedNoticeFor is landedNotice's wording for a curation.
func landedNoticeFor(e *brain.WriteLandedError, verb string) string {
	if e.Record {
		return verb + ", but the activity record of it could not be written."
	}
	return verb + ", but a follow-up write failed."
}

// graphUnits is every drawn unit by id, for the relation list's labels.
func graphUnits(n brain.Neighbourhood) map[string]unit.Unit {
	byID := map[string]unit.Unit{n.Centre.Unit.ID: n.Centre.Unit}
	for _, nb := range n.Neighbours {
		byID[nb.Unit.ID] = nb.Unit
	}
	return byID
}
