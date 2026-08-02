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
// This is the first slice of PR 10b's pipeline (design D4): only the
// ordinary path — a classification that persists a unit — is implemented
// yet, so this type carries only what that path has to report. The later
// slices of this same PR add to it rather than replace it: Embedded bool
// (a captured unit may exist without an embedding — design D8, task 10b.8)
// and Stored bool plus a Deferred value (Q3a's refusal path — task's own
// design table assigns that to PR 10c) both land as new fields once their
// own pipeline steps exist. Adding a field nothing sets yet would be a
// promise this slice cannot keep.
type CaptureResult struct {
	// UnitID is the ID of the unit this capture persisted.
	UnitID string
}
