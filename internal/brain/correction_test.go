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
// though it proves R1.9/I23 at L2 (no I/O, memrepo fakes only, always
// runs). correctionRunner and applyWithPreImage are deliberately unexported
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
			r := correctionRunner{units: units, log: log, ids: &fakeIDs{}}

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
