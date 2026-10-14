// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// catchUps are the catch-up reminders unitID has, at any status.
func (v *i37Vault) catchUps(unitID string) []ports.DueTrigger {
	v.t.Helper()
	all, err := v.triggers.OneShotForUnit(context.Background(), unitID)
	if err != nil {
		v.t.Fatal(err)
	}
	var out []ports.DueTrigger
	for _, d := range all {
		if d.Payload.CatchUp {
			out = append(out, d)
		}
	}
	return out
}

// owedACatchUp seeds an event an hour away whose every lead has passed, with
// the late reminders of both leads fired and never delivered.
func owedACatchUp(v *i37Vault, leads ...int) time.Time {
	event := i37Day(14, 11, 0)
	v.event("u", &event)
	for i, lead := range leads {
		v.fired("late-"+string(rune('a'+i)), "u", "dentist", event.Add(-time.Duration(lead)*time.Minute), modern(event, lead))
	}
	return event
}

// TestI37_LoneStaleFiredRowYieldsACatchUp (C2): the 24-hour reminder fired a
// day ago and never reached the user, and every lead of the event an hour away
// has passed. The unit gets one reminder at once, marked as a catch-up with its
// attempt number in its id, and the late row lapses.
func TestI37_LoneStaleFiredRowYieldsACatchUp(t *testing.T) {
	v := newI37Vault(t)
	event := owedACatchUp(v, 24*60)

	report := v.check(false)

	got := v.catchUps("u")
	wantID := "catch-up:u:" + event.UTC().Format(time.RFC3339) + ":1"
	if len(got) != 1 || got[0].ID != wantID || !got[0].FireAt.Equal(i37Now) || got[0].Payload.LeadMinutes != 0 || !got[0].Payload.About.Equal(event) {
		t.Fatalf("catch-ups = %+v, want one %q at now about the event", got, wantID)
	}
	if report.RemindersArmed != 1 || report.TriggersLapsed != 1 || v.undelivered()["late-a"] || len(v.rows(ports.ActionCheckReminderArmed)) != 1 {
		t.Errorf("report = %+v (undelivered %v); want one armed, the late row lapsed", report, v.undelivered())
	}
}

// TestI37_TwoLapsedLeadsOnePassOneCatchUp: both leads of the event lapsed
// undelivered in one pass, and the unit gets one catch-up, not one per lead.
func TestI37_TwoLapsedLeadsOnePassOneCatchUp(t *testing.T) {
	v := newI37Vault(t)
	owedACatchUp(v, 48*60, 24*60)

	report := v.check(false)

	if got := v.catchUps("u"); len(got) != 1 || report.RemindersArmed != 1 || report.TriggersLapsed != 2 {
		t.Errorf("catch-ups = %+v, report %+v; want one catch-up for two lapsed leads", got, report)
	}
}

// TestI37_ThreeMorePassesNoSecond: the catch-up is armed, delivered on the next
// pass, and from then on counts as sent. Three more passes arm nothing.
func TestI37_ThreeMorePassesNoSecond(t *testing.T) {
	v := newI37Vault(t)
	owedACatchUp(v, 48*60, 24*60)
	v.check(false)

	for i := 1; i <= 4; i++ {
		v.checkAt(i37Now.Add(time.Duration(i)*5*time.Minute), false)
	}

	if got := v.catchUps("u"); len(got) != 1 || len(v.ch.texts) != 1 {
		t.Errorf("catch-ups = %+v, sent %q; want the one catch-up delivered once", got, v.ch.texts)
	}
}

// TestI37_CatchUpAfterUndeliveredLapseIsAttemptTwo: a catch-up that fired and
// could not be delivered lapses like any late row, and the event, still ahead,
// is owed another: attempt 2. Once one has been delivered there is none.
func TestI37_CatchUpAfterUndeliveredLapseIsAttemptTwo(t *testing.T) {
	event := i37Day(14, 11, 0)
	first := "catch-up:u:" + event.UTC().Format(time.RFC3339) + ":1"
	seed := func(v *i37Vault, deliver bool) {
		v.event("u", &event)
		v.fired(first, "u", "dentist", i37Day(13, 23, 0), func(tr *ports.Trigger) {
			modern(event, 0)(tr)
			tr.Payload.UserLeadsMinutes, tr.Payload.CatchUp = nil, true
		})
		if deliver {
			if err := v.triggers.Surface(context.Background(), first, i37Day(13, 23, 1)); err != nil {
				t.Fatal(err)
			}
		}
	}

	failed := newI37Vault(t)
	seed(failed, false)
	// A catch-up for an earlier instant of the event is attempt 1 of that one.
	failed.row("catch-up:u:"+i37Day(13, 11, 0).UTC().Format(time.RFC3339)+":1", "u", "dentist", i37Day(13, 10, 0), func(tr *ports.Trigger) {
		modern(i37Day(13, 11, 0), 0)(tr)
		tr.Payload.UserLeadsMinutes, tr.Payload.CatchUp = nil, true
	})
	failed.fired("late", "u", "dentist", i37Day(13, 11, 0), modern(event, 24*60))
	report := failed.check(false)
	if got := failed.catchUps("u"); len(got) != 3 || report.RemindersArmed != 1 || failed.undelivered()[first] {
		t.Errorf("after an undelivered catch-up: %+v, report %+v; want attempt 2 armed and attempt 1 lapsed", got, report)
	}
	var second string
	for _, g := range failed.catchUps("u") {
		if g.Payload.About.Equal(event) && g.ID != first {
			second = g.ID
		}
	}
	if second != "catch-up:u:"+event.UTC().Format(time.RFC3339)+":2" {
		t.Errorf("second attempt id = %q", second)
	}

	sent := newI37Vault(t)
	seed(sent, true)
	sent.fired("late", "u", "dentist", i37Day(13, 11, 0), modern(event, 24*60))
	if sent.check(false); len(sent.catchUps("u")) != 1 {
		t.Errorf("after a delivered catch-up: %+v, want no other", sent.catchUps("u"))
	}
}

// TestI37_ViableSiblingBlocksTheCatchUp: another reminder of the event fired ten
// minutes ago and is still being delivered. It counts as sent, so the late row
// lapses and nothing new is armed.
func TestI37_ViableSiblingBlocksTheCatchUp(t *testing.T) {
	v := newI37Vault(t)
	event := owedACatchUp(v, 24*60)
	v.fired("fresh", "u", "dentist", i37Now.Add(-10*time.Minute), modern(event, 50))

	report := v.check(false)

	if got := v.catchUps("u"); len(got) != 0 || report.RemindersArmed != 0 || report.TriggersLapsed != 1 {
		t.Errorf("catch-ups = %+v, report %+v; want none, the late row lapsed", got, report)
	}
}

// TestI37_SupersededInQuietHoursCatchUpAtQuietEnd: the event moved to 07:30 and
// it is 06:00. The catch-up waits for quiet end, 07:00, still before the event.
func TestI37_SupersededInQuietHoursCatchUpAtQuietEnd(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 7, 30)
	v.event("u", &event)
	v.fired("old", "u", "dentist", i37Day(13, 22, 0), modern(i37Day(14, 9, 0), 660))

	report := v.checkAt(i37Day(14, 6, 0), false)

	got := v.catchUps("u")
	if len(got) != 1 || !got[0].FireAt.Equal(i37Day(14, 7, 0)) || got[0].Payload.LeadMinutes != 30 || report.TriggersLapsed != 1 {
		t.Errorf("catch-ups = %+v, report %+v; want one at 07:00, 30 minutes ahead of the event", got, report)
	}
}

// TestI37_QuietEndAfterEventIsRefused: the event is at 06:30 and it is 06:00, so
// quiet end falls after it. Nothing can be sent, and the trail says so rather
// than leaving the late row's retirement unexplained.
func TestI37_QuietEndAfterEventIsRefused(t *testing.T) {
	seed := func() *i37Vault {
		v := newI37Vault(t)
		event := i37Day(14, 6, 30)
		v.event("u", &event)
		v.fired("old", "u", "dentist", i37Day(13, 22, 0), modern(i37Day(14, 9, 0), 660))
		return v
	}
	dry, v := seed(), seed()

	preview := dry.checkAt(i37Day(14, 6, 0), true)
	report := v.checkAt(i37Day(14, 6, 0), false)

	if preview != report || len(dry.rows(ports.ActionCheckReminderRefused)) != 0 {
		t.Errorf("dry run %+v, real %+v; want the same report and no row from the dry run", preview, report)
	}

	refused := v.rows(ports.ActionCheckReminderRefused)
	if len(v.catchUps("u")) != 0 || report.RemindersRefused != 1 || report.TriggersLapsed != 1 || len(refused) != 1 {
		t.Errorf("catch-ups %+v, report %+v, refused rows %+v; want one refusal and the late row lapsed", v.catchUps("u"), report, refused)
	}
}

// TestI37_ACatchUpAnotherPassTookIsNotRecorded: two passes racing for the same
// attempt. The id decides it: the loser's create fails and it writes no row,
// leaving the winner's reminder the only one.
func TestI37_ACatchUpAnotherPassTookIsNotRecorded(t *testing.T) {
	for _, sent := range []bool{false, true} {
		v := newI37Vault(t)
		event := owedACatchUp(v, 24*60)
		taken := "catch-up:u:" + event.UTC().Format(time.RFC3339) + ":1"
		fireAt := i37Day(14, 10, 30)
		if sent {
			fireAt = i37Day(14, 9, 59)
		}
		v.row(taken, "u", "dentist", fireAt, func(tr *ports.Trigger) {
			modern(event, 0)(tr)
			tr.Payload.UserLeadsMinutes, tr.Payload.CatchUp = nil, true
		})
		if sent { // the other pass fired it and delivered it, after this one looked
			if err := v.triggers.Fire(context.Background(), taken, fireAt); err != nil {
				t.Fatal(err)
			}
			if err := v.triggers.Surface(context.Background(), taken, fireAt); err != nil {
				t.Fatal(err)
			}
		}
		v.repo = &hidesCatchUps{TriggerRepo: v.triggers}

		report := v.check(false)

		if report.RemindersArmed != 0 || len(v.rows(ports.ActionCheckReminderArmed)) != 0 || len(v.catchUps("u")) != 1 {
			t.Errorf("sent=%v: report %+v, armed rows %+v; want the lost race to leave no trace", sent, report, v.rows(ports.ActionCheckReminderArmed))
		}
	}
}

// hidesCatchUps reads as a pass would that looked before another created its
// attempt: the first reads of the catch-up it took miss it, later ones do not.
type hidesCatchUps struct {
	ports.TriggerRepo
	oneShot, armed, fired bool // each already answered once
}

func (h *hidesCatchUps) FiredForUnit(ctx context.Context, unitID string) ([]ports.FiredTrigger, error) {
	all, err := h.TriggerRepo.FiredForUnit(ctx, unitID)
	if h.fired {
		return all, err
	}
	h.fired = true
	var out []ports.FiredTrigger
	for _, f := range all {
		if !f.Payload.CatchUp {
			out = append(out, f)
		}
	}
	return out, err
}

func (h *hidesCatchUps) OneShotForUnit(ctx context.Context, unitID string) ([]ports.DueTrigger, error) {
	if h.oneShot {
		return h.TriggerRepo.OneShotForUnit(ctx, unitID)
	}
	h.oneShot = true
	return nil, nil
}

func (h *hidesCatchUps) ArmedForUnit(ctx context.Context, unitID string) ([]ports.DueTrigger, error) {
	all, err := h.TriggerRepo.ArmedForUnit(ctx, unitID)
	if h.armed {
		return all, err
	}
	h.armed = true
	var out []ports.DueTrigger
	for _, t := range all {
		if !t.Payload.CatchUp {
			out = append(out, t)
		}
	}
	return out, err
}

// TestI37_ANightEventWhoseLateReminderLapsesIsRefusedAndSaysSo: the event is at
// 06:00 and it is 02:00. A lead (the 2-hour one, 04:00) is still ahead, but it
// falls in the night and the evening before has passed, so nothing can be sent.
// Not being owed a catch-up (a lead is ahead) must not make that silent: the
// late row (superseded by the move, so judged even in the night) lapses with a
// refusal in the trail.
func TestI37_ANightEventWhoseLateReminderLapsesIsRefusedAndSaysSo(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(15, 6, 0)
	v.event("u", &event)
	v.fired("late", "u", "dentist", i37Day(14, 6, 0), modern(i37Day(14, 9, 0), 180)) // about the date before the move

	report := v.checkAt(i37Day(15, 2, 0), false)

	if len(v.catchUps("u")) != 0 || report.RemindersRefused != 1 || report.TriggersLapsed != 1 || len(v.rows(ports.ActionCheckReminderRefused)) != 1 {
		t.Errorf("report %+v, catch-ups %+v; want the late row lapsed with one refusal", report, v.catchUps("u"))
	}
}

// TestI37_ALiveReminderBlocksTheQuietHoursRefusal: the same night, but the unit
// still has a healthy reminder armed. The plan then speaks through the changes
// it makes to that reminder (a cancellation says why), and no refusal is
// written beside it: a refusal is for a unit left with nothing.
func TestI37_ALiveReminderBlocksTheQuietHoursRefusal(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(15, 6, 0)
	v.event("u", &event)
	v.fired("late", "u", "dentist", i37Day(14, 6, 0), modern(i37Day(14, 9, 0), 180)) // about the date before the move
	v.row("live", "u", "dentist", i37Day(15, 4, 0), modern(event, 120))

	report := v.checkAt(i37Day(15, 2, 0), false)

	if report.RemindersRefused != 0 || len(v.rows(ports.ActionCheckReminderRefused)) != 0 || report.RemindersCancelled != 1 {
		t.Errorf("report %+v; want the live row cancelled and no refusal", report)
	}
}

// TestI37_ACatchUpIdTakenByAMovedRowAdvancesTheAttempt: a catch-up that a
// correction moved off its instant, and a later correction moved back, keeps
// its id but has lost its mark. The attempt number comes from the ids, not the
// mark, so the next attempt is 2 and is created, and a dry run says the same.
func TestI37_ACatchUpIdTakenByAMovedRowAdvancesTheAttempt(t *testing.T) {
	event := i37Day(14, 11, 0)
	seed := func() *i37Vault {
		v := newI37Vault(t)
		v.event("u", &event)
		for _, n := range []string{"1", "2"} { // two attempts, both moved away and back
			v.fired("catch-up:u:"+event.UTC().Format(time.RFC3339)+":"+n, "u", "dentist", i37Day(13, 23, 0), func(tr *ports.Trigger) {
				modern(event, 0)(tr)
				tr.Payload.UserLeadsMinutes = nil // the mark is gone, the id stays
			})
		}
		return v
	}
	dry, v := seed(), seed()

	preview := dry.check(true)
	report := v.check(false)

	var ids []string
	for _, c := range v.catchUps("u") {
		ids = append(ids, c.ID)
	}
	if preview != report || report.RemindersArmed != 1 || len(ids) != 1 || ids[0] != "catch-up:u:"+event.UTC().Format(time.RFC3339)+":3" {
		t.Errorf("dry run %+v, real %+v, catch-ups %v; want attempt 3 created, and the preview to say so", preview, report, ids)
	}
}

// TestI37_ADateOnlyEventOnItsDayGetsACatchUp: a date-only event is stored as
// midnight but is the whole day, and the gate carries its reminder until the
// day ends. The 13th's reminder never reached the user; at 10:00 on the 14th the
// event is owed one catch-up, not a silent lapse.
func TestI37_ADateOnlyEventOnItsDayGetsACatchUp(t *testing.T) {
	v := newI37Vault(t)
	day := i37Day(14, 0, 0)
	v.event("u", &day)
	v.fired("late", "u", "passport", i37Day(13, 9, 0), modern(day, 15*60))

	report := v.check(false)

	if got := v.catchUps("u"); len(got) != 1 || report.RemindersArmed != 1 || report.TriggersLapsed != 1 {
		t.Errorf("catch-ups %+v, report %+v; want one catch-up and the late row lapsed", got, report)
	}
}
