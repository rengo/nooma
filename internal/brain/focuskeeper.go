package brain

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/ports"
)

// FocusKeeper holds the previous focus (the incumbent) in process memory and
// runs the one focus computation both of its writers call (design m4c §3.1).
//
// It is built exactly once per serve process, by cmd/nooma, and handed to
// every writer: a freshly constructed keeper holds nothing, which is what
// makes "a fresh service has no incumbent" true by construction (I01, R3).
type FocusKeeper struct {
	units ports.UnitRepo
	cfg   ports.ConfigRepo
	rels  ports.RelationRepo
	held  atomic.Pointer[incumbent] // nil: no incumbent, a fresh process
}

// NewFocusKeeper builds a keeper with no incumbent.
func NewFocusKeeper(units ports.UnitRepo, cfg ports.ConfigRepo, rels ports.RelationRepo) *FocusKeeper {
	return &FocusKeeper{units: units, cfg: cfg, rels: rels}
}

// incumbent is one whole two-Kind snapshot. It is never mutated after
// publish: a new round builds a new one.
type incumbent struct {
	byKind map[focus.Kind]focus.Selection
}

// focusRound is one computation's whole result.
type focusRound struct {
	members map[focus.Kind][]focus.Ranked // Select's order; Score is Rank's literal value
	next    *incumbent                    // both Kinds, always
}

// compute runs one focus computation at now.
func (k *FocusKeeper) compute(ctx context.Context, now time.Time) (focusRound, error) {
	return focusRound{
		members: map[focus.Kind][]focus.Ranked{},
		next:    &incumbent{byKind: map[focus.Kind]focus.Selection{}},
	}, nil
}

// publish replaces the held incumbent with the round's, whole.
func (k *FocusKeeper) publish(r focusRound) { k.held.Store(r.next) }
