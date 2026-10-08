package brain

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// Edit (m4e design §3.4, spec R2, mutants B1-B8, B15, B16, B18).

func TestBeliefEdit_LandsContentOriginRowAndSignal(t *testing.T) {
	w := newBeliefsWorld(t)
	before := w.snapshot()

	if err := w.service().Edit(context.Background(), "g1", "run a half marathon"); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	// Only content, origin and updated_at move; every other column and
	// every other belief is as seeded.
	want := w.seeded["g1"]
	want.Content = "run a half marathon"
	want.Origin = selfmodel.OriginUserStated
	want.UpdatedAt = w.now
	if got := w.belief("g1"); !reflect.DeepEqual(got, want) {
		t.Errorf("edited belief:\n got  %+v\n want %+v", got, want)
	}
	after := w.snapshot()
	for i, b := range after.Beliefs {
		if b.ID == "g1" {
			continue
		}
		if !reflect.DeepEqual(b, before.Beliefs[i]) {
			t.Errorf("belief %s changed by an edit of g1: %+v", b.ID, b)
		}
	}
	if n := len(w.newDecisions()); n != 1 {
		t.Errorf("new decision_log rows = %d, want 1", n)
	}
	if n := len(w.newSignals()); n != 1 {
		t.Errorf("new learning signals = %d, want 1", n)
	}
}

// B16: what is stored and what the row calls next.content is the
// normalised value.
func TestBeliefEdit_StoresAndLogsTheNormalisedContent(t *testing.T) {
	w := newBeliefsWorld(t)

	if err := w.service().Edit(context.Background(), "v1", "  a\r\nb  "); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	if got := w.belief("v1").Content; got != "a\nb" {
		t.Errorf("stored content = %q, want %q", got, "a\nb")
	}
	rows := w.newDecisions()
	if len(rows) != 1 {
		t.Fatalf("new decision_log rows = %d, want 1", len(rows))
	}
	next, _ := rows[0].Ctx["next"].(map[string]any)
	if next["content"] != "a\nb" {
		t.Errorf("next.content = %q, want the normalised %q", next["content"], "a\nb")
	}
}

// B7, and the row's own shape: keyed by column name, previous and next.
func TestBeliefEdit_RowIsKeyedByColumnWithPreImage(t *testing.T) {
	w := newBeliefsWorld(t)

	if err := w.service().Edit(context.Background(), "g1", "run a half marathon"); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	rows := w.newDecisions()
	if len(rows) != 1 {
		t.Fatalf("new decision_log rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Action != ports.ActionBeliefEdited {
		t.Errorf("action = %q, want %q", r.Action, ports.ActionBeliefEdited)
	}
	if r.ID == "" || !r.OccurredAt.Equal(w.now) {
		t.Errorf("row id %q occurred_at %v, want a non-empty id and %v", r.ID, r.OccurredAt, w.now)
	}
	if !strings.Contains(r.Rationale, "about to replace") {
		t.Errorf("rationale %q does not say the row is written before the edit (\"about to replace\")", r.Rationale)
	}
	if want := []string{"belief_id", "fields", "next", "previous", "topic_key"}; !slices.Equal(keysOf(r.Ctx), want) {
		t.Errorf("context keys = %v, want %v", keysOf(r.Ctx), want)
	}
	if r.Ctx["belief_id"] != "g1" || r.Ctx["topic_key"] != "derived/goal/g1" {
		t.Errorf("belief_id/topic_key = %v/%v, want g1/derived/goal/g1", r.Ctx["belief_id"], r.Ctx["topic_key"])
	}
	if !reflect.DeepEqual(r.Ctx["fields"], []any{"content", "origin"}) {
		t.Errorf("fields = %v, want [content origin]", r.Ctx["fields"])
	}
	previous, _ := r.Ctx["previous"].(map[string]any)
	next, _ := r.Ctx["next"].(map[string]any)
	if want := []string{"content", "origin"}; !slices.Equal(keysOf(previous), want) || !slices.Equal(keysOf(next), want) {
		t.Errorf("previous keys %v, next keys %v, want %v for both", keysOf(previous), keysOf(next), want)
	}
	if previous["content"] != "run a marathon" || previous["origin"] != "derived" {
		t.Errorf("previous = %v, want the stored content and origin", previous)
	}
	if next["content"] != "run a half marathon" || next["origin"] != "user_stated" {
		t.Errorf("next = %v, want the new content and user_stated", next)
	}
}

// B6: the signal names the belief and the log row; the decision bucket is
// the one that produced the belief, and only when that was derive.
func TestBeliefEdit_SignalNamesBeliefAndLogRow(t *testing.T) {
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
			if err := w.service().Edit(context.Background(), tc.id, "something else entirely"); err != nil {
				t.Fatalf("Edit: %v", err)
			}

			rows, sigs := w.newDecisions(), w.newSignals()
			if len(rows) != 1 || len(sigs) != 1 {
				t.Fatalf("new rows = %d, new signals = %d, want 1 and 1", len(rows), len(sigs))
			}
			s := sigs[0]
			if s.Type != ports.SignalBeliefEdit || s.Valence != ports.ValenceNegative {
				t.Errorf("type/valence = %q/%q, want belief_edit/negative", s.Type, s.Valence)
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

// B1: an unknown id writes nothing, not even a row naming nothing.
func TestBeliefEdit_UnknownIDWritesNothing(t *testing.T) {
	w := newBeliefsWorld(t)
	before := w.snapshot()

	err := w.service().Edit(context.Background(), "no-such-belief", "anything")

	if !errors.Is(err, ports.ErrBeliefNotFound) {
		t.Fatalf("Edit error = %v, want ErrBeliefNotFound", err)
	}
	w.wantNothingChanged(before)
	if w.log.calls != 0 || w.signals.calls != 0 || w.model.editCalls != 0 {
		t.Errorf("log/signal/edit calls = %d/%d/%d, want 0/0/0", w.log.calls, w.signals.calls, w.model.editCalls)
	}
}

// B2: judged on the normalised value, against the normalised stored text. A
// belief the user already owns (user_stated) is left exactly as it is.
func TestBeliefEdit_SameContentOnUserStatedBeliefWritesNothing(t *testing.T) {
	cases := []struct {
		name, submitted string
		setup           func(*beliefsWorld)
	}{
		{name: "identical text", submitted: "read every day"},
		{name: "CRLF resubmission of a multi-line belief", submitted: "line one\r\nline two", setup: func(w *beliefsWorld) {
			w.seed("u2", selfmodel.FacetValue, selfmodel.OriginUserStated, selfmodel.StatusActive, 0.4, "line one\nline two")
		}},
		{name: "surrounding whitespace only", submitted: "  read every day \r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newBeliefsWorld(t)
			id := "g2"
			if tc.setup != nil {
				tc.setup(w)
				id = "u2"
			}
			before := w.snapshot()

			if err := w.service().Edit(context.Background(), id, tc.submitted); err != nil {
				t.Fatalf("Edit: %v", err)
			}

			w.wantNothingChanged(before)
			if w.log.calls != 0 || w.signals.calls != 0 || w.model.editCalls != 0 {
				t.Errorf("log/signal/edit calls = %d/%d/%d, want 0/0/0", w.log.calls, w.signals.calls, w.model.editCalls)
			}
		})
	}
}

// Owner ruling 2026-10-08: saving a belief the user does not own yet,
// unchanged, claims it. The text is kept byte for byte (including a trailing
// space the user cannot see), origin becomes user_stated so derive stops
// rewriting it, and the claim is recorded like any other user write.
func TestBeliefEdit_UnchangedSubmitClaimsANonUserStatedBelief(t *testing.T) {
	cases := []struct {
		name, id, submitted string
		setup               func(*beliefsWorld)
	}{
		{name: "derived, identical text", id: "g1", submitted: "run a marathon"},
		{name: "derived, CRLF resubmission of a multi-line belief", id: "v1", submitted: "line one\r\nline two"},
		{name: "derived, surrounding whitespace only", id: "g1", submitted: "  run a marathon \r\n"},
		{name: "seed, identical text", id: "v2", submitted: "honesty first"},
		{
			name: "derived, stored text has a trailing space", id: "t1", submitted: "tea",
			setup: func(w *beliefsWorld) {
				w.seed("t1", selfmodel.FacetPreference, selfmodel.OriginDerived, selfmodel.StatusActive, 0.4, "tea ")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newBeliefsWorld(t)
			if tc.setup != nil {
				tc.setup(w)
			}
			prior := w.seeded[tc.id]
			before := w.snapshot()

			if err := w.service().Edit(context.Background(), tc.id, tc.submitted); err != nil {
				t.Fatalf("Edit: %v", err)
			}

			want := prior
			want.Origin = selfmodel.OriginUserStated
			want.UpdatedAt = w.now
			if got := w.belief(tc.id); !reflect.DeepEqual(got, want) {
				t.Errorf("claimed belief:\n got  %+v\n want %+v (content byte-identical)", got, want)
			}
			after := w.snapshot()
			for i, b := range after.Beliefs {
				if b.ID != tc.id && !reflect.DeepEqual(b, before.Beliefs[i]) {
					t.Errorf("belief %s changed by a claim of %s: %+v", b.ID, tc.id, b)
				}
			}

			rows, sigs := w.newDecisions(), w.newSignals()
			if len(rows) != 1 || len(sigs) != 1 {
				t.Fatalf("new rows = %d, new signals = %d, want 1 and 1", len(rows), len(sigs))
			}
			r := rows[0]
			if r.Action != ports.ActionBeliefEdited {
				t.Errorf("action = %q, want %q", r.Action, ports.ActionBeliefEdited)
			}
			if r.Ctx["claimed"] != true {
				t.Errorf("claimed = %v, want true", r.Ctx["claimed"])
			}
			if want := []string{"belief_id", "claimed", "fields", "next", "previous", "topic_key"}; !slices.Equal(keysOf(r.Ctx), want) {
				t.Errorf("context keys = %v, want %v", keysOf(r.Ctx), want)
			}
			previous, _ := r.Ctx["previous"].(map[string]any)
			next, _ := r.Ctx["next"].(map[string]any)
			if previous["content"] != prior.Content || previous["origin"] != string(prior.Origin) {
				t.Errorf("previous = %v, want the stored content and origin exactly", previous)
			}
			if next["content"] != prior.Content || next["origin"] != "user_stated" {
				t.Errorf("next = %v, want the stored content and user_stated", next)
			}
			if sigs[0].Type != ports.SignalBeliefEdit || sigs[0].Ctx["decision_id"] != r.ID {
				t.Errorf("signal = %q linking %v, want belief_edit linking the claim row %s", sigs[0].Type, sigs[0].Ctx["decision_id"], r.ID)
			}
			// Owner ruling 2026-10-08: a claim says the system derived it
			// right, so it is positive; a real edit stays negative (B6).
			if sigs[0].Valence != ports.ValencePositive {
				t.Errorf("claim signal valence = %q, want positive", sigs[0].Valence)
			}
		})
	}
}

// A real edit is not a claim: the row carries no claimed field.
func TestBeliefEdit_ChangedContentRowIsNotMarkedClaimed(t *testing.T) {
	w := newBeliefsWorld(t)
	if err := w.service().Edit(context.Background(), "g1", "run a half marathon"); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if _, present := w.newDecisions()[0].Ctx["claimed"]; present {
		t.Error("an edit row carries a claimed field, want it only on a claim")
	}
}

// A claim is record-first like an edit: a failed record means no write, no
// signal.
func TestBeliefEdit_ClaimLogFailureLeavesBeliefUntouched(t *testing.T) {
	w := newBeliefsWorld(t)
	boom := errBoom("log down")
	w.log.err = boom
	before := w.snapshot()

	err := w.service().Edit(context.Background(), "g1", "run a marathon")

	if !errors.Is(err, boom) || errors.Is(err, ErrWriteLanded) {
		t.Fatalf("Edit error = %v, want the plain log failure", err)
	}
	w.wantNothingChanged(before)
	if w.model.editCalls != 0 || w.signals.calls != 0 {
		t.Errorf("edit/signal calls = %d/%d, want 0/0", w.model.editCalls, w.signals.calls)
	}
}

// The claim is what protects the text: after it, a derive pass whose
// proposal carries the belief's topic_key reinforces it and cannot overwrite
// its content, though derive rewrote that content before the claim.
func TestBeliefEdit_ClaimedBeliefSurvivesALaterDerivePass(t *testing.T) {
	// Control: unclaimed, the derived belief is overwritten by the proposal.
	control := newShieldWorld(t)
	control.seed(seedBelief{id: "d", key: "derived/goal/swim", content: "swim a mile", conf: 0.5, vec: []float32{1, 0, 0}})
	control.propose("goal", "swim", "swim two miles", []float32{0, 1, 0})
	control.mustRun()
	if got := control.belief("d").Content; got != "swim two miles" {
		t.Fatalf("control: unclaimed derived belief reads %q, want it rewritten by derive", got)
	}

	w := newShieldWorld(t)
	w.seed(seedBelief{id: "d", key: "derived/goal/swim", content: "swim a mile", conf: 0.5, vec: []float32{1, 0, 0}})
	svc := NewBeliefsService(fixedClock{w.now}, &seqIDs{n: 500}, w.base, memrepo.NewSignals(), w.log)
	if err := svc.Edit(context.Background(), "d", "swim a mile"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	w.propose("goal", "swim", "swim two miles", []float32{0, 1, 0})

	w.mustRun()

	got := w.belief("d")
	if got.Content != "swim a mile" || got.Origin != selfmodel.OriginUserStated {
		t.Errorf("belief after derive = %+v, want the claimed text and user_stated", got)
	}
	if n := len(w.rows(ports.ActionDeriveBeliefCreated)); n != 0 {
		t.Errorf("belief_created rows = %d, want 0: the proposal must not create a duplicate", n)
	}
}

// B3: a retired belief is a conflict before anything is written, even when
// the submitted text equals what is stored.
func TestBeliefEdit_RetiredBeliefWritesNothing(t *testing.T) {
	for _, submitted := range []string{"a brand new text", "learn latin"} {
		t.Run(submitted, func(t *testing.T) {
			w := newBeliefsWorld(t)
			before := w.snapshot()

			err := w.service().Edit(context.Background(), "gr", submitted)

			if !errors.Is(err, ports.ErrBeliefStatusConflict) {
				t.Fatalf("Edit error = %v, want ErrBeliefStatusConflict", err)
			}
			w.wantNothingChanged(before)
			if w.log.calls != 0 || w.signals.calls != 0 || w.model.editCalls != 0 {
				t.Errorf("log/signal/edit calls = %d/%d/%d, want 0/0/0", w.log.calls, w.signals.calls, w.model.editCalls)
			}
		})
	}
}

// B4: the pre-image is written first; when it cannot be, nothing is.
func TestBeliefEdit_LogFailureLeavesBeliefUntouched(t *testing.T) {
	w := newBeliefsWorld(t)
	boom := errBoom("log down")
	w.log.err = boom
	before := w.snapshot()

	err := w.service().Edit(context.Background(), "g1", "run a half marathon")

	if !errors.Is(err, boom) {
		t.Fatalf("Edit error = %v, want the log failure", err)
	}
	if errors.Is(err, ErrWriteLanded) {
		t.Error("error is ErrWriteLanded, want a plain failure: nothing landed")
	}
	w.wantNothingChanged(before)
	if w.model.editCalls != 0 || w.signals.calls != 0 {
		t.Errorf("edit/signal calls = %d/%d, want 0/0: the write must not run after a failed pre-image", w.model.editCalls, w.signals.calls)
	}
}

// B5: a failed write emits no signal. (The pre-image row it follows is the
// accepted ADR-0016 window and is deliberately not asserted away.)
func TestBeliefEdit_WriteFailureEmitsNoSignal(t *testing.T) {
	w := newBeliefsWorld(t)
	w.model.editErr = ports.ErrBeliefStatusConflict
	before := w.snapshot()

	err := w.service().Edit(context.Background(), "g1", "run a half marathon")

	if !errors.Is(err, ports.ErrBeliefStatusConflict) || errors.Is(err, ErrWriteLanded) {
		t.Fatalf("Edit error = %v, want a plain ErrBeliefStatusConflict", err)
	}
	if w.signals.calls != 0 {
		t.Errorf("signal writes = %d, want 0", w.signals.calls)
	}
	if got := w.snapshot(); !reflect.DeepEqual(before.Signals, got.Signals) || !reflect.DeepEqual(before.Beliefs, got.Beliefs) {
		t.Errorf("signals or beliefs changed after a failed write")
	}
}

// B15: the edit and its row stand; only the signal is missing.
func TestBeliefEdit_SignalFailureAfterWriteIsNonFatal(t *testing.T) {
	w := newBeliefsWorld(t)
	boom := errBoom("signals down")
	w.signals.err = boom

	err := w.service().Edit(context.Background(), "g1", "run a half marathon")

	if !errors.Is(err, ErrWriteLanded) || !errors.Is(err, boom) {
		t.Fatalf("Edit error = %v, want ErrWriteLanded wrapping the signal failure", err)
	}
	var landed *WriteLandedError
	if !errors.As(err, &landed) || landed.Record || !landed.Signal {
		t.Fatalf("WriteLandedError = %+v, want Signal only", landed)
	}
	if got := w.belief("g1"); got.Content != "run a half marathon" || got.Origin != selfmodel.OriginUserStated {
		t.Errorf("belief = %+v, want the edit to have landed", got)
	}
	if n := len(w.newDecisions()); n != 1 {
		t.Errorf("new decision_log rows = %d, want the pre-image row to stand", n)
	}
}

// B8: empty and over-bound input is a typed error with nothing written,
// decided before any read.
func TestBeliefEdit_RejectsEmptyAndOverBoundContent(t *testing.T) {
	cases := []struct {
		name, id, content string
		want              error
	}{
		{"empty", "g1", "", selfmodel.ErrEmptyContent},
		{"whitespace only", "g1", " \r\n\t ", selfmodel.ErrEmptyContent},
		{"one rune over", "g1", strings.Repeat("a", selfmodel.MaxBeliefContentRunes+1), selfmodel.ErrContentTooLong},
		{"multibyte one rune over", "g1", strings.Repeat("é", selfmodel.MaxBeliefContentRunes+1), selfmodel.ErrContentTooLong},
		{"empty on an unknown id", "no-such-belief", "", selfmodel.ErrEmptyContent},
		{"too long on a retired id", "gr", strings.Repeat("a", selfmodel.MaxBeliefContentRunes+1), selfmodel.ErrContentTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newBeliefsWorld(t)
			before := w.snapshot()

			err := w.service().Edit(context.Background(), tc.id, tc.content)

			if !errors.Is(err, tc.want) {
				t.Fatalf("Edit error = %v, want %v", err, tc.want)
			}
			w.wantNothingChanged(before)
		})
	}

	for _, tc := range []struct{ name, content string }{
		{"exactly the bound", strings.Repeat("a", selfmodel.MaxBeliefContentRunes)},
		{"multibyte at the bound", strings.Repeat("é", selfmodel.MaxBeliefContentRunes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newBeliefsWorld(t)
			if err := w.service().Edit(context.Background(), "g1", "  "+tc.content+"  "); err != nil {
				t.Fatalf("Edit: %v", err)
			}
			if got := w.belief("g1").Content; got != tc.content {
				t.Errorf("stored %d runes, want the bound's %d unpadded", len([]rune(got)), selfmodel.MaxBeliefContentRunes)
			}
		})
	}
}

// B18: derived content is unbounded and may look blank; an edit of such a
// belief must not trip over its own stored text.
func TestBeliefEdit_OverBoundDerivedBeliefIsEditable(t *testing.T) {
	long := strings.Repeat("x", 1500) + " "
	for name, stored := range map[string]string{"over the bound with a trailing space": long, "whitespace only": "   "} {
		t.Run(name, func(t *testing.T) {
			w := newBeliefsWorld(t)
			w.seed("odd", selfmodel.FacetValue, selfmodel.OriginDerived, selfmodel.StatusActive, 0.5, stored)

			if err := w.service().Edit(context.Background(), "odd", "a short, valid text"); err != nil {
				t.Fatalf("Edit: %v", err)
			}

			if got := w.belief("odd"); got.Content != "a short, valid text" || got.Origin != selfmodel.OriginUserStated {
				t.Errorf("belief = %+v, want the edit to have landed as user_stated", got)
			}
			rows := w.newDecisions()
			if len(rows) != 1 || len(w.newSignals()) != 1 {
				t.Fatalf("new rows = %d, new signals = %d, want 1 and 1", len(rows), len(w.newSignals()))
			}
			previous, _ := rows[0].Ctx["previous"].(map[string]any)
			if previous["content"] != stored {
				t.Errorf("previous.content = %q, want the stored text exactly", previous["content"])
			}
		})
	}

	t.Run("resubmitting the over-bound text is rejected on the submitted side", func(t *testing.T) {
		w := newBeliefsWorld(t)
		w.seed("odd", selfmodel.FacetValue, selfmodel.OriginDerived, selfmodel.StatusActive, 0.5, long)
		before := w.snapshot()

		err := w.service().Edit(context.Background(), "odd", long)

		if !errors.Is(err, selfmodel.ErrContentTooLong) {
			t.Fatalf("Edit error = %v, want ErrContentTooLong", err)
		}
		w.wantNothingChanged(before)
	})
}

// One clock read per operation feeds updated_at, the row and the signal.
func TestBeliefEdit_OneInstantFeedsEveryWrite(t *testing.T) {
	w := newBeliefsWorld(t)
	w.clock = &tickClock{t: w.now}

	if err := w.service().Edit(context.Background(), "g1", "run a half marathon"); err != nil {
		t.Fatalf("Edit: %v", err)
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

func ptr[T any](v T) *T { return &v }

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
