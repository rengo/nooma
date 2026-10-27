package reading

import (
	"errors"
	"fmt"
	"slices"

	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/testdata/llm"
)

// FieldJudgeOutcome is the one field a judge case can miss.
const FieldJudgeOutcome = "judge_outcome"

// TaskJudge is the task a judge case is recorded for.
const TaskJudge = "relation_evaluation"

// The outcomes a judge case can expect, the judge's own vocabulary.
var judgeOutcomes = []string{
	string(relation.OutcomeNew), string(relation.OutcomeRelated), string(relation.OutcomeDuplicate),
}

// ValidateJudgeCase rejects a judge case replay could not judge: one with no
// expected outcome, an outcome outside the judge's vocabulary, or nothing to
// compare against.
func ValidateJudgeCase(c llm.Case) error {
	var errs []error
	if c.Task != TaskJudge {
		errs = append(errs, fmt.Errorf("task %q is not %s", c.Task, TaskJudge))
	}
	if c.Expect == nil || !slices.Contains(judgeOutcomes, c.Expect.JudgeOutcome) {
		errs = append(errs, fmt.Errorf("expect.judge_outcome must be one of %v", judgeOutcomes))
	}
	if len(c.Candidates) == 0 {
		errs = append(errs, errors.New("a judge case needs a candidate"))
	}
	for _, cand := range c.Candidates {
		if cand.ID == "" || cand.Content == "" {
			errs = append(errs, errors.New("every candidate needs an id and a content"))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("case %s: %w", c.ID, err)
	}
	return nil
}

// ReplayJudge judges one recorded judge case: prompt, which the caller builds
// through brain.JudgePrompt as production does, must hash to the recorded
// prompt_sha256, and the response must store what c.Expect says. A
// known_misread mark must still misread, as in Replay.
func ReplayJudge(c llm.Case, prompt, response string) error {
	if err := recordedAgainst(c, prompt); err != nil {
		return err
	}
	var misses []string
	if got, ok := storedOutcome(c, response); !ok || got != c.Expect.JudgeOutcome {
		misses = append(misses, FieldJudgeOutcome)
	}
	return verdictOf(c, misses)
}

// storedOutcome is what production would store from response, at the default
// thresholds: relation.Propose's reading, the rule capture and the nightly
// connect both apply, then relation.Decide. A judgment that decided nothing,
// named a unit it was not shown, or falls below the persist bar stores
// nothing and reads as new. A response with no outcome at all is no reading.
func storedOutcome(c llm.Case, response string) (string, bool) {
	j, err := relation.DecodeJudgment(response)
	if err != nil || j.Outcome == nil {
		return "", false
	}
	offered := make([]string, len(c.Candidates))
	for i, cand := range c.Candidates {
		offered[i] = cand.ID
	}
	p, reading := relation.Propose(j, offered)
	if reading != relation.Proposed || relation.Decide(p.Confidence, relation.Resolve(nil)) == relation.Discard {
		return string(relation.OutcomeNew), true
	}
	return string(p.Outcome), true
}
