package brain

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestDigestBoundKeepsTheQuestion: the relation question is a digest's other
// reason to exist, so the bound drops reminder lines to make room for it and
// never the question.
func TestDigestBoundKeepsTheQuestion(t *testing.T) {
	var pending []ports.DueTrigger
	for i := 0; i < 14; i++ {
		pending = append(pending, digestTrigger(fmt.Sprintf("t%02d", i), "", fmt.Sprintf("%02d%s", i, strings.Repeat("x", 430))))
	}
	triggers := &undeliveredTriggers{pending: pending}
	questions := queuedQuestions(t, question("q-1", digestQuestionNow, "plan the trip", "the budget"))
	ch := &sendingChannel{}

	carried, _, err := questionRunner(t, triggers, &digestUnits{}, memrepo.NewState(), memrepo.NewDecisionLog(), ch, questions).
		assembleDigest(context.Background(), digestNow, true)
	if err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}

	msg := ch.sent[0]
	if n := len(utf16.Encode([]rune(msg))); n > 4096 {
		t.Errorf("the digest is %d units", n)
	}
	if !strings.Contains(msg, "plan the trip") || !strings.Contains(msg, "and ") {
		t.Errorf("the question or the \"more\" note is missing:\n%.200s", msg)
	}
	if carried != len(triggers.surfaced) || carried >= 14 {
		t.Errorf("carried %d, surfaced %d; want equal and under 14", carried, len(triggers.surfaced))
	}
}

// boundCase is a digest of two reminders whose second line is padded so the
// whole message is exactly want units, or one more: the edge the bound is about.
func boundCase(t *testing.T, q *ports.RelationQuestion, extra int) (string, []prospection.DigestItem) {
	t.Helper()
	pending := []ports.DueTrigger{digestTrigger("a", "u-a", strings.Repeat("a", 1500)), digestTrigger("b", "u-b", "")}
	carry := []prospection.DigestItem{{ID: "a"}, {ID: "b"}}
	pad := 1
	for range 3 { // converge: the length is linear in the padding
		pending[1].Payload.ActionText = strings.Repeat("b", pad)
		text, _ := renderDigest(sentences(), carry, pending, q)
		pad += 4096 - utf16Len(text)
	}
	pending[1].Payload.ActionText = strings.Repeat("b", pad+extra)
	return renderDigest(sentences(), carry, pending, q)
}

// TestDigestBoundEdge: a digest of exactly 4096 units is kept whole and one of
// 4097 drops its last line, with and without a question, which counts toward
// the 4096. The header counts the lines kept.
func TestDigestBoundEdge(t *testing.T) {
	q := question("q-1", digestQuestionNow, "plan the trip", "the budget")
	for _, c := range []struct {
		name string
		q    *ports.RelationQuestion
	}{{"no question", nil}, {"with a question", &q}} {
		t.Run(c.name, func(t *testing.T) {
			text, sent := boundCase(t, c.q, 0)
			if n := utf16Len(text); n != 4096 || len(sent) != 2 || !strings.HasPrefix(text, "Here are 2 things") {
				t.Errorf("exactly 4096: %d units, %d items kept, want 4096 and both", n, len(sent))
			}

			text, sent = boundCase(t, c.q, 1)
			if n := utf16Len(text); n > 4096 || len(sent) != 1 || sent[0].ID != "a" {
				t.Errorf("4097: %d units, kept %+v, want it within the bound with only the first line", n, sent)
			}
			if !strings.HasPrefix(text, "Here is 1 thing for today:") || !strings.Contains(text, "and 1 more.") {
				t.Errorf("the header counts the kept lines and the note the dropped:\n%.120s", text)
			}
			if c.q != nil && !strings.Contains(text, "plan the trip") {
				t.Error("the question was dropped")
			}
		})
	}
}

// TestDigestSentRationale: the check.digest.sent row counts what was sent, and
// speaks of lines left for tomorrow only when the bound dropped some.
func TestDigestSentRationale(t *testing.T) {
	for _, c := range []struct {
		name     string
		n, chars int
		contains []string
		absent   string
	}{
		{"fits", 3, 20, []string{"carried 3 item(s) and held 0"}, "left"},
		{"bound", 14, 430, nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			var pending []ports.DueTrigger
			for i := 0; i < c.n; i++ {
				pending = append(pending, digestTrigger(fmt.Sprintf("t%02d", i), "", strings.Repeat("x", c.chars)))
			}
			triggers := &undeliveredTriggers{pending: pending}
			log := memrepo.NewDecisionLog()

			carried, _, err := digestRunner(t, triggers, &digestUnits{}, memrepo.NewState(), log, &sendingChannel{}).
				assembleDigest(context.Background(), digestNow, true)
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := log.Since(context.Background(), digestNow.AddDate(0, 0, -1), -1)
			var got string
			for _, r := range rows {
				if r.Action == ports.ActionCheckDigestSent {
					got = r.Rationale
				}
			}
			want := []string{fmt.Sprintf("carried %d item(s) and held 0", carried)}
			if c.name == "bound" {
				want = append(want, fmt.Sprintf("left %d for tomorrow", c.n-carried))
				if carried >= c.n {
					t.Fatalf("carried %d, the bound did not bite", carried)
				}
			}
			for _, w := range append(want, c.contains...) {
				if !strings.Contains(got, w) {
					t.Errorf("rationale %q lacks %q", got, w)
				}
			}
			if c.absent != "" && strings.Contains(got, c.absent) {
				t.Errorf("rationale %q mentions %q though nothing was dropped", got, c.absent)
			}
		})
	}
}
