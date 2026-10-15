package classify

import (
	"strconv"
	"testing"
)

// reminderBase is a classification whose only reminder fields are the ones a
// test adds to it, so "the rest survived" is a claim about a known baseline.
func reminderPayload(extra string) string {
	return `{"type":"task","normalized_content":"buy milk","weight":0.6,"decay_rate":0.1` + extra + `}`
}

// TestAllReminderIntentsAndAnswersSwept pins the two closed vocabularies of
// ADR-0033 point 1, so a member added later fails here until it is named.
func TestAllReminderIntentsAndAnswersSwept(t *testing.T) {
	assertVocabulary(t, AllReminderIntents(), map[ReminderIntent]bool{
		ReminderIntentNone: true, ReminderIntentStop: true, ReminderIntentKeep: true,
	})
	assertVocabulary(t, AllQuestionAnswers(), map[QuestionAnswer]bool{
		QuestionAnswerNone: true, QuestionAnswerWhen: true, QuestionAnswerNo: true, QuestionAnswerChoice: true,
	})
}

// TestDecode_ReminderFields: the fields decode, and an unknown enum, a wrong
// type or a choice_number outside 1-5 degrades that field alone (I14).
func TestDecode_ReminderFields(t *testing.T) {
	t.Run("a well-formed answer decodes", func(t *testing.T) {
		c, err := Decode(reminderPayload(`,"reminder_intent":"stop","question_answer":"choice","choice_number":3`), testNow)
		if err != nil || len(c.Degradations) != 0 {
			t.Fatalf("Decode = %v, degraded %v; want clean", err, c.Degradations)
		}
		if c.ReminderIntent == nil || *c.ReminderIntent != ReminderIntentStop {
			t.Errorf("ReminderIntent = %v, want stop", c.ReminderIntent)
		}
		if c.QuestionAnswer == nil || *c.QuestionAnswer != QuestionAnswerChoice {
			t.Errorf("QuestionAnswer = %v, want choice", c.QuestionAnswer)
		}
		if c.ChoiceNumber == nil || *c.ChoiceNumber != 3 {
			t.Errorf("ChoiceNumber = %v, want 3", c.ChoiceNumber)
		}
	})
	t.Run("the last position of the list decodes", func(t *testing.T) {
		c, _ := Decode(reminderPayload(`,"choice_number":`+strconv.Itoa(MaxReminderChoices)), testNow)
		if len(c.Degradations) != 0 || c.ChoiceNumber == nil || *c.ChoiceNumber != MaxReminderChoices {
			t.Errorf("got %v degraded %v, want %d clean", c.ChoiceNumber, c.Degradations, MaxReminderChoices)
		}
	})
	t.Run("absent fields stay nil and are not a loss", func(t *testing.T) {
		c, _ := Decode(reminderPayload(``), testNow)
		if len(c.Degradations) != 0 || c.ReminderIntent != nil || c.QuestionAnswer != nil || c.ChoiceNumber != nil {
			t.Errorf("got %+v, want no reminder field and no degradation", c)
		}
	})
	for _, tc := range []struct {
		name, extra, field string
		reason             Reason
	}{
		{"unknown intent", `,"reminder_intent":"remove"`, "reminder_intent", ReasonUnknownEnum},
		{"intent of the wrong type", `,"reminder_intent":true`, "reminder_intent", ReasonWrongType},
		{"unknown answer", `,"question_answer":"maybe"`, "question_answer", ReasonUnknownEnum},
		{"answer of the wrong type", `,"question_answer":1`, "question_answer", ReasonWrongType},
		{"choice below the list", `,"choice_number":0`, "choice_number", ReasonBadFormat},
		{"choice past the list", `,"choice_number":` + strconv.Itoa(MaxReminderChoices+1), "choice_number", ReasonBadFormat},
		{"choice as text", `,"choice_number":"2"`, "choice_number", ReasonWrongType},
		{"choice with a fraction", `,"choice_number":2.5`, "choice_number", ReasonWrongType},
		{"choice null", `,"choice_number":null`, "choice_number", ReasonWrongType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Decode(reminderPayload(tc.extra), testNow)
			if err != nil {
				t.Fatal(err)
			}
			assertOnlyDegraded(t, c, tc.field, tc.reason)
			if c.Kind == nil || c.NormalizedContent == nil {
				t.Errorf("the rest of the classification did not survive: %+v", c)
			}
			if c.ChoiceNumber != nil && tc.field == "choice_number" {
				t.Errorf("ChoiceNumber = %d, want nil", *c.ChoiceNumber)
			}
		})
	}
}

// TestDecode_RemindRequestBoolOrNull: true, false and null decode; anything
// else degrades. A null is an answer ("not a request"), not a loss.
func TestDecode_RemindRequestBoolOrNull(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want *bool
	}{{`true`, boolPtr(true)}, {`false`, boolPtr(false)}, {`null`, nil}} {
		c, _ := Decode(reminderPayload(`,"remind_request":`+tc.raw), testNow)
		if len(c.Degradations) != 0 {
			t.Errorf("remind_request %s degraded %v, want clean", tc.raw, c.Degradations)
		}
		if (c.RemindRequest == nil) != (tc.want == nil) || (tc.want != nil && *c.RemindRequest != *tc.want) {
			t.Errorf("remind_request %s decoded %v, want %v", tc.raw, c.RemindRequest, tc.want)
		}
	}
	for _, raw := range []string{`"yes"`, `1`, `{}`} {
		c, _ := Decode(reminderPayload(`,"remind_request":`+raw), testNow)
		assertOnlyDegraded(t, c, "remind_request", ReasonWrongType)
		if c.RemindRequest != nil {
			t.Errorf("remind_request %s decoded %v, want nil", raw, *c.RemindRequest)
		}
	}
}

// TestDecode_ReminderTarget: a position 1-5 and "unclear" decode; 0, 6 and
// "2" degrade.
func TestDecode_ReminderTarget(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want ReminderTarget
	}{{`1`, ReminderTarget{Position: 1}}, {`5`, ReminderTarget{Position: 5}}, {`"` + ReminderTargetUnclear + `"`, ReminderTarget{Unclear: true}}} {
		c, _ := Decode(reminderPayload(`,"reminder_target":`+tc.raw), testNow)
		if len(c.Degradations) != 0 || c.ReminderTarget == nil || *c.ReminderTarget != tc.want {
			t.Errorf("reminder_target %s = %v degraded %v; want %+v clean", tc.raw, c.ReminderTarget, c.Degradations, tc.want)
		}
	}
	for _, tc := range []struct {
		raw    string
		reason Reason
	}{{`0`, ReasonBadFormat}, {`6`, ReasonBadFormat}, {`"2"`, ReasonUnknownEnum}, {`null`, ReasonWrongType}, {`true`, ReasonWrongType}} {
		c, _ := Decode(reminderPayload(`,"reminder_target":`+tc.raw), testNow)
		assertOnlyDegraded(t, c, "reminder_target", tc.reason)
		if c.ReminderTarget != nil {
			t.Errorf("reminder_target %s decoded %+v, want nil", tc.raw, *c.ReminderTarget)
		}
	}
}

func boolPtr(v bool) *bool { return &v }
