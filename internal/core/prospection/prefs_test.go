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

	if got := ResolveReminderPrefs(nil, nil, nil, nil, nil); !reflect.DeepEqual(got, def) {
		t.Errorf("nothing stored = %+v, want the defaults", got)
	}
	got := ResolveReminderPrefs(str(`[180]`), str("20:30"), nil, nil, nil)
	if !reflect.DeepEqual(got, ReminderPrefs{TimedLeads: []time.Duration{3 * time.Hour}, DateOnlyAt: TimeOfDay{20, 30}, QuietEnd: def.QuietEnd, EveningAt: def.EveningAt, RemindMeLead: def.RemindMeLead}) {
		t.Errorf("both stored = %+v", got)
	}
	got = ResolveReminderPrefs(str(`[0]`), str("20:30"), nil, nil, nil)
	if !reflect.DeepEqual(got.TimedLeads, def.TimedLeads) || got.DateOnlyAt != (TimeOfDay{20, 30}) {
		t.Errorf("a corrupt lead list = %+v, want default leads and the stored time", got)
	}
	got = ResolveReminderPrefs(str(`[180]`), str("25:00"), nil, nil, nil)
	if !reflect.DeepEqual(got.TimedLeads, []time.Duration{3 * time.Hour}) || got.DateOnlyAt != def.DateOnlyAt {
		t.Errorf("a corrupt time = %+v, want the stored leads and the default time", got)
	}
	if MaxEventReminderLeads != 5 || MaxEventLeadDays != 30 || MaxQuietEndHour != 12 {
		t.Error("the bounds drifted from doc 02 §13's rows")
	}
}

// TestParseQuietEnd: quiet end is a time of day from 00:01 to 12:00 — an
// empty night or one running past noon is refused (ADR-0030 point 5).
func TestParseQuietEnd(t *testing.T) {
	for in, want := range map[string]TimeOfDay{"00:01": {0, 1}, "07:00": {7, 0}, "12:00": {12, 0}} {
		if got, err := ParseQuietEnd(in); err != nil || got != want {
			t.Errorf("ParseQuietEnd(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"00:00", "12:01", "23:00", "7:00", "", "ab:cd"} {
		if got, err := ParseQuietEnd(in); !errors.Is(err, ErrInvalidReminderPref) {
			t.Errorf("ParseQuietEnd(%q) = %v, %v; want ErrInvalidReminderPref", in, got, err)
		}
	}
}

// TestParseEveningAt: the evening time is from quiet end to 23:59, so the
// evening before is never itself in quiet hours (ADR-0030 point 5).
func TestParseEveningAt(t *testing.T) {
	quietEnd := TimeOfDay{Hour: 7}
	for in, want := range map[string]TimeOfDay{"07:00": {7, 0}, "22:00": {22, 0}, "23:59": {23, 59}} {
		if got, err := ParseEveningAt(in, quietEnd); err != nil || got != want {
			t.Errorf("ParseEveningAt(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"06:59", "00:00", "24:00", "", "22"} {
		if got, err := ParseEveningAt(in, quietEnd); !errors.Is(err, ErrInvalidReminderPref) {
			t.Errorf("ParseEveningAt(%q) = %v, %v; want ErrInvalidReminderPref", in, got, err)
		}
	}
}

// TestResolveReminderPrefs_TheNight: quiet end and the evening time are
// used when valid, each falls back to its own default, and the evening is
// judged against the quiet end actually resolved.
func TestResolveReminderPrefs_TheNight(t *testing.T) {
	def := DefaultReminderPrefs()
	str := func(s string) *string { return &s }
	for _, tc := range []struct {
		name               string
		quietEnd, evening  *string
		wantQuiet, wantEve TimeOfDay
	}{
		{"both stored", str("08:30"), str("21:00"), TimeOfDay{8, 30}, TimeOfDay{21, 0}},
		{"neither stored", nil, nil, def.QuietEnd, def.EveningAt},
		{"a corrupt quiet end keeps the stored evening", str("13:00"), str("21:00"), def.QuietEnd, TimeOfDay{21, 0}},
		{"an evening before the stored quiet end falls back", str("09:00"), str("08:00"), TimeOfDay{9, 0}, def.EveningAt},
		{"an evening judged against the resolved quiet end, not the corrupt one", str("13:00"), str("08:00"), def.QuietEnd, TimeOfDay{8, 0}},
	} {
		got := ResolveReminderPrefs(nil, nil, tc.quietEnd, tc.evening, nil)
		if got.QuietEnd != tc.wantQuiet || got.EveningAt != tc.wantEve {
			t.Errorf("%s: QuietEnd, EveningAt = %v, %v; want %v, %v", tc.name, got.QuietEnd, got.EveningAt, tc.wantQuiet, tc.wantEve)
		}
		if !reflect.DeepEqual(got.TimedLeads, def.TimedLeads) || got.DateOnlyAt != def.DateOnlyAt {
			t.Errorf("%s: the leads moved: %+v", tc.name, got)
		}
	}
}
