// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"strings"
	"testing"
	"time"
)

// i36Downtime walks the check passes every five minutes from the capture to
// until, skipping the server's downtime [down, up), and returns each reminder
// push by the local minute it went out.
func (v *i36Vault) i36Downtime(t *testing.T, down, up, until time.Time) []string {
	t.Helper()
	var sent []string
	for at := i36Now.Truncate(5 * time.Minute).Add(5 * time.Minute); at.Before(until); at = at.Add(5 * time.Minute) {
		if !at.Before(down) && at.Before(up) {
			continue
		}
		for _, s := range reminders(v.pass(t, at)) {
			sent = append(sent, at.Format("Mon 01-02 15:04")+" "+s)
		}
	}
	return sent
}

// TestI36_ARestartKeepsWhatTheUserAskedFor: the server is down from Monday
// 22:00 to Thursday 21:30, so a lead of each unit goes stale and the re-plan
// step (ADR-0032 point 3) owns the unit. It plans the reminders a capture of
// the same request would still arm (ADR-0031 point 2, doc 02 §7): the asked
// event's healthy rows are carried, an asked day keeps its own day, and an
// asked day alone whose ring the downtime ate gets its catch-up while that
// day lasts.
func TestI36_ARestartKeepsWhatTheUserAskedFor(t *testing.T) {
	down, up, until := i36At(19, 22, 0), i36At(22, 21, 30), i36At(24, 12, 0)
	for _, tc := range []struct {
		name, id, text string
		want           []string
	}{
		{"the asked event's healthy rows are carried", "classify-request-named-time-friday", "recordame el dentista el viernes a las 10",
			[]string{"Fri 10-23 08:00 Recordatorio: Cita con el dentista — vie 23 oct, 10:00",
				"Fri 10-23 09:45 Recordatorio: Cita con el dentista — vie 23 oct, 10:00"}},
		{"an asked date-only event keeps its day", "classify-request-acordate-de-avisarme", "acordate de avisarme que vence el seguro el viernes",
			[]string{"Fri 10-23 09:00 Recordatorio: Vencimiento del seguro — vie 23 oct"}},
		{"an asked day alone is caught up while its day lasts", "classify-request-day-only-thursday", "recordame pagar el gas el jueves",
			[]string{"Thu 10-22 21:35 Recordatorio: Pagar el gas — jue 22 oct"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newI36Vault()
			v.capture(t, i36Now, i36Recordings(t), tc.id, tc.text)
			if got := v.i36Downtime(t, down, up, until); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("pushes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}
