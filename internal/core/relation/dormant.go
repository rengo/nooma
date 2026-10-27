package relation

// Dormant reports whether a stored relation is dormant: its confidence is
// below its type's current persist bar (doc 02 §4, I49). A dormant relation
// is kept, never deleted, and every reader that shows or follows relations
// leaves it out: the graph, a unit's page, focus adjacency, curation and the
// digest's question queue. It wakes when the bar falls back under it, so it
// is decided at read time and never written.
//
// The boundary is Decide's: a relation exactly at the bar would be stored
// today, so it is awake. A NaN confidence is awake, since Confirmable offers
// it the confirm that repairs it.
func Dormant(confidence float64, t Thresholds) bool {
	return confidence < t.Persist
}
