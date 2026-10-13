package brain

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

var prefsNow = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

// failingPrefsWrite fails every preference write, so a test can prove a failed
// write logs nothing.
type failingPrefsWrite struct{ *memrepo.Config }

func (failingPrefsWrite) SetReminderPrefs(context.Context, ports.ReminderPrefsUpdate, time.Time) error {
	return errors.New("disk full")
}

func prefsService(cfg ports.ConfigRepo, log ports.DecisionLog) *PreferencesService {
	return NewPreferencesService(fixedClock{now: prefsNow}, &seqIDs{}, cfg, log)
}

func defaultPrefs() Preferences {
	return Preferences{LeadMinutes: []int{1440, 120}, DateOnlyAt: "09:00", QuietEnd: "07:00", EveningAt: "22:00"}
}

func prefsRows(t *testing.T, log *memrepo.DecisionLog) []ports.Decision {
	t.Helper()
	rows, err := log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	return rows
}

func TestPreferences_ShowTheDefaultsOnAVaultThatNeverChose(t *testing.T) {
	got, err := prefsService(memrepo.NewConfig(), memrepo.NewDecisionLog()).Preferences(context.Background())
	if err != nil {
		t.Fatalf("Preferences: %v", err)
	}
	if want := defaultPrefs(); !slices.Equal(got.LeadMinutes, want.LeadMinutes) || got.DateOnlyAt != want.DateOnlyAt ||
		got.QuietEnd != want.QuietEnd || got.EveningAt != want.EveningAt {
		t.Errorf("Preferences() = %+v, want the defaults %+v", got, want)
	}
}

func TestPreferences_ShowTheStoredValuesLargestLeadFirst(t *testing.T) {
	cfg := memrepo.NewConfig()
	leads, end := "[30,180]", "06:15"
	cfg.SeedConfig(t, ports.VaultConfig{EventReminderLeads: &leads, QuietHoursEnd: &end})
	got, err := prefsService(cfg, memrepo.NewDecisionLog()).Preferences(context.Background())
	if err != nil {
		t.Fatalf("Preferences: %v", err)
	}
	if !slices.Equal(got.LeadMinutes, []int{180, 30}) || got.QuietEnd != "06:15" || got.DateOnlyAt != "09:00" {
		t.Errorf("Preferences() = %+v, want leads [180 30], quiet end 06:15, date-only 09:00", got)
	}
}

func TestPreferencesSave_InvalidInputNamesEachFieldAndWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(p *Preferences)
		field PreferenceField
	}{
		{"no lead", func(p *Preferences) { p.LeadMinutes = nil }, FieldLeads},
		{"a zero lead", func(p *Preferences) { p.LeadMinutes = []int{0} }, FieldLeads},
		{"a negative lead", func(p *Preferences) { p.LeadMinutes = []int{-60} }, FieldLeads},
		{"a duplicate lead", func(p *Preferences) { p.LeadMinutes = []int{120, 120} }, FieldLeads},
		{"six leads", func(p *Preferences) { p.LeadMinutes = []int{1, 2, 3, 4, 5, 6} }, FieldLeads},
		{"a lead past 30 days", func(p *Preferences) { p.LeadMinutes = []int{prospection.MaxEventLeadDays*1440 + 1} }, FieldLeads},
		{"a malformed date-only time", func(p *Preferences) { p.DateOnlyAt = "9:00" }, FieldDateOnlyAt},
		{"an hour past 23", func(p *Preferences) { p.DateOnlyAt = "24:00" }, FieldDateOnlyAt},
		{"a quiet end at midnight", func(p *Preferences) { p.QuietEnd = "00:00" }, FieldQuietEnd},
		{"a quiet end past noon", func(p *Preferences) { p.QuietEnd = "12:01" }, FieldQuietEnd},
		{"an evening inside quiet hours", func(p *Preferences) { p.EveningAt = "06:59" }, FieldEveningAt},
		{"a malformed evening", func(p *Preferences) { p.EveningAt = "late" }, FieldEveningAt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, log := memrepo.NewConfig(), memrepo.NewDecisionLog()
			p := defaultPrefs()
			p.LeadMinutes = []int{180}
			tc.edit(&p)
			_, err := prefsService(cfg, log).Save(context.Background(), p)
			var perr *PreferencesError
			if !errors.As(err, &perr) || !errors.Is(err, prospection.ErrInvalidReminderPref) {
				t.Fatalf("Save error = %v, want a *PreferencesError wrapping ErrInvalidReminderPref", err)
			}
			if perr.Fields[tc.field] == nil || len(perr.Fields) != 1 {
				t.Errorf("fields in error = %v, want exactly %s", perr.Fields, tc.field)
			}
			if cfg.RowExists(t) || len(prefsRows(t, log)) != 0 {
				t.Error("an invalid save wrote config or logged a row")
			}
		})
	}
}

// The evening is judged against the quiet end submitted with it, not the
// one in force: moving both later together is valid.
func TestPreferencesSave_EveningIsJudgedAgainstTheSubmittedQuietEnd(t *testing.T) {
	p := defaultPrefs()
	p.QuietEnd, p.EveningAt = "11:00", "10:00"
	_, err := prefsService(memrepo.NewConfig(), memrepo.NewDecisionLog()).Save(context.Background(), p)
	var perr *PreferencesError
	if !errors.As(err, &perr) || perr.Fields[FieldEveningAt] == nil {
		t.Fatalf("Save error = %v, want the evening refused against the submitted 11:00 quiet end", err)
	}
}

func TestPreferencesSave_DefaultsOnAVaultThatNeverChoseWriteNothing(t *testing.T) {
	cfg, log := memrepo.NewConfig(), memrepo.NewDecisionLog()
	p := defaultPrefs()
	p.LeadMinutes = []int{120, 1440} // the same set in another order
	res, err := prefsService(cfg, log).Save(context.Background(), p)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(res.Changed) != 0 || cfg.RowExists(t) || len(prefsRows(t, log)) != 0 {
		t.Errorf("saving the defaults wrote: changed %v, row %v, %d log rows; want nothing — NULL means never chosen",
			res.Changed, cfg.RowExists(t), len(prefsRows(t, log)))
	}
}

func TestPreferencesSave_WritesOnlyWhatChangedAndRecordsThePreviousValue(t *testing.T) {
	cfg, log := memrepo.NewConfig(), memrepo.NewDecisionLog()
	p := defaultPrefs()
	p.LeadMinutes = []int{60, 2880}
	p.QuietEnd = "10:00"
	res, err := prefsService(cfg, log).Save(context.Background(), p)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !slices.Equal(res.Changed, []PreferenceField{FieldLeads, FieldQuietEnd}) {
		t.Errorf("Changed = %v, want leads and quiet end", res.Changed)
	}
	stored, _ := cfg.Load(context.Background())
	if stored.EventReminderLeads == nil || *stored.EventReminderLeads != "[2880,60]" || stored.QuietHoursEnd == nil || *stored.QuietHoursEnd != "10:00" {
		t.Errorf("stored = %+v, want leads [2880,60] and quiet end 10:00", stored)
	}
	if stored.DateOnlyReminderAt != nil || stored.EveningReminderAt != nil {
		t.Errorf("stored = %+v, want the unchanged fields left NULL", stored)
	}

	rows := prefsRows(t, log)
	if len(rows) != 1 || rows[0].Action != ports.ActionConfigUpdated || !rows[0].OccurredAt.Equal(prefsNow) {
		t.Fatalf("log = %+v, want one config.updated row at the save's instant", rows)
	}
	var change struct {
		Fields   []string          `json:"fields"`
		Previous map[string]string `json:"previous"`
		Next     map[string]string `json:"next"`
	}
	if err := json.Unmarshal(rows[0].Context, &change); err != nil {
		t.Fatalf("context: %v", err)
	}
	if !slices.Equal(change.Fields, []string{"event_reminder_leads", "quiet_hours_end"}) ||
		change.Previous["event_reminder_leads"] != "24 hours, 2 hours" || change.Next["event_reminder_leads"] != "2 days, 1 hour" ||
		change.Previous["quiet_hours_end"] != "07:00" || change.Next["quiet_hours_end"] != "10:00" {
		t.Errorf("change = %+v, want the two fields with the values in force before and after", change)
	}
}

func TestPreferencesSave_TheSameValuesAgainWriteNothing(t *testing.T) {
	cfg, log := memrepo.NewConfig(), memrepo.NewDecisionLog()
	end := "08:00"
	cfg.SeedConfig(t, ports.VaultConfig{QuietHoursEnd: &end})
	p := defaultPrefs()
	p.QuietEnd = "08:00"
	res, err := prefsService(cfg, log).Save(context.Background(), p)
	if err != nil || len(res.Changed) != 0 || len(prefsRows(t, log)) != 0 {
		t.Errorf("Save = %+v, %v with %d rows; want nothing changed", res, err, len(prefsRows(t, log)))
	}
}

// A corrupt stored value resolves to its default; submitting that default
// repairs the column rather than leaving it corrupt forever.
func TestPreferencesSave_ACorruptStoredValueIsRepaired(t *testing.T) {
	cfg, log := memrepo.NewConfig(), memrepo.NewDecisionLog()
	bad := "25:99"
	cfg.SeedConfig(t, ports.VaultConfig{DateOnlyReminderAt: &bad})
	res, err := prefsService(cfg, log).Save(context.Background(), defaultPrefs())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	stored, _ := cfg.Load(context.Background())
	if !slices.Equal(res.Changed, []PreferenceField{FieldDateOnlyAt}) || stored.DateOnlyReminderAt == nil || *stored.DateOnlyReminderAt != "09:00" {
		t.Errorf("Changed %v, stored %v; want the corrupt time rewritten as 09:00", res.Changed, stored.DateOnlyReminderAt)
	}
}

func TestPreferencesSave_AFailedWriteLogsNothing(t *testing.T) {
	log := memrepo.NewDecisionLog()
	p := defaultPrefs()
	p.QuietEnd = "08:00"
	_, err := prefsService(failingPrefsWrite{memrepo.NewConfig()}, log).Save(context.Background(), p)
	if err == nil || errors.Is(err, ErrWriteLanded) || len(prefsRows(t, log)) != 0 {
		t.Errorf("Save = %v with %d rows; want a plain error and nothing logged", err, len(prefsRows(t, log)))
	}
}

func TestPreferencesSave_AFailedLogAfterTheWriteSaysTheWriteLanded(t *testing.T) {
	cfg := memrepo.NewConfig()
	p := defaultPrefs()
	p.QuietEnd = "08:00"
	_, err := prefsService(cfg, memrepo.NewFailingDecisionLog(errors.New("log down"))).Save(context.Background(), p)
	var landed *WriteLandedError
	if !errors.As(err, &landed) || !landed.Record {
		t.Fatalf("Save error = %v, want a *WriteLandedError with Record", err)
	}
	if stored, _ := cfg.Load(context.Background()); stored.QuietHoursEnd == nil || *stored.QuietHoursEnd != "08:00" {
		t.Error("the value is not in force although the error says the write landed")
	}
}

func TestLeadText(t *testing.T) {
	for minutes, want := range map[int]string{1: "1 minute", 90: "90 minutes", 60: "1 hour", 120: "2 hours", 1440: "24 hours", 2880: "2 days", 4320: "3 days", 2940: "49 hours"} {
		if got := LeadText(minutes); got != want {
			t.Errorf("LeadText(%d) = %q, want %q", minutes, got, want)
		}
	}
}

// LeadUnit is a closed set: an unknown unit does not parse, and its
// minutes are 0, a lead core always refuses, so it can never be saved as a
// silent wrong value.
func TestLeadUnit_IsAClosedSet(t *testing.T) {
	for _, s := range []string{"minutes", "hours", "days"} {
		if u, ok := ParseLeadUnit(s); !ok || string(u) != s || u.Minutes() == 0 {
			t.Errorf("ParseLeadUnit(%q) = %v, %v; want the unit", s, u, ok)
		}
	}
	for _, s := range []string{"weeks", "", "Hours"} {
		if _, ok := ParseLeadUnit(s); ok {
			t.Errorf("ParseLeadUnit(%q) accepted an unknown unit", s)
		}
		if m := LeadUnit(s).Minutes(); m != 0 {
			t.Errorf("LeadUnit(%q).Minutes() = %d, want 0", s, m)
		}
	}
	_, err := prefsService(memrepo.NewConfig(), memrepo.NewDecisionLog()).Save(context.Background(),
		Preferences{LeadMinutes: []int{2 * LeadUnit("weeks").Minutes()}, DateOnlyAt: "09:00", QuietEnd: "07:00", EveningAt: "22:00"})
	var perr *PreferencesError
	if !errors.As(err, &perr) || perr.Fields[FieldLeads] == nil {
		t.Errorf("two weeks saved as %v, want the leads refused", err)
	}
}
