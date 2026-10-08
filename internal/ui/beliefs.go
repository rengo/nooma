package ui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
)

// Beliefs is /ui/beliefs' own entry point into the belief read model and its
// two user writes (m4e design §3.9): ByFacet for the page, Edit and Retire
// for the two POSTs. Edit takes the content and nothing else, so a posted
// facet or confidence has nowhere to go. *brain.BeliefsService satisfies it.
type Beliefs interface {
	ByFacet(ctx context.Context) ([]brain.FacetBeliefs, error)
	Edit(ctx context.Context, id, content string) error
	Retire(ctx context.Context, id string) error
}

// The outcome markers a belief POST answers with (data-outcome).
const (
	outcomeSaved           = "saved"
	outcomeRetired         = "retired"
	outcomeSavedWithNotice = "saved-with-notice"
	outcomeInvalid         = "invalid"
	outcomeConflict        = "conflict"
)

// beliefResult is what a belief POST reports back: the outcome marker, the
// message shown to the user, and, for a rejected edit, the belief it was for
// and the text that was submitted, so the form can be shown again with it.
type beliefResult struct {
	Outcome string
	Message string
	ID      string
	Content string
}

// serveBeliefs answers GET /ui/beliefs: the active beliefs under the five
// facets, in the order brain.ByFacet returns them (spec R1). It writes
// nothing.
func (h *Handler) serveBeliefs(w http.ResponseWriter, r *http.Request) {
	if h.deps.Beliefs == nil {
		http.Error(w, "beliefs: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	groups, err := h.deps.Beliefs.ByFacet(r.Context())
	if err != nil {
		slog.Error("beliefs: listing failed", "err", err)
		http.Error(w, "beliefs: internal error", http.StatusInternalServerError)
		return
	}
	writeBeliefsPage(w, r, http.StatusOK, groups, nil)
}

// serveBeliefEdit answers POST /ui/beliefs/{id}/edit: the path's id and the
// submitted content, raw, and nothing else. The UI does not normalise or
// validate the text; brain does, once, and its refusals come back as the 400
// below. Any posted facet, confidence or id is never read.
func (h *Handler) serveBeliefEdit(w http.ResponseWriter, r *http.Request) {
	if h.deps.Beliefs == nil {
		http.Error(w, "beliefs: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, captureFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	content := r.PostFormValue("content")

	err := h.deps.Beliefs.Edit(r.Context(), id, content)
	h.respondBelief(w, r, err, beliefResult{Outcome: outcomeSaved, Message: "Saved.", ID: id, Content: content})
}

// serveBeliefRetire answers POST /ui/beliefs/{id}/retire: the path's id and
// nothing else; the body is never read.
func (h *Handler) serveBeliefRetire(w http.ResponseWriter, r *http.Request) {
	if h.deps.Beliefs == nil {
		http.Error(w, "beliefs: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")

	err := h.deps.Beliefs.Retire(r.Context(), id)
	h.respondBelief(w, r, err, beliefResult{Outcome: outcomeRetired, Message: "Retired.", ID: id})
}

// respondBelief maps a belief write's error to a status and a message
// (design §3.4 and §3.9), then answers the outcome fragment alone on
// HX-Request or the full page otherwise: renderUnits' own split.
func (h *Handler) respondBelief(w http.ResponseWriter, r *http.Request, err error, saved beliefResult) {
	var landed *brain.WriteLandedError
	res, status := saved, http.StatusOK
	switch {
	case err == nil:
	case errors.As(err, &landed):
		// The write landed, so this is success with a notice for the part
		// that is missing: a retry would only conflict with it.
		slog.Warn("beliefs: the write landed but a follow-up write failed", "belief", saved.ID, "err", err)
		res = beliefResult{Outcome: outcomeSavedWithNotice, Message: landedNotice(landed)}
	case errors.Is(err, selfmodel.ErrEmptyContent):
		res, status = invalidResult(saved, "Content cannot be empty."), http.StatusBadRequest
	case errors.Is(err, selfmodel.ErrContentTooLong):
		res, status = invalidResult(saved, "Content is too long: at most "+strconv.Itoa(selfmodel.MaxBeliefContentRunes)+" characters."), http.StatusBadRequest
	case errors.Is(err, ports.ErrBeliefNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, ports.ErrBeliefStatusConflict):
		// A lost compare-and-swap is not only a retire: the nightly pass can
		// rewrite a derived belief's text between the read and the write.
		res = beliefResult{Outcome: outcomeConflict, Message: "This belief changed or was retired since the page was loaded. Reload to see its current state."}
		status = http.StatusConflict
	default:
		slog.Error("beliefs: write failed", "belief", saved.ID, "err", err)
		http.Error(w, "beliefs: internal error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_ = beliefOutcomeView(res).Render(r.Context(), w)
		return
	}
	groups, listErr := h.deps.Beliefs.ByFacet(r.Context())
	if listErr != nil {
		// The write is already settled; say so, without the list.
		slog.Error("beliefs: listing after a write failed", "err", listErr)
	}
	writeBeliefsPage(w, r, status, groups, &res)
}

// invalidResult is a rejected edit's result: the message, and the belief and
// the submitted text so the form can be shown again with them.
func invalidResult(submitted beliefResult, message string) beliefResult {
	return beliefResult{Outcome: outcomeInvalid, Message: message, ID: submitted.ID, Content: submitted.Content}
}

// landedNotice names only the part of a landed write that is missing, so it
// never claims the record failed when only the signal did (design §3.4).
func landedNotice(e *brain.WriteLandedError) string {
	switch {
	case e.Record && e.Signal:
		return "Saved, but neither the activity record nor the learning signal could be written."
	case e.Record:
		return "Saved, but the activity record of it could not be written."
	case e.Signal:
		return "Saved and recorded, but the learning signal could not be written."
	default:
		return "Saved, but a follow-up write failed."
	}
}

// writeBeliefsPage writes the full page with status.
func writeBeliefsPage(w http.ResponseWriter, r *http.Request, status int, groups []brain.FacetBeliefs, res *beliefResult) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = BeliefsPage(groups, res).Render(r.Context(), w)
}

// rejected reports whether res is a rejected edit of b, whose form is shown
// again with the submitted text instead of the stored one.
func rejected(b ports.Belief, res *beliefResult) bool {
	return res != nil && res.Outcome == outcomeInvalid && res.ID == b.ID
}

// editText is the text b's edit form starts with.
func editText(b ports.Belief, res *beliefResult) string {
	if rejected(b, res) {
		return res.Content
	}
	return b.Content
}
