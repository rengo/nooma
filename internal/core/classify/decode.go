package classify

import (
	"bytes"
	"encoding/json"
	"time"
)

// dateOnlyLayout is the second accepted date format, per design D2 — the one
// testdata/classify/format.md's own example uses. A value in this layout has
// no zone of its own, which is the entire reason Decode takes an instant.
const dateOnlyLayout = "2006-01-02"

// fieldSpec is one wire field: its name, whether its absence is a loss worth
// reporting, and how to put it on a Classification.
//
// assign returns the empty Reason on success. The five Reason constants are
// all non-empty, so the zero value is unambiguous, and it saves an ok-bool
// on every one of the thirteen rows.
type fieldSpec struct {
	name     string
	required bool
	assign   func(raw json.RawMessage, c *Classification, now time.Time) Reason
}

// fieldSpecs is the decoder's whole table — design D11 point 1. One row per
// wire field, and one loop over it in Decode, so statement count is O(1) in
// the number of fields rather than O(fields): adding a field adds a row
// (data, zero statements) and a three-line assigner, not a branch. That is
// what turns "every field × 3 malformation shapes" from a branch per pair
// into one loop. A count is deliberately not written here: it was "13"
// through two field additions and was wrong for both.
//
// The order is doc 02 §5's, and Degradations reports in it.
func fieldSpecs() []fieldSpec {
	return []fieldSpec{
		{"type", true, assignEnum(AllKinds, func(c *Classification, v *Kind) { c.Kind = v })},
		{"normalized_content", true, assignString(func(c *Classification, v *string) { c.NormalizedContent = v })},
		{"structured_data", false, assignRaw},
		{"weight", true, assignFloat(func(c *Classification, v *float64) { c.Weight = v })},
		{"decay_rate", true, assignFloat(func(c *Classification, v *float64) { c.DecayRate = v })},
		{"event_at", false, assignTime(func(c *Classification, v *time.Time, dayOnly bool) { c.EventAt, c.EventAtDayOnly = v, dayOnly })},
		{"due_at", false, assignTime(func(c *Classification, v *time.Time, dayOnly bool) { c.DueAt, c.DueAtDayOnly = v, dayOnly })},
		{"nudge_outcome", false, assignEnum(AllNudgeOutcomes, func(c *Classification, v *NudgeOutcome) { c.NudgeOutcome = v })},
		{"relation_outcome", false, assignEnum(AllRelationOutcomes, func(c *Classification, v *RelationOutcome) { c.RelationOutcome = v })},
		{"state_outcome", false, assignEnum(AllStateOutcomes, func(c *Classification, v *StateOutcome) { c.StateOutcome = v })},
		{"task_checkin_outcome", false, assignEnum(AllTaskCheckinOutcomes, func(c *Classification, v *TaskCheckinOutcome) { c.TaskCheckinOutcome = v })},
		{"list_op", false, assignEnum(AllListOps, func(c *Classification, v *ListOp) { c.ListOp = v })},
		{"person_ref_status", false, assignEnum(AllPersonRefStatuses, func(c *Classification, v *PersonRefStatus) { c.PersonRefStatus = v })},
		{"interrupt_level", false, assignInterruptLevel},
		{"recurrence_rule", false, assignEnum(AllRecurrenceRules,
			func(c *Classification, v *RecurrenceRule) { c.RecurrenceRule = v })},
		// Optional, not required, and the reason is the corpus rather than
		// the field's importance. Marking it required would report a
		// Degradation for every recording in testdata/llm/cases/ — all of
		// them predate this field — and `nooma doctor`'s quality gate
		// counts a clean case as one with zero degradations, so a green
		// 22/22 would read 0/22 overnight. The alternative is editing
		// twenty-two files whose whole value is being real recorded
		// responses. An absent language is not a quality defect the way an
		// absent weight is: it falls back and nothing is lost.
		{"language", false, assignEnum(AllLanguages,
			func(c *Classification, v *Language) { c.Language = v })},
		// The reminder fields (ADR-0033 point 1) are optional for the reason
		// language is: no existing recording carries them, and an absent one
		// reads as none rather than as a loss.
		{"title", false, nullIsAbsent(assignTitle)},
		{"remind_request", false, assignRemindRequest},
		{"reminder_intent", false, nullIsAbsent(assignEnum(AllReminderIntents,
			func(c *Classification, v *ReminderIntent) { c.ReminderIntent = v }))},
		{"question_answer", false, nullIsAbsent(assignEnum(AllQuestionAnswers,
			func(c *Classification, v *QuestionAnswer) { c.QuestionAnswer = v }))},
		{"choice_number", false, nullIsAbsent(assignChoiceNumber)},
		{"reminder_target", false, nullIsAbsent(assignReminderTarget)},
		// ADR-0036's follow-up fields, optional for the same reason.
		{"referent", false, nullIsAbsent(assignReferent)},
		{"clarifying_question", false, nullIsAbsent(assignClarifyingQuestion)},
		{"reminder_leads_minutes", false, nullIsAbsent(assignReminderLeads)},
	}
}

// Decode turns a provider's raw capture response into a Classification —
// docs/02-cognitive-core.md §5 step 1, I14. A malformed or truncated field
// degrades to its absent value and is recorded in Degradations; the rest of
// the classification survives. The only error is ErrNoFieldsSalvaged.
//
// now supplies the location a date-only event_at/due_at parses in (design
// D2). Decode reads nothing else from it: internal/core cannot obtain a
// location on its own — forbidigo denies it time.Now and os.Getenv, and
// time.Local is the OS's answer under another name — so the caller passes
// the instant it already read from the clock once (design D4, conflict C7).
// Decode stays pure, and an L1 test fixes the timezone by passing one.
func Decode(raw string, now time.Time) (Classification, error) {
	fields, truncated := Salvage([]byte(raw))
	if len(fields) == 0 {
		return Classification{}, ErrNoFieldsSalvaged
	}

	var c Classification
	for _, spec := range fieldSpecs() {
		value, present := fields[spec.name]
		if !present {
			// Only a required field's absence is a loss. The other nine are
			// optional by testdata/classify/format.md:51-61's own "no"
			// column — reporting every unset orthogonal field would bury
			// the real degradations brain has to write a rationale for.
			if spec.required {
				c.Degradations = append(c.Degradations,
					Degradation{Field: spec.name, Reason: missingReason(truncated)})
			}
			continue
		}
		if r := spec.assign(value, &c, now); r != "" {
			c.Degradations = append(c.Degradations, Degradation{Field: spec.name, Reason: r})
		}
	}
	return c, nil
}

// missingReason distinguishes the two ways a required field can fail to
// arrive. brain writes a different rationale for each (I12): "the model did
// not emit it" is a quality signal about the provider, "the stream was cut"
// is one about the transport.
//
// A truncated payload cannot tell which of its *optional* fields were lost —
// Salvage's flag is per-response, not per-field — and Decode does not pretend
// otherwise: it reports only the required ones.
func missingReason(truncated bool) Reason {
	if truncated {
		return ReasonTruncated
	}
	return ReasonAbsent
}

// decodeEnum matches a raw value against a closed vocabulary — design D11
// point 2. One generic function serves Kind and all six orthogonal fields,
// so there is one set of arms rather than seven, and it is why none of the
// seven vocabularies needs a ParseX.
//
// D11 writes the return as (*T, error). It is (*T, Reason) here because the
// caller needs to tell wrong-type from unknown-enum apart, and converting an
// error back into a Reason would need a type assertion whose failure arm no
// input can reach — D11 point 3's "no unreachable arm" applied to D11 point
// 2's own signature.
func decodeEnum[T ~string](raw json.RawMessage, all []T) (*T, Reason) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, ReasonWrongType
	}
	for _, member := range all {
		if string(member) == s {
			return &member, ""
		}
	}
	return nil, ReasonUnknownEnum
}

// assignEnum builds the assigner for one closed vocabulary. all is taken as
// a function rather than a slice so each row calls AllX by name, which keeps
// the table readable as a list of vocabularies.
func assignEnum[T ~string](all func() []T, set func(*Classification, *T)) func(json.RawMessage, *Classification, time.Time) Reason {
	return func(raw json.RawMessage, c *Classification, _ time.Time) Reason {
		v, r := decodeEnum(raw, all())
		if r != "" {
			return r
		}
		set(c, v)
		return ""
	}
}

// Null handling across the rest of the table, recorded so it is not
// rediscovered: decodeEnum and assignTime read an explicit null into a
// non-pointer too, and both are safe for a reason neither states in its own
// signature. decodeEnum leaves "", a member of no vocabulary in outcomes.go
// or kind.go, so it degrades with ReasonUnknownEnum; assignTime leaves "",
// which parses as neither RFC3339 nor 2006-01-02, so it degrades with
// ReasonBadFormat. Neither can produce a claimed value, which is the failure
// assignString, assignFloat and assignInterruptLevel each had. What both get
// wrong is only the label — doc 02's rationale for ReasonWrongType is "null
// is not the JSON type this field reads" — which makes an I12 rationale less
// precise than it could be rather than miscoding a value, so it is written
// down here instead of fixed by inventing a branch neither field needs.
func assignString(set func(*Classification, *string)) func(json.RawMessage, *Classification, time.Time) Reason {
	return func(raw json.RawMessage, c *Classification, _ time.Time) Reason {
		// Into a *string, for the reason assignFloat gives below, and with a
		// sharper consequence here: ToUnit refuses a classification whose
		// NormalizedContent is nil (ErrNoContent), and a non-nil pointer to
		// "" walks past that check into a persisted unit with empty content.
		// The guard is correct; reading into a plain string let the defect in
		// underneath it. doc 02 §5.1 calls this field's loss the one that is
		// "not survivable downstream" — recall cannot reach a unit with no
		// content — so a claimed empty string is the shape that guarantee
		// most needs to exclude.
		var s *string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ReasonWrongType
		}
		if s == nil {
			return ReasonWrongType
		}
		set(c, s)
		return ""
	}
}

func assignFloat(set func(*Classification, *float64)) func(json.RawMessage, *Classification, time.Time) Reason {
	return func(raw json.RawMessage, c *Classification, _ time.Time) Reason {
		// Into a *float64, not a float64. An explicit `"weight": null` is
		// present rather than absent — Salvage stores any decodable value
		// under its key — so the missing-field branch never runs, and
		// json.Unmarshal accepts null for a non-pointer destination without
		// error, leaving the zero value. Reading into a float64 and taking
		// its address therefore yields a non-nil pointer to 0.0 with no
		// Reason: a claimed zero the model never asked for, which is
		// precisely the collapse Classification's pointer fields exist to
		// prevent (§5.1: "a degraded weight is not a zero weight"). For
		// decay_rate the cost is larger — a λ of 0 never decays, so §6's
		// archiving pass can never reach the unit.
		var f *float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return ReasonWrongType
		}
		if f == nil {
			return ReasonWrongType
		}
		set(c, f)
		return ""
	}
}

// assignTime is the only assigner that reads now, and it reads only its
// location — design D2's two accepted formats. RFC3339 carries its own
// offset and keeps it; a date-only value has none, so it becomes midnight in
// now's location and set is told so (dayOnly): the value's shape, which a
// timestamp at midnight does not share (ADR-0031 D2). Anything else is ReasonBadFormat: the JSON type was right
// (a string), and dates are not a vocabulary, so neither of the other two
// reasons describes it.
func assignTime(set func(*Classification, *time.Time, bool)) func(json.RawMessage, *Classification, time.Time) Reason {
	return func(raw json.RawMessage, c *Classification, now time.Time) Reason {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ReasonWrongType
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			set(c, &t, false)
			return ""
		}
		if t, ok := parseDateOnly(s, now.Location()); ok {
			set(c, &t, true)
			return ""
		}
		return ReasonBadFormat
	}
}

// parseDateOnly is the first instant of the civil day s names, in loc. A day
// whose midnight does not exist (Chile springs forward at 24:00) parses onto the
// previous civil day, and a bare date is the first instant of its own day, so
// the parse steps forward to it.
func parseDateOnly(s string, loc *time.Location) (time.Time, bool) {
	t, err := time.ParseInLocation(dateOnlyLayout, s, loc)
	if err != nil {
		return time.Time{}, false
	}
	for i := 0; i < 4 && t.Format(dateOnlyLayout) != s; i++ {
		t = t.Add(30 * time.Minute)
	}
	return t, true
}

// assignInterruptLevel is interrupt_level's own assigner (design §3.8) —
// assignFloat alone cannot serve this row: it returns only ReasonWrongType,
// and the [0,1] range check is this field's whole point (spec R3.1). A
// value outside that range degrades with ReasonBadFormat, reused rather than
// adding a sixth Reason — owner-review R3, design §3.8's own argument: the
// existing vocabulary already means "the JSON type was right and the value
// is not one this field reads", which an out-of-range number is exactly.
//
// json.Unmarshal itself already rejects NaN/±Inf as invalid JSON number
// syntax (there is no literal token for either in the JSON grammar), so
// those arrive here as ReasonWrongType, not as a value this range check
// ever sees — the same "no unreachable arm" discipline decode.go already
// states for decodeEnum above.
// It unmarshals into a *float64 rather than a float64 on purpose. An
// explicit `"interrupt_level": null` is present, not absent — Salvage
// stores any decodable value under its key — and json.Unmarshal accepts
// null for a non-pointer destination without error, leaving the zero value
// in place. Read into a float64, that turns "the model declined to answer"
// into "the model claimed 0.0", with no Reason recorded to tell them apart.
// Read into a *float64, null arrives as nil and degrades, which is what
// §5.1's "a degraded weight is not a zero weight" requires and what doc 02
// §7's NULL round trip depends on.
func assignInterruptLevel(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	var f *float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return ReasonWrongType
	}
	if f == nil {
		return ReasonWrongType
	}
	if *f < 0 || *f > 1 {
		return ReasonBadFormat
	}
	c.InterruptLevel = f
	return ""
}

// assignRaw stores structured_data verbatim. Its shape varies by type and is
// fixed by no schema in doc 02 (testdata/classify/format.md:53), so there is
// no wrong type to detect — any JSON value Salvage completed is valid here,
// and this is the one row that can never degrade.
func assignRaw(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	c.StructuredData = raw
	return ""
}

// assignRemindRequest reads remind_request: a JSON bool, or null. A null is an
// answer the model may give ("not a request"), so it leaves the field nil
// without a degradation; any other type degrades.
func assignRemindRequest(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	var b *bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return ReasonWrongType
	}
	c.RemindRequest = b
	return ""
}

// readPosition reads a whole number from 1 to MaxReminderChoices. A fraction,
// a string or a null is the wrong type; a whole number outside the list is a
// value this field does not read (ReasonBadFormat).
func readPosition(raw json.RawMessage) (int, Reason) {
	var n *int
	if err := json.Unmarshal(raw, &n); err != nil || n == nil {
		return 0, ReasonWrongType
	}
	if *n < 1 || *n > MaxReminderChoices {
		return 0, ReasonBadFormat
	}
	return *n, ""
}

func assignChoiceNumber(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	n, r := readPosition(raw)
	if r != "" {
		return r
	}
	c.ChoiceNumber = &n
	return ""
}

// assignReminderTarget reads reminder_target: a position or the word
// "unclear". Any other string is outside the vocabulary.
func assignReminderTarget(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	var s *string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == nil {
			return ReasonWrongType
		}
		if *s != ReminderTargetUnclear {
			return ReasonUnknownEnum
		}
		c.ReminderTarget = &ReminderTarget{Unclear: true}
		return ""
	}
	n, r := readPosition(raw)
	if r != "" {
		return r
	}
	c.ReminderTarget = &ReminderTarget{Position: n}
	return ""
}

// assignTitle reads title and keeps what ResolveTitle keeps. A title the check
// drops is a value this field does not read (ReasonBadFormat), recorded so the
// decision log shows the model was asked for one and gave an unusable one.
func assignTitle(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	var s *string
	if err := json.Unmarshal(raw, &s); err != nil || s == nil {
		return ReasonWrongType
	}
	title := ResolveTitle(*s)
	if title == nil {
		return ReasonBadFormat
	}
	c.Title = title
	return ""
}

// nullIsAbsent makes an explicit JSON null the same as an absent key for an
// optional reminder field: the model saying "nothing here" is not a loss, and
// logging a degradation for every plain note answered with "title": null would
// lower the doctor's clean rate for nothing. remind_request reads null the same
// way in its own assigner.
func nullIsAbsent(assign func(json.RawMessage, *Classification, time.Time) Reason) func(json.RawMessage, *Classification, time.Time) Reason {
	return func(raw json.RawMessage, c *Classification, now time.Time) Reason {
		if string(bytes.TrimSpace(raw)) == "null" {
			return ""
		}
		return assign(raw, c, now)
	}
}
