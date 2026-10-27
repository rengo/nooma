package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/reading"
	"github.com/rengo/nooma/test/support/goldenset"
	"github.com/rengo/nooma/testdata/llm"
)

// judgePromptOf is the prompt production sends the judge for c: the new unit's
// content against its candidates, through the one builder capture and the
// nightly connect both call.
func judgePromptOf(c llm.Case) string {
	candidates := make([]unit.Unit, len(c.Candidates))
	for i, cand := range c.Candidates {
		candidates[i] = unit.Unit{ID: cand.ID, Content: cand.Content}
	}
	return brain.JudgePrompt(unit.Unit{Content: c.Message}, candidates)
}

// TestJudgePhrasingSetReadsAsExpected replays every recorded case of
// testdata/llm/judge/ against its hand-written expectation, the reminder
// set's discipline applied to the relation judge (doc 02 §4): the prompt is
// rebuilt and must hash to the one the response was recorded against, so a
// prompt change fails here and names the cases to record again. A case still
// awaiting its recording is skipped and counted; scripts/recordings-complete.sh
// is the merge gate for those.
func TestJudgePhrasingSetReadsAsExpected(t *testing.T) {
	dir := filepath.Join(repoRootFromCaller(t), "testdata", "llm", "judge")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	awaiting, total := 0, 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		total++
		var ex goldenset.LLMExample
		if err := goldenset.Load(filepath.Join(dir, entry.Name()), &ex); err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
			continue
		}
		c := ex.Case()
		if want := strings.TrimSuffix(entry.Name(), ".json"); c.ID != want {
			t.Errorf("%s holds id %q: a case's file is named by its id", entry.Name(), c.ID)
		}
		if err := reading.ValidateJudgeCase(c); err != nil {
			t.Errorf("%s: %v", c.ID, err)
		}
		t.Run(c.ID, func(t *testing.T) {
			if ex.Record {
				awaiting++
				t.Skip("awaiting recording (nooma doctor --record)")
			}
			if err := reading.ReplayJudge(c, judgePromptOf(c), ex.Response); err != nil {
				t.Error(err)
			}
		})
	}
	if total == 0 {
		t.Fatal("the judge set is empty")
	}
	t.Logf("judge set: %d case(s), %d awaiting recording", total, awaiting)
}

// TestJudgePhrasingSetPinsTheSharedDayRule pins the cases the client's ruling
// of 2026-10-27 asked for, by content rather than by count: two unrelated
// things on one day come back new, and two things about one appointment on
// that day still relate.
func TestJudgePhrasingSetPinsTheSharedDayRule(t *testing.T) {
	cases, err := llm.JudgeCases()
	if err != nil {
		t.Fatal(err)
	}
	pins := []struct {
		what, message, candidate, outcome string
	}{
		{"a dentist appointment and a coffee errand on one day", "dentista", "café", "new"},
		{"a dentist appointment and the mouthwash that dentist prescribed", "dentista", "enjuague", "related"},
	}
	for _, pin := range pins {
		found := false
		for _, c := range cases {
			if c.Expect == nil || c.Expect.JudgeOutcome != pin.outcome || len(c.Candidates) != 1 {
				continue
			}
			found = found || strings.Contains(c.Message, pin.message) && strings.Contains(c.Candidates[0].Content, pin.candidate)
		}
		if !found {
			t.Errorf("the judge set has no case for %s expecting %s", pin.what, pin.outcome)
		}
	}
}
