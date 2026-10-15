package conformance

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// telegramUnits is a message's length the way Telegram counts it.
func telegramUnits(s string) int { return len(utf16.Encode([]rune(s))) }

// longDigest seeds n recurring reminders of n units, each saying text(i), plus a
// newest unit that speaks language ("" none), and returns the vault.
func longDigest(t *testing.T, n int, language string, text func(i int) string) *i37Vault {
	v := newI37Vault(t)
	v.speaks("newest", language, i37Now.Add(-time.Hour), nil)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("u%02d", i)
		v.speaks(id, language, i37Now.Add(-48*time.Hour), i37At(1, 12))
		v.fired("r"+id, id, text(i), i37Day(13, 16, 0), yearlyOn21)
	}
	return v
}

// addLines adds the index every line of msg starts with to said, failing a
// reminder said twice.
func addLines(t *testing.T, said map[string]bool, msg string) {
	t.Helper()
	for _, l := range strings.Split(msg, "\n\u2022 ")[1:] {
		key := l[:2]
		if said[key] {
			t.Errorf("reminder %s was said twice", key)
		}
		said[key] = true
	}
}

func lines(msg string) int { return strings.Count(msg, "\n\u2022 ") }

// TestDigestOverTheMessageBoundDropsTheTail: a digest of dozens of reminders can
// exceed Telegram's 4096 units, which refuses it and fails every pass. The lines
// past the bound are dropped, say how many in the digest's language, stay
// undelivered, and tomorrow's digest carries them; the report counts only what
// was sent. The bound is Telegram's, UTF-16 units, so astral text is measured
// by what it costs there, not by its runes.
func TestDigestOverTheMessageBoundDropsTheTail(t *testing.T) {
	plain := func(i int) string { return fmt.Sprintf("%02d%s", i, strings.Repeat("x", 430)) }
	astral := func(i int) string { return fmt.Sprintf("%02d%s", i, strings.Repeat("\U0001F600", 300)) }
	for _, c := range []struct {
		name, language, more string
		text                 func(int) string
	}{
		{"english", "en", "and %d more", plain},
		{"spanish", "es", "y %d más", plain},
		{"astral text counts as two units a rune", "", "and %d more", astral},
	} {
		t.Run(c.name, func(t *testing.T) {
			const n = 14
			v := longDigest(t, n, c.language, c.text)

			report := v.check(false)

			if len(v.ch.texts) != 1 {
				t.Fatalf("sent %d messages, want 1", len(v.ch.texts))
			}
			msg := v.ch.texts[0]
			shown := lines(msg)
			if got := telegramUnits(msg); got > 4096 {
				t.Errorf("the digest is %d units, over Telegram's 4096", got)
			}
			if shown < 1 || shown >= n {
				t.Fatalf("%d lines shown of %d, want some dropped", shown, n)
			}
			if want := fmt.Sprintf(c.more, n-shown); !strings.HasSuffix(msg, want+".") {
				t.Errorf("the digest does not end saying %q:\n...%s", want, msg[max(0, len(msg)-80):])
			}
			if report.DigestCarried != shown {
				t.Errorf("DigestCarried = %d, want the %d lines sent", report.DigestCarried, shown)
			}
			left, err := v.triggers.Undelivered(context.Background())
			if err != nil || len(left) != n-shown {
				t.Fatalf("%d reminders left undelivered, %v; want the %d dropped", len(left), err, n-shown)
			}

			// Tomorrow's digests carry what was dropped, each within the bound,
			// until every reminder has been said once and only once.
			said := map[string]bool{}
			addLines(t, said, msg)
			for day := 1; len(said) < n && day <= 4; day++ {
				v.checkAt(i37Now.Add(time.Duration(day)*24*time.Hour), false)
				next := v.ch.texts[len(v.ch.texts)-1]
				if got := telegramUnits(next); got > 4096 || lines(next) < 1 {
					t.Fatalf("day %d digest is %d units with %d lines", day, got, lines(next))
				}
				addLines(t, said, next)
			}
			if len(said) != n {
				t.Errorf("%d reminders said across the digests, want each of the %d once", len(said), n)
			}
			if left, _ := v.triggers.Undelivered(context.Background()); len(left) != 0 {
				t.Errorf("%d reminders still undelivered", len(left))
			}
		})
	}
}

// TestDigestUnderTheBoundIsUnchanged: nothing is dropped and nothing is said of
// it when the digest fits.
func TestDigestUnderTheBoundIsUnchanged(t *testing.T) {
	v := longDigest(t, 3, "", func(i int) string { return fmt.Sprintf("thing %d", i) })

	report := v.check(false)

	if report.DigestCarried != 3 || len(v.ch.texts) != 1 || lines(v.ch.texts[0]) != 3 || strings.Contains(v.ch.texts[0], "more") {
		t.Errorf("carried %d, sent %q; want all three and no \"more\"", report.DigestCarried, v.ch.texts)
	}
}

// TestDryRunCountsOnlyWhatTheBoundKeeps: a dry run reports the lines the real
// pass would send, not every reminder that waits.
func TestDryRunCountsOnlyWhatTheBoundKeeps(t *testing.T) {
	text := func(i int) string { return fmt.Sprintf("%02d%s", i, strings.Repeat("x", 430)) }
	dry := longDigest(t, 14, "", text).check(true)
	real := longDigest(t, 14, "", text).check(false)

	if dry.DigestCarried != real.DigestCarried || real.DigestCarried >= 14 {
		t.Errorf("dry run carried %d, real %d; want equal and under 14", dry.DigestCarried, real.DigestCarried)
	}
}

// TestTodayRendersFromTheUnit: Today says what a push and the digest say, the
// unit's text now and its instant, not the words frozen when the reminder was
// armed; a unit that cannot be read falls back to the frozen text. It speaks the
// newest unit's language, as the digest it previews does, and writes nothing.
func TestTodayRendersFromTheUnit(t *testing.T) {
	v := newI37Vault(t)
	v.silent = true
	event := i37Day(14, 20, 0)
	v.speaks("u", "es", i37Now.Add(-time.Hour), &event)
	v.row("soon", "u", "Dentist (old text)", i37Day(14, 18, 0), modern(event, 120))
	v.say("u", "Dentista")
	v.speaks("bd", "es", i37Now.Add(-2*time.Hour), i37At(1, 12))
	v.fired("b", "bd", "Frozen birthday", i37Day(13, 16, 0), yearlyOn21)
	v.say("bd", "Cumpleaños de Leo")
	v.fired("lost", "gone", "Frozen words", i37Day(13, 16, 0), yearlyOn21)

	today, err := newI37Today(v).Today(context.Background())
	if err != nil {
		t.Fatalf("Today: %v", err)
	}

	if len(today.Upcoming) != 1 || today.Upcoming[0].Text != "Dentista — mié 14 oct, 20:00" {
		t.Errorf("Upcoming = %+v, want the unit's text and instant in Spanish", today.Upcoming)
	}
	got := map[string]string{}
	for _, l := range today.Digest.Items {
		got[l.TriggerID] = l.Text
	}
	if got["b"] != "Cumpleaños de Leo — mié 21 oct" || got["lost"] != "Frozen words" {
		t.Errorf("Digest items = %v, want the unit's current text and day, and the frozen text of an unreadable unit", got)
	}
}

// TestTodayFallsBackToEnglish: with no language known Today speaks English.
func TestTodayFallsBackToEnglish(t *testing.T) {
	v := newI37Vault(t)
	v.silent = true
	v.speaks("bd", "", i37Now.Add(-2*time.Hour), i37At(1, 12))
	v.fired("b", "bd", "x", i37Day(13, 16, 0), yearlyOn21)
	v.say("bd", "Leo's birthday")

	today, err := newI37Today(v).Today(context.Background())
	if err != nil || len(today.Digest.Items) != 1 || today.Digest.Items[0].Text != "Leo's birthday — Wed 21 Oct" {
		t.Errorf("Today = %+v, %v; want the English line", today.Digest.Items, err)
	}
}
