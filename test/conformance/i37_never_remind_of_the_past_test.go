// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
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

// i37Channel records what a pass sent.
type i37Channel struct{ texts []string }

func (c *i37Channel) Name() string                                            { return "test" }
func (c *i37Channel) Receive(context.Context) ([]ports.ChannelMessage, error) { return nil, nil }
func (c *i37Channel) Confirm(context.Context, string) error                   { return nil }
func (c *i37Channel) Close() error                                            { return nil }
func (c *i37Channel) Send(_ context.Context, _ ports.ConversationID, text string) error {
	c.texts = append(c.texts, text)
	return nil
}

// i37Vault is the ports a due scan reads, in memory.
type i37Vault struct {
	t        *testing.T
	triggers *memrepo.Triggers
	units    *memrepo.Units
	log      *memrepo.DecisionLog
	ch       *i37Channel
}

func newI37Vault(t *testing.T) *i37Vault {
	return &i37Vault{t: t, triggers: memrepo.NewTriggers(), units: memrepo.NewUnits(), log: memrepo.NewDecisionLog(), ch: &i37Channel{}}
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
	report, err := brain.NewCheckService(fixedClock{now: i37Now}, v.triggers, memrepo.NewTimers(), &counterIDs{}, v.log, v.ch,
		v.units, memrepo.NewState(), nil, "12449194", nil, nil).Check(context.Background(), brain.CheckRequest{DryRun: dry})
	if err != nil {
		v.t.Fatalf("Check: %v", err)
	}
	return report
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
// that fired them minutes after capture with no channel, three of them about
// flights that had already departed. The digest carries the four whose event
// is ahead and lapses the three that are not, each with a row saying why.
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

	if report.TriggersLapsed != 3 || report.DigestCarried != 4 {
		t.Errorf("report = lapsed %d, carried %d; want 3 and 4: %+v", report.TriggersLapsed, report.DigestCarried, report)
	}
	if len(v.ch.texts) != 1 {
		t.Fatalf("sent %d messages, want the one digest", len(v.ch.texts))
	}
	for _, past := range []string{"already gone 1", "already gone 2", "already gone 3"} {
		if strings.Contains(v.ch.texts[0], past) {
			t.Errorf("the digest says %q, a flight that already departed:\n%s", past, v.ch.texts[0])
		}
	}
	for _, ahead := range []string{"dentist ahead", "flight ahead 15", "flight ahead 16"} {
		if !strings.Contains(v.ch.texts[0], ahead) {
			t.Errorf("the digest lost %q, an event still ahead:\n%s", ahead, v.ch.texts[0])
		}
	}
	lapsed := v.rows(ports.ActionCheckTriggerLapsed)
	if len(lapsed) != 3 {
		t.Fatalf("check.trigger.lapsed rows = %+v, want one per past flight", lapsed)
	}
	for _, row := range lapsed {
		if row.Reason != string(prospection.LapsePassed) || !strings.HasPrefix(row.ID, "p") {
			t.Errorf("lapsed row %+v, want a past flight with reason passed", row)
		}
	}
	if left := v.undelivered(); len(left) != 0 {
		t.Errorf("still undelivered after the digest: %v", left)
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

// TestI37_DigestIgnoresOverdueUntilLink3: a fired reminder waiting for the
// digest is judged on its event alone. Twelve hours after it fired (past
// ADR-0009's six-hour window), with its event ahead, it is carried: that
// window belongs to a push, and the digest is not one.
func TestI37_DigestIgnoresOverdueUntilLink3(t *testing.T) {
	v := newI37Vault(t)
	v.event("u1", i37At(15, 3))
	v.fired("late", "u1", "flight tomorrow", *i37At(13, 22), nil)

	report := v.check(false)

	if report.DigestCarried != 1 || report.TriggersLapsed != 0 || len(v.ch.texts) != 1 {
		t.Errorf("report = %+v (sent %d); want the twelve-hour-old reminder carried, none lapsed", report, len(v.ch.texts))
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
// counted nor listed, and its row is not lapsed (i27Triggers fails the test on
// any write).
func TestTodayCountsAfterTheGate(t *testing.T) {
	ctx := context.Background()
	v := newI37Vault(t)
	v.event("u-gone", i37At(12, 9))
	v.event("u-ahead", i37At(15, 9))
	v.fired("gone", "u-gone", "flight already gone", *i37At(9, 16), nil)
	v.fired("ahead", "u-ahead", "flight ahead", *i37At(9, 16), nil)

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
