package brain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// m4c's second link (design §3.5): the digest's Carry and Today's pending-digest
// mirror order low-energy items by their adjacency to the focus, so an item on
// a unit related to something the user is focused on is carried ahead of an
// equal one that is not.
//
// FX-L (design §6): Carry returns every item unranked unless energy is low, and
// then carries only LowEnergyDigestSize (3). The fixture therefore holds five
// pending items, a low-energy reading, and two contested items on equal terms:
//
//	trg-hi  weight 3    ranks above both, always carried
//	trg-1   weight 1    equal to trg-2 in every base term
//	trg-2   weight 1    ... so Rank's id tie-break puts trg-1 first
//	trg-lo  weight 0.2  ranks below both, held
//	trg-nil no unit     a pattern watcher: no candidate, no adjacency entry
//
// The trigger ids are not the unit ids (u-p, u-q), so an adjacency map that
// was never re-keyed from units to triggers cannot match anything. The units
// are knowledge, a type in neither focus, so they never enter the pool whose
// incumbent is under test. Adjacency lifts an item by AdjacencyWeight times
// its strength (1.225x at 0.9), which cannot reach trg-hi.

const (
	carryHi  = "trg-hi"
	carryOne = "trg-1" // on u-p
	carryTwo = "trg-2" // on u-q
)

// lowEnergy records a fresh low reading, so Carry ranks and truncates.
func (f *fxH) lowEnergy() {
	f.state.RecordEnergy(prospection.EnergyReading{Level: prospection.LowEnergyMax - 0.4, RecordedAt: todayNow.Add(-time.Minute)})
}

// carryTrigger seeds a fired, undelivered trigger on a knowledge unit of the
// given weight.
func (f *fxH) carryTrigger(t *testing.T, trigID, unitID string, w float64) {
	t.Helper()
	ctx := context.Background()
	if err := f.units.Create(ctx, unit.Unit{
		ID: unitID, Type: unit.TypeKnowledge, Status: unit.StatusPool, Content: unitID,
		Weight: w, LastTouchedAt: todayNow, CreatedAt: todayNow, UpdatedAt: todayNow,
	}); err != nil {
		t.Fatalf("seed unit %s: %v", unitID, err)
	}
	uid, fireAt := unitID, todayNow.Add(-time.Hour)
	if err := f.triggers.Create(ctx, ports.Trigger{
		ID: trigID, UnitID: &uid, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "act on " + trigID}, FireAt: &fireAt, CreatedAt: todayNow,
	}); err != nil {
		t.Fatalf("seed trigger %s: %v", trigID, err)
	}
	if err := f.triggers.Fire(ctx, trigID, todayNow); err != nil {
		t.Fatalf("fire trigger %s: %v", trigID, err)
	}
}

// fxL seeds the five pending items above, under a low-energy reading.
func (f *fxH) fxL(t *testing.T) {
	t.Helper()
	f.lowEnergy()
	f.carryTrigger(t, carryHi, "u-hi", 3)
	f.carryTrigger(t, carryOne, "u-p", 1)
	f.carryTrigger(t, carryTwo, "u-q", 1)
	f.carryTrigger(t, "trg-lo", "u-lo", 0.2)
	ctx := context.Background()
	if err := f.triggers.Create(ctx, ports.Trigger{
		ID: "trg-nil", Kind: ports.TriggerKindPatternBased,
		Payload: ports.TriggerPayload{ActionText: "act on trg-nil"}, CreatedAt: todayNow,
	}); err != nil {
		t.Fatalf("seed nil-unit trigger: %v", err)
	}
	if err := f.triggers.Fire(ctx, "trg-nil", todayNow); err != nil {
		t.Fatalf("fire nil-unit trigger: %v", err)
	}
}

// contest seeds one Kind's FX-H contest (weights 1.0 against 0.99) and names
// its incumbent-to-be and its challenger.
func (f *fxH) contest(t *testing.T, k focus.Kind) (incumbent, challenger string) {
	t.Helper()
	if k == focus.KindLoad {
		f.loadContest(t, 1.0, 0.99)
		return "C", "D"
	}
	f.taskContest(t, 1.0, 0.99)
	return "A", "B"
}

// digestOrder runs one digest at todayNow and returns the trigger ids it sent,
// in the order the message lists them, and how many items it reported.
func (f *fxH) digestOrder(t *testing.T) (order []string, carried int) {
	t.Helper()
	ch := &sendingChannel{}
	carried, err := f.digestRunner(ch).assembleDigest(context.Background(), todayNow, true)
	if err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if ch.count() != 1 {
		t.Fatalf("the digest sent %d message(s), want 1", ch.count())
	}
	for _, line := range strings.Split(ch.sent[0], "\n• ")[1:] {
		order = append(order, strings.TrimPrefix(line, "act on "))
	}
	return order, carried
}

func mirrorOrder(today Today) []string {
	out := make([]string, len(today.Digest.Items))
	for i, line := range today.Digest.Items {
		out[i] = line.TriggerID
	}
	return out
}

// adjacencyFixture is FX-H plus FX-L, with the incumbent seeded by a first
// request and u-q related to it: trg-2 is the item adjacent to the focus, and
// trg-1, first in the tie-break, the one a digest that ignored adjacency
// would carry ahead of it.
func adjacencyFixture(t *testing.T, k focus.Kind) *fxH {
	t.Helper()
	f := newFxH(t)
	inc, _ := f.contest(t, k)
	f.fxL(t)
	f.request(t)
	f.relate(t, inc, "u-q", 0.9, 0.9)
	return f
}

func TestDigest_AdjacentItemCarriedAhead(t *testing.T) {
	f := adjacencyFixture(t, focus.KindTask)
	order, carried := f.digestOrder(t)
	if carried != prospection.LowEnergyDigestSize {
		t.Fatalf("carried %d, want %d", carried, prospection.LowEnergyDigestSize)
	}
	assertIDs(t, "digest order: trg-2 sits on a unit related to task member A", order, []string{carryHi, carryTwo, carryOne})
}

func TestDigest_ItemAdjacentToLoadFocusCarried(t *testing.T) {
	f := adjacencyFixture(t, focus.KindLoad)
	order, _ := f.digestOrder(t)
	assertIDs(t, "digest order: trg-2 sits on a unit related to load member C", order, []string{carryHi, carryTwo, carryOne})
}

// TestDigest_FreshKeeperHasNoAdjacency is R10's digest half: on a fresh keeper
// the digest has no incumbent, so it reads empty adjacency, though the round it
// is about to publish is already non-empty. The same relations as above, no
// seeding request: the plain tie-break order stands.
func TestDigest_FreshKeeperHasNoAdjacency(t *testing.T) {
	f := newFxH(t)
	inc, _ := f.contest(t, focus.KindTask)
	f.fxL(t)
	f.relate(t, inc, "u-q", 0.9, 0.9)

	order, _ := f.digestOrder(t)
	assertIDs(t, "digest order on a fresh keeper", order, []string{carryHi, carryOne, carryTwo})
}

// mirrorFixture is the P != P' fixture (design §6), run once per Kind: the
// contested Kind's incumbent is displaced by its challenger, trg-1's unit
// (X) sits next to the incumbent and trg-2's (Y) next to the challenger. A
// digest against P carries X first, against P' Y first.
func mirrorFixture(t *testing.T, k focus.Kind) (f *fxH, challenger string) {
	t.Helper()
	f = newFxH(t)
	inc, chal := f.contest(t, k)
	f.fxL(t)
	f.relate(t, inc, "u-p", 0.9, 0.9)
	f.relate(t, chal, "u-q", 0.9, 0.9)
	f.request(t) // seeds P
	return f, chal
}

func TestToday_PendingDigestMatchesDigestOrder(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind focus.Kind
	}{
		{"task focus contested", focus.KindTask},
		{"load focus contested", focus.KindLoad},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, chal := mirrorFixture(t, tc.kind)
			f.setWeight(t, chal, 1.2) // past 1.0*(1+margin): the next request displaces the incumbent

			today := f.request(t)
			task, load := focusIDs(today)
			held := task
			if tc.kind == focus.KindLoad {
				held = load
			}
			if last := held[len(held)-1]; last != chal {
				t.Fatalf("slot 7 = %s, want %s — the fixture must have P != P'", last, chal)
			}

			want := []string{carryHi, carryTwo, carryOne}
			assertIDs(t, "mirror order, read against P' (what the digest would see next)", mirrorOrder(today), want)

			order, _ := f.digestOrder(t)
			assertIDs(t, "a digest sharing the keeper, run next", order, want)

			control, _ := mirrorFixture(t, tc.kind)
			controlOrder, _ := control.digestOrder(t)
			assertIDs(t, "control digest on a keeper seeded with P only", controlOrder, []string{carryHi, carryOne, carryTwo})
		})
	}
}

// TestToday_FirstRequestMirrorUsesNextAdjacencyOnFreshKeeper pins R10's
// carve-out: the mirror previews the digest a request from now, which loads
// the incumbent this very request publishes, so even the first request on a
// fresh keeper orders its low-energy Carry by adjacency, while the focus's own
// ranking, which reads the (empty) loaded incumbent, does not.
func TestToday_FirstRequestMirrorUsesNextAdjacencyOnFreshKeeper(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	f.fxL(t)
	f.relate(t, "A", "u-q", 0.9, 0.9)

	today := f.request(t)

	assertIDs(t, "mirror order on the first request", mirrorOrder(today), []string{carryHi, carryTwo, carryOne})

	candidates, err := f.units.LiveFocusCandidatesByType(context.Background(), focus.Types(focus.KindTask))
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	want := focus.Rank(candidates, map[string]float64{}, todayNow)[:focus.DefaultSize]
	got := today.Focuses[0].Members
	if len(got) != len(want) {
		t.Fatalf("task focus has %d members, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].Candidate.ID || got[i].Score != want[i].Score {
			t.Fatalf("member %d = (%s, %v), want plain Rank's (%s, %v) — the focus's own ranking reads no adjacency on a fresh keeper",
				i, got[i].ID, got[i].Score, want[i].Candidate.ID, want[i].Score)
		}
	}
}

// carryAdjacency's own contract, with no keeper in the way: unit-keyed
// strengths become trigger-keyed, a trigger with no unit or no entry gets none.
func TestCarryAdjacency_ReKeysUnitStrengthsToTriggerIds(t *testing.T) {
	u1, u2, u3 := "u-1", "u-2", "u-3"
	pending := []ports.DueTrigger{
		{ID: "trg-a", UnitID: &u1},
		{ID: "trg-b", UnitID: &u2},
		{ID: "trg-c", UnitID: &u3},
		{ID: "trg-d"},
	}
	got := carryAdjacency(map[string]float64{u1: 0.9, u3: 0.4, "trg-b": 0.7}, pending)

	want := map[string]float64{"trg-a": 0.9, "trg-c": 0.4}
	if len(got) != len(want) {
		t.Fatalf("carryAdjacency = %v, want %v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Fatalf("carryAdjacency[%s] = %v, want %v (full map %v)", id, got[id], w, got)
		}
	}
}

// TestDigest_NotLowEnergyCarriesEveryItemWithAKeeper: adjacency only ever
// reorders and truncates under low energy. With a keeper, a high-energy digest
// still carries all five items in pending's own order, none held.
func TestDigest_NotLowEnergyCarriesEveryItemWithAKeeper(t *testing.T) {
	f := newFxH(t)
	inc, _ := f.contest(t, focus.KindTask)
	f.fxL(t)
	f.state.RecordEnergy(prospection.EnergyReading{Level: 0.9, RecordedAt: todayNow})
	f.request(t)
	f.relate(t, inc, "u-q", 0.9, 0.9)

	order, carried := f.digestOrder(t)
	if carried != 5 {
		t.Fatalf("carried %d, want all 5 — the care gate is off", carried)
	}
	assertIDs(t, "digest order, not low energy", order, []string{carryOne, carryTwo, carryHi, "trg-lo", "trg-nil"})
}

// countingRelations counts ByUnit reads per unit id.
type countingRelations struct {
	ports.RelationRepo
	reads map[string]int
}

func (c *countingRelations) ByUnit(ctx context.Context, id string) ([]ports.Relation, error) {
	c.reads[id]++
	return c.RelationRepo.ByUnit(ctx, id)
}

// TestFocusKeeper_ReadsEachMembersRelationsOnce is design §3.3's bound: one
// round reads the relations of every distinct member of P and P' once, so the
// six fillers both snapshots share are not read twice.
func TestFocusKeeper_ReadsEachMembersRelationsOnce(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	cr := &countingRelations{RelationRepo: f.rels, reads: map[string]int{}}
	k := NewFocusKeeper(f.units, f.cfg, cr)

	first, err := k.compute(context.Background(), todayNow)
	if err != nil {
		t.Fatalf("seeding compute: %v", err)
	}
	k.publish(first)
	f.setWeight(t, "B", 1.2)
	cr.reads = map[string]int{}

	second, err := k.compute(context.Background(), todayNow)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	assertIDs(t, "P' task members", rankedIDs(second.members[focus.KindTask]), append(ids("F", 6), "B"))
	want := map[string]int{"A": 1, "B": 1}
	for _, id := range ids("F", 6) {
		want[id] = 1
	}
	if len(cr.reads) != len(want) {
		t.Fatalf("relations read for %v, want exactly the members of P and P': %v", cr.reads, want)
	}
	for id, n := range want {
		if cr.reads[id] != n {
			t.Fatalf("ByUnit(%s) read %d time(s), want %d (all reads %v)", id, cr.reads[id], n, cr.reads)
		}
	}
}

// TestFocusKeeper_NextRelationsErrorPropagates: on a fresh keeper P is empty, so
// the only relation reads are P' members', and a failure there fails the round.
func TestFocusKeeper_NextRelationsErrorPropagates(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	k := NewFocusKeeper(f.units, f.cfg, failingRelations{f.rels})
	if _, err := k.compute(context.Background(), todayNow); !errors.Is(err, errKeeperBoom) {
		t.Fatalf("compute err = %v, want the relations error", err)
	}
}
