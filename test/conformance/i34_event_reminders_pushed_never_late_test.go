// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
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

// TestI34_EventRemindersArePushedAndNeverLate is the delivery half
// (ADR-0030 points 1 and 4): a due event reminder is pushed whatever its
// interrupt level, a degraded one included; a recurring reminder keeps its
// interrupt routing; and a reminder whose event has already started is
// expired with a check.trigger.expired row rather than sent.
func TestI34_EventRemindersArePushedAndNeverLate(t *testing.T) {
	now := time.Date(2026, 10, 16, 8, 30, 0, 0, time.UTC)
	low := 0.1
	yearly := prospection.RuleYearly
	unitID := "u1"

	cases := []struct {
		name          string
		trigger       ports.Trigger
		wantDelivered int
		wantExpired   int
	}{
		{"a low-interrupt event reminder is pushed", ports.Trigger{InterruptLevel: &low,
			Payload: ports.TriggerPayload{ActionText: "dentist", LeadMinutes: 120}, FireAt: ptrTime(now.Add(-time.Minute))}, 1, 0},
		{"a degraded event reminder is pushed", ports.Trigger{
			Payload: ports.TriggerPayload{ActionText: "dentist", LeadMinutes: 120}, FireAt: ptrTime(now.Add(-time.Minute))}, 1, 0},
		{"a pre-ADR-0029 one-shot reminder is pushed", ports.Trigger{InterruptLevel: &low,
			Payload: ports.TriggerPayload{ActionText: "dentist", LeadDays: 7}, FireAt: ptrTime(now.Add(-time.Minute))}, 1, 0},
		{"a low-interrupt recurring reminder still waits for the digest", ports.Trigger{InterruptLevel: &low,
			Payload: ports.TriggerPayload{ActionText: "birthday", LeadDays: 7}, FireAt: ptrTime(now.Add(-time.Minute)),
			RecurrenceRule: &yearly, RecurrenceAnchor: &prospection.Anchor{Month: time.October, Day: 23}}, 0, 0},
		{"a reminder whose event started 30 minutes ago is expired, not sent", ports.Trigger{InterruptLevel: &low,
			Payload: ports.TriggerPayload{ActionText: "flight", LeadMinutes: 60}, FireAt: ptrTime(now.Add(-90 * time.Minute))}, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			triggers := memrepo.NewTriggers()
			log := memrepo.NewDecisionLog()
			ch := &countingChannel{}
			tr := tc.trigger
			tr.ID, tr.UnitID, tr.Kind, tr.CreatedAt = "trg", &unitID, ports.TriggerKindTimeBased, now.Add(-48*time.Hour)
			if err := triggers.Create(ctx, tr); err != nil {
				t.Fatal(err)
			}
			report, err := brain.NewCheckService(fixedClock{now: now}, triggers, memrepo.NewTimers(), &counterIDs{}, log, ch,
				nil, nil, nil, "12449194", nil, nil).Check(ctx, brain.CheckRequest{})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if report.TriggersDelivered != tc.wantDelivered || ch.sent != tc.wantDelivered {
				t.Errorf("delivered = %d (sent %d), want %d", report.TriggersDelivered, ch.sent, tc.wantDelivered)
			}
			if report.TriggersExpired != tc.wantExpired {
				t.Errorf("expired = %d, want %d", report.TriggersExpired, tc.wantExpired)
			}
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// TestI34_EventRemindersFollowTheStoredNight is ADR-0030 point 5 at arm
// time: quiet end and the evening time are the user's preferences, read
// from the vault's config row, and a stored value that fails validation
// falls back to its own default.
func TestI34_EventRemindersFollowTheStoredNight(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, zone) }
	str := func(s string) *string { return &s }
	flight := `{"type":"event","normalized_content":"vuelo","weight":0.7,"decay_rate":0.05,"event_at":"2026-10-16T08:00:00-03:00"}`

	cases := []struct {
		name              string
		quietEnd, evening *string
		want              []time.Time
	}{
		{"quiet until 09:00, evening at 21:00: the 24h reminder at 09:00, the 2h one at 21:00 the evening before",
			str("09:00"), str("21:00"), []time.Time{at(15, 9), at(15, 21)}},
		{"a quiet end past noon and an evening before quiet end are invalid: the defaults",
			str("13:00"), str("06:00"), []time.Time{at(15, 8), at(16, 7)}},
		{"an evening before the stored quiet end is invalid on its own",
			str("09:00"), str("08:30"), []time.Time{at(15, 9), at(15, 22)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := memrepo.NewConfig()
			cfg.SeedConfig(t, ports.VaultConfig{QuietHoursEnd: tc.quietEnd, EveningReminderAt: tc.evening})
			triggers, _, _ := i33CaptureWith(t, at(13, 10), flight, cfg)
			rows := triggers.All()
			sort.Slice(rows, func(i, j int) bool { return rows[i].FireAt.Before(*rows[j].FireAt) })
			got := make([]time.Time, len(rows))
			for i, r := range rows {
				got[i] = *r.FireAt
			}
			if !slices.EqualFunc(got, tc.want, time.Time.Equal) {
				t.Errorf("fires at %v, want %v", got, tc.want)
			}
		})
	}
}

// TestI34_TheScanWaitsForTheStoredQuietEnd is ADR-0030 point 5 at delivery:
// the due scan reads the user's quiet end, so a trigger due before it is
// deferred to it rather than sent at the default 07:00, and the daily
// digest is not due before it either.
func TestI34_TheScanWaitsForTheStoredQuietEnd(t *testing.T) {
	ctx := context.Background()
	day := func(hour, minute int) time.Time { return time.Date(2026, 10, 16, hour, minute, 0, 0, time.UTC) }
	str := func(s string) *string { return &s }
	cfg := memrepo.NewConfig()
	cfg.SeedConfig(t, ports.VaultConfig{QuietHoursEnd: str("09:00")})

	triggers := memrepo.NewTriggers()
	unitID := "u1"
	low := 0.1
	yearly := prospection.RuleYearly
	for _, tr := range []ports.Trigger{
		{ID: "event", UnitID: &unitID, Payload: ports.TriggerPayload{ActionText: "dentist", LeadMinutes: 270}, FireAt: ptrTime(day(7, 30))},
		{ID: "birthday", InterruptLevel: &low, Payload: ports.TriggerPayload{ActionText: "birthday", LeadDays: prospection.EventLeadDays},
			FireAt: ptrTime(day(-2, 30)), RecurrenceRule: &yearly, RecurrenceAnchor: &prospection.Anchor{Month: time.October, Day: 23}},
	} {
		tr.Kind, tr.CreatedAt = ports.TriggerKindTimeBased, day(0, 0).Add(-48*time.Hour)
		if err := triggers.Create(ctx, tr); err != nil {
			t.Fatal(err)
		}
	}
	// Yesterday's digest went out at 10:00, so the birthday reminder that
	// fires at 22:30 waits for today's: the digest owed this morning has
	// an item, and sending it before the stored quiet end is observable.
	log := memrepo.NewDecisionLog()
	if err := log.Record(ctx, ports.Decision{ID: "d0", Action: ports.ActionCheckDigestSent, Rationale: "yesterday's digest", OccurredAt: day(-14, 0)}); err != nil {
		t.Fatal(err)
	}
	ch := &countingChannel{}
	ids := &counterIDs{}
	check := func(now time.Time) brain.CheckReport {
		t.Helper()
		report, err := brain.NewCheckService(fixedClock{now: now}, triggers, memrepo.NewTimers(), ids, log, ch,
			memrepo.NewUnits(), memrepo.NewState(), nil, "12449194", nil, nil).WithQuietHours(cfg).Check(ctx, brain.CheckRequest{})
		if err != nil {
			t.Fatalf("Check at %v: %v", now, err)
		}
		return report
	}

	if r := check(day(-1, 0)); r.TriggersFired != 1 || r.DigestCarried != 0 || ch.sent != 0 {
		t.Fatalf("at 23:00 the evening before: %+v (sent %d), want the birthday fired and held for the digest", r, ch.sent)
	}
	if r := check(day(8, 30)); r.TriggersDelivered != 0 || r.DigestCarried != 0 || r.TriggersExpired != 0 || ch.sent != 0 {
		t.Errorf("at 08:30, before the stored quiet end: %+v (sent %d), want nothing sent and nothing expired", r, ch.sent)
	}
	if r := check(day(9, 0)); r.TriggersDelivered != 1 || r.DigestCarried == 0 {
		t.Errorf("at 09:00, the stored quiet end: %+v, want the event reminder pushed and the digest sent", r)
	}
}
