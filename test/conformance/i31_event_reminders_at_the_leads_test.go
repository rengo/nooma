// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI31_EventRemindersAtTheLeads is doc 02 §7's "Lead time" (ADR-0029): a
// captured event arms one trigger per lead still ahead — 24 hours and 2
// hours before a timed event, the day before at 09:00 local for a date-only
// one — and one at once only when every lead is behind and the event is
// not. Each trigger writes its own capture.armed.trigger row.
func TestI31_EventRemindersAtTheLeads(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, zone) }
	const timed = `{"type":"event","normalized_content":"dentista el 2026-10-16 a las 10:00","weight":0.7,"decay_rate":0.05,"event_at":"2026-10-16T10:00:00-03:00"}`
	const dateOnly = `{"type":"event","normalized_content":"renovar el pasaporte el 2026-10-16","weight":0.7,"decay_rate":0.05,"event_at":"2026-10-16"}`

	cases := []struct {
		name     string
		response string
		now      time.Time
		want     []time.Time
		leads    []int // payload.lead_minutes, one per trigger
	}{
		{"a timed event days ahead: 24 hours and 2 hours before", timed, at(13, 10),
			[]time.Time{at(15, 10), at(16, 8)}, []int{1440, 120}},
		{"a timed event 20 hours ahead: only the 2-hour lead is ahead", timed, at(15, 14),
			[]time.Time{at(16, 8)}, []int{120}},
		{"a timed event 1 hour ahead: one reminder at once", timed, at(16, 9),
			[]time.Time{at(16, 9)}, []int{0}},
		{"a date-only event: the day before at 09:00 local", dateOnly, at(13, 10),
			[]time.Time{at(15, 9)}, []int{15 * 60}},
		{"a date-only event after 09:00 the day before: at once", dateOnly, at(15, 12),
			[]time.Time{at(15, 12)}, []int{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			triggers, log, result := i31Capture(t, tc.now, tc.response)

			rows := triggers.All() // ordered by id, which says nothing about firing order
			sort.Slice(rows, func(i, j int) bool { return rows[i].FireAt.Before(*rows[j].FireAt) })
			if len(rows) != len(tc.want) {
				t.Fatalf("triggers = %d, want %d: %+v", len(rows), len(tc.want), rows)
			}
			for i, want := range tc.want {
				if rows[i].FireAt == nil || !rows[i].FireAt.Equal(want) {
					t.Errorf("trigger %d fires at %v, want %s", i, rows[i].FireAt, want)
				}
				if rows[i].Payload.LeadMinutes != tc.leads[i] {
					t.Errorf("trigger %d payload.lead_minutes = %d, want %d", i, rows[i].Payload.LeadMinutes, tc.leads[i])
				}
				if rows[i].UnitID == nil || *rows[i].UnitID != result.UnitID {
					t.Errorf("trigger %d hangs off %v, want the captured unit %q", i, rows[i].UnitID, result.UnitID)
				}
			}
			armedRows := 0
			all, err := log.Since(context.Background(), time.Time{}, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range all {
				if d.Action == ports.ActionCaptureArmedTrigger {
					armedRows++
				}
			}
			if armedRows != len(tc.want) {
				t.Errorf("capture.armed.trigger rows = %d, want one per trigger (%d)", armedRows, len(tc.want))
			}
			if result.Armed == nil || !result.Armed.FireAt.Equal(tc.want[0]) || len(result.Armed.Later) != len(tc.want)-1 {
				t.Errorf("Armed = %+v, want the earliest firing and the rest in Later", result.Armed)
			}
		})
	}
}

// TestI31_EventRemindersFollowTheUsersPreferences: the leads are the user's
// own, read from the vault at arm time (ADR-0029 point 5); a stored value
// that fails validation falls back to its own default.
func TestI31_EventRemindersFollowTheUsersPreferences(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	const timed = `{"type":"event","normalized_content":"dentista","weight":0.7,"decay_rate":0.05,"event_at":"2026-10-16T10:00:00-03:00"}`
	const dateOnly = `{"type":"event","normalized_content":"pasaporte","weight":0.7,"decay_rate":0.05,"event_at":"2026-10-16"}`
	str := func(s string) *string { return &s }
	fires := func(tr *memrepo.Triggers) []time.Time {
		var out []time.Time
		for _, r := range tr.All() {
			out = append(out, *r.FireAt)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
		return out
	}

	cases := []struct {
		name, response string
		leads, at      *string
		want           []time.Time
	}{
		{"three hours before", timed, str(`[180]`), nil, []time.Time{at(16, 7, 0)}},
		{"a week and an hour before", timed, str(`[60,10080]`), nil, []time.Time{at(9, 10, 0), at(16, 9, 0)}},
		{"a date-only event at 20:30 the day before", dateOnly, nil, str("20:30"), []time.Time{at(15, 20, 30)}},
		{"a corrupt stored lead list falls back to the defaults", timed, str(`[0]`), str("20:30"), []time.Time{at(15, 10, 0), at(16, 8, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := memrepo.NewConfig()
			cfg.SeedConfig(t, ports.VaultConfig{EventReminderLeads: tc.leads, DateOnlyReminderAt: tc.at})
			triggers, _, _ := i31CaptureWith(t, at(1, 10, 0), tc.response, cfg)
			if got := fires(triggers); !slices.EqualFunc(got, tc.want, time.Time.Equal) {
				t.Errorf("fires at %v, want %v", got, tc.want)
			}
		})
	}
}

// i31Capture runs one capture of response at now over fresh fakes, with no
// stored preferences.
func i31Capture(t *testing.T, now time.Time, response string) (*memrepo.Triggers, ports.DecisionLog, brain.CaptureResult) {
	t.Helper()
	return i31CaptureWith(t, now, response, nil)
}

// i31CaptureWith runs one capture reading its reminder preferences from cfg
// when it is not nil.
func i31CaptureWith(t *testing.T, now time.Time, response string, cfg ports.ConfigRepo) (*memrepo.Triggers, ports.DecisionLog, brain.CaptureResult) {
	t.Helper()
	ctx := context.Background()
	embeddings := memrepo.NewEmbeddings()
	triggers := memrepo.NewTriggers()
	log := memrepo.NewDecisionLog()
	llm := fakeprovider.New(t, i28Case(t, "i31", "replayed", response), "i31")
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(), embeddings,
		memrepo.NewLexical(), memrepo.NewRelations(), log, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), memrepo.NewSignals(), triggers, memrepo.NewTimers(), 0.5, nil)
	if cfg != nil {
		svc = svc.WithReminderPrefs(cfg)
	}
	result, err := svc.Capture(ctx, brain.CaptureInput{Text: "replayed", Channel: "chat"})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return triggers, log, result
}
