package prospection

import (
	"testing"
	"time"
)

// checkinDay is the evening a check-in reached the user; the digests that
// follow it are at DigestHour on the next mornings.
func checkinDay(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC)
}

// TestCheckinStep_ASnoozedCheckinIsAskedAtTheNextDigest: a check-in put off
// in the evening is asked again by the next morning's digest, not before it.
func TestCheckinStep_ASnoozedCheckinIsAskedAtTheNextDigest(t *testing.T) {
	c := OpenCheckin{SurfacedAt: checkinDay(14, 21, 0), HasUnit: true, Snoozed: true}
	q := DefaultQuietHours()
	if got := q.CheckinStep(c, checkinDay(15, DigestHour-1, 59)); got != CheckinWait {
		t.Errorf("before the digest: %q, want %q", got, CheckinWait)
	}
	if got := q.CheckinStep(c, checkinDay(15, DigestHour, 5)); got != CheckinReask {
		t.Errorf("at the next digest: %q, want %q", got, CheckinReask)
	}
}

// TestCheckinStep_AReaskIsAskedAgainWhateverTheAnswer: a re-ask the user put
// off again, or left unanswered, counts toward MaxCheckinReasks, and the
// digest after the last one closes the check-in.
func TestCheckinStep_AReaskIsAskedAgainWhateverTheAnswer(t *testing.T) {
	q := DefaultQuietHours()
	for _, snoozed := range []bool{true, false} {
		first := OpenCheckin{SurfacedAt: checkinDay(15, DigestHour, 5), HasUnit: true, Snoozed: snoozed, Reasks: 1}
		if got := q.CheckinStep(first, checkinDay(15, 20, 0)); got != CheckinWait {
			t.Errorf("snoozed=%v, the day of the first re-ask: %q, want %q", snoozed, got, CheckinWait)
		}
		if got := q.CheckinStep(first, checkinDay(16, DigestHour, 5)); got != CheckinReask {
			t.Errorf("snoozed=%v, after one re-ask: %q, want %q", snoozed, got, CheckinReask)
		}
		last := OpenCheckin{SurfacedAt: checkinDay(16, DigestHour, 5), HasUnit: true, Snoozed: snoozed, Reasks: MaxCheckinReasks}
		if got := q.CheckinStep(last, checkinDay(17, DigestHour, 5)); got != CheckinExpire {
			t.Errorf("snoozed=%v, after %d re-asks: %q, want %q", snoozed, MaxCheckinReasks, got, CheckinExpire)
		}
	}
}

// TestCheckinStep_AnUnansweredCheckinExpiresAndIsNeverAsked: a check-in nobody
// answered or put off is not asked again, and closes at the digest a check-in
// put off the evening it was delivered would close at, the third.
func TestCheckinStep_AnUnansweredCheckinExpiresAndIsNeverAsked(t *testing.T) {
	q := DefaultQuietHours()
	for _, c := range []OpenCheckin{
		{SurfacedAt: checkinDay(14, 21, 0), HasUnit: true},
		// A nudge with no unit has no words to ask again: put off, it only
		// gets the same lifetime.
		{SurfacedAt: checkinDay(14, 21, 0), Snoozed: true},
	} {
		for _, at := range []time.Time{checkinDay(15, DigestHour, 5), checkinDay(16, DigestHour, 5), checkinDay(17, DigestHour-1, 0)} {
			if got := q.CheckinStep(c, at); got != CheckinWait {
				t.Errorf("%+v at %s: %q, want %q", c, at, got, CheckinWait)
			}
		}
		if got := q.CheckinStep(c, checkinDay(17, DigestHour, 5)); got != CheckinExpire {
			t.Errorf("%+v at the third digest: %q, want %q", c, got, CheckinExpire)
		}
	}
}

// TestCheckinStep_ASnoozeAfterADigestStillEarnsItsReasks: a check-in left
// unanswered through one digest and then put off is asked at the next one.
func TestCheckinStep_ASnoozeAfterADigestStillEarnsItsReasks(t *testing.T) {
	c := OpenCheckin{SurfacedAt: checkinDay(14, 21, 0), HasUnit: true, Snoozed: true}
	if got := DefaultQuietHours().CheckinStep(c, checkinDay(17, DigestHour, 5)); got != CheckinReask {
		t.Errorf("put off before the third digest: %q, want %q", got, CheckinReask)
	}
}

// TestCheckinStep_TheDigestThatDeliveredItDoesNotCount: a digest at or before
// the instant the check-in reached the user is not a digest after it.
func TestCheckinStep_TheDigestThatDeliveredItDoesNotCount(t *testing.T) {
	c := OpenCheckin{SurfacedAt: checkinDay(15, DigestHour, 0), HasUnit: true, Snoozed: true}
	if got := DefaultQuietHours().CheckinStep(c, checkinDay(15, 23, 0)); got != CheckinWait {
		t.Errorf("same morning: %q, want %q", got, CheckinWait)
	}
}

// TestCheckinStep_FollowsTheUsersDigestTime: the digest a check-in waits for
// is the user's, due at their later quiet end.
func TestCheckinStep_FollowsTheUsersDigestTime(t *testing.T) {
	q := QuietHours{End: TimeOfDay{Hour: 8, Minute: 30}}
	c := OpenCheckin{SurfacedAt: checkinDay(14, 21, 0), HasUnit: true, Snoozed: true}
	if got := q.CheckinStep(c, checkinDay(15, 8, 0)); got != CheckinWait {
		t.Errorf("before a quiet end of 08:30: %q, want %q", got, CheckinWait)
	}
	if got := q.CheckinStep(c, checkinDay(15, 8, 35)); got != CheckinReask {
		t.Errorf("after it: %q, want %q", got, CheckinReask)
	}
}

// TestCheckinStep_TheMorningIsNowsZone: surfaced_at is read back in UTC, and
// the morning it is compared with is the user's, in now's zone. 06:30 in Buenos
// Aires is before that morning's 07:00 digest; read in UTC it would be after.
func TestCheckinStep_TheMorningIsNowsZone(t *testing.T) {
	ba := time.FixedZone("-03", -3*60*60)
	c := OpenCheckin{SurfacedAt: time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC), HasUnit: true, Snoozed: true}
	if got := DefaultQuietHours().CheckinStep(c, time.Date(2026, 10, 16, 7, 5, 0, 0, ba)); got != CheckinReask {
		t.Errorf("%q, want %q: 06:30 local was before the 07:00 digest", got, CheckinReask)
	}
}
