package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
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
	return NewPreferencesService(fixedClock{now: prefsNow}, &seqIDs{}, cfg, log, memrepo.NewUnits(), memrepo.NewTriggers())
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

	// The change's row, then the re-arm's completion (nothing was armed).
	rows := prefsRows(t, log)
	if len(rows) != 2 || rows[0].Action != ports.ActionConfigUpdated || !rows[0].OccurredAt.Equal(prefsNow) ||
		rows[1].Action != ports.ActionConfigRemindersFollowed {
		t.Fatalf("log = %+v, want one config.updated row at the save's instant, then config.reminders.followed", rows)
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

// Every field is judged in one save, so a refusal names all the wrong ones:
// a malformed evening is reported even when the quiet end it would be judged
// against is wrong too.
func TestPreferencesSave_SeveralInvalidFieldsAreAllNamed(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Preferences
		want []PreferenceField
	}{
		{"all four", Preferences{LeadMinutes: []int{0}, DateOnlyAt: "9", QuietEnd: "13:00", EveningAt: "late"},
			[]PreferenceField{FieldLeads, FieldDateOnlyAt, FieldQuietEnd, FieldEveningAt}},
		{"a bad quiet end and a malformed evening", Preferences{LeadMinutes: []int{120}, DateOnlyAt: "09:00", QuietEnd: "00:00", EveningAt: "22:61"},
			[]PreferenceField{FieldQuietEnd, FieldEveningAt}},
		{"a bad quiet end and a well-formed evening", Preferences{LeadMinutes: []int{120}, DateOnlyAt: "09:00", QuietEnd: "00:00", EveningAt: "05:00"},
			[]PreferenceField{FieldQuietEnd}},
		{"leads and the date-only time", Preferences{LeadMinutes: []int{60, 60}, DateOnlyAt: "24:00", QuietEnd: "07:00", EveningAt: "22:00"},
			[]PreferenceField{FieldLeads, FieldDateOnlyAt}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prefsService(memrepo.NewConfig(), memrepo.NewDecisionLog()).Save(context.Background(), tc.p)
			var perr *PreferencesError
			if !errors.As(err, &perr) {
				t.Fatalf("Save error = %v, want a *PreferencesError", err)
			}
			got := make([]PreferenceField, 0, len(perr.Fields))
			for f := range perr.Fields {
				got = append(got, f)
			}
			slices.Sort(got)
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("fields = %v, want exactly %v", got, want)
			}
		})
	}
}

// A stored evening is judged against the stored quiet end, as the arm-time
// read judges it: 06:30 under a stored 06:00 end is valid and is not
// rewritten by a save that submits it unchanged.
func TestPreferencesSave_AStoredEveningIsJudgedAgainstTheStoredQuietEnd(t *testing.T) {
	cfg, log := memrepo.NewConfig(), memrepo.NewDecisionLog()
	end, evening := "06:00", "06:30"
	cfg.SeedConfig(t, ports.VaultConfig{QuietHoursEnd: &end, EveningReminderAt: &evening})
	p := defaultPrefs()
	p.QuietEnd, p.EveningAt = "06:00", "06:30"
	res, err := prefsService(cfg, log).Save(context.Background(), p)
	if err != nil || len(res.Changed) != 0 || len(prefsRows(t, log)) != 0 {
		t.Errorf("Save = %+v, %v with %d rows; want nothing changed", res, err, len(prefsRows(t, log)))
	}
}

// Saved is what is in force after the save, the submitted values in their
// stored form: leads largest first.
func TestPreferencesSave_ResultHoldsTheValuesNowInForce(t *testing.T) {
	p := Preferences{LeadMinutes: []int{30, 300}, DateOnlyAt: "08:15", QuietEnd: "06:00", EveningAt: "21:00"}
	res, err := prefsService(memrepo.NewConfig(), memrepo.NewDecisionLog()).Save(context.Background(), p)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if want := (Preferences{LeadMinutes: []int{300, 30}, DateOnlyAt: "08:15", QuietEnd: "06:00", EveningAt: "21:00"}); !slices.Equal(res.Saved.LeadMinutes, want.LeadMinutes) ||
		res.Saved.DateOnlyAt != want.DateOnlyAt || res.Saved.QuietEnd != want.QuietEnd || res.Saved.EveningAt != want.EveningAt {
		t.Errorf("Saved = %+v, want %+v", res.Saved, want)
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

// A re-arm that stops part way leaves the values saved and says so; saving
// the same values again finishes it.
func TestPreferencesSave_AFailedReArmKeepsTheValuesAndASecondSaveFinishesIt(t *testing.T) {
	ctx := context.Background()
	cfg, log, units := memrepo.NewConfig(), memrepo.NewDecisionLog(), memrepo.NewUnits()
	event := prefsNow.Add(78 * time.Hour) // 15:00, clear of quiet hours at every lead
	if err := units.Create(ctx, unit.Unit{ID: "flight", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "flight",
		EventAt: &event, Source: "cli", CreatedAt: prefsNow, UpdatedAt: prefsNow}); err != nil {
		t.Fatalf("seeding unit: %v", err)
	}
	triggers := memrepo.NewTriggers()
	unitID, fireAt := "flight", event.Add(-2*time.Hour)
	if err := triggers.Create(ctx, ports.Trigger{ID: "flight-2h", UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "flight", LeadMinutes: 120}, FireAt: &fireAt}); err != nil {
		t.Fatalf("seeding trigger: %v", err)
	}
	p := defaultPrefs()
	p.LeadMinutes = []int{180}

	ids := &seqIDs{}
	broken := NewPreferencesService(fixedClock{now: prefsNow}, ids, cfg, log, units, &failingTriggers{err: errors.New("disk")})
	if _, err := broken.Save(ctx, p); !errors.Is(err, ErrRemindersNotFollowed) || errors.Is(err, ErrWriteLanded) {
		t.Fatalf("Save error = %v, want ErrRemindersNotFollowed alone", err)
	}
	if stored, _ := cfg.Load(ctx); stored.EventReminderLeads == nil || *stored.EventReminderLeads != "[180]" {
		t.Fatalf("stored leads = %v, want [180] saved despite the re-arm failure", stored.EventReminderLeads)
	}

	res, err := NewPreferencesService(fixedClock{now: prefsNow}, ids, cfg, log, units, triggers).Save(ctx, p)
	if err != nil || len(res.Changed) != 0 {
		t.Fatalf("second Save = %+v, %v; want nothing changed and no error", res, err)
	}
	armed, _ := triggers.ArmedForUnit(ctx, "flight")
	if len(armed) != 1 || !armed[0].FireAt.Equal(event.Add(-3*time.Hour)) {
		t.Errorf("armed after the second save = %+v, want one reminder 3 hours before", armed)
	}
}

// failingAfter lets the first n trigger writes land and fails every write
// after them, so a re-arm can stop part way.
type failingAfter struct {
	*memrepo.Triggers
	n, writes int
}

func (f *failingAfter) write() error {
	f.writes++
	if f.writes > f.n {
		return errors.New("disk")
	}
	return nil
}

func (f *failingAfter) Reschedule(ctx context.Context, id string, m ports.TriggerMove) error {
	if err := f.write(); err != nil {
		return err
	}
	return f.Triggers.Reschedule(ctx, id, m)
}

func (f *failingAfter) Expire(ctx context.Context, id string) error {
	if err := f.write(); err != nil {
		return err
	}
	return f.Triggers.Expire(ctx, id)
}

func (f *failingAfter) Create(ctx context.Context, t ports.Trigger) error {
	if err := f.write(); err != nil {
		return err
	}
	return f.Triggers.Create(ctx, t)
}

// A re-arm that moved one reminder and then failed is finished by the next
// save, which changes nothing: the moved one stays where it went, the one
// left is cancelled, nothing is duplicated, and a third save does nothing.
func TestPreferencesSave_AReArmThatStoppedAfterOneWriteIsFinishedOnce(t *testing.T) {
	ctx := context.Background()
	cfg, log, units := memrepo.NewConfig(), memrepo.NewDecisionLog(), memrepo.NewUnits()
	event := prefsNow.Add(78 * time.Hour) // 15:00, clear of quiet hours at every lead
	if err := units.Create(ctx, unit.Unit{ID: "flight", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "flight",
		EventAt: &event, Source: "cli", CreatedAt: prefsNow, UpdatedAt: prefsNow}); err != nil {
		t.Fatalf("seeding unit: %v", err)
	}
	triggers := &failingAfter{Triggers: memrepo.NewTriggers(), n: 1}
	unitID := "flight"
	for id, lead := range map[string]int{"flight-24h": 1440, "flight-2h": 120} {
		fireAt := event.Add(-time.Duration(lead) * time.Minute)
		if err := triggers.Triggers.Create(ctx, ports.Trigger{ID: id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
			Payload: ports.TriggerPayload{ActionText: "flight", LeadMinutes: lead}, FireAt: &fireAt}); err != nil {
			t.Fatalf("seeding trigger: %v", err)
		}
	}
	ids := &seqIDs{}
	svc := NewPreferencesService(fixedClock{now: prefsNow}, ids, cfg, log, units, triggers)
	p := defaultPrefs()
	p.LeadMinutes = []int{180}

	if _, err := svc.Save(ctx, p); !errors.Is(err, ErrRemindersNotFollowed) {
		t.Fatalf("first Save error = %v, want ErrRemindersNotFollowed", err)
	}
	if triggers.writes != 2 {
		t.Fatalf("trigger writes = %d, want the move to land and the cancel to fail", triggers.writes)
	}

	triggers.n = 100
	for i, want := range []int{1, 0} {
		res, err := svc.Save(ctx, p)
		if err != nil || len(res.Changed) != 0 {
			t.Fatalf("save %d = %+v, %v; want nothing changed and no error", i+2, res, err)
		}
		armed, _ := triggers.ArmedForUnit(ctx, "flight")
		if len(armed) != 1 || armed[0].ID != "flight-24h" || !armed[0].FireAt.Equal(event.Add(-3*time.Hour)) {
			t.Fatalf("after save %d armed = %+v, want flight-24h alone, 3 hours before", i+2, armed)
		}
		if got := triggers.writes - 2; i == 0 && got != want {
			t.Errorf("the finishing save made %d trigger writes, want only the cancel", got)
		}
	}
	if triggers.writes != 3 {
		t.Errorf("trigger writes in all = %d, want 3: the move, the failed cancel, the cancel; the third save writes none", triggers.writes)
	}
}

// When the change's config.updated row and its re-arm both fail, the error
// says both, so the page does not promise that saving again finishes it:
// nothing marks the re-arm pending (ErrRemindersNotFollowed's documented
// gap), and the next save that changes nothing re-arms nothing. The vault's
// own log holds an earlier change whose re-arm completed.
func TestPreferencesSave_ADoubleFailureSaysBoth(t *testing.T) {
	ctx := context.Background()
	cfg, ids := memrepo.NewConfig(), &seqIDs{}
	log := &switchingLog{DecisionLog: memrepo.NewDecisionLog()}
	first := defaultPrefs()
	first.EveningAt = "21:00"
	if _, err := NewPreferencesService(fixedClock{now: prefsNow}, ids, cfg, log, memrepo.NewUnits(), memrepo.NewTriggers()).Save(ctx, first); err != nil {
		t.Fatalf("the earlier change: %v", err)
	}

	p := first
	p.QuietEnd = "08:00"
	log.failing = true
	_, err := NewPreferencesService(fixedClock{now: prefsNow}, ids, cfg, log, memrepo.NewUnits(), &failingTriggers{err: errors.New("disk")}).Save(ctx, p)
	if !errors.Is(err, ErrRemindersNotFollowed) || !errors.Is(err, ErrWriteLanded) {
		t.Fatalf("Save error = %v, want both ErrRemindersNotFollowed and ErrWriteLanded", err)
	}
	log.failing = false
	triggers := &countingArmedReads{Triggers: memrepo.NewTriggers()}
	if _, err := NewPreferencesService(fixedClock{now: prefsNow}, ids, cfg, log, memrepo.NewUnits(), triggers).Save(ctx, p); err != nil || triggers.reads != 0 {
		t.Errorf("the next unchanged save = %v with %d re-arm reads, want no re-arm: nothing marked it pending", err, triggers.reads)
	}
}

// switchingLog fails every Record while failing is set.
type switchingLog struct {
	*memrepo.DecisionLog
	failing bool
}

func (l *switchingLog) Record(ctx context.Context, d ports.Decision) error {
	if l.failing {
		return errors.New("log down")
	}
	return l.DecisionLog.Record(ctx, d)
}

type countingArmedReads struct {
	*memrepo.Triggers
	reads int
}

func (c *countingArmedReads) ArmedUnitIDs(ctx context.Context) ([]string, error) {
	c.reads++
	return c.Triggers.ArmedUnitIDs(ctx)
}

// Pending is judged by write order alone: a change written after the last
// completed re-arm is pending even when the clock stepped back and stamped
// it earlier than that completion.
func TestPreferencesSave_PendingFollowsWriteOrderNotTheClock(t *testing.T) {
	ctx := context.Background()
	log := memrepo.NewDecisionLog()
	for i, row := range []struct {
		action ports.DecisionAction
		at     time.Time
	}{
		{ports.ActionConfigUpdated, prefsNow},
		{ports.ActionConfigRemindersFollowed, prefsNow},
		{ports.ActionConfigUpdated, prefsNow.Add(-time.Hour)}, // its re-arm never completed
	} {
		if err := log.Record(ctx, ports.Decision{ID: fmt.Sprintf("row-%d", i), Action: row.action, Context: []byte(`{}`), OccurredAt: row.at}); err != nil {
			t.Fatalf("seeding the log: %v", err)
		}
	}
	triggers := &countingArmedReads{Triggers: memrepo.NewTriggers()}
	if _, err := NewPreferencesService(fixedClock{now: prefsNow}, &seqIDs{}, memrepo.NewConfig(), log, memrepo.NewUnits(), triggers).Save(ctx, defaultPrefs()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if triggers.reads != 1 {
		t.Errorf("re-arm reads = %d, want the pending re-arm run once", triggers.reads)
	}
}

// sentReminders places what went out: a recorded event instant as is; a
// row from before that was recorded by fire_at plus lead_minutes; one with
// no lead (pulled to now, or at once) as about the event now in force, but
// only when it went out before that event — never one sent after it.
func TestSentReminders_PlacesWhatWentOut(t *testing.T) {
	event := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	fire := time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)
	got := sentReminders([]ports.FiredTrigger{
		{FireAt: fire, FiredAt: fire, Payload: ports.TriggerPayload{About: event.Add(time.Hour), UserLeadsMinutes: []int{180, 120}}},
		{FireAt: fire, FiredAt: fire, Payload: ports.TriggerPayload{LeadMinutes: 1440}},
		{FireAt: fire, FiredAt: fire},
		{FireAt: event.Add(time.Hour), FiredAt: event.Add(time.Hour)},
	}, event)
	if !got[0].About.Equal(event.Add(time.Hour)) || !slices.Equal(got[0].Leads, []time.Duration{3 * time.Hour, 2 * time.Hour}) {
		t.Errorf("recorded row = %+v, want its own instant and both leads", got[0])
	}
	// Placed at fire_at + lead_minutes, the date it was about (here before a
	// correction), never the event now in force.
	if !got[1].About.Equal(fire.Add(1440*time.Minute)) || got[1].About.Equal(event) {
		t.Errorf("legacy row with a lead = %+v, want it placed at fire_at + lead_minutes, not at the current event", got[1])
	}
	if !got[2].About.Equal(event) || len(got[2].Leads) != 0 {
		t.Errorf("legacy row with no lead, sent before the event = %+v, want it about the event, by time only", got[2])
	}
	if !got[3].About.IsZero() {
		t.Errorf("legacy row with no lead, sent after the event = %+v, want it placed nowhere", got[3])
	}
}

// A live reminder of a lead the new preferences drop expires as covered
// when what is left already went out, and its row says so, not "passed".
func TestPreferencesSave_ACoveredReminderSaysWhy(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)
	event := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	units, triggers, log := memrepo.NewUnits(), memrepo.NewTriggers(), memrepo.NewDecisionLog()
	if err := units.Create(ctx, unit.Unit{ID: "flight", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "flight",
		EventAt: &event, Source: "cli", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seeding unit: %v", err)
	}
	unitID := "flight"
	for _, tr := range []struct {
		id   string
		at   time.Time
		lead int
	}{{"flight-2h", time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC), 120}, {"flight-6h", time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC), 360}} {
		at := tr.at
		if err := triggers.Create(ctx, ports.Trigger{ID: tr.id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &at,
			Payload: ports.TriggerPayload{ActionText: "flight", LeadMinutes: int(event.Sub(at) / time.Minute), About: event, UserLeadsMinutes: []int{tr.lead}}}); err != nil {
			t.Fatalf("seeding trigger: %v", err)
		}
	}
	if err := triggers.Fire(ctx, "flight-2h", time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	p := defaultPrefs()
	p.LeadMinutes = []int{120}
	if _, err := NewPreferencesService(fixedClock{now: now}, &seqIDs{}, memrepo.NewConfig(), log, units, triggers).Save(ctx, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var cancelled []ports.Decision
	for _, d := range prefsRows(t, log) {
		if d.Action == ports.ActionConfigReminderCancelled {
			cancelled = append(cancelled, d)
		}
	}
	if len(cancelled) != 1 || !strings.Contains(cancelled[0].Rationale, "already sent covers its leads") {
		t.Errorf("cancelled rows = %+v, want one saying a reminder already sent covers the leads", cancelled)
	}
}
