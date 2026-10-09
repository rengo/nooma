package correction

import (
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
)

func TestFollowDate(t *testing.T) {
	art := time.FixedZone("ART", -3*60*60)
	at := func(y int, m time.Month, d, h, min int, loc *time.Location) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, loc)
	}

	cases := []struct {
		name       string
		text       string
		prev, next time.Time
		zone       *time.Location
		want       string
		changed    bool
	}{
		{"the client's body, date moved", "dentista el 2026-10-16 a las 10",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 10, 0, time.UTC), time.UTC,
			"dentista el 2026-10-09 a las 10", true},
		{"date and time moved", "dentista el 2026-10-16 a las 10:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 30, time.UTC), time.UTC,
			"dentista el 2026-10-09 a las 11:30", true},
		{"time only moved", "dentista el 2026-10-16 a las 10:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 16, 11, 0, time.UTC), time.UTC,
			"dentista el 2026-10-16 a las 11:00", true},
		{"every occurrence", "2026-10-16: dentist (2026-10-16)",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 17, 10, 0, time.UTC), time.UTC,
			"2026-10-17: dentist (2026-10-17)", true},
		{"written in the user's zone", "dentista el 2026-10-16 a las 10:00",
			at(2026, 10, 16, 10, 0, art), at(2026, 10, 9, 10, 0, art), art,
			"dentista el 2026-10-09 a las 10:00", true},
		{"written in UTC while the user's zone differs", "dentista el 2026-10-16 a las 10:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 10, 0, time.UTC), art,
			"dentista el 2026-10-09 a las 10:00", true},
		{"written in UTC, the time moved, the user's zone differs", "dentista el 2026-10-16 a las 10:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 16, 11, 0, time.UTC), art,
			"dentista el 2026-10-16 a las 11:00", true},
		{"a time is rewritten only where it follows the date", "room 2026-10-16 a las 10:00 and 10:00-11:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"room 2026-10-09 a las 11:00 and 10:00-11:00", true},
		{"a bare time with no date is never rewritten", "gym daily at 10:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"gym daily at 10:00", false},
		{"a bare time in the user's zone is never rewritten", "meeting 07:00 then dentist",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), art,
			"meeting 07:00 then dentist", false},
		{"a URL is never touched", "https://x.io/2026-10-16/notes?t=10:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"https://x.io/2026-10-16/notes?t=10:00", false},
		{"a word carrying a scheme is never touched", "slides https://x.io/a-2026-10-16-b",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"slides https://x.io/a-2026-10-16-b", false},
		{"a query value is never touched", "see ?d=2026-10-16&x=1",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"see ?d=2026-10-16&x=1", false},
		{"a range after the date keeps its times", "dentista 2026-10-16 10:00-11:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"dentista 2026-10-09 10:00-11:00", true},
		{"a timestamp's own time follows", "dentista 2026-10-16T10:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"dentista 2026-10-09T11:00", true},
		{"the user's zone is tried before UTC", "dentista el 2026-10-16",
			at(2026, 10, 16, 12, 0, time.UTC), at(2026, 10, 17, 2, 0, time.UTC), art,
			"dentista el 2026-10-16", false},
		{"a question mark ending the sentence is punctuation", "Is the dentist on 2026-10-16?",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"Is the dentist on 2026-10-09?", true},
		{"an opening and closing question keep the time", "¿El dentista es el 2026-10-16 a las 10:00?",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"¿El dentista es el 2026-10-09 a las 11:00?", true},
		{"a hashtag glued to the time is not a URL", "Dentist 2026-10-16 at 10:00#urgent",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"Dentist 2026-10-09 at 11:00#urgent", true},
		{"a space joins the date to its time", "dentista 2026-10-16 10:00 en punto",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"dentista 2026-10-09 11:00 en punto", true},
		{"at joins the date to its time", "Dentist on 2026-10-16 at 10:00",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"Dentist on 2026-10-09 at 11:00", true},
		{"a path before the date is untouched", "www.x.io/2026-10-16",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"www.x.io/2026-10-16", false},
		{"a path after the date is untouched", "see 2026-10-16/notes",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"see 2026-10-16/notes", false},
		{"a path after the time keeps the time", "dentist 2026-10-16 at 10:00/x",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"dentist 2026-10-09 at 10:00/x", true},
		{"a date inside a file name is untouched", "minutes-2026-10-16.pdf",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"minutes-2026-10-16.pdf", false},
		{"a date before an extension is untouched", "2026-10-16.pdf",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"2026-10-16.pdf", false},
		{"a date glued after an underscore is untouched", "notes_2026-10-16",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"notes_2026-10-16", false},
		{"a date glued after a letter is untouched", "v2026-10-16",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"v2026-10-16", false},
		{"a date run into another number is untouched", "2026-10-16-17",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"2026-10-16-17", false},
		{"a sentence-ending period is punctuation", "It is on 2026-10-16.",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"It is on 2026-10-09.", true},
		{"a trailing question mark is punctuation even in a URL-shaped word", "dentist(id=4):2026-10-16?",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"dentist(id=4):2026-10-09?", true},
		{"a bracketed date in a scheme-bearing word is untouched", "https://x.io/a(2026-10-16)",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"https://x.io/a(2026-10-16)", false},
		{"a fragment in a path is a URL mark", "x.io/p#2026-10-16",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 9, 11, 0, time.UTC), time.UTC,
			"x.io/p#2026-10-16", false},
		{"another form is not recognized", "Dentist appointment on the 14th",
			at(2026, 8, 14, 9, 0, time.UTC), at(2026, 8, 15, 9, 0, time.UTC), time.UTC,
			"Dentist appointment on the 14th", false},
		{"a longer number is not a time", "room 110:00x and 2026-10-160",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 17, 11, 0, time.UTC), time.UTC,
			"room 110:00x and 2026-10-160", false},
		{"no change", "dentista el 2026-10-16",
			at(2026, 10, 16, 10, 0, time.UTC), at(2026, 10, 16, 10, 0, time.UTC), time.UTC,
			"dentista el 2026-10-16", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := FollowDate(tc.text, tc.prev, tc.next, tc.zone)
			if got != tc.want || changed != tc.changed {
				t.Errorf("FollowDate(%q) = %q, %v; want %q, %v", tc.text, got, changed, tc.want, tc.changed)
			}
		})
	}
}

func TestCarryText(t *testing.T) {
	prev := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	next := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	u := unit.Unit{Content: "dentista el 2026-10-16", EventAt: &prev, DueAt: &prev}

	t.Run("an event_at edit carries the body", func(t *testing.T) {
		got := CarryText([]Edit{NewEventAtEdit(next)}, u, time.UTC)
		if len(got) != 2 || got[0].Field() != FieldEventAt || got[1].Field() != FieldContent {
			t.Fatalf("plan = %v, want [event_at, content]", got)
		}
		if c, _ := got[1].Content(); c != "dentista el 2026-10-09" {
			t.Errorf("content = %q", c)
		}
	})
	t.Run("a due_at edit carries the body", func(t *testing.T) {
		got := CarryText([]Edit{NewDueAtEdit(next)}, u, time.UTC)
		if len(got) != 2 || got[1].Field() != FieldContent {
			t.Fatalf("plan = %v, want [due_at, content]", got)
		}
	})
	t.Run("the previous value is the edited column's own", func(t *testing.T) {
		other := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		v := u
		v.DueAt = &other
		if got := CarryText([]Edit{NewDueAtEdit(next)}, v, time.UTC); len(got) != 1 {
			t.Errorf("plan = %v, want the due_at edit alone — the body names event_at's date, not due_at's", got)
		}
	})
	t.Run("an empty previous value carries nothing", func(t *testing.T) {
		v := u
		v.EventAt = nil
		if got := CarryText([]Edit{NewEventAtEdit(next)}, v, time.UTC); len(got) != 1 {
			t.Errorf("plan = %v, want the event_at edit alone", got)
		}
	})
	t.Run("a content edit carries nothing", func(t *testing.T) {
		if got := CarryText([]Edit{NewContentEdit("x")}, u, time.UTC); len(got) != 1 {
			t.Errorf("plan = %v, want the content edit alone", got)
		}
	})
}

// TestFollowDate_RewritesTheFormThePromptAsksFor ties FollowDate's layouts
// to the example classify.BuildPrompt shows the model: if either drifts,
// corrections stop finding the dates captures wrote.
func TestFollowDate_RewritesTheFormThePromptAsksFor(t *testing.T) {
	now := time.Date(2026, 10, 9, 14, 30, 0, 0, time.UTC)
	example := now.Format(textDateLayout) + " " + now.Format(textTimeLayout)
	if !strings.Contains(classify.BuildPrompt("x", nil, now, 0.5), example) {
		t.Errorf("the prompt does not show %q, the form FollowDate rewrites", example)
	}
}
