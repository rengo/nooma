// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakechannel"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// The classify readings of a bare "no" (snooze, the client's ruling) and of
// "done", in the shape the reminder phrasing set records them.
const (
	i39NotYet = `{"type":"chitchat","normalized_content":"No.","weight":0.2,"decay_rate":0.3,"language":"en","task_checkin_outcome":"snooze","remind_request":false,"reminder_intent":"none"}`
	i39Done   = `{"type":"chitchat","normalized_content":"Done.","weight":0.2,"decay_rate":0.3,"language":"en","task_checkin_outcome":"done","remind_request":false,"reminder_intent":"none"}`
)

// i39Delivered is the evening a check-in reached the user; i39Morning(n) is
// the pass just after the nth digest that follows it.
var i39Delivered = time.Date(2026, 10, 14, 21, 0, 0, 0, time.UTC)

func i39Morning(n int) time.Time {
	return time.Date(2026, 10, 14+n, prospection.DigestHour, 5, 0, 0, time.UTC)
}

// i39Vault is one vault the capture and the check passes share.
type i39Vault struct {
	triggers *memrepo.Triggers
	units    *memrepo.Units
	log      *memrepo.DecisionLog
	ids      *counterIDs
	channel  *fakechannel.Fake
	signals  *memrepo.Signals
	// signalStore, when set, stands in for signals in every pass: a test of
	// what happens when the signal write fails.
	signalStore ports.SignalRepo
}

// store is the signal store the passes write to.
func (v *i39Vault) store() ports.SignalRepo {
	if v.signalStore != nil {
		return v.signalStore
	}
	return v.signals
}

func newI39Vault() *i39Vault {
	return &i39Vault{triggers: memrepo.NewTriggers(), units: memrepo.NewUnits(), log: memrepo.NewDecisionLog(),
		ids: &counterIDs{}, channel: fakechannel.New(), signals: memrepo.NewSignals()}
}

// deliver stores a one-shot check-in about a task with content (a nudge with
// no unit when content is empty) that reached the user at at.
func (v *i39Vault) deliver(t *testing.T, id, content string, at time.Time) {
	t.Helper()
	v.deliverKind(t, id, unit.TypeTask, content, at)
}

// deliverKind is deliver for a unit of kind.
func (v *i39Vault) deliverKind(t *testing.T, id string, kind unit.Type, content string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	trg := ports.Trigger{ID: id, Kind: ports.TriggerKindTimeBased, FireAt: &at, CreatedAt: at.Add(-time.Hour),
		Payload: ports.TriggerPayload{ActionText: "armed text"}}
	if content != "" {
		unitID := "u-" + id
		if err := v.units.Create(ctx, unit.Unit{ID: unitID, Type: kind, Status: unit.StatusPool, Content: content,
			Source: "chat", Weight: 1, LastTouchedAt: at, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
		trg.UnitID = &unitID
	}
	if err := v.triggers.Create(ctx, trg); err != nil {
		t.Fatal(err)
	}
	if err := v.triggers.Fire(ctx, id, at); err != nil {
		t.Fatal(err)
	}
	if err := v.triggers.Surface(ctx, id, at); err != nil {
		t.Fatal(err)
	}
}

// answer captures text through the real pipeline, the model reading response.
func (v *i39Vault) answer(t *testing.T, text, response string, at time.Time) {
	t.Helper()
	if err := v.answerErr(t, text, response, at, true); err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
}

// answerErr is answer, returning the capture's error instead of failing. reply
// says whether the capture is scripted to reach its chat reply; one expected
// to fail before it is not.
func (v *i39Vault) answerErr(t *testing.T, text, response string, at time.Time, reply bool) error {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	for id, c := range map[string]map[string]string{
		"classify-answer": {"provider": "openai", "model": "gpt-6-luna", "task": "classify", "message": text, "response": response},
		"chat-answer":     {"provider": "openai", "model": "gpt-6-luna", "task": "chat", "message": text, "response": "Ok."},
	} {
		c["id"] = id
		body, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	scripted := []string{"classify-answer"}
	if reply {
		scripted = append(scripted, "chat-answer")
	}
	llm := fakeprovider.New(t, dir, scripted...)
	svc := brain.NewCaptureService(fixedClock{now: at}, v.ids, v.units, embeddings, memrepo.NewLexical(), memrepo.NewRelations(),
		v.log, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx),
		v.store(), v.triggers, memrepo.NewTimers(), 0.5, memrepo.NewPendingQuestions())
	_, err = svc.Capture(ctx, brain.CaptureInput{Text: text, Channel: "chat"})
	return err
}

// checkErr runs one check pass at at and returns its error.
func (v *i39Vault) checkErr(at time.Time, req brain.CheckRequest) error {
	_, err := brain.NewCheckService(fixedClock{now: at}, v.triggers, memrepo.NewTimers(), v.ids, v.log, v.channel,
		v.units, memrepo.NewState(), nil, "12449194", nil, nil, v.store(), nil).Check(context.Background(), req)
	return err
}

// check runs one check pass at at and returns what it sent.
func (v *i39Vault) check(t *testing.T, at time.Time) []string {
	t.Helper()
	return v.checkWith(t, at, brain.CheckRequest{})
}

// checkDry is check as a dry run, which writes nothing.
func (v *i39Vault) checkDry(t *testing.T, at time.Time) []string {
	t.Helper()
	return v.checkWith(t, at, brain.CheckRequest{DryRun: true})
}

func (v *i39Vault) checkWith(t *testing.T, at time.Time, req brain.CheckRequest) []string {
	t.Helper()
	before := len(v.channel.Sent(t))
	if err := v.checkErr(at, req); err != nil {
		t.Fatalf("Check at %s: %v", at, err)
	}
	var texts []string
	for _, m := range v.channel.Sent(t)[before:] {
		texts = append(texts, m.Text)
	}
	return texts
}

func (v *i39Vault) open(t *testing.T) []string {
	t.Helper()
	open, err := v.triggers.Delivered(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range open {
		ids = append(ids, c.ID)
	}
	return ids
}

func (v *i39Vault) rows(t *testing.T, action ports.DecisionAction) int {
	t.Helper()
	all, err := v.log.Since(context.Background(), i39Delivered.Add(-time.Hour), -1)
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

func mentions(texts []string, s string) bool {
	for _, text := range texts {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// TestI39_ASnoozedCheckinIsAskedTwiceThenCloses is the client's ruling of
// 2026-10-16: a check-in put off with a bare "no" is asked again by the next
// morning digest, up to MaxCheckinReasks times, whether the re-ask is put off
// again or left unanswered, and the digest after the last closes it.
func TestI39_ASnoozedCheckinIsAskedTwiceThenCloses(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-coffee", "buy coffee", i39Delivered)
	v.answer(t, "no", i39NotYet, i39Delivered.Add(30*time.Minute))

	for n := 1; n <= prospection.MaxCheckinReasks; n++ {
		if sent := v.check(t, i39Morning(n)); !mentions(sent, "buy coffee") {
			t.Fatalf("digest %d sent %q, want it to ask about buy coffee again", n, sent)
		}
		if open := v.open(t); len(open) != 1 {
			t.Fatalf("after digest %d, open check-ins %v, want t-coffee still open", n, open)
		}
		if n == 1 {
			// The second re-ask is put off again; it counts the same as silence.
			v.answer(t, "no", i39NotYet, i39Morning(n).Add(time.Hour))
		}
	}
	if n := v.rows(t, ports.ActionCheckCheckinReasked); n != prospection.MaxCheckinReasks {
		t.Errorf("%d check.checkin.reasked rows, want %d", n, prospection.MaxCheckinReasks)
	}

	if sent := v.check(t, i39Morning(prospection.MaxCheckinReasks+1)); mentions(sent, "buy coffee") {
		t.Errorf("the digest after the last re-ask asked again: %q", sent)
	}
	if open := v.open(t); len(open) != 0 {
		t.Errorf("open check-ins %v, want it closed by itself", open)
	}
	if n := v.rows(t, ports.ActionCheckCheckinExpired); n != 1 {
		t.Errorf("%d check.checkin.expired rows, want 1", n)
	}
}

// TestI39_AnUnansweredCheckinClosesAndIsNeverAsked: one nobody answered or put
// off is never asked again, and closes at the same digest, so an old check-in
// stops collecting answers.
func TestI39_AnUnansweredCheckinClosesAndIsNeverAsked(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-coffee", "buy coffee", i39Delivered)

	for n := 1; n <= prospection.MaxCheckinReasks; n++ {
		if sent := v.check(t, i39Morning(n)); mentions(sent, "buy coffee") {
			t.Fatalf("digest %d asked an unanswered check-in again: %q", n, sent)
		}
		if open := v.open(t); len(open) != 1 {
			t.Fatalf("after digest %d, open check-ins %v, want it still open", n, open)
		}
	}
	v.check(t, i39Morning(prospection.MaxCheckinReasks+1))
	if open := v.open(t); len(open) != 0 {
		t.Errorf("open check-ins %v, want it closed at the third digest", open)
	}
	if n := v.rows(t, ports.ActionCheckCheckinExpired); n != 1 {
		t.Errorf("%d check.checkin.expired rows, want 1", n)
	}
	if n := v.rows(t, ports.ActionCheckCheckinReasked); n != 0 {
		t.Errorf("%d check.checkin.reasked rows, want none", n)
	}
}

// TestI39_ASnoozedNudgeIsNeverAsked: a nudge with no unit has no words to ask
// again; put off, it only gets the same lifetime.
func TestI39_ASnoozedNudgeIsNeverAsked(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-nudge", "", i39Delivered)
	v.answer(t, "no", i39NotYet, i39Delivered.Add(30*time.Minute))

	for n := 1; n <= prospection.MaxCheckinReasks; n++ {
		if sent := v.check(t, i39Morning(n)); len(sent) != 0 {
			t.Fatalf("digest %d sent %q for a snoozed nudge", n, sent)
		}
	}
	v.check(t, i39Morning(prospection.MaxCheckinReasks+1))
	if open := v.open(t); len(open) != 0 {
		t.Errorf("open check-ins %v, want the nudge closed", open)
	}
}

// TestI39_AnAnswerToAReaskReachesIt: the re-ask makes the check-in the newest
// open one, so "done" after the digest closes it and not a reminder pushed
// later the evening before.
func TestI39_AnAnswerToAReaskReachesIt(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-coffee", "buy coffee", i39Delivered)
	v.answer(t, "no", i39NotYet, i39Delivered.Add(30*time.Minute))
	v.deliver(t, "t-later", "call the bank", i39Delivered.Add(time.Hour))

	if sent := v.check(t, i39Morning(1)); !mentions(sent, "buy coffee") {
		t.Fatalf("digest sent %q, want it to ask about buy coffee again", sent)
	}
	v.answer(t, "done", i39Done, i39Morning(1).Add(time.Hour))
	if open := v.open(t); len(open) != 1 || open[0] != "t-later" {
		t.Errorf("open check-ins %v, want only t-later: the answer was to the re-ask", open)
	}
}

// TestI39_ASnoozedEventIsNotAsked: only something still to do is asked again.
// "cena con Juan" put off is not asked the morning after the dinner (ADR-0032:
// a reminder never speaks of the past); it closes on the same schedule.
func TestI39_ASnoozedEventIsNotAsked(t *testing.T) {
	v := newI39Vault()
	v.deliverKind(t, "t-dinner", unit.TypeEvent, "dinner with Juan", i39Delivered)
	v.answer(t, "no", i39NotYet, i39Delivered.Add(30*time.Minute))

	for n := 1; n <= prospection.MaxCheckinReasks; n++ {
		if sent := v.check(t, i39Morning(n)); mentions(sent, "dinner with Juan") {
			t.Fatalf("digest %d asked a snoozed event again: %q", n, sent)
		}
	}
	v.check(t, i39Morning(prospection.MaxCheckinReasks+1))
	if open := v.open(t); len(open) != 0 {
		t.Errorf("open check-ins %v, want the event closed at the third digest", open)
	}
	if n := v.rows(t, ports.ActionCheckCheckinReasked); n != 0 {
		t.Errorf("%d check.checkin.reasked rows, want none", n)
	}
}

// TestI39_AnExpiredRecurringCheckinLeavesTheNextOccurrence: closing a
// delivered recurring reminder nobody answered moves that one row and nothing
// else. The unit's next occurrence, armed on its own row, still fires and is
// carried by its digest; the closed row keeps its rule, and Today and a
// correction, which read armed rows only, see the series as before.
func TestI39_AnExpiredRecurringCheckinLeavesTheNextOccurrence(t *testing.T) {
	ctx := context.Background()
	v := newI39Vault()
	unitID := "u-t-birthday"
	yearly := prospection.RuleYearly
	anchor := &prospection.Anchor{Month: time.October, Day: 25}
	if err := v.units.Create(ctx, unit.Unit{ID: unitID, Type: unit.TypeEvent, Status: unit.StatusPool, Content: "Ana's birthday",
		Source: "chat", Weight: 1, LastTouchedAt: i39Delivered, CreatedAt: i39Delivered, UpdatedAt: i39Delivered}); err != nil {
		t.Fatal(err)
	}
	delivered := i39Delivered
	if err := v.triggers.Create(ctx, ports.Trigger{ID: "t-birthday", UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		FireAt: &delivered, CreatedAt: delivered, Payload: ports.TriggerPayload{ActionText: "Ana's birthday"},
		RecurrenceRule: &yearly, RecurrenceAnchor: anchor}); err != nil {
		t.Fatal(err)
	}
	if err := v.triggers.Fire(ctx, "t-birthday", delivered); err != nil {
		t.Fatal(err)
	}
	if err := v.triggers.Surface(ctx, "t-birthday", delivered); err != nil {
		t.Fatal(err)
	}
	next := i39Morning(prospection.MaxCheckinReasks + 3).Add(-time.Hour)
	if err := v.triggers.Create(ctx, ports.Trigger{ID: "t-birthday-next", UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		FireAt: &next, CreatedAt: i39Delivered, Payload: ports.TriggerPayload{ActionText: "Ana's birthday"},
		RecurrenceRule: &yearly, RecurrenceAnchor: anchor}); err != nil {
		t.Fatal(err)
	}

	for n := 1; n <= prospection.MaxCheckinReasks+1; n++ {
		v.check(t, i39Morning(n))
	}
	if open := v.open(t); len(open) != 0 {
		t.Fatalf("open %v, want the delivered birthday reminder closed", open)
	}
	armed, err := v.triggers.ArmedForUnit(ctx, unitID)
	if err != nil {
		t.Fatal(err)
	}
	if len(armed) != 1 || armed[0].ID != "t-birthday-next" || armed[0].RecurrenceRule == nil || *armed[0].RecurrenceRule != yearly {
		t.Fatalf("armed %+v, want the next occurrence untouched", armed)
	}

	if sent := v.check(t, i39Morning(prospection.MaxCheckinReasks+3)); !mentions(sent, "Ana's birthday") {
		t.Errorf("the next occurrence's digest sent %q, want it carried", sent)
	}
	if open := v.open(t); len(open) != 1 || open[0] != "t-birthday-next" {
		t.Errorf("open %v, want the next occurrence delivered", open)
	}
}

// TestI39_ABareAnswerReachesTheLastThingThePassSent is QA's repro on #366: in
// one pass a late reminder is pushed as its own message and then the digest
// asks a snoozed check-in again. The re-ask went out last, so "done" reaches
// it, whichever id sorts first: the pass stamps everything it sends in send
// order, not each message on its own.
func TestI39_ABareAnswerReachesTheLastThingThePassSent(t *testing.T) {
	for _, ids := range [][2]string{{"t-a", "t-z"}, {"t-z", "t-a"}} {
		snoozed, pushed := ids[0], ids[1]
		t.Run("snoozed "+snoozed, func(t *testing.T) {
			ctx := context.Background()
			v := newI39Vault()
			v.deliver(t, snoozed, "snoozed one", i39Delivered)
			v.answer(t, "no", i39NotYet, i39Delivered.Add(30*time.Minute))

			dueAt := time.Date(2026, 10, 15, 7, 30, 0, 0, time.UTC)
			unitID := "u-" + pushed
			if err := v.units.Create(ctx, unit.Unit{ID: unitID, Type: unit.TypeTask, Status: unit.StatusPool, Content: "day item",
				Source: "chat", Weight: 1, LastTouchedAt: i39Delivered, CreatedAt: i39Delivered, UpdatedAt: i39Delivered}); err != nil {
				t.Fatal(err)
			}
			if err := v.triggers.Create(ctx, ports.Trigger{ID: pushed, UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
				FireAt: &dueAt, CreatedAt: i39Delivered, Payload: ports.TriggerPayload{ActionText: "day item"}}); err != nil {
				t.Fatal(err)
			}

			sent := v.check(t, time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC))
			if len(sent) != 2 || !strings.Contains(sent[0], "day item") || !strings.Contains(sent[1], "snoozed one") {
				t.Fatalf("sent %q, want the push and then the re-ask", sent)
			}
			v.answer(t, "listo", i39Done, time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC))
			if open := v.open(t); len(open) != 1 || open[0] != pushed {
				t.Errorf("open %v, want only %s: the answer was to the re-ask, sent last", open, pushed)
			}
		})
	}
}
