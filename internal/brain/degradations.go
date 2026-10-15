package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
)

// degradationEntry is one lost field as a decision_log context carries it
// (doc 02 §5.1, I12): the wire name of the field and why it did not survive.
type degradationEntry struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// degradationEntries lists what classify.Decode lost, in its order. It is nil
// for a clean classification.
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

// degradedLog is the decision log of one reading that lost something: every
// row it records carries the `degradations` list in its context and names it
// in its rationale (doc 02 §5.1, I12). The capture flow writes rows from more
// than twenty places after Decode; a log that stamps them is the one place
// that cannot forget a row, which is the failure the per-row version had.
//
// A clean reading never builds one (logWithDegradations returns the log it was
// given), so a row without the key means nothing was lost.
type degradedLog struct {
	ports.DecisionLog
	entries []degradationEntry
}

// logWithDegradations wraps log for a reading that lost entries, and returns
// log itself when nothing was lost.
func logWithDegradations(log ports.DecisionLog, entries []degradationEntry) ports.DecisionLog {
	if len(entries) == 0 {
		return log
	}
	return degradedLog{DecisionLog: log, entries: entries}
}

// Record stamps d and records it. A context that is not a JSON object cannot
// take a key, and is recorded as it came: the rationale still names the loss.
func (l degradedLog) Record(ctx context.Context, d ports.Decision) error {
	parts := make([]string, len(l.entries))
	for i, e := range l.entries {
		parts[i] = fmt.Sprintf("%s: %s", e.Field, e.Reason)
	}
	d.Rationale += "; the classification degraded " + strings.Join(parts, ", ")

	var fields map[string]json.RawMessage
	if len(d.Context) > 0 && json.Unmarshal(d.Context, &fields) == nil && fields != nil {
		list, err := json.Marshal(l.entries)
		if err != nil {
			return fmt.Errorf("encode degradations: %w", err)
		}
		fields["degradations"] = list
		if d.Context, err = json.Marshal(fields); err != nil {
			return fmt.Errorf("encode %s context: %w", d.Action, err)
		}
	}
	return l.DecisionLog.Record(ctx, d)
}

// withDegradedLog returns r with every decision it writes from here on
// stamped with what c lost. at calls it right after Decode, on its own copy, so
// the shared runner is never touched and the wrapped log reaches the correction
// runner and the reminder writers built from r.log.
func (r captureRunner) withDegradedLog(c classify.Classification) captureRunner {
	log := logWithDegradations(r.log, degradationEntries(c))
	r.log = log
	r.correction.log = log
	return r
}
