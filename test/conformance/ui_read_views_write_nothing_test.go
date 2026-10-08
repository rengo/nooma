// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
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

// rvwT is the slice of *testing.T the decorators use, so the classification
// check below can hand them a recorder and watch which methods fail.
type rvwT interface {
	Helper()
	Fatalf(format string, args ...any)
}

func rvwFail(t rvwT, port, method string) {
	t.Helper()
	t.Fatalf("a read view called %s.%s — a GET must not write (G7, I27)", port, method)
}

// rvwSelfModel wraps memrepo.SelfModel, failing the test on SelfModelRepo's
// four write methods and counting the read the beliefs view must make.
type rvwSelfModel struct {
	*memrepo.SelfModel
	t           rvwT
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
	t rvwT
}

func (g *rvwSignals) Record(context.Context, ports.Signal) error {
	rvwFail(g.t, "SignalRepo", "Record")
	return nil
}

// rvwDecisionLog wraps memrepo.DecisionLog, failing on DecisionLog's one
// write.
type rvwDecisionLog struct {
	*memrepo.DecisionLog
	t rvwT
}

func (g *rvwDecisionLog) Record(context.Context, ports.Decision) error {
	rvwFail(g.t, "DecisionLog", "Record")
	return nil
}

// The reads each port is allowed to promote from its memrepo, by name. Every
// other method of the three ports must be overridden by its decorator to fail:
// the decorators embed memrepo types, so a write method added to a port later
// would be promoted silently and the gate would stay green.
var (
	rvwSelfModelReads = []string{"ActiveBeliefs", "BeliefByID", "RetiredBeliefs"}
	rvwSignalReads    = []string{"Since"}
	rvwDecisionReads  = []string{"Since"}
)

// rvwRecorder is an rvwT that counts failures instead of stopping the test.
type rvwRecorder struct{ fails int }

func (r *rvwRecorder) Helper()               {}
func (r *rvwRecorder) Fatalf(string, ...any) { r.fails++ }

// rvwUnclassified returns the methods of port that are neither in reads nor
// overridden by decorator to fail, plus allow-list names the port no longer
// has. A method is probed by calling it with zero arguments: an override fails
// through rec; a promoted memrepo method does not (a panic on the zero
// arguments counts as not failing through rec).
func rvwUnclassified(port reflect.Type, decorator any, rec *rvwRecorder, reads []string) []string {
	allowed := map[string]bool{}
	for _, name := range reads {
		allowed[name] = true
	}
	var bad []string
	dv := reflect.ValueOf(decorator)
	for i := 0; i < port.NumMethod(); i++ {
		name := port.Method(i).Name
		if allowed[name] {
			delete(allowed, name)
			continue
		}
		m := dv.MethodByName(name)
		if !m.IsValid() {
			bad = append(bad, name+" (missing on the decorator)")
			continue
		}
		args := make([]reflect.Value, m.Type().NumIn())
		for j := range args {
			args[j] = reflect.Zero(m.Type().In(j))
		}
		before := rec.fails
		func() {
			defer func() { _ = recover() }()
			m.Call(args)
		}()
		if rec.fails == before {
			bad = append(bad, name)
		}
	}
	for name := range allowed {
		bad = append(bad, name+" (in the read allow-list but not a method of "+port.Name()+")")
	}
	sort.Strings(bad)
	return bad
}

// TestUIReadViewDecoratorsClassifyEveryPortMethod makes the decorators'
// completeness a gate: a method added to SelfModelRepo, SignalRepo or
// DecisionLog fails here until it is either allow-listed as a read or
// overridden to fail.
func TestUIReadViewDecoratorsClassifyEveryPortMethod(t *testing.T) {
	rec := &rvwRecorder{}
	cases := []struct {
		port      reflect.Type
		decorator any
		reads     []string
	}{
		{reflect.TypeOf((*ports.SelfModelRepo)(nil)).Elem(), &rvwSelfModel{SelfModel: memrepo.NewSelfModel(), t: rec}, rvwSelfModelReads},
		{reflect.TypeOf((*ports.SignalRepo)(nil)).Elem(), &rvwSignals{Signals: memrepo.NewSignals(), t: rec}, rvwSignalReads},
		{reflect.TypeOf((*ports.DecisionLog)(nil)).Elem(), &rvwDecisionLog{DecisionLog: memrepo.NewDecisionLog(), t: rec}, rvwDecisionReads},
	}
	for _, c := range cases {
		if bad := rvwUnclassified(c.port, c.decorator, rec, c.reads); len(bad) > 0 {
			t.Errorf("%s has methods the read-view gate does not classify: %v — allow-list the read or override the write to fail", c.port.Name(), bad)
		}
	}
}

// rvwProbePort is a port with one write the decorator below does not guard.
type rvwProbePort interface {
	ports.DecisionLog
	Purge(ctx context.Context) error
}

// rvwProbeDecorator guards Record like the real decorator and forgets Purge,
// which is how a write added to a port later would look.
type rvwProbeDecorator struct {
	*rvwDecisionLog
}

func (rvwProbeDecorator) Purge(context.Context) error { return nil }

// TestUIReadViewClassificationCheckFiresOnAnUnguardedMethod is the probe of
// the check above: it must name exactly the method nobody classified.
func TestUIReadViewClassificationCheckFiresOnAnUnguardedMethod(t *testing.T) {
	rec := &rvwRecorder{}
	dec := rvwProbeDecorator{&rvwDecisionLog{DecisionLog: memrepo.NewDecisionLog(), t: rec}}
	bad := rvwUnclassified(reflect.TypeOf((*rvwProbePort)(nil)).Elem(), dec, rec, rvwDecisionReads)
	if len(bad) != 1 || bad[0] != "Purge" {
		t.Errorf("the check named %v, want exactly [Purge]", bad)
	}

	// And a stale allow-list entry is named too.
	stale := rvwUnclassified(reflect.TypeOf((*ports.DecisionLog)(nil)).Elem(),
		&rvwDecisionLog{DecisionLog: memrepo.NewDecisionLog(), t: rec}, rec, []string{"Since", "Gone"})
	if len(stale) != 1 || !strings.HasPrefix(stale[0], "Gone") {
		t.Errorf("the check named %v, want the stale Gone entry only", stale)
	}
}
