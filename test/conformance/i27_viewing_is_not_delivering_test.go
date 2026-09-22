// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI27_ViewingIsNotDelivering is I27 (docs/06-harness.md §4, doc 02
// §7): rendering Today must call no write method on any port it reads —
// the one mutation this milestone is named for (design §3.6, §8).
//
// The forbidden methods below are enumerated from the six port
// interfaces' own Go source this PR reads against — not from memory —
// and stated here so a reader can check the list against the same files:
// internal/ports/unitrepo.go (UnitRepo: Create, UpdateContent,
// UpdateEventAt, UpdateDueAt, SetStatus, ApplyBoosts are the six methods
// that mutate a row; ByID, LiveByIDs, CountLiveByType, IncompleteOlderThan,
// LiveDecayStates, LiveFocusCandidates and LiveFocusCandidatesByType are
// reads), triggerrepo.go (TriggerRepo: Create, Fire, Surface, Resolve,
// Expire write; Due, Undelivered, Delivered read),
// pendingquestionrepo.go (PendingQuestionRepo: Create, MarkAsked,
// Confirm, Reject, Expire write; Unasked, Open read), staterepo.go
// (StateRepo: OpenHypothesis writes; LastHypothesisAt, LatestEnergy
// read), configrepo.go (ConfigRepo: RecordConsolidationRun writes; Load
// reads), decisionlog.go (DecisionLog: Record writes; Since reads) —
// sixteen distinct method names in total, design §3.6's own count.
func TestI27_ViewingIsNotDelivering(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	units := &i27Units{Units: memrepo.NewUnits(), t: t}
	triggers := &i27Triggers{Triggers: memrepo.NewTriggers(), t: t}
	questions := &i27Questions{PendingQuestions: memrepo.NewPendingQuestions(), t: t}
	state := &i27State{State: memrepo.NewState(), t: t}
	cfg := &i27Config{Config: memrepo.NewConfig(), t: t}
	log := &i27DecisionLog{DecisionLog: memrepo.NewDecisionLog(), t: t}

	const unitID = "u-1"
	if err := units.Units.Create(ctx, unit.Unit{
		ID: unitID, Type: unit.TypeTask, Status: unit.StatusPool,
		Content: "renew the passport", Weight: 1, LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	fireAt := now.Add(-time.Hour)
	if err := triggers.Triggers.Create(ctx, ports.Trigger{
		ID: "trg-1", UnitID: strPtrI27(unitID), Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "renew the passport"}, FireAt: &fireAt, CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	if err := triggers.Triggers.Fire(ctx, "trg-1", now); err != nil {
		t.Fatalf("fire trigger: %v", err)
	}
	const relID = "rel-1"
	questions.EnsureRelation(t, relID, "same_topic", "u-a", "u-b", "plan the offsite", "book the venue")
	if err := questions.PendingQuestions.Create(ctx, ports.PendingQuestion{
		ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: relID, CreatedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed question: %v", err)
	}

	svc := brain.NewTodayService(fixedClock{now: now}, units, cfg, state, triggers, questions, log)

	beforeUndelivered, err := triggers.Undelivered(ctx)
	if err != nil {
		t.Fatalf("Undelivered (before): %v", err)
	}
	beforeUnasked, err := questions.Unasked(ctx)
	if err != nil {
		t.Fatalf("Unasked (before): %v", err)
	}

	// R6's own scenario wording: "/ui is requested three times ...
	// surfaced_at and asked_at remain NULL after all three" — looped
	// rather than called once. TodayService is stateless, so a single call
	// proves the identical postcondition, but this matches the spec's own
	// scenario as written rather than a weaker paraphrase of it.
	for i := 0; i < 3; i++ {
		if _, err := svc.Today(ctx); err != nil {
			t.Fatalf("Today request %d: %v", i+1, err)
		}
	}

	afterUndelivered, err := triggers.Undelivered(ctx)
	if err != nil {
		t.Fatalf("Undelivered (after): %v", err)
	}
	afterUnasked, err := questions.Unasked(ctx)
	if err != nil {
		t.Fatalf("Unasked (after): %v", err)
	}
	if len(beforeUndelivered) != len(afterUndelivered) {
		t.Fatalf("Undelivered() changed from %d to %d rows — viewing Today must never mark a trigger delivered", len(beforeUndelivered), len(afterUndelivered))
	}
	if len(beforeUnasked) != len(afterUnasked) {
		t.Fatalf("Unasked() changed from %d to %d rows — viewing Today must never mark a question asked", len(beforeUnasked), len(afterUnasked))
	}
}

func strPtrI27(s string) *string { return &s }

// i27Fail fails t naming the port and method a write reached — every
// override below is one line calling this, so the sixteen names stay a
// list to read rather than sixteen bespoke messages to keep in sync.
func i27Fail(t *testing.T, port, method string) {
	t.Helper()
	t.Fatalf("TodayService called %s.%s — a view must not write (I27, docs/06-harness.md §4)", port, method)
}

// i27Units wraps memrepo.Units, failing the test on any of UnitRepo's six
// write methods; every other method is promoted from the embedded fake
// unchanged.
type i27Units struct {
	*memrepo.Units
	t *testing.T
}

func (g *i27Units) Create(context.Context, unit.Unit) error {
	i27Fail(g.t, "UnitRepo", "Create")
	return nil
}
func (g *i27Units) UpdateContent(context.Context, string, string, time.Time) error {
	i27Fail(g.t, "UnitRepo", "UpdateContent")
	return nil
}
func (g *i27Units) UpdateEventAt(context.Context, string, time.Time, time.Time) error {
	i27Fail(g.t, "UnitRepo", "UpdateEventAt")
	return nil
}
func (g *i27Units) UpdateDueAt(context.Context, string, time.Time, time.Time) error {
	i27Fail(g.t, "UnitRepo", "UpdateDueAt")
	return nil
}
func (g *i27Units) SetStatus(context.Context, string, unit.Status, unit.Status, time.Time) error {
	i27Fail(g.t, "UnitRepo", "SetStatus")
	return nil
}
func (g *i27Units) ApplyBoosts(context.Context, []weight.Boost, time.Time) error {
	i27Fail(g.t, "UnitRepo", "ApplyBoosts")
	return nil
}

// i27Triggers wraps memrepo.Triggers, failing on TriggerRepo's five write
// methods.
type i27Triggers struct {
	*memrepo.Triggers
	t *testing.T
}

func (g *i27Triggers) Create(context.Context, ports.Trigger) error {
	i27Fail(g.t, "TriggerRepo", "Create")
	return nil
}
func (g *i27Triggers) Fire(context.Context, string, time.Time) error {
	i27Fail(g.t, "TriggerRepo", "Fire")
	return nil
}
func (g *i27Triggers) Surface(context.Context, string, time.Time) error {
	i27Fail(g.t, "TriggerRepo", "Surface")
	return nil
}
func (g *i27Triggers) Resolve(context.Context, string, ports.TriggerResolution, time.Time) error {
	i27Fail(g.t, "TriggerRepo", "Resolve")
	return nil
}
func (g *i27Triggers) Expire(context.Context, string) error {
	i27Fail(g.t, "TriggerRepo", "Expire")
	return nil
}

// i27Questions wraps memrepo.PendingQuestions, failing on
// PendingQuestionRepo's five write methods.
type i27Questions struct {
	*memrepo.PendingQuestions
	t *testing.T
}

func (g *i27Questions) Create(context.Context, ports.PendingQuestion) error {
	i27Fail(g.t, "PendingQuestionRepo", "Create")
	return nil
}
func (g *i27Questions) MarkAsked(context.Context, string, time.Time) error {
	i27Fail(g.t, "PendingQuestionRepo", "MarkAsked")
	return nil
}
func (g *i27Questions) Confirm(context.Context, string, time.Time) error {
	i27Fail(g.t, "PendingQuestionRepo", "Confirm")
	return nil
}
func (g *i27Questions) Reject(context.Context, string, time.Time) error {
	i27Fail(g.t, "PendingQuestionRepo", "Reject")
	return nil
}
func (g *i27Questions) Expire(context.Context, string, time.Time) error {
	i27Fail(g.t, "PendingQuestionRepo", "Expire")
	return nil
}

// i27State wraps memrepo.State, failing on StateRepo's one write method.
type i27State struct {
	*memrepo.State
	t *testing.T
}

func (g *i27State) OpenHypothesis(context.Context, ports.StateHypothesis) error {
	i27Fail(g.t, "StateRepo", "OpenHypothesis")
	return nil
}

// i27Config wraps memrepo.Config, failing on ConfigRepo's one write
// method.
type i27Config struct {
	*memrepo.Config
	t *testing.T
}

func (g *i27Config) RecordConsolidationRun(context.Context, time.Time) error {
	i27Fail(g.t, "ConfigRepo", "RecordConsolidationRun")
	return nil
}

// i27DecisionLog wraps memrepo.DecisionLog, failing on DecisionLog's one
// write method.
type i27DecisionLog struct {
	*memrepo.DecisionLog
	t *testing.T
}

func (g *i27DecisionLog) Record(context.Context, ports.Decision) error {
	i27Fail(g.t, "DecisionLog", "Record")
	return nil
}
