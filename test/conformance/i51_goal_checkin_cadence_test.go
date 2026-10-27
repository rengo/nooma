package conformance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakechannel"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I51 (doc 02 §7, ADR-0054): a stagnant goal is asked about in the morning
// digest, answered by naming it, and asked again only at its cadence:
// goal_stagnation_days after the last check-in, twice that after a decline.

const i51Chat ports.ConversationID = "12449194"

const i51Goal = "correr una media maratón"

// i51Asked is the whole morning digest that asks the goal alone.
const i51Asked = "¿Seguís con «" + i51Goal + "»?"

var i51Day0 = time.Date(2026, 11, 2, 0, 0, 0, 0, time.FixedZone("ART", -3*3600))

func i51At(day, hour, minute int) time.Time {
	return i51Day0.AddDate(0, 0, day).Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

type i51Vault struct {
	units     *memrepo.Units
	selfModel *memrepo.SelfModel
	triggers  *memrepo.Triggers
	log       *memrepo.DecisionLog
	signals   *memrepo.Signals
	cfg       *memrepo.Config
	channel   *fakechannel.Fake
	conv      *brain.Conversation
	ids       *counterIDs
	state     *memrepo.State
}

func newI51Vault(t *testing.T) *i51Vault {
	t.Helper()
	ctx := context.Background()
	v := &i51Vault{units: memrepo.NewUnits(), selfModel: memrepo.NewSelfModel(), triggers: memrepo.NewTriggers(),
		log: memrepo.NewDecisionLog(), signals: memrepo.NewSignals(), cfg: memrepo.NewConfig(), channel: fakechannel.New(),
		conv: brain.NewConversation(memrepo.NewConversations()), ids: &counterIDs{}, state: memrepo.NewState()}
	long := i51Day0.AddDate(0, 0, -30)
	// One unit in Spanish, so Nooma speaks the user's language (ADR-0035).
	if err := v.units.Create(ctx, unit.Unit{ID: "note", Type: unit.TypeKnowledge, Status: unit.StatusPool, Source: "telegram",
		Content: "Hoy llovió.", Language: "es", Weight: 1, LastTouchedAt: long, CreatedAt: long, UpdatedAt: long}); err != nil {
		t.Fatal(err)
	}
	if err := v.selfModel.UpsertByTopicKey(ctx, ports.Belief{ID: "b-run", Facet: selfmodel.FacetGoal, TopicKey: "goal/run", Content: i51Goal,
		Confidence: 0.8, Origin: selfmodel.OriginUserStated, Status: selfmodel.StatusActive, LastReinforcedAt: long, CreatedAt: long, UpdatedAt: long}); err != nil {
		t.Fatal(err)
	}
	return v
}

// night runs pattern_eval at 03:00 of day.
func (v *i51Vault) night(t *testing.T, day int) {
	t.Helper()
	phase := consolidation.PhasePatternEval
	svc := brain.NewConsolidateService(fixedClock{now: i51At(day, 3, 0)}, v.cfg, v.units, memrepo.NewRelations(), v.ids, v.log, nil, nil,
		v.selfModel, memrepo.NewState(), memrepo.NewPendingQuestions(), v.signals, v.triggers)
	if _, err := svc.Consolidate(context.Background(), brain.ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("pattern_eval on day %d: %v", day, err)
	}
}

// morning runs the check pass just after the digest hour of day and returns
// what it sent.
func (v *i51Vault) morning(t *testing.T, day int) []string {
	t.Helper()
	return v.check(t, day, 7, 5)
}

// check runs one check pass at hour:minute of day and returns what it sent.
func (v *i51Vault) check(t *testing.T, day, hour, minute int) []string {
	t.Helper()
	before := len(v.channel.Sent(t))
	svc := brain.NewCheckService(fixedClock{now: i51At(day, hour, minute)}, v.triggers, memrepo.NewTimers(), v.ids, v.log, v.channel,
		v.units, v.state, nil, i51Chat, nil, nil, v.signals, nil).WithConversation(v.conv)
	if _, err := svc.Check(context.Background(), brain.CheckRequest{}); err != nil {
		t.Fatalf("check on day %d: %v", day, err)
	}
	var texts []string
	for _, m := range v.channel.Sent(t)[before:] {
		texts = append(texts, m.Text)
	}
	return texts
}

// answer captures the user's reply in the chat at 09:00 of day, read as the
// goal answer outcome.
func (v *i51Vault) answer(t *testing.T, day int, text, outcome string) {
	t.Helper()
	ctx := context.Background()
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := &scriptedLLM{response: `{"type":"chitchat","normalized_content":"Sí.","weight":0.1,"decay_rate":0.3,"language":"es","goal_target":1,"goal_outcome":"` + outcome + `"}`}
	svc := brain.NewCaptureService(fixedClock{now: i51At(day, 9, 0)}, v.ids, v.units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), v.log, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx),
		v.signals, v.triggers, memrepo.NewTimers(), 0.5, memrepo.NewPendingQuestions()).WithConversation(v.conv)
	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: text, Channel: "telegram", Conversation: i51Chat}); err != nil {
		t.Fatalf("answer on day %d: %v", day, err)
	}
	if len(llm.prompts) == 0 || !strings.Contains(llm.prompts[0], "Seguís con") {
		t.Fatalf("the answer on day %d was not read beside the goal check-in", day)
	}
}

func (v *i51Vault) armed(t *testing.T) int {
	t.Helper()
	all, err := v.log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range all {
		if d.Action == ports.ActionPatternEvalGoalCheckInArmed {
			n++
		}
	}
	return n
}

func (v *i51Vault) rows(t *testing.T, action ports.DecisionAction) int {
	t.Helper()
	all, err := v.log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range all {
		if d.Action == action {
			n++
		}
	}
	return n
}

func (v *i51Vault) openIDs(t *testing.T) []string {
	t.Helper()
	open, err := v.triggers.Delivered(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range open {
		ids = append(ids, o.ID)
	}
	return ids
}

func TestI51_AStagnantGoalIsAskedAndAnsweredAtItsCadence(t *testing.T) {
	v := newI51Vault(t)

	// Night 0: the goal has been stagnant for 30 days; a check-in is armed.
	v.night(t, 0)
	if n := v.armed(t); n != 1 {
		t.Fatalf("%d goal check-ins armed on night 0, want 1", n)
	}
	// The morning digest asks it, in the user's language, as its own item:
	// nothing else is due, so the digest is that question alone.
	if sent := v.morning(t, 0); len(sent) != 1 || sent[0] != i51Asked {
		t.Fatalf("the morning digest sent %q, want exactly %q", sent, i51Asked)
	}
	if open := v.openIDs(t); len(open) != 1 {
		t.Fatalf("open check-ins after the digest = %v, want the goal's", open)
	}
	// "sí, ayer salí a correr" names it: engaged closes it, the goal stays.
	v.answer(t, 0, "sí, ayer salí a correr", "engaged")
	if open := v.openIDs(t); len(open) != 0 {
		t.Fatalf("open check-ins after the answer = %v, want none", open)
	}
	assertGoalSignal(t, v.signals, ports.SignalNudgeEngaged)

	// Still stagnant, but asked once per goal_stagnation_days (21).
	for _, day := range []int{1, 7, 20} {
		v.night(t, day)
	}
	if n := v.armed(t); n != 1 {
		t.Fatalf("%d check-ins armed by night 20, want still 1", n)
	}
	v.night(t, 21)
	if n := v.armed(t); n != 2 {
		t.Fatalf("%d check-ins armed by night 21, want 2", n)
	}
	if sent := v.morning(t, 21); !mentions(sent, i51Goal) {
		t.Fatalf("the day-21 digest sent %q, want the goal asked again", sent)
	}
	// "dejala descansar": declined, the goal stays, and the next waits twice.
	v.answer(t, 21, "dejala descansar", "declined")
	assertGoalSignal(t, v.signals, ports.SignalNudgeDeclined)
	if active, err := v.selfModel.ActiveBeliefs(context.Background()); err != nil || len(active) != 1 {
		t.Fatalf("active beliefs = %+v, %v; a declined goal is never retired", active, err)
	}
	for _, day := range []int{42, 62} {
		v.night(t, day)
	}
	if n := v.armed(t); n != 2 {
		t.Fatalf("%d check-ins armed by night 62, want still 2: a declined goal waits twice the interval", n)
	}
	v.night(t, 63)
	if n := v.armed(t); n != 3 {
		t.Fatalf("%d check-ins armed by night 63, want 3", n)
	}
}

// TestI51_AnArmedGoalCheckInHasAnInstant (#412 QA): it is armed with a
// fire_at, so the scan that reads it once it is delivered does not fail.
func TestI51_AnArmedGoalCheckInHasAnInstant(t *testing.T) {
	v := newI51Vault(t)
	v.night(t, 0)
	due, err := v.triggers.Due(context.Background(), i51At(0, 3, 0))
	if err != nil || len(due) != 1 || due[0].FireAt.IsZero() || due[0].UnitID != nil || due[0].Payload.BeliefID != "b-run" {
		t.Fatalf("due at arming = %+v, %v; want one goal check-in with its instant, no unit, and b-run", due, err)
	}
}

// TestI51_ANightThatAsksNoGoalWritesNothing: stagnant but not due is a
// decision with no effect, and writes no row (I12).
func TestI51_ANightThatAsksNoGoalWritesNothing(t *testing.T) {
	v := newI51Vault(t)
	v.night(t, 0)
	before, _ := v.log.Since(context.Background(), time.Time{}, -1)
	v.night(t, 1)
	after, _ := v.log.Since(context.Background(), time.Time{}, -1)
	if len(after) != len(before) {
		t.Errorf("night 1 wrote %d rows for a goal already asked, want none", len(after)-len(before))
	}
}

// assertGoalSignal finds the newest signal of typ and checks it names the
// goal it answered (#412 review): goal cadence reads the belief from it.
func assertGoalSignal(t *testing.T, signals *memrepo.Signals, typ ports.SignalType) {
	t.Helper()
	all := signalsOf(t, signals)
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Type != typ {
			continue
		}
		var c struct {
			BeliefID string `json:"belief_id"`
		}
		if err := json.Unmarshal(all[i].Context, &c); err != nil || c.BeliefID != "b-run" {
			t.Errorf("%s context = %s, want belief_id b-run", typ, all[i].Context)
		}
		return
	}
	t.Errorf("no %s signal", typ)
}

// TestI51_TheGoalIsAskedInTheDigestNeverAlone (#413 review B1): the check-in
// is a digest item. No pass sends it on its own: not in quiet hours, not after
// the digest has gone out, and never with a lateness note, because Nooma chose
// its moment and nothing was late.
func TestI51_TheGoalIsAskedInTheDigestNeverAlone(t *testing.T) {
	v := newI51Vault(t)
	v.night(t, 0)
	if sent := v.check(t, 0, 3, 5); len(sent) != 0 {
		t.Fatalf("a pass in quiet hours sent %q", sent)
	}
	// Today previews the digest, the goal it will ask included.
	today, err := brain.NewTodayService(fixedClock{now: i51At(0, 6, 0)}, v.units, v.cfg, v.state, v.triggers,
		memrepo.NewPendingQuestions(), v.log, brain.NewFocusKeeper(v.units, v.cfg, memrepo.NewRelations()), nil).Today(context.Background())
	if err != nil || today.Digest.Goal != i51Asked {
		t.Fatalf("Today previews goal %q (%v), want %q", today.Digest.Goal, err, i51Asked)
	}
	// #413 QA: the digest preview shows it; it is no upcoming reminder.
	for _, u := range today.Upcoming {
		if strings.Contains(u.Text, i51Goal) {
			t.Errorf("Today lists the goal check-in under upcoming reminders: %+v", u)
		}
	}
	if sent := v.morning(t, 0); len(sent) != 1 || sent[0] != i51Asked {
		t.Fatalf("the digest sent %q, want exactly %q", sent, i51Asked)
	}
	if sent := v.check(t, 0, 7, 30); len(sent) != 0 {
		t.Errorf("a pass after the digest sent %q", sent)
	}
	// #413 review N1: asking it is one "Goal asked" row.
	if n := v.rows(t, ports.ActionCheckDigestGoalAsked); n != 1 {
		t.Errorf("%d %s rows, want 1", n, ports.ActionCheckDigestGoalAsked)
	}

	// Armed after the day's digest went out (a manual pass in the
	// afternoon), it waits for tomorrow's digest instead of being pushed.
	w := newI51Vault(t)
	if err := w.log.Record(context.Background(), ports.Decision{ID: "digest-0", Action: ports.ActionCheckDigestSent,
		Rationale: "the daily digest carried 0 item(s)", OccurredAt: i51At(0, 7, 0)}); err != nil {
		t.Fatal(err)
	}
	phase := consolidation.PhasePatternEval
	svc := brain.NewConsolidateService(fixedClock{now: i51At(0, 15, 0)}, w.cfg, w.units, memrepo.NewRelations(), w.ids, w.log, nil, nil,
		w.selfModel, memrepo.NewState(), memrepo.NewPendingQuestions(), w.signals, w.triggers)
	if _, err := svc.Consolidate(context.Background(), brain.ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatal(err)
	}
	if sent := w.check(t, 0, 15, 5); len(sent) != 0 {
		t.Errorf("a goal armed after the digest was pushed on its own: %q", sent)
	}
	if sent := w.morning(t, 1); len(sent) != 1 || sent[0] != i51Asked {
		t.Errorf("the next digest sent %q, want exactly %q", sent, i51Asked)
	}
}

// TestI51_ALowEnergyMorningHoldsTheGoal (#413 review B1): the care gate holds
// a goal check-in as it holds the relation question; it waits, armed, for a
// morning that is not low.
func TestI51_ALowEnergyMorningHoldsTheGoal(t *testing.T) {
	v := newI51Vault(t)
	v.night(t, 0)
	v.state.RecordEnergy(prospection.EnergyReading{Level: 0.1, RecordedAt: i51At(0, 6, 0), Source: "user"})
	if sent := v.morning(t, 0); len(sent) != 0 {
		t.Fatalf("a low-energy morning asked the goal: %q", sent)
	}
	if open := v.openIDs(t); len(open) != 0 {
		t.Fatalf("a held goal check-in reads as delivered: %v", open)
	}
	if sent := v.morning(t, 1); len(sent) != 1 || sent[0] != i51Asked {
		t.Errorf("the next morning sent %q, want exactly %q", sent, i51Asked)
	}
}

// TestI51_AnIgnoredGoalIsAskedAgainAtItsInterval (#413 review B2): unanswered,
// the check-in closes at the third digest after it was asked, as ignored, and
// the goal is asked again after the plain interval, not the declined one.
func TestI51_AnIgnoredGoalIsAskedAgainAtItsInterval(t *testing.T) {
	v := newI51Vault(t)
	v.night(t, 0)
	v.morning(t, 0)
	for day := 1; day <= 3; day++ {
		v.morning(t, day)
	}
	if open := v.openIDs(t); len(open) != 0 {
		t.Fatalf("open check-ins after three digests = %v, want it closed unanswered", open)
	}
	assertGoalSignal(t, v.signals, ports.SignalNudgeIgnored)
	v.night(t, 20)
	if n := v.armed(t); n != 1 {
		t.Fatalf("%d armed by night 20, want 1", n)
	}
	v.night(t, 21)
	if n := v.armed(t); n != 2 {
		t.Errorf("%d armed by night 21, want 2: an ignored check-in waits the plain interval", n)
	}
}

// TestI51_TheGoalIsWordedInTheDigestsLanguage (#413 review N2): the question
// is worded when the digest is built, in the digest's language, not frozen in
// the language of the night it was armed.
func TestI51_TheGoalIsWordedInTheDigestsLanguage(t *testing.T) {
	v := newI51Vault(t)
	v.night(t, 0)
	for i, id := range []string{"en-1", "en-2", "en-3"} {
		at := i51At(0, 4, i)
		if err := v.units.Create(context.Background(), unit.Unit{ID: id, Type: unit.TypeKnowledge, Status: unit.StatusPool, Source: "telegram",
			Content: "It rained.", Language: "en", Weight: 1, LastTouchedAt: at, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	if sent, want := v.morning(t, 0), `Still on "`+i51Goal+`"?`; len(sent) != 1 || sent[0] != want {
		t.Errorf("the digest sent %q, want exactly %q", sent, want)
	}
}
