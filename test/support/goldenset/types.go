// Package goldenset holds stdlib-only Go types and a loader for the
// golden-set fixture formats documented in
// testdata/{recall,classify,llm}/format.md (spec R10.1-R10.4, design.md
// §10, docs/06-harness.md §5).
//
// This package deliberately imports nothing beyond the standard library
// (design §3), mirroring test/support/schema: M1's real implementation can
// depend on it later without internal/core ever seeing test-only code.
package goldenset

import "encoding/json"

// RecallExample is one case of testdata/recall/'s golden set (ADR-0010): a
// small, self-contained corpus of units plus one or more queries and each
// query's expected fused result order. See testdata/recall/format.md for
// the full field-by-field contract.
type RecallExample struct {
	ID      string        `json:"id"`
	Units   []RecallUnit  `json:"units"`
	Queries []RecallQuery `json:"queries"`
}

// RecallUnit is a minimal unit projection — only the fields hybrid recall
// reasons about (docs/03-data-model.md's units.id/type/content), not the
// full units table.
type RecallUnit struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

// RecallQuery is one query against a RecallExample's Units and its
// expected fused order (ADR-0010's RRF result), as a list of unit IDs,
// most relevant first.
type RecallQuery struct {
	Query           string   `json:"query"`
	ExpectedUnitIDs []string `json:"expected_unit_ids"`
}

// ClassifyExample is one case of testdata/classify/'s golden set
// (ADR-0002, docs/02-cognitive-core.md §5): an input message and the
// classification output it must produce. See testdata/classify/format.md
// for the full field-by-field contract, including why the eventual corpus
// must contain deliberately malformed cases (I14, docs/06-harness.md §5).
type ClassifyExample struct {
	ID       string           `json:"id"`
	Input    string           `json:"input"`
	Expected ClassifyExpected `json:"expected"`
}

// ClassifyExpected is the structured output docs/02-cognitive-core.md §5's
// classify step must produce for a ClassifyExample.Input: the taxonomy
// type, the normalized content, optional structured data, the initial
// weight/decay-rate pair, and the orthogonal resolution fields. Only
// Type, NormalizedContent, Weight and DecayRate are required — the six
// resolution fields are optional and independent of one another (§5's
// "orthogonal fields, not types").
type ClassifyExpected struct {
	Type               string          `json:"type"`
	NormalizedContent  string          `json:"normalized_content"`
	StructuredData     json.RawMessage `json:"structured_data,omitempty"`
	Weight             float64         `json:"weight"`
	DecayRate          float64         `json:"decay_rate"`
	NudgeOutcome       string          `json:"nudge_outcome,omitempty"`
	RelationOutcome    string          `json:"relation_outcome,omitempty"`
	StateOutcome       string          `json:"state_outcome,omitempty"`
	TaskCheckinOutcome string          `json:"task_checkin_outcome,omitempty"`
	ListOp             string          `json:"list_op,omitempty"`
	PersonRefStatus    string          `json:"person_ref_status,omitempty"`
}

// LLMExample is one recorded provider response (docs/06-harness.md §5's
// testdata/llm/): a fixed provider/model/task/prompt and the response
// recorded once, replayed by a fake provider so a test never touches the
// network or a real LLM (CLAUDE.md non-negotiable #5).
type LLMExample struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Task     string `json:"task"`
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}
