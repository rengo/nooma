package brain

import (
	"fmt"
	"strings"

	"github.com/rengo/nooma/internal/core/classify"
)

// degradationEntry is one lost field as a decision_log context carries it
// (doc 02 §5.1, I12): the wire name of the field and why it did not survive.
type degradationEntry struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// degradationEntries lists what classify.Decode lost, in its order. It is nil
// for a clean classification, so a context built with `omitempty` carries no
// key at all and a row without degradations stays byte-identical to what it
// was before they were recorded.
func degradationEntries(c classify.Classification) []degradationEntry {
	if len(c.Degradations) == 0 {
		return nil
	}
	entries := make([]degradationEntry, len(c.Degradations))
	for i, d := range c.Degradations {
		entries[i] = degradationEntry{Field: d.Field, Reason: string(d.Reason)}
	}
	return entries
}

// withDegradations appends the degradations to a rationale, naming each field
// and reason. A clean classification returns the rationale unchanged.
func withDegradations(rationale string, entries []degradationEntry) string {
	if len(entries) == 0 {
		return rationale
	}
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf("%s: %s", e.Field, e.Reason)
	}
	return rationale + "; the classification degraded " + strings.Join(parts, ", ")
}
