package prospection

import (
	"testing"
	"time"
)

// TestInQuietHours_Boundaries proves R2.1's half-open window
// [QuietHoursStartHour, QuietHoursEndHour): the start bound is inclusive,
// the end bound is exclusive, and nothing outside the window reads true.
func TestInQuietHours_Boundaries(t *testing.T) {
	loc := time.UTC

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{
			name: "00:00:00 is the inclusive start bound",
			now:  time.Date(2026, 8, 7, 0, 0, 0, 0, loc),
			want: true,
		},
		{
			name: "06:59:59 is still inside quiet hours",
			now:  time.Date(2026, 8, 7, 6, 59, 59, 0, loc),
			want: true,
		},
		{
			name: "07:00:00 is the exclusive end bound",
			now:  time.Date(2026, 8, 7, 7, 0, 0, 0, loc),
			want: false,
		},
		{
			name: "23:59:59 is well outside quiet hours",
			now:  time.Date(2026, 8, 7, 23, 59, 59, 0, loc),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InQuietHours(tt.now)
			if got != tt.want {
				t.Errorf("InQuietHours(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

// TestInQuietHours_ZoneTravelsWithInstant proves spec R2.1's own scenario:
// the zone is read off the instant's own Location, never off a configured
// or global clock. Two time.Time values denoting the SAME instant but
// carrying different fixed-offset Locations (deliberately not an IANA
// zone, so the assertion needs no tzdata and stays reproducible on every
// platform this repo cross-compiles for, ADR-0013) must be judged
// differently: one reads 06:30 local (inside quiet hours), the other
// reads 08:30 local (outside).
func TestInQuietHours_ZoneTravelsWithInstant(t *testing.T) {
	plusTwo := time.FixedZone("UTC+2", 2*60*60)
	plusFour := time.FixedZone("UTC+4", 4*60*60)

	instant := time.Date(2026, 8, 7, 4, 30, 0, 0, time.UTC)

	readsAsSixThirty := instant.In(plusTwo)
	readsAsEightThirty := instant.In(plusFour)

	if !readsAsSixThirty.Equal(readsAsEightThirty) {
		t.Fatalf("test fixture is broken: %v and %v are not the same instant",
			readsAsSixThirty, readsAsEightThirty)
	}
	if got, want := readsAsSixThirty.Hour(), 6; got != want {
		t.Fatalf("test fixture is broken: UTC+2 reading has hour %d, want %d", got, want)
	}
	if got, want := readsAsEightThirty.Hour(), 8; got != want {
		t.Fatalf("test fixture is broken: UTC+4 reading has hour %d, want %d", got, want)
	}

	if got := InQuietHours(readsAsSixThirty); got != true {
		t.Errorf("InQuietHours(%v) = %v, want true — 06:30 local is inside quiet hours",
			readsAsSixThirty, got)
	}
	if got := InQuietHours(readsAsEightThirty); got != false {
		t.Errorf("InQuietHours(%v) = %v, want false — 08:30 local is outside quiet hours",
			readsAsEightThirty, got)
	}
}

// TestDeliverableFrom proves design §3.1/§3.3's own arithmetic: outside
// quiet hours, t passes through unchanged; inside quiet hours, the first
// deliverable instant is that same day's QuietHoursEndHour, in t's own
// Location; and the end bound is exclusive on this function exactly as it
// is on InQuietHours — t already at QuietHoursEndHour is already
// deliverable and passes through unchanged, never shifted a day forward.
func TestDeliverableFrom(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)

	tests := []struct {
		name string
		t    time.Time
		want time.Time
	}{
		{
			name: "outside quiet hours passes through unchanged",
			t:    time.Date(2026, 8, 7, 14, 30, 0, 0, loc),
			want: time.Date(2026, 8, 7, 14, 30, 0, 0, loc),
		},
		{
			name: "inside quiet hours shifts to that day's QuietHoursEndHour, same Location",
			t:    time.Date(2026, 8, 7, 3, 15, 0, 0, loc),
			want: time.Date(2026, 8, 7, QuietHoursEndHour, 0, 0, 0, loc),
		},
		{
			name: "exactly at QuietHoursEndHour is unchanged — the end bound is exclusive",
			t:    time.Date(2026, 8, 7, QuietHoursEndHour, 0, 0, 0, loc),
			want: time.Date(2026, 8, 7, QuietHoursEndHour, 0, 0, 0, loc),
		},
		{
			// The other bound. The start is inclusive, so the very first
			// instant of the window shifts — and this is the input the
			// naive `now - fire_at` staleness formula killed nightly
			// (design §3.3), so it is the one case worth pinning by name.
			name: "exactly at QuietHoursStartHour shifts — the start bound is inclusive",
			t:    time.Date(2026, 8, 7, QuietHoursStartHour, 0, 0, 0, loc),
			want: time.Date(2026, 8, 7, QuietHoursEndHour, 0, 0, 0, loc),
		},
		{
			// A negative offset, so the sign of the zone is an exercised
			// input class rather than an assumed symmetry. DeliverableFrom
			// rebuilds its result from t.Date() and t.Location(); nothing
			// in that is sign-aware, and this is what proves it.
			name: "a negative-offset zone behaves like a positive one",
			t:    time.Date(2026, 8, 7, 3, 15, 0, 0, time.FixedZone("UTC-5", -5*60*60)),
			want: time.Date(2026, 8, 7, QuietHoursEndHour, 0, 0, 0, time.FixedZone("UTC-5", -5*60*60)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeliverableFrom(tt.t)
			if !got.Equal(tt.want) || got.Location().String() != tt.want.Location().String() {
				t.Errorf("DeliverableFrom(%v) = %v, want %v", tt.t, got, tt.want)
			}
		})
	}
}

// TestQuietHours is ADR-0030 point 5's night: [QuietHoursStartHour, End)
// on the instant's own wall clock, deferred to End.
func TestQuietHours(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 16, hour, minute, 0, 0, zone) }
	q := QuietHours{End: TimeOfDay{Hour: 9, Minute: 30}}
	for _, tc := range []struct {
		now   time.Time
		quiet bool
		from  time.Time
	}{
		{at(0, 0), true, at(9, 30)},
		{at(9, 29), true, at(9, 30)},
		{at(9, 30), false, at(9, 30)},
		{at(23, 59), false, at(23, 59)},
	} {
		if got := q.Contains(tc.now); got != tc.quiet {
			t.Errorf("Contains(%v) = %v, want %v", tc.now, got, tc.quiet)
		}
		if got := q.DeliverableFrom(tc.now); !got.Equal(tc.from) {
			t.Errorf("DeliverableFrom(%v) = %v, want %v", tc.now, got, tc.from)
		}
	}
	// The default night is the package's own InQuietHours, minute by minute.
	def := DefaultQuietHours()
	for m := 0; m < 24*60; m++ {
		now := at(0, 0).Add(time.Duration(m) * time.Minute)
		if def.Contains(now) != InQuietHours(now) || !def.DeliverableFrom(now).Equal(DeliverableFrom(now)) {
			t.Fatalf("the default night disagrees with InQuietHours/DeliverableFrom at %v", now)
		}
	}
}

// TestQuietHours_TriggerVerdict: a trigger is deferred to the user's quiet
// end, and its staleness is measured from it.
func TestQuietHours_TriggerVerdict(t *testing.T) {
	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 16, hour, minute, 0, 0, time.UTC) }
	q := QuietHours{End: TimeOfDay{Hour: 9}}
	fireAt := at(6, 0)
	for _, tc := range []struct {
		now  time.Time
		want Verdict
	}{
		{at(8, 59), VerdictDefer},
		{at(9, 0), VerdictDeliver},
		{at(9, 0).Add(TriggerStalenessHours * time.Hour), VerdictDeliver},
		{at(9, 1).Add(TriggerStalenessHours * time.Hour), VerdictStale},
	} {
		if got := q.TriggerVerdict(fireAt, tc.now); got != tc.want {
			t.Errorf("TriggerVerdict at %v = %s, want %s", tc.now, got, tc.want)
		}
	}
}

// TestQuietHours_DigestDue: the digest is due at the later of DigestHour
// and the user's quiet end, never inside the night.
func TestQuietHours_DigestDue(t *testing.T) {
	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 16, hour, minute, 0, 0, time.UTC) }
	for _, tc := range []struct {
		end     TimeOfDay
		notYet  time.Time
		dueFrom time.Time
	}{
		{TimeOfDay{Hour: 9, Minute: 15}, at(9, 14), at(9, 15)},
		{TimeOfDay{Hour: 5}, at(DigestHour, 0).Add(-time.Minute), at(DigestHour, 0)},
	} {
		q := QuietHours{End: tc.end}
		if q.DigestDue(nil, tc.notYet) || !q.DigestDue(nil, tc.dueFrom) {
			t.Errorf("quiet end %v: DigestDue at %v, %v = %v, %v; want false, true",
				tc.end, tc.notYet, tc.dueFrom, q.DigestDue(nil, tc.notYet), q.DigestDue(nil, tc.dueFrom))
		}
		sentYesterday := tc.dueFrom.AddDate(0, 0, -1)
		if !q.DigestDue(&sentYesterday, tc.dueFrom) || q.DigestDue(&tc.dueFrom, tc.dueFrom.Add(time.Hour)) {
			t.Errorf("quiet end %v: the last-sent instant is not judged against the shifted due time", tc.end)
		}
	}
}

// TestQuietHours_DigestDue_AtMostOnePerLocalDate: a digest already sent today
// is today's digest, whatever the due time was when it went out. Moving the
// quiet end later after the morning digest (sent at 08:00 under a 07:00 end,
// then the end moved to 10:00) must not make a second one due at 10:00.
func TestQuietHours_DigestDue_AtMostOnePerLocalDate(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	sent := time.Date(2026, 10, 16, 8, 0, 0, 0, zone)
	later := QuietHours{End: TimeOfDay{Hour: 10}}
	for _, now := range []time.Time{
		time.Date(2026, 10, 16, 10, 0, 0, 0, zone),
		time.Date(2026, 10, 16, 23, 59, 0, 0, zone),
	} {
		if later.DigestDue(&sent, now) {
			t.Errorf("a digest sent at %v is due again at %v after the quiet end moved to 10:00", sent, now)
		}
	}
	// The next local date owes its own digest, at the new quiet end.
	if !later.DigestDue(&sent, time.Date(2026, 10, 17, 10, 0, 0, 0, zone)) {
		t.Error("the next day's digest is not due at the moved quiet end")
	}
	// The local date is the user's, not UTC's: 22:00 at UTC-3 is already
	// the next day in UTC, and still today here.
	lateSent := time.Date(2026, 10, 16, 22, 0, 0, 0, zone)
	if later.DigestDue(&lateSent, time.Date(2026, 10, 16, 23, 0, 0, 0, zone)) {
		t.Error("a digest sent at 22:00 local made another due the same local evening")
	}
}
