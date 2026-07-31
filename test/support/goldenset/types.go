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

	// Prompt is the sole replay key today. It is fragile: classify's real
	// prompt is built from active self-beliefs plus the local date and
	// timezone (docs/02-cognitive-core.md §5), so literal prompt equality
	// will break on any fixture drift or clock difference. Flagged here
	// for whoever builds the fake provider (four-lens pre-PR review,
	// SUGGESTION finding 9) — not solved by this change.
	Prompt string `json:"prompt"`

	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Validate implements Validator: id/provider/model/task/prompt are
// required, and exactly one of Response or Error must be set.
func (e *LLMExample) Validate() error {
	for _, f := range [...]struct{ name, value string }{
		{"id", e.ID},
		{"provider", e.Provider},
		{"model", e.Model},
		{"task", e.Task},
		{"prompt", e.Prompt},
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
