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
