package classify

import (
	"encoding/json"
	"testing"
	"time"
)

// testZone is a fixed non-UTC location built in memory. It is deliberately
// not time.LoadLocation: that reads the tzdata files off disk, and depguard
// denies os to internal/core/** with no $test selector. It is deliberately
// not UTC either — a date-only value parsed in the wrong location is
// indistinguishable from a correct one when the location IS UTC, so a UTC
// fixture would let a time.Local regression pass on any machine whose clock
// happens to be set to UTC (every CI runner).
var testZone = time.FixedZone("test-05", -5*60*60)

// testNow is the instant callers hand to Decode. Its wall value is
// irrelevant — Decode reads nothing from it but Location() — and it is
// stated here once so every table row can pass the same one.
var testNow = time.Date(2026, 8, 1, 12, 0, 0, 0, testZone)

// wholePayload is a classification with every one of the thirteen wire
// fields present and well-formed. Each test below corrupts exactly one field
// of it, so "every other field survived" is a claim about a known baseline
// rather than about whatever the payload happened to contain.
const wholePayload = `{
  "type": "task",
  "normalized_content": "buy milk",
  "structured_data": {"store": "corner"},
  "weight": 1.5,
  "decay_rate": 0.02,
  "event_at": "2026-08-04T09:00:00Z",
  "due_at": "2026-08-05",
  "nudge_outcome": "engaged",
  "relation_outcome": "confirmed",
  "state_outcome": "denied",
  "task_checkin_outcome": "snooze",
  "list_op": "append",
  "person_ref_status": "ambiguous"
}`

// decodeWhole decodes wholePayload and fails the test if it degraded
// anything — the baseline every corruption test measures against.
func decodeWhole(t *testing.T) Classification {
	t.Helper()

	c, err := Decode(wholePayload, testNow)
	if err != nil {
		t.Fatalf("Decode(wholePayload) error = %v, want nil", err)
	}
	if len(c.Degradations) != 0 {
		t.Fatalf("wholePayload degraded %v — the baseline must be clean, or every "+
			"single-field test below measures against the wrong thing", c.Degradations)
	}
	return c
}

// degradedFields lists the field names c reports as degraded, so a test can
// assert "exactly this one, and nothing else".
func degradedFields(c Classification) []string {
	names := make([]string, 0, len(c.Degradations))
	for _, d := range c.Degradations {
		names = append(names, d.Field)
	}
	return names
}

// assertOnlyDegraded checks that c degraded exactly field, for exactly
// reason. This is I14's actual claim — not "the field degraded", but "the
// field degraded and nothing else did".
func assertOnlyDegraded(t *testing.T, c Classification, field string, reason Reason) {
	t.Helper()

	if len(c.Degradations) != 1 {
		t.Fatalf("degraded %v, want exactly [%s] — I14 requires the rest of the "+
			"classification to survive", degradedFields(c), field)
	}
	got := c.Degradations[0]
	if got.Field != field {
		t.Errorf("degraded field = %q, want %q", got.Field, field)
	}
	if got.Reason != reason {
		t.Errorf("degraded %q with reason %q, want %q", got.Field, got.Reason, reason)
	}
}

// TestDecode_NoFieldsSalvaged covers design D1's stated floor: a payload with
// no fields has none to degrade, so it is an error rather than a
// classification whose every field is absent.
func TestDecode_NoFieldsSalvaged(t *testing.T) {
	for _, raw := range []string{
		``,
		`[1, 2, 3]`,
		`"a bare string"`,
		`{`,
		`not json at all`,
	} {
		c, err := Decode(raw, testNow)
		if err == nil {
			t.Errorf("Decode(%q) error = nil, want ErrNoFieldsSalvaged (got %+v)", raw, c)
			continue
		}
		if err != ErrNoFieldsSalvaged {
			t.Errorf("Decode(%q) error = %v, want ErrNoFieldsSalvaged", raw, err)
		}
	}
}

// TestDecode_WrongTypedFieldDegradesAlone is I14 shape 2 (spec R1.2): a field
// carrying a JSON value of the wrong type degrades to absent, and every other
// field is decoded exactly as the clean baseline decoded it.
func TestDecode_WrongTypedFieldDegradesAlone(t *testing.T) {
	tests := map[string]struct {
		payload string
		field   string
	}{
		"weight as a string": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":"heavy","decay_rate":0.02}`,
			field:   "weight",
		},
		"decay_rate as an object": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":{"n":1}}`,
			field:   "decay_rate",
		},
		"normalized_content as a number": {
			payload: `{"type":"task","normalized_content":7,"weight":1.5,"decay_rate":0.02}`,
			field:   "normalized_content",
		},
		"type as a number": {
			payload: `{"type":13,"normalized_content":"buy milk","weight":1.5,"decay_rate":0.02}`,
			field:   "type",
		},
		"nudge_outcome as a bool": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"nudge_outcome":true}`,
			field:   "nudge_outcome",
		},
		// A date as a bare number is the realistic failure — a model that
		// drops the quotes emits 20260805, not a string. It is wrong-type,
		// not bad-format: bad-format is for a string that parses as neither
		// accepted layout, and this never reached a layout at all.
		"due_at as a number": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"due_at":20260805}`,
			field:   "due_at",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c, err := Decode(tc.payload, testNow)
			if err != nil {
				t.Fatalf("Decode error = %v, want nil — a wrong-typed field must not "+
					"fail the whole classification", err)
			}
			assertOnlyDegraded(t, c, tc.field, ReasonWrongType)

			// The three fields every one of these payloads shares, none of
			// which is the corrupted one in more than a single case.
			if tc.field != "type" && (c.Kind == nil || *c.Kind != KindTask) {
				t.Errorf("Kind = %v, want task — it survived in the payload", c.Kind)
			}
			if tc.field != "weight" && (c.Weight == nil || *c.Weight != 1.5) {
				t.Errorf("Weight = %v, want 1.5", c.Weight)
			}
			if tc.field != "normalized_content" &&
				(c.NormalizedContent == nil || *c.NormalizedContent != "buy milk") {
				t.Errorf("NormalizedContent = %v, want %q", c.NormalizedContent, "buy milk")
			}
		})
	}
}

// TestDecode_UnknownEnumDegradesAlone is I14 shape 3 (spec R1.2): a value
// outside a closed vocabulary degrades that field only. It is a distinct
// reason from ReasonWrongType — the JSON type was right, the value was not,
// and brain writes a different rationale for each (I12).
func TestDecode_UnknownEnumDegradesAlone(t *testing.T) {
	tests := map[string]struct {
		payload string
		field   string
	}{
		"type outside the taxonomy": {
			payload: `{"type":"grocery","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02}`,
			field:   "type",
		},
		"nudge_outcome outside its vocabulary": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"nudge_outcome":"ignored"}`,
			field:   "nudge_outcome",
		},
		"list_op outside its vocabulary": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"list_op":"prepend"}`,
			field:   "list_op",
		},
		// state_outcome's vocabulary is confirmed|denied. "rejected" is a
		// real value — of relation_outcome. Borrowing across two
		// vocabularies that share "confirmed" is the realistic model error,
		// and it must be rejected as firmly as nonsense is.
		"state_outcome borrowing relation_outcome's value": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"state_outcome":"rejected"}`,
			field:   "state_outcome",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c, err := Decode(tc.payload, testNow)
			if err != nil {
				t.Fatalf("Decode error = %v, want nil", err)
			}
			assertOnlyDegraded(t, c, tc.field, ReasonUnknownEnum)

			if tc.field != "type" && (c.Kind == nil || *c.Kind != KindTask) {
				t.Errorf("Kind = %v, want task — an unknown enum elsewhere must not touch it", c.Kind)
			}
			if c.NormalizedContent == nil || *c.NormalizedContent != "buy milk" {
				t.Errorf("NormalizedContent = %v, want %q", c.NormalizedContent, "buy milk")
			}
		})
	}
}

// TestDecode_DegradedKindLeavesEverythingElsePopulated is 7a.4's own named
// case: Kind is the field most likely to be treated as load-bearing, and I14
// says it is not. A classification whose type failed still carries every
// other field, because brain decides what to do about a missing type — the
// decoder does not decide for it.
func TestDecode_DegradedKindLeavesEverythingElsePopulated(t *testing.T) {
	baseline := decodeWhole(t)

	broken := `{
	  "type": "grocery",
	  "normalized_content": "buy milk",
	  "structured_data": {"store": "corner"},
	  "weight": 1.5,
	  "decay_rate": 0.02,
	  "event_at": "2026-08-04T09:00:00Z",
	  "due_at": "2026-08-05",
	  "nudge_outcome": "engaged",
	  "relation_outcome": "confirmed",
	  "state_outcome": "denied",
	  "task_checkin_outcome": "snooze",
	  "list_op": "append",
	  "person_ref_status": "ambiguous"
	}`

	c, err := Decode(broken, testNow)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	assertOnlyDegraded(t, c, "type", ReasonUnknownEnum)

	if c.Kind != nil {
		t.Errorf("Kind = %v, want nil — an unknown taxonomy value decodes to absent", *c.Kind)
	}

	// Every other field matches the clean baseline exactly.
	if c.NormalizedContent == nil || *c.NormalizedContent != *baseline.NormalizedContent {
		t.Errorf("NormalizedContent = %v, want %v", c.NormalizedContent, baseline.NormalizedContent)
	}
	if c.Weight == nil || *c.Weight != *baseline.Weight {
		t.Errorf("Weight = %v, want %v", c.Weight, baseline.Weight)
	}
	if c.DecayRate == nil || *c.DecayRate != *baseline.DecayRate {
		t.Errorf("DecayRate = %v, want %v", c.DecayRate, baseline.DecayRate)
	}
	if c.EventAt == nil || !c.EventAt.Equal(*baseline.EventAt) {
		t.Errorf("EventAt = %v, want %v", c.EventAt, baseline.EventAt)
	}
	if c.DueAt == nil || !c.DueAt.Equal(*baseline.DueAt) {
		t.Errorf("DueAt = %v, want %v", c.DueAt, baseline.DueAt)
	}
	if c.NudgeOutcome == nil || *c.NudgeOutcome != *baseline.NudgeOutcome {
		t.Errorf("NudgeOutcome = %v, want %v", c.NudgeOutcome, baseline.NudgeOutcome)
	}
	if c.RelationOutcome == nil || *c.RelationOutcome != *baseline.RelationOutcome {
		t.Errorf("RelationOutcome = %v, want %v", c.RelationOutcome, baseline.RelationOutcome)
	}
	if c.StateOutcome == nil || *c.StateOutcome != *baseline.StateOutcome {
		t.Errorf("StateOutcome = %v, want %v", c.StateOutcome, baseline.StateOutcome)
	}
	if c.TaskCheckinOutcome == nil || *c.TaskCheckinOutcome != *baseline.TaskCheckinOutcome {
		t.Errorf("TaskCheckinOutcome = %v, want %v", c.TaskCheckinOutcome, baseline.TaskCheckinOutcome)
	}
	if c.ListOp == nil || *c.ListOp != *baseline.ListOp {
		t.Errorf("ListOp = %v, want %v", c.ListOp, baseline.ListOp)
	}
	if c.PersonRefStatus == nil || *c.PersonRefStatus != *baseline.PersonRefStatus {
		t.Errorf("PersonRefStatus = %v, want %v", c.PersonRefStatus, baseline.PersonRefStatus)
	}
	if string(c.StructuredData) != string(baseline.StructuredData) {
		t.Errorf("StructuredData = %s, want %s", c.StructuredData, baseline.StructuredData)
	}
}

// TestDecode_OrthogonalFieldsDegradeIndependently is spec R1.2's "the six
// orthogonal resolution fields degrade independently of each other and of
// type" (spec.md:103-104). Each of the six is corrupted in turn against an
// otherwise whole payload; the other five must survive every time.
func TestDecode_OrthogonalFieldsDegradeIndependently(t *testing.T) {
	orthogonal := []struct {
		field   string
		present func(Classification) bool
	}{
		{"nudge_outcome", func(c Classification) bool { return c.NudgeOutcome != nil }},
		{"relation_outcome", func(c Classification) bool { return c.RelationOutcome != nil }},
		{"state_outcome", func(c Classification) bool { return c.StateOutcome != nil }},
		{"task_checkin_outcome", func(c Classification) bool { return c.TaskCheckinOutcome != nil }},
		{"list_op", func(c Classification) bool { return c.ListOp != nil }},
		{"person_ref_status", func(c Classification) bool { return c.PersonRefStatus != nil }},
	}

	// The table covers all six, and says so — a seventh orthogonal field
	// added without a row here fails rather than going quietly untested
	// (design D11 point 4).
	const wantOrthogonal = 6
	if len(orthogonal) != wantOrthogonal {
		t.Fatalf("table covers %d orthogonal fields, doc 02:120-123 names %d",
			len(orthogonal), wantOrthogonal)
	}

	for _, corrupted := range orthogonal {
		t.Run(corrupted.field, func(t *testing.T) {
			// Rebuild wholePayload with exactly this one field set to a
			// value outside its vocabulary.
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(wholePayload), &fields); err != nil {
				t.Fatalf("wholePayload is not valid JSON: %v", err)
			}
			fields[corrupted.field] = json.RawMessage(`"not_a_member"`)
			payload, err := json.Marshal(fields)
			if err != nil {
				t.Fatalf("re-marshalling the payload: %v", err)
			}

			c, err := Decode(string(payload), testNow)
			if err != nil {
				t.Fatalf("Decode error = %v, want nil", err)
			}
			assertOnlyDegraded(t, c, corrupted.field, ReasonUnknownEnum)

			for _, other := range orthogonal {
				if other.field == corrupted.field {
					continue
				}
				if !other.present(c) {
					t.Errorf("%s is absent after %s degraded — the six are orthogonal "+
						"(doc 02:120), they do not fall together",
						other.field, corrupted.field)
				}
			}
			if c.Kind == nil || *c.Kind != KindTask {
				t.Errorf("Kind = %v, want task — orthogonal to all six", c.Kind)
			}
		})
	}
}

// TestDecode_DatesParseInTheGivenLocation is C7's whole point, and the reason
// Decode takes an instant at all (design D1, D2). A date-only value is
// midnight in now's location — never in the OS's, which internal/core cannot
// read and this test would not catch if it passed UTC.
func TestDecode_DatesParseInTheGivenLocation(t *testing.T) {
	payload := `{"type":"event","normalized_content":"standup","weight":1.5,
	             "decay_rate":0.02,"due_at":"2026-08-05"}`

	c, err := Decode(payload, testNow)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	if len(c.Degradations) != 0 {
		t.Fatalf("degraded %v, want none — 2006-01-02 is an accepted format (design D2)",
			degradedFields(c))
	}
	if c.DueAt == nil {
		t.Fatal("DueAt = nil, want midnight 2026-08-05")
	}

	want := time.Date(2026, 8, 5, 0, 0, 0, 0, testZone)
	if !c.DueAt.Equal(want) {
		t.Errorf("DueAt = %v, want %v — a date-only value is midnight in now's own "+
			"location (design D2), not UTC and not the machine's zone", *c.DueAt, want)
	}
	if gotOffset, wantOffset := offsetOf(*c.DueAt), offsetOf(want); gotOffset != wantOffset {
		t.Errorf("DueAt zone offset = %ds, want %ds", gotOffset, wantOffset)
	}
}

func offsetOf(t time.Time) int {
	_, offset := t.Zone()
	return offset
}

// TestDecode_RFC3339DatesKeepTheirOwnOffset guards the other half of D2's
// rule: an RFC3339 value carries its own zone, and now's location must not
// overwrite it. Only date-only values borrow the location.
func TestDecode_RFC3339DatesKeepTheirOwnOffset(t *testing.T) {
	payload := `{"type":"event","normalized_content":"standup","weight":1.5,
	             "decay_rate":0.02,"event_at":"2026-08-04T09:00:00Z"}`

	c, err := Decode(payload, testNow)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	if c.EventAt == nil {
		t.Fatal("EventAt = nil, want the RFC3339 instant")
	}
	want := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	if !c.EventAt.Equal(want) {
		t.Errorf("EventAt = %v, want %v", *c.EventAt, want)
	}
}

// TestDecode_MalformedDateDegradesAlone: a date neither RFC3339 nor
// 2006-01-02 is ReasonBadFormat — a fourth reason, distinct from wrong-type
// (the JSON type was string, correctly) and from unknown-enum (dates are not
// a vocabulary). design D2 names this as where ReasonBadFormat comes from.
func TestDecode_MalformedDateDegradesAlone(t *testing.T) {
	payload := `{"type":"event","normalized_content":"standup","weight":1.5,
	             "decay_rate":0.02,"due_at":"next tuesday"}`

	c, err := Decode(payload, testNow)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	assertOnlyDegraded(t, c, "due_at", ReasonBadFormat)

	if c.DueAt != nil {
		t.Errorf("DueAt = %v, want nil", *c.DueAt)
	}
	if c.Kind == nil || *c.Kind != KindEvent {
		t.Errorf("Kind = %v, want event — a bad date does not take the type with it", c.Kind)
	}
}

// TestDecode_TruncatedPayload is I14 shape 1 (spec R1.2) at the Decode level:
// Salvage returns the members completed before the cut, Decode decodes every
// one of them, and the required fields that never arrived are ReasonTruncated
// rather than ReasonAbsent — brain writes a different rationale for "the
// model did not say" than for "the stream was cut" (I12).
func TestDecode_TruncatedPayload(t *testing.T) {
	// Cut partway through decay_rate's value.
	payload := `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_ra`

	c, err := Decode(payload, testNow)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil — three fields completed before the cut", err)
	}

	if c.Kind == nil || *c.Kind != KindTask {
		t.Errorf("Kind = %v, want task — it completed before the cut", c.Kind)
	}
	if c.NormalizedContent == nil || *c.NormalizedContent != "buy milk" {
		t.Errorf("NormalizedContent = %v, want %q", c.NormalizedContent, "buy milk")
	}
	if c.Weight == nil || *c.Weight != 1.5 {
		t.Errorf("Weight = %v, want 1.5", c.Weight)
	}

	assertOnlyDegraded(t, c, "decay_rate", ReasonTruncated)
}

// TestDecode_MissingRequiredFieldIsAbsent: in a payload that closed cleanly,
// a required field that never arrived is ReasonAbsent. The nine optional
// fields are not reported — their absence is not a loss, it is the ordinary
// case (testdata/classify/format.md:51-61 marks exactly four required).
func TestDecode_MissingRequiredFieldIsAbsent(t *testing.T) {
	payload := `{"type":"chitchat","normalized_content":"hello"}`

	c, err := Decode(payload, testNow)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	got := map[string]Reason{}
	for _, d := range c.Degradations {
		got[d.Field] = d.Reason
	}
	want := map[string]Reason{
		"weight":     ReasonAbsent,
		"decay_rate": ReasonAbsent,
	}
	if len(got) != len(want) {
		t.Fatalf("degraded %v, want exactly %v — the nine optional fields' absence is "+
			"not a degradation", got, want)
	}
	for field, reason := range want {
		if got[field] != reason {
			t.Errorf("%s degraded with %q, want %q", field, got[field], reason)
		}
	}
}

// TestDecode_FieldSpecsCoverEveryWireField asserts the decoder's own table is
// complete (design D11 point 4): every field of Classification has a
// fieldSpec row, and every row names a distinct wire field. A field added to
// Classification without a row would silently never decode.
func TestDecode_FieldSpecsCoverEveryWireField(t *testing.T) {
	want := []string{
		"type", "normalized_content", "structured_data", "weight", "decay_rate",
		"event_at", "due_at", "nudge_outcome", "relation_outcome", "state_outcome",
		"task_checkin_outcome", "list_op", "person_ref_status",
	}

	specs := fieldSpecs()
	if len(specs) != len(want) {
		t.Fatalf("fieldSpecs() has %d rows, want %d", len(specs), len(want))
	}

	byName := make(map[string]bool, len(specs))
	for _, s := range specs {
		if byName[s.name] {
			t.Errorf("fieldSpecs() names %q twice", s.name)
		}
		byName[s.name] = true
	}
	for _, name := range want {
		if !byName[name] {
			t.Errorf("fieldSpecs() has no row for %q", name)
		}
	}

	// Exactly four are required — format.md:51-61's own "yes" column.
	required := 0
	for _, s := range specs {
		if s.required {
			required++
		}
	}
	if required != 4 {
		t.Errorf("fieldSpecs() marks %d fields required, want 4 (type, normalized_content, "+
			"weight, decay_rate — testdata/classify/format.md:51-55)", required)
	}
}
