package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

type learningClock struct{ now time.Time }

func (c learningClock) Now() time.Time { return c.now }

type learningIDs struct{ n int }

func (g *learningIDs) New() string { g.n++; return fmt.Sprintf("id-%d", g.n) }

// TestLearningRoute_ShowsTheSummaryAndWritesNothing: GET /learning is the
// summary as JSON, one entry per learned value, and reading it writes no row.
func TestLearningRoute_ShowsTheSummaryAndWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	at := time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)
	log := memrepo.NewDecisionLog()
	if err := log.Record(ctx, ports.Decision{ID: "d-1", Action: ports.ActionConfigUpdated, Rationale: "the user changed it", OccurredAt: at,
		Context: json.RawMessage(`{"fields":["goal_stagnation_days"],"previous":{"goal_stagnation_days":21},"next":{"goal_stagnation_days":35}}`)}); err != nil {
		t.Fatal(err)
	}
	cfg := memrepo.NewConfig()
	if err := cfg.SetGoalStagnationDays(ctx, 35, at); err != nil {
		t.Fatal(err)
	}
	svc := brain.NewLearningService(learningClock{now: at}, &learningIDs{}, memrepo.NewRelations(), cfg, log)

	rec := getRoute(t, Handler(Deps{Version: "test", Learning: svc}), "/learning")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got struct {
		Values []struct {
			Kind       string     `json:"kind"`
			Key        string     `json:"key"`
			Value      string     `json:"value"`
			Pinned     bool       `json:"pinned"`
			Sentence   string     `json:"sentence"`
			At         *time.Time `json:"at"`
			CanUndo    bool       `json:"can_undo"`
			CanRelease bool       `json:"can_release"`
		} `json:"values"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
	if len(got.Values) != 1 {
		t.Fatalf("values = %+v, want the goal cadence alone", got.Values)
	}
	v := got.Values[0]
	if v.Kind != "goal_cadence" || v.Key != "goal_stagnation_days" || v.Value != "35 days" || !v.Pinned ||
		v.Sentence != "You set goal check-ins to wait 35 days in Admin." || v.At == nil || !v.At.Equal(at) || v.CanUndo || !v.CanRelease {
		t.Errorf("value = %+v", v)
	}
	if all, _ := log.Since(ctx, time.Time{}, -1); len(all) != 1 {
		t.Errorf("reading the summary wrote %d rows", len(all)-1)
	}
}

func TestLearningRoute_NotWiredIs503(t *testing.T) {
	t.Parallel()
	if rec := getRoute(t, Handler(Deps{Version: "test"}), "/learning"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
