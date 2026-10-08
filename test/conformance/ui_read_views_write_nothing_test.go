// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestUIReadViewsWriteNothing is G7 (m4e design 3.12), I27's shape applied to
// the mirror's read views beyond Today: a GET renders and writes nothing.
// Today's own proof is TestI27_ViewingIsNotDelivering; this one drives each
// later view through the real handler stack over write-counting decorators
// of every repo its service holds, and fails the moment any write method is
// reached. The beliefs GET is the first entry; the activity and admin GETs
// add theirs in their own changes.
//
// The forbidden methods are enumerated from internal/ports: SelfModelRepo's
// four writes (UpsertByTopicKey, ReinforceByID, SetStatus, EditContent),
// SignalRepo.Record and DecisionLog.Record; ActiveBeliefs, RetiredBeliefs,
// BeliefByID and the two Since reads are reads.
func TestUIReadViewsWriteNothing(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	selfModel := &rvwSelfModel{SelfModel: memrepo.NewSelfModel(), t: t}
	signals := &rvwSignals{Signals: memrepo.NewSignals(), t: t}
	log := &rvwDecisionLog{DecisionLog: memrepo.NewDecisionLog(), t: t}

	seed := []ports.Belief{
		{ID: "b-goal", Facet: selfmodel.FacetGoal, TopicKey: "goal/run", Content: "run a marathon", Confidence: 0.8, Origin: selfmodel.OriginDerived, Status: selfmodel.StatusActive},
		{ID: "b-value", Facet: selfmodel.FacetValue, TopicKey: "value/honesty", Content: "honesty first", Confidence: 0.6, Origin: selfmodel.OriginUserStated, Status: selfmodel.StatusActive},
		{ID: "b-gone", Facet: selfmodel.FacetGoal, TopicKey: "goal/old", Content: "a retired goal", Confidence: 0.9, Origin: selfmodel.OriginDerived, Status: selfmodel.StatusRetired},
	}
	for _, b := range seed {
		b.LastReinforcedAt, b.CreatedAt, b.UpdatedAt = now, now, now
		if err := selfModel.SelfModel.UpsertByTopicKey(ctx, b); err != nil {
			t.Fatalf("seed belief %s: %v", b.ID, err)
		}
	}

	svc := brain.NewBeliefsService(fixedClock{now: now}, &counterIDs{}, selfModel, signals, log)
	h := httpapi.Handler(httpapi.Deps{Version: "test", UI: ui.New(ui.Deps{Beliefs: svc})})

	// Three requests, I27's own loop: a write that needs a second request to
	// show (a cursor, a lazily seeded row) would not hide behind one.
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/beliefs", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/beliefs request %d = %d, want 200: %s", i+1, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{"run a marathon", "honesty first"} {
			if !strings.Contains(body, want) {
				t.Fatalf("GET /ui/beliefs request %d does not render %q — the view must actually run for this gate to mean anything:\n%s", i+1, want, body)
			}
		}
		if strings.Contains(body, "a retired goal") {
			t.Errorf("GET /ui/beliefs request %d renders a retired belief", i+1)
		}
	}
	if selfModel.activeReads != 3 {
		t.Errorf("ActiveBeliefs was read %d time(s) over three requests, want 3 — the view did not go through the decorated repo", selfModel.activeReads)
	}
}

func rvwFail(t *testing.T, port, method string) {
	t.Helper()
	t.Fatalf("a read view called %s.%s — a GET must not write (G7, I27)", port, method)
}

// rvwSelfModel wraps memrepo.SelfModel, failing the test on SelfModelRepo's
// four write methods and counting the read the beliefs view must make.
type rvwSelfModel struct {
	*memrepo.SelfModel
	t           *testing.T
	activeReads int
}

func (g *rvwSelfModel) ActiveBeliefs(ctx context.Context) ([]ports.Belief, error) {
	g.activeReads++
	return g.SelfModel.ActiveBeliefs(ctx)
}

func (g *rvwSelfModel) UpsertByTopicKey(context.Context, ports.Belief) error {
	rvwFail(g.t, "SelfModelRepo", "UpsertByTopicKey")
	return nil
}

func (g *rvwSelfModel) ReinforceByID(context.Context, string, float64, time.Time) error {
	rvwFail(g.t, "SelfModelRepo", "ReinforceByID")
	return nil
}

func (g *rvwSelfModel) SetStatus(context.Context, string, selfmodel.Status, selfmodel.Status, time.Time) error {
	rvwFail(g.t, "SelfModelRepo", "SetStatus")
	return nil
}

func (g *rvwSelfModel) EditContent(context.Context, string, string, string, time.Time) error {
	rvwFail(g.t, "SelfModelRepo", "EditContent")
	return nil
}

// rvwSignals wraps memrepo.Signals, failing on SignalRepo's one write.
type rvwSignals struct {
	*memrepo.Signals
	t *testing.T
}

func (g *rvwSignals) Record(context.Context, ports.Signal) error {
	rvwFail(g.t, "SignalRepo", "Record")
	return nil
}

// rvwDecisionLog wraps memrepo.DecisionLog, failing on DecisionLog's one
// write.
type rvwDecisionLog struct {
	*memrepo.DecisionLog
	t *testing.T
}

func (g *rvwDecisionLog) Record(context.Context, ports.Decision) error {
	rvwFail(g.t, "DecisionLog", "Record")
	return nil
}
