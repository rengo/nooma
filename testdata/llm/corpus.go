// Package llm embeds testdata/llm/cases' recorded provider examples so
// `nooma doctor`'s structured-JSON quality gate (ADR-0002, spec R5.1, R5.3)
// can build the same live prompt production sends, from the same message
// (and, for relation_evaluation, the same candidates) the test golden
// files already read from disk — "written once, used in two places"
// (ADR-0002's own wording), not a second, hand-copied corpus that could
// drift from this one. See the project/quality-gate-sends-stub-prompts
// Engram observation and openspec/changes/m1c-surface/tasks.md's Conflicts
// §C24 for why this package no longer carries a `prompt` field: the gate
// used to send that field's own short, fake-replay text live, and a real
// provider answered every one of it in prose.
//
// This file lives inside testdata/ on purpose, not beside it: //go:embed
// forbids a ".." path element (a source file can only embed files at or
// below its own directory), so the one tree that can embed cases/*.json
// without duplicating it is this one. The cost, named rather than
// discovered later: both `go vet ./...` and golangci-lint's default
// exclude-dirs list skip directories named "testdata" as scan TARGETS, so a
// bug written directly in this file would still compile — cmd/nooma
// imports it, which forces the compiler to type-check it — but would not
// be flagged by `make lint`'s own analyzers the way every other production
// file is. Mitigated two ways: this file stays to one function with no
// branch beyond json.Unmarshal's own error path, and
// TestCasesMatchesTheSourceFiles (cmd/nooma/doctor_test.go) proves every
// embedded entry equals its source file's own id/task/message/candidates
// byte for byte, so a real bug here still fails a test `make check` does
// run. See tasks.md's Conflicts (link 16a-i, and its later correction
// §C24) for the full finding.
package llm

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed cases/*.json
var casesFS embed.FS

// Case is the one slice of a testdata/llm/ recorded example the quality
// gate needs at runtime: which pipeline call it feeds, and the raw
// message (plus, for relation_evaluation, the candidates) its real prompt
// builder — classify.BuildPrompt or brain.JudgePrompt — renders into the
// prompt actually sent. Response and Error, format.md's other two required
// fields, are deliberately absent from this type — a struct that cannot
// hold them is spec R5.3's MUST NOT ("the gate compare the live response
// against the corpus case's own response field") made structural, the same
// shape link 15's EnvVarName already gave "the writer is incapable of
// receiving a secret" (cmd/nooma/init.go).
type Case struct {
	ID         string      `json:"id"`
	Task       string      `json:"task"`
	Message    string      `json:"message"`
	Candidates []Candidate `json:"candidates,omitempty"`
}

// Candidate is one relation_evaluation candidate a Case carries — the same
// {id, content} pair goldenset.LLMCandidate holds on disk.
type Candidate struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// Cases returns every embedded testdata/llm/cases/ example's
// id/task/message/candidates, in the deterministic order fs.ReadDir
// already guarantees (sorted by filename, and every case's filename is
// its own id). json.Unmarshal ignores the response/error/provider/model
// fields those files also carry — Case has no field to assign them into.
func Cases() ([]Case, error) {
	entries, err := casesFS.ReadDir("cases")
	if err != nil {
		return nil, fmt.Errorf("llm: reading embedded cases: %w", err)
	}
	cases := make([]Case, 0, len(entries))
	for _, entry := range entries {
		data, err := casesFS.ReadFile("cases/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("llm: reading embedded case %q: %w", entry.Name(), err)
		}
		var c Case
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("llm: decoding embedded case %q: %w", entry.Name(), err)
		}
		cases = append(cases, c)
	}
	return cases, nil
}
