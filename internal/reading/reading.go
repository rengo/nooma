// Package reading is the measure of how well a model reads a reminder
// message (ADR-0033 point 5): it rebuilds the prompt a phrasing is read with,
// hashes it, and compares a response to the reading the phrasing should get.
//
// It is not core. It reads the phrasing set embedded from testdata/llm, which
// core may not import, and the doctor command and the replay test both need
// it, so it cannot live in package main. It does no I/O and reads no clock:
// the instant is the case's own.
package reading

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/testdata/llm"
)

// The fields a case can miss, named as they are on the wire.
const (
	FieldRemindRequest      = "remind_request"
	FieldReminderIntent     = "reminder_intent"
	FieldQuestionAnswer     = "question_answer"
	FieldChoiceNumber       = "choice_number"
	FieldReminderTarget     = "reminder_target"
	FieldDayOnly            = "day_only"
	FieldTitle              = "title"
	FieldTaskCheckinOutcome = "task_checkin_outcome"
	FieldNudgeOutcome       = "nudge_outcome"
	FieldRelationOutcome    = "relation_outcome"
	FieldStateOutcome       = "state_outcome"
	FieldType               = "type"
)

// ReminderFields are the fields the reminder reading rate counts.
var ReminderFields = []string{
	FieldRemindRequest, FieldReminderIntent, FieldQuestionAnswer, FieldChoiceNumber, FieldReminderTarget,
}

// ReminderMissed says whether misses (from Check) include a reminder field, or
// an answer that could not be read at all. ADR-0033's reversal criterion, the
// reading rate, counts only these: a bad title or day-only flag does not make
// a phrasing misread.
func ReminderMissed(misses []string) bool {
	return slices.ContainsFunc(misses, func(m string) bool { return m == "(response)" || slices.Contains(ReminderFields, m) })
}

// Prompt is the prompt production sends for c, at the instant c records.
func Prompt(c llm.Case) (string, error) {
	now, err := time.Parse(time.RFC3339, c.Now)
	if err != nil {
		return "", fmt.Errorf("case %s: now must be RFC 3339 (record it first): %w", c.ID, err)
	}
	return PromptAt(c, now), nil
}

// PromptAt is Prompt at an explicit instant, for a case not yet recorded.
//
// Beliefs are nil, as in captureRunner.at and the quality gate: nothing in
// the prompt may depend on a vault, or a recording would not reproduce.
func PromptAt(c llm.Case, now time.Time) string {
	var rc classify.ReminderContext
	if q := c.OpenQuestion; q != nil {
		rc.Open = &classify.OpenQuestion{Kind: classify.ReminderQuestionKind(q.Kind), Text: q.Text}
	}
	for _, e := range c.CandidatesList {
		rc.Candidates = append(rc.Candidates, classify.ReminderEntry{Label: e.Label, When: e.When})
	}
	return classify.BuildPrompt(c.Message, nil, now, consolidation.DefaultWeightThreshold, rc)
}

// Hash is the prompt_sha256 of a prompt.
func Hash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// Replay judges one recorded case: its prompt must hash to the recorded
// prompt_sha256, and its response must read as c.Expect says. A case marked
// known_misread must still misread: a mark that no longer holds is an error,
// so it is removed the day the model reads the phrasing right.
func Replay(c llm.Case, response string) error {
	prompt, err := Prompt(c)
	if err != nil {
		return err
	}
	if c.PromptSHA256 == "" {
		return fmt.Errorf("case %s: no prompt_sha256: it was never recorded", c.ID)
	}
	if got := Hash(prompt); got != c.PromptSHA256 {
		return fmt.Errorf("case %s: prompt hash mismatch: it was recorded against another prompt; "+
			"set record to true and run nooma doctor --record --force on it", c.ID)
	}
	now, _ := time.Parse(time.RFC3339, c.Now)
	misses := []string{"(response)"}
	if cls, err := classify.Decode(response, now); err == nil {
		misses = Check(c, cls)
	}
	switch {
	case c.KnownMisread != "" && len(misses) == 0:
		return fmt.Errorf("case %s: known_misread (%q) but it reads as expected: remove the mark", c.ID, c.KnownMisread)
	case c.KnownMisread == "" && len(misses) > 0:
		return fmt.Errorf("case %s: misread %s", c.ID, strings.Join(misses, ", "))
	}
	return nil
}

// Check names the fields of cls that are not what c.Expect says, in a fixed
// order. The reminder fields and the outcomes are strict: with nothing
// expected, anything but none, false or absent is a miss. Day only, title and
// type are checked only when expected.
func Check(c llm.Case, cls classify.Classification) []string {
	e := llm.Expect{}
	if c.Expect != nil {
		e = *c.Expect
	}
	var misses []string
	miss := func(field string, bad bool) {
		if bad {
			misses = append(misses, field)
		}
	}
	miss(FieldRemindRequest, (cls.RemindRequest != nil && *cls.RemindRequest) != (e.RemindRequest != nil && *e.RemindRequest))
	miss(FieldReminderIntent, orNone(cls.ReminderIntent) != orDefault(e.ReminderIntent, "none"))
	miss(FieldQuestionAnswer, orNone(cls.QuestionAnswer) != orDefault(e.QuestionAnswer, "none"))
	miss(FieldChoiceNumber, !equalInt(cls.ChoiceNumber, e.ChoiceNumber))
	miss(FieldReminderTarget, targetOf(cls.ReminderTarget) != e.ReminderTarget)
	if e.DayOnly != nil {
		miss(FieldDayOnly, (cls.DueAtDayOnly || cls.EventAtDayOnly) != *e.DayOnly)
	}
	if e.Title != nil {
		miss(FieldTitle, (cls.Title != nil) != *e.Title)
	}
	// An outcome has an effect in production even when nobody expected it: a
	// check-in outcome closes the most recent open check-in, a relation outcome
	// confirms or deletes, a state outcome records a signal. So one the case
	// does not name is a miss. The two check-in fields are one answer: a case
	// naming either leaves the other free only while the reading closes the
	// check-in as the expected one would (the recorded drop read both).
	checkin := (e.TaskCheckinOutcome != "" || e.NudgeOutcome != "") && sameCheckinEffect(e, cls)
	miss(FieldTaskCheckinOutcome, outcomeMissed(e.TaskCheckinOutcome, ptrString(cls.TaskCheckinOutcome), checkin))
	miss(FieldNudgeOutcome, outcomeMissed(e.NudgeOutcome, ptrString(cls.NudgeOutcome), checkin))
	miss(FieldRelationOutcome, outcomeMissed(e.RelationOutcome, ptrString(cls.RelationOutcome), false))
	// No phrasing expects a state answer: the set measures reminder reading.
	miss(FieldStateOutcome, cls.StateOutcome != nil)
	miss(FieldType, e.Type != "" && ptrString(cls.Kind) != e.Type)
	return misses
}

// sameCheckinEffect says whether cls closes an open check-in as the expected
// outcomes would, through the mapping production uses.
func sameCheckinEffect(e llm.Expect, cls classify.Classification) bool {
	var want classify.Classification
	if e.NudgeOutcome != "" {
		o := classify.NudgeOutcome(e.NudgeOutcome)
		want.NudgeOutcome = &o
	}
	if e.TaskCheckinOutcome != "" {
		o := classify.TaskCheckinOutcome(e.TaskCheckinOutcome)
		want.TaskCheckinOutcome = &o
	}
	wantRes, wantOK := classify.ResolveCheckin(want)
	gotRes, gotOK := classify.ResolveCheckin(cls)
	return wantRes == gotRes && wantOK == gotOK
}

// outcomeMissed says whether an outcome field reads wrong: the expected value
// when one is named, else nothing, unless a sibling field names the answer.
func outcomeMissed(expected, got string, siblingExpected bool) bool {
	switch {
	case expected != "":
		return got != expected
	case siblingExpected:
		return false
	}
	return got != ""
}

func ptrString[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

func orNone[T ~string](p *T) string { return orDefault(ptrString(p), "none") }

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func equalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func targetOf(t *classify.ReminderTarget) string {
	switch {
	case t == nil:
		return ""
	case t.Unclear:
		return classify.ReminderTargetUnclear
	}
	return strconv.Itoa(t.Position)
}

// ValidateCase rejects a case whose expectation or context names something
// the vocabulary does not have, so a typo fails at once instead of reading as
// a miss on the day the case is recorded.
func ValidateCase(c llm.Case) error {
	if c.Expect == nil {
		return fmt.Errorf("case %s: expect is required", c.ID)
	}
	e := c.Expect
	var errs []error
	oneOf := func(field, v string, vocab []string) {
		if v != "" && !slices.Contains(vocab, v) {
			errs = append(errs, fmt.Errorf("%s %q is not one of %v", field, v, vocab))
		}
	}
	oneOf(FieldReminderIntent, e.ReminderIntent, strs(classify.AllReminderIntents()))
	oneOf(FieldQuestionAnswer, e.QuestionAnswer, strs(classify.AllQuestionAnswers()))
	oneOf(FieldTaskCheckinOutcome, e.TaskCheckinOutcome, strs(classify.AllTaskCheckinOutcomes()))
	oneOf(FieldNudgeOutcome, e.NudgeOutcome, strs(classify.AllNudgeOutcomes()))
	oneOf(FieldRelationOutcome, e.RelationOutcome, strs(classify.AllRelationOutcomes()))
	oneOf(FieldType, e.Type, strs(classify.AllKinds()))
	if n := e.ChoiceNumber; n != nil && (*n < 1 || *n > classify.MaxReminderChoices) {
		errs = append(errs, fmt.Errorf("choice_number %d is outside 1-%d", *n, classify.MaxReminderChoices))
	}
	if t := e.ReminderTarget; t != "" && t != classify.ReminderTargetUnclear {
		if n, err := strconv.Atoi(t); err != nil || n < 1 || n > classify.MaxReminderChoices {
			errs = append(errs, fmt.Errorf("reminder_target %q is not 1-%d or %q", t, classify.MaxReminderChoices, classify.ReminderTargetUnclear))
		}
	}
	if q := c.OpenQuestion; q != nil {
		oneOf("open_question.kind", q.Kind, []string{
			string(classify.QuestionRemindWhen), string(classify.QuestionWhichReminder), string(classify.QuestionUndo)})
		if q.Kind == "" || q.Text == "" {
			errs = append(errs, errors.New("open_question needs a kind and a text"))
		}
	}
	if len(c.CandidatesList) > classify.MaxReminderChoices {
		errs = append(errs, fmt.Errorf("candidates_list holds %d entries, at most %d", len(c.CandidatesList), classify.MaxReminderChoices))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("case %s: %w", c.ID, err)
	}
	return nil
}

func strs[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}
