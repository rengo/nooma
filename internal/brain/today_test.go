package brain

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// todayNow is this file's one fixed instant — fixedClock (consolidate_test.go)
// hands it to every TodayService these tests build.
var todayNow = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

func newTodayService(units ports.UnitRepo, cfg ports.ConfigRepo, state ports.StateRepo, triggers ports.TriggerRepo, questions ports.PendingQuestionRepo, log ports.DecisionLog) *TodayService {
	return NewTodayService(fixedClock{now: todayNow}, units, cfg, state, triggers, questions, log, NewFocusKeeper(units, cfg, memrepo.NewRelations()))
}

func seedTodayUnit(t *testing.T, units *memrepo.Units, id string, typ unit.Type, weight float64) {
	t.Helper()
	if err := units.Create(context.Background(), unit.Unit{
		ID: id, Type: typ, Status: unit.StatusPool, Content: id,
		Weight: weight, LastTouchedAt: todayNow, CreatedAt: todayNow, UpdatedAt: todayNow,
	}); err != nil {
		t.Fatalf("seed unit %s: %v", id, err)
	}
}

// wantTopIDs computes what a direct focus.Rank call over the port's own
// read gives for k — the design §8 testing-strategy's own "a fake Rank is
// not needed: compare against focus.Rank called directly" case, so this
// test fails against a focus.Select call (a margin) exactly as it would
// against any other departure from Rank + [:DefaultSize].
func wantTopIDs(t *testing.T, units ports.UnitRepo, k focus.Kind) []string {
	t.Helper()
	candidates, err := units.LiveFocusCandidatesByType(context.Background(), focus.Types(k))
	if err != nil {
		t.Fatalf("LiveFocusCandidatesByType(%q): %v", k, err)
	}
	ranked := focus.Rank(candidates, map[string]float64{}, todayNow)
	if len(ranked) > focus.DefaultSize {
		ranked = ranked[:focus.DefaultSize]
	}
	ids := make([]string, len(ranked))
	for i, r := range ranked {
		ids[i] = r.Candidate.ID
	}
	return ids
}

func memberIDs(f Focus) []string {
	ids := make([]string, len(f.Members))
	for i, m := range f.Members {
		ids[i] = m.ID
	}
	return ids
}

// TestToday_PriorityOnlyTopNPerKind is R5's FOCUS section: Priority-only,
// top focus.DefaultSize per Kind, no type leak between them, no call to
// focus.Select (design §3.6's own rejected-Select table).
func TestToday_PriorityOnlyTopNPerKind(t *testing.T) {
	units := memrepo.NewUnits()

	taskCount := focus.DefaultSize + 2
	for i := 0; i < taskCount; i++ {
		seedTodayUnit(t, units, fmt.Sprintf("task-%02d", i), unit.TypeTask, float64(taskCount-i))
	}
	loadCount := focus.DefaultSize + 1
	for i := 0; i < loadCount; i++ {
		seedTodayUnit(t, units, fmt.Sprintf("load-%02d", i), unit.TypeMentalLoad, float64(loadCount-i))
	}

	svc := newTodayService(units, memrepo.NewConfig(), memrepo.NewState(), memrepo.NewTriggers(), memrepo.NewPendingQuestions(), memrepo.NewDecisionLog())
	today, err := svc.Today(context.Background())
	if err != nil {
		t.Fatalf("Today: %v", err)
	}

	if len(today.Focuses) != len(focus.AllKinds()) {
		t.Fatalf("len(Focuses) = %d, want %d", len(today.Focuses), len(focus.AllKinds()))
	}
	for i, k := range focus.AllKinds() {
		if today.Focuses[i].Kind != k {
			t.Fatalf("Focuses[%d].Kind = %q, want %q — focus.AllKinds()'s own order", i, today.Focuses[i].Kind, k)
		}
	}

	taskFocus, loadFocus := today.Focuses[0], today.Focuses[1]
	if len(taskFocus.Members) != focus.DefaultSize {
		t.Fatalf("task focus has %d members, want focus.DefaultSize (%d)", len(taskFocus.Members), focus.DefaultSize)
	}
	if len(loadFocus.Members) != focus.DefaultSize {
		t.Fatalf("load focus has %d members, want focus.DefaultSize (%d)", len(loadFocus.Members), focus.DefaultSize)
	}
	for _, m := range taskFocus.Members {
		if m.Type == unit.TypeMentalLoad {
			t.Fatalf("task focus contains a mental_load member %q — the type leaked across focuses", m.ID)
		}
	}
	for _, m := range loadFocus.Members {
		if m.Type != unit.TypeMentalLoad {
			t.Fatalf("load focus contains a %q member %q — the type leaked across focuses", m.Type, m.ID)
		}
	}

	if got, want := memberIDs(taskFocus), wantTopIDs(t, units, focus.KindTask); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("task focus members = %v, want %v — focus.Rank + [:DefaultSize], never focus.Select with a margin", got, want)
	}
	if got, want := memberIDs(loadFocus), wantTopIDs(t, units, focus.KindLoad); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("load focus members = %v, want %v — focus.Rank + [:DefaultSize], never focus.Select with a margin", got, want)
	}
}

func seedTodayTrigger(t *testing.T, triggers *memrepo.Triggers, units *memrepo.Units, id, unitID, text string, fireAt time.Time) {
	t.Helper()
	ctx := context.Background()
	uid := unitID
	if err := triggers.Create(ctx, ports.Trigger{
		ID: id, UnitID: &uid, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: text}, FireAt: &fireAt, CreatedAt: todayNow,
	}); err != nil {
		t.Fatalf("seed trigger %s: %v", id, err)
	}
	if err := triggers.Fire(ctx, id, todayNow); err != nil {
		t.Fatalf("fire trigger %s: %v", id, err)
	}
	seedTodayUnit(t, units, unitID, unit.TypeTask, 1)
}

// TestToday_DigestMirrorsCarry is R5's PENDING DIGEST section (design
// §3.6, OR2/OR3's decided defaults): Items is Carry's own carry slice
// joined back to pending by id, Held is a count and never a list, and the
// question slot follows assembleDigest's own rule.
func TestToday_DigestMirrorsCarry(t *testing.T) {
	ctx := context.Background()
	triggers := memrepo.NewTriggers()
	units := memrepo.NewUnits()
	questions := memrepo.NewPendingQuestions()

	const relID = "rel-1"
	questions.EnsureRelation(t, relID, "same_topic", "u-a", "u-b", "plan the offsite", "book the venue")
	if err := questions.Create(ctx, ports.PendingQuestion{ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: relID, CreatedAt: todayNow.Add(-time.Hour)}); err != nil {
		t.Fatalf("seed question: %v", err)
	}

	// Five triggers, identical weight: focus.Rank ties every level down to
	// the id tie-break, so Carry's own ranked order (and therefore which
	// three carry under low energy) is exactly trg-1..trg-5 ascending.
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("trg-%d", i)
		seedTodayTrigger(t, triggers, units, id, "u-"+id, "item "+id, todayNow.Add(-time.Duration(6-i)*time.Hour))
	}

	t.Run("no low-energy reading", func(t *testing.T) {
		svc := newTodayService(units, memrepo.NewConfig(), memrepo.NewState(), triggers, questions, memrepo.NewDecisionLog())
		today, err := svc.Today(ctx)
		if err != nil {
			t.Fatalf("Today: %v", err)
		}
		if today.Digest.LowEnergy {
			t.Fatal("LowEnergy = true with no energy reading")
		}
		if len(today.Digest.Items) != 5 {
			t.Fatalf("len(Items) = %d, want 5 — every undelivered trigger, none held with no low-energy gate", len(today.Digest.Items))
		}
		for i, item := range today.Digest.Items {
			want := fmt.Sprintf("trg-%d", i+1)
			if item.TriggerID != want {
				t.Fatalf("Items[%d].TriggerID = %q, want %q — pending's own (fired_at, id) order", i, item.TriggerID, want)
			}
		}
		if today.Digest.Items[0].Text != "item trg-1" || today.Digest.Items[0].FireAt.IsZero() {
			t.Fatalf("Items[0] = %+v — not joined back to trg-1's own Text/FireAt", today.Digest.Items[0])
		}
		if today.Digest.Held != 0 {
			t.Fatalf("Held = %d, want 0", today.Digest.Held)
		}
		if today.Digest.Question == nil || today.Digest.Question.ID != "q-1" {
			t.Fatalf("Question = %+v, want Unasked()[0] (q-1)", today.Digest.Question)
		}
	})

	t.Run("low energy", func(t *testing.T) {
		state := memrepo.NewState()
		state.RecordEnergy(prospection.EnergyReading{Level: prospection.LowEnergyMax - 0.1, RecordedAt: todayNow.Add(-time.Minute)})
		svc := newTodayService(units, memrepo.NewConfig(), state, triggers, questions, memrepo.NewDecisionLog())
		today, err := svc.Today(ctx)
		if err != nil {
			t.Fatalf("Today: %v", err)
		}
		if !today.Digest.LowEnergy {
			t.Fatal("LowEnergy = false with a low reading")
		}
		if today.Digest.Question != nil {
			t.Fatalf("Question = %+v, want nil — no question on a low-energy day", today.Digest.Question)
		}
		if len(today.Digest.Items) != prospection.LowEnergyDigestSize {
			t.Fatalf("len(Items) = %d, want LowEnergyDigestSize (%d)", len(today.Digest.Items), prospection.LowEnergyDigestSize)
		}
		if today.Digest.Held != 5-prospection.LowEnergyDigestSize {
			t.Fatalf("Held = %d, want %d — the held items are counted, not listed", today.Digest.Held, 5-prospection.LowEnergyDigestSize)
		}
		for i, item := range today.Digest.Items {
			want := fmt.Sprintf("trg-%d", i+1)
			if item.TriggerID != want {
				t.Fatalf("Items[%d].TriggerID = %q, want %q — a Today that listed a held item would fail here", i, item.TriggerID, want)
			}
		}
	})
}

// TestToday_FocusMemberScoreCarriesANaNWithoutCoercion is FocusMember's own
// doc comment made into a test: Score is the literal value focus.Rank
// produced — NaN included — never coerced (PR 7 renders it; this package
// only carries it).
//
// The NaN reaches focus.Rank the honest way — a unit whose stored weight is
// already NaN, one of weight.Effective's own four documented NaN-producing
// shapes (internal/core/weight/decay.go) — rather than hand-written into a
// FocusMember the service never computes.
func TestToday_FocusMemberScoreCarriesANaNWithoutCoercion(t *testing.T) {
	units := memrepo.NewUnits()
	seedTodayUnit(t, units, "task-nan", unit.TypeTask, math.NaN())

	svc := newTodayService(units, memrepo.NewConfig(), memrepo.NewState(), memrepo.NewTriggers(), memrepo.NewPendingQuestions(), memrepo.NewDecisionLog())
	today, err := svc.Today(context.Background())
	if err != nil {
		t.Fatalf("Today: %v", err)
	}

	taskFocus := today.Focuses[0]
	if len(taskFocus.Members) != 1 {
		t.Fatalf("task focus has %d members, want 1", len(taskFocus.Members))
	}
	if got := taskFocus.Members[0].Score; !math.IsNaN(got) {
		t.Fatalf("FocusMember.Score = %v, want NaN — focus.Rank's own literal Score, never coerced to 0", got)
	}
}

// unitsArchivedBetweenReads wraps a real memrepo.Units and drops missing
// from whatever LiveByIDs returns, simulating a candidate
// LiveFocusCandidatesByType already returned getting archived before
// rankFocus's second read runs — design §3.7's N7.
type unitsArchivedBetweenReads struct {
	*memrepo.Units
	missing string
}

func (u unitsArchivedBetweenReads) LiveByIDs(ctx context.Context, ids []string) ([]unit.Unit, error) {
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == u.missing {
			continue
		}
		kept = append(kept, id)
	}
	return u.Units.LiveByIDs(ctx, kept)
}

// TestToday_N7ArchivedBetweenReadsDropsOneMemberSilently is design §3.7's
// N7: LiveFocusCandidatesByType returns a candidate LiveByIDs no longer
// has, because it was archived in the gap between rankFocus's two reads.
// The view shows one member fewer and misreports nothing — no zero-value
// FocusMember standing in for the dropped candidate.
func TestToday_N7ArchivedBetweenReadsDropsOneMemberSilently(t *testing.T) {
	base := memrepo.NewUnits()
	seedTodayUnit(t, base, "task-01", unit.TypeTask, 2)
	seedTodayUnit(t, base, "task-02", unit.TypeTask, 1)
	units := unitsArchivedBetweenReads{Units: base, missing: "task-02"}

	svc := newTodayService(units, memrepo.NewConfig(), memrepo.NewState(), memrepo.NewTriggers(), memrepo.NewPendingQuestions(), memrepo.NewDecisionLog())
	today, err := svc.Today(context.Background())
	if err != nil {
		t.Fatalf("Today: %v", err)
	}

	taskFocus := today.Focuses[0]
	want := len(wantTopIDs(t, base, focus.KindTask)) - 1
	if len(taskFocus.Members) != want {
		t.Fatalf("task focus has %d members, want %d — LiveFocusCandidatesByType found 2, LiveByIDs only 1 (N7)", len(taskFocus.Members), want)
	}
	for _, m := range taskFocus.Members {
		if m.ID == "" || m.Content == "" {
			t.Fatalf("FocusMember %+v carries a zero value — N7's dropped candidate must vanish, not survive as an empty entry", m)
		}
		if m.ID == "task-02" {
			t.Fatalf("task focus still contains %q, which LiveByIDs no longer has", m.ID)
		}
	}
}

// newDigestParityFixture seeds one undelivered trigger and one queued
// relation question, identically, for two independent runs of the digest —
// one that never called Today, one that called it repeatedly — so
// TestToday_RepeatedRequestsLeaveDeliveryBookkeepingUnchanged can compare
// what each run's real assembleDigest sends.
//
// Every port the digest reads is backed by a real memrepo implementation
// here, not digest_test.go's own inert undeliveredTriggers/digestUnits
// stubs — those replay a fixed slice regardless of what Surface writes to
// them, which would make a write performed during a Today request unable
// to ever reach the later assembleDigest read, and the parity test exists
// to detect exactly that write. seedTodayTrigger seeds through the same
// Create+Fire production write path test/conformance's I27 guard uses.
func newDigestParityFixture(t *testing.T) (*memrepo.Triggers, *memrepo.Units, *memrepo.PendingQuestions, *memrepo.DecisionLog, *memrepo.State, *memrepo.Config) {
	t.Helper()
	triggers := memrepo.NewTriggers()
	units := memrepo.NewUnits()
	seedTodayTrigger(t, triggers, units, "trg-1", "u-1", "renew the passport", digestNow.Add(-time.Hour))
	questions := queuedQuestions(t, question("q-1", digestQuestionNow, "plan the offsite", "the travel budget"))
	log := memrepo.NewDecisionLog()
	return triggers, units, questions, log, memrepo.NewState(), memrepo.NewConfig()
}

// TestToday_RepeatedRequestsLeaveDeliveryBookkeepingUnchanged is R6's
// second half, narrowed by m4c: delivery bookkeeping (the surfaced, asked
// and held rows) is unchanged by any number of Today requests, including
// zero. A real assembleDigest run over five prior Today requests is
// compared, byte for byte, against the identical assembleDigest run over a
// zero-request baseline — both seeded the same way from
// newDigestParityFixture, so the only difference between the two runs is
// whether Today was ever called.
//
// It no longer claims the whole digest is unaffected. Since m4c, viewing
// /ui can change a later low-energy digest's order, because Today publishes
// the focus it selected and the digest reads adjacency against it. This
// test cannot observe that, and says so: questionRunner builds a keeper-free
// checkRunner (r.focus == nil), so the digest it runs reads no incumbent at
// all. The shared-keeper behaviour is pinned where a keeper exists
// (TestDigest_TodaySeesDigestIncumbent and the wiring test).
//
// newDigestParityFixture backs every port with a real memrepo
// implementation rather than a stub replaying a fixed slice, so a write
// Today makes during those five requests can reach the later digest —
// but only on the ports assembleDigest actually reads, and only where
// the write changes what it renders. Injecting a TriggerRepo.Surface or
// a PendingQuestionRepo.MarkAsked into todayRunner.at turns this red;
// both confirmed by injection, reverted before commit. A DecisionLog
// write turns it red only in one shape — an ActionCheckDigestHeld record
// carrying a held trigger's ID, which heldCounts reads; a Record of any
// other action passes unnoticed, also confirmed by injection.
//
// Two of the ports Today reads are invisible here, structurally, and this
// test does not pretend otherwise: checkRunner carries no ConfigRepo at
// all, so no ConfigRepo write can ever reach assembleDigest; and
// StateRepo's only write, OpenHypothesis, appends to a row set
// LatestEnergy never reads. I27 (test/conformance) is the guard for
// those, and for write-avoidance in general — this test proves the
// narrower thing its name says, that the delivery itself is unchanged.
func TestToday_RepeatedRequestsLeaveDeliveryBookkeepingUnchanged(t *testing.T) {
	ctx := context.Background()

	// Baseline: zero Today requests before the digest assembles.
	baseTriggers, baseUnits, baseQuestions, baseLog, baseState, _ := newDigestParityFixture(t)
	baseCh := &sendingChannel{}
	baseCarried, err := questionRunner(t, baseTriggers, baseUnits, baseState, baseLog, baseCh, baseQuestions).
		assembleDigest(ctx, digestNow, true)
	if err != nil {
		t.Fatalf("baseline assembleDigest: %v", err)
	}

	// Five Today requests, with no morning digest run in between, over an
	// identically seeded fixture.
	triggers, units, questions, log, state, cfg := newDigestParityFixture(t)
	svc := NewTodayService(fixedClock{now: digestNow}, units, cfg, state, triggers, questions, log, NewFocusKeeper(units, cfg, memrepo.NewRelations()))
	for i := 0; i < 5; i++ {
		if _, err := svc.Today(ctx); err != nil {
			t.Fatalf("Today request %d: %v", i+1, err)
		}
	}
	ch := &sendingChannel{}
	carried, err := questionRunner(t, triggers, units, state, log, ch, questions).
		assembleDigest(ctx, digestNow, true)
	if err != nil {
		t.Fatalf("assembleDigest after five Today requests: %v", err)
	}

	if carried != baseCarried {
		t.Fatalf("carried %d after five Today requests, want %d — the zero-request baseline", carried, baseCarried)
	}
	if got, want := ch.count(), baseCh.count(); got != want {
		t.Fatalf("sent %d digest(s) after five Today requests, want %d — the zero-request baseline", got, want)
	}
	if got, want := strings.Join(ch.sent, "\x00"), strings.Join(baseCh.sent, "\x00"); got != want {
		t.Fatalf("digest text after five Today requests =\n%q\nwant byte-identical to the zero-request baseline:\n%q", got, want)
	}
}

// focusIDs returns the two Focuses' member ids, task first.
func focusIDs(today Today) (task, load []string) {
	return memberIDs(today.Focuses[0]), memberIDs(today.Focuses[1])
}

// TestToday_IncumbentHeldInsideMargin is R1, both halves, on FX-H: A holds
// slot 7 from a seeding request; a challenger B inside A's margin (1.03 <
// 1.0*1.05) does not displace it, and one beyond it (1.06) does.
func TestToday_IncumbentHeldInsideMargin(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	fill := ids("F", 6)

	seed, _ := focusIDs(f.request(t))
	assertIDs(t, "seeding request task focus", seed, append(slices.Clone(fill), "A"))

	f.setWeight(t, "B", 1.03)
	held, _ := focusIDs(f.request(t))
	assertIDs(t, "task focus with B inside the margin", held, append(slices.Clone(fill), "A"))

	f.setWeight(t, "B", 1.06)
	displaced, _ := focusIDs(f.request(t))
	assertIDs(t, "task focus with B beyond the margin", displaced, append(slices.Clone(fill), "B"))
}

// TestToday_ConfiguredZeroMarginDisplaces is R2: the margin is read from
// config on every request, so a configured 0 lets a 1% challenger displace
// where the 5% default would hold, and a configured 0.5 holds against a
// challenger the default would let in.
func TestToday_ConfiguredZeroMarginDisplaces(t *testing.T) {
	cases := []struct {
		name   string
		margin float64
		b      float64
		want   string
	}{
		{"zero margin: a 1% challenger displaces", 0, 1.01, "B"},
		{"wide margin: a 40% challenger is held off", 0.5, 1.4, "A"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFxH(t)
			f.taskContest(t, 1.0, 0.99)
			f.request(t) // seeds P = {F1..F6, A} under the default margin

			f.cfg.SeedConfig(t, ports.VaultConfig{HysteresisMargin: &tc.margin})
			f.setWeight(t, "B", tc.b)
			got, _ := focusIDs(f.request(t))
			assertIDs(t, "task focus", got, append(ids("F", 6), tc.want))
		})
	}
}

// TestToday_KindsAreIndependent is R4: both Kinds hold inside the margin; then
// only the load challenger crosses it, and only the load focus changes.
func TestToday_KindsAreIndependent(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	f.loadContest(t, 1.0, 0.99)
	f.request(t)

	f.setWeight(t, "B", 1.03)
	f.setWeight(t, "D", 1.03)
	task, load := focusIDs(f.request(t))
	assertIDs(t, "task focus, both challengers inside their margins", task, append(ids("F", 6), "A"))
	assertIDs(t, "load focus, both challengers inside their margins", load, append(ids("G", 6), "C"))

	f.setWeight(t, "D", 1.06)
	task, load = focusIDs(f.request(t))
	assertIDs(t, "task focus, only the load challenger moved", task, append(ids("F", 6), "A"))
	assertIDs(t, "load focus, its challenger crossed the margin", load, append(ids("G", 6), "D"))
}

// TestToday_HeldMemberShowsItsOwnRankScore is R7, on a fixture where Select's
// order differs from Rank's: A (weight 1.0, adjacent to F1 at strength 0.4, so
// Rank scores it 1.0*(1+0.25*0.4) = 1.1) is held over D (1.12, not an
// incumbent). Rank orders D before A; Select, which lifts incumbents by the
// margin (A: 1.1*1.05 = 1.155 > 1.12), orders A before D. Both are in the top
// 7 (F1..F5 fill the rest), so the order and the score of each member are the
// observable.
func TestToday_HeldMemberShowsItsOwnRankScore(t *testing.T) {
	f := newFxH(t)
	f.fillers(t, unit.TypeTask, "F", 5)
	seedTodayUnit(t, f.units, "A", unit.TypeTask, 1.0)
	seedTodayUnit(t, f.units, "D", unit.TypeTask, 0.4)
	seedTodayUnit(t, f.units, "E", unit.TypeTask, 0.5)
	f.relate(t, "F1", "A", 0.4, 0.4)

	seed, _ := focusIDs(f.request(t))
	assertIDs(t, "seeding request task focus", seed, []string{"F1", "F2", "F3", "F4", "F5", "A", "E"})

	f.setWeight(t, "D", 1.12)
	today := f.request(t)
	got := today.Focuses[0].Members
	assertIDs(t, "task focus order (Select's)", memberIDs(today.Focuses[0]), []string{"F1", "F2", "F3", "F4", "F5", "A", "D"})

	byID := map[string]float64{}
	for _, m := range got {
		byID[m.ID] = m.Score
	}
	for id, want := range map[string]float64{"A": 1.1, "D": 1.12, "F1": 11, "F2": 10} {
		if math.Abs(byID[id]-want) > 1e-9 {
			t.Errorf("Score of %s = %v, want %v — the literal Rank value, adjacency included", id, byID[id], want)
		}
	}
}

// TestToday_FailedRequestPublishesNothing: a request that fails after its
// round was computed leaves the incumbent where it was. A holds P; B jumps
// past the margin on a request that fails at questions.Open; once B falls
// back inside the margin a successful request must still find A held.
func TestToday_FailedRequestPublishesNothing(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	f.request(t)

	f.setWeight(t, "B", 1.06)
	f.questions.failOpen = true
	if _, err := f.svc.Today(context.Background()); !errors.Is(err, errKeeperBoom) {
		t.Fatalf("Today err = %v, want the questions error", err)
	}
	f.questions.failOpen = false

	f.setWeight(t, "B", 1.03)
	got, _ := focusIDs(f.request(t))
	assertIDs(t, "task focus after a failed request", got, append(ids("F", 6), "A"))
}

// The R8 fixture: A (1.0) is the incumbent; B and C both weigh 0.9, so on
// their own neither displaces A (0.9 < 1.0*1.05). B has a relation of
// strength 0.9 to A, so adjacency lifts it to 0.9*(1+0.25*0.9) = 1.1025,
// which clears A*(1.05) = 1.05. C has no relation and stays out.
func r8Fixture(t *testing.T, relate func(f *fxH)) *fxH {
	t.Helper()
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.9)
	seedTodayUnit(t, f.units, "C", unit.TypeTask, 0.9)
	seed, _ := focusIDs(f.request(t))
	assertIDs(t, "seeding request task focus", seed, append(ids("F", 6), "A"))
	relate(f)
	return f
}

func TestToday_AdjacentCandidateOutranksEqualUnrelatedOne(t *testing.T) {
	f := r8Fixture(t, func(f *fxH) { f.relate(t, "A", "B", 0.9, 0.9) })
	got, _ := focusIDs(f.request(t))
	assertIDs(t, "task focus: B (related to A) displaces A, C (unrelated) does not", got, append(ids("F", 6), "B"))
}

// TestToday_AdjacencyUsesStrengthNotConfidence: doc 02 §4 defines strength as
// relevance and confidence as certainty. 0.9/0.1 lifts B; 0.1/0.9 does not.
func TestToday_AdjacencyUsesStrengthNotConfidence(t *testing.T) {
	t.Run("strong and unsure lifts", func(t *testing.T) {
		f := r8Fixture(t, func(f *fxH) { f.relate(t, "A", "B", 0.9, 0.1) })
		got, _ := focusIDs(f.request(t))
		assertIDs(t, "task focus", got, append(ids("F", 6), "B"))
	})
	t.Run("weak and certain does not", func(t *testing.T) {
		f := r8Fixture(t, func(f *fxH) { f.relate(t, "A", "B", 0.1, 0.9) })
		got, _ := focusIDs(f.request(t))
		assertIDs(t, "task focus", got, append(ids("F", 6), "A"))
	})
}

// TestToday_AdjacencyIsUndirected: a relation stored B -> A lifts B exactly as
// A -> B does.
func TestToday_AdjacencyIsUndirected(t *testing.T) {
	for _, dir := range [][2]string{{"A", "B"}, {"B", "A"}} {
		t.Run(dir[0]+" to "+dir[1], func(t *testing.T) {
			f := r8Fixture(t, func(f *fxH) { f.relate(t, dir[0], dir[1], 0.9, 0.9) })
			got, _ := focusIDs(f.request(t))
			assertIDs(t, "task focus", got, append(ids("F", 6), "B"))
		})
	}
}

// TestToday_AdjacencyFromSecondMember: the relation joins B to F2, not to the
// first member of the incumbent or to A. Every member's relations are read.
func TestToday_AdjacencyFromSecondMember(t *testing.T) {
	f := r8Fixture(t, func(f *fxH) { f.relate(t, "F2", "B", 0.9, 0.9) })
	got, _ := focusIDs(f.request(t))
	assertIDs(t, "task focus", got, append(ids("F", 6), "B"))
}

// TestToday_LoadFocusReadsItsOwnMembersRelations: the same shape in the load
// Kind. The relation joins D to the load incumbent C; the task focus is
// present and unrelated, so only a sweep that reads the load members' relations
// can lift D (0.9 -> 1.1025 > 1.0*1.05).
func TestToday_LoadFocusReadsItsOwnMembersRelations(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	f.loadContest(t, 1.0, 0.9)
	seedTodayUnit(t, f.units, "E", unit.TypeMentalLoad, 0.9)
	f.request(t)

	f.relate(t, "C", "D", 0.9, 0.9)
	task, load := focusIDs(f.request(t))
	assertIDs(t, "load focus: D (related to C) displaces C", load, append(ids("G", 6), "D"))
	assertIDs(t, "task focus is untouched", task, append(ids("F", 6), "A"))
}

// TestToday_TaskNotLiftedByLoadIncumbent is OQ4: each focus reads adjacency
// against its OWN incumbent. B (task, 0.9) is related to the load incumbent C
// at strength 0.9; a union reading would lift B past A, the per-Kind reading
// must not.
func TestToday_TaskNotLiftedByLoadIncumbent(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.9)
	f.loadContest(t, 1.0, 0.9)
	f.request(t)

	f.relate(t, "C", "B", 0.9, 0.9)
	task, load := focusIDs(f.request(t))
	assertIDs(t, "task focus: a load incumbent's relation does not lift a task challenger", task, append(ids("F", 6), "A"))
	assertIDs(t, "load focus", load, append(ids("G", 6), "C"))
}

// TestToday_FreshKeeperRanksWithoutAdjacency is R10's own-ranking half: on a
// fresh keeper there is no incumbent, so the first request ranks with empty
// adjacency even over a graph full of relations. Every member's Score equals
// plain focus.Rank's.
func TestToday_FreshKeeperRanksWithoutAdjacency(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	f.relate(t, "F1", "A", 0.9, 0.9)
	f.relate(t, "A", "B", 0.9, 0.9)

	today := f.request(t)

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
			t.Fatalf("member %d = (%s, %v), want plain Rank's (%s, %v)", i, got[i].ID, got[i].Score, want[i].Candidate.ID, want[i].Score)
		}
	}
}
