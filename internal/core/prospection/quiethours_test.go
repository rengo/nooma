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
