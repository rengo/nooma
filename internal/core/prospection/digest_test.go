package prospection

import (
	"math"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/unit"
)

// TestDigestDue proves owner ruling 2's cadence: once daily, at DigestHour,
// and a vault that was off for three days owes exactly one digest rather
// than three (ADR-0014's "a late delivery is a normal case" applied to the
// cadence itself).
func TestDigestDue(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, 8, day, hour, minute, 0, 0, loc)
	}
	ptr := func(t time.Time) *time.Time { return &t }

	tests := []struct {
		name       string
		lastDigest *time.Time
		now        time.Time
		want       bool
	}{
		{
			name:       "no digest ever, now at DigestHour",
			lastDigest: nil,
			now:        at(7, DigestHour, 0),
			want:       true,
		},
		{
			name:       "no digest ever, now before DigestHour",
			lastDigest: nil,
			now:        at(7, DigestHour-1, 59),
			want:       false,
		},
		{
			name:       "today's digest already sent, later the same day",
			lastDigest: ptr(at(7, DigestHour, 0)),
			now:        at(7, 18, 0),
			want:       false,
		},
		{
			name:       "yesterday's digest, now at today's DigestHour",
			lastDigest: ptr(at(6, DigestHour, 0)),
			now:        at(7, DigestHour, 0),
			want:       true,
		},
		{
			// Downtime is a normal case, and the cadence must not accrue a
			// backlog: three days off owes one digest, not three. Asserted
			// as the verdict at a single instant, which is all a pure
			// predicate can say — that one call returns true once, and
			// false again after the digest is recorded, is the pair that
			// makes "exactly one" observable.
			name:       "three days of downtime owes one digest",
			lastDigest: ptr(at(4, DigestHour, 0)),
			now:        at(7, DigestHour+3, 0),
			want:       true,
		},
		{
			name:       "and once that digest is recorded, no more today",
			lastDigest: ptr(at(7, DigestHour+3, 0)),
			now:        at(7, 23, 59),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DigestDue(tt.lastDigest, tt.now); got != tt.want {
				t.Errorf("DigestDue(%v, %v) = %v, want %v", tt.lastDigest, tt.now, got, tt.want)
			}
		})
	}
}

// TestDigestHourIsNotBeforeQuietHoursEnd pins the one relation that matters
// between two constants deliberately kept separate (design §3.5).
//
// A digest hour before quiet hours end is a digest born deferred every
// single day: the cadence would be decorative and the real delivery hour
// would be QuietHoursEndHour anyway. Today the two are equal, which is the
// only hour that is both a morning digest and never born deferred — but
// they are two knobs with two §13 rows, and this asserts the invariant
// rather than their current equality, so either can be recalibrated as long
// as the relation survives.
//
// Disclosed per m2a C9: both constants already exist by this point in the
// chain, so there is no missing-symbol red available for this check.
func TestDigestHourIsNotBeforeQuietHoursEnd(t *testing.T) {
	if DigestHour < QuietHoursEndHour {
		t.Fatalf("DigestHour (%d) is before QuietHoursEndHour (%d): every digest would be born "+
			"deferred, the cadence would be decorative, and the real delivery hour would be %d",
			DigestHour, QuietHoursEndHour, QuietHoursEndHour)
	}
}

// TestLowEnergy proves doc 02 §7's own two-part condition — "low (recent
// reading)" — with both halves independently falsifiable.
func TestLowEnergy(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	reading := func(level float64, ago time.Duration) *EnergyReading {
		return &EnergyReading{Level: level, RecordedAt: now.Add(-ago)}
	}

	tests := []struct {
		name string
		r    *EnergyReading
		want bool
	}{
		{
			// No observation is not an observation of depletion. The gate
			// suppresses delivery, so silence must not be read as consent
			// to suppress.
			name: "no reading at all",
			r:    nil,
			want: false,
		},
		{
			name: "low and recent",
			r:    reading(LowEnergyMax-0.1, time.Hour),
			want: true,
		},
		{
			// Strict: the gate suppresses, so the burden of proof is on
			// "low", and exactly the midpoint is not low. Same convention as
			// consolidation.CatchUpDue and the staleness gate.
			name: "exactly at the threshold is not low",
			r:    reading(LowEnergyMax, time.Hour),
			want: false,
		},
		{
			name: "low but stale — the recent half fails",
			r:    reading(0.0, (EnergyReadingMaxAgeHours+1)*time.Hour),
			want: false,
		},
		{
			name: "exactly at the age bound is still recent",
			r:    reading(LowEnergyMax-0.1, EnergyReadingMaxAgeHours*time.Hour),
			want: true,
		},
		{
			name: "high and recent",
			r:    reading(0.9, time.Hour),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LowEnergy(tt.r, now); got != tt.want {
				t.Errorf("LowEnergy(%+v, %v) = %v, want %v", tt.r, now, got, tt.want)
			}
		})
	}
}

// digestItem builds a DigestItem whose rank is controlled by weight alone:
// every other Priority term is held constant, so a larger weight is a
// strictly higher rank and the tests below can talk about "ranked last"
// without depending on the formula's shape.
func digestItem(id string, weight float64, deferrals int, now time.Time) DigestItem {
	return DigestItem{
		ID: id,
		Candidate: &focus.Candidate{
			ID:            id,
			Type:          unit.TypeTask,
			Weight:        weight,
			DecayRate:     0,
			LastTouchedAt: now,
			CreatedAt:     now,
		},
		Deferrals: deferrals,
	}
}

func ids(items []DigestItem) []string {
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, i.ID)
	}
	return out
}

func TestCarry(t *testing.T) {
	now := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)

	t.Run("not low energy carries everything", func(t *testing.T) {
		items := []DigestItem{
			digestItem("a", 1.0, 0, now),
			digestItem("b", 2.0, 0, now),
			digestItem("c", 3.0, 0, now),
			digestItem("d", 4.0, 0, now),
			digestItem("e", 5.0, 0, now),
		}
		carry, held := Carry(items, nil, false, now)
		if len(carry) != len(items) || len(held) != 0 {
			t.Errorf("carried %v, held %v — the care gate is the only thing that holds an item "+
				"back, so a normal-energy digest carries all %d", ids(carry), ids(held), len(items))
		}
	})

	t.Run("fewer items than the low-energy size carries all", func(t *testing.T) {
		items := []DigestItem{digestItem("a", 1.0, 0, now), digestItem("b", 2.0, 0, now)}
		carry, held := Carry(items, nil, true, now)
		if len(carry) != 2 || len(held) != 0 {
			t.Errorf("carried %v, held %v — truncation cannot hold back what already fits",
				ids(carry), ids(held))
		}
	})

	t.Run("low energy truncates to the top LowEnergyDigestSize by rank", func(t *testing.T) {
		items := []DigestItem{
			digestItem("lowest", 1.0, 0, now),
			digestItem("low", 2.0, 0, now),
			digestItem("mid", 3.0, 0, now),
			digestItem("high", 4.0, 0, now),
			digestItem("highest", 5.0, 0, now),
		}
		carry, held := Carry(items, nil, true, now)
		if len(carry) != LowEnergyDigestSize {
			t.Fatalf("carried %v (%d), want exactly LowEnergyDigestSize (%d)",
				ids(carry), len(carry), LowEnergyDigestSize)
		}
		if len(held) != len(items)-LowEnergyDigestSize {
			t.Errorf("held %v, want the remaining %d", ids(held), len(items)-LowEnergyDigestSize)
		}
		for _, want := range []string{"highest", "high", "mid"} {
			var found bool
			for _, c := range carry {
				if c.ID == want {
					found = true
				}
			}
			if !found {
				t.Errorf("carried %v, missing %q — truncation keeps the top of the ranking",
					ids(carry), want)
			}
		}
	})

	t.Run("an item at MaxDigestDeferrals is carried regardless of rank", func(t *testing.T) {
		items := []DigestItem{
			digestItem("starved", 0.001, MaxDigestDeferrals, now),
			digestItem("a", 1.0, 0, now),
			digestItem("b", 2.0, 0, now),
			digestItem("c", 3.0, 0, now),
			digestItem("d", 4.0, 0, now),
		}
		carry, _ := Carry(items, nil, true, now)

		var found bool
		for _, c := range carry {
			if c.ID == "starved" {
				found = true
			}
		}
		if !found {
			t.Errorf("carried %v, missing the starved item — at MaxDigestDeferrals an item is "+
				"carried regardless of rank, and it is ranked last here on purpose", ids(carry))
		}
		if len(carry) != LowEnergyDigestSize+1 {
			t.Errorf("carried %d items, want %d — a force-carried item is carried IN ADDITION "+
				"to the truncation, never as one of its slots; competing for the same slots is "+
				"how a low-ranked item stays starved forever", len(carry), LowEnergyDigestSize+1)
		}
	})

	t.Run("starvation is bounded, not merely counted", func(t *testing.T) {
		// The whole anti-starvation claim, walked: the same last-ranked item,
		// against the same fuller-ranked field, on four consecutive digests.
		// Held, held, held, carried. A test that only checked "an item with
		// MaxDigestDeferrals is carried" would pass even if nothing ever
		// reached that count.
		var carried int
		for deferrals := 0; deferrals <= MaxDigestDeferrals; deferrals++ {
			items := []DigestItem{
				digestItem("persistent", 0.001, deferrals, now),
				digestItem("a", 1.0, 0, now),
				digestItem("b", 2.0, 0, now),
				digestItem("c", 3.0, 0, now),
				digestItem("d", 4.0, 0, now),
			}
			carry, _ := Carry(items, nil, true, now)

			var in bool
			for _, c := range carry {
				if c.ID == "persistent" {
					in = true
				}
			}
			switch {
			case deferrals < MaxDigestDeferrals && in:
				t.Errorf("deferrals=%d: the last-ranked item was carried before the bound; "+
					"the truncation is supposed to hold it", deferrals)
			case deferrals == MaxDigestDeferrals && !in:
				t.Errorf("deferrals=%d: the item was held at the bound — starvation is unbounded",
					deferrals)
			case in:
				carried++
			}
		}
		if carried != 1 {
			t.Errorf("the persistent item was carried %d times across the walk, want exactly 1 "+
				"— at the bound and not before", carried)
		}
	})

	t.Run("a trigger with no source unit ranks last and never panics", func(t *testing.T) {
		items := []DigestItem{
			{ID: "pattern-watcher", Candidate: nil, Deferrals: 0},
			digestItem("a", 1.0, 0, now),
			digestItem("b", 2.0, 0, now),
			digestItem("c", 3.0, 0, now),
		}
		carry, held := Carry(items, nil, true, now)

		for _, c := range carry {
			if c.ID == "pattern-watcher" {
				t.Errorf("carried %v — a trigger with no source unit has priority 0 and must "+
					"rank last, so it is the first thing a depleted user stops being asked",
					ids(carry))
			}
		}
		var inHeld bool
		for _, h := range held {
			if h.ID == "pattern-watcher" {
				inHeld = true
			}
		}
		if !inHeld {
			t.Errorf("held %v — the unit-less trigger went missing entirely; every item must "+
				"appear in exactly one of the two slices", ids(held))
		}
	})

	t.Run("every item lands in exactly one slice", func(t *testing.T) {
		items := []DigestItem{
			{ID: "no-unit", Candidate: nil, Deferrals: 0},
			digestItem("starved", 0.001, MaxDigestDeferrals, now),
			digestItem("a", 1.0, 0, now),
			digestItem("b", 2.0, 0, now),
			digestItem("c", 3.0, 0, now),
			digestItem("d", 4.0, 0, now),
		}
		carry, held := Carry(items, nil, true, now)

		seen := map[string]int{}
		for _, i := range append(append([]DigestItem{}, carry...), held...) {
			seen[i.ID]++
		}
		if len(seen) != len(items) {
			t.Errorf("carry+held covers %d distinct ids, want %d — an item was dropped",
				len(seen), len(items))
		}
		for id, n := range seen {
			if n != 1 {
				t.Errorf("%q appears %d times across carry and held, want exactly 1", id, n)
			}
		}
	})

	t.Run("an empty input takes no position on publishing", func(t *testing.T) {
		carry, held := Carry(nil, nil, true, now)
		if len(carry) != 0 || len(held) != 0 {
			t.Errorf("carried %v, held %v — nothing in, nothing out", ids(carry), ids(held))
		}
	})
}

// TestLowEnergy_UnvouchableLevelIsNotLow covers the values core cannot
// vouch for. current_state.energy is a bare REAL with no CHECK constraint
// (migration 0001), so nothing between the database and this gate
// guarantees a number in [0,1].
//
// The direction is the same one the nil case already states: this gate
// SUPPRESSES delivery, so anything core cannot read must not be grounds to
// suppress. NaN is the sharp case — every IEEE-754 comparison against NaN
// is false, so a plain `Level >= LowEnergyMax` guard falls straight through
// to the recency check and reports a corrupt reading as low energy.
// focus/priority.go records this same trap being found twice before.
func TestLowEnergy_UnvouchableLevelIsNotLow(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	for name, level := range map[string]float64{
		"NaN":         math.NaN(),
		"+Inf":        math.Inf(1),
		"-Inf":        math.Inf(-1),
		"below range": -0.5,
		"above range": 9.9,
	} {
		t.Run(name, func(t *testing.T) {
			if LowEnergy(&EnergyReading{Level: level, RecordedAt: now}, now) {
				t.Errorf("LowEnergy(level=%v, recent) = true — a level core cannot vouch for is "+
					"not an observation of depletion, and this gate suppresses delivery", level)
			}
		})
	}
}

// TestCarry_DuplicateIDsLoseNothing is the invariant "every item lands in
// exactly one slice" under the input that actually breaks a lookup by ID.
//
// focus.Rank's own doc comment says it neither validates nor deduplicates
// ids, and names three ways a caller may close that: guarantee uniqueness
// upstream, reject a duplicate outright, or accept either order explicitly.
// Carry is Rank's second caller in this repository and takes the third: it
// resolves each ranked entry against a per-id queue rather than a single map
// slot, so duplicates round-trip and only their relative order is
// unspecified.
//
// The failure this guards against is not a crash. With a single-slot map the
// counts still balance — four in, four out — while the highest-priority item
// silently vanishes and a lower one is delivered twice. A test that only
// counted would have passed.
func TestCarry_DuplicateIDsLoseNothing(t *testing.T) {
	now := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)

	items := []DigestItem{
		digestItem("dup", 5.0, 0, now),
		digestItem("dup", 1.0, 0, now),
		digestItem("c", 3.0, 0, now),
		digestItem("d", 4.0, 0, now),
	}
	carry, held := Carry(items, nil, true, now)

	if got := len(carry) + len(held); got != len(items) {
		t.Fatalf("carry+held holds %d items, want %d", got, len(items))
	}

	weights := map[float64]int{}
	for _, i := range append(append([]DigestItem{}, carry...), held...) {
		weights[i.Candidate.Weight]++
	}
	for _, want := range []float64{5.0, 1.0, 3.0, 4.0} {
		if weights[want] != 1 {
			t.Errorf("the item with weight %v appears %d times across carry and held, want "+
				"exactly 1 — identity is resolved per ranked entry, so a shared id must not "+
				"collapse two items into one", want, weights[want])
		}
	}
}

// TestLowEnergy_FutureReadingIsNotRecent covers clock skew and a backdated
// import. now.Sub(future) is negative, and a negative duration is always
// <= a positive bound, so an unguarded recency check reads a reading dated
// next year as "recent" and lets it suppress delivery indefinitely.
//
// The repository already has this convention twice: weight.Effective clamps
// a negative deltaDays to 0, and focus.AgeRamp's own comment names
// "createdAt after now — clock skew, a backdated import". This gate follows
// it, in the direction its own nil case already established.
func TestLowEnergy_FutureReadingIsNotRecent(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	for name, ahead := range map[string]time.Duration{
		"a second ahead": time.Second,
		"a year ahead":   365 * 24 * time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			r := &EnergyReading{Level: 0.1, RecordedAt: now.Add(ahead)}
			if LowEnergy(r, now) {
				t.Errorf("LowEnergy(recordedAt=%v ahead of now) = true — a reading from the "+
					"future is not an observation of the present, and this gate suppresses "+
					"delivery", ahead)
			}
		})
	}
}

// TestMaxDigestDeferralsIsInsideItsDerivedBand is task 5.8: the band the
// constant was chosen inside, asserted from both named constants rather
// than restated in prose. More than one deferral, or anti-starvation is a
// one-day delay wearing the name; strictly fewer than the load cooldown, or
// an item can be silenced across exactly the window in which the load
// watcher has stopped looking.
//
// Disclosed per m2a C9: both constants already exist, so there is no
// missing-symbol red available for this check.
func TestMaxDigestDeferralsIsInsideItsDerivedBand(t *testing.T) {
	if MaxDigestDeferrals <= 1 {
		t.Errorf("MaxDigestDeferrals = %d, want > 1 — at 1 or below, the care gate is a "+
			"one-day delay and anti-starvation bounds nothing", MaxDigestDeferrals)
	}
	if MaxDigestDeferrals >= consolidation.LoadCooldownDays {
		t.Errorf("MaxDigestDeferrals = %d, want < consolidation.LoadCooldownDays (%d) — an item "+
			"suppressible for the whole cooldown could be silenced across exactly the window in "+
			"which the load watcher will not re-open a state hypothesis",
			MaxDigestDeferrals, consolidation.LoadCooldownDays)
	}
}

// TestLowEnergyDigestSizeIsHalfFocusSize is task 5.9: the derivation, held
// as a relation between two named constants so a recalibration of focus_size
// carries it rather than silently diverging from the comment that claims it.
//
// Disclosed per m2a C9: both constants already exist, so no missing-symbol
// red is available.
func TestLowEnergyDigestSizeIsHalfFocusSize(t *testing.T) {
	if want := focus.DefaultSize / 2; LowEnergyDigestSize != want {
		t.Errorf("LowEnergyDigestSize = %d, want focus.DefaultSize/2 = %d — the derivation is "+
			"the point, and a constant that drifts from it is an invented number",
			LowEnergyDigestSize, want)
	}
	if LowEnergyDigestSize < 1 {
		t.Errorf("LowEnergyDigestSize = %d — a low-energy digest that carries nothing is not a "+
			"care gate, it is silence", LowEnergyDigestSize)
	}
}
