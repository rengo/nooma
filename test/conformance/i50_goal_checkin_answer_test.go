package conformance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I50 (doc 02 §7, ADR-0054, the client's ruling of 2026-10-28): beside a
// chat, an open goal check-in is listed in the prompt, and only a typed
// answer naming its listed position closes it. Nothing else reaches a goal.

const i50Chat ports.ConversationID = "50"

const i50Question = "¿Seguís con correr una media maratón?"

var (
	i50Now    = time.Date(2026, 10, 28, 20, 0, 0, 0, time.FixedZone("ART", -3*3600))
	i50Pushed = i50Now.Add(-3 * time.Hour)
)

type i50Fixture struct {
	svc       *brain.CaptureService
	llm       *scriptedLLM
	units     *memrepo.Units
	triggers  *memrepo.Triggers
	decisions *memrepo.DecisionLog
	signals   *memrepo.Signals
}

// newI50 holds an open goal check-in, g-run, pushed three hours ago, and an
// open task check-in, t-coffee, pushed after it; then the user talked about
// the coffee, so the goal's push is not the latest exchange.
func newI50(t *testing.T, response string) i50Fixture {
	t.Helper()
	ctx := context.Background()
	f := i50Fixture{llm: &scriptedLLM{response: response}, units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(),
		decisions: memrepo.NewDecisionLog(), signals: memrepo.NewSignals()}
	coffee := unit.Unit{ID: "coffee", Type: unit.TypeTask, Status: unit.StatusPool, Source: "telegram", Content: "Comprar café.",
		Title: "Comprar café", CreatedAt: i50Pushed, UpdatedAt: i50Pushed}
	if err := f.units.Create(ctx, coffee); err != nil {
		t.Fatal(err)
	}
	coffeeID := "coffee"
	for _, trg := range []ports.Trigger{
		{ID: "g-run", Kind: ports.TriggerKindPatternBased, Payload: ports.TriggerPayload{ActionText: i50Question, BeliefID: "b-run"}},
		{ID: "t-coffee", Kind: ports.TriggerKindTimeBased, UnitID: &coffeeID, Payload: ports.TriggerPayload{ActionText: "Comprar café"}},
	} {
		at := i50Pushed
		if trg.ID == "t-coffee" {
			at = i50Pushed.Add(time.Hour)
		}
		trg.FireAt, trg.CreatedAt = &at, at.Add(-time.Hour)
		if err := f.triggers.Create(ctx, trg); err != nil {
			t.Fatal(err)
		}
		if err := f.triggers.Fire(ctx, trg.ID, at); err != nil {
			t.Fatal(err)
		}
		if err := f.triggers.Surface(ctx, trg.ID, at); err != nil {
			t.Fatal(err)
		}
	}
	conv := brain.NewConversation(memrepo.NewConversations())
	conv.Add(ctx, i50Chat, brain.Turn{At: i50Pushed, Speaker: classify.SpeakerPush, Text: i50Question})
	conv.Add(ctx, i50Chat, brain.Turn{At: i50Pushed.Add(time.Hour), Speaker: classify.SpeakerPush, Text: "Recordatorio: Comprar café", UnitIDs: []string{"coffee"}})
	conv.Add(ctx, i50Chat, brain.Turn{At: i50Now.Add(-time.Minute), Speaker: classify.SpeakerUser, Text: "el café lo compro mañana", UnitIDs: []string{"coffee"}})
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	f.svc = brain.NewCaptureService(fixedClock{now: i50Now}, &counterIDs{}, f.units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), f.decisions, f.llm, f.llm, f.llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), f.signals, f.triggers, memrepo.NewTimers(), 0.5, memrepo.NewPendingQuestions()).WithConversation(conv)
	return f
}

func (f i50Fixture) say(t *testing.T, text string, chat ports.ConversationID) {
	t.Helper()
	if _, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: text, Channel: "telegram", Conversation: chat}); err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
}

func (f i50Fixture) open(t *testing.T) map[string]bool {
	t.Helper()
	open, err := f.triggers.Delivered(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, o := range open {
		out[o.ID] = true
	}
	return out
}

func (f i50Fixture) signalTypes(t *testing.T) []ports.SignalType {
	t.Helper()
	var out []ports.SignalType
	for _, s := range signalsOf(t, f.signals) {
		out = append(out, s.Type)
	}
	return out
}

func i50Reading(fields string) string {
	return `{"type":"chitchat","normalized_content":"Sí.","weight":0.1,"decay_rate":0.3,"language":"es"` + fields + `}`
}

// TestI50_TheChatPromptListsTheOpenGoalCheckIn: what Nooma asked is listed, by
// position, and nothing else about the goal is.
func TestI50_TheChatPromptListsTheOpenGoalCheckIn(t *testing.T) {
	f := newI50(t, i50Reading(""))
	f.say(t, "hola", i50Chat)
	if p := f.llm.prompts[0]; !strings.Contains(p, "1) "+i50Question) || !strings.Contains(p, "goal_target") || strings.Contains(p, "b-run") {
		t.Errorf("the prompt does not list the goal check-in as position 1, or shows more of the goal:\n%s", p)
	}
}

func TestI50_AGoalAnswerClosesItsCheckIn(t *testing.T) {
	for _, tc := range []struct {
		outcome, text string
		wantOpen      bool
		want          ports.SignalType
		row           ports.DecisionAction
		resolution    string
	}{
		{"engaged", "sí, ayer salí a correr 5 km", false, ports.SignalNudgeEngaged, ports.ActionCaptureCheckInResolved, `"resolution":"engaged"`},
		{"declined", "dejala descansar", false, ports.SignalNudgeDeclined, ports.ActionCaptureCheckInResolved, `"resolution":"declined"`},
		{"snooze", "no por ahora", true, ports.SignalNudgeAck, ports.ActionCaptureCheckInSnoozed, `"trigger_id":"g-run"`},
	} {
		t.Run(tc.outcome, func(t *testing.T) {
			f := newI50(t, i50Reading(`,"goal_target":1,"goal_outcome":"`+tc.outcome+`"`))
			f.say(t, tc.text, i50Chat)
			open := f.open(t)
			if open["g-run"] != tc.wantOpen || !open["t-coffee"] {
				t.Errorf("open check-ins = %v; want g-run open %v and t-coffee untouched", open, tc.wantOpen)
			}
			got := signalsOf(t, f.signals)
			if len(got) != 1 || got[0].Type != tc.want || *got[0].TargetID != "g-run" {
				t.Errorf("signals = %+v, want one %s about g-run", got, tc.want)
			}
			n := 0
			for _, d := range i40Actions(t, f.decisions) {
				if d.Action == tc.row && strings.Contains(string(d.Context), `"g-run"`) && strings.Contains(string(d.Context), tc.resolution) {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%d %s rows naming g-run with %s, want 1", n, tc.row, tc.resolution)
			}
		})
	}
}

// TestI50_ANudgeAnswerNeverClosesAGoal: the generic check-in answer (a bare
// "sí" read as nudge_outcome engaged) answers the task check-ins the old way
// and never a goal one, which only its own typed answer closes.
func TestI50_ANudgeAnswerNeverClosesAGoal(t *testing.T) {
	f := newI50(t, i50Reading(`,"nudge_outcome":"engaged"`))
	// With the task check-in answered, the goal one is the only open check-in:
	// read alone, the generic answer takes the newest open one, so only the
	// exclusion keeps it off the goal.
	if err := f.triggers.Resolve(context.Background(), "t-coffee", ports.ResolutionEngaged, i50Now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.say(t, "sí", "")
	if open := f.open(t); !open["g-run"] {
		t.Errorf("open = %v: a nudge answer closed the goal check-in", open)
	}
	for _, typ := range f.signalTypes(t) {
		if typ == ports.SignalNudgeEngaged {
			for _, s := range signalsOf(t, f.signals) {
				if *s.TargetID == "g-run" {
					t.Errorf("a nudge answer signalled the goal check-in: %+v", s)
				}
			}
		}
	}
}

// TestI50_ReadAloneAGoalIsNotListed: away from a chat nothing lists the goal,
// so nothing can answer it, even a reading that names a position.
func TestI50_ReadAloneAGoalIsNotListed(t *testing.T) {
	f := newI50(t, i50Reading(`,"goal_target":1,"goal_outcome":"engaged"`))
	f.say(t, "sí", "")
	if strings.Contains(f.llm.prompts[0], i50Question) {
		t.Error("a message read alone listed the goal check-in")
	}
	if open := f.open(t); !open["g-run"] {
		t.Errorf("open = %v: a position never shown closed the goal check-in", open)
	}
}

// TestI50_APositionNotListedClosesNothing: the code binds only what it showed.
func TestI50_APositionNotListedClosesNothing(t *testing.T) {
	for _, target := range []string{`2`, `"unclear"`} {
		f := newI50(t, i50Reading(`,"goal_target":`+target+`,"goal_outcome":"engaged"`))
		f.say(t, "sí", i50Chat)
		if open := f.open(t); !open["g-run"] {
			t.Errorf("goal_target %s closed the goal check-in", target)
		}
		if got := signalsOf(t, f.signals); len(got) != 0 {
			t.Errorf("goal_target %s emitted %+v", target, got)
		}
	}
}

// TestI50_ACorrectionNeverLandsOnAGoal: a correction about the goal's topic is
// a correction of the unit it names; even a reading that adds goal fields to
// it closes no goal check-in, and no unit takes the goal's place.
func TestI50_ACorrectionNeverLandsOnAGoal(t *testing.T) {
	f := newI50(t, `{"type":"correction","normalized_content":"Comprar café de especialidad.","weight":0.6,"decay_rate":0.1,"language":"es",`+
		`"referent":"A","goal_target":1,"goal_outcome":"engaged"}`)
	f.say(t, "el café que sea de especialidad, para después de correr", i50Chat)
	if open := f.open(t); !open["g-run"] {
		t.Errorf("open = %v: a correction closed the goal check-in", open)
	}
	for _, s := range signalsOf(t, f.signals) {
		if s.TargetID != nil && *s.TargetID == "g-run" {
			t.Errorf("a correction signalled the goal check-in: %+v", s)
		}
	}
	if u, err := f.units.ByID(context.Background(), "coffee"); err != nil || !strings.Contains(u.Content, "especialidad") {
		t.Errorf("the correction did not land on the unit it named: %+v, %v", u, err)
	}
}

// TestI50_AGoalAnswerIsTheMessagesOnlyCheckInAnswer: a reading that both
// names the goal and carries a generic outcome for the listed coffee answers
// the goal only. The generic answer is not applied on top of it.
func TestI50_AGoalAnswerIsTheMessagesOnlyCheckInAnswer(t *testing.T) {
	f := newI50(t, i50Reading(`,"goal_target":1,"goal_outcome":"engaged","nudge_outcome":"engaged","referent":"A"`))
	f.say(t, "sí", i50Chat)
	if open := f.open(t); open["g-run"] || !open["t-coffee"] {
		t.Errorf("open = %v; want the goal closed and the coffee check-in untouched", open)
	}
}
