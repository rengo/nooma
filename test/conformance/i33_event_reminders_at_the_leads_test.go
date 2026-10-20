// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI33_EventRemindersAtTheLeads is doc 02 §7's "Lead time" (ADR-0029): a
// captured event arms one trigger per lead still ahead — 24 hours and 2
// hours before a timed event, the day before at 09:00 local for a date-only
// one — and none at once when every lead is behind (ADR-0047). Each trigger
// writes its own capture.armed.trigger row.
func TestI33_EventRemindersAtTheLeads(t *testing.T) {
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
		// Not at once about what was just said: no reminder (ADR-0047).
		{"a timed event 1 hour ahead: no reminder", timed, at(16, 9), nil, nil},
		{"a date-only event: the day before at 09:00 local", dateOnly, at(13, 10),
			[]time.Time{at(15, 9)}, []int{15 * 60}},
		// Not at once about what was just said: on its own day (ADR-0039).
		{"a date-only event after 09:00 the day before: 09:00 on its day", dateOnly, at(15, 12),
			[]time.Time{at(16, 9)}, []int{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			triggers, log, result := i33Capture(t, tc.now, tc.response)

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
			if len(tc.want) == 0 {
				if result.Armed != nil {
					t.Errorf("Armed = %+v, want nothing armed", result.Armed)
				}
				return
			}
			if result.Armed == nil || !result.Armed.FireAt.Equal(tc.want[0]) || len(result.Armed.Later) != len(tc.want)-1 {
				t.Errorf("Armed = %+v, want the earliest firing and the rest in Later", result.Armed)
			}
		})
	}
}

// TestI33_EventRemindersFollowTheUsersPreferences: the leads are the user's
// own, read from the vault at arm time (ADR-0029 point 5); a stored value
// that fails validation falls back to its own default.
func TestI33_EventRemindersFollowTheUsersPreferences(t *testing.T) {
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
			triggers, _, _ := i33CaptureWith(t, at(1, 10, 0), tc.response, cfg)
			if got := fires(triggers); !slices.EqualFunc(got, tc.want, time.Time.Equal) {
				t.Errorf("fires at %v, want %v", got, tc.want)
			}
		})
	}
}

// TestI33_NoUnaskedRingOnTheDayTheEventIsTold is doc 02 §7's "Lead time"
// (ADR-0037): a lead nobody asked for that would ring on the local day the
// event is captured, for an event on a later day, is not armed — the user just
// said it — unless no other reminder would be left, when the nearest one stays.
func TestI33_NoUnaskedRingOnTheDayTheEventIsTold(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	const timed = `{"type":"event","normalized_content":"dentista el 2026-10-16 a las 10:00","weight":0.7,"decay_rate":0.05,"event_at":"2026-10-16T10:00:00-03:00"}`
	const dateOnly = `{"type":"event","normalized_content":"renovar el pasaporte el 2026-10-16","weight":0.7,"decay_rate":0.05,"event_at":"2026-10-16"}`
	str := func(s string) *string { return &s }

	cases := []struct {
		name, response string
		leads          *string
		now            time.Time
		want           []time.Time
	}{
		{"tomorrow at 10 told at 08:44: only the 2-hour ring", timed, nil, at(15, 8, 44), []time.Time{at(16, 8, 0)}},
		{"a date-only event tomorrow told at 08:00: its one ring stays", dateOnly, nil, at(15, 8, 0), []time.Time{at(15, 9, 0)}},
		{"every lead lands today: the nearest stays", timed, str(`[1500,1560]`), at(15, 7, 30), []time.Time{at(15, 9, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := memrepo.NewConfig()
			cfg.SeedConfig(t, ports.VaultConfig{EventReminderLeads: tc.leads})
			triggers, _, result := i33CaptureWith(t, tc.now, tc.response, cfg)
			var got []time.Time
			for _, r := range triggers.All() {
				got = append(got, *r.FireAt)
			}
			sort.Slice(got, func(i, j int) bool { return got[i].Before(got[j]) })
			if !slices.EqualFunc(got, tc.want, time.Time.Equal) {
				t.Errorf("fires at %v, want %v", got, tc.want)
			}
			if result.Armed == nil || len(result.Armed.Later) != len(tc.want)-1 {
				t.Errorf("Armed = %+v, want the reply to name exactly the armed rings", result.Armed)
			}
		})
	}
}

// TestI33_TheKeptRingIsNeverAtOnce is doc 02 §7's "Lead time" (ADR-0039): an
// event on a later day than it is told, whose every lead is behind, is not
// rung at once about what the user just said. A date-only one rings on its own
// day at the date-only time; a timed one at that time on its day when it is
// before the event, else at quiet end; with no daytime left before it, none.
func TestI33_TheKeptRingIsNeverAtOnce(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	event := func(instant string) string {
		return `{"type":"event","normalized_content":"evento","weight":0.7,"decay_rate":0.05,"event_at":"` + instant + `"}`
	}
	str := func(s string) *string { return &s }

	cases := []struct {
		name, response string
		leads          *string
		now            time.Time
		want           []time.Time
	}{
		{"a date-only event tomorrow told at 13:32: tomorrow at 09:00", event("2026-10-16"), nil, at(15, 13, 32), []time.Time{at(16, 9, 0)}},
		{"a timed event tomorrow whose one lead is behind: 09:00 that day", event("2026-10-16T15:00:00-03:00"), str(`[1440]`), at(15, 16, 0), []time.Time{at(16, 9, 0)}},
		{"before 09:00 it is quiet end that day", event("2026-10-16T08:30:00-03:00"), str(`[1440]`), at(15, 9, 0), []time.Time{at(16, 7, 0)}},
		{"no daytime left before it: nothing", event("2026-10-16T00:30:00-03:00"), nil, at(15, 23, 0), nil},
		{"05:00 tomorrow said at 22:30: its 2-hour lead, pulled by the night to now, does not ring", event("2026-10-16T05:00:00-03:00"), nil, at(15, 22, 30), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := memrepo.NewConfig()
			cfg.SeedConfig(t, ports.VaultConfig{EventReminderLeads: tc.leads})
			triggers, log, result := i33CaptureWith(t, tc.now, tc.response, cfg)
			var got []time.Time
			for _, r := range triggers.All() {
				got = append(got, *r.FireAt)
			}
			if !slices.EqualFunc(got, tc.want, time.Time.Equal) {
				t.Errorf("fires at %v, want %v", got, tc.want)
			}
			if len(tc.want) > 0 && (result.Armed == nil || result.Armed.Immediate) {
				t.Errorf("Armed = %+v, want a ring that is not at once", result.Armed)
			}
			if len(tc.want) == 0 {
				rows, err := log.Since(context.Background(), time.Time{}, 100)
				if err != nil {
					t.Fatal(err)
				}
				refused := slices.ContainsFunc(rows, func(d ports.Decision) bool {
					return d.Action == ports.ActionCaptureArmRefused && strings.Contains(string(d.Context), string(prospection.RefusalQuietHours))
				})
				if result.Outcome != brain.OutcomeStored || !refused {
					t.Errorf("outcome %q, refused row %v; want the unit stored and a quiet_hours refusal row", result.Outcome, refused)
				}
			}
		})
	}
}

// i33Capture runs one capture of response at now over fresh fakes, with no
// stored preferences.
func i33Capture(t *testing.T, now time.Time, response string) (*memrepo.Triggers, ports.DecisionLog, brain.CaptureResult) {
	t.Helper()
	return i33CaptureWith(t, now, response, nil)
}

// i33CaptureWith runs one capture reading its reminder preferences from cfg
// when it is not nil.
func i33CaptureWith(t *testing.T, now time.Time, response string, cfg ports.ConfigRepo) (*memrepo.Triggers, ports.DecisionLog, brain.CaptureResult) {
	t.Helper()
	ctx := context.Background()
	embeddings := memrepo.NewEmbeddings()
	triggers := memrepo.NewTriggers()
	log := memrepo.NewDecisionLog()
	llm := fakeprovider.New(t, i28Case(t, "i33", "replayed", response), "i33")
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

// TestI33_NothingUnaskedRingsAtOnceOnTheToldDay is doc 02 §7's "Lead time"
// (ADR-0047): an event today, told when a lead nobody asked for is already
// behind, never rings at the moment it is told. The lead behind is dropped, a
// lead still ahead stays, and with none left there is no reminder: the unit is
// stored, capture.arm.refused (leads_behind) says why, and the reply promises
// no ring. The client's case, 2026-10-20 at 17:14: "hoy a las 21 tengo turno
// con el kinesiólogo" with a 4-hour lead was answered "Te aviso ahora mismo.".
func TestI33_NothingUnaskedRingsAtOnceOnTheToldDay(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	now := time.Date(2026, 10, 20, 17, 14, 0, 0, zone)
	const event = `{"type":"event","normalized_content":"turno con el kinesiólogo el 2026-10-20 a las 21:00",` +
		`"weight":0.7,"decay_rate":0.05,"language":"es","event_at":"2026-10-20T21:00:00-03:00"`
	str := func(s string) *string { return &s }

	cases := []struct {
		name, response string
		leads          *string
		want           []time.Time
		reply          string
	}{
		{"a 4-hour lead already behind: no ring", event + `}`, str(`[240]`), nil, "Anotado."},
		{"its own 4-hour lead already behind: no ring", event + `,"reminder_leads_minutes":[240]}`, nil, nil, "Anotado."},
		{"the 2-hour lead still ahead stays, the 24-hour one is dropped", event + `}`, nil,
			[]time.Time{time.Date(2026, 10, 20, 19, 0, 0, 0, zone)}, "Anotado para el mar 20 oct, 21:00. Te aviso unas horas antes."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := memrepo.NewConfig()
			cfg.SeedConfig(t, ports.VaultConfig{EventReminderLeads: tc.leads})
			triggers, log, result := i33CaptureWith(t, now, tc.response, cfg)
			var got []time.Time
			for _, r := range triggers.All() {
				got = append(got, *r.FireAt)
			}
			if !slices.EqualFunc(got, tc.want, time.Time.Equal) {
				t.Errorf("fires at %v, want %v", got, tc.want)
			}
			if result.Armed != nil && result.Armed.Immediate {
				t.Errorf("Armed = %+v, want no ring at once", result.Armed)
			}
			if got := channels.RenderReply(result); got != tc.reply {
				t.Errorf("reply = %q, want %q", got, tc.reply)
			}
			if len(tc.want) > 0 {
				return
			}
			rows, err := log.Since(context.Background(), time.Time{}, 100)
			if err != nil {
				t.Fatal(err)
			}
			refused := slices.ContainsFunc(rows, func(d ports.Decision) bool {
				return d.Action == ports.ActionCaptureArmRefused && strings.Contains(string(d.Context), string(prospection.RefusalLeadsBehind))
			})
			if result.Outcome != brain.OutcomeStored || !refused {
				t.Errorf("outcome %q, refused row %v; want the unit stored and a leads_behind refusal row", result.Outcome, refused)
			}
		})
	}
}
