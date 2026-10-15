package brain

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

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
