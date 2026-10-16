package prospection

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// checkinDay is an instant on a day of October 2026, in UTC.
func checkinDay(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC)
}

// task is an open check-in about a task still in the pool, put off at snoozed
// (zero when it never was).
func task(surfaced, snoozed time.Time, reasks int) OpenCheckin {
	return OpenCheckin{SurfacedAt: surfaced, SnoozedAt: snoozed, UnitType: unit.TypeTask, UnitStatus: unit.StatusPool, Reasks: reasks}
}

func wantStep(t *testing.T, q QuietHours, c OpenCheckin, at time.Time, want CheckinStep) {
	t.Helper()
	if got := q.CheckinStep(c, at); got != want {
		t.Errorf("%+v at %s: %q, want %q", c, at, got, want)
	}
}

// TestCheckinStep_ASnoozedCheckinIsAskedAtTheNextDigest: a check-in put off
// in the evening is asked again by the next morning's digest, not before it.
func TestCheckinStep_ASnoozedCheckinIsAskedAtTheNextDigest(t *testing.T) {
	c := task(checkinDay(14, 21, 0), checkinDay(14, 21, 30), 0)
	q := DefaultQuietHours()
	wantStep(t, q, c, checkinDay(15, DigestHour-1, 59), CheckinWait)
	wantStep(t, q, c, checkinDay(15, DigestHour, 5), CheckinReask)
}

// TestCheckinStep_AReaskIsAskedAgainWhateverTheAnswer: a re-ask the user put
// off again, or left unanswered, counts toward MaxCheckinReasks, and the
// digest after the last closes it.
func TestCheckinStep_AReaskIsAskedAgainWhateverTheAnswer(t *testing.T) {
	q := DefaultQuietHours()
	for _, snoozed := range []time.Time{{}, checkinDay(15, 9, 0)} {
		first := task(checkinDay(15, DigestHour, 5), snoozed, 1)
		wantStep(t, q, first, checkinDay(15, 20, 0), CheckinWait)
		wantStep(t, q, first, checkinDay(16, DigestHour, 5), CheckinReask)
		last := task(checkinDay(16, DigestHour, 5), snoozed, MaxCheckinReasks)
		wantStep(t, q, last, checkinDay(17, DigestHour, 5), CheckinExpire)
	}
}

// TestCheckinStep_AnUnansweredCheckinExpiresAndIsNeverAsked: a check-in nobody
// answered or put off is not asked again, and closes at the digest a check-in
// put off the evening it was delivered would close at, the third.
func TestCheckinStep_AnUnansweredCheckinExpiresAndIsNeverAsked(t *testing.T) {
	q := DefaultQuietHours()
	evening, putOff := checkinDay(14, 21, 0), checkinDay(14, 21, 30)
	for _, c := range []OpenCheckin{
		task(evening, time.Time{}, 0),
		// A nudge with no unit has no words to ask again: put off, it only
		// gets the same lifetime.
		{SurfacedAt: evening, SnoozedAt: putOff},
		// Nor does an event: by the next morning it is usually past (ADR-0032).
		{SurfacedAt: evening, SnoozedAt: putOff, UnitType: unit.TypeEvent, UnitStatus: unit.StatusPool},
	} {
		for _, at := range []time.Time{checkinDay(15, DigestHour, 5), checkinDay(16, DigestHour, 5), checkinDay(17, DigestHour-1, 0)} {
			wantStep(t, q, c, at, CheckinWait)
		}
		wantStep(t, q, c, checkinDay(17, DigestHour, 5), CheckinExpire)
	}
}

// TestCheckinStep_ASnoozeAfterADigestStillEarnsItsReasks: a check-in left
// unanswered through two digests and then put off is asked at the next one.
func TestCheckinStep_ASnoozeAfterADigestStillEarnsItsReasks(t *testing.T) {
	c := task(checkinDay(14, 21, 0), checkinDay(16, 12, 0), 0)
	wantStep(t, DefaultQuietHours(), c, checkinDay(17, DigestHour, 5), CheckinReask)
}

// TestCheckinStep_TheNextMorningIsTheNextCalendarDay is the client's "el
// resumen de la mañana siguiente": a check-in pushed at 07:00 with the digest
// due at 08:00 is not asked that same morning, nor is one put off before that
// morning's digest; both wait for the next day's. The lifetime counts the same
// mornings.
func TestCheckinStep_TheNextMorningIsTheNextCalendarDay(t *testing.T) {
	q := QuietHours{End: TimeOfDay{Hour: 8}}
	pushed := checkinDay(14, 7, 0)
	c := task(pushed, checkinDay(14, 7, 10), 0)
	wantStep(t, q, c, checkinDay(14, 8, 5), CheckinWait)
	wantStep(t, q, c, checkinDay(15, 8, 5), CheckinReask)

	unanswered := task(pushed, time.Time{}, 0)
	wantStep(t, q, unanswered, checkinDay(16, 8, 5), CheckinWait)
	wantStep(t, q, unanswered, checkinDay(17, 8, 5), CheckinExpire)

	// Delivered the evening before and put off this morning, before today's
	// digest: the snooze is today's, so the first re-ask is tomorrow's.
	late := task(checkinDay(13, 21, 0), checkinDay(14, 6, 30), 0)
	wantStep(t, q, late, checkinDay(14, 8, 5), CheckinWait)
	wantStep(t, q, late, checkinDay(15, 8, 5), CheckinReask)
}

// TestCheckinStep_EveryReaskRechecksTheUnit: a check-in asked again is asked
// again only while its unit is still something to do. Corrected to an event,
// or archived, after the first re-ask, it gets no more, only the lifetime
// from that re-ask.
func TestCheckinStep_EveryReaskRechecksTheUnit(t *testing.T) {
	q := DefaultQuietHours()
	reasked := checkinDay(15, DigestHour, 5)
	for _, c := range []OpenCheckin{
		{SurfacedAt: reasked, UnitType: unit.TypeEvent, UnitStatus: unit.StatusPool, Reasks: 1},
		{SurfacedAt: reasked, UnitType: unit.TypeTask, UnitStatus: unit.StatusArchived, Reasks: 1},
	} {
		wantStep(t, q, c, checkinDay(16, DigestHour, 5), CheckinWait)
		wantStep(t, q, c, checkinDay(18, DigestHour, 5), CheckinExpire)
	}
}

// TestCheckinStep_OnlyPendingWorkIsAskedAgain sweeps every unit kind: a check-in
// put off is asked again only when its unit is still something to do. Any
// other kind, an event included, only gets the lifetime (ADR-0032: a reminder
// never speaks of the past). A kind added later fails here until it is decided.
func TestCheckinStep_OnlyPendingWorkIsAskedAgain(t *testing.T) {
	want := map[unit.Type]CheckinStep{
		unit.TypeTask: CheckinReask, unit.TypeMentalLoad: CheckinReask,
		unit.TypeEvent: CheckinWait, unit.TypeKnowledge: CheckinWait, unit.TypeProcedural: CheckinWait,
		unit.TypeEmotional: CheckinWait, unit.TypeList: CheckinWait, unit.TypeStructuredRef: CheckinWait,
		unit.TypeInsight: CheckinWait,
	}
	for _, kind := range unit.AllTypes() {
		expected, known := want[kind]
		if !known {
			t.Errorf("unit kind %q has no re-ask decision", kind)
			continue
		}
		c := OpenCheckin{SurfacedAt: checkinDay(14, 21, 0), SnoozedAt: checkinDay(14, 21, 30), UnitType: kind, UnitStatus: unit.StatusPool}
		wantStep(t, DefaultQuietHours(), c, checkinDay(15, DigestHour, 5), expected)
	}
}

// TestCheckinStep_OnlyAnActiveUnitIsAskedAgain sweeps every unit status: an
// archived or superseded task is no longer something to ask about.
func TestCheckinStep_OnlyAnActiveUnitIsAskedAgain(t *testing.T) {
	want := map[unit.Status]CheckinStep{
		unit.StatusPool: CheckinReask, unit.StatusIncomplete: CheckinReask,
		unit.StatusArchived: CheckinWait, unit.StatusSuperseded: CheckinWait,
	}
	for _, status := range unit.AllStatuses() {
		expected, known := want[status]
		if !known {
			t.Errorf("unit status %q has no re-ask decision", status)
			continue
		}
		c := OpenCheckin{SurfacedAt: checkinDay(14, 21, 0), SnoozedAt: checkinDay(14, 21, 30), UnitType: unit.TypeTask, UnitStatus: status}
		wantStep(t, DefaultQuietHours(), c, checkinDay(15, DigestHour, 5), expected)
	}
}

// TestCheckinStep_TheDigestThatDeliveredItDoesNotCount: the digest of the day
// the check-in reached the user is not a morning after it.
func TestCheckinStep_TheDigestThatDeliveredItDoesNotCount(t *testing.T) {
	c := task(checkinDay(15, DigestHour, 0), checkinDay(15, 9, 0), 0)
	wantStep(t, DefaultQuietHours(), c, checkinDay(15, 23, 0), CheckinWait)
}

// TestCheckinStep_FollowsTheUsersDigestTime: the digest a check-in waits for
// is the user's, due at their later quiet end.
func TestCheckinStep_FollowsTheUsersDigestTime(t *testing.T) {
	q := QuietHours{End: TimeOfDay{Hour: 8, Minute: 30}}
	c := task(checkinDay(14, 21, 0), checkinDay(14, 21, 30), 0)
	wantStep(t, q, c, checkinDay(15, 8, 0), CheckinWait)
	wantStep(t, q, c, checkinDay(15, 8, 35), CheckinReask)
}

// TestCheckinStep_TheMorningIsNowsZone: surfaced_at is read back in UTC, and
// the day it is compared with is the user's, in now's zone. 23:30 UTC on the
// 15th is 20:30 on the 15th in Buenos Aires, so the 16th's digest is the next
// morning; read in UTC it would be the same day's.
func TestCheckinStep_TheMorningIsNowsZone(t *testing.T) {
	ba := time.FixedZone("-03", -3*60*60)
	c := task(time.Date(2026, 10, 16, 2, 30, 0, 0, time.UTC), time.Date(2026, 10, 16, 2, 40, 0, 0, time.UTC), 0)
	wantStep(t, DefaultQuietHours(), c, time.Date(2026, 10, 16, 7, 5, 0, 0, ba), CheckinReask)
}

// TestCheckinStep_AcrossDSTAndMonthEnds: the mornings are calendar days on the
// local wall clock, through a spring-forward and a fall-back night and across
// the end of a month and of a year.
func TestCheckinStep_AcrossDSTAndMonthEnds(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	santiago, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	q := DefaultQuietHours()
	for name, evening := range map[string]time.Time{
		"Madrid spring forward":  time.Date(2026, 3, 28, 21, 0, 0, 0, madrid),
		"Madrid fall back":       time.Date(2026, 10, 24, 21, 0, 0, 0, madrid),
		"Santiago spring":        time.Date(2026, 9, 5, 21, 0, 0, 0, santiago),
		"the end of October":     time.Date(2026, 10, 31, 21, 0, 0, 0, time.UTC),
		"the end of the year":    time.Date(2026, 12, 31, 21, 0, 0, 0, madrid),
		"the end of February":    time.Date(2027, 2, 28, 21, 0, 0, 0, santiago),
		"a leap year's February": time.Date(2028, 2, 28, 21, 0, 0, 0, madrid),
	} {
		loc := evening.Location()
		morning := func(n int) time.Time {
			y, m, d := evening.Date()
			return time.Date(y, m, d+n, DigestHour, 5, 0, 0, loc)
		}
		snoozed := task(evening.UTC(), evening.Add(30*time.Minute).UTC(), 0)
		if got := q.CheckinStep(snoozed, morning(1)); got != CheckinReask {
			t.Errorf("%s: put off, at %s: %q, want %q", name, morning(1), got, CheckinReask)
		}
		unanswered := task(evening.UTC(), time.Time{}, 0)
		for n, want := range map[int]CheckinStep{1: CheckinWait, 2: CheckinWait, 3: CheckinExpire} {
			if got := q.CheckinStep(unanswered, morning(n)); got != want {
				t.Errorf("%s: unanswered, at %s: %q, want %q", name, morning(n), got, want)
			}
		}
	}
}

// TestMaxReasksPerDigestIsAShortList: the block a digest asks again is a short
// list after the day's items, never longer than a low-energy digest's whole
// carry, and asks at least one.
func TestMaxReasksPerDigestIsAShortList(t *testing.T) {
	if MaxReasksPerDigest < 1 || MaxReasksPerDigest > LowEnergyDigestSize {
		t.Errorf("MaxReasksPerDigest = %d, want 1 to LowEnergyDigestSize (%d)", MaxReasksPerDigest, LowEnergyDigestSize)
	}
}

// TestReaskDue is the timing half of a re-ask, its unit aside: due the morning
// after a snooze or after an earlier re-ask, under the cap; not for a check-in
// never put off, not the morning of the snooze, not past the cap.
func TestReaskDue(t *testing.T) {
	q := DefaultQuietHours()
	morning := checkinDay(15, DigestHour, 5)
	for name, tc := range map[string]struct {
		c    OpenCheckin
		want bool
	}{
		"put off the evening before": {OpenCheckin{SurfacedAt: checkinDay(14, 21, 0), SnoozedAt: checkinDay(14, 21, 30)}, true},
		"asked again yesterday":      {OpenCheckin{SurfacedAt: checkinDay(14, DigestHour, 5), Reasks: 1}, true},
		"never put off":              {OpenCheckin{SurfacedAt: checkinDay(14, 21, 0)}, false},
		"put off this morning":       {OpenCheckin{SurfacedAt: checkinDay(14, 21, 0), SnoozedAt: checkinDay(15, 6, 30)}, false},
		"delivered this morning":     {OpenCheckin{SurfacedAt: checkinDay(15, 6, 0), SnoozedAt: checkinDay(15, 6, 30)}, false},
		"at the cap":                 {OpenCheckin{SurfacedAt: checkinDay(14, DigestHour, 5), Reasks: MaxCheckinReasks}, false},
	} {
		if got := q.ReaskDue(tc.c, morning); got != tc.want {
			t.Errorf("%s: ReaskDue = %v, want %v", name, got, tc.want)
		}
	}
}
