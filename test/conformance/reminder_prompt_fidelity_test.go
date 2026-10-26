// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/reading"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/testdata/llm"
)

// noProductionInput is every phrasing case carrying a field production has no
// input for: brain's capture never passes an open reminder question or a
// numbered reminder list to classify.BuildPrompt, so no production prompt
// shows one. Those recordings validate a prompt production never sends, and
// stay listed here, shrink-only, until production renders them or the cases
// drop the fields (TestReminderPrompts_TheUnreachableListOnlyShrinks).
var noProductionInput = []string{
	"classify-negative-bare-listo-with-candidates",
	"classify-negative-bare-no-with-candidates",
	"classify-negative-bare-si-with-candidates",
	"classify-negative-bare-ya-with-candidates",
	"classify-negative-listo-nos-vemos-with-candidates",
	"classify-negative-ya-lo-hice-with-candidates",
	"classify-stop-english-curly-apostrophe",
	"classify-stop-english-names-the-first",
	"classify-stop-english-no-apostrophe",
	"classify-stop-listo-no-me-lo-recuerdes",
	"classify-stop-names-the-second",
	"classify-stop-no-me-lo-recuerdes-mas",
	"classify-stop-one-candidate-other-topic",
	"classify-undo-bare-dale-is-not-keep",
	"classify-undo-bare-si-is-not-keep",
	"classify-undo-keep",
	"classify-when-answer-a-day-only",
	"classify-when-answer-a-time",
	"classify-when-answer-bare-no",
	"classify-when-answer-no-hace-falta",
	"classify-which-answer-by-name",
	"classify-which-answer-by-number",
	"classify-which-answer-something-else",
}

// TestReminderPrompts_AreTheProductionPrompt is the fidelity gate: for every
// recorded phrasing, the prompt the recording was made against
// (reading.Prompt) is byte for byte the prompt production's capture sends for
// the same inputs — the message, the instant, and the chat's turns and the
// units they name, stored as serve stores them. A recording validates a
// prompt only if production can send it.
func TestReminderPrompts_AreTheProductionPrompt(t *testing.T) {
	cases, err := llm.ReminderCases()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			if slices.Contains(noProductionInput, c.ID) {
				t.Skip("carries an input production never renders (noProductionInput)")
			}
			if c.Now == "" {
				t.Skip("not recorded yet: no instant to build the prompt at")
			}
			want, err := reading.Prompt(c)
			if err != nil {
				t.Fatal(err)
			}
			if got := productionPrompt(t, c); got != want {
				t.Errorf("the recorded prompt is not the one production sends.\nproduction:\n%s\nrecorded:\n%s", got, want)
			}
		})
	}
}

// TestReminderPrompts_TheUnreachableListOnlyShrinks keeps noProductionInput
// exact: a case that gains an open question or a reminder list fails until it
// is listed, and a listed case that no longer carries one fails until it is
// removed, so the gate above runs on it again.
func TestReminderPrompts_TheUnreachableListOnlyShrinks(t *testing.T) {
	cases, err := llm.ReminderCases()
	if err != nil {
		t.Fatal(err)
	}
	var carrying []string
	for _, c := range cases {
		if c.OpenQuestion != nil || len(c.CandidatesList) > 0 {
			carrying = append(carrying, c.ID)
		}
	}
	if !slices.Equal(carrying, noProductionInput) {
		t.Errorf("cases carrying an open question or a reminder list = %v, noProductionInput = %v: keep them equal", carrying, noProductionInput)
	}
}

// productionPrompt runs c's message through brain's capture, as serve wires
// it, and returns the classification prompt it sent. Each listed item is a
// live unit and each turn a stored turn about the units its letters name.
func productionPrompt(t *testing.T, c llm.Case) string {
	t.Helper()
	ctx := context.Background()
	now, err := time.Parse(time.RFC3339, c.Now)
	if err != nil {
		t.Fatal(err)
	}
	units, turns := memrepo.NewUnits(), memrepo.NewConversations()
	conv := brain.NewConversation(turns)
	const chat ports.ConversationID = "fidelity"
	in := brain.CaptureInput{Text: c.Message, Channel: "telegram"}
	if cv := c.Conversation; cv != nil {
		in.Conversation = chat
		ids := make([]string, len(cv.Items))
		for i, item := range cv.Items {
			ids[i] = "item-" + strconv.Itoa(i+1)
			u := unit.Unit{ID: ids[i], Type: unit.TypeTask, Status: unit.StatusPool, Source: "telegram",
				Content: item.Content, Title: item.Title, CreatedAt: now, UpdatedAt: now,
				EventAt: caseInstant(t, item.EventAt, now), DueAt: caseInstant(t, item.DueAt, now)}
			if u.EventAt != nil {
				u.Type = unit.TypeEvent
			}
			if err := units.Create(ctx, u); err != nil {
				t.Fatal(err)
			}
		}
		for _, turn := range cv.Turns {
			var about []string
			for _, letter := range turn.About {
				for pos := 1; pos <= len(ids); pos++ {
					if classify.ReferentLetter(pos) == letter {
						about = append(about, ids[pos-1])
					}
				}
			}
			conv.Add(ctx, chat, brain.Turn{At: *caseInstant(t, turn.At, now), Speaker: classify.Speaker(turn.Speaker), Text: turn.Text, UnitIDs: about})
		}
	}
	model := &scriptedLLM{response: `{"type":"chitchat","normalized_content":"x","weight":0.1,"decay_rate":0.3}`}
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), memrepo.NewDecisionLog(), model, model, model, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), seedTimers(t, c, now),
		consolidation.DefaultWeightThreshold, memrepo.NewPendingQuestions()).WithConversation(conv)
	if _, err := svc.Capture(ctx, in); err != nil {
		t.Fatal(err)
	}
	if len(model.prompts) == 0 {
		t.Fatal("capture sent no prompt")
	}
	return model.prompts[0]
}

// caseInstant reads a case's instant: RFC 3339, or a bare date as the start
// of that day in now's zone, as reading reads it.
func caseInstant(t *testing.T, s string, now time.Time) *time.Time {
	t.Helper()
	if s == "" {
		return nil
	}
	if at, err := time.Parse(time.RFC3339, s); err == nil {
		return &at
	}
	day, err := time.ParseInLocation("2006-01-02", s, now.Location())
	if err != nil {
		t.Fatalf("instant %q: %v", s, err)
	}
	return &day
}

// seedTimers stores a case's timers_list as pending timers, as capture reads
// them (ADR-0051), with ids that keep the case's order where two ring at once.
func seedTimers(t *testing.T, c llm.Case, now time.Time) *memrepo.Timers {
	t.Helper()
	timers := memrepo.NewTimers()
	for i, e := range c.TimersList {
		var text *string
		if e.Text != "" {
			text = &e.Text
		}
		at := caseInstant(t, e.At, now)
		if err := timers.Create(context.Background(), ports.Timer{ID: fmt.Sprintf("tmr-%02d", i), FireAt: *at, ActionText: text, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	return timers
}
