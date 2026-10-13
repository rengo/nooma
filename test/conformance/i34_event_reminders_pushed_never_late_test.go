// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// TestI34_EventRemindersAreArmedAroundQuietHours is doc 02 §7's delivery
// rule for event reminders (ADR-0030), at the defaults: a reminder whose
// lead falls in quiet hours [00:00, 07:00) is armed at quiet end when that
// is before the event, and otherwise the evening before at 22:00; one with
// no allowed instant before its event is not armed, and the capture says
// why. Each case is a row of ADR-0030's worked examples.
func TestI34_EventRemindersAreArmedAroundQuietHours(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	event := func(eventAt string) string {
		return `{"type":"event","normalized_content":"vuelo","weight":0.7,"decay_rate":0.05,"event_at":"` + eventAt + `"}`
	}

	cases := []struct {
		name     string
		eventAt  string
		now      time.Time
		want     []time.Time
		leads    []int
		refusedQ bool // a capture.arm.refused row with why quiet_hours
	}{
		{"flight 05:00: the 24h reminder at 07:00 the day before, the 2h one at 22:00 the evening before",
			"2026-10-16T05:00:00-03:00", at(13, 10, 0), []time.Time{at(15, 7, 0), at(15, 22, 0)}, []int{22 * 60, 7 * 60}, false},
		{"flight 08:00: the 2h reminder (06:00) at quiet end, 07:00, still before the event",
			"2026-10-16T08:00:00-03:00", at(13, 10, 0), []time.Time{at(15, 8, 0), at(16, 7, 0)}, []int{24 * 60, 60}, false},
		{"flight 07:00, exactly at quiet end: the 2h reminder the evening before",
			"2026-10-16T07:00:00-03:00", at(13, 10, 0), []time.Time{at(15, 7, 0), at(15, 22, 0)}, []int{24 * 60, 9 * 60}, false},
		{"dentist 10:00: daytime reminders are not moved",
			"2026-10-16T10:00:00-03:00", at(13, 10, 0), []time.Time{at(15, 10, 0), at(16, 8, 0)}, []int{24 * 60, 120}, false},
		{"flight 05:00 captured at 23:00, past the evening time: at once, not after the event",
			"2026-10-16T05:00:00-03:00", at(15, 23, 0), []time.Time{at(15, 23, 0)}, []int{0}, false},
		{"flight 07:30 captured at 06:00: the at-once reminder waits for quiet end",
			"2026-10-16T07:30:00-03:00", at(16, 6, 0), []time.Time{at(16, 7, 0)}, []int{30}, false},
		{"flight 05:00 captured at 02:00: no allowed instant before it, nothing armed, the capture says why",
			"2026-10-16T05:00:00-03:00", at(16, 2, 0), nil, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			triggers, log, result := i33Capture(t, tc.now, event(tc.eventAt))
			rows := triggers.All()
			sort.Slice(rows, func(i, j int) bool { return rows[i].FireAt.Before(*rows[j].FireAt) })
			if len(rows) != len(tc.want) {
				t.Fatalf("triggers = %d, want %d: %+v", len(rows), len(tc.want), rows)
			}
			for i, want := range tc.want {
				if !rows[i].FireAt.Equal(want) || rows[i].Payload.LeadMinutes != tc.leads[i] {
					t.Errorf("trigger %d fires at %v with lead %d, want %v with lead %d",
						i, rows[i].FireAt.In(zone), rows[i].Payload.LeadMinutes, want, tc.leads[i])
				}
			}
			refused := 0
			all, err := log.Since(context.Background(), time.Time{}, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range all {
				if d.Action != ports.ActionCaptureArmRefused {
					continue
				}
				var c struct {
					Why    string `json:"why"`
					UnitID string `json:"unit_id"`
				}
				if err := json.Unmarshal(d.Context, &c); err != nil {
					t.Fatal(err)
				}
				if c.Why == string(prospection.RefusalQuietHours) && c.UnitID == result.UnitID && result.UnitID != "" {
					refused++
				}
			}
			if want := map[bool]int{true: 1}[tc.refusedQ]; refused != want {
				t.Errorf("capture.arm.refused rows for quiet_hours on the unit = %d, want %d", refused, want)
			}
			if tc.refusedQ && (result.Outcome != brain.OutcomeStored || result.UnitID == "") {
				t.Errorf("result = %+v, want the event stored though nothing was armed", result)
			}
		})
	}
}
