package classify

import (
	"strings"
	"testing"
)

func TestResolveTitle_TrimsAndRejectsControl(t *testing.T) {
	if got := ResolveTitle("  Reunión  "); got == nil || *got != "Reunión" {
		t.Errorf("ResolveTitle(padded) = %v, want %q", got, "Reunión")
	}
	for _, in := range []string{"a\nb", "a\x07b", "a\tb", "   ", ""} {
		if got := ResolveTitle(in); got != nil {
			t.Errorf("ResolveTitle(%q) = %q, want nil", in, *got)
		}
	}
}

func TestResolveTitle(t *testing.T) {
	if got := ResolveTitle("Reunión con Juan"); got == nil || *got != "Reunión con Juan" {
		t.Errorf("ResolveTitle(plain) = %v, want it kept", got)
	}
	atLimit := strings.Repeat("ñ", MaxTitleRunes)
	if got := ResolveTitle(atLimit); got == nil || *got != atLimit {
		t.Errorf("a title of exactly %d runes was dropped, want it kept", MaxTitleRunes)
	}
	if got := ResolveTitle(atLimit + "ñ"); got != nil {
		t.Errorf("a title of %d runes was kept, want nil", MaxTitleRunes+1)
	}
	for _, in := range []string{"Reunión 2026-10-14", "Reunión a las 22:30", "call at 9:05", "2026-10-14"} {
		if got := ResolveTitle(in); got != nil {
			t.Errorf("ResolveTitle(%q) = %q, want nil: it carries an ISO date or an HH:MM", in, *got)
		}
	}
}

func TestDecode_Title(t *testing.T) {
	c, _ := Decode(reminderPayload(`,"title":"  Reunión con Juan "`), testNow)
	if len(c.Degradations) != 0 || c.Title == nil || *c.Title != "Reunión con Juan" {
		t.Errorf("got %v degraded %v, want the trimmed title clean", c.Title, c.Degradations)
	}
	for _, tc := range []struct {
		raw    string
		reason Reason
	}{
		{`"Reunión 2026-10-14"`, ReasonBadFormat},
		{`"   "`, ReasonBadFormat},
		{`7`, ReasonWrongType},
		{`null`, ReasonWrongType},
	} {
		c, _ := Decode(reminderPayload(`,"title":`+tc.raw), testNow)
		assertOnlyDegraded(t, c, "title", tc.reason)
		if c.Title != nil {
			t.Errorf("title %s kept as %q, want nil", tc.raw, *c.Title)
		}
	}
}
