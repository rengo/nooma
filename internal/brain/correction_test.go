package brain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// This file lives inside package brain (white-box), not test/conformance,
// though it proves R1.9/I23 and R1.10 (design D6) at L2 (no I/O, memrepo
// fakes only, always runs). correctionRunner and applyWithPreImage are
// deliberately unexported
// (design D5/D7: "no CorrectionService ... it would have no caller" outside
// this package, the same encapsulation captureRunner already has and
// test/conformance never reaches directly either). A separate importing
// package cannot name or call an unexported symbol it does not declare
// itself — Go's own visibility rule, not a project convention to relax —
// so this test sits beside the code it drives, the way internal/core's own
// L1 tests already do; see tasks.md Conflicts §C5.

// fakeIDs is a deterministic ports.IDGen for this file only, mirroring
// test/conformance/capture_clock_test.go's counterIDs — that type lives in
// package conformance and is not visible here.
type fakeIDs struct{ n int }

func (g *fakeIDs) New() string {
	g.n++
	return "decision-" + string(rune('0'+g.n))
}

func floatPtr(f float64) *float64 { return &f }

// TestApplyWithPreImage_AuditFailureBlocksTheEdit is spec R1.9's own
// Scenario and design D5 Layer 3 — ADR-0016's RED-first audit-failure test
// (I23). The L2 AST guard (test/conformance) proves the two calls are
// ordered in source; this test proves the runtime consequence: a pre-image
// write that fails must leave the referent unit untouched.
func TestApplyWithPreImage_AuditFailureBlocksTheEdit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	originalEventAt := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	target := unit.Unit{
		ID: "u-1", Type: unit.TypeTask, Status: unit.StatusPool,
		Content: "Dentist appointment", EventAt: &originalEventAt,
		Source: "chat", CreatedAt: now, UpdatedAt: now,
	}
	if err := units.Create(ctx, target); err != nil {
		t.Fatalf("seeding target unit: %v", err)
	}

	failErr := errors.New("decision log unavailable")
	r := correctionRunner{units: units, log: memrepo.NewFailingDecisionLog(failErr), ids: &fakeIDs{}}

	newEventAt := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	plan := []correction.Edit{correction.NewEventAtEdit(newEventAt)}

	err := r.applyWithPreImage(ctx, target, plan, referentSource{Source: "explicit"}, now)
	if err == nil {
		t.Fatal("applyWithPreImage error = nil, want the audit-write failure to propagate")
	}
	if !errors.Is(err, failErr) {
		t.Fatalf("applyWithPreImage error = %v, want it to wrap %v", err, failErr)
	}

	got, err := units.ByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("units.ByID(%q): %v", target.ID, err)
	}
	if got.Content != target.Content {
		t.Errorf("Content = %q, want unchanged %q", got.Content, target.Content)
	}
	if got.EventAt == nil || !got.EventAt.Equal(originalEventAt) {
		t.Errorf("EventAt = %v, want unchanged %v — no Update* call may reach ports.UnitRepo when the pre-image write fails", got.EventAt, originalEventAt)
	}
}

// TestApplyWithPreImage_PreImageShape is task 12f-i.3: a successful
// correction writes exactly one correction.applied row whose context
// carries {unit_id, fields, previous, next, referent} per design D5's JSON
// shape, previous/next keyed by column name and equal to what ByID
// returned before the edit; the referent's score keys are present only on
// the recall path and omitted (not zeroed) on the explicit path.
func TestApplyWithPreImage_PreImageShape(t *testing.T) {
	cases := []struct {
		name string
		ref  referentSource
	}{
		{"explicit referent omits score keys", referentSource{Source: "explicit"}},
		{"recall referent carries its scores", referentSource{Source: "recall", Score: floatPtr(0.0328), RunnerUpScore: floatPtr(0.0164), Margin: floatPtr(2.0)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

			units := memrepo.NewUnits()
			target := unit.Unit{
				ID: "u-2", Type: unit.TypeTask, Status: unit.StatusPool,
				Content: "Meeting with Anna on Tuesday", // no EventAt: previous must be null
				Source:  "chat", CreatedAt: now, UpdatedAt: now,
			}
			if err := units.Create(ctx, target); err != nil {
				t.Fatalf("seeding target unit: %v", err)
			}

			log := memrepo.NewDecisionLog()
			signals := memrepo.NewSignals()
			r := correctionRunner{units: units, log: log, signals: signals, ids: &fakeIDs{}}

			newEventAt := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
			plan := []correction.Edit{correction.NewEventAtEdit(newEventAt)}

			if err := r.applyWithPreImage(ctx, target, plan, tc.ref, now); err != nil {
				t.Fatalf("applyWithPreImage: %v", err)
			}

			rows, err := log.Since(ctx, now.Add(-time.Hour), -1)
			if err != nil {
				t.Fatalf("log.Since: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("decision_log has %d rows, want exactly 1: %+v", len(rows), rows)
			}
			row := rows[0]
			if row.Action != ports.ActionCorrectionApplied {
				t.Errorf("Action = %q, want %q", row.Action, ports.ActionCorrectionApplied)
			}

			var body struct {
				UnitID   string                     `json:"unit_id"`
				Fields   []string                   `json:"fields"`
				Previous map[string]json.RawMessage `json:"previous"`
				Next     map[string]json.RawMessage `json:"next"`
				Referent struct {
					Source        string   `json:"source"`
					Score         *float64 `json:"score"`
					RunnerUpScore *float64 `json:"runner_up_score"`
					Margin        *float64 `json:"margin"`
				} `json:"referent"`
			}
			if err := json.Unmarshal(row.Context, &body); err != nil {
				t.Fatalf("Context is not valid JSON: %v (%s)", err, row.Context)
			}

			if body.UnitID != target.ID {
				t.Errorf("unit_id = %q, want %q", body.UnitID, target.ID)
			}
			if len(body.Fields) != 1 || body.Fields[0] != string(correction.FieldEventAt) {
				t.Errorf("fields = %v, want [%q]", body.Fields, correction.FieldEventAt)
			}
			if got := string(body.Previous["event_at"]); got != "null" {
				t.Errorf("previous.event_at = %s, want null — the column was empty before this edit", got)
			}
			gotNext, err := time.Parse(`"`+time.RFC3339+`"`, string(body.Next["event_at"]))
			if err != nil || !gotNext.Equal(newEventAt) {
				t.Errorf("next.event_at = %s, want %v", body.Next["event_at"], newEventAt)
			}

			if body.Referent.Source != tc.ref.Source {
				t.Errorf("referent.source = %q, want %q", body.Referent.Source, tc.ref.Source)
			}
			if tc.ref.Score == nil && body.Referent.Score != nil {
				t.Errorf("referent.score = %v, want omitted on the explicit path", *body.Referent.Score)
			}
			if tc.ref.Score != nil && (body.Referent.Score == nil || *body.Referent.Score != *tc.ref.Score) {
				t.Errorf("referent.score = %v, want %v", body.Referent.Score, *tc.ref.Score)
			}

			got, err := units.ByID(ctx, target.ID)
			if err != nil {
				t.Fatalf("units.ByID(%q): %v", target.ID, err)
			}
			if got.EventAt == nil || !got.EventAt.Equal(newEventAt) {
				t.Errorf("EventAt = %v, want %v — the edit must land after a successful pre-image write", got.EventAt, newEventAt)
			}
			if got.Content != target.Content {
				t.Errorf("Content = %q, want unchanged %q — this plan wrote event_at only", got.Content, target.Content)
			}
		})
	}
}

// TestApplyWithPreImage_RecordsCorrectionSignalAfterSuccess is spec R1.10
// and design D6: a successful correction — the pre-image write and every
// Update* call both landing — leaves exactly one learning_signals-shaped
// row (via the fake ports.SignalRepo) naming signal_type = "correction",
// target_kind = "unit", target_id = the referent unit's id, valence =
// negative, and a context linking back to the accompanying
// correction.applied decision_log row by id.
func TestApplyWithPreImage_RecordsCorrectionSignalAfterSuccess(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	target := unit.Unit{
		ID: "u-3", Type: unit.TypeTask, Status: unit.StatusPool,
		Content: "Dentist appointment", Source: "chat", CreatedAt: now, UpdatedAt: now,
	}
	if err := units.Create(ctx, target); err != nil {
		t.Fatalf("seeding target unit: %v", err)
	}

	log := memrepo.NewDecisionLog()
	signals := memrepo.NewSignals()
	r := correctionRunner{units: units, log: log, signals: signals, ids: &fakeIDs{}}

	newEventAt := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	plan := []correction.Edit{correction.NewEventAtEdit(newEventAt)}

	if err := r.applyWithPreImage(ctx, target, plan, referentSource{Source: "explicit"}, now); err != nil {
		t.Fatalf("applyWithPreImage: %v", err)
	}

	decisionRows, err := log.Since(ctx, now.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("log.Since: %v", err)
	}
	if len(decisionRows) != 1 {
		t.Fatalf("decision_log has %d rows, want exactly 1: %+v", len(decisionRows), decisionRows)
	}
	decisionID := decisionRows[0].ID

	rows, err := signals.Since(ctx, now.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("signals.Since: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("learning_signals has %d rows, want exactly 1: %+v", len(rows), rows)
	}
	s := rows[0]

	if s.Type != ports.SignalCorrection {
		t.Errorf("Type = %q, want %q", s.Type, ports.SignalCorrection)
	}
	if s.Valence != ports.ValenceNegative {
		t.Errorf("Valence = %q, want %q", s.Valence, ports.ValenceNegative)
	}
	if s.TargetKind == nil || *s.TargetKind != ports.TargetKindUnit {
		t.Errorf("TargetKind = %v, want %q", s.TargetKind, ports.TargetKindUnit)
	}
	if s.TargetID == nil || *s.TargetID != target.ID {
		t.Errorf("TargetID = %v, want %q", s.TargetID, target.ID)
	}
	if s.DecisionAction != nil {
		t.Errorf("DecisionAction = %v, want nil — design D6: left nil rather than guessed", s.DecisionAction)
	}

	var body struct {
		UnitID     string   `json:"unit_id"`
		Fields     []string `json:"fields"`
		DecisionID string   `json:"decision_id"`
	}
	if err := json.Unmarshal(s.Context, &body); err != nil {
		t.Fatalf("Context is not valid JSON: %v (%s)", err, s.Context)
	}
	if body.UnitID != target.ID {
		t.Errorf("context.unit_id = %q, want %q", body.UnitID, target.ID)
	}
	if len(body.Fields) != 1 || body.Fields[0] != string(correction.FieldEventAt) {
		t.Errorf("context.fields = %v, want [%q]", body.Fields, correction.FieldEventAt)
	}
	if body.DecisionID != decisionID {
		t.Errorf("context.decision_id = %q, want %q — the accompanying correction.applied row", body.DecisionID, decisionID)
	}
}

// TestApplyWithPreImage_FailedEditRecordsNoSignal is design D6's negative
// case: a correction whose edit does not land must leave no signal — "a
// signal for an edit that failed would teach a future learning pass from an
// event that did not occur". The pre-image write succeeds (it is not what
// fails here), but dispatchEdits fails on an edit plan with no known field,
// so applyWithPreImage must return an error and record.Signal must never be
// called.
func TestApplyWithPreImage_FailedEditRecordsNoSignal(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	target := unit.Unit{
		ID: "u-4", Type: unit.TypeTask, Status: unit.StatusPool,
		Content: "Dentist appointment", Source: "chat", CreatedAt: now, UpdatedAt: now,
	}
	if err := units.Create(ctx, target); err != nil {
		t.Fatalf("seeding target unit: %v", err)
	}

	signals := memrepo.NewSignals()
	r := correctionRunner{units: units, log: memrepo.NewDecisionLog(), signals: signals, ids: &fakeIDs{}}

	// A zero-value Edit has no field recognized by dispatchEdits's switch —
	// its default arm returns an "unknown edit field" error, standing in for
	// any dispatchEdits failure without needing a second failing fake.
	plan := []correction.Edit{{}}

	err := r.applyWithPreImage(ctx, target, plan, referentSource{Source: "explicit"}, now)
	if err == nil {
		t.Fatal("applyWithPreImage error = nil, want the dispatchEdits failure to propagate")
	}

	rows, err := signals.Since(ctx, now.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("signals.Since: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("learning_signals has %d rows, want 0 — a failed edit must record no signal (design D6)", len(rows))
	}
}
