// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// noDestinationVault is a vault holding one reminder of every shape a pass with
// nowhere to speak could be tempted to touch: a fresh one due now, an overdue
// one, a moved one and a fired one that is late, all about events still ahead.
func noDestinationVault(t *testing.T) *i37Vault {
	v := newI37Vault(t)
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.row("fresh", "u", "dentist", i37Now.Add(-time.Minute), modern(event, 120))
	v.row("overdue", "u", "dentist", i37Day(13, 20, 0), modern(event, 24*60))
	moved := i37Day(15, 20, 0)
	v.event("m", &moved)
	v.row("moved", "m", "flight", i37Now.Add(-time.Minute), modern(i37Day(14, 22, 0), 120))
	v.event("f", &event)
	v.fired("late", "f", "visa", i37Day(13, 20, 0), modern(event, 24*60))
	return v
}

// TestI37_NilChannelTouchesNoRowAheadAndArmsNothing: a vault with no channel has
// nowhere to speak, so its due scan leaves every one-shot reminder whose event
// is ahead exactly as it found it: not fired, not expired, not lapsed, not
// re-planned, and writes nothing.
func TestI37_NilChannelTouchesNoRowAheadAndArmsNothing(t *testing.T) {
	v := noDestinationVault(t)
	v.silent = true

	report := v.check(false)

	if all, _ := v.log.Since(context.Background(), time.Time{}, -1); len(all) != 0 {
		t.Errorf("the pass wrote %+v, want nothing", all)
	}
	if got := len(v.armed("u")) + len(v.armed("m")); got != 3 || !v.undelivered()["late"] {
		t.Errorf("armed %d, undelivered %v; want every row as found", got, v.undelivered())
	}
	if report.TriggersFired+report.TriggersExpired+report.TriggersLapsed+report.RemindersArmed != 0 {
		t.Errorf("report = %+v; want no effect", report)
	}
}

// TestI37_NoConversationOneLoudRow: a channel with nowhere to push to is a
// configuration the user meant to work (non-negotiable 7), so it is said, once
// per reminder per process rather than once per pass, and no reminder is fired
// into a state it cannot leave. A dry run says nothing.
func TestI37_NoConversationOneLoudRow(t *testing.T) {
	v := noDestinationVault(t)
	v.noConversation = true

	v.checkAt(i37Now, true)
	if got := v.rows(ports.ActionCheckDeliveryFailed); len(got) != 0 {
		t.Fatalf("a dry run recorded %+v", got)
	}
	for i := 0; i < 3; i++ {
		v.checkAt(i37Now.Add(time.Duration(i)*5*time.Minute), false)
	}

	seen := map[string]int{}
	for _, r := range v.rows(ports.ActionCheckDeliveryFailed) {
		seen[r.ID]++
	}
	for _, id := range []string{"fresh", "overdue", "moved"} {
		if seen[id] != 1 {
			t.Errorf("check.delivery_failed rows for %s = %d, want exactly one in three passes", id, seen[id])
		}
	}
	if got := len(v.armed("u")) + len(v.armed("m")); got != 3 || len(v.ch.texts) != 0 {
		t.Errorf("armed %d, sent %d; want every row still armed and nothing sent", got, len(v.ch.texts))
	}
}

// TestI15_OverdueExpiresWithAChannel: ADR-0009's expiry belongs to a pass that
// can speak: with a channel and a conversation an overdue one-shot reminder with
// no unit at all expires, as it always has.
func TestI15_OverdueExpiresWithAChannel(t *testing.T) {
	v := newI37Vault(t)
	fireAt := i37Day(13, 20, 0)
	if err := v.triggers.Create(context.Background(), ports.Trigger{ID: "x", Kind: ports.TriggerKindTimeBased, FireAt: &fireAt, CreatedAt: fireAt}); err != nil {
		t.Fatal(err)
	}

	if report := v.check(false); report.TriggersExpired != 1 {
		t.Errorf("report = %+v; want the overdue unit-less reminder expired", report)
	}
}

// TestI15_OverdueIsKeptWithANilChannel amends ADR-0009 (ADR-0032): a pass with
// no channel cannot re-plan, and expiring an overdue reminder there would lose
// the one a later pass could still have replaced. It is kept, a unit-less one
// included, and an event that has passed still expires.
func TestI15_OverdueIsKeptWithANilChannel(t *testing.T) {
	v := newI37Vault(t)
	v.silent = true
	fireAt := i37Day(13, 20, 0)
	if err := v.triggers.Create(context.Background(), ports.Trigger{ID: "x", Kind: ports.TriggerKindTimeBased, FireAt: &fireAt, CreatedAt: fireAt}); err != nil {
		t.Fatal(err)
	}
	v.event("gone", i37At(13, 15))
	v.row("past", "gone", "flight", i37Day(13, 9, 0), nil)

	report := v.check(false)

	if report.TriggersExpired != 1 || len(v.armed("gone")) != 0 {
		t.Errorf("report = %+v; want only the reminder whose event passed expired", report)
	}
	due, _ := v.triggers.Due(context.Background(), i37Now)
	if len(due) != 1 || due[0].ID != "x" {
		t.Errorf("still armed and due = %+v, want the unit-less overdue reminder kept", due)
	}
}

// TestI37_TodayListsArmedRemindersWithoutAChannel: on a vault that cannot speak
// the armed one-shot reminders due within a day are the only place the user can
// see them, so Today lists them. Viewing writes nothing (I27).
func TestI37_TodayListsArmedRemindersWithoutAChannel(t *testing.T) {
	v := newI37Vault(t)
	v.silent = true
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.row("soon", "u", "dentist", i37Day(14, 18, 0), modern(event, 120))
	v.row("overdue", "u", "dentist", i37Day(13, 20, 0), modern(event, 24*60))
	far := i37Day(20, 20, 0)
	v.event("far", &far)
	v.row("later", "far", "flight", i37Day(19, 20, 0), modern(far, 24*60))
	v.event("gone", i37At(14, 9))
	v.row("passed", "gone", "flight", i37Day(14, 8, 0), func(tr *ports.Trigger) { tr.Payload.LeadMinutes = 120 })
	yearly := prospection.RuleYearly
	v.row("birthday", "u", "birthday", i37Day(14, 12, 0), func(tr *ports.Trigger) {
		tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
	})
	cfg := memrepo.NewConfig()
	svc := brain.NewTodayService(fixedClock{now: i37Now}, v.units, cfg, memrepo.NewState(),
		&i27Triggers{Triggers: v.triggers, t: t}, memrepo.NewPendingQuestions(), v.log, brain.NewFocusKeeper(v.units, cfg, memrepo.NewRelations()), nil)

	today, err := svc.Today(context.Background())
	if err != nil {
		t.Fatalf("Today: %v", err)
	}

	if len(today.Upcoming) != 2 || today.Upcoming[0].TriggerID != "overdue" || today.Upcoming[1].TriggerID != "soon" {
		t.Errorf("Upcoming = %+v, want the armed one-shot reminders due within a day (the overdue one a silent vault kept included), not the one whose event passed, the later one nor the birthday", today.Upcoming)
	}
}

// TestI37_TodayToleratesAnUnreadableUnit: a page that aborts for one damaged
// unit row hides everything else the user could see. Today skips what it cannot
// judge, as a due scan does, and shows the rest.
func TestI37_TodayToleratesAnUnreadableUnit(t *testing.T) {
	v := newI37Vault(t)
	v.units = i37BrokenUnits{memrepo.NewUnits()}
	v.silent = true
	event := i37Day(14, 20, 0)
	v.fired("legacy-fired", "u1", "visa", i37Now.Add(-time.Hour), nil)
	v.row("legacy-armed", "u1", "visa", i37Now.Add(-time.Minute), nil)
	v.fired("ok", "u2", "dentist", i37Now.Add(-time.Hour), modern(event, 120))
	v.row("ok-armed", "u2", "dentist", i37Day(14, 18, 0), modern(event, 120))

	today, err := newI37Today(v).Today(context.Background())
	if err != nil {
		t.Fatalf("Today aborted for one unreadable unit: %v", err)
	}
	if today.Status.Undelivered != 1 || len(today.Upcoming) != 1 || today.Upcoming[0].TriggerID != "ok-armed" {
		t.Errorf("Today = undelivered %d, upcoming %+v; want the readable reminders only", today.Status.Undelivered, today.Upcoming)
	}
}
