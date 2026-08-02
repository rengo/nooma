package brain

import "github.com/rengo/nooma/internal/core/classify"

// CaptureInput is what a caller hands to CaptureService.Capture — the "in"
// design D4's pipeline diagram threads through every step.
//
// Text and Channel are both the caller's facts, not core's: BuildPrompt
// renders Text verbatim (design D4), and Channel becomes classify.ToUnit's
// source parameter (conflict C10.1) — units.source is NOT NULL and its
// column DEFAULT never fires, because the repository always passes the
// field explicitly. Hardcoding a channel inside core would be silently
// wrong the day a capture arrives from somewhere other than chat; naming it
// here, at the one place that actually knows it, is what keeps that from
// happening.
type CaptureInput struct {
	// Text is the raw message text to classify.
	Text string
	// Channel is where this capture came from (e.g. "chat", "telegram"),
	// and becomes the persisted unit's Source.
	Channel string
}

// CaptureResult is what CaptureService.Capture returns.
//
// PR 10b built the ordinary path — a classification that persists a unit,
// is embedded, then runs hybrid recall for dedup/relation candidates. This
// PR (10c) adds Stored and Deferred: Q3a's refusal path (spec R4.6) needs a
// result distinguishable from an ordinary success, which a bare Go error
// would not give it — a timer classification is not a failure, so Capture
// returns (CaptureResult, nil) for it too, with Stored == false naming the
// difference.
type CaptureResult struct {
	// Stored reports whether this capture persisted a unit at all. False
	// only for Q3a's refusal path (a timer/recurring_reminder
	// classification, spec R4.6): every other classification this PR
	// handles — including an ambiguous person reference, spec R4.7 — sets
	// this true, because a pool unit is still persisted for it. When false,
	// UnitID/Embedded/Candidates are all their zero values: there is no
	// unit for them to describe.
	Stored bool
	// UnitID is the ID of the unit this capture persisted. Empty when
	// Stored is false.
	UnitID string
	// Embedded reports whether this capture's embedding was written.
	// False means the unit is persisted and lexically findable but not yet
	// semantically searchable — design D8's accepted, named gap: a local
	// embedding-provider or store outage degrades the index, it does not
	// refuse the capture (doc 02 §5's product rule). A caller that cares —
	// Phase C's HTTP route, the CLI — can tell "stored and searchable" from
	// "stored, semantic search pending" without querying decision_log
	// itself.
	Embedded bool
	// Candidates holds the ids RecallService found for this capture's own
	// unit, in the RRF-fused, I02-filtered order design D5 produces — the
	// just-persisted unit's own id is never among them (spec R4.4's own
	// MUST). Empty, never nil, when embedding did not happen (Embedded ==
	// false: there is no vector to search with) or when recall found
	// nothing. PR 11c is the first consumer that does anything with this
	// list beyond observing it; today it is a caller-visible fact, not yet
	// a decision.
	Candidates []string
	// Deferred is non-nil exactly when Stored is false — Q3a's refusal
	// path, spec R4.6. Nil for every other classification this PR handles.
	Deferred *Deferred
}

// Deferred is what CaptureResult carries in place of a persisted unit, when
// Q3a's refusal path activates (design D9, spec R4.6). It exists so the
// refusal is representable and distinguishable from an ordinary success —
// Phase C's HTTP route and CLI render it; this PR's job is only that it can
// be told apart, not how it looks on the wire (design.md §8: "the HTTP and
// CLI shapes of CaptureResult.Deferred" are explicitly not decided here).
type Deferred struct {
	// Kind is the classification kind that triggered the refusal —
	// classify.KindTimer or classify.KindRecurringReminder today. A typed
	// value, not a bare string, for the same reason classify.Kind exists at
	// all: the vocabulary is closed and greppable.
	Kind classify.Kind
	// Message is the caller-visible refusal, in plain words — Q3a's own
	// wording: "tells the caller 'not yet' in plain words". Never a
	// technical error message; a caller renders this directly.
	Message string
}
