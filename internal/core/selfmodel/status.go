package selfmodel

// Status is a belief's lifecycle state — doc 03 self_beliefs.status. It is
// a defined string type for Facet's reasons: it prints as itself in an
// error, a log line and a decision_log row, and binds to the TEXT column
// with no conversion table.
//
// The vocabulary is closed at two members. Retiring a belief is a
// transition, never a removal (doc 02 §10, I03). Derive never undoes a
// retirement; only an explicit user act could reverse it, and no surface
// offers one yet.
type Status string

// The Status vocabulary members, doc 03 self_beliefs.status.
const (
	StatusActive  Status = "active"
	StatusRetired Status = "retired"
)

// AllStatuses returns a fresh slice holding the Status members, in the
// order the constants above declare them (and in the order doc 03's
// column comment lists them — TestBeliefStatusDocMatchesAllStatuses pins
// the two together).
func AllStatuses() []Status {
	return []Status{StatusActive, StatusRetired}
}

// Origin records where a belief came from — doc 03 self_beliefs.origin.
// Same defined-string-type reasoning as Status and Facet.
type Origin string

// The Origin vocabulary members, doc 03 self_beliefs.origin.
const (
	OriginSeed       Origin = "seed"
	OriginDerived    Origin = "derived"
	OriginUserStated Origin = "user_stated"
)

// AllOrigins returns a fresh slice holding the Origin members, in the
// order the constants above declare them.
func AllOrigins() []Origin {
	return []Origin{OriginSeed, OriginDerived, OriginUserStated}
}
