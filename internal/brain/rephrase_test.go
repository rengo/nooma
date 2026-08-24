package brain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// scriptedLLM answers with a fixed text or a fixed error, and counts calls.
type scriptedLLM struct {
	text  string
	err   error
	calls int
	seen  []ports.LLMRequest
}

func (l *scriptedLLM) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	l.calls++
	l.seen = append(l.seen, req)
	if l.err != nil {
		return ports.LLMResponse{}, l.err
	}
	return ports.LLMResponse{Text: l.text}, nil
}

func dueTimer(id, text string, fireAt time.Time) ports.DueTimer {
	t := ports.DueTimer{ID: id, FireAt: fireAt}
	if text != "" {
		t.ActionText = &text
	}
	return t
}

var rephraseFireAt = time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)

// TestRenderTimer_WordsTheRequestAtDelivery is R4.1.
func TestRenderTimer_WordsTheRequestAtDelivery(t *testing.T) {
	llm := &scriptedLLM{text: "  Time to take the bread out.  "}
	r := checkRunner{ids: &countingIDs{}, log: &recordingLog{}, llm: llm}

	got, err := r.renderTimer(context.Background(), dueTimer("t", "take the bread out", rephraseFireAt), rephraseFireAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("renderTimer: %v", err)
	}

	if got.text != "Time to take the bread out." {
		t.Errorf("text = %q, want the rephrasing, trimmed", got.text)
	}
	if got.rendered == nil || *got.rendered != got.text {
		t.Errorf("rendered = %v, want the same wording stored", got.rendered)
	}
	if llm.calls != 1 {
		t.Errorf("%d provider calls, want 1", llm.calls)
	}
	if !strings.Contains(llm.seen[0].Prompt, "take the bread out") {
		t.Errorf("the prompt does not carry the request:\n%s", llm.seen[0].Prompt)
	}
}

// TestRenderTimer_AFailedRephrasingDeliversTheUsersOwnWords: the user
// asked for a reminder, and a model outage is not a reason to withhold it.
func TestRenderTimer_AFailedRephrasingDeliversTheUsersOwnWords(t *testing.T) {
	for _, tc := range []struct {
		name string
		llm  *scriptedLLM
	}{
		{"the provider errors", &scriptedLLM{err: errors.New("no provider bound")}},
		{"the provider answers with nothing", &scriptedLLM{text: "   "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &recordingLog{}
			r := checkRunner{ids: &countingIDs{}, log: log, llm: tc.llm}

			got, err := r.renderTimer(context.Background(), dueTimer("t", "take the bread out", rephraseFireAt), rephraseFireAt.Add(time.Minute))
			if err != nil {
				t.Fatalf("renderTimer: %v", err)
			}

			if got.text != "take the bread out" {
				t.Errorf("text = %q, want the user's own words verbatim", got.text)
			}
			if got.rendered != nil {
				t.Errorf("rendered = %q — nothing was worded, so the column stays NULL", *got.rendered)
			}
			if n := log.count(ports.ActionCheckTimerRephraseFailed); n != 1 {
				t.Errorf("%d degradation rows, want 1 — the glass box should show the user got their own words back", n)
			}
		})
	}
}

// TestRenderTimer_AGenericNudgeMakesNoProviderCall: spending one on a
// nudge with no content would be paying for a sentence the model would
// have to invent.
func TestRenderTimer_AGenericNudgeMakesNoProviderCall(t *testing.T) {
	llm := &scriptedLLM{text: "should never be used"}
	r := checkRunner{ids: &countingIDs{}, log: &recordingLog{}, llm: llm}

	got, err := r.renderTimer(context.Background(), dueTimer("t", "", rephraseFireAt), rephraseFireAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("renderTimer: %v", err)
	}

	if llm.calls != 0 {
		t.Fatalf("%d provider calls for a generic nudge, want 0", llm.calls)
	}
	if got.text != genericNudge {
		t.Errorf("text = %q, want the generic nudge", got.text)
	}
	if got.rendered != nil {
		t.Errorf("rendered = %q, want NULL — nothing was worded", *got.rendered)
	}
}

// TestRenderTimer_TheDelayCaveatIsSweptAcrossItsBoundary is R4.2.
//
// Swept rather than sampled, and the boundary itself is asserted: m3a's F6
// decided DelayCaveat is INCLUSIVE deliberately, on the grounds that a
// caveat that was not strictly necessary costs one clause of politeness
// while a missing one on a genuinely late delivery is the failure ADR-0009
// exists to prevent.
func TestRenderTimer_TheDelayCaveatIsSweptAcrossItsBoundary(t *testing.T) {
	boundary := time.Duration(prospection.DelayCaveatMinutes) * time.Minute

	for offset := -5 * time.Minute; offset <= 5*time.Minute; offset += time.Minute {
		overdue := boundary + offset
		if overdue < 0 {
			continue
		}
		t.Run(overdue.String(), func(t *testing.T) {
			// No provider: the fallback path is where withCaveat runs,
			// and a successful rephrasing is asked to mention the delay
			// in its own sentence instead.
			r := checkRunner{ids: &countingIDs{}, log: &recordingLog{}}

			got, err := r.renderTimer(context.Background(),
				dueTimer("t", "take the bread out", rephraseFireAt), rephraseFireAt.Add(overdue))
			if err != nil {
				t.Fatalf("renderTimer: %v", err)
			}

			mentioned := strings.Contains(got.text, "later than you asked")
			want := overdue >= boundary
			if mentioned != want {
				t.Fatalf("overdue %s: caveat mentioned = %v, want %v — the boundary is inclusive by m3a's F6",
					overdue, mentioned, want)
			}
		})
	}
}

// TestRephrasePrompt_AsksForTheDelayOnlyWhenItIsLate: a successful
// rephrasing carries the caveat in its own sentence, so the prompt has to
// be the thing that asks — appending afterwards would say it twice.
func TestRephrasePrompt_AsksForTheDelayOnlyWhenItIsLate(t *testing.T) {
	onTime := rephrasePrompt("x", time.Minute)
	late := rephrasePrompt("x", time.Duration(prospection.DelayCaveatMinutes)*time.Minute)

	if strings.Contains(onTime, "later than they asked") {
		t.Errorf("an on-time prompt asks for a delay note:\n%s", onTime)
	}
	if !strings.Contains(late, "later than they asked") {
		t.Errorf("a late prompt does not ask for a delay note:\n%s", late)
	}
}
