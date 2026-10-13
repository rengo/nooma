package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// PreferencesService reads and saves the user's reminder and quiet-hours
// preferences (ADR-0029, ADR-0030): the timed-event leads, the date-only
// reminder time, the end of quiet hours and the evening time. Capture and
// correction read them at arm time, so a saved value reaches every reminder
// armed after it (ui-preferences design D4).
type PreferencesService struct {
	clock    ports.Clock
	ids      ports.IDGen
	cfg      ports.ConfigRepo
	log      ports.DecisionLog
	units    ports.UnitRepo
	triggers ports.TriggerRepo
}

// NewPreferencesService wires a PreferencesService. units and triggers are
// what a save re-arms (ADR-0029 point 6).
func NewPreferencesService(clock ports.Clock, ids ports.IDGen, cfg ports.ConfigRepo, log ports.DecisionLog, units ports.UnitRepo, triggers ports.TriggerRepo) *PreferencesService {
	return &PreferencesService{clock: clock, ids: ids, cfg: cfg, log: log, units: units, triggers: triggers}
}

// Preferences is the four preferences in the shape a form edits them:
// leads in whole minutes, times as HH:MM.
type Preferences struct {
	LeadMinutes []int
	DateOnlyAt  string
	QuietEnd    string
	EveningAt   string
}

// PreferenceField names one preference by its config column.
type PreferenceField string

// The four preference fields, in the order a save reports them.
const (
	FieldLeads      PreferenceField = "event_reminder_leads"
	FieldDateOnlyAt PreferenceField = "date_only_reminder_at"
	FieldQuietEnd   PreferenceField = "quiet_hours_end"
	FieldEveningAt  PreferenceField = "evening_reminder_at"
)

// PreferencesError is a save refused because at least one field is invalid;
// Fields holds each invalid field's reason. Nothing was written.
type PreferencesError struct {
	Fields map[PreferenceField]error
}

// Error implements error.
func (e *PreferencesError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, f := range []PreferenceField{FieldLeads, FieldDateOnlyAt, FieldQuietEnd, FieldEveningAt} {
		if err := e.Fields[f]; err != nil {
			parts = append(parts, err.Error())
		}
	}
	return "preferences: " + strings.Join(parts, "; ")
}

// Is reports whether target is prospection.ErrInvalidReminderPref.
func (e *PreferencesError) Is(target error) bool {
	return target == prospection.ErrInvalidReminderPref
}

// SaveResult is what a valid save did: the fields it wrote (none when
// every submitted value was already in force) and the values now in force.
type SaveResult struct {
	Changed []PreferenceField
	Saved   Preferences
}

// Preferences returns the values in force: each stored value, or its
// default when never chosen or invalid. Leads come largest first.
func (s *PreferencesService) Preferences(ctx context.Context) (Preferences, error) {
	stored, err := s.cfg.Load(ctx)
	if err != nil {
		return Preferences{}, fmt.Errorf("preferences: read config: %w", err)
	}
	return formPrefs(resolvePrefs(stored)), nil
}

// Save validates p with the parsers the arm-time read resolves with, then
// writes the fields whose value differs from the one in force — or whose
// stored value is invalid — records one config.updated row, and re-arms
// (rearm), returning ErrRemindersNotFollowed if that stops part way. An
// invalid field refuses the whole save with a *PreferencesError.
// The write comes before its row (m4e2-admin design §3.1): a failed write
// logs nothing, and a failed row after a landed write is a
// *WriteLandedError.
func (s *PreferencesService) Save(ctx context.Context, p Preferences) (SaveResult, error) {
	next, err := validatePrefs(p)
	if err != nil {
		return SaveResult{}, err
	}
	stored, err := s.cfg.Load(ctx)
	if err != nil {
		return SaveResult{}, fmt.Errorf("preferences: read config: %w", err)
	}
	inForce := formPrefs(resolvePrefs(stored))
	saved := formPrefs(next)

	var update ports.ReminderPrefsUpdate
	var changed []PreferenceField
	for _, f := range []struct {
		field    PreferenceField
		was, now string
		stored   *string
		to       **string
	}{
		{FieldLeads, leadsColumn(inForce.LeadMinutes), leadsColumn(saved.LeadMinutes), stored.EventReminderLeads, &update.EventReminderLeads},
		{FieldDateOnlyAt, inForce.DateOnlyAt, saved.DateOnlyAt, stored.DateOnlyReminderAt, &update.DateOnlyReminderAt},
		{FieldQuietEnd, inForce.QuietEnd, saved.QuietEnd, stored.QuietHoursEnd, &update.QuietHoursEnd},
		{FieldEveningAt, inForce.EveningAt, saved.EveningAt, stored.EveningReminderAt, &update.EveningReminderAt},
	} {
		if f.was != f.now || (f.stored != nil && !storedValid(f.field, *f.stored, stored)) {
			v := f.now
			*f.to = &v
			changed = append(changed, f.field)
		}
	}
	result := SaveResult{Changed: changed, Saved: saved}
	now := s.clock.Now()
	var landed error
	if len(changed) > 0 {
		if err := s.cfg.SetReminderPrefs(ctx, update, now); err != nil {
			return SaveResult{}, fmt.Errorf("preferences: write: %w", err)
		}
		if err := s.recordChange(ctx, changed, inForce, saved, now); err != nil {
			landed = &WriteLandedError{Record: true, Err: err}
		}
	}
	// A save that changes nothing re-arms only to finish a pending re-arm.
	if len(changed) == 0 {
		if pending, err := s.rearmPending(ctx); err != nil || !pending {
			return result, err
		}
	}
	if err := s.rearm(ctx, next, now); err != nil {
		return result, errors.Join(landed, fmt.Errorf("%w: %w", ErrRemindersNotFollowed, err))
	}
	if err := s.recordFollowed(ctx, now); err != nil {
		return result, errors.Join(landed, &WriteLandedError{Record: true, Err: err})
	}
	return result, landed
}

// validatePrefs parses every field, judging the evening time against the
// quiet end submitted with it, and collects each field's refusal.
func validatePrefs(p Preferences) (prospection.ReminderPrefs, error) {
	bad := map[PreferenceField]error{}
	var out prospection.ReminderPrefs
	var err error
	if out.TimedLeads, err = prospection.ParseEventReminderLeads(leadsColumn(p.LeadMinutes)); err != nil {
		bad[FieldLeads] = err
	}
	if out.DateOnlyAt, err = prospection.ParseTimeOfDay(p.DateOnlyAt); err != nil {
		bad[FieldDateOnlyAt] = err
	}
	if out.QuietEnd, err = prospection.ParseQuietEnd(p.QuietEnd); err != nil {
		bad[FieldQuietEnd] = err
		// With no valid night to judge it against, the evening can only be
		// checked for its form.
		_, err = prospection.ParseTimeOfDay(p.EveningAt)
	} else {
		out.EveningAt, err = prospection.ParseEveningAt(p.EveningAt, out.QuietEnd)
	}
	if err != nil {
		bad[FieldEveningAt] = err
	}
	if len(bad) > 0 {
		return prospection.ReminderPrefs{}, &PreferencesError{Fields: bad}
	}
	return out, nil
}

// storedValid reports whether a stored column parses, judged as
// ResolveReminderPrefs judges it.
func storedValid(f PreferenceField, v string, stored ports.VaultConfig) bool {
	var err error
	switch f {
	case FieldLeads:
		_, err = prospection.ParseEventReminderLeads(v)
	case FieldDateOnlyAt:
		_, err = prospection.ParseTimeOfDay(v)
	case FieldQuietEnd:
		_, err = prospection.ParseQuietEnd(v)
	case FieldEveningAt:
		_, err = prospection.ParseEveningAt(v, resolvePrefs(stored).QuietEnd)
	}
	return err == nil
}

func resolvePrefs(c ports.VaultConfig) prospection.ReminderPrefs {
	return prospection.ResolveReminderPrefs(c.EventReminderLeads, c.DateOnlyReminderAt, c.QuietHoursEnd, c.EveningReminderAt)
}

// formPrefs renders resolved preferences in form shape, leads largest first.
func formPrefs(p prospection.ReminderPrefs) Preferences {
	leads := make([]int, len(p.TimedLeads))
	for i, d := range p.TimedLeads {
		leads[i] = int(d / time.Minute)
	}
	slices.SortFunc(leads, func(a, b int) int { return b - a })
	return Preferences{LeadMinutes: leads, DateOnlyAt: clockText(p.DateOnlyAt), QuietEnd: clockText(p.QuietEnd), EveningAt: clockText(p.EveningAt)}
}

func clockText(d prospection.TimeOfDay) string { return fmt.Sprintf("%02d:%02d", d.Hour, d.Minute) }

// leadsColumn is the stored form of leads: a JSON array of minutes, largest
// first, so two orders of one set store and compare the same.
func leadsColumn(minutes []int) string {
	sorted := slices.Clone(minutes)
	slices.SortFunc(sorted, func(a, b int) int { return b - a })
	b, _ := json.Marshal(sorted) // a []int always marshals
	if sorted == nil {
		return "[]"
	}
	return string(b)
}

// LeadUnit is the unit a lead is shown and edited in.
type LeadUnit string

// The three lead units, smallest first.
const (
	LeadMinutes LeadUnit = "minutes"
	LeadHours   LeadUnit = "hours"
	LeadDays    LeadUnit = "days"
)

// ParseLeadUnit reads one of the three lead units, and nothing else.
func ParseLeadUnit(s string) (LeadUnit, bool) {
	switch u := LeadUnit(s); u {
	case LeadMinutes, LeadHours, LeadDays:
		return u, true
	}
	return "", false
}

// Minutes is how many minutes one u is, or 0 for a unit outside the set:
// a lead of 0 minutes is one core always refuses, so an unknown unit can
// never become a silent wrong value.
func (u LeadUnit) Minutes() int {
	switch u {
	case LeadDays:
		return 24 * 60
	case LeadHours:
		return 60
	case LeadMinutes:
		return 1
	}
	return 0
}

// SplitLead names a lead in the largest unit that divides it, except that a
// single day reads as 24 hours: the default is "24 hours before", and
// that is how people say it.
func SplitLead(minutes int) (int, LeadUnit) {
	switch {
	case minutes%LeadDays.Minutes() == 0 && minutes >= 2*LeadDays.Minutes():
		return minutes / LeadDays.Minutes(), LeadDays
	case minutes%LeadHours.Minutes() == 0:
		return minutes / LeadHours.Minutes(), LeadHours
	}
	return minutes, LeadMinutes
}

// LeadText is a lead in words: "2 hours", "1 minute", "3 days".
func LeadText(minutes int) string {
	n, u := SplitLead(minutes)
	word := string(u)
	if n == 1 {
		word = strings.TrimSuffix(word, "s")
	}
	return fmt.Sprintf("%d %s", n, word)
}

func leadsText(minutes []int) string {
	parts := make([]string, len(minutes))
	for i, m := range minutes {
		parts[i] = LeadText(m)
	}
	return strings.Join(parts, ", ")
}

// recordChange writes the config.updated row: the changed columns with the
// values in force before and after, readable as /ui/activity shows them.
func (s *PreferencesService) recordChange(ctx context.Context, changed []PreferenceField, was, now Preferences, at time.Time) error {
	text := func(p Preferences, f PreferenceField) string {
		switch f {
		case FieldLeads:
			return leadsText(p.LeadMinutes)
		case FieldDateOnlyAt:
			return p.DateOnlyAt
		case FieldQuietEnd:
			return p.QuietEnd
		}
		return p.EveningAt
	}
	fields := make([]string, len(changed))
	previous, next := map[string]string{}, map[string]string{}
	described := make([]string, len(changed))
	for i, f := range changed {
		fields[i] = string(f)
		previous[fields[i]], next[fields[i]] = text(was, f), text(now, f)
		described[i] = fmt.Sprintf("%s from %s to %s", f, previous[fields[i]], next[fields[i]])
	}
	contextJSON, err := json.Marshal(struct {
		Fields   []string          `json:"fields"`
		Previous map[string]string `json:"previous"`
		Next     map[string]string `json:"next"`
	}{fields, previous, next})
	if err != nil {
		return fmt.Errorf("preferences: encode change: %w", err)
	}
	if err := s.log.Record(ctx, ports.Decision{
		ID:         s.ids.New(),
		Action:     ports.ActionConfigUpdated,
		Rationale:  "the user changed their reminder preferences: " + strings.Join(described, "; "),
		Context:    contextJSON,
		OccurredAt: at,
	}); err != nil {
		return fmt.Errorf("preferences: record change: %w", err)
	}
	return nil
}
