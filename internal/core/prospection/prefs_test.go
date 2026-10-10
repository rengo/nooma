package prospection

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// TestParseEventReminderLeads is ADR-0029 point 5's lead validation: a JSON
// array of 1 to MaxEventReminderLeads whole minutes, each from 1 minute to
// MaxEventLeadDays days, no two equal.
func TestParseEventReminderLeads(t *testing.T) {
	maxMinutes := MaxEventLeadDays * 24 * 60
	valid := map[string][]time.Duration{
		`[1440,120]`:       {24 * time.Hour, 2 * time.Hour},
		`[180]`:            {3 * time.Hour},
		`[1]`:              {time.Minute},
		` [ 30 , 10080 ] `: {30 * time.Minute, 7 * 24 * time.Hour},
		`[1,2,3,4,5]`:      {time.Minute, 2 * time.Minute, 3 * time.Minute, 4 * time.Minute, 5 * time.Minute},
		`[43200]`:          {time.Duration(maxMinutes) * time.Minute},
	}
	for in, want := range valid {
		got, err := ParseEventReminderLeads(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ParseEventReminderLeads(%q) = %v, %v, want %v", in, got, err, want)
		}
	}
	invalid := []string{
		``, `[]`, `null`, `120`, `"120"`, `[0]`, `[-5]`, `[1.5]`, `["60"]`, `[43201]`,
		`[120,120]`, `[1,2,3,4,5,6]`, `[120]x`, `{"a":1}`, `[1e2]`,
	}
	for _, in := range invalid {
		if got, err := ParseEventReminderLeads(in); !errors.Is(err, ErrInvalidReminderPref) {
			t.Errorf("ParseEventReminderLeads(%q) = %v, %v, want ErrInvalidReminderPref", in, got, err)
		}
	}
}

// TestParseTimeOfDay is ADR-0029 point 5's time validation: HH:MM, 00:00 to
// 23:59, exactly that form.
func TestParseTimeOfDay(t *testing.T) {
	valid := map[string]TimeOfDay{"09:00": {9, 0}, "00:00": {0, 0}, "23:59": {23, 59}, "20:30": {20, 30}}
	for in, want := range valid {
		if got, err := ParseTimeOfDay(in); err != nil || got != want {
			t.Errorf("ParseTimeOfDay(%q) = %v, %v, want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "9:00", "24:00", "12:60", "12:5", "1200", "12:00:00", "-1:00", "ab:cd", " 09:00", "+9:00"} {
		if got, err := ParseTimeOfDay(in); err == nil {
			t.Errorf("ParseTimeOfDay(%q) = %v, nil, want an error", in, got)
		}
	}
}

// TestResolveReminderPrefs: each stored preference is used when valid and
// falls back to its own default when absent or invalid — never the other
// one's (ADR-0029 point 5).
func TestResolveReminderPrefs(t *testing.T) {
	def := DefaultReminderPrefs()
	str := func(s string) *string { return &s }

	if got := ResolveReminderPrefs(nil, nil); !reflect.DeepEqual(got, def) {
		t.Errorf("nothing stored = %+v, want the defaults", got)
	}
	got := ResolveReminderPrefs(str(`[180]`), str("20:30"))
	if !reflect.DeepEqual(got, ReminderPrefs{TimedLeads: []time.Duration{3 * time.Hour}, DateOnlyAt: TimeOfDay{20, 30}}) {
		t.Errorf("both stored = %+v", got)
	}
	got = ResolveReminderPrefs(str(`[0]`), str("20:30"))
	if !reflect.DeepEqual(got.TimedLeads, def.TimedLeads) || got.DateOnlyAt != (TimeOfDay{20, 30}) {
		t.Errorf("a corrupt lead list = %+v, want default leads and the stored time", got)
	}
	got = ResolveReminderPrefs(str(`[180]`), str("25:00"))
	if !reflect.DeepEqual(got.TimedLeads, []time.Duration{3 * time.Hour}) || got.DateOnlyAt != def.DateOnlyAt {
		t.Errorf("a corrupt time = %+v, want the stored leads and the default time", got)
	}
	if MaxEventReminderLeads != 5 || MaxEventLeadDays != 30 {
		t.Error("the bounds drifted from doc 02 §13's rows")
	}
}
