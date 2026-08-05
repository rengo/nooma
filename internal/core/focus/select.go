package focus

import "github.com/rengo/nooma/internal/core/unit"

// Kind is the criterion selecting which contest a unit is in (spec R4.1,
// design D7): the task focus and the load focus are two Select calls with
// a different Kind, not two schemas or two data shapes. Values are
// deliberately "task" and "load", never "focus" (spec R4.2 — no file under
// this package contains the double-quoted literal "focus").
type Kind string

const (
	KindTask Kind = "task"
	KindLoad Kind = "load"
)

// DefaultSize is doc 02 §13's focus_size default (spec R4.1, design D7): a
// human attention bound, 7±2 — the least-invented number available for
// one — shared by both focuses. One constant rather than two follows
// recall.FuseScored's WeightVector/WeightLexical precedent: split it when
// data says the two focuses want different sizes, not before.
const DefaultSize = 7

// Selection is one focus's current membership (spec R4.1, R4.2, design
// D7): Members is unit ids, in rank order — never units themselves, since
// a []unit.Unit would be a persistable shape and would put I01 one
// careless repository call away.
type Selection struct {
	Kind    Kind
	Members []string
}

// AllKinds returns a fresh slice holding the Kind vocabulary's members, in
// declaration order.
//
// STUB (RED commit, design D11): returns the zero value unconditionally.
// The implementation lands in the paired GREEN commit.
func AllKinds() []Kind {
	return nil
}

// Types returns the unit.Type vocabulary Kind's focus selects over (spec
// R4.1, design D7): KindLoad is exactly {mental_load}; KindTask is exactly
// {task, event} — a fresh slice each call, never an exported var.
//
// STUB (RED commit, design D11): returns the zero value unconditionally.
// The implementation lands in the paired GREEN commit.
func Types(k Kind) []unit.Type {
	return nil
}

// Select computes doc 02 §3's focus for Kind k (spec R4.1, R4.3-R4.8,
// design D7, D8): filter ranked to the units k's Types already selected,
// apply anti-jitter hysteresis against previous's members, and take the
// top size.
//
// STUB (RED commit, design D11): returns the zero value unconditionally.
// The implementation lands in the paired GREEN commit.
func Select(k Kind, ranked []Ranked, previous Selection, margin float64, size int) Selection {
	return Selection{}
}
