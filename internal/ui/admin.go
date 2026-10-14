package ui

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rengo/nooma/internal/brain"
)

// adminSetting is one editable setting as the page shows it: the field it
// posts to, in words, and the value in force as text.
type adminSetting struct {
	Field brain.ConfigField
	Label string
	Hint  string
	Value string
	// Error is the refusal of a submitted value, shown beside the field.
	Error string
}

// adminPage is /ui/admin as rendered.
type adminPage struct {
	View     brain.AdminView
	Settings []adminSetting
	// Outcome is what the last POST did, if this page answers one.
	Outcome *beliefResult
}

func newAdminPage(v brain.AdminView) adminPage {
	s := v.Settings
	return adminPage{View: v, Settings: []adminSetting{
		{brain.ConfigWeightThreshold, "Archive threshold", "A unit whose weight falls below this is archived by the nightly pass.", formatNumber(s.WeightThreshold), ""},
		{brain.ConfigHysteresisMargin, "Focus margin", "How far ahead a challenger must be to take the focus from the current one. 0 turns the protection off.", formatNumber(s.HysteresisMargin), ""},
		{brain.ConfigGoalStagnationDays, "Goal stagnation (days)", "Days without progress before a goal is called stagnant.", strconv.Itoa(s.GoalStagnationDays), ""},
		{brain.ConfigMentalLoadThreshold, "Mental load threshold", "How many open items count as too much at once.", strconv.Itoa(s.MentalLoadThreshold), ""},
		{brain.ConfigConsolidationEnabled, "Nightly consolidation", "Whether the nightly pass runs.", strconv.FormatBool(s.ConsolidationEnabled), ""},
	}}
}

// formatNumber is a float as the shortest text that reads back the same.
func formatNumber(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// serveAdmin answers GET /ui/admin: the five settings in force, the last full
// consolidation, the learned thresholds and the recent effects. It writes
// nothing.
func (h *Handler) serveAdmin(w http.ResponseWriter, r *http.Request) {
	if h.deps.Admin == nil {
		http.Error(w, "admin: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	v, err := h.deps.Admin.View(r.Context())
	if err != nil {
		slog.Error("admin: reading failed", "err", err)
		http.Error(w, "admin: internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = AdminPage(newAdminPage(v)).Render(r.Context(), w)
}

// serveAdminSave answers POST /ui/admin: one setting, named by field and
// carrying its text in value. The field is read through brain.ParseConfigField
// first, so a name outside the five never reaches Update. A refused value
// comes back as a 400 with the form shown again; a write that landed without
// its log row is a success with a notice.
func (h *Handler) serveAdminSave(w http.ResponseWriter, r *http.Request) {
	if h.deps.Admin == nil {
		http.Error(w, "admin: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, captureFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	field, err := brain.ParseConfigField(r.PostFormValue("field"))
	if err != nil {
		http.Error(w, "admin: unknown setting", http.StatusBadRequest)
		return
	}
	raw := r.PostFormValue("value")

	res, err := h.deps.Admin.Update(r.Context(), field, raw)
	var refused *brain.ConfigRangeError
	var landed *brain.WriteLandedError
	status, outcome := http.StatusOK, beliefResult{Outcome: outcomeSaved, Message: "Saved."}
	switch {
	case err == nil:
		if !res.Changed {
			outcome.Message = "Nothing changed: that value is already in force."
		}
	case errors.As(err, &refused):
		status, outcome = http.StatusBadRequest, beliefResult{Outcome: outcomeInvalid, Message: "Nothing was saved. Check the setting marked below."}
	case errors.As(err, &landed):
		slog.Warn("admin: the write landed but a follow-up write failed", "field", field, "err", err)
		outcome = beliefResult{Outcome: outcomeSavedWithNotice, Message: landedNotice(landed, false)}
	default:
		slog.Error("admin: save failed", "field", field, "err", err)
		http.Error(w, "admin: internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	v, viewErr := h.deps.Admin.View(r.Context())
	if viewErr != nil {
		// The write is settled; say so, without the page.
		slog.Error("admin: reading after a save failed", "err", viewErr)
		if status != http.StatusOK {
			http.Error(w, "admin: internal error", http.StatusInternalServerError)
			return
		}
		_ = beliefOutcomeView(outcome).Render(r.Context(), w)
		return
	}
	page := newAdminPage(v)
	page.Outcome = &outcome
	if refused != nil {
		page.refuse(refused)
	}
	if r.Header.Get("HX-Boosted") == "true" {
		w.Header().Set("HX-Replace-Url", "/ui/admin")
	}
	w.WriteHeader(status)
	_ = AdminPage(page).Render(r.Context(), w)
}

// refuse shows a refused setting with brain's message and, for a typed
// number, the text as typed. A choice has no text to give back: its select
// keeps the value in force, or no option would be selected.
func (p *adminPage) refuse(e *brain.ConfigRangeError) {
	for i := range p.Settings {
		if p.Settings[i].Field == e.Field {
			if e.Field != brain.ConfigConsolidationEnabled {
				p.Settings[i].Value = e.Raw
			}
			p.Settings[i].Error = e.Message
		}
	}
}
