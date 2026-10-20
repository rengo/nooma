package brain

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// seedUnit stores u, live, captured a day before the digest.
func seedUnit(t *testing.T, units ports.UnitRepo, u unit.Unit) {
	t.Helper()
	u.Status, u.Source = unit.StatusPool, "chat"
	u.LastTouchedAt, u.CreatedAt, u.UpdatedAt = digestNow.AddDate(0, 0, -1), digestNow.AddDate(0, 0, -1), digestNow.AddDate(0, 0, -1)
	if err := units.Create(context.Background(), u); err != nil {
		t.Fatalf("seed %s: %v", u.ID, err)
	}
}

// dueToday is a task due on digestNow's date, a bare date.
func dueToday(id, title string) unit.Unit {
	due := time.Date(digestNow.Year(), digestNow.Month(), digestNow.Day(), 0, 0, 0, 0, time.UTC)
	return unit.Unit{ID: id, Type: unit.TypeTask, Content: title + ".", Title: title, Weight: 1, DueAt: &due}
}

// TestDigest_LowEnergyNeverHoldsADueTask: the care gate truncates the day's
// reminders to LowEnergyDigestSize, and a task due today is named beside them,
// never one of those slots and never held. A due date is the one thing a
// depleted morning must still hear about; it is a line, not a reminder.
func TestDigest_LowEnergyNeverHoldsADueTask(t *testing.T) {
	units := memrepo.NewUnits()
	pending := make([]ports.DueTrigger, 0, 6)
	for i := 0; i < 6; i++ {
		id := string(rune('a' + i))
		seedUnit(t, units, unit.Unit{ID: "u-" + id, Type: unit.TypeEvent, Content: "item " + id, Weight: float64(6-i) / 6})
		pending = append(pending, digestTrigger("trg-"+id, "u-"+id, "item "+id))
	}
	// The lowest weight of all: a ranked candidate would be held first.
	task := dueToday("u-passport", "Renew the passport")
	task.Weight = 0.01
	seedUnit(t, units, task)

	state := memrepo.NewState()
	state.RecordEnergy(prospection.EnergyReading{Level: prospection.LowEnergyMax - 0.1, RecordedAt: digestNow.Add(-time.Hour)})
	triggers := &undeliveredTriggers{pending: pending}
	ch := &sendingChannel{}

	carried, _, err := digestRunner(t, triggers, units, state, memrepo.NewDecisionLog(), ch).assembleDigest(context.Background(), digestNow, true)
	if err != nil {
		t.Fatal(err)
	}
	if carried != prospection.LowEnergyDigestSize || len(triggers.surfaced) != carried {
		t.Fatalf("carried %d, surfaced %d; want LowEnergyDigestSize (%d) reminders, the task taking none of the slots",
			carried, len(triggers.surfaced), prospection.LowEnergyDigestSize)
	}
	if ch.count() != 1 {
		t.Fatalf("sent %d digests, want 1", ch.count())
	}
	lines := strings.Split(ch.sent[0], "\n")
	if want := prospection.LowEnergyDigestSize + 2; len(lines) != want || lines[1] != "• Renew the passport — Wed 5 Aug" {
		t.Errorf("digest:\n%s\nwant a header, the task first, then the %d reminders", ch.sent[0], prospection.LowEnergyDigestSize)
	}
}

// TestDigest_ADueTaskAndItsUnitsReminderAreOneLine: a unit's reminders are one
// line (ADR-0032 point 4), and a due task is its unit's line too, so a legacy
// reminder of the same unit joins it and is marked delivered with it.
func TestDigest_ADueTaskAndItsUnitsReminderAreOneLine(t *testing.T) {
	units := memrepo.NewUnits()
	seedUnit(t, units, dueToday("u-passport", "Renew the passport"))
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{digestTrigger("trg-1", "u-passport", "renew the passport")}}
	ch := &sendingChannel{}

	if _, _, err := digestRunner(t, triggers, units, memrepo.NewState(), memrepo.NewDecisionLog(), ch).
		assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatal(err)
	}
	want := "Here is 1 thing for today:\n• Renew the passport — Wed 5 Aug"
	if ch.count() != 1 || ch.sent[0] != want {
		t.Errorf("sent %q, want exactly [%q]", ch.sent, want)
	}
	if !reflect.DeepEqual(triggers.surfaced, []string{"trg-1"}) {
		t.Errorf("surfaced %v, want the reminder that shares the task's line", triggers.surfaced)
	}
}

// TestDigest_TheTurnNamesTheDueTasks: the digest is a turn about every unit it
// named (ADR-0036), a due task included, so "ya lo hice" after it is read
// beside the task.
func TestDigest_TheTurnNamesTheDueTasks(t *testing.T) {
	units := memrepo.NewUnits()
	seedUnit(t, units, dueToday("u-passport", "Renew the passport"))
	seedUnit(t, units, unit.Unit{ID: "u-1", Type: unit.TypeEvent, Content: "Ana's birthday", Weight: 1})
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{digestTrigger("trg-1", "u-1", "Ana's birthday")}}
	r := digestRunner(t, triggers, units, memrepo.NewState(), memrepo.NewDecisionLog(), &sendingChannel{})
	r.conv = NewConversation(memrepo.NewConversations())

	if _, _, err := r.assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatal(err)
	}
	got := r.conv.Recent(context.Background(), testConversation)
	if len(got) != 1 || !reflect.DeepEqual(got[0].UnitIDs, []string{"u-1", "u-passport"}) {
		t.Errorf("turns = %+v, want the digest about u-1 and u-passport", got)
	}
}

// TestDigestMessage_DropsADueTaskLineOnlyAfterTheReminders: lines past the
// message bound go from the end, and the due tasks lead, so they are the last
// thing a digest that does not fit gives up; the ones kept are named.
func TestDigestMessage_DropsADueTaskLineOnlyAfterTheReminders(t *testing.T) {
	long := strings.Repeat("x", 3000)
	tasks := []dueLine{{unitID: "u-task", text: "Renew the passport"}}
	pending := []ports.DueTrigger{digestTrigger("trg-1", "u-1", long), digestTrigger("trg-2", "u-2", long)}
	carry := []prospection.DigestItem{{ID: "trg-1"}, {ID: "trg-2"}}

	text, sent, _, named := digestMessageWith(sentences(), tasks, carry, pending, nil, nil)
	if utf16Len(text) > digestMessageUnits || !strings.Contains(text, "Renew the passport") {
		t.Errorf("digest of %d units:\n%.200s", utf16Len(text), text)
	}
	if len(sent) != 1 || sent[0].ID != "trg-1" || !reflect.DeepEqual(named, []string{"u-task"}) {
		t.Errorf("sent %+v, named %v; want the first reminder and the task kept", sent, named)
	}
}
