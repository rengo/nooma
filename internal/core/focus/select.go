package focus

import (
	"sort"

	"github.com/rengo/nooma/internal/core/unit"
)

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
// declaration order — the same contract unit.AllTypes and unit.AllStatuses
// already keep for their own vocabularies.
func AllKinds() []Kind {
	return []Kind{KindTask, KindLoad}
}

// Types returns the unit.Type vocabulary Kind's focus selects over (spec
// R4.1, design D7): KindLoad is exactly {mental_load} — doc 02 §3 says so
// outright; KindTask is exactly {task, event} — this package's own scoping
// choice and an owner-review item (design D7's rejected-alternatives
// table): the task focus answers "what should I be doing", and a meeting
// in two hours is the strongest possible answer to that question, so
// excluding event would force a third focus for it on day one, while a
// list is a container and putting one in a focus of atoms means the focus
// sometimes holds a thing that cannot itself be done.
// knowledge, procedural, emotional, structured_ref, insight and list are
// in neither focus.
//
// Returns a fresh slice on every call — never an exported var — so a
// caller mutating the result cannot corrupt the next call's answer (R4.1's
// own MUST, and R4.2's package-level-var prohibition).
func Types(k Kind) []unit.Type {
	switch k {
	case KindLoad:
		return []unit.Type{unit.TypeMentalLoad}
	case KindTask:
		return []unit.Type{unit.TypeTask, unit.TypeEvent}
	default:
		return nil
	}
}

// Select computes doc 02 §3's focus for Kind k (spec R4.1, R4.3-R4.8,
// design D7, D8): filter ranked to the units k's Types already selected,
// then take the top size by an adjusted sort that implements anti-jitter
// hysteresis without a swap loop — Score*(1+margin) for an incumbent
// (present in previous.Members), Score for everyone else — proven
// equivalent to the Displaces predicate over a boundary table
// (hysteresis.go's own doc comment; select_test.go's
// TestSelect_AgreesWithDisplaces, spec R4.8). The equivalence holds
// because an incumbent i holds its slot against a non-incumbent c iff
// c.Score <= i.Score*(1+margin), which is exactly !Displaces(c, i,
// margin); incumbent-vs-incumbent comparisons scale both sides by the same
// factor, so their relative order is untouched; non-incumbent-vs-non-
// incumbent is untouched.
//
// An empty previous.Members (R4.5, the first computation after a process
// start) needs no special case: with no incumbents, every candidate's
// adjusted key is its own Score, and the sort degenerates to a plain
// top-size by Score, exactly R4.5's MUST. An incumbent id absent from
// ranked (archived since, or now the wrong type for k) is simply never
// looked up — it contributes nothing and blocks nobody (R4.5's second
// half).
//
// Each candidate's raw Score is put through scoreKey (rank.go) before the
// margin multiplier, the identical NaN-to-negative-infinity remap
// Displaces itself applies (hysteresis.go's own doc comment) — a
// Rank-produced Score can be NaN or +Inf, and this keeps the adjusted-sort
// spelling and the Displaces spelling equal on that boundary too, not only
// on R4.3's finite one (select_test.go's TestSelect_AgreesWithDisplaces).
// Without it, sort.Slice's comparator would be inconsistent whenever a NaN
// adjusted key is present — the same undefined-behaviour risk rank.go's
// own scoreKey already exists to close for Rank's comparator.
//
// margin is a parameter, never resolved here: ResolveMargin's nil-vs-value
// distinction is the caller's concern (R4.4); Select only ever sees the
// resolved float64.
func Select(k Kind, ranked []Ranked, previous Selection, margin float64, size int) Selection {
	incumbent := make(map[string]bool, len(previous.Members))
	for _, id := range previous.Members {
		incumbent[id] = true
	}

	wantType := make(map[unit.Type]bool)
	for _, t := range Types(k) {
		wantType[t] = true
	}

	eligible := make([]Ranked, 0, len(ranked))
	for _, r := range ranked {
		if wantType[r.Candidate.Type] {
			eligible = append(eligible, r)
		}
	}

	adjustedKey := func(r Ranked) float64 {
		key := scoreKey(r.Score)
		if incumbent[r.Candidate.ID] {
			return key * (1 + margin)
		}
		return key
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		return adjustedKey(eligible[i]) > adjustedKey(eligible[j])
	})

	n := size
	if n > len(eligible) {
		n = len(eligible)
	}
	if n < 0 {
		n = 0
	}

	members := make([]string, n)
	for i := 0; i < n; i++ {
		members[i] = eligible[i].Candidate.ID
	}

	return Selection{Kind: k, Members: members}
}
