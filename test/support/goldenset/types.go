// Package goldenset holds stdlib-only Go types and a loader for the
// golden-set fixture formats documented in
// testdata/{recall,classify,llm}/format.md (spec R10.1-R10.4, design.md
// §10, docs/06-harness.md §5).
//
// This package deliberately imports nothing beyond the standard library
// (design §3), mirroring test/support/schema: M1's real implementation can
// depend on it later without internal/core ever seeing test-only code.
package goldenset

import (
	"encoding/json"
	"fmt"
)

// Validator is implemented by every golden-set example type so DecodeStrict
// can enforce "Required: yes" fields the same way it already enforces
// DisallowUnknownFields — one shared gate for every caller (a real case
// file via Load, or a format.md's fenced example via
// TestHarness_GoldenSetFormatMatchesType), instead of a check some callers
// apply and others silently skip (four-lens pre-PR review, WARNING finding
// 4, CRITICAL finding 2).
type Validator interface {
	Validate() error
}

// RecallExample is one case of testdata/recall/'s golden set (ADR-0010): a
// small, self-contained corpus of units plus one or more queries and each
// query's expected fused result order. See testdata/recall/format.md for
// the full field-by-field contract.
type RecallExample struct {
	ID      string        `json:"id"`
	Units   []RecallUnit  `json:"units"`
	Queries []RecallQuery `json:"queries"`
}

// Validate implements Validator: id and at least one unit and one query are
// required (spec R10.2's "Required: yes" columns), every unit/query is
// itself validated, and the vector cross-field rule (design §4.2) holds:
// once any unit in this case carries a Vector, every unit and every query
// must carry one too, and every vector must share one length.
func (e *RecallExample) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("id is required and must not be empty")
	}
	if len(e.Units) == 0 {
		return fmt.Errorf("units is required and must have at least 1 entry")
	}
	for i, u := range e.Units {
		if err := u.Validate(); err != nil {
			return fmt.Errorf("units[%d]: %w", i, err)
		}
	}
	if len(e.Queries) == 0 {
		return fmt.Errorf("queries is required and must have at least 1 entry")
	}
	for i, q := range e.Queries {
		if err := q.Validate(); err != nil {
			return fmt.Errorf("queries[%d]: %w", i, err)
		}
	}
	return e.validateVectors()
}

// validateVectors implements design §4.2's mechanizable cross-field check:
// vectors are optional per case (a case with no ranking disagreement to
// author needs none), but once any unit carries one, the check switches on
// and every unit and every query must carry one too, all sharing one
// dimension — the shape `internal/core/recall.Search` (PR 8a) needs to run
// for real over this case, rather than a mismatch surfacing later as a
// dimension error deep inside Search.
func (e *RecallExample) validateVectors() error {
	dim := -1
	for _, u := range e.Units {
		if len(u.Vector) > 0 {
			dim = len(u.Vector)
			break
		}
	}
	if dim == -1 {
		return nil
	}

	for i, u := range e.Units {
		if len(u.Vector) == 0 {
			return fmt.Errorf("units[%d]: vector is required once any unit in this case carries one", i)
		}
		if len(u.Vector) != dim {
			return fmt.Errorf("units[%d]: vector has length %d, want %d (every vector in a case must share one length)", i, len(u.Vector), dim)
		}
	}
	for i, q := range e.Queries {
		if len(q.Vector) == 0 {
			return fmt.Errorf("queries[%d]: vector is required once any unit in this case carries one", i)
		}
		if len(q.Vector) != dim {
			return fmt.Errorf("queries[%d]: vector has length %d, want %d (every vector in a case must share one length)", i, len(q.Vector), dim)
		}
	}
	return nil
}

// RecallUnit is a minimal unit projection — only the fields hybrid recall
// reasons about (docs/03-data-model.md's units.id/type/content/status), not
// the full units table.
//
// Status carries docs/03-data-model.md's units.status domain
// (pool|archived|superseded|incomplete) so a case can express the one thing
// I02 (docs/02-cognitive-core.md §1) exists to prevent: a unit that matches
// a query semantically but must never appear in a query's
// expected_unit_ids because it is superseded or incomplete. format.md's own
// example carries such a unit (four-lens pre-PR review, WARNING finding 5).
//
// Vector is optional and, when present, stated explicitly by the case
// author rather than computed by testdata/llm's fake embedder — whose
// hash-based vectors cannot author a genuine lexical/vector disagreement
// (design §4.2). See RecallExample.validateVectors for the cross-field rule
// this field participates in.
type RecallUnit struct {
	ID      string    `json:"id"`
	Type    string    `json:"type"`
	Content string    `json:"content"`
	Status  string    `json:"status"`
	Vector  []float32 `json:"vector,omitempty"`
}

// Validate implements the per-unit half of RecallExample.Validate.
func (u RecallUnit) Validate() error {
	if u.ID == "" {
		return fmt.Errorf("id is required and must not be empty")
	}
	if u.Type == "" {
		return fmt.Errorf("type is required and must not be empty")
	}
	if u.Content == "" {
		return fmt.Errorf("content is required and must not be empty")
	}
	if u.Status == "" {
		return fmt.Errorf("status is required and must not be empty")
	}
	return nil
}

// RecallQuery is one query against a RecallExample's Units and its
// expected fused order (ADR-0010's RRF result), as a list of unit IDs,
// most relevant first.
//
// Vector and LexicalRanking are both optional, stated explicitly by the
// case author for the same reason RecallUnit.Vector is (design §4.2):
// Vector is this query's embedding, feeding internal/core/recall.Search
// (PR 8a) directly; LexicalRanking is the ranking the real FTS5 leg is
// expected to produce for this query over Units, best match first — a
// ground truth PR 9c's L3 test later confirms against the real leg, not a
// value Load or Validate cross-checks against Units' content.
type RecallQuery struct {
	Query           string    `json:"query"`
	Vector          []float32 `json:"vector,omitempty"`
	LexicalRanking  []string  `json:"lexical_ranking,omitempty"`
	ExpectedUnitIDs []string  `json:"expected_unit_ids"`
}

// Validate implements the per-query half of RecallExample.Validate.
func (q RecallQuery) Validate() error {
	if q.Query == "" {
		return fmt.Errorf("query is required and must not be empty")
	}
	if len(q.ExpectedUnitIDs) == 0 {
		return fmt.Errorf("expected_unit_ids is required and must have at least 1 entry")
	}
	return nil
}

// ClassifyExample is one case of testdata/classify/'s golden set
// (ADR-0002, docs/02-cognitive-core.md §5): an input message and the
// classification output it must produce. See testdata/classify/format.md
// for the full field-by-field contract, including why the eventual corpus
// must contain deliberately malformed cases (I14, docs/06-harness.md §5).
//
// LLMCaseID, when set, names the id of a testdata/llm/ case that recorded
// the malformed provider response this case's Expected degrades from — the
// structural link I14 needs between the two corpora (four-lens pre-PR
// review, WARNING finding 6). It is optional: most classify cases exercise
// the taxonomy directly and need no backing recording.
type ClassifyExample struct {
	ID        string           `json:"id"`
	Input     string           `json:"input"`
	Expected  ClassifyExpected `json:"expected"`
	LLMCaseID string           `json:"llm_case_id,omitempty"`
}

// Validate implements Validator: id, input, and Expected are required.
func (e *ClassifyExample) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("id is required and must not be empty")
	}
	if e.Input == "" {
		return fmt.Errorf("input is required and must not be empty")
	}
	return e.Expected.Validate()
}

// ClassifyExpected is the structured output docs/02-cognitive-core.md §5's
// classify step must produce for a ClassifyExample.Input: the taxonomy
// type, the normalized content, optional structured data, the initial
// weight/decay-rate pair, and the orthogonal resolution fields. Only
// Type, NormalizedContent, Weight and DecayRate are required — the six
// resolution fields are optional and independent of one another (§5's
// "orthogonal fields, not types").
//
// Weight and DecayRate are pointers, not float64, specifically so an
// absent or explicit `null` value can be told apart from a legitimately
// present zero (four-lens pre-PR review, WARNING finding 4): with a plain
// float64, `"weight": null` and a missing "weight" key both silently
// decode to 0.0, indistinguishable from a case that genuinely means
// weight zero. A nil pointer means "not provided" in both of those input
// shapes, which Validate below rejects; a non-nil pointer to 0.0 means the
// author explicitly wrote a legitimate zero, which Validate accepts.
//
// EventAt and DueAt are top-level and separately named, not members of
// StructuredData, because I18 forbids the three timestamps from ever being
// interchanged and both of these are governed NOT NULL-adjacent columns.
// Keeping them inside the opaque payload would mean the brain reaching into
// an explicitly unschema'd blob to extract a governed column — format.md
// says structured_data's "shape varies by expected.type and is not fixed by
// a single schema in doc 02" — which is the two-sources-of-truth defect this
// project keeps naming.
//
// They are *string, the recorded wire text, rather than *time.Time: this
// type records what the provider said, and a case whose date is malformed on
// purpose (I14's bad-format shape) must be expressible here. Parsing is
// classify.Decode's job, and its failure is the thing under test.
//
// docs/03-data-model.md persists DecayRate as the `weight_decay_rate`
// column; docs/02-cognitive-core.md's decay formula (§2, line 29) uses the
// short name `decay_rate`, which is what this field and its JSON tag
// follow. See testdata/classify/format.md's field table for the mapping.
type ClassifyExpected struct {
	Type               string          `json:"type"`
	NormalizedContent  string          `json:"normalized_content"`
	StructuredData     json.RawMessage `json:"structured_data,omitempty"`
	Weight             *float64        `json:"weight"`
	DecayRate          *float64        `json:"decay_rate"`
	EventAt            *string         `json:"event_at,omitempty"`
	DueAt              *string         `json:"due_at,omitempty"`
	NudgeOutcome       string          `json:"nudge_outcome,omitempty"`
	RelationOutcome    string          `json:"relation_outcome,omitempty"`
	StateOutcome       string          `json:"state_outcome,omitempty"`
	TaskCheckinOutcome string          `json:"task_checkin_outcome,omitempty"`
	ListOp             string          `json:"list_op,omitempty"`
	PersonRefStatus    string          `json:"person_ref_status,omitempty"`
}

// Validate implements the Expected half of ClassifyExample.Validate.
func (e ClassifyExpected) Validate() error {
	if e.Type == "" {
		return fmt.Errorf("expected.type is required and must not be empty")
	}
	if e.NormalizedContent == "" {
		return fmt.Errorf("expected.normalized_content is required and must not be empty")
	}
	if e.Weight == nil {
		return fmt.Errorf("expected.weight is required (present and non-null), got neither")
	}
	if e.DecayRate == nil {
		return fmt.Errorf("expected.decay_rate is required (present and non-null), got neither")
	}
	return nil
}

// LLMExample is one recorded provider response (docs/06-harness.md §5's
// testdata/llm/): a fixed provider/model/task/prompt and either the
// response recorded once and replayed by a fake provider, or Error
// describing a provider-level failure (timeout, HTTP error, rate limit)
// that same fake provider must surface instead — never a real network call
// or a real LLM (CLAUDE.md non-negotiable #5). Exactly one of Response or
// Error is set (four-lens pre-PR review, SUGGESTION finding 9): a
// success-shaped Response field alone could not express a recorded
// failure, and docs/06-harness.md §3 says providers are always served from
// fixtures, so a failure path can only ever be tested from one of these.
type LLMExample struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Task     string `json:"task"`

	// Message is the raw text the pipeline's own real prompt builder starts
	// from — the user's message for a capture_processing case, the new
	// unit's content for a relation_evaluation case. `nooma doctor`'s
	// quality gate (cmd/nooma/doctor.go) builds the live prompt it actually
	// sends from this field, through classify.BuildPrompt or
	// brain.JudgePrompt — the exact functions production calls — rather
	// than replaying a separately recorded prompt string. That used to be
	// this field's job (a `prompt` field, since removed): a stub 60-84
	// bytes long, nothing like classify's real ~1550-byte prompt, and a
	// real provider answered it in prose every time (Engram
	// project/quality-gate-sends-stub-prompts). One field cannot be both a
	// stable replay key (fakeprovider.Fake selects by case id, never by
	// prompt content — see SeenPrompts below) and a genuine elicitor of the
	// recorded response, so this corpus no longer tries to make it be
	// both: Message only ever feeds the real builder, and only the real
	// builder decides what is actually sent.
	Message string `json:"message"`

	// Candidates is relation_evaluation's own second input:
	// brain.JudgePrompt renders one line per candidate alongside Message.
	// Always empty for a capture_processing case — a documentation
	// convention by task, the same posture format.md already takes for
	// other task-specific fields, not mechanized by Validate below.
	Candidates []LLMCandidate `json:"candidates,omitempty"`

	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

// LLMCandidate is one relation_evaluation recall candidate an LLMExample
// carries — the same {id, content} pair brain.JudgePrompt renders per
// candidate, nothing more: judgePrompt reads only a candidate unit's ID
// and Content, never its status, weight, or any other unit.Unit field.
type LLMCandidate struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// Validate implements Validator: id/provider/model/task/message are
// required, and exactly one of Response or Error must be set.
func (e *LLMExample) Validate() error {
	for _, f := range [...]struct{ name, value string }{
		{"id", e.ID},
		{"provider", e.Provider},
		{"model", e.Model},
		{"task", e.Task},
		{"message", e.Message},
	} {
		if f.value == "" {
			return fmt.Errorf("%s is required and must not be empty", f.name)
		}
	}
	hasResponse := e.Response != ""
	hasError := e.Error != ""
	if !hasResponse && !hasError {
		return fmt.Errorf("exactly one of response or error is required, both are empty")
	}
	if hasResponse && hasError {
		return fmt.Errorf("exactly one of response or error is required, both are set")
	}
	return nil
}

// ConsolidationExample is one case of testdata/consolidation/'s golden set
// (spec R4.1-R4.5, design §8.2): a capture script that builds this case's
// own corpus by driving brain.CaptureService.Capture (design D6), the
// single injected "now" the consolidation pass runs at, an optional
// last_run_at, and the expected archive/connect/derive effects. See
// testdata/consolidation/format.md for the full field-by-field contract.
type ConsolidationExample struct {
	ID            string                 `json:"id"`
	CaptureScript []ConsolidationCapture `json:"capture_script"`
	Now           string                 `json:"now"`
	LastRunAt     *string                `json:"last_run_at,omitempty"`

	// Expected is a pointer, not a plain ConsolidationExpected, specifically
	// so a document missing the "expected" key entirely can be told apart
	// from one that spells it out as an explicit, legitimately empty
	// `"expected": {}` (format.md's own "Checked" section, four-lens pre-PR
	// review JD-8-01) — the same absent-vs-zero-value problem
	// ClassifyExpected.Weight/DecayRate and this struct's own LastRunAt
	// above already solve with a pointer. With a plain struct, both shapes
	// decode to the same Go zero value and Validate below could not reject
	// only the first: a case asserting "nothing should happen" (an
	// explicit, empty `expected: {}`) must stay valid — a scheduler needs
	// to be able to express exactly that — while a case that never mentions
	// `expected` at all is the actual defect this field's absence-check
	// below catches.
	Expected *ConsolidationExpected `json:"expected"`
}

// Validate implements Validator: id, capture_script (at least 1 entry,
// each itself validated), now, and expected (present, possibly empty) are
// required.
func (e *ConsolidationExample) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("id is required and must not be empty")
	}
	if len(e.CaptureScript) == 0 {
		return fmt.Errorf("capture_script is required and must have at least 1 entry")
	}
	for i, c := range e.CaptureScript {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("capture_script[%d]: %w", i, err)
		}
	}
	if e.Now == "" {
		return fmt.Errorf("now is required and must not be empty")
	}
	if e.Expected == nil {
		return fmt.Errorf("expected is required (present, possibly as an explicit empty object {}), got neither")
	}
	return nil
}

// ConsolidationCapture is one capture_script entry: the offset from a
// simulated t0, the raw message text, and the testdata/llm/ case id the
// scripted fake provider replays for this capture's classify call (design
// D7's selection-by-id, never by prompt content).
type ConsolidationCapture struct {
	Offset    string `json:"offset"`
	Text      string `json:"text"`
	LLMCaseID string `json:"llm_case_id"`
}

// Validate implements the per-capture half of ConsolidationExample.Validate.
func (c ConsolidationCapture) Validate() error {
	if c.Offset == "" {
		return fmt.Errorf("offset is required and must not be empty")
	}
	if c.Text == "" {
		return fmt.Errorf("text is required and must not be empty")
	}
	if c.LLMCaseID == "" {
		return fmt.Errorf("llm_case_id is required and must not be empty")
	}
	return nil
}

// ConsolidationExpected is the archive/connect/derive effects a
// ConsolidationExample's pass must produce (spec R4.4/R4.5), each naming
// capture_script indices rather than unit IDs — a captured unit's ID does
// not exist until CaptureService.Capture runs (design D6, format.md's own
// "Why indices, not unit IDs" section). All three fields are optional per
// case, and none is cross-validated against capture_script's own length
// here (format.md's cross-field constraint, documented not mechanized, the
// same posture testdata/recall/format.md's own cross-field section takes).
type ConsolidationExpected struct {
	Archived         []int    `json:"archived,omitempty"`
	RelationsCreated [][2]int `json:"relations_created,omitempty"`
	Beliefs          []int    `json:"beliefs,omitempty"`
}
