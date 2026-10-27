package conformance

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I47 (doc 02 §9): every verdict the user gives emits its learning signal, on
// every surface it can be given from. The learner reads only the signals, so a
// verdict that emits none teaches it nothing, and a relation signal without its
// type cannot be counted against that type. The belief, correction and state
// verdicts are pinned where their own paths are tested (docs/06-harness.md §4,
// I47's row); this file holds the relation and check-in verdicts M5 completed.

// i47Drop is a check-in answered "drop that one": declined.
const i47Drop = `{"type":"chitchat","normalized_content":"Drop it.","weight":0.2,"decay_rate":0.3,"language":"en","task_checkin_outcome":"drop","remind_request":false,"reminder_intent":"none"}`

func signalsOf(t *testing.T, s *memrepo.Signals) []ports.Signal {
	t.Helper()
	got, err := s.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// onlySignal fails unless exactly one signal was emitted, of type typ and
// valence v, about target of kind kind, and returns it.
func onlySignal(t *testing.T, got []ports.Signal, typ ports.SignalType, v ports.Valence, kind ports.TargetKind, target string) ports.Signal {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("signals = %+v, want exactly one %s", got, typ)
	}
	s := got[0]
	if s.Type != typ || s.Valence != v {
		t.Errorf("signal is %s/%s, want %s/%s", s.Type, s.Valence, typ, v)
	}
	if s.TargetKind == nil || *s.TargetKind != kind || s.TargetID == nil || *s.TargetID != target {
		t.Errorf("signal targets %v %v, want %s %q", s.TargetKind, s.TargetID, kind, target)
	}
	return s
}

// relationSignalContext is what a relation verdict carries beside its type:
// who made the relation and how sure the judge was, as it stood when the user
// judged it.
type relationSignalContext struct {
	CreatedBy  string   `json:"created_by"`
	Confidence *float64 `json:"confidence"`
}

func assertTypedRelationSignal(t *testing.T, s ports.Signal, relType, createdBy string, confidence float64) {
	t.Helper()
	if s.RelationType == nil || *s.RelationType != relType {
		t.Errorf("relation_type = %v, want %q", s.RelationType, relType)
	}
	var c relationSignalContext
	if err := json.Unmarshal(s.Context, &c); err != nil {
		t.Fatalf("signal context %s: %v", s.Context, err)
	}
	if c.CreatedBy != createdBy || c.Confidence == nil || *c.Confidence != confidence {
		t.Errorf("signal context = %s, want created_by %q and confidence %v", s.Context, createdBy, confidence)
	}
}

func TestI47_AGraphDeleteSignalsTheRelationsTypeAndMaker(t *testing.T) {
	v := newCurationVault(t)
	if rec := v.post(t, "/ui/graph/relations/r-near/reject"); rec.Code != http.StatusOK {
		t.Fatalf("reject: %d %s", rec.Code, rec.Body)
	}
	s := onlySignal(t, signalsOf(t, v.signals), ports.SignalRelationReject, ports.ValenceNegative, ports.TargetKindRelation, "r-near")
	assertTypedRelationSignal(t, s, "same_topic", "consolidation", 0.35)
}

func TestI47_AGraphConfirmSignalsTheRelationsTypeAndMaker(t *testing.T) {
	v := newCurationVault(t)
	if rec := v.post(t, "/ui/graph/relations/r-near/confirm"); rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body)
	}
	s := onlySignal(t, signalsOf(t, v.signals), ports.SignalRelationConfirm, ports.ValencePositive, ports.TargetKindRelation, "r-near")
	// The confidence the user judged, before the confirm raised it.
	assertTypedRelationSignal(t, s, "same_topic", "consolidation", 0.35)
}

func TestI47_AChatRejectSignalsTheRelationsTypeAndMaker(t *testing.T) {
	_, _, _, signals := relationAnswerFixture(t, "classify-relation-answer-rejected", "q-one")
	s := onlySignal(t, signalsOf(t, signals), ports.SignalRelationReject, ports.ValenceNegative, ports.TargetKindRelation, "rel-q-one")
	assertTypedRelationSignal(t, s, "same_topic", "consolidation", 0.4)
}

func TestI47_AChatConfirmSignalsTheRelationsTypeAndMaker(t *testing.T) {
	_, _, _, signals := relationAnswerFixture(t, "classify-relation-answer-confirmed", "q-one")
	s := onlySignal(t, signalsOf(t, signals), ports.SignalRelationConfirm, ports.ValencePositive, ports.TargetKindRelation, "rel-q-one")
	assertTypedRelationSignal(t, s, "same_topic", "consolidation", 0.4)
}

func TestI47_ACheckInAnsweredDoneSignalsEngaged(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-coffee", "buy coffee", i39Delivered)
	v.answer(t, "done", i39Done, i39Delivered.Add(30*time.Minute))
	onlySignal(t, signalsOf(t, v.signals), ports.SignalNudgeEngaged, ports.ValencePositive, ports.TargetKindTrigger, "t-coffee")
}

func TestI47_ACheckInAnsweredDropSignalsDeclined(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-coffee", "buy coffee", i39Delivered)
	v.answer(t, "drop it", i47Drop, i39Delivered.Add(30*time.Minute))
	onlySignal(t, signalsOf(t, v.signals), ports.SignalNudgeDeclined, ports.ValenceNegative, ports.TargetKindTrigger, "t-coffee")
}

// A snooze answers without resolving: the user saw it and put it off, which is
// neither engaging nor declining. nudge_ack is that, and neutral.
func TestI47_ASnoozedCheckInSignalsANeutralAck(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-coffee", "buy coffee", i39Delivered)
	v.answer(t, "no", i39NotYet, i39Delivered.Add(30*time.Minute))
	onlySignal(t, signalsOf(t, v.signals), ports.SignalNudgeAck, ports.ValenceNeutral, ports.TargetKindTrigger, "t-coffee")
}

// Doc 02 §9: learn from whether the user responded, not from whether it was
// delivered. One that reached the user and closed unanswered is ignored.
func TestI47_ACheckInNobodyAnsweredSignalsIgnored(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-coffee", "buy coffee", i39Delivered)
	for n := 1; n <= prospection.MaxCheckinReasks+1; n++ {
		v.check(t, i39Morning(n))
	}
	if open := v.open(t); len(open) != 0 {
		t.Fatalf("open check-ins %v, want it closed unanswered", open)
	}
	onlySignal(t, signalsOf(t, v.signals), ports.SignalNudgeIgnored, ports.ValenceNegative, ports.TargetKindTrigger, "t-coffee")
}

// A put-off check-in that then closes unanswered was acknowledged once and
// ignored after: two signals, in that order.
func TestI47_ASnoozedCheckInThatClosesSignalsAckThenIgnored(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-coffee", "buy coffee", i39Delivered)
	v.answer(t, "no", i39NotYet, i39Delivered.Add(30*time.Minute))
	for n := 1; n <= prospection.MaxCheckinReasks+1; n++ {
		v.check(t, i39Morning(n))
	}
	got := signalsOf(t, v.signals)
	if len(got) != 2 || got[0].Type != ports.SignalNudgeAck || got[1].Type != ports.SignalNudgeIgnored {
		t.Errorf("signals = %+v, want nudge_ack then nudge_ignored", got)
	}
}

// A dry run closes nothing, so it signals nothing.
func TestI47_ADryRunSignalsNothing(t *testing.T) {
	v := newI39Vault()
	v.deliver(t, "t-coffee", "buy coffee", i39Delivered)
	for n := 1; n <= prospection.MaxCheckinReasks+1; n++ {
		v.checkDry(t, i39Morning(n))
	}
	if got := signalsOf(t, v.signals); len(got) != 0 {
		t.Errorf("a dry run emitted %+v", got)
	}
}

// "Listo, pagué la tarjeta" closes the task and answers its check-in engaged
// (I42): the same verdict as answering the check-in, so the same signal.
func TestI47_ADoneFollowUpSignalsEngagedForTheCheckInItAnswers(t *testing.T) {
	f := newI42(t, i42Done("A"), "Pagar la tarjeta")
	ctx := context.Background()
	unitID := "t1"
	at := i42Digest.Add(-time.Hour)
	if err := f.triggers.Create(ctx, ports.Trigger{ID: "pushed", UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &at,
		CreatedAt: i42Created, Payload: ports.TriggerPayload{ActionText: "Pagar la tarjeta"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.triggers.Fire(ctx, "pushed", at); err != nil {
		t.Fatal(err)
	}
	if err := f.triggers.Surface(ctx, "pushed", at); err != nil {
		t.Fatal(err)
	}
	f.capture(t, "listo, pagué la tarjeta")
	onlySignal(t, signalsOf(t, f.signals), ports.SignalNudgeEngaged, ports.ValencePositive, ports.TargetKindTrigger, "pushed")
}

// A timer is infrastructure, not memory (doc 02 §8): cancelling one is no
// verdict on anything the brain decided, so it teaches the learner nothing.
func TestI47_ATimerCancelSignalsNothing(t *testing.T) {
	f := newI46(t, i46Reading("correction", "cancel", "1"), i46Two, nil)
	if res := f.capture(t, "cancelá el del horno"); res.Timers == nil || res.Timers.Cancelled == nil {
		t.Fatalf("result = %+v, want a timer cancelled", res)
	}
	if got := signalsOf(t, f.signals); len(got) != 0 {
		t.Errorf("a timer cancel emitted %+v", got)
	}
}
