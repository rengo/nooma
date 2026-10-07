package brain

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/weight"
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

	// adjacent and nextAdjacent are unit-keyed adjacency for the pending
	// digest's Carry, against the loaded incumbent P and against the round's
	// own next incumbent P' (design m4c §3.5). Unset until the second link.
	adjacent     map[string]float64
	nextAdjacent map[string]float64
}

// selection is the held Selection for k, empty when nothing is held.
func (p *incumbent) selection(k focus.Kind) focus.Selection {
	if p == nil {
		return focus.Selection{}
	}
	return p.byKind[k]
}

// compute runs one focus computation at now, against ONE load of the held
// incumbent P. That single snapshot is the "incumbent before this request's
// Select" for every adjacency read and every Select in the round, so the
// ordering R8 and R9 require is a property of the data flow, not a rule a
// writer must remember. compute reads and never stores: a writer publishes
// the round only once it has earned it.
//
// For each Kind it ranks that Kind's live candidates with adjacency to P's
// own members of that Kind (OQ4: a task is not lifted for being related to a
// worry), then Selects with the configured margin. A member's Score is the
// literal value Rank produced for it in this round, adjacency included (R7),
// looked up by id, in Select's order.
func (k *FocusKeeper) compute(ctx context.Context, now time.Time) (focusRound, error) {
	prev := k.held.Load()

	cfg, err := k.cfg.Load(ctx)
	if err != nil {
		return focusRound{}, fmt.Errorf("focus: reading config: %w", err)
	}
	margin := focus.ResolveMargin(cfg.HysteresisMargin)

	edges, err := k.edges(ctx, prev)
	if err != nil {
		return focusRound{}, err
	}

	round := focusRound{
		members: make(map[focus.Kind][]focus.Ranked, len(focus.AllKinds())),
		next:    &incumbent{byKind: make(map[focus.Kind]focus.Selection, len(focus.AllKinds()))},
	}
	for _, kind := range focus.AllKinds() {
		candidates, err := k.units.LiveFocusCandidatesByType(ctx, focus.Types(kind))
		if err != nil {
			return focusRound{}, fmt.Errorf("focus: candidates for %q: %w", kind, err)
		}
		held := prev.selection(kind)
		ranked := focus.Rank(candidates, focus.AdjacencyStrengths(held, edges), now)
		selected := focus.Select(kind, ranked, held, margin, focus.DefaultSize)

		scoreByID := make(map[string]focus.Ranked, len(ranked))
		for _, r := range ranked {
			scoreByID[r.Candidate.ID] = r
		}
		members := make([]focus.Ranked, len(selected.Members))
		for i, id := range selected.Members {
			members[i] = scoreByID[id]
		}
		round.members[kind] = members
		round.next.byKind[kind] = selected
	}
	return round, nil
}

// edges is the one ByUnit sweep over every member of P, both Kinds, as
// undirected weight edges. A relation's Strength is its relevance; its
// Confidence is certainty, and the two are never combined (doc 02 §4).
func (k *FocusKeeper) edges(ctx context.Context, p *incumbent) ([]weight.Edge, error) {
	var edges []weight.Edge
	for _, kind := range focus.AllKinds() {
		for _, id := range p.selection(kind).Members {
			rels, err := k.rels.ByUnit(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("focus: relations of %q: %w", id, err)
			}
			for _, r := range rels {
				edges = append(edges, weight.Edge{From: r.FromUnitID, To: r.ToUnitID, Strength: r.Strength})
			}
		}
	}
	return edges, nil
}

// publish replaces the held incumbent with the round's, whole. The only write
// the type allows is this one pointer swap, so two concurrent writers leave
// one writer's complete selection, never a mix.
func (k *FocusKeeper) publish(r focusRound) { k.held.Store(r.next) }
