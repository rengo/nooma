package focus

import (
	"flag"
	"hash/maphash"
	"math"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
)

// dueIn returns a *time.Time exactly d days after now — a small helper so
// every UrgencyRamp fixture below states its intent (days until due) rather
// than a raw time.Duration.
func dueIn(now time.Time, days float64) *time.Time {
	t := now.Add(time.Duration(days * 24 * float64(time.Hour)))
	return &t
}

// TestUrgencyRamp_Table proves spec R3.3's boundary table: nil is exactly
// 0, not the d -> infinity limit; the ramp is 0 at or beyond the lead
// window, rises linearly to 1 at d = 0, and clamps at 1 once a unit is
// overdue, never growing further no matter how overdue it gets.
func TestUrgencyRamp_Table(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		dueAt *time.Time
		want  float64
	}{
		{"nil due date", nil, 0},
		{"due at exactly the lead window", dueIn(now, UrgencyLeadDays), 0},
		{"due well beyond the lead window", dueIn(now, 30), 0},
		{"due halfway through the lead window", dueIn(now, 3.5), 0.5},
		{"due exactly now", dueIn(now, 0), 1},
		{"overdue by one day", dueIn(now, -1), 1},
		{"overdue by 1000 days — does not grow past 1", dueIn(now, -1000), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := UrgencyRamp(c.dueAt, now)
			if got != c.want {
				t.Errorf("UrgencyRamp(%v, now) = %v, want %v", c.dueAt, got, c.want)
			}
		})
	}
}

// TestAgeRamp_Table proves spec R3.4's boundary table: the ramp is 0 at
// creation, rises linearly to 1 at AgeHorizonDays and never grows past it,
// and clamps at 0 for a createdAt after now (clock skew, a backdated
// import). Every fixture is expressed as a multiple of AgeHorizonDays,
// never as a literal day count, so a future recalibration of the horizon
// needs no edit here.
func TestAgeRamp_Table(t *testing.T) {
	createdAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	daysLater := func(days float64) time.Time {
		return createdAt.Add(time.Duration(days * 24 * float64(time.Hour)))
	}

	cases := []struct {
		name string
		now  time.Time
		want float64
	}{
		{"captured this instant", createdAt, 0},
		{"half the horizon", daysLater(AgeHorizonDays / 2.0), 0.5},
		{"exactly the horizon", daysLater(AgeHorizonDays), 1},
		{"twice the horizon — does not grow past 1", daysLater(2 * AgeHorizonDays), 1},
		{"createdAt one hour after now — negative-Δt clamp", daysLater(-1.0 / 24), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AgeRamp(createdAt, c.now)
			if got != c.want {
				t.Errorf("AgeRamp(createdAt, now) = %v, want %v", got, c.want)
			}
		})
	}
}

// splitmix64 is a deterministic pseudo-random generator, seeded per call
// site, used only to synthesize varied test fixtures below — the same
// mechanism internal/core/weight's decay_test.go uses, and for the same
// reason: forbidigo forbids the rand.* call pattern inside internal/core
// (docs/06-harness.md §2, nooma-core hard rule 2).
func splitmix64(state *uint64) uint64 {
	*state += 0x9E3779B97F4A7C15
	z := *state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// focusSeedFlag overrides propertySeed's derived seed, mirroring
// internal/core/weight's -weight-seed (C3.1, tasks.md): 0 (the default)
// means "derive one for this run and print it".
var focusSeedFlag = flag.Uint64("focus-seed", 0, "override the property test's PRNG seed (0 = derive one per run and print it)")

// propertySeed resolves a per-run splitmix64 seed via hash/maphash, logging
// it so a failing run is reproducible via -focus-seed. A seed fixed at the
// source is a permanent blind spot, not an unlucky one (C3.1, tasks.md):
// this file follows the same per-run-seeding discipline decay_test.go
// adopted after that finding, rather than reintroducing the fixed-seed gap
// in a second property test.
func propertySeed(t *testing.T) uint64 {
	t.Helper()
	if *focusSeedFlag != 0 {
		t.Logf("property seed = %d (from -focus-seed)", *focusSeedFlag)
		return *focusSeedFlag
	}
	seed := maphash.Bytes(maphash.MakeSeed(), nil)
	t.Logf("property seed = %d (rerun with -args -focus-seed=%d to reproduce this exact run)", seed, seed)
	return seed
}

// TestPriority_NeverBelowEffectiveWeight_Property proves spec R3.1's first
// MUST: priority >= e for every input. Every factor in the envelope is
// >= 1, so context can promote a unit and can never demote one.
func TestPriority_NeverBelowEffectiveWeight_Property(t *testing.T) {
	state := propertySeed(t)
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	const iterations = 2000
	for i := 0; i < iterations; i++ {
		w := float64(splitmix64(&state)%2_000_001) / 1_000_000.0    // [0, 2.0]
		lambda := float64(splitmix64(&state)%100_001) / 1_000_000.0 // [0, 0.1]
		touchedHoursAgo := int64(splitmix64(&state) % 10_001)       // [0, 10000]h
		ageDays := float64(splitmix64(&state)%10_001) / 100.0       // [0, 100] days
		// [-1.0, 2.0], deliberately outside [0,1] some of the time: adjacency
		// is an externally sourced parameter (ultimately AdjacencyStrengths'
		// max-over-edges of relation.strength, which carries no schema CHECK)
		// and Judgment Day round 1 found this generator previously only ever
		// produced [0,1], leaving the out-of-domain case entirely untested.
		adjacency := float64(splitmix64(&state)%3_000_001)/1_000_000.0 - 1.0
		// Three iterations in eight — one per non-finite value — override
		// adjacency instead: Judgment Day round 1's out-of-domain sweep above
		// only ever produced finite values, so it never exercised the case
		// round 2 found — NaN escapes clamp's two-branch comparison
		// entirely (both judges, independently). ±Inf is included too, to
		// keep pinning the already-correct saturation behaviour under the
		// same generator that now also covers NaN.
		switch splitmix64(&state) % 8 {
		case 0:
			adjacency = math.NaN()
		case 1:
			adjacency = math.Inf(1)
		case 2:
			adjacency = math.Inf(-1)
		}

		var dueAt *time.Time
		if splitmix64(&state)%2 == 0 {
			d := now.Add(time.Duration(int64(splitmix64(&state)%20_001)-10_000) * time.Hour)
			dueAt = &d
		}

		c := Candidate{
			ID:            "u",
			Type:          unit.TypeTask,
			Weight:        w,
			DecayRate:     lambda,
			LastTouchedAt: now.Add(-time.Duration(touchedHoursAgo) * time.Hour),
			CreatedAt:     now.Add(-time.Duration(ageDays*24) * time.Hour),
			DueAt:         dueAt,
		}

		e := weight.Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
		got := Priority(c, adjacency, now)
		// !(got >= e), not got < e: every IEEE 754 comparison against NaN
		// is false, so got < e is silently false when got is NaN and this
		// property would pass right over the exact defect it exists to
		// catch (Judgment Day round 2, both judges independently).
		// !(got >= e) is true both for a genuine deficit and for a NaN
		// got, since NaN >= e is also false.
		if !(got >= e) {
			t.Fatalf("iteration %d: Priority(%+v, %v, now) = %v, want >= effective weight %v", i, c, adjacency, got, e)
		}
	}
}

// TestPriority_AdjacencyClampedToUnitInterval proves adjacency is clamped to
// [0,1] at Priority's entry point rather than trusted as given. Judgment Day
// round 1 falsified R3.1's "priority >= e for every input" MUST with
// adjacency = -1.0: Priority(Candidate{Weight: 1.0, DecayRate: 0,
// LastTouchedAt: now, CreatedAt: now}, -1.0, now) returned 0.75 against
// e = 1.0. adjacency is an ordinary float64 parameter with no schema CHECK
// anywhere upstream (ultimately AdjacencyStrengths' max over
// relation.strength, spec R3.7) — the same threat model
// weight.Effective already sanitizes weight and decayRate against, and the
// same one spread.go's clampStrength sanitizes edge strength against (C15,
// C16). Every out-of-domain adjacency below 0 or above 1 must clamp to
// exactly the same result its nearest in-domain boundary (0 or 1) produces.
func TestPriority_AdjacencyClampedToUnitInterval(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	c := Candidate{
		ID:            "u",
		Weight:        1.0,
		DecayRate:     0,
		LastTouchedAt: now,
		CreatedAt:     now,
	}
	e := weight.Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)

	// Pinned against literals, not against Priority(c, 0.0, now) /
	// Priority(c, 1.0, now): a computed oracle routes both the actual and
	// the expected value through the same clamp call, so a mutant that
	// swaps clamp's bounds (clamp(adjacency, 1, 0)) stays invisible to this
	// test — both sides invert together (Judgment Day round 2, judge A,
	// confirmed by applying the mutant: this test stayed green while 7
	// others caught it). With e = 1.0 (Weight 1.0, DecayRate 0,
	// LastTouchedAt == now == CreatedAt, no DueAt so u = g = 0), the
	// envelope reduces to e * (1 + AdjacencyWeight*a): 1.0 at a=0's lower
	// bound, 1.0*(1+0.25) = 1.25 at a=1's upper bound.
	const atLowerBound = 1.0
	const atUpperBound = 1.25
	if math.Abs(atUpperBound-(1+AdjacencyWeight)) > 1e-9 {
		t.Fatalf("this test's own pinned upper bound = %v, want 1+AdjacencyWeight = %v — AdjacencyWeight changed without this test being updated", atUpperBound, 1+AdjacencyWeight)
	}

	cases := []struct {
		name      string
		adjacency float64
		want      float64
	}{
		{"negative adjacency clamps to the same result as 0", -1.0, atLowerBound},
		{"deeply negative adjacency clamps to the same result as 0", -5.0, atLowerBound},
		{"adjacency above 1 clamps to the same result as 1", 2.0, atUpperBound},
		{"adjacency far above 1 clamps to the same result as 1", 1e6, atUpperBound},
		{"-Inf adjacency clamps to the same result as 0", math.Inf(-1), atLowerBound},
		{"+Inf adjacency clamps to the same result as 1", math.Inf(1), atUpperBound},
		// NaN is not "out of range toward one side or the other" the way
		// ±Inf is — every IEEE 754 comparison against NaN is false, so the
		// two-branch clamp(v, lo, hi) (`v < lo`, `v > hi`) lets NaN fall
		// through both branches unclamped, and clamp(NaN, 0, 1) returned
		// NaN (Judgment Day round 2, both judges independently):
		// Priority(Candidate{Weight: 1.0, DecayRate: 0, LastTouchedAt: now,
		// CreatedAt: now}, math.NaN(), now) returned NaN against a fully
		// finite e = 1.0, and NaN >= e is false, collapsing the very MUST
		// this test exists to prove. clamp now guards NaN explicitly and
		// maps it to lo — the conservative "no adjacency" reading, since
		// adjacency is a promotion-only signal and a corrupt one should
		// promote nothing (see clamp's own doc comment).
		{"NaN adjacency clamps to the same result as 0 (no promotion)", math.NaN(), atLowerBound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Priority(c, tc.adjacency, now)
			if got != tc.want {
				t.Errorf("Priority(c, %v, now) = %v, want %v (clamped)", tc.adjacency, got, tc.want)
			}
			if got < e {
				t.Errorf("Priority(c, %v, now) = %v, want >= effective weight %v (spec R3.1)", tc.adjacency, got, e)
			}
		})
	}
}

// TestPriority_MonotoneNonDecreasingInEffectiveWeight proves spec R3.1's
// second MUST: two units in identical context rank by weight. Fixing every
// term but c.Weight, priority strictly increases as weight increases (for
// e > 0; the envelope's factors are all >= 1 and independent of weight).
func TestPriority_MonotoneNonDecreasingInEffectiveWeight(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	due := now.AddDate(0, 0, 3)
	base := Candidate{
		ID:            "u",
		Type:          unit.TypeTask,
		DecayRate:     0.01,
		LastTouchedAt: now,
		CreatedAt:     now.AddDate(0, 0, -10),
		DueAt:         &due,
	}

	weights := []float64{0.1, 0.5, 1.0, 1.5, 2.0}
	prev := math.Inf(-1)
	for _, w := range weights {
		c := base
		c.Weight = w
		got := Priority(c, 0.3, now)
		if got <= prev {
			t.Fatalf("Priority at weight %v = %v, want strictly greater than the previous weight's %v", w, got, prev)
		}
		prev = got
	}
}

// TestPriority_HomogeneousOfDegreeOneInEffectiveWeight proves spec R3.1's
// third MUST: scaling every candidate's effective weight by the same
// positive factor leaves the relative ordering identical (design §3.1 —
// this is what makes the two focuses' scores comparable, and why the
// hysteresis margin must be relative rather than absolute).
func TestPriority_HomogeneousOfDegreeOneInEffectiveWeight(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	dueSoon := now.AddDate(0, 0, 2)

	candidates := []Candidate{
		{ID: "a", Weight: 1.5, DecayRate: 0.01, LastTouchedAt: now, CreatedAt: now.AddDate(0, 0, -5)},
		{ID: "b", Weight: 0.8, DecayRate: 0.02, LastTouchedAt: now.AddDate(0, 0, -3), CreatedAt: now.AddDate(0, 0, -20), DueAt: &dueSoon},
		{ID: "c", Weight: 1.0, DecayRate: 0.0, LastTouchedAt: now, CreatedAt: now},
	}
	adjacency := map[string]float64{"a": 0.1, "b": 0.0, "c": 0.4}

	priorityAt := func(scale float64) map[string]float64 {
		got := make(map[string]float64, len(candidates))
		for _, c := range candidates {
			c.Weight *= scale
			got[c.ID] = Priority(c, adjacency[c.ID], now)
		}
		return got
	}

	unscaled := priorityAt(1.0)
	scaled := priorityAt(0.5)

	// Guard against a stub that returns the same constant regardless of
	// input: the candidates above differ enough (weight, decay, age,
	// urgency, adjacency) that a genuine Priority must not score them all
	// identically. Without this, a zero-value stub would pass every check
	// below trivially — the exact undisclosed-trivial-pass shape C14
	// (tasks.md) found and requires fixing rather than only naming.
	allEqual := true
	for _, c := range candidates[1:] {
		if unscaled[c.ID] != unscaled[candidates[0].ID] {
			allEqual = false
			break
		}
	}
	if allEqual {
		t.Fatalf("Priority returned the same value (%v) for every candidate despite differing weight, age, urgency and adjacency — cannot be a genuine implementation", unscaled[candidates[0].ID])
	}

	for _, a := range candidates {
		for _, b := range candidates {
			if a.ID == b.ID {
				continue
			}
			unscaledOrder := unscaled[a.ID] > unscaled[b.ID]
			scaledOrder := scaled[a.ID] > scaled[b.ID]
			if unscaledOrder != scaledOrder {
				t.Errorf("scaling every candidate's weight by 0.5 changed the order of %q vs %q: unscaled %v>%v=%v, scaled %v>%v=%v",
					a.ID, b.ID, unscaled[a.ID], unscaled[b.ID], unscaledOrder, scaled[a.ID], scaled[b.ID], scaledOrder)
			}
		}
	}

	// The stronger claim: priority itself scales by exactly 0.5, not just
	// the ordering it induces.
	for _, c := range candidates {
		want := unscaled[c.ID] * 0.5
		if math.Abs(scaled[c.ID]-want) > 1e-9 {
			t.Errorf("Priority(%q) at half weight = %v, want exactly half of the unscaled value %v", c.ID, scaled[c.ID], want)
		}
	}
}

// TestPriority_MaximumAmplificationIdentity proves spec R3.1's stated
// dynamic range: the maximum context amplification is
// UrgencyMax * (1 + AgeWeight + AdjacencyWeight) = 4.35, reached only when
// all three ramps are simultaneously at their extremes.
func TestPriority_MaximumAmplificationIdentity(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	overdue := now.AddDate(-1, 0, 0) // long overdue: UrgencyRamp saturates at 1
	c := Candidate{
		ID:            "u",
		Weight:        1.0,
		DecayRate:     0,
		LastTouchedAt: now,
		CreatedAt:     now.AddDate(0, 0, -2*AgeHorizonDays), // AgeRamp saturates at 1
		DueAt:         &overdue,
	}

	want := UrgencyMax * (1 + AgeWeight + AdjacencyWeight)
	if math.Abs(want-4.35) > 1e-9 {
		t.Fatalf("this test's own expected amplification = %v, want 4.35 — the constants changed without this test being updated", want)
	}

	got := Priority(c, 1.0, now) // adjacency at its own maximum, 1.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("Priority at all three ramps' extremes = %v, want the maximum amplification %v (e = 1.0)", got, want)
	}
}

// TestPriority_TypeIndependent proves spec R3.2 (owner ruling 8): a task
// and an event identical in every other field score exactly equal. type
// enters the ranking only as the focus-membership predicate Types selects
// over, never as a term inside Priority.
func TestPriority_TypeIndependent(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	due := now.AddDate(0, 0, 2)
	base := Candidate{
		ID:            "u",
		Weight:        1.2,
		DecayRate:     0.015,
		LastTouchedAt: now.AddDate(0, 0, -1),
		CreatedAt:     now.AddDate(0, 0, -8),
		DueAt:         &due,
	}

	task := base
	task.Type = unit.TypeTask
	event := base
	event.Type = unit.TypeEvent

	adjacency := 0.2
	gotTask := Priority(task, adjacency, now)
	gotEvent := Priority(event, adjacency, now)
	if gotTask != gotEvent {
		t.Errorf("Priority(task) = %v, Priority(event) = %v, want exactly equal — no term reads c.Type (spec R3.2)", gotTask, gotEvent)
	}

	// The equality above would pass trivially against a stub returning a
	// constant for every input (the exact undisclosed-trivial-pass shape
	// C14, tasks.md, warns against): pin gotTask against a value derived
	// independently from Priority's own body, using UrgencyRamp, AgeRamp
	// and weight.Effective — all three already implemented and verified by
	// this point in the commit sequence — rather than Priority's own
	// arithmetic.
	e := weight.Effective(base.Weight, base.DecayRate, base.LastTouchedAt, now)
	u := UrgencyRamp(base.DueAt, now)
	g := AgeRamp(base.CreatedAt, now)
	want := e * (1 + (UrgencyMax-1)*u) * (1 + AgeWeight*g + AdjacencyWeight*adjacency)
	if math.Abs(gotTask-want) > 1e-9 {
		t.Errorf("Priority(task) = %v, want %v (independently derived from UrgencyRamp, AgeRamp and weight.Effective)", gotTask, want)
	}
}

// TestPriority_P1_BoundedLeverageWithNoDeadlineOrAdjacency proves spec
// R3.5 P1: with no deadline and no adjacency, priority <= e * (1 +
// AgeWeight). The age term's entire lifetime leverage is 20%; it never
// grows past AgeHorizonDays and never operates on anything but e.
//
// Not a genuine RED against this commit's zero-value stub: an upper-bound
// property (priority <= max) is trivially satisfied by a stub that always
// returns 0, for any non-negative max. Disclosed per this document's own
// convention (tasks.md's intro) rather than left unstated — the other nine
// tests in this commit are the ones that fail against the stub for the
// missing-formula reason; this one is the permanent guard once Priority is
// implemented, not a red step.
func TestPriority_P1_BoundedLeverageWithNoDeadlineOrAdjacency(t *testing.T) {
	state := propertySeed(t)
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	const iterations = 1000
	for i := 0; i < iterations; i++ {
		w := float64(splitmix64(&state)%2_000_001) / 1_000_000.0
		lambda := float64(splitmix64(&state)%100_001) / 1_000_000.0
		ageDays := float64(splitmix64(&state)%40_001) / 100.0 // [0, 400] days

		c := Candidate{
			ID:            "u",
			Weight:        w,
			DecayRate:     lambda,
			LastTouchedAt: now,
			CreatedAt:     now.Add(-time.Duration(ageDays*24) * time.Hour),
		}

		e := weight.Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
		got := Priority(c, 0, now)
		max := e * (1 + AgeWeight)
		if got > max+1e-9 {
			t.Fatalf("iteration %d: Priority(no deadline, no adjacency) = %v, want <= e*(1+AgeWeight) = %v", i, got, max)
		}
	}
}

// TestPriority_P2_OverturnableDeficitCrossoverRatio proves spec R3.5 P2:
// two units in identical context differing only in age cross over exactly
// at e_old/e_new = 1/(1 + AgeWeight*Δg). At the extreme (Δg=1) that ratio
// is 1/1.20 = 0.8333..., and age can overturn an effective-weight deficit
// of at most AgeWeight/(1+AgeWeight) = 16.7%, never more. Pinned from both
// sides of the boundary: a ratio just above it and the older unit wins; a
// ratio just below it and the older unit does not.
func TestPriority_P2_OverturnableDeficitCrossoverRatio(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	eYoung := 1.0

	deltaGs := []float64{0, 0.5, 1}
	for _, deltaG := range deltaGs {
		ratio := 1 / (1 + AgeWeight*deltaG)
		if deltaG == 1 {
			const want = 1.0 / 1.20
			if math.Abs(ratio-want) > 1e-9 {
				t.Fatalf("crossover ratio at Δg=1 = %v, want %v (0.8333...)", ratio, want)
			}
		}

		older := func(eOld float64) Candidate {
			return Candidate{
				ID:            "old",
				Weight:        eOld,
				DecayRate:     0,
				LastTouchedAt: now,
				CreatedAt:     now.Add(-time.Duration(deltaG*AgeHorizonDays*24) * time.Hour),
			}
		}
		young := Candidate{
			ID:            "young",
			Weight:        eYoung,
			DecayRate:     0,
			LastTouchedAt: now,
			CreatedAt:     now,
		}
		youngPriority := Priority(young, 0, now)

		t.Run("just above the crossover ratio, older wins", func(t *testing.T) {
			eOld := eYoung * ratio * 1.001
			oldPriority := Priority(older(eOld), 0, now)
			if oldPriority <= youngPriority {
				t.Errorf("Δg=%v, e_old/e_new=%v (just above %v): older's priority %v, want > young's %v", deltaG, eOld/eYoung, ratio, oldPriority, youngPriority)
			}
		})
		t.Run("just below the crossover ratio, older does not win", func(t *testing.T) {
			eOld := eYoung * ratio * 0.999
			oldPriority := Priority(older(eOld), 0, now)
			if oldPriority > youngPriority {
				t.Errorf("Δg=%v, e_old/e_new=%v (just below %v): older's priority %v, want <= young's %v", deltaG, eOld/eYoung, ratio, oldPriority, youngPriority)
			}
		})
	}
}

// TestPriority_P3_FloorCannotClimbOutOfArchiveThreshold proves spec R3.5
// P3: a unit at the archive floor (e = 0.5) at full age reaches at most
// 0.5*1.20 = 0.60, while a healthy unit at classify's base weight (e =
// 1.0), brand new, scores 1.0 — beating the starved unit at the floor by
// 1.67x at any age. Anti-starvation re-ranks among units that still hold
// weight; it does not rescue units that have lost it.
func TestPriority_P3_FloorCannotClimbOutOfArchiveThreshold(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	starved := Candidate{
		ID:            "starved",
		Weight:        0.5,
		DecayRate:     0,
		LastTouchedAt: now,
		CreatedAt:     now.Add(-time.Duration(2*AgeHorizonDays*24) * time.Hour), // full age
	}
	healthy := Candidate{
		ID:            "healthy",
		Weight:        1.0,
		DecayRate:     0,
		LastTouchedAt: now,
		CreatedAt:     now, // brand new
	}

	gotStarved := Priority(starved, 0, now)
	gotHealthy := Priority(healthy, 0, now)

	if gotHealthy <= gotStarved {
		t.Fatalf("Priority(healthy, brand new) = %v, want strictly greater than Priority(starved, full age) = %v", gotHealthy, gotStarved)
	}
	if gotStarved > 0.60+1e-9 {
		t.Errorf("Priority(starved, full age) = %v, want <= 0.60 (0.5 * 1.20)", gotStarved)
	}
}

// ageMultiplier reproduces the age-only half of Priority's envelope,
// (1 + AgeWeight*clamp(ageDays/horizonDays, 0, 1)), parameterized over
// horizonDays rather than fixed at the package's AgeHorizonDays constant.
// Used only by TestPriority_P4P5 and TestPriority_P6 below, to walk the
// closed-form thresholds design §3.1 derives and to compare the shipped
// horizon against the rejected one — not a production code path.
func ageMultiplier(ageDays, horizonDays float64) float64 {
	ramp := ageDays / horizonDays
	if ramp < 0 {
		ramp = 0
	}
	if ramp > 1 {
		ramp = 1
	}
	return 1 + AgeWeight*ramp
}

// TestPriority_P4P5_RisesToPeakAtHorizonThenDeclines proves spec R3.5
// P4/P5, rewritten under owner ruling 10: an untouched unit's priority,
// walked at t in {0, horizon/2, horizon, 2*horizon, 4*horizon} at the base
// decay rate (classify.PriorDecayRate), rises to its maximum at exactly
// t=horizon and declines strictly thereafter — the opposite of what this
// property asserted before ruling 10 moved the horizon from 30 to 15. Both
// closed-form thresholds are pinned from both sides: at
// lambda=0.0111 (<= the peak-at-horizon threshold, 0.01111...) the peak is
// still at the horizon; at lambda=0.0134 (> the rising-at-origin threshold,
// 0.01333...) the sequence is already decreasing from t=0.
func TestPriority_P4P5_RisesToPeakAtHorizonThenDeclines(t *testing.T) {
	createdAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	walk := []float64{0, AgeHorizonDays / 2.0, AgeHorizonDays, 2 * AgeHorizonDays, 4 * AgeHorizonDays}

	priorityAt := func(lambda, ageDays float64) float64 {
		now := createdAt.Add(time.Duration(ageDays*24) * time.Hour)
		c := Candidate{
			ID:            "u",
			Weight:        1.0,
			DecayRate:     lambda,
			LastTouchedAt: createdAt,
			CreatedAt:     createdAt,
		}
		return Priority(c, 0, now)
	}

	t.Run("base decay rate: rises to the horizon, then declines", func(t *testing.T) {
		values := make([]float64, len(walk))
		for i, days := range walk {
			values[i] = priorityAt(classify.PriorDecayRate, days)
		}
		// index 2 is t=horizon.
		for i := 0; i < 2; i++ {
			if values[i+1] <= values[i] {
				t.Errorf("priority at day %v = %v, want strictly greater than day %v's %v (rising toward the horizon)", walk[i+1], values[i+1], walk[i], values[i])
			}
		}
		for i := 2; i < len(values)-1; i++ {
			if values[i+1] >= values[i] {
				t.Errorf("priority at day %v = %v, want strictly less than day %v's %v (declining past the horizon)", walk[i+1], values[i+1], walk[i], values[i])
			}
		}
	})

	t.Run("lambda=0.0111: peak still at the horizon", func(t *testing.T) {
		const lambda = 0.0111
		values := make([]float64, len(walk))
		for i, days := range walk {
			values[i] = priorityAt(lambda, days)
		}
		for i := 0; i < 2; i++ {
			if values[i+1] <= values[i] {
				t.Errorf("priority at day %v = %v, want strictly greater than day %v's %v", walk[i+1], values[i+1], walk[i], values[i])
			}
		}
		for i := 2; i < len(values)-1; i++ {
			if values[i+1] >= values[i] {
				t.Errorf("priority at day %v = %v, want strictly less than day %v's %v", walk[i+1], values[i+1], walk[i], values[i])
			}
		}
	})

	t.Run("lambda=0.0134: decreasing from t=0", func(t *testing.T) {
		const lambda = 0.0134
		values := make([]float64, len(walk))
		for i, days := range walk {
			values[i] = priorityAt(lambda, days)
		}
		for i := 0; i < len(values)-1; i++ {
			if values[i+1] >= values[i] {
				t.Errorf("priority at day %v = %v, want strictly less than day %v's %v — this lambda is above the rising-at-origin threshold", walk[i+1], values[i+1], walk[i], values[i])
			}
		}
	})
}

// TestPriority_P6_HorizonBoughtAWindowNotAFloor proves spec R3.5 P6: day-30
// and day-60 priorities are independent of AgeHorizonDays, because the age
// ramp is already saturated at 1 for both the shipped horizon (15) and the
// rejected one (30) by those days. Computed two ways — once through the
// real Priority (fixed at the package's AgeHorizonDays) and once through
// ageMultiplier's closed form at the rejected horizon of 30 — and asserted
// equal, proving the lever bought an earlier arrival, not more power.
func TestPriority_P6_HorizonBoughtAWindowNotAFloor(t *testing.T) {
	createdAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	for _, days := range []float64{30, 60} {
		now := createdAt.Add(time.Duration(days*24) * time.Hour)
		c := Candidate{
			ID:            "u",
			Weight:        1.0,
			DecayRate:     classify.PriorDecayRate,
			LastTouchedAt: createdAt,
			CreatedAt:     createdAt,
		}

		gotAtShippedHorizon := Priority(c, 0, now)

		e := weight.Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
		wantAtRejectedHorizon := e * ageMultiplier(days, 30)

		if math.Abs(gotAtShippedHorizon-wantAtRejectedHorizon) > 1e-9 {
			t.Errorf("day %v: priority at AgeHorizonDays=15 = %v, want equal to the value at the rejected horizon 30 = %v — both should be saturated at the same 1.20 factor", days, gotAtShippedHorizon, wantAtRejectedHorizon)
		}
	}
}
