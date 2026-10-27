package brain_test

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
)

// TestJudgePrompt_ASharedDayIsNotARelation pins the client's ruling of
// 2026-10-27 (doc 02 §4): two units are related by what they are about, and
// falling on the same day is not a relation by itself. Both judge call sites,
// capture and the nightly connect, build their prompt here, so one rule
// covers both. Whether a model follows it is measured by the recorded judge
// set (testdata/llm/judge), not by this test.
func TestJudgePrompt_ASharedDayIsNotARelation(t *testing.T) {
	p := brain.JudgePrompt(
		unit.Unit{Content: "Turno con el dentista el jueves 10 a las 15"},
		[]unit.Unit{{ID: "c1", Content: "Comprar café el jueves 10"}},
	)
	for _, want := range []string{
		"related only by what they are about",
		"same date or day",
		"is not a relation by itself",
		"answer new",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the judge prompt does not say %q:\n%s", want, p)
		}
	}
	// The rule comes before the fields, so the model reads it before it
	// decides an outcome.
	if strings.Index(p, "is not a relation by itself") > strings.Index(p, "Required fields") {
		t.Error("the shared-day rule comes after the fields it governs")
	}
}
