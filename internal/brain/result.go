package brain

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

// CaptureResult is what CaptureService.Capture returns on success.
//
// This is PR 10b's third slice (design D4): the ordinary path — a
// classification that persists a unit, is embedded, then runs hybrid
// recall for dedup/relation candidates — is implemented, so this type now
// carries all three. A fourth slice of this same PR adds to it rather than
// replaces it: Stored bool plus a Deferred value (Q3a's refusal path —
// task's own design table assigns that to PR 10c) lands as a new field
// once that pipeline step exists. Adding a field nothing sets yet would be
// a promise this slice cannot keep.
type CaptureResult struct {
	// UnitID is the ID of the unit this capture persisted.
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
}
