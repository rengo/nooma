package classify

// The base priors — design D3, doc 02 §13's calibration table.
//
// units.weight and units.weight_decay_rate are NOT NULL (migration 0001),
// so a classification whose weight or λ degraded cannot be persisted as
// null. Something has to supply a value, and these are it.
//
// There are exactly two numbers here, not eighteen. Doc 02 §13 names the
// knob as "λ per type (weight_decay_rate) — prior per type, base 0.01/day"
// and enumerates no per-type table anywhere; doc 02 §2 says type "orients
// the direction" while the self-model "personalizes the value", and both of
// those are the model's job, performed through the prompt. Inventing nine
// per-type constants in Go would be inventing calibration doc 02 never
// stated, in the one place doc 02 says the model decides.
//
// Their values are migration 0001's own column DEFAULTs, and
// TestClassifyPriorsMatchMigrationDefaults (L2) reads the migration off disk
// to keep them one number rather than two that drift.
const (
	// PriorWeight fills a degraded weight — units.weight DEFAULT 1.0.
	PriorWeight = 1.0
	// PriorDecayRate fills a degraded λ — units.weight_decay_rate
	// DEFAULT 0.01, doc 02 §13's "base 0.01/day".
	PriorDecayRate = 0.01
)
