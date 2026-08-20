package prospection

// PushThreshold is doc 02 §7's own comparison: interrupt_level >= 0.70 is
// a push (spec R3.2). Declared alongside DefaultInterruptLevel because
// TestDefaultInterruptLevel_BelowPushThreshold below needs both named
// constants to exist together, ahead of Route (task 3.3).
const PushThreshold = 0.70

// DefaultInterruptLevel fills a degraded or unreadable interrupt_level.
const DefaultInterruptLevel = 0.0

// Interrupt is a resolved interrupt level. See ResolveInterrupt.
type Interrupt struct {
	level     float64
	confirmed bool
}

// ResolveInterrupt is not implemented yet.
func ResolveInterrupt(level *float64) Interrupt {
	return Interrupt{}
}

// Level returns the resolved interrupt level.
func (i Interrupt) Level() float64 { return i.level }

// Degraded is not implemented yet.
func (i Interrupt) Degraded() bool { return i.confirmed }
