package classify

import (
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
// what turns "13 fields × 3 malformation shapes" from 39 branches into one
// loop.
//
// The order is doc 02 §5's, and Degradations reports in it.
func fieldSpecs() []fieldSpec {
	return []fieldSpec{
		{"type", true, assignEnum(AllKinds, func(c *Classification, v *Kind) { c.Kind = v })},
		{"normalized_content", true, assignString(func(c *Classification, v *string) { c.NormalizedContent = v })},
		{"structured_data", false, assignRaw},
		{"weight", true, assignFloat(func(c *Classification, v *float64) { c.Weight = v })},
		{"decay_rate", true, assignFloat(func(c *Classification, v *float64) { c.DecayRate = v })},
		{"event_at", false, assignTime(func(c *Classification, v *time.Time) { c.EventAt = v })},
		{"due_at", false, assignTime(func(c *Classification, v *time.Time) { c.DueAt = v })},
		{"nudge_outcome", false, assignEnum(AllNudgeOutcomes, func(c *Classification, v *NudgeOutcome) { c.NudgeOutcome = v })},
		{"relation_outcome", false, assignEnum(AllRelationOutcomes, func(c *Classification, v *RelationOutcome) { c.RelationOutcome = v })},
		{"state_outcome", false, assignEnum(AllStateOutcomes, func(c *Classification, v *StateOutcome) { c.StateOutcome = v })},
		{"task_checkin_outcome", false, assignEnum(AllTaskCheckinOutcomes, func(c *Classification, v *TaskCheckinOutcome) { c.TaskCheckinOutcome = v })},
		{"list_op", false, assignEnum(AllListOps, func(c *Classification, v *ListOp) { c.ListOp = v })},
		{"person_ref_status", false, assignEnum(AllPersonRefStatuses, func(c *Classification, v *PersonRefStatus) { c.PersonRefStatus = v })},
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

func assignString(set func(*Classification, *string)) func(json.RawMessage, *Classification, time.Time) Reason {
	return func(raw json.RawMessage, c *Classification, _ time.Time) Reason {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ReasonWrongType
		}
		set(c, &s)
		return ""
	}
}

func assignFloat(set func(*Classification, *float64)) func(json.RawMessage, *Classification, time.Time) Reason {
	return func(raw json.RawMessage, c *Classification, _ time.Time) Reason {
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return ReasonWrongType
		}
		set(c, &f)
		return ""
	}
}

// assignTime is the only assigner that reads now, and it reads only its
// location — design D2's two accepted formats. RFC3339 carries its own
// offset and keeps it; a date-only value has none, so it becomes midnight in
// now's location. Anything else is ReasonBadFormat: the JSON type was right
// (a string), and dates are not a vocabulary, so neither of the other two
// reasons describes it.
func assignTime(set func(*Classification, *time.Time)) func(json.RawMessage, *Classification, time.Time) Reason {
	return func(raw json.RawMessage, c *Classification, now time.Time) Reason {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ReasonWrongType
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			set(c, &t)
			return ""
		}
		if t, err := time.ParseInLocation(dateOnlyLayout, s, now.Location()); err == nil {
			set(c, &t)
			return ""
		}
		return ReasonBadFormat
	}
}

// assignRaw stores structured_data verbatim. Its shape varies by type and is
// fixed by no schema in doc 02 (testdata/classify/format.md:53), so there is
// no wrong type to detect — any JSON value Salvage completed is valid here,
// and this is the one row that can never degrade.
func assignRaw(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	c.StructuredData = raw
	return ""
}
