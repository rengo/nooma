package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
)

// Preferences is /ui/preferences' entry point into the user's reminder and
// quiet-hours preferences (ui-preferences design D7): the values in force,
// and a save that validates, writes, records and re-arms. It reads and
// writes nothing else. *brain.PreferencesService satisfies it.
type Preferences interface {
	Preferences(ctx context.Context) (brain.Preferences, error)
	Save(ctx context.Context, p brain.Preferences) (brain.SaveResult, error)
}

// leadRow is one row of the leads editor, as typed.
type leadRow struct {
	Amount string
	Unit   brain.LeadUnit
}

// prefsForm is the preferences form as shown: the rows and times it holds,
// each field's refusal keyed by the brain's field name (also the inputs'
// names), and the outcome of the POST that produced it.
type prefsForm struct {
	Rows                          []leadRow
	DateOnlyAt, QuietEnd, Evening string
	Errors                        map[brain.PreferenceField]string
	Outcome                       *beliefResult
}

// leadUnits is every unit the editor offers, smallest first.
var leadUnits = []brain.LeadUnit{brain.LeadMinutes, brain.LeadHours, brain.LeadDays}

// prefsMessage restates each rule with core's own bounds (design D8): the
// UI names the field, the brain decides.
func prefsMessage(field brain.PreferenceField) string {
	switch field {
	case brain.FieldLeads:
		return fmt.Sprintf("Give 1 to %d leads, each a whole number from 1 minute to %d days, no two the same.",
			prospection.MaxEventReminderLeads, prospection.MaxEventLeadDays)
	case brain.FieldDateOnlyAt:
		return "Use a time from 00:00 to 23:59."
	case brain.FieldQuietEnd:
		return fmt.Sprintf("Quiet hours must end between 00:01 and %02d:00.", prospection.MaxQuietEndHour)
	}
	return "The evening time must be at or after the end of quiet hours, and no later than 23:59."
}

// formFromPrefs is the form showing p, plus an empty row while there is room.
func formFromPrefs(p brain.Preferences) prefsForm {
	f := prefsForm{DateOnlyAt: p.DateOnlyAt, QuietEnd: p.QuietEnd, Evening: p.EveningAt}
	for _, m := range p.LeadMinutes {
		n, u := brain.SplitLead(m)
		f.Rows = append(f.Rows, leadRow{Amount: strconv.Itoa(n), Unit: u})
	}
	return f.withSpareRow()
}

func (f prefsForm) withSpareRow() prefsForm {
	if len(f.Rows) < prospection.MaxEventReminderLeads {
		f.Rows = append(f.Rows, leadRow{Unit: brain.LeadHours})
	}
	return f
}

// formFromRequest reads the posted form as typed.
func formFromRequest(r *http.Request) prefsForm {
	f := prefsForm{DateOnlyAt: r.PostFormValue(string(brain.FieldDateOnlyAt)), QuietEnd: r.PostFormValue(string(brain.FieldQuietEnd)),
		Evening: r.PostFormValue(string(brain.FieldEveningAt))}
	units := r.PostForm["lead_unit"]
	for i, amount := range r.PostForm["lead_amount"] {
		row := leadRow{Amount: strings.TrimSpace(amount), Unit: brain.LeadHours}
		if i < len(units) {
			row.Unit = brain.LeadUnit(units[i])
		}
		f.Rows = append(f.Rows, row)
	}
	return f
}

// leadMinutes turns the filled rows into minutes; an empty row is no lead.
// A row that is not a whole number in a known unit refuses the leads.
func (f prefsForm) leadMinutes() ([]int, bool) {
	limit := prospection.MaxEventLeadDays * brain.LeadDays.Minutes()
	var out []int
	for _, row := range f.Rows {
		if row.Amount == "" {
			continue
		}
		n, err := strconv.Atoi(row.Amount)
		if err != nil || !slices.Contains(leadUnits, row.Unit) {
			return nil, false
		}
		m := n * row.Unit.Minutes()
		if n > limit || n < -limit {
			m = limit + 1 // still out of range, never an overflowed product
		}
		out = append(out, m)
	}
	return out, true
}

// servePreferences answers GET /ui/preferences: the values in force.
func (h *Handler) servePreferences(w http.ResponseWriter, r *http.Request) {
	if h.deps.Preferences == nil {
		http.Error(w, "preferences: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	p, err := h.deps.Preferences.Preferences(r.Context())
	if err != nil {
		slog.Error("preferences: reading failed", "err", err)
		http.Error(w, "preferences: internal error", http.StatusInternalServerError)
		return
	}
	writePreferencesPage(w, r, http.StatusOK, formFromPrefs(p))
}

// servePreferencesSave answers POST /ui/preferences: op "add" and
// "remove-<i>" edit the rows without saving; anything else saves the form.
func (h *Handler) servePreferencesSave(w http.ResponseWriter, r *http.Request) {
	if h.deps.Preferences == nil {
		http.Error(w, "preferences: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, captureFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f := formFromRequest(r)
	if op := r.PostFormValue("op"); op == "add" || strings.HasPrefix(op, "remove-") {
		h.editRows(w, r, f, op)
		return
	}

	minutes, ok := f.leadMinutes()
	if !ok {
		refusePreferences(w, r, f, brain.FieldLeads)
		return
	}
	res, err := h.deps.Preferences.Save(r.Context(), brain.Preferences{LeadMinutes: minutes, DateOnlyAt: f.DateOnlyAt, QuietEnd: f.QuietEnd, EveningAt: f.Evening})
	var refused *brain.PreferencesError
	var landed *brain.WriteLandedError
	notFollowed := errors.Is(err, brain.ErrRemindersNotFollowed)
	switch {
	case errors.As(err, &refused):
		var fields []brain.PreferenceField
		for field := range refused.Fields {
			fields = append(fields, field)
		}
		refusePreferences(w, r, f, fields...)
		return
	case err != nil && !notFollowed && !errors.As(err, &landed):
		slog.Error("preferences: save failed", "err", err)
		http.Error(w, "preferences: internal error", http.StatusInternalServerError)
		return
	}
	f = formFromPrefs(res.Saved)
	f.Outcome = &beliefResult{Outcome: outcomeSaved, Message: "Preferences saved."}
	switch {
	case notFollowed:
		slog.Warn("preferences: saved, but the reminders already set were not all moved", "err", err)
		f.Outcome = &beliefResult{Outcome: outcomeSavedWithNotice, Message: "Saved, but some reminders already set could not be moved. Save again to finish."}
	case landed != nil:
		slog.Warn("preferences: the write landed but a follow-up write failed", "err", err)
		f.Outcome = &beliefResult{Outcome: outcomeSavedWithNotice, Message: landedNotice(landed, false)}
	case len(res.Changed) == 0:
		f.Outcome.Message = "Nothing changed."
	}
	writePreferencesPage(w, r, http.StatusOK, f)
}

// refusePreferences answers a refused save: each named field's rule beside
// it, what was typed kept, and nothing saved.
func refusePreferences(w http.ResponseWriter, r *http.Request, f prefsForm, fields ...brain.PreferenceField) {
	f.Errors = map[brain.PreferenceField]string{}
	for _, field := range fields {
		f.Errors[field] = prefsMessage(field)
	}
	f.Outcome = &beliefResult{Outcome: outcomeInvalid, Message: "Nothing was saved. Check the fields marked below."}
	writePreferencesPage(w, r, http.StatusBadRequest, f)
}

// editRows adds an empty row or removes one, and answers the leads editor
// alone to htmx, the whole page otherwise. Nothing is saved.
func (h *Handler) editRows(w http.ResponseWriter, r *http.Request, f prefsForm, op string) {
	if op == "add" {
		if len(f.Rows) < prospection.MaxEventReminderLeads {
			f.Rows = append(f.Rows, leadRow{Unit: brain.LeadHours})
		}
	} else if i, err := strconv.Atoi(strings.TrimPrefix(op, "remove-")); err == nil && i >= 0 && i < len(f.Rows) {
		f.Rows = append(f.Rows[:i:i], f.Rows[i+1:]...)
	}
	if len(f.Rows) == 0 {
		f = f.withSpareRow()
	}
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = leadsEditor(f).Render(r.Context(), w)
		return
	}
	writePreferencesPage(w, r, http.StatusOK, f)
}

// writePreferencesPage writes the full page with status. A boosted POST
// would leave its URL in the address bar, and a reload of it is a 405.
func writePreferencesPage(w http.ResponseWriter, r *http.Request, status int, f prefsForm) {
	if r.Method == http.MethodPost && r.Header.Get("HX-Boosted") == "true" {
		w.Header().Set("HX-Replace-Url", "/ui/preferences")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = PreferencesPage(f).Render(r.Context(), w)
}
