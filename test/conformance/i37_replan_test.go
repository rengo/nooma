// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI37_StaleL24HealthyL2KeepsOneAtTheTwoHourLead (C1): the 24-hour reminder
// is stale (fired fourteen hours ago and never delivered, or armed and overdue)
// and the 2-hour one is armed and healthy. The re-plan must not carry the stale
// row onto the plan and then lose it: exactly one armed reminder remains, the
// original 2-hour row, moved nowhere.
func TestI37_StaleL24HealthyL2KeepsOneAtTheTwoHourLead(t *testing.T) {
	for _, staleArmed := range []bool{false, true} {
		v := newI37Vault(t)
		event := i37Day(14, 20, 0)
		v.event("u", &event)
		if staleArmed {
			v.row("l24", "u", "dentist", i37Day(13, 20, 0), modern(event, 24*60))
		} else {
			v.fired("l24", "u", "dentist", i37Day(13, 20, 0), modern(event, 24*60))
		}
		v.row("l2", "u", "dentist", i37Day(14, 18, 0), modern(event, 120))

		report := v.check(false)

		if got := v.armed("u"); len(got) != 1 || got[0].ID != "l2" || !got[0].FireAt.Equal(i37Day(14, 18, 0)) {
			t.Errorf("stale armed=%v: armed after the pass = %+v, want only l2 at 18:00", staleArmed, got)
		}
		if report.TriggersLapsed+report.TriggersExpired != 1 || report.RemindersArmed+report.RemindersMoved+report.RemindersCancelled != 0 || report.DigestCarried != 0 || len(v.ch.texts) != 0 {
			t.Errorf("stale armed=%v: report = %+v (sent %d); want the stale row retired and nothing else", staleArmed, report, len(v.ch.texts))
		}
	}
}

// supersededUnit seeds a reminder that went out about 15 October at 11:00 and
// was never delivered, for a unit since corrected to 16 October at 11:00.
func supersededUnit(v *i37Vault) {
	corrected := i37Day(16, 11, 0)
	v.event("u", &corrected)
	v.fired("old", "u", "dentist", i37Day(14, 8, 0), modern(i37Day(15, 11, 0), 27*60))
}

// TestI37_SupersededRowReplansTheNewInstant: a reminder about the old date is
// never sent, and the unit gets the reminders of its new date.
func TestI37_SupersededRowReplansTheNewInstant(t *testing.T) {
	v := newI37Vault(t)
	supersededUnit(v)

	report := v.check(false)

	got := v.armed("u")
	if len(got) != 2 || !got[0].FireAt.Equal(i37Day(15, 11, 0)) || !got[1].FireAt.Equal(i37Day(16, 9, 0)) {
		t.Fatalf("armed = %+v, want the 24 h and 2 h leads of 16 October", got)
	}
	if report.RemindersArmed != 2 || report.TriggersLapsed != 1 || report.DigestCarried != 0 || len(v.ch.texts) != 0 {
		t.Errorf("report = %+v (sent %d); want two armed, the old row lapsed, nothing sent", report, len(v.ch.texts))
	}
	if got := v.reasons(ports.ActionCheckTriggerLapsed); len(got) != 1 || got[0] != string(prospection.LapseSuperseded) {
		t.Errorf("lapsed reasons = %v, want superseded", got)
	}
	if len(v.rows(ports.ActionCheckReminderArmed)) != 2 {
		t.Errorf("check.reminder.armed rows = %+v, want one per armed reminder", v.rows(ports.ActionCheckReminderArmed))
	}
}

// lapseFailsOnce is a trigger store whose first Lapse fails.
type lapseFailsOnce struct {
	ports.TriggerRepo
	failed bool
}

func (r *lapseFailsOnce) Lapse(ctx context.Context, id string) error {
	if !r.failed {
		r.failed = true
		return errors.New("disk full")
	}
	return r.TriggerRepo.Lapse(ctx, id)
}

// TestI37_AbortMidPassThenCleanPassCreatesNothingTwice: the plan is written
// before the stale row is retired, so a pass that dies in between leaves the
// row un-lapsed, and the next pass finds the plan already armed and carries it.
func TestI37_AbortMidPassThenCleanPassCreatesNothingTwice(t *testing.T) {
	v := newI37Vault(t)
	supersededUnit(v)
	v.repo = &lapseFailsOnce{TriggerRepo: v.triggers}
	ctx := context.Background()

	_, err := brain.NewCheckService(fixedClock{now: i37Now}, v.repo, v.timers, &counterIDs{}, v.log, v.ch, v.units, newI37State(), nil, "12449194", nil, nil, nil).Check(ctx, brain.CheckRequest{})
	if err == nil {
		t.Fatal("the first pass did not fail on Lapse")
	}
	if got := len(v.armed("u")); got != 2 {
		t.Fatalf("armed after the aborted pass = %d, want the plan written first", got)
	}
	if !v.undelivered()["old"] {
		t.Fatal("the stale row was retired before the pass could finish")
	}

	report := v.check(false)

	if got := len(v.armed("u")); got != 2 || report.RemindersArmed != 0 || report.TriggersLapsed != 1 {
		t.Errorf("after the clean pass: armed %d, report %+v; want the same two, none created, the row lapsed", got, report)
	}
}

// TestI37_DentistLapsedInDigestPathIsReplanned: a reminder that fired
// undelivered for an event this evening, with its 2-hour lead still ahead,
// is replaced by that lead before the digest runs, and the digest does not
// carry the old one.
func TestI37_DentistLapsedInDigestPathIsReplanned(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.fired("late", "u", "dentist", i37Day(13, 20, 0), modern(event, 24*60))

	report := v.check(false)

	if got := v.armed("u"); len(got) != 1 || !got[0].FireAt.Equal(i37Day(14, 18, 0)) {
		t.Errorf("armed = %+v, want the 2 h lead at 18:00", got)
	}
	if report.RemindersArmed != 1 || report.TriggersLapsed != 1 || report.DigestCarried != 0 || len(v.ch.texts) != 0 {
		t.Errorf("report = %+v (sent %d); want one armed, one lapsed, nothing sent", report, len(v.ch.texts))
	}
}

// TestI37_LegacyRowSupersededByNewerArming (D8): a row that names no event
// cannot be compared with its unit, but a unit that has since armed a reminder
// of its own has moved on from the legacy one, which lapses as superseded
// though it is not late.
func TestI37_LegacyRowSupersededByNewerArming(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.fired("legacy", "u", "dentist", i37Day(14, 9, 0), nil)
	v.row("l2", "u", "dentist", i37Day(14, 18, 0), modern(event, 120))

	report := v.check(false)

	if report.TriggersLapsed != 1 || report.DigestCarried != 0 || len(v.ch.texts) != 0 {
		t.Errorf("report = %+v (sent %d); want the legacy row lapsed", report, len(v.ch.texts))
	}
	if got := v.reasons(ports.ActionCheckTriggerLapsed); len(got) != 1 || got[0] != string(prospection.LapseSuperseded) {
		t.Errorf("lapsed reasons = %v, want superseded", got)
	}
	if got := v.armed("u"); len(got) != 1 || got[0].ID != "l2" || report.RemindersMoved != 0 {
		t.Errorf("armed = %+v, moved %d; want l2 untouched", got, report.RemindersMoved)
	}
}

// overdueArmed seeds an armed 24-hour reminder fourteen hours late, for an
// event this evening whose 2-hour lead is still ahead.
func overdueArmed(v *i37Vault) {
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.row("a24", "u", "dentist", i37Day(13, 20, 0), modern(event, 24*60))
}

// TestI37_CanDeliverDefersOverdueArmedToTheStep: a pass that can deliver does
// not expire an overdue armed reminder on its own: the re-plan step arms the
// lead still ahead first and only then retires it.
func TestI37_CanDeliverDefersOverdueArmedToTheStep(t *testing.T) {
	v := newI37Vault(t)
	overdueArmed(v)

	report := v.check(false)

	if got := v.armed("u"); len(got) != 1 || !got[0].FireAt.Equal(i37Day(14, 18, 0)) {
		t.Errorf("armed = %+v, want the 2 h lead at 18:00", got)
	}
	if report.TriggersExpired != 1 || report.RemindersArmed != 1 || len(v.ch.texts) != 0 {
		t.Errorf("report = %+v (sent %d); want a24 expired and the 2 h lead armed", report, len(v.ch.texts))
	}
}

// TestI37_DryRunEqualsTheRealScan: a dry run over a vault with a superseded
// row, a deferred overdue row, a held unit, a past one and one that fires in the pass reports exactly what
// the real pass over a twin does, and writes nothing.
func TestI37_DryRunEqualsTheRealScan(t *testing.T) {
	seed := func(v *i37Vault) {
		supersededUnit(v)
		event := i37Day(14, 20, 0)
		v.event("u2", &event)
		v.row("a24", "u2", "dentist", i37Day(13, 20, 0), modern(event, 24*60))
		soon := i37Day(14, 11, 0)
		v.event("u3", &soon)
		v.fired("held", "u3", "flight", i37Day(13, 11, 0), modern(soon, 24*60))
		v.event("u4", i37At(12, 9))
		v.fired("gone", "u4", "flight already gone", i37Day(9, 16, 0), nil)
		// A reminder that fires and is pushed in this very pass covers its lead,
		// so a dry run, which fires nothing, must see it as sent, not as armed.
		noon := i37Day(14, 12, 0)
		v.event("u5", &noon)
		v.fired("s24", "u5", "flight", i37Day(13, 12, 0), modern(noon, 24*60))
		v.row("due", "u5", "flight", i37Day(14, 10, 0), func(tr *ports.Trigger) {
			modern(noon, 120)(tr)
			tr.Payload.UserLeadsMinutes = nil
		})
		// A reminder expired as passed in this pass is no longer a live row of
		// its unit, though a dry run, which expires nothing, still reads it armed.
		evening := i37Day(14, 20, 0)
		v.event("u7", &evening)
		v.row("passed-lead", "u7", "dentist", i37Day(14, 8, 0), func(tr *ports.Trigger) { tr.Payload.LeadMinutes = 120 })
		v.fired("late7", "u7", "dentist", i37Day(13, 20, 0), modern(evening, 24*60))
	}
	dry, real := newI37Vault(t), newI37Vault(t)
	seed(dry)
	seed(real)

	preview, report := dry.check(true), real.check(false)

	if preview != report {
		t.Errorf("dry run reported %+v, the real pass %+v", preview, report)
	}
	if report.RemindersArmed != 5 || report.RemindersMoved != 0 || report.TriggersLapsed != 5 || report.TriggersExpired != 2 || report.TriggersFired != 1 {
		t.Errorf("real report = %+v; the scenario no longer exercises every step", report)
	}
	if all, _ := dry.log.Since(context.Background(), time.Time{}, -1); len(all) != 0 || len(dry.armed("u")) != 0 || len(dry.armed("u2")) != 1 || len(dry.armed("u5")) != 1 || len(dry.armed("u7")) != 1 || len(dry.undelivered()) != 5 {
		t.Errorf("the dry run wrote: log %+v, armed u %d, u2 %d, undelivered %v", all, len(dry.armed("u")), len(dry.armed("u2")), dry.undelivered())
	}
}

// TestI37_AnUnreadableUnitHoldsItsStaleRowsAndIsReported: a unit the re-plan
// step cannot read cannot be planned for. The pass does not abort, which would
// cost the digest and every timer behind it; its stale rows are held, and the
// unit is recorded once as unreadable however many rows it has.
func TestI37_AnUnreadableUnitHoldsItsStaleRowsAndIsReported(t *testing.T) {
	v := newI37Vault(t)
	v.units = i37BrokenUnits{memrepo.NewUnits()}
	event := i37Day(20, 9, 0)
	v.fired("a", "u", "dentist", i37Day(13, 9, 0), modern(event, 24*60))
	v.fired("b", "u", "dentist", i37Day(13, 10, 0), modern(event, 120))

	report := v.check(false)

	if report.TriggersLapsed != 0 || report.RemindersArmed != 0 || len(v.undelivered()) != 2 || len(v.ch.texts) != 0 {
		t.Errorf("report = %+v (undelivered %v); want both rows held", report, v.undelivered())
	}
	if got := v.rows(ports.ActionCheckDeliveryFailed); len(got) != 1 {
		t.Errorf("check.delivery_failed rows = %+v, want one for the unit", got)
	}
}

// TestI37_ALateRowThatServedBothLeadsDoesNotCoverTheTwoHourLead: the late 24-hour
// reminder was merged with the 2-hour lead (its payload serves both) and never
// reached the user. It must not count as sent, or the 2-hour lead would be
// "covered" by a reminder nobody saw and the unit would be left with nothing.
func TestI37_ALateRowThatServedBothLeadsDoesNotCoverTheTwoHourLead(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.fired("both", "u", "dentist", i37Day(13, 20, 0), func(tr *ports.Trigger) {
		modern(event, 24*60)(tr)
		tr.Payload.UserLeadsMinutes = []int{24 * 60, 120}
	})

	report := v.check(false)

	if got := v.armed("u"); len(got) != 1 || !got[0].FireAt.Equal(i37Day(14, 18, 0)) || report.RemindersArmed != 1 {
		t.Errorf("armed = %+v, report %+v; want the 2 h lead armed at 18:00", got, report)
	}
}

// TestI37_APassThatCannotDeliverReplansNothing: with no channel, or a
// channel and nowhere to push to, a superseded or late fired row is neither
// re-planned, nor lapsed, nor sent.
func TestI37_APassThatCannotDeliverReplansNothing(t *testing.T) {
	for _, tc := range []struct {
		name           string
		silent, noConv bool
	}{{"no channel", true, false}, {"a channel and no conversation", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			v := newI37Vault(t)
			v.silent, v.noConversation = tc.silent, tc.noConv
			supersededUnit(v)
			event := i37Day(14, 20, 0)
			v.event("late", &event)
			v.fired("older", "late", "dentist", i37Day(13, 20, 0), modern(event, 24*60))

			report := v.check(false)

			if report.RemindersArmed+report.RemindersMoved+report.RemindersCancelled+report.TriggersLapsed != 0 || len(v.armed("u"))+len(v.armed("late")) != 0 || len(v.undelivered()) != 2 || len(v.ch.texts) != 0 {
				t.Errorf("report = %+v (undelivered %v, sent %d); want nothing re-planned, lapsed or sent", report, v.undelivered(), len(v.ch.texts))
			}
		})
	}
}

// TestI37_AStaleRowOfAMissingUnitIsRetired: a unit that is gone cannot be
// planned for, but unlike one that cannot be read it is not coming back, so the
// late row is retired rather than held for ever.
func TestI37_AStaleRowOfAMissingUnitIsRetired(t *testing.T) {
	v := newI37Vault(t)
	v.fired("orphan", "gone", "dentist", i37Day(13, 9, 0), nil)

	report := v.check(false)

	if report.TriggersLapsed != 1 || report.RemindersArmed != 0 || v.undelivered()["orphan"] {
		t.Errorf("report = %+v (undelivered %v); want the orphan lapsed", report, v.undelivered())
	}
}

// TestI37_TheReplacementKeepsTheLevelOfTheRowItReplaces: the reminders the step
// arms take the interrupt level of the late row they stand in for.
func TestI37_TheReplacementKeepsTheLevelOfTheRowItReplaces(t *testing.T) {
	v := newI37Vault(t)
	level := 0.9
	corrected := i37Day(16, 11, 0)
	v.event("u", &corrected)
	v.fired("old", "u", "dentist", i37Day(14, 8, 0), func(tr *ports.Trigger) {
		modern(i37Day(15, 11, 0), 27*60)(tr)
		tr.InterruptLevel = &level
	})

	v.check(false)

	got := v.armed("u")
	if len(got) != 2 || got[0].InterruptLevel == nil || *got[0].InterruptLevel != level || got[1].InterruptLevel == nil || *got[1].InterruptLevel != level {
		t.Errorf("armed = %+v, want both replacements at level %v", got, level)
	}
}

// TestI37_AnOverdueRecurringRowExpiresAndIsNeverReplanned: a recurring armed
// reminder overdue past the window expires as it always has; the re-plan step is
// for one-shot reminders, and a unit carrying a recurring one gets no leads. Its
// series re-arms (I17): this row was armed a week ahead of 21 October, so the
// one row left armed rings on that day itself (ADR-0034).
func TestI37_AnOverdueRecurringRowExpiresAndIsNeverReplanned(t *testing.T) {
	v := newI37Vault(t)
	yearly := prospection.RuleYearly
	v.event("u", i37At(21, 12))
	v.row("b", "u", "Leo's birthday", i37Day(13, 20, 0), func(tr *ports.Trigger) {
		tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
	})

	report := v.check(false)

	armed := v.armed("u")
	if report.TriggersExpired != 1 || report.RemindersArmed != 0 || len(armed) != 1 || armed[0].RecurrenceRule == nil ||
		!armed[0].FireAt.Equal(time.Date(2026, 10, 21, prospection.RecurrenceAnchorHour, 0, 0, 0, armed[0].FireAt.Location())) {
		t.Errorf("report = %+v, armed %+v; want it expired, nothing planned, and its series re-armed for 21 October", report, armed)
	}
}

// TestI37_AReplanKeepsAnEventsOwnLeads: the due scan re-plans an event whose
// leads a correction set (I40, ADR-0038) at those leads, not the preferences'.
func TestI37_AReplanKeepsAnEventsOwnLeads(t *testing.T) {
	v := newI37Vault(t)
	supersededUnit(v)
	if err := v.units.UpdateReminderLeads(context.Background(), "u", []int{60}, i37Now); err != nil {
		t.Fatal(err)
	}

	v.check(false)

	if got := v.armed("u"); len(got) != 1 || !got[0].FireAt.Equal(i37Day(16, 10, 0)) {
		t.Errorf("armed = %+v, want one reminder an hour before 16 October 11:00", got)
	}
}
