package ui

import (
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
}

// adminPage is /ui/admin as rendered.
type adminPage struct {
	View     brain.AdminView
	Settings []adminSetting
}

func newAdminPage(v brain.AdminView) adminPage {
	s := v.Settings
	return adminPage{View: v, Settings: []adminSetting{
		{brain.ConfigWeightThreshold, "Archive threshold", "A unit whose weight falls below this is archived by the nightly pass.", formatNumber(s.WeightThreshold)},
		{brain.ConfigHysteresisMargin, "Focus margin", "How far ahead a challenger must be to take the focus from the current one. 0 turns the protection off.", formatNumber(s.HysteresisMargin)},
		{brain.ConfigGoalStagnationDays, "Goal stagnation (days)", "Days without progress before a goal is called stagnant.", strconv.Itoa(s.GoalStagnationDays)},
		{brain.ConfigMentalLoadThreshold, "Mental load threshold", "How many open items count as too much at once.", strconv.Itoa(s.MentalLoadThreshold)},
		{brain.ConfigConsolidationEnabled, "Nightly consolidation", "Whether the nightly pass runs.", strconv.FormatBool(s.ConsolidationEnabled)},
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
