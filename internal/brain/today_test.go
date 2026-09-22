package brain

import (
	"context"
	"fmt"
	"math"
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
	return NewTodayService(fixedClock{now: todayNow}, units, cfg, state, triggers, questions, log)
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

// newDigestParityFixture seeds one undelivered trigger and one queued
// relation question, identically, for two independent runs of the digest —
// one that never called Today, one that called it repeatedly — so
// TestToday_RepeatedRequestsLeaveTheMorningDigestByteIdentical can compare
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

// TestToday_RepeatedRequestsLeaveTheMorningDigestByteIdentical is R6's
// second half: "the morning delivery ... is unaffected by any number of
// Today requests, including zero." A real assembleDigest run over five
// prior Today requests is compared, byte for byte, against the identical
// assembleDigest run over a zero-request baseline — both seeded the same
// way from newDigestParityFixture, so the only difference between the two
// runs is whether Today was ever called.
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
func TestToday_RepeatedRequestsLeaveTheMorningDigestByteIdentical(t *testing.T) {
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
	svc := NewTodayService(fixedClock{now: digestNow}, units, cfg, state, triggers, questions, log)
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
