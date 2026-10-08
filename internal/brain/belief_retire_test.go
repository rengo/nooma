package brain

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
)

// Retire (m4e design §3.4, spec R3, mutants B9, B10, B12-B14, B17, B19).

// B10: the transition goes active -> retired and nothing else moves.
func TestBeliefRetire_ActiveBeliefReadsRetiredWithRowAndSignal(t *testing.T) {
	w := newBeliefsWorld(t)
	before := w.snapshot()

	if err := w.service().Retire(context.Background(), "g1"); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	want := w.seeded["g1"]
	want.Status = selfmodel.StatusRetired
	want.UpdatedAt = w.now
	if got := w.belief("g1"); !reflect.DeepEqual(got, want) {
		t.Errorf("retired belief:\n got  %+v\n want %+v", got, want)
	}
	after := w.snapshot()
	for i, b := range after.Beliefs {
		if b.ID != "g1" && !reflect.DeepEqual(b, before.Beliefs[i]) {
			t.Errorf("belief %s changed by a retire of g1: %+v", b.ID, b)
		}
	}
	rows := w.newDecisions()
	if len(rows) != 1 {
		t.Fatalf("new decision_log rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Action != ports.ActionBeliefRetired || r.ID == "" || !r.OccurredAt.Equal(w.now) || r.Rationale == "" {
		t.Errorf("row = action %q id %q at %v rationale %q, want belief.retired, an id, %v and a sentence", r.Action, r.ID, r.OccurredAt, r.Rationale, w.now)
	}
	if want := []string{"belief_id", "content", "from", "to", "topic_key"}; !slices.Equal(keysOf(r.Ctx), want) {
		t.Errorf("context keys = %v, want %v", keysOf(r.Ctx), want)
	}
	if r.Ctx["belief_id"] != "g1" || r.Ctx["topic_key"] != "derived/goal/g1" || r.Ctx["content"] != "run a marathon" ||
		r.Ctx["from"] != "active" || r.Ctx["to"] != "retired" {
		t.Errorf("context = %v, want g1, its key and content, active -> retired", r.Ctx)
	}
	if n := len(w.newSignals()); n != 1 {
		t.Errorf("new learning signals = %d, want 1", n)
	}
}

// B9: retiring twice writes once.
func TestBeliefRetire_TwiceLogsOnce(t *testing.T) {
	w := newBeliefsWorld(t)
	svc := w.service()

	if err := svc.Retire(context.Background(), "g1"); err != nil {
		t.Fatalf("first Retire: %v", err)
	}
	afterFirst := w.snapshot()
	err := svc.Retire(context.Background(), "g1")

	if !errors.Is(err, ports.ErrBeliefStatusConflict) {
		t.Fatalf("second Retire error = %v, want ErrBeliefStatusConflict", err)
	}
	w.wantNothingChanged(afterFirst)
	if n := len(w.newDecisions()); n != 1 {
		t.Errorf("new decision_log rows = %d, want 1", n)
	}
	if n := len(w.newSignals()); n != 1 {
		t.Errorf("new learning signals = %d, want 1", n)
	}
}

// B12: an already-retired or unknown belief is refused before any write is
// attempted, so no counter moves.
func TestBeliefRetire_AlreadyRetiredConflictsBeforeAnyWrite(t *testing.T) {
	cases := []struct {
		name, id string
		want     error
	}{
		{"already retired", "gr", ports.ErrBeliefStatusConflict},
		{"unknown id", "no-such-belief", ports.ErrBeliefNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newBeliefsWorld(t)
			before := w.snapshot()

			err := w.service().Retire(context.Background(), tc.id)

			if !errors.Is(err, tc.want) {
				t.Fatalf("Retire error = %v, want %v", err, tc.want)
			}
			if w.model.setCalls != 0 || w.log.calls != 0 || w.signals.calls != 0 {
				t.Errorf("SetStatus/Record/signal calls = %d/%d/%d, want 0/0/0", w.model.setCalls, w.log.calls, w.signals.calls)
			}
			w.wantNothingChanged(before)
		})
	}
}

// A writer landing between the read and the write: the status write is the
// decision, and a conflict there leaves no row and no signal.
func TestBeliefRetire_StatusWriteRaceWritesNothingElse(t *testing.T) {
	w := newBeliefsWorld(t)
	w.model.setErr = ports.ErrBeliefStatusConflict
	before := w.snapshot()

	err := w.service().Retire(context.Background(), "g1")

	if !errors.Is(err, ports.ErrBeliefStatusConflict) || errors.Is(err, ErrWriteLanded) {
		t.Fatalf("Retire error = %v, want a plain ErrBeliefStatusConflict", err)
	}
	if w.log.calls != 0 || w.signals.calls != 0 {
		t.Errorf("Record/signal calls = %d/%d, want 0/0: nothing follows a write that did not land", w.log.calls, w.signals.calls)
	}
	w.wantNothingChanged(before)
}

// B13: one case per origin; the bucket is derive's only for a derived belief.
func TestBeliefRetire_RetireSignalFields(t *testing.T) {
	cases := []struct {
		id         string
		wantAction *ports.DecisionAction
	}{
		{"g1", ptr(ports.ActionDeriveBeliefCreated)}, // derived
		{"g2", nil}, // user_stated
		{"v2", nil}, // seed
	}
	for _, tc := range cases {
		w := newBeliefsWorld(t)
		prior := w.seeded[tc.id]
		t.Run(string(prior.Origin), func(t *testing.T) {
			if err := w.service().Retire(context.Background(), tc.id); err != nil {
				t.Fatalf("Retire: %v", err)
			}

			rows, sigs := w.newDecisions(), w.newSignals()
			if len(rows) != 1 || len(sigs) != 1 {
				t.Fatalf("new rows = %d, new signals = %d, want 1 and 1", len(rows), len(sigs))
			}
			s := sigs[0]
			if s.Type != ports.SignalBeliefDelete || s.Valence != ports.ValenceNegative {
				t.Errorf("type/valence = %q/%q, want belief_delete/negative", s.Type, s.Valence)
			}
			if s.TargetKind == nil || *s.TargetKind != ports.TargetKindBelief || s.TargetID == nil || *s.TargetID != tc.id {
				t.Errorf("target = %v/%v, want belief/%s", deref(s.TargetKind), deref(s.TargetID), tc.id)
			}
			if !reflect.DeepEqual(s.DecisionAction, tc.wantAction) {
				t.Errorf("decision action = %v, want %v", deref(s.DecisionAction), deref(tc.wantAction))
			}
			if s.Magnitude != nil || s.RelationType != nil {
				t.Errorf("magnitude/relation_type = %v/%v, want both nil", s.Magnitude, s.RelationType)
			}
			if !s.OccurredAt.Equal(w.now) || s.ID == "" || s.ID == rows[0].ID {
				t.Errorf("signal id %q occurred_at %v, want a fresh id and %v", s.ID, s.OccurredAt, w.now)
			}
			if want := []string{"belief_id", "decision_id", "topic_key"}; !slices.Equal(keysOf(s.Ctx), want) {
				t.Errorf("context keys = %v, want %v", keysOf(s.Ctx), want)
			}
			if s.Ctx["belief_id"] != tc.id || s.Ctx["topic_key"] != prior.TopicKey || s.Ctx["decision_id"] != rows[0].ID {
				t.Errorf("context = %v, want belief %s, key %s and the log row's id %s", s.Ctx, tc.id, prior.TopicKey, rows[0].ID)
			}
		})
	}
}

// B14: the write landed and the row did not. The signal is still written,
// without a decision_id, because the learning pass should hear the act.
func TestBeliefRetire_RecordFailureAfterWriteIsNonFatal(t *testing.T) {
	w := newBeliefsWorld(t)
	boom := errBoom("log down")
	w.log.err = boom

	err := w.service().Retire(context.Background(), "g1")

	if !errors.Is(err, ErrWriteLanded) || !errors.Is(err, boom) {
		t.Fatalf("Retire error = %v, want ErrWriteLanded wrapping the log failure", err)
	}
	var landed *WriteLandedError
	if !errors.As(err, &landed) || !landed.Record || landed.Signal {
		t.Fatalf("WriteLandedError = %+v, want Record only", landed)
	}
	if got := w.belief("g1").Status; got != selfmodel.StatusRetired {
		t.Errorf("status = %q, want retired", got)
	}
	if n := len(w.newDecisions()); n != 0 {
		t.Errorf("new decision_log rows = %d, want 0 (the log is down)", n)
	}
	sigs := w.newSignals()
	if len(sigs) != 1 {
		t.Fatalf("new learning signals = %d, want 1", len(sigs))
	}
	if want := []string{"belief_id", "topic_key"}; !slices.Equal(keysOf(sigs[0].Ctx), want) {
		t.Errorf("signal context keys = %v, want %v: no row, so no decision_id", keysOf(sigs[0].Ctx), want)
	}
}

// B17: the row landed and the signal did not.
func TestBeliefRetire_SignalFailureAfterWriteIsNonFatal(t *testing.T) {
	w := newBeliefsWorld(t)
	boom := errBoom("signals down")
	w.signals.err = boom

	err := w.service().Retire(context.Background(), "g1")

	if !errors.Is(err, ErrWriteLanded) || !errors.Is(err, boom) {
		t.Fatalf("Retire error = %v, want ErrWriteLanded wrapping the signal failure", err)
	}
	var landed *WriteLandedError
	if !errors.As(err, &landed) || landed.Record || !landed.Signal {
		t.Fatalf("WriteLandedError = %+v, want Signal only", landed)
	}
	if got := w.belief("g1").Status; got != selfmodel.StatusRetired {
		t.Errorf("status = %q, want retired", got)
	}
	rows := w.newDecisions()
	if len(rows) != 1 || rows[0].Action != ports.ActionBeliefRetired {
		t.Errorf("new decision_log rows = %+v, want the belief.retired row to stand", rows)
	}
}

// B19: both follow-ups failed and the caller is told about both.
func TestBeliefRetire_RecordAndSignalFailureReportsBoth(t *testing.T) {
	w := newBeliefsWorld(t)
	logBoom, signalBoom := errBoom("log down"), errBoom("signals down")
	w.log.err, w.signals.err = logBoom, signalBoom

	err := w.service().Retire(context.Background(), "g1")

	if !errors.Is(err, ErrWriteLanded) || !errors.Is(err, logBoom) || !errors.Is(err, signalBoom) {
		t.Fatalf("Retire error = %v, want ErrWriteLanded wrapping both failures", err)
	}
	var landed *WriteLandedError
	if !errors.As(err, &landed) || !landed.Record || !landed.Signal {
		t.Fatalf("WriteLandedError = %+v, want Record and Signal", landed)
	}
	if got := w.belief("g1").Status; got != selfmodel.StatusRetired {
		t.Errorf("status = %q, want retired", got)
	}
}

// One clock read per operation feeds updated_at, the row and the signal.
func TestBeliefRetire_OneInstantFeedsEveryWrite(t *testing.T) {
	w := newBeliefsWorld(t)
	w.clock = &tickClock{t: w.now}

	if err := w.service().Retire(context.Background(), "g1"); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	want := w.now.Add(time.Second)
	rows, sigs := w.newDecisions(), w.newSignals()
	if len(rows) != 1 || len(sigs) != 1 {
		t.Fatalf("new rows = %d, new signals = %d, want 1 and 1", len(rows), len(sigs))
	}
	if got := w.belief("g1").UpdatedAt; !got.Equal(want) {
		t.Errorf("updated_at = %v, want the one clock read %v", got, want)
	}
	if !rows[0].OccurredAt.Equal(want) || !sigs[0].OccurredAt.Equal(want) {
		t.Errorf("row at %v, signal at %v, want both %v", rows[0].OccurredAt, sigs[0].OccurredAt, want)
	}
}
