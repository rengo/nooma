package relation

// Confirmable reports whether a stored relation is one a confirm would
// raise: its confidence is below its type's Surface threshold (doc 02 §4).
// At or above it the relation is already asserted, as Decide puts the
// boundary, and asking the user to confirm it would read as a task. A
// relation stored before its type's thresholds rose can sit below Persist
// too; it is still below Surface, so it is confirmable. Written as
// !(confidence >= Surface) so a NaN, which ConfirmedConfidence lands on
// the floor, is offered the confirm that repairs it.
func Confirmable(confidence float64, t Thresholds) bool {
	return !(confidence >= t.Surface)
}
