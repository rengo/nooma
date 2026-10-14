// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// i37Now is a Wednesday, 10:00 UTC: past the digest hour and outside quiet
// hours, so a digest is due and nothing is deferred.
var i37Now = time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

// i37Channel records what a pass sent. attempts is every Send, texts the ones
// that went out; failIf, when set, makes a Send of that text fail.
type i37Channel struct {
	texts, attempts []string
	failIf          func(text string) bool
}

func (c *i37Channel) Name() string                                            { return "test" }
func (c *i37Channel) Receive(context.Context) ([]ports.ChannelMessage, error) { return nil, nil }
func (c *i37Channel) Confirm(context.Context, string) error                   { return nil }
func (c *i37Channel) Close() error                                            { return nil }
func (c *i37Channel) Send(_ context.Context, _ ports.ConversationID, text string) error {
	c.attempts = append(c.attempts, text)
	if c.failIf != nil && c.failIf(text) {
		return errors.New("the transport is down")
	}
	c.texts = append(c.texts, text)
	return nil
}

// i37Vault is the ports a due scan reads, in memory.
type i37Vault struct {
	t        *testing.T
	triggers *memrepo.Triggers
	units    ports.UnitRepo
	timers   *memrepo.Timers
	log      *memrepo.DecisionLog
	ch       *i37Channel
	// repo, when set, is the trigger store the scan sees (a failing wrapper).
	repo ports.TriggerRepo
	// silent: the scan has no channel, as `nooma check` on a vault without one.
	silent bool
	// noConversation: a channel, but nowhere to push to.
	noConversation bool
	// svc is the one long-lived service every pass of a test goes through.
	svc   *brain.CheckService
	clock *i37Clock
	// cfg, when set, is the vault's stored preferences (the user's quiet end).
	cfg ports.ConfigRepo
	// ids outlives a service, so a test can restart the process.
	ids counterIDs
}

func newI37Vault(t *testing.T) *i37Vault {
	return &i37Vault{t: t, triggers: memrepo.NewTriggers(), units: memrepo.NewUnits(), timers: memrepo.NewTimers(), log: memrepo.NewDecisionLog(), ch: &i37Channel{}}
}

// event stores an event unit whose instant is at (nil: it has none).
func (v *i37Vault) event(id string, at *time.Time) {
	v.t.Helper()
	if err := v.units.Create(context.Background(), unit.Unit{
		ID: id, Type: unit.TypeEvent, Status: unit.StatusPool, Content: id, Weight: 1,
		EventAt: at, LastTouchedAt: i37Now, CreatedAt: i37Now, UpdatedAt: i37Now,
	}); err != nil {
		v.t.Fatalf("seed unit %s: %v", id, err)
	}
}

// row stores a one-shot reminder for unitID that fires at fireAt, saying text.
func (v *i37Vault) row(id, unitID, text string, fireAt time.Time, mutate func(*ports.Trigger)) {
	v.t.Helper()
	tr := ports.Trigger{
		ID: id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &fireAt, CreatedAt: fireAt.Add(-time.Hour),
		Payload: ports.TriggerPayload{ActionText: text, LeadDays: 7},
	}
	if mutate != nil {
		mutate(&tr)
	}
	if err := v.triggers.Create(context.Background(), tr); err != nil {
		v.t.Fatalf("seed trigger %s: %v", id, err)
	}
}

// fired is row, already fired and never delivered: what a vault that ran
// without a channel holds.
func (v *i37Vault) fired(id, unitID, text string, fireAt time.Time, mutate func(*ports.Trigger)) {
	v.t.Helper()
	v.row(id, unitID, text, fireAt, mutate)
	if err := v.triggers.Fire(context.Background(), id, fireAt); err != nil {
		v.t.Fatalf("fire %s: %v", id, err)
	}
}

func (v *i37Vault) check(dry bool) brain.CheckReport {
	v.t.Helper()
	return v.checkAt(i37Now, dry)
}

// i37Clock is a clock a test moves between passes of one long-lived service.
type i37Clock struct{ now time.Time }

func (c *i37Clock) Now() time.Time { return c.now }

func (v *i37Vault) checkAt(now time.Time, dry bool) brain.CheckReport {
	v.t.Helper()
	report, err := v.tryCheckAt(now, dry)
	if err != nil {
		v.t.Fatalf("Check: %v", err)
	}
	return report
}

// tryCheckAt is checkAt for a test that expects the pass to be able to fail.
func (v *i37Vault) tryCheckAt(now time.Time, dry bool) (brain.CheckReport, error) {
	v.t.Helper()
	if v.svc == nil {
		var repo ports.TriggerRepo = v.triggers
		if v.repo != nil {
			repo = v.repo
		}
		var channel ports.Channel = v.ch
		if v.silent {
			channel = nil
		}
		conversation := ports.ConversationID("12449194")
		if v.noConversation {
			conversation = ""
		}
		v.clock = &i37Clock{}
		v.svc = brain.NewCheckService(v.clock, repo, v.timers, &v.ids, v.log, channel,
			v.units, memrepo.NewState(), nil, conversation, nil, nil)
	}
	if v.cfg != nil {
		v.svc.WithQuietHours(v.cfg)
	}
	v.clock.now = now
	return v.svc.Check(context.Background(), brain.CheckRequest{DryRun: dry})
}

// rows are the decision_log rows of one action, as the ids and reasons they name.
func (v *i37Vault) rows(action ports.DecisionAction) []struct{ ID, Reason string } {
	v.t.Helper()
	all, err := v.log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		v.t.Fatal(err)
	}
	var out []struct{ ID, Reason string }
	for _, d := range all {
		if d.Action != action {
			continue
		}
		var c struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(d.Context, &c); err != nil {
			v.t.Fatal(err)
		}
		out = append(out, struct{ ID, Reason string }{c.ID, c.Reason})
	}
	return out
}

func (v *i37Vault) undelivered() map[string]bool {
	v.t.Helper()
	got, err := v.triggers.Undelivered(context.Background())
	if err != nil {
		v.t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, d := range got {
		ids[d.ID] = true
	}
	return ids
}

func i37At(day, hour int) *time.Time {
	t := time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC)
	return &t
}

// TestI37_ClientsSevenRowsSendNothingPast replays the client's vault of
// 2026-10-14 (ADR-0032): seven undelivered reminders, all armed by a binary
// that fired them days earlier with no channel, three of them about flights
// that had already departed. None is sent: the three past flights lapse
// (passed), and the four whose event is still ahead lapse as late and get the
// reminders their leads still have ahead, each with a row saying why.
func TestI37_ClientsSevenRowsSendNothingPast(t *testing.T) {
	v := newI37Vault(t)
	for _, u := range []struct {
		id string
		at *time.Time
	}{
		{"u-dentist", i37At(15, 11)}, {"u-flight-15", i37At(15, 8)}, {"u-flight-16", i37At(16, 6)},
		{"u-past-1", i37At(11, 14)}, {"u-past-2", i37At(12, 9)}, {"u-past-3", i37At(12, 20)},
	} {
		v.event(u.id, u.at)
	}
	fireAt := i37At(9, 16)
	// dentist twice: one unit, two rows, as the client's vault held it.
	for _, r := range []struct{ id, unit, text string }{
		{"d1", "u-dentist", "dentist ahead"}, {"d2", "u-dentist", "dentist ahead again"},
		{"f15", "u-flight-15", "flight ahead 15"}, {"f16", "u-flight-16", "flight ahead 16"},
		{"p1", "u-past-1", "flight already gone 1"}, {"p2", "u-past-2", "flight already gone 2"}, {"p3", "u-past-3", "flight already gone 3"},
	} {
		v.fired(r.id, r.unit, r.text, *fireAt, nil)
	}

	report := v.check(false)

	if report.TriggersLapsed != 7 || report.RemindersArmed != 5 || report.DigestCarried != 0 || len(v.ch.texts) != 0 {
		t.Errorf("report = %+v (sent %q); want seven lapsed, five re-planned, nothing sent", report, v.ch.texts)
	}
	passed := 0
	for _, row := range v.rows(ports.ActionCheckTriggerLapsed) {
		if row.Reason == string(prospection.LapsePassed) {
			if !strings.HasPrefix(row.ID, "p") {
				t.Errorf("row %+v lapsed as passed, but its event is ahead", row)
			}
			passed++
		}
	}
	if passed != 3 {
		t.Errorf("%d rows lapsed as passed, want the three flights that departed", passed)
	}
	if left := v.undelivered(); len(left) != 0 {
		t.Errorf("still undelivered after the pass: %v", left)
	}
}

// TestI37_ArmedOverdueKeepsTodaysVerdict: link 2 touches fired rows only. An
// armed reminder overdue past ADR-0009's window is expired as before, with the
// same row and counter, not lapsed, and its unit's event being ahead changes
// nothing.
func TestI37_ArmedOverdueKeepsTodaysVerdict(t *testing.T) {
	v := newI37Vault(t)
	v.event("u1", i37At(20, 9))
	v.row("overdue", "u1", "dentist", *i37At(13, 20), nil) // 14 hours late

	report := v.check(false)

	if report.TriggersExpired != 1 || report.TriggersLapsed != 0 || len(v.ch.texts) != 0 {
		t.Errorf("report = %+v (sent %d); want it expired, not lapsed, nothing sent", report, len(v.ch.texts))
	}
	if got := v.rows(ports.ActionCheckTriggerExpired); len(got) != 1 || got[0].ID != "overdue" {
		t.Errorf("check.trigger.expired rows = %+v, want the one", got)
	}
	if got := v.rows(ports.ActionCheckTriggerLapsed); len(got) != 0 {
		t.Errorf("check.trigger.lapsed rows = %+v, want none: an armed row is expired", got)
	}
}

// TestI37_ArmedRowWhoseEventPassedIsExpiredNotSent: the gate also runs when an
// armed reminder comes due. A pre-ADR-0029 row carries no lead, so only its
// unit says its event has gone.
func TestI37_ArmedRowWhoseEventPassedIsExpiredNotSent(t *testing.T) {
	v := newI37Vault(t)
	v.event("gone", i37At(12, 9))
	v.event("ahead", i37At(15, 9))
	just := i37Now.Add(-time.Minute)
	v.row("old", "gone", "flight already gone", just, nil)
	v.row("fresh", "ahead", "flight ahead", just, nil)

	report := v.check(false)

	if report.TriggersExpired != 1 || report.TriggersFired != 1 || report.TriggersDelivered != 1 {
		t.Errorf("report = %+v; want the past flight expired and the one ahead pushed", report)
	}
	if len(v.ch.texts) != 1 || !strings.Contains(v.ch.texts[0], "flight ahead") || strings.Contains(v.ch.texts[0], "already gone") {
		t.Errorf("sent %q, want only the flight ahead", v.ch.texts)
	}
	if got := v.rows(ports.ActionCheckTriggerExpired); len(got) != 1 || got[0].ID != "old" || got[0].Reason != string(prospection.LapsePassed) {
		t.Errorf("check.trigger.expired rows = %+v, want old with reason passed", got)
	}
}

// TestI37_PastBirthdayIsNeverSent: a recurring reminder's event is its next
// occurrence after fire_at, not the year the unit was captured in. One whose
// occurrence passed lapses; one whose occurrence is ahead is carried.
func TestI37_PastBirthdayIsNeverSent(t *testing.T) {
	v := newI37Vault(t)
	yearly := prospection.RuleYearly
	recurring := func(month time.Month, day int) func(*ports.Trigger) {
		return func(tr *ports.Trigger) {
			tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: month, Day: day}
		}
	}
	v.event("u-ana", i37At(1, 12)) // the unit's own event_at is an anchor, never an instant to compare
	v.event("u-leo", i37At(1, 12))
	v.fired("ana", "u-ana", "Ana's birthday", *i37At(4, 12), recurring(time.October, 11)) // 11 October: passed
	v.fired("leo", "u-leo", "Leo's birthday", *i37At(14, 7), recurring(time.October, 21)) // 21 October: ahead

	report := v.check(false)

	if report.TriggersLapsed != 1 || report.DigestCarried != 1 {
		t.Errorf("report = %+v; want Ana's lapsed and Leo's carried", report)
	}
	if len(v.ch.texts) != 1 || strings.Contains(v.ch.texts[0], "Ana") || !strings.Contains(v.ch.texts[0], "Leo") {
		t.Errorf("sent %q, want a digest with Leo's birthday and not Ana's", v.ch.texts)
	}
	if got := v.rows(ports.ActionCheckTriggerLapsed); len(got) != 1 || got[0].ID != "ana" {
		t.Errorf("check.trigger.lapsed rows = %+v, want Ana's", got)
	}
}

// TestTodayCountsAfterTheGate: Today mirrors the digest, so it applies the
// same gate, and being a view it writes nothing: the past reminder is neither
// counted nor listed, and its row is not lapsed (i37ReadOnlyTriggers fails the
// test on any Lapse, and the row is still undelivered afterwards).
func TestTodayCountsAfterTheGate(t *testing.T) {
	ctx := context.Background()
	v := newI37Vault(t)
	v.event("u-gone", i37At(12, 9))
	v.event("u-ahead", i37At(15, 9))
	v.fired("gone", "u-gone", "flight already gone", *i37At(9, 16), nil)
	v.fired("ahead", "u-ahead", "flight ahead", i37Now.Add(-time.Hour), nil)

	triggers := &i37ReadOnlyTriggers{Triggers: v.triggers, t: t}
	cfg, rels := memrepo.NewConfig(), memrepo.NewRelations()
	svc := brain.NewTodayService(fixedClock{now: i37Now}, v.units, cfg, memrepo.NewState(), triggers, memrepo.NewPendingQuestions(), v.log, brain.NewFocusKeeper(v.units, cfg, rels))

	today, err := svc.Today(ctx)
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	if today.Status.Undelivered != 1 || len(today.Digest.Items) != 1 || today.Digest.Items[0].TriggerID != "ahead" {
		t.Errorf("Today = undelivered %d, items %+v; want only the flight ahead", today.Status.Undelivered, today.Digest.Items)
	}
	if left := v.undelivered(); len(left) != 2 {
		t.Errorf("undelivered after viewing = %v, want both rows untouched", left)
	}
}

// i37ReadOnlyTriggers fails the test on Lapse, the one write Today could be
// tempted into.
type i37ReadOnlyTriggers struct {
	*memrepo.Triggers
	t *testing.T
}

func (g *i37ReadOnlyTriggers) Lapse(context.Context, string) error {
	i27Fail(g.t, "TriggerRepo", "Lapse")
	return nil
}

// TestDryRunCountsALapseAndWritesNothing: a dry run reaches the verdict and
// reports it as the real pass would, and persists nothing: the row stays
// undelivered, no decision is logged, no message is sent.
func TestDryRunCountsALapseAndWritesNothing(t *testing.T) {
	v := newI37Vault(t)
	v.event("u-gone", i37At(12, 9))
	v.fired("gone", "u-gone", "flight already gone", *i37At(9, 16), nil)

	report := v.check(true)

	if report.TriggersLapsed != 1 {
		t.Errorf("TriggersLapsed = %d, want 1", report.TriggersLapsed)
	}
	if all, _ := v.log.Since(context.Background(), time.Time{}, -1); len(all) != 0 {
		t.Errorf("a dry run logged %+v", all)
	}
	if !v.undelivered()["gone"] || len(v.ch.texts) != 0 {
		t.Errorf("a dry run touched the row or sent (undelivered %v, sent %d)", v.undelivered(), len(v.ch.texts))
	}
	if real := v.check(false); real.TriggersLapsed != report.TriggersLapsed {
		t.Errorf("the real pass lapsed %d, the preview said %d", real.TriggersLapsed, report.TriggersLapsed)
	}
}

// i37Day is an instant on 2026-10-day at hour:minute UTC.
func i37Day(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC)
}

// TestI37_DateOnlyEventIsCarriedOnItsDay: a date-only event is stored as local
// midnight, so "at or before now" would call it passed from the first minute of
// its own day. It names a day, and is passed only once that day has ended: the
// undelivered reminder is pushed at ten on the day and lapsed the next one.
// The unit alone says the event (a legacy row), and so does the payload.
func TestI37_DateOnlyEventIsCarriedOnItsDay(t *testing.T) {
	day := i37Day(14, 0, 0)
	for _, shape := range []struct {
		name   string
		mutate func(*ports.Trigger)
	}{
		{"legacy row: only the unit knows the event", nil},
		{"modern row: the payload names the event", func(tr *ports.Trigger) { tr.Payload.About, tr.Payload.LeadDays = day, 0 }},
	} {
		t.Run(shape.name+"/on the day", func(t *testing.T) {
			v := newI37Vault(t)
			v.event("u1", &day)
			v.fired("r", "u1", "passport appointment", i37Day(14, 8, 0), shape.mutate)
			report := v.checkAt(i37Day(14, 10, 0), false)
			if report.TriggersLapsed != 0 || report.TriggersDelivered != 1 {
				t.Errorf("report = %+v; want the reminder pushed on the event's own day", report)
			}
		})
		t.Run(shape.name+"/the next day", func(t *testing.T) {
			v := newI37Vault(t)
			v.event("u1", &day)
			v.fired("r", "u1", "passport appointment", i37Day(14, 8, 0), shape.mutate)
			report := v.checkAt(i37Day(15, 10, 0), false)
			if report.TriggersLapsed != 1 || report.DigestCarried != 0 {
				t.Errorf("report = %+v; want the reminder lapsed once the day ended", report)
			}
		})
	}
}

// TestI37_BirthdayIsCarriedAfterNoonOnItsDay: a recurring occurrence is
// anchored at noon but names a calendar day, so a birthday reminder that never
// reached the user is still carried at 13:00 on the birthday and lapses the
// day after.
func TestI37_BirthdayIsCarriedAfterNoonOnItsDay(t *testing.T) {
	yearly := prospection.RuleYearly
	birthday := func(tr *ports.Trigger) {
		tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 11}
	}
	for _, tc := range []struct {
		name        string
		now         time.Time
		wantLapsed  int
		wantCarried int
	}{
		{"13:00 on the day", i37Day(11, 13, 0), 0, 1},
		{"the next day", i37Day(12, 10, 0), 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newI37Vault(t)
			v.event("u-ana", i37At(1, 12))
			v.fired("ana", "u-ana", "Ana's birthday", i37Day(4, 12, 0), birthday)
			report := v.checkAt(tc.now, false)
			if report.TriggersLapsed != tc.wantLapsed || report.DigestCarried != tc.wantCarried {
				t.Errorf("report = %+v; want lapsed %d, carried %d", report, tc.wantLapsed, tc.wantCarried)
			}
		})
	}
}

// TestI37_ModernFiredRowWithAPastAboutLapses: a row armed since ADR-0029 names
// its event in the payload, at-once rows (lead 0) included, and that event is
// what the unit is compared with. Where the unit still has it, it was
// yesterday and the reminder is past; where the unit says it is ahead, the
// event moved and the reminder is superseded. Either way it is not sent.
func TestI37_ModernFiredRowWithAPastAboutLapses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		unitAt *time.Time
		want   prospection.LapseReason
	}{
		{"the unit still has the event", i37At(13, 15), prospection.LapsePassed},
		{"the unit moved the event ahead", i37At(20, 9), prospection.LapseSuperseded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newI37Vault(t)
			v.event("u1", tc.unitAt)
			v.fired("r", "u1", "flight", i37Day(12, 9, 0), func(tr *ports.Trigger) {
				tr.Payload.About, tr.Payload.LeadMinutes, tr.Payload.LeadDays = i37Day(13, 15, 0), 0, 0
			})
			if report := v.check(false); report.TriggersLapsed != 1 || report.DigestCarried != 0 {
				t.Errorf("report = %+v; want the reminder lapsed on its payload's event", report)
			}
			if got := v.reasons(ports.ActionCheckTriggerLapsed); len(got) != 1 || got[0] != string(tc.want) {
				t.Errorf("lapsed reasons = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestI37_ModernArmedRowWithAPastAboutExpires: the same at fire time, for an
// armed at-once row whose payload event was yesterday.
func TestI37_ModernArmedRowWithAPastAboutExpires(t *testing.T) {
	for _, tc := range []struct {
		name   string
		unitAt *time.Time
		want   prospection.LapseReason
	}{
		{"the unit still has the event", i37At(13, 15), prospection.LapsePassed},
		{"the unit moved the event ahead", i37At(20, 9), prospection.LapseSuperseded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newI37Vault(t)
			v.event("u1", tc.unitAt)
			v.row("r", "u1", "flight", i37Now.Add(-time.Minute), func(tr *ports.Trigger) {
				tr.Payload.About, tr.Payload.LeadMinutes, tr.Payload.LeadDays = i37Day(13, 15, 0), 0, 0
			})
			report := v.check(false)
			if report.TriggersExpired != 1 || report.TriggersDelivered != 0 || len(v.ch.texts) != 0 {
				t.Errorf("report = %+v (sent %d); want it expired, not pushed", report, len(v.ch.texts))
			}
			if got := v.reasons(ports.ActionCheckTriggerExpired); len(got) != 1 || got[0] != string(tc.want) {
				t.Errorf("expired reasons = %v, want %v", got, tc.want)
			}
		})
	}
}

// i37BrokenUnits is a unit store whose rows cannot be read one by one.
type i37BrokenUnits struct{ *memrepo.Units }

func (i37BrokenUnits) ByID(context.Context, string) (unit.Unit, error) {
	return unit.Unit{}, errors.New("unit row is corrupt")
}

// TestI37_ACorruptUnitDoesNotStallAModernRow: a damaged unit row cannot abort
// the pass, and with it every due timer and the digest: a reminder that names
// its event is judged without the unit, and the unit is reported once with its
// id, however many rows it has.
func TestI37_ACorruptUnitDoesNotStallAModernRow(t *testing.T) {
	v := newI37Vault(t)
	v.units = i37BrokenUnits{memrepo.NewUnits()}
	v.row("modern", "u1", "dentist", i37Now.Add(-time.Minute), func(tr *ports.Trigger) {
		tr.Payload.About, tr.Payload.LeadMinutes = i37Day(15, 9, 0), 0
	})
	v.row("modern-too", "u1", "dentist again", i37Now.Add(-time.Minute), func(tr *ports.Trigger) {
		tr.Payload.About, tr.Payload.LeadMinutes = i37Day(15, 9, 0), 0
	})
	v.fired("waiting", "u2", "flight", i37Now.Add(-time.Hour), func(tr *ports.Trigger) {
		tr.Payload.About, tr.Payload.LeadDays = i37Day(16, 9, 0), 0
	})
	if err := v.timers.Create(context.Background(), ports.Timer{ID: "tmr", FireAt: i37Now.Add(-time.Minute), CreatedAt: i37Now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	report := v.check(false)

	if report.TriggersDelivered != 3 || report.TimersFired != 1 || report.DigestCarried != 0 {
		t.Errorf("report = %+v; want both modern rows and the waiting row pushed and the timer fired", report)
	}
	if got := v.rows(ports.ActionCheckDeliveryFailed); len(got) != 2 {
		t.Errorf("check.delivery_failed rows = %+v, want one per unit judged without its row", got)
	}
	all, _ := v.log.Since(context.Background(), time.Time{}, -1)
	units := map[string]bool{}
	for _, d := range all {
		var c struct {
			UnitID string `json:"unit_id"`
		}
		if d.Action == ports.ActionCheckDeliveryFailed && json.Unmarshal(d.Context, &c) == nil {
			units[c.UnitID] = true
		}
	}
	if !units["u1"] || !units["u2"] {
		t.Errorf("delivery_failed contexts name units %v, want u1 and u2", units)
	}
}

// newI37Today is Today over the vault's own stores.
func newI37Today(v *i37Vault) *brain.TodayService {
	cfg := memrepo.NewConfig()
	return brain.NewTodayService(fixedClock{now: i37Now}, v.units, cfg, memrepo.NewState(), v.triggers, memrepo.NewPendingQuestions(), v.log,
		brain.NewFocusKeeper(v.units, cfg, memrepo.NewRelations()))
}

func newI37State() ports.StateRepo { return memrepo.NewState() }
