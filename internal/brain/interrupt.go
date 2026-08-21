package brain

import "github.com/rengo/nooma/internal/core/prospection"

// interruptColumn is doc 02 §7's NULL <-> degraded contract, in one place:
// a degraded resolution persists as SQL NULL, and any other resolution
// persists its level.
//
// It is not a method on prospection.Interrupt on purpose. "Absent means no
// claim was made" is a persistence decision about a nullable column, and
// core is deliberately unaware that the column exists — m3a left the
// contract stated and m3b implements it. Putting it here also keeps
// internal/ports free of prospection.Interrupt: ports.Trigger takes the
// already-converted *float64, so the port never imports the vocabulary.
//
// The inverse is prospection.ResolveInterrupt, and the two compose to the
// identity over everything ResolveInterrupt can return — asserted in
// interrupt_test.go, with no SQLite anywhere near it.
func interruptColumn(i prospection.Interrupt) *float64 {
	if i.Degraded() {
		return nil
	}
	level := i.Level()
	return &level
}
