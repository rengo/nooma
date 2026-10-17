// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// speaks stores a unit whose capture was written in language ("" for none),
// created at the given time, with the event at (nil: none). It is what capture
// writes (ADR-0032 point 5).
func (v *i37Vault) speaks(id, language string, created time.Time, at *time.Time) {
	v.t.Helper()
	if err := v.units.Create(context.Background(), unit.Unit{
		ID: id, Type: unit.TypeEvent, Status: unit.StatusPool, Content: id, Weight: 1, Language: language,
		EventAt: at, LastTouchedAt: created, CreatedAt: created, UpdatedAt: created,
	}); err != nil {
		v.t.Fatalf("seed unit %s: %v", id, err)
	}
}

func yearlyOn21(tr *ports.Trigger) {
	yearly := prospection.RuleYearly
	tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
}

// TestI37_SpanishUnitPushAndDigestInSpanish: a person who writes in Spanish gets
// the pushed reminder, the digest's header and the digest's lines in Spanish: the
// client's first digest was English to a user whose every reply came back in
// Spanish (ADR-0032 point 5).
func TestI37_SpanishUnitPushAndDigestInSpanish(t *testing.T) {
	push := newI37Vault(t)
	event := i37Day(15, 11, 0)
	push.speaks("dentist", "es", i37Now.Add(-time.Hour), &event)
	push.row("r", "dentist", "Dentista", i37Now.Add(-time.Minute), modern(event, 1500))
	push.check(false)
	if want := "Recordatorio: Dentista — jue 15 oct, 11:00"; len(push.ch.texts) != 1 || push.ch.texts[0] != want {
		t.Errorf("push sent %q, want exactly [%q]", push.ch.texts, want)
	}

	digest := newI37Vault(t)
	digest.speaks("bd", "es", i37Now.Add(-time.Hour), i37At(1, 12))
	digest.fired("b", "bd", "Cumpleaños de Leo", i37Day(13, 16, 0), yearlyOn21)
	digest.check(false)
	if want := "Hoy hay 1 cosa:\n• Cumpleaños de Leo — mié 21 oct"; len(digest.ch.texts) != 1 || digest.ch.texts[0] != want {
		t.Errorf("digest sent %q, want exactly [%q]", digest.ch.texts, want)
	}
}

// TestPushFallsBackToLatestThenEnglish: a push speaks its unit's language; a unit
// that knows none speaks the newest unit's; none known at all, English. The
// digest speaks the newest capture's language whatever the carried unit's own.
func TestPushFallsBackToLatestThenEnglish(t *testing.T) {
	event := i37Day(15, 11, 0)
	cases := []struct {
		name     string
		unit     string // the reminded unit's language
		other    string // a newer unit's language, "" none
		want     string
		digestOf string
	}{
		{"its own language wins over the newest", "en", "es", "Reminder: Dentist — Thu 15 Oct, 11:00", "Hoy hay 1 cosa:\n• Dentist — mié 21 oct"},
		{"no language of its own: the newest unit's", "", "es", "Recordatorio: Dentist — jue 15 oct, 11:00", "Hoy hay 1 cosa:\n• Dentist — mié 21 oct"},
		{"nobody knows: English", "", "", "Reminder: Dentist — Thu 15 Oct, 11:00", "Here is 1 thing for today:\n• Dentist — Wed 21 Oct"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			push := newI37Vault(t)
			push.speaks("dentist", c.unit, i37Now.Add(-48*time.Hour), &event)
			push.speaks("newer", c.other, i37Now.Add(-time.Hour), nil)
			push.row("r", "dentist", "Dentist", i37Now.Add(-time.Minute), modern(event, 1500))
			push.check(false)
			if len(push.ch.texts) != 1 || push.ch.texts[0] != c.want {
				t.Errorf("push sent %q, want exactly [%q]", push.ch.texts, c.want)
			}

			digest := newI37Vault(t)
			digest.speaks("dentist", c.unit, i37Now.Add(-48*time.Hour), i37At(1, 12))
			digest.speaks("newer", c.other, i37Now.Add(-time.Hour), nil)
			digest.fired("b", "dentist", "Dentist", i37Day(13, 16, 0), yearlyOn21)
			digest.check(false)
			if len(digest.ch.texts) != 1 || digest.ch.texts[0] != c.digestOf {
				t.Errorf("digest sent %q, want exactly [%q]", digest.ch.texts, c.digestOf)
			}
		})
	}
}

// TestTheDigestSpeaksTheLanguageUsuallyWritten: three Spanish captures and a
// newer English one; what Nooma says first, the digest and a push whose unit
// knows no language, is in Spanish, the language the person usually writes in
// (ADR-0035), not the newest capture's.
func TestTheDigestSpeaksTheLanguageUsuallyWritten(t *testing.T) {
	event := i37Day(15, 11, 0)
	seed := func(v *i37Vault) {
		for i, id := range []string{"es-1", "es-2", "es-3"} {
			v.speaks(id, "es", i37Now.Add(-time.Duration(10-i)*time.Hour), nil)
		}
		v.speaks("newest-en", "en", i37Now.Add(-time.Hour), nil)
	}

	push := newI37Vault(t)
	seed(push)
	push.speaks("dentist", "", i37Now.Add(-48*time.Hour), &event)
	push.row("r", "dentist", "Dentist", i37Now.Add(-time.Minute), modern(event, 1500))
	push.check(false)
	if want := "Recordatorio: Dentist — jue 15 oct, 11:00"; len(push.ch.texts) != 1 || push.ch.texts[0] != want {
		t.Errorf("push sent %q, want exactly [%q]", push.ch.texts, want)
	}

	digest := newI37Vault(t)
	seed(digest)
	digest.speaks("dentist", "", i37Now.Add(-48*time.Hour), i37At(1, 12))
	digest.fired("b", "dentist", "Dentist", i37Day(13, 16, 0), yearlyOn21)
	digest.check(false)
	if want := "Hoy hay 1 cosa:\n• Dentist — mié 21 oct"; len(digest.ch.texts) != 1 || digest.ch.texts[0] != want {
		t.Errorf("digest sent %q, want exactly [%q]", digest.ch.texts, want)
	}
}

// TestFallbackSentencesSpeakTheLatestLanguage: the sentences that stand in for
// a unit (one that cannot be read) and the lateness note speak the same
// language as the rest of the message.
func TestFallbackSentencesSpeakTheLatestLanguage(t *testing.T) {
	event := i37Day(15, 11, 0)

	unreadable := newI37Vault(t)
	unreadable.speaks("newer", "es", i37Now.Add(-time.Hour), nil)
	unreadable.row("r", "gone", "", i37Now.Add(-time.Minute), modern(event, 1500))
	unreadable.check(false)
	if want := "Me pediste que te recordara algo."; len(unreadable.ch.texts) != 1 || unreadable.ch.texts[0] != want {
		t.Errorf("a reminder with nothing to say sent %q, want [%q]", unreadable.ch.texts, want)
	}

	late := newI37Vault(t)
	late.speaks("dentist", "es", i37Now.Add(-time.Hour), &event)
	late.fired("r", "dentist", "Dentista", i37Now.Add(-20*time.Minute), modern(event, 1500))
	late.check(false)
	want := "Recordatorio: Dentista — jue 15 oct, 11:00 (Esto llega más tarde de lo previsto.)"
	if len(late.ch.texts) != 1 || late.ch.texts[0] != want {
		t.Errorf("a late reminder sent %q, want [%q]", late.ch.texts, want)
	}
}
