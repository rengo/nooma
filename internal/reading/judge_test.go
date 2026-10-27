package reading

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/testdata/llm"
)

const judgePrompt = "the judge prompt this case was recorded against"

func judgeCase(outcome string) llm.Case {
	return llm.Case{
		ID: "j", Task: "relation_evaluation", Message: "Turno con el dentista el jueves 10",
		Candidates:   []llm.Candidate{{ID: "c1", Content: "Comprar café el jueves 10"}},
		Expect:       &llm.Expect{JudgeOutcome: outcome},
		PromptSHA256: Hash(judgePrompt),
	}
}

func TestValidateJudgeCaseRejectsWhatReplayCouldNotMean(t *testing.T) {
	if err := ValidateJudgeCase(judgeCase("new")); err != nil {
		t.Fatalf("a well-formed judge case: %v", err)
	}
	for name, mutate := range map[string]func(*llm.Case){
		"no expect":         func(c *llm.Case) { c.Expect = nil },
		"no outcome":        func(c *llm.Case) { c.Expect.JudgeOutcome = "" },
		"unknown outcome":   func(c *llm.Case) { c.Expect.JudgeOutcome = "unrelated" },
		"no candidate":      func(c *llm.Case) { c.Candidates = nil },
		"a classify task":   func(c *llm.Case) { c.Task = "classify" },
		"a candidate no id": func(c *llm.Case) { c.Candidates[0].ID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			c := judgeCase("new")
			mutate(&c)
			if err := ValidateJudgeCase(c); err == nil {
				t.Error("accepted")
			}
		})
	}
}

// A judgment is read by its effect, as production applies it at the default
// thresholds: a related one below the persist bar, or naming a unit it was
// not shown, stores nothing, and so reads as new.
func TestReplayJudgeReadsAJudgmentByWhatItWouldStore(t *testing.T) {
	cases := map[string]struct {
		expect, response string
		ok               bool
	}{
		"new is new":                          {"new", `{"outcome":"new"}`, true},
		"related is not new":                  {"new", `{"outcome":"related","target_unit_id":"c1","type":"same_day","strength":0.4,"confidence":0.8}`, false},
		"related below persist is new":        {"new", `{"outcome":"related","target_unit_id":"c1","type":"same_day","strength":0.1,"confidence":0.2}`, true},
		"related to an unoffered is new":      {"new", `{"outcome":"related","target_unit_id":"zz","type":"same_day","strength":0.4,"confidence":0.8}`, true},
		"related with no type is new":         {"new", `{"outcome":"related","target_unit_id":"c1","strength":0.4,"confidence":0.8}`, true},
		"related is related":                  {"related", `{"outcome":"related","target_unit_id":"c1","type":"same_topic","strength":0.6,"confidence":0.7}`, true},
		"related at persist is related":       {"related", `{"outcome":"related","target_unit_id":"c1","type":"same_topic","strength":0.6,"confidence":0.3}`, true},
		"related below persist misses":        {"related", `{"outcome":"related","target_unit_id":"c1","type":"same_topic","strength":0.6,"confidence":0.29}`, false},
		"new is not related":                  {"related", `{"outcome":"new"}`, false},
		"duplicate is not related":            {"related", `{"outcome":"duplicate","target_unit_id":"c1","strength":0.9,"confidence":0.9}`, false},
		"duplicate is duplicate":              {"duplicate", `{"outcome":"duplicate","target_unit_id":"c1","strength":0.9,"confidence":0.9}`, true},
		"prose is no reading":                 {"new", `I think they are unrelated.`, false},
		"no outcome is no reading":            {"new", `{"target_unit_id":"c1","confidence":0.8}`, false},
		"duplicate below persist is new":      {"new", `{"outcome":"duplicate","target_unit_id":"c1","confidence":0.2}`, true},
		"related with no confidence is new":   {"new", `{"outcome":"related","target_unit_id":"c1","type":"same_day"}`, true},
		"duplicate with no type is duplicate": {"duplicate", `{"outcome":"duplicate","target_unit_id":"c1","confidence":0.9}`, true},
		"duplicate to an unoffered is new":    {"new", `{"outcome":"duplicate","target_unit_id":"zz","confidence":0.9}`, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := ReplayJudge(judgeCase(tc.expect), judgePrompt, tc.response)
			if tc.ok && err != nil {
				t.Errorf("ReplayJudge = %v, want it to read as %s", err, tc.expect)
			}
			if !tc.ok && err == nil {
				t.Errorf("ReplayJudge accepted it as %s", tc.expect)
			}
		})
	}
}

func TestReplayJudgeFailsOnAnotherPrompt(t *testing.T) {
	err := ReplayJudge(judgeCase("new"), "a prompt changed since", `{"outcome":"new"}`)
	if err == nil || !strings.Contains(err.Error(), "prompt hash mismatch") {
		t.Errorf("ReplayJudge = %v, want a prompt hash mismatch", err)
	}
	c := judgeCase("new")
	c.PromptSHA256 = ""
	if err := ReplayJudge(c, judgePrompt, `{"outcome":"new"}`); err == nil {
		t.Error("a case never recorded replayed")
	}
}

func TestReplayJudgeKnownMisreadStaysMarked(t *testing.T) {
	c := judgeCase("new")
	c.KnownMisread = "judge_outcome: the model links two errands of one day"
	related := `{"outcome":"related","target_unit_id":"c1","type":"same_day","strength":0.4,"confidence":0.8}`
	if err := ReplayJudge(c, judgePrompt, related); err != nil {
		t.Errorf("a marked misread that still misreads: %v", err)
	}
	if err := ReplayJudge(c, judgePrompt, `{"outcome":"new"}`); err == nil {
		t.Error("a mark that no longer holds was accepted")
	}
}
