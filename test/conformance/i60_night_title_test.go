package conformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// nightNamer is the scripted unit_title provider of the night's title phase. It
// answers by the unit's content, "content-<id>.", and records the
// order the units were asked in. Nothing but unit_title may be asked.
type nightNamer struct {
	t       *testing.T
	titles  map[string]string // unit id -> raw answer
	failFor string            // unit id whose call returns an error
	asked   []string
	during  func(id string)
}

func (n *nightNamer) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	if req.Task != "unit_title" {
		n.t.Fatalf("night title: unscripted %q call", req.Task)
	}
	for id, raw := range n.titles {
		if strings.Contains(req.Prompt, "content-"+id+".") {
			n.asked = append(n.asked, id)
			if n.during != nil {
				n.during(id)
			}
			if id == n.failFor {
				return ports.LLMResponse{}, errors.New("provider down")
			}
			return ports.LLMResponse{Text: raw}, nil
		}
	}
	n.t.Fatalf("night title: a unit this test did not seed was asked: %s", req.Prompt)
	return ports.LLMResponse{}, nil
}

type nightVault struct {
	units     *memrepo.Units
	triggers  *memrepo.Triggers
	decisions *memrepo.DecisionLog
	now       time.Time
}

func newNightVault() *nightVault {
	return &nightVault{units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(), decisions: memrepo.NewDecisionLog(),
		now: time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)}
}

// seed stores a live untitled unit whose content names its id, dated at event.
func (v *nightVault) seed(t *testing.T, id string, event time.Time, title string) unit.Unit {
	t.Helper()
	at := v.now.Add(-24 * time.Hour)
	u := unit.Unit{ID: id, Type: unit.TypeEvent, Status: unit.StatusPool, Content: "content-" + id + ".", Title: title, EventAt: &event,
		Source: "chat", Weight: 1, LastTouchedAt: at, CreatedAt: at, UpdatedAt: at}
	if err := v.units.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func (v *nightVault) arm(t *testing.T, unitID string, fire time.Time, text string) {
	t.Helper()
	if err := v.triggers.Create(context.Background(), ports.Trigger{ID: "trg-" + unitID, UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		FireAt: &fire, Payload: ports.TriggerPayload{ActionText: text}, CreatedAt: v.now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
}

// night runs the title phase alone against llm (nil: no provider).
func (v *nightVault) night(t *testing.T, llm ports.LLMProvider) error {
	t.Helper()
	svc := brain.NewConsolidateService(fixedClock{now: v.now}, memrepo.NewConfig(), v.units, memrepo.NewRelations(), &counterIDs{}, v.decisions,
		nil, nil, memrepo.NewSelfModel(), memrepo.NewState(), nil, nil, v.triggers).WithNaming(llm)
	phase := consolidation.PhaseTitle
	_, err := svc.Consolidate(context.Background(), brain.ConsolidateRequest{Phase: &phase})
	return err
}

func (v *nightVault) rows(t *testing.T, action ports.DecisionAction) []ports.Decision {
	t.Helper()
	all, err := v.decisions.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	var out []ports.Decision
	for _, r := range all {
		if r.Action == action {
			out = append(out, r)
		}
	}
	return out
}

func (v *nightVault) title(t *testing.T, id string) string {
	t.Helper()
	u, err := v.units.ByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return u.Title
}

func answer(title string) string { return fmt.Sprintf(`{"title":%q}`, title) }

// TestI60_NightNamesInOrderUnderTheCap: the night names at most
// title_backfill_per_pass units: first the unit with a pending reminder, then the
// nearest future instant, then the past ones, most recent first. The oldest past
// units are left for a later night (ADR-0065 point 3).
func TestI60_NightNamesInOrderUnderTheCap(t *testing.T) {
	v := newNightVault()
	titles := map[string]string{}
	add := func(id string, event time.Time) {
		v.seed(t, id, event, "")
		titles[id] = answer("Title " + id)
	}
	add("armed", v.now.Add(500*time.Hour))
	add("future", v.now.Add(2*time.Hour))
	for i := 1; i <= consolidation.TitleBackfillPerPass+1; i++ { // one past the cap's room
		add(fmt.Sprintf("past%02d", i), v.now.Add(-time.Duration(i)*time.Hour))
	}
	v.arm(t, "armed", v.now.Add(time.Hour), "content-armed.")
	llm := &nightNamer{t: t, titles: titles}

	if err := v.night(t, llm); err != nil {
		t.Fatalf("night: %v", err)
	}

	if len(llm.asked) != consolidation.TitleBackfillPerPass {
		t.Fatalf("asked %d units, want the cap %d", len(llm.asked), consolidation.TitleBackfillPerPass)
	}
	if !reflect.DeepEqual(llm.asked[:3], []string{"armed", "future", "past01"}) {
		t.Errorf("asked in order %v, want the pending reminder, the future instant, then the most recent past first", llm.asked[:3])
	}
	last := fmt.Sprintf("past%02d", consolidation.TitleBackfillPerPass-2)
	if llm.asked[len(llm.asked)-1] != last {
		t.Errorf("last asked %q, want %q", llm.asked[len(llm.asked)-1], last)
	}
	for _, id := range []string{"past49", "past50", "past51"} {
		if v.title(t, id) != "" {
			t.Errorf("%s was named beyond the cap", id)
		}
	}
	if got := v.rows(t, ports.ActionTitleNamed); len(got) != consolidation.TitleBackfillPerPass {
		t.Errorf("%d consolidate.title.named rows, want %d", len(got), consolidation.TitleBackfillPerPass)
	}
}

// TestI60_NightWritesOnlyTheTitleAndLogsIt: a unit named by the night keeps its
// content, instants and status, its armed reminder says the title, and the row
// carries the previous (null) and next title. A titled unit is never asked.
func TestI60_NightWritesOnlyTheTitleAndLogsIt(t *testing.T) {
	v := newNightVault()
	event := v.now.Add(30 * time.Hour)
	before := v.seed(t, "u1", event, "")
	v.seed(t, "named", event, "Already named")
	v.arm(t, "u1", event.Add(-time.Hour), "content-u1.")
	llm := &nightNamer{t: t, titles: map[string]string{"u1": answer("Reunión con Juan"), "named": answer("Overwrite")}}

	if err := v.night(t, llm); err != nil {
		t.Fatalf("night: %v", err)
	}

	got, err := v.units.ByID(context.Background(), "u1")
	want := before
	want.Title, want.UpdatedAt = "Reunión con Juan", v.now
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("unit after the night = %+v, %v; want only Title and UpdatedAt changed: %+v", got, err, want)
	}
	if v.title(t, "named") != "Already named" || len(llm.asked) != 1 {
		t.Errorf("a titled unit was touched: title %q, asked %v", v.title(t, "named"), llm.asked)
	}
	rows := v.rows(t, ports.ActionTitleNamed)
	if len(rows) != 1 {
		t.Fatalf("%d consolidate.title.named rows, want 1", len(rows))
	}
	if id, _, next := titleRow(t, rows[0]); id != "u1" || next != "Reunión con Juan" {
		t.Errorf("row names unit %q next %v, want u1 and the title", id, next)
	}
	for _, tr := range v.triggers.All() {
		if tr.Payload.ActionText != "Reunión con Juan" {
			t.Errorf("reminder says %q, want the new title", tr.Payload.ActionText)
		}
	}
	// The exact set of rows the night wrote: the title, then the rewrite of the
	// reminder's text under the naming's own action. Never a correction's: the
	// user corrected nothing, and /ui/activity would say "Reminder moved".
	all, err := v.decisions.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	var actions []ports.DecisionAction
	for _, d := range all {
		actions = append(actions, d.Action)
	}
	if want := []ports.DecisionAction{ports.ActionTitleNamed, ports.ActionTitleReminderRetexted}; !reflect.DeepEqual(actions, want) {
		t.Fatalf("the night wrote rows %v, want exactly %v", actions, want)
	}
	if c := string(all[1].Context); !strings.Contains(c, `"action_text"`) || !strings.Contains(c, "content-u1.") || !strings.Contains(c, "Reunión con Juan") {
		t.Errorf("retext row %s, want the action_text change from the old text to the title", c)
	}
}

// TestI60_NightProviderFailureDoesNotAbortThePass: one unit's provider error and
// one unusable answer are logged as consolidate.title.unnamed with a reason, the
// rest of the pass is named, and the night returns no error.
func TestI60_NightProviderFailureDoesNotAbortThePass(t *testing.T) {
	v := newNightVault()
	for i, id := range []string{"a", "b", "c"} {
		v.seed(t, id, v.now.Add(time.Duration(i+1)*time.Hour), "")
	}
	llm := &nightNamer{t: t, failFor: "a", titles: map[string]string{"a": "x", "b": answer("Reunión 22:30"), "c": answer("Dentista")}}

	if err := v.night(t, llm); err != nil {
		t.Fatalf("night returned %v, want the failure logged and the pass to go on", err)
	}

	if v.title(t, "c") != "Dentista" || v.title(t, "a") != "" || v.title(t, "b") != "" {
		t.Errorf("titles a=%q b=%q c=%q, want only c named", v.title(t, "a"), v.title(t, "b"), v.title(t, "c"))
	}
	rows := v.rows(t, ports.ActionTitleUnnamed)
	if len(rows) != 2 {
		t.Fatalf("%d consolidate.title.unnamed rows, want 2 (a provider error, an unusable answer)", len(rows))
	}
	for _, r := range rows {
		if !strings.Contains(string(r.Context), `"unit_id"`) || !strings.Contains(string(r.Context), `"reason"`) {
			t.Errorf("unnamed row %s lacks unit_id or reason", r.Context)
		}
	}
}

// TestI60_NightWithNoProviderWritesNothing: with no provider the phase touches
// and logs nothing.
func TestI60_NightWithNoProviderWritesNothing(t *testing.T) {
	v := newNightVault()
	v.seed(t, "u1", v.now.Add(time.Hour), "")

	if err := v.night(t, nil); err != nil {
		t.Fatalf("night: %v", err)
	}
	if v.title(t, "u1") != "" {
		t.Error("a unit was named with no provider")
	}
	if all, _ := v.decisions.Since(context.Background(), time.Time{}, -1); len(all) != 0 {
		t.Errorf("%d rows logged with no provider", len(all))
	}
}

// TestI60_NightKeepsATitleWrittenMeanwhile: a title a correction or the user wrote
// while the model was thinking is not overwritten and not logged as the night's.
func TestI60_NightKeepsATitleWrittenMeanwhile(t *testing.T) {
	v := newNightVault()
	v.seed(t, "u1", v.now.Add(time.Hour), "")
	llm := &nightNamer{t: t, titles: map[string]string{"u1": answer("Model title")}}
	llm.during = func(id string) {
		if err := v.units.UpdateTitle(context.Background(), id, "User title", v.now); err != nil {
			t.Fatal(err)
		}
	}

	if err := v.night(t, llm); err != nil {
		t.Fatalf("night: %v", err)
	}
	if v.title(t, "u1") != "User title" {
		t.Errorf("title = %q, want the one written meanwhile", v.title(t, "u1"))
	}
	if n := len(v.rows(t, ports.ActionTitleNamed)); n != 0 {
		t.Errorf("%d consolidate.title.named rows after a lost race, want none", n)
	}
}
