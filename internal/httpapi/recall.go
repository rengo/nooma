package httpapi

import (
	"encoding/json"
	"net/http"
)

// recallRequest is POST /recall's request body (design D10 §5.2).
type recallRequest struct {
	Query string `json:"query"`
}

// recallResponse is what POST /recall answers with — the same units
// rendering and the same semantic_leg_available flag an `outcome: recalled`
// capture carries (design D10 §5.2: "a shared renderer, not two that agree
// today"). Unlike capture's own recall fork, which discards
// RecallService.ForText's degradation flag with `_` (tasks.md Conflicts
// §C13 finding 2, out of 13b's own file ownership to fix), this route calls
// ForText directly and renders it — closing half of that finding for this
// entrance.
type recallResponse struct {
	Units                []unitResponse `json:"units"`
	SemanticLegAvailable bool           `json:"semantic_leg_available"`
}

// recallHandler wires POST /recall to brain.RecallService.ForText — the
// exact same method capture's own `type: recall` routing calls (design D9,
// spec R2.5, I22) — over the caller's raw query text, never a normalized or
// classified one (Q3b: "no classify call on the read path"). This handler
// never touches internal/core/classify or ports.LLMProvider: neither symbol
// is reachable from this package (design's own dependency-rule check
// sanctions only internal/brain, internal/core/unit and crypto/subtle
// here), and RecallService's own constructor takes no ports.LLMProvider
// either — so "no LLM completion call occurs" (spec R2.4) is a structural
// fact this handler cannot violate, not a runtime check.
func recallHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// d.Recall is nil in the same transitional window captureHandler's
		// own d.Capture == nil branch guards (cmd/nooma/serve.go, until
		// 13d's full wiring lands) — the identical treatment applied to the
		// sibling dependency this PR's own route depends on, not a one-off:
		// nil check, 503, a detail-free body, no fallthrough that could
		// panic on a nil receiver.
		if d.Recall == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "recall is not wired in this build"})
			return
		}

		var req recallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the request body is not valid JSON"})
			return
		}
		if req.Query == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
			return
		}

		units, semanticLegAvailable, err := d.Recall.ForText(r.Context(), req.Query)
		if err != nil {
			// The same conservative default captureHandler uses (tasks.md
			// Conflicts §C13 finding 3): RecallService.ForText returns a
			// plain error with no exported way to distinguish an embedding-
			// provider failure (already degraded to the lexical leg alone,
			// design D9) from a lexical-leg failure — every error here is
			// therefore 500, never a silent 200.
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "recall failed"})
			return
		}

		rendered := make([]unitResponse, len(units))
		for i, u := range units {
			rendered[i] = renderUnit(u)
		}
		writeJSON(w, http.StatusOK, recallResponse{Units: rendered, SemanticLegAvailable: semanticLegAvailable})
	}
}
