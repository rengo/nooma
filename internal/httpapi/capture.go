package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
)

// captureRequest is POST /capture's request body (design D10 §5.1). Source
// defaults to "api" when absent — it becomes units.source, the caller's own
// fact. UnitID is optional and ignored unless the classification resolves to
// a correction (spec R1.5) — ignored rather than rejected, deliberately.
type captureRequest struct {
	Text   string `json:"text"`
	Source string `json:"source"`
	UnitID string `json:"unit_id"`
}

// captureResponse is what POST /capture answers with. Outcome is always
// present and is the discriminator every other field is conditional on
// (design D10 §5.1) — this spec does not mandate exact field names (R2.1),
// only that the mapping is total and every outcome is distinguishable.
type captureResponse struct {
	Outcome string `json:"outcome"`

	// OutcomeStored.
	UnitID     string   `json:"unit_id,omitempty"`
	Embedded   bool     `json:"embedded,omitempty"`
	Candidates []string `json:"candidates,omitempty"`

	// OutcomeRecalled.
	Units []unitResponse `json:"units,omitempty"`

	// OutcomeCorrected | OutcomeAsked.
	Correction *correctionResponse `json:"correction,omitempty"`
}

// correctionResponse renders brain.Correction — the fields captureRunner's
// correction path actually populates (result.go), not the illustrative
// "question"/"candidates"/"referent" shape design.md §5.1 sketches before
// 12g's own Correction struct concretized. See tasks.md Conflicts §C13.
type correctionResponse struct {
	UnitID    string   `json:"unit_id,omitempty"`
	Fields    []string `json:"fields,omitempty"`
	Ambiguous bool     `json:"ambiguous"`
}

// unitResponse is design D10 §5.3's shared rendering: id, type, content,
// status, weight, event_at, due_at, created_at, updated_at, and nothing
// else — weight_decay_rate, confidence and structured_data stay unrendered
// because nothing on this surface does anything with them yet.
type unitResponse struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Content   string     `json:"content"`
	Status    string     `json:"status"`
	Weight    float64    `json:"weight"`
	EventAt   *time.Time `json:"event_at,omitempty"`
	DueAt     *time.Time `json:"due_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func renderUnit(u unit.Unit) unitResponse {
	return unitResponse{
		ID:        u.ID,
		Type:      string(u.Type),
		Content:   u.Content,
		Status:    string(u.Status),
		Weight:    u.Weight,
		EventAt:   u.EventAt,
		DueAt:     u.DueAt,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

// captureHandler wires POST /capture to brain.CaptureService.Capture
// unchanged (spec R2.1's own MUST) — this function's only job is decoding
// the request and encoding the result; every decision already happened in
// brain/core before renderCaptureResult ever runs.
func captureHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// d.Capture is nil whenever a caller has not wired production
		// dependencies yet — cmd/nooma/serve.go's own transitional state
		// until 13d's full wiring lands (providers/repos/Index/services).
		// Without this check, an AUTHENTICATED request in that window
		// reaches d.Capture.Capture on a nil receiver and panics the whole
		// process: a trivial denial of service against anyone who builds
		// main between 13b and 13d landing, and worse than an ordinary bug
		// because the request that triggers it is the correct one. 503, not
		// 500 (nothing went wrong — this build just does not have the
		// dependency) and not 404 (the route exists); no more detail than
		// that, matching every other error response this handler writes.
		//
		// The more structural fix — refusing to build Handler at all when
		// Deps is incomplete — was considered and rejected here on purpose:
		// it would churn Handler's own signature and the guarded-mux tests
		// already proven against it, for a benefit small next to an honest
		// per-request 503. This check is not a placeholder scheduled for
		// removal once 13d wires the real services — once every Deps a
		// caller builds is complete, this branch is simply unreachable, and
		// it stays as the structural answer to "what if a future refactor
		// leaves a dependency unwired again."
		if d.Capture == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "capture is not wired in this build"})
			return
		}

		var req captureRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the request body is not valid JSON"})
			return
		}
		if req.Text == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required"})
			return
		}

		source := req.Source
		if source == "" {
			source = "api"
		}

		result, err := d.Capture.Capture(r.Context(), brain.CaptureInput{
			Text:       req.Text,
			Channel:    source,
			ReferentID: req.UnitID,
		})
		if err != nil {
			// design D10's aspiration is "provider failures -> 502, store
			// failures -> 500"; brain.CaptureService.Capture returns a plain
			// error with no exported way for this package to tell the two
			// apart without importing internal/core/classify (which the
			// design's own dependency-rule check does not list for this
			// package — see tasks.md Conflicts §C13). Every Capture error
			// is therefore 500 here — the conservative default, never a
			// silent 200.
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "capture failed"})
			return
		}

		status, body := renderCaptureResult(result)
		writeJSON(w, status, body)
	}
}

// renderCaptureResult is R2.1's own total switch over brain.AllCaptureOutcomes() —
// design D10: "stored" -> 201, every other outcome -> 200 with a body naming
// what happened. There is deliberately no default clause: a CaptureOutcome
// added later without a case here falls through to status 0, which
// TestAllCaptureOutcomesHaveAStatusMapping catches loudly rather than this
// function silently answering 200 for a member it does not know.
func renderCaptureResult(result brain.CaptureResult) (int, captureResponse) {
	switch result.Outcome {
	case brain.OutcomeStored:
		return http.StatusCreated, captureResponse{
			Outcome:    string(result.Outcome),
			UnitID:     result.UnitID,
			Embedded:   result.Embedded,
			Candidates: result.Candidates,
		}

	case brain.OutcomeDiscarded:
		return http.StatusOK, captureResponse{Outcome: string(result.Outcome)}

	case brain.OutcomeRecalled:
		units := make([]unitResponse, len(result.Recalled))
		for i, u := range result.Recalled {
			units[i] = renderUnit(u)
		}
		return http.StatusOK, captureResponse{Outcome: string(result.Outcome), Units: units}

	case brain.OutcomeCorrected, brain.OutcomeAsked:
		fields := make([]string, len(result.Correction.Fields))
		for i, f := range result.Correction.Fields {
			fields[i] = string(f)
		}
		return http.StatusOK, captureResponse{
			Outcome: string(result.Outcome),
			Correction: &correctionResponse{
				UnitID:    result.Correction.UnitID,
				Fields:    fields,
				Ambiguous: result.Correction.Ambiguous,
			},
		}
	}

	return 0, captureResponse{}
}
