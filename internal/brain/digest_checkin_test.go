package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// checkinEvening is when the fixtures' check-ins reached the user; the
// mornings after it are checkinMorning(1), (2), ...
var checkinEvening = time.Date(2026, 10, 14, 21, 0, 0, 0, time.UTC)

func checkinMorning(n int) time.Time {
	return time.Date(2026, 10, 14+n, prospection.DigestHour, 5, 0, 0, time.UTC)
}

// checkinVault is a vault with one delivered check-in about a task.
type checkinVault struct {
	triggers *memrepo.Triggers
	units    ports.UnitRepo
	log      ports.DecisionLog
	channel  *recordingChannel
	ids      *countingIDs
}

func newCheckinVault(t *testing.T, snoozed bool) *checkinVault {
	t.Helper()
	ctx := context.Background()
	units := memrepo.NewUnits()
	if err := units.Create(ctx, unit.Unit{ID: "u-coffee", Type: unit.TypeTask, Status: unit.StatusPool, Content: "buy coffee",
		Source: "chat", Weight: 1, LastTouchedAt: checkinEvening, CreatedAt: checkinEvening, UpdatedAt: checkinEvening}); err != nil {
		t.Fatal(err)
	}
	triggers := memrepo.NewTriggers()
	unitID, at := "u-coffee", checkinEvening
	if err := triggers.Create(ctx, ports.Trigger{ID: "t-coffee", UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &at, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := triggers.Fire(ctx, "t-coffee", at); err != nil {
		t.Fatal(err)
	}
	if err := triggers.Surface(ctx, "t-coffee", at); err != nil {
		t.Fatal(err)
	}
	log := memrepo.NewDecisionLog()
	if snoozed {
		if err := log.Record(ctx, ports.Decision{ID: "d-snooze", Action: ports.ActionCaptureCheckInSnoozed,
			Context: []byte(`{"trigger_id":"t-coffee","open_check_ins":1}`), OccurredAt: at.Add(30 * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	return &checkinVault{triggers: triggers, units: units, log: log, channel: &recordingChannel{}, ids: &countingIDs{}}
}

func (v *checkinVault) digest(t *testing.T, at time.Time, commit bool) error {
	t.Helper()
	r := checkRunner{triggers: v.triggers, timers: &emptyTimers{}, ids: v.ids, log: v.log, channel: v.channel,
		units: v.units, state: memrepo.NewState(), conversation: testConversation}
	_, _, err := r.assembleDigest(context.Background(), at, commit)
	return err
}

func (v *checkinVault) open(t *testing.T) []ports.OpenCheckIn {
	t.Helper()
	open, err := v.triggers.Delivered(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return open
}

// TestDigest_ADryRunAsksAndClosesNothing: `nooma check` without --commit
// leaves an expirable check-in open and an askable one unasked, and writes no
// row about either.
func TestDigest_ADryRunAsksAndClosesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		snoozed bool
		at      time.Time
	}{
		"expirable": {false, checkinMorning(prospection.MaxCheckinReasks + 1)},
		"askable":   {true, checkinMorning(1)},
	} {
		v := newCheckinVault(t, tc.snoozed)
		if err := v.digest(t, tc.at, false); err != nil {
			t.Fatalf("%s: dry run: %v", name, err)
		}
		open := v.open(t)
		if len(open) != 1 || !open[0].SurfacedAt.Equal(checkinEvening) || open[0].Payload.Reasks != 0 {
			t.Errorf("%s: after a dry run, open %+v, want the check-in untouched", name, open)
		}
		rows, err := v.log.Since(context.Background(), checkinEvening.Add(time.Hour), -1)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range rows {
			if d.Action == ports.ActionCheckCheckinExpired || d.Action == ports.ActionCheckCheckinReasked {
				t.Errorf("%s: a dry run wrote %q", name, d.Action)
			}
		}
		if len(v.channel.sent) != 0 {
			t.Errorf("%s: a dry run sent %q", name, v.channel.sent)
		}
	}
}

// failingReaskLog fails to record the re-ask row, as a full disk would.
type failingReaskLog struct{ *memrepo.DecisionLog }

func (l failingReaskLog) Record(ctx context.Context, d ports.Decision) error {
	if d.Action == ports.ActionCheckCheckinReasked {
		return errors.New("disk full")
	}
	return l.DecisionLog.Record(ctx, d)
}

// TestDigest_AReaskCountsEvenWhenItsRowFails: the count is written with the
// restamp, so a re-ask whose audit row failed still counts, and the check-in
// is asked MaxCheckinReasks times in all, never once more.
func TestDigest_AReaskCountsEvenWhenItsRowFails(t *testing.T) {
	v := newCheckinVault(t, true)
	healthy := v.log
	v.log = failingReaskLog{healthy.(*memrepo.DecisionLog)}
	if err := v.digest(t, checkinMorning(1), true); err == nil {
		t.Fatal("a failed re-ask row was not reported")
	}
	if open := v.open(t); len(open) != 1 || open[0].Payload.Reasks != 1 {
		t.Fatalf("after the first re-ask, open %+v, want one re-ask counted", open)
	}
	v.log = healthy
	for n := 2; n <= prospection.MaxCheckinReasks+1; n++ {
		if err := v.digest(t, checkinMorning(n), true); err != nil {
			t.Fatalf("morning %d: %v", n, err)
		}
	}
	if len(v.channel.sent) != prospection.MaxCheckinReasks {
		t.Errorf("%d digests sent, want %d: one re-ask per morning, never an extra one", len(v.channel.sent), prospection.MaxCheckinReasks)
	}
	if open := v.open(t); len(open) != 0 {
		t.Errorf("open %+v, want it closed the morning after the last re-ask", open)
	}
}

// unreadableUnits fails every read, as a locked or corrupt vault would.
type unreadableUnits struct{ ports.UnitRepo }

func (unreadableUnits) ByID(context.Context, string) (unit.Unit, error) {
	return unit.Unit{}, errors.New("database is locked")
}

// TestDigest_AnUnreadableUnitSkipsOnlyTheReask: a unit that cannot be read
// this morning, for any reason but its absence, skips that morning's re-ask,
// which needs its words, and says so in decision_log; the lifetime does not
// need the unit, so the check-in still closes on schedule and an error that
// never clears cannot keep it open.
func TestDigest_AnUnreadableUnitSkipsOnlyTheReask(t *testing.T) {
	ctx := context.Background()
	v := newCheckinVault(t, true)
	v.units = unreadableUnits{v.units}
	if err := v.digest(t, checkinMorning(1), true); err != nil {
		t.Fatal(err)
	}
	if open := v.open(t); len(open) != 1 || open[0].Payload.Reasks != 0 || len(v.channel.sent) != 0 {
		t.Errorf("open %+v, sent %q: want it neither asked nor closed this morning", open, v.channel.sent)
	}
	rows, err := v.log.Since(ctx, checkinEvening.Add(time.Hour), -1)
	if err != nil {
		t.Fatal(err)
	}
	skipped := 0
	for _, d := range rows {
		if d.Action == ports.ActionCheckCheckinReaskSkipped && strings.Contains(d.Rationale, "t-coffee") {
			skipped++
		}
	}
	if skipped != 1 {
		t.Errorf("%d check.checkin.reask_skipped rows about t-coffee, want 1", skipped)
	}

	if err := v.digest(t, checkinMorning(prospection.MaxCheckinReasks+1), true); err != nil {
		t.Fatal(err)
	}
	if open := v.open(t); len(open) != 0 {
		t.Errorf("open %+v, want it closed on schedule though its unit cannot be read", open)
	}
}

// zeroSurfaced reads an open check-in back with no surfaced_at.
type zeroSurfaced struct{ *memrepo.Triggers }

func (r zeroSurfaced) Delivered(ctx context.Context) ([]ports.OpenCheckIn, error) {
	open, err := r.Triggers.Delivered(ctx)
	for i := range open {
		open[i].SurfacedAt = time.Time{}
	}
	return open, err
}

// TestDigest_ANoTimeCheckinIsNeverDecided: a zero surfaced_at is no instant,
// and counting mornings from year 1 would close the check-in at once.
func TestDigest_ANoTimeCheckinIsNeverDecided(t *testing.T) {
	v := newCheckinVault(t, true)
	r := checkRunner{triggers: zeroSurfaced{v.triggers}, timers: &emptyTimers{}, ids: &countingIDs{}, log: v.log,
		channel: v.channel, units: v.units, state: memrepo.NewState(), conversation: testConversation}
	if _, _, err := r.assembleDigest(context.Background(), checkinMorning(1), true); err != nil {
		t.Fatal(err)
	}
	if open := v.open(t); len(open) != 1 || open[0].Payload.Reasks != 0 {
		t.Errorf("open %+v, want the check-in left as it was", open)
	}
}

// TestDigestHistoryCoversTheCheckinLifetime: the snooze and the mornings it
// is counted against must fit in the history the digest reads.
func TestDigestHistoryCoversTheCheckinLifetime(t *testing.T) {
	if digestHistoryDays <= prospection.MaxCheckinReasks+1 {
		t.Errorf("digestHistoryDays = %d, want more than MaxCheckinReasks + 1 = %d", digestHistoryDays, prospection.MaxCheckinReasks+1)
	}
}

// addSnoozedTask adds one more delivered, snoozed task check-in to v.
func (v *checkinVault) addSnoozedTask(t *testing.T, id string, surfaced time.Time) {
	t.Helper()
	ctx := context.Background()
	unitID := "u-" + id
	if err := v.units.Create(ctx, unit.Unit{ID: unitID, Type: unit.TypeTask, Status: unit.StatusPool, Content: "task " + id,
		Source: "chat", Weight: 1, LastTouchedAt: surfaced, CreatedAt: surfaced, UpdatedAt: surfaced}); err != nil {
		t.Fatal(err)
	}
	if err := v.triggers.Create(ctx, ports.Trigger{ID: id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &surfaced, CreatedAt: surfaced}); err != nil {
		t.Fatal(err)
	}
	if err := v.triggers.Fire(ctx, id, surfaced); err != nil {
		t.Fatal(err)
	}
	if err := v.triggers.Surface(ctx, id, surfaced); err != nil {
		t.Fatal(err)
	}
	if err := v.log.Record(ctx, ports.Decision{ID: "d-snooze-" + id, Action: ports.ActionCaptureCheckInSnoozed,
		Context: []byte(`{"trigger_id":"` + id + `","open_check_ins":1}`), OccurredAt: surfaced.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
}

// TestDigest_AtMostMaxReasksPerDigestOldestFirst: a morning asks at most
// MaxReasksPerDigest check-ins again, the longest waiting first; the rest wait
// for a later morning, uncounted.
func TestDigest_AtMostMaxReasksPerDigestOldestFirst(t *testing.T) {
	v := newCheckinVault(t, true)
	extra := prospection.MaxReasksPerDigest + 1
	for i := 0; i < extra; i++ {
		v.addSnoozedTask(t, fmt.Sprintf("t-later-%d", i), checkinEvening.Add(time.Duration(i+1)*time.Minute))
	}
	if err := v.digest(t, checkinMorning(1), true); err != nil {
		t.Fatal(err)
	}
	asked := map[string]bool{}
	for _, c := range v.open(t) {
		if c.Payload.Reasks == 1 {
			asked[c.ID] = true
		}
	}
	if len(asked) != prospection.MaxReasksPerDigest || !asked["t-coffee"] {
		t.Errorf("asked %v, want %d, the oldest (t-coffee) among them", asked, prospection.MaxReasksPerDigest)
	}
}

// TestDigest_ReasksAreNotCountedAsCarriedReminders: check.digest.sent, the
// carried count and the dry run's count are the day's reminders; the re-asks
// are counted apart.
func TestDigest_ReasksAreNotCountedAsCarriedReminders(t *testing.T) {
	v := newCheckinVault(t, true)
	ctx := context.Background()
	yearly := prospection.RuleYearly
	fire := checkinMorning(1).Add(-time.Hour)
	if err := v.triggers.Create(ctx, ports.Trigger{ID: "t-birthday", Kind: ports.TriggerKindTimeBased, FireAt: &fire, CreatedAt: fire,
		Payload:        ports.TriggerPayload{ActionText: "Ana's birthday"},
		RecurrenceRule: &yearly, RecurrenceAnchor: &prospection.Anchor{Month: time.October, Day: 20}}); err != nil {
		t.Fatal(err)
	}
	if err := v.triggers.Fire(ctx, "t-birthday", fire); err != nil {
		t.Fatal(err)
	}
	r := checkRunner{triggers: v.triggers, timers: &emptyTimers{}, ids: v.ids, log: v.log, channel: v.channel,
		units: v.units, state: memrepo.NewState(), conversation: testConversation}

	if dry, _, err := r.assembleDigest(ctx, checkinMorning(1), false); err != nil || dry != 1 {
		t.Errorf("dry run carried %d (%v), want the one reminder", dry, err)
	}
	carried, _, err := r.assembleDigest(ctx, checkinMorning(1), true)
	if err != nil || carried != 1 {
		t.Fatalf("carried %d (%v), want the one reminder", carried, err)
	}
	rows, err := v.log.Since(ctx, checkinMorning(1).Add(-time.Minute), -1)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range rows {
		if d.Action == ports.ActionCheckDigestSent && !strings.Contains(d.Rationale, "carried 1 item(s)") || d.Action == ports.ActionCheckDigestSent && !strings.Contains(d.Rationale, "asked 1 check-in(s) again") {
			t.Errorf("check.digest.sent says %q, want 1 carried and 1 asked again, apart", d.Rationale)
		}
	}
}

// TestDigest_ABareAnswerReachesTheLastItemShown: the digest stamps what it
// shows in the order shown, so the newest open check-in, which a bare answer
// reaches, is its last line: here the re-ask after the day's new item, whose
// id would win a tie.
func TestDigest_ABareAnswerReachesTheLastItemShown(t *testing.T) {
	v := newCheckinVault(t, true)
	ctx := context.Background()
	yearly := prospection.RuleYearly
	fire := checkinMorning(1).Add(-time.Hour)
	if err := v.triggers.Create(ctx, ports.Trigger{ID: "zz-birthday", Kind: ports.TriggerKindTimeBased, FireAt: &fire, CreatedAt: fire,
		Payload:        ports.TriggerPayload{ActionText: "Ana's birthday"},
		RecurrenceRule: &yearly, RecurrenceAnchor: &prospection.Anchor{Month: time.October, Day: 20}}); err != nil {
		t.Fatal(err)
	}
	if err := v.triggers.Fire(ctx, "zz-birthday", fire); err != nil {
		t.Fatal(err)
	}
	if err := v.digest(t, checkinMorning(1), true); err != nil {
		t.Fatal(err)
	}
	if open := v.open(t); len(open) != 2 || open[0].ID != "t-coffee" {
		t.Errorf("open %+v, want t-coffee, the last line shown, first", open)
	}
}
