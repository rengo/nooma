package brain

import (
	"context"
	"fmt"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// RecallService is the one hybrid-recall mechanism spec R4.4 and design D5
// name: it does not reimplement core/recall's fusion, it calls it — the
// vector leg (recall.Search over Index's own snapshot), the lexical leg
// (ports.LexicalSearch over recall.Tokenize), fused by recall.Fuse (design
// D5: "Phase B always passes exactly two, vector first"). ADR-0010's "one
// mechanism, three consumers" names capture's dedup/relation candidates
// (this PR) as the first of the three; answering a standalone /recall and
// consolidation's connect phase are later phases reusing this same type.
//
// RecallService holds no ports.Clock and needs none: nothing in the hybrid
// recall computation is a function of time (design D4's "RecallService takes
// the same shape" is about the clockless-worker split generally, not a claim
// that this particular service reads an instant it happens not to need).
type RecallService struct {
	index *Index
	lex   ports.LexicalSearch
	units ports.UnitRepo
	embed ports.EmbeddingProvider
}

// NewRecallService wires a RecallService over the ports its two legs need.
// embed is design D9's addition: Candidates never needs it (its caller
// already has a vector), but ScoredFor/ForText do, because a standalone
// /recall query has no vector until this service embeds it.
func NewRecallService(index *Index, lex ports.LexicalSearch, units ports.UnitRepo, embed ports.EmbeddingProvider) *RecallService {
	return &RecallService{index: index, lex: lex, units: units, embed: embed}
}

// Candidates returns the dedup/relation candidates for a unit whose
// unit-normalized embedding is vector (produced under model) and whose text
// is content, excluding excludeID — the just-persisted unit itself — from
// its own candidate list (spec R4.4's own MUST).
//
// Where I02 is enforced, and why here and nowhere else in this method: both
// legs return bare ids, resolved through exactly one call to
// ports.UnitRepo.LiveByIDs, which alone filters positively on status = pool
// (design D5's "one filter, for both legs, before fusion" — neither
// recall.Search over Index nor ports.LexicalSearch's in-memory fake knows
// what a unit's status is, and this method must not teach either of them,
// or I02 would have two places to get wrong instead of one). The returned
// units are LiveByIDs' own materialized values, in the fused ranking's
// order — the same units a later relation-judging step (PR 11c) needs in
// full, not merely by id.
func (s *RecallService) Candidates(ctx context.Context, content string, vector []float32, model string, excludeID string) ([]unit.Unit, error) {
	var vectorIDs []string
	if len(vector) > 0 {
		scored, err := recall.Search(s.index.Snapshot(), recall.VectorQuery{
			Model:  model,
			Vector: vector,
			K:      recall.RecallTopK,
		})
		if err != nil {
			return nil, fmt.Errorf("recall: vector leg: %w", err)
		}
		vectorIDs = make([]string, 0, len(scored))
		for _, sc := range scored {
			if sc.ID == excludeID {
				continue
			}
			vectorIDs = append(vectorIDs, sc.ID)
		}
	}

	lexicalHits, err := s.lex.SearchLexical(ctx, recall.Tokenize(content), recall.RecallTopK)
	if err != nil {
		return nil, fmt.Errorf("recall: lexical leg: %w", err)
	}
	lexicalIDs := make([]string, 0, len(lexicalHits))
	for _, id := range lexicalHits {
		if id == excludeID {
			continue
		}
		lexicalIDs = append(lexicalIDs, id)
	}

	fused := recall.Fuse(vectorIDs, lexicalIDs)

	live, err := s.units.LiveByIDs(ctx, fused)
	if err != nil {
		return nil, fmt.Errorf("recall: resolve live candidates: %w", err)
	}
	return live, nil
}

// ScoredUnit is one live candidate paired with the fused score that ranked
// it (design D9), joined from FuseScored's own map rather than recomputed.
type ScoredUnit struct {
	Unit  unit.Unit
	Score float64
}

// ScoredFor answers a raw-text recall query — design D9: capture's own
// `type: recall` entrance and the standalone /recall route both call this
// one method with the same `text` argument, always the raw text the caller
// received, never classify.NormalizedContent (/recall never runs classify,
// so a normalized-text call site would answer the same question
// differently — I22 pins this). Unlike Candidates, it embeds text itself,
// since /recall's caller has no vector to hand it.
//
// The bool reports whether the vector leg ran: an embedding-provider outage,
// or a query that normalizes to a zero vector, degrades to the lexical leg
// alone rather than refusing the read (`m1b`'s D8 product rule, restated for
// the read path), and both entrances degrade identically because both call
// this one method.
//
// Scores survive the live filter by a join, not a second fusion: LiveByIDs
// returns survivors in the caller's id order, so this walks them once
// against an id -> score map built from FuseScored.
// ScoredFor is the generous net: everything either leg found, fused. Its
// callers ask "what MIGHT be related" — correction referent resolution and
// the nightly connect phase — and a judge decides afterwards. ADR-0010's
// own warning is why it is left alone: its fusion feeds the relation
// graph, and a bias here propagates into all of it.
func (s *RecallService) ScoredFor(ctx context.Context, text string) ([]ScoredUnit, bool, error) {
	return s.scoredFor(ctx, text, false)
}

// scoredFor gathers both legs. admit applies ADR-0020's floor, which only
// the reader-facing answer wants.
func (s *RecallService) scoredFor(ctx context.Context, text string, admit bool) ([]ScoredUnit, bool, error) {
	var vectorIDs []string
	semanticLegAvailable := false

	if ev, err := s.embed.Embed(ctx, ports.EmbedRequest{Text: text}); err == nil {
		if normalized, nerr := recall.Normalize(ev.Vector); nerr == nil {
			scored, serr := recall.Search(s.index.Snapshot(), recall.VectorQuery{
				Model:  ev.Model,
				Vector: normalized,
				K:      recall.RecallTopK,
			})
			if serr != nil {
				return nil, false, fmt.Errorf("recall: vector leg: %w", serr)
			}
			// **The vector admits** (ADR-0020), for the reader-facing
			// answer only. Everything below the floor stops being an
			// answer here rather than being ranked low and printed anyway.
			admitted := scored
			if admit {
				admitted = recall.Admit(scored)
			}
			vectorIDs = make([]string, len(admitted))
			for i, sc := range admitted {
				vectorIDs[i] = sc.ID
			}
			semanticLegAvailable = true
		}
	}

	lexicalIDs, err := s.lex.SearchLexical(ctx, recall.Tokenize(text), recall.RecallTopK)
	if err != nil {
		return nil, false, fmt.Errorf("recall: lexical leg: %w", err)
	}

	// **The lexical leg ranks, it does not admit** (ADR-0020). It kept
	// admitting on function words — Tokenize has no stopword handling, so
	// "cuando tengo gym" matched the dentist on "tengo" — which is why a
	// similarity floor on the vector leg alone would have changed nothing.
	//
	// It is intersected rather than dropped: an admitted unit that also
	// matches lexically still ranks above one that does not, which is what
	// the fusion is for once admission is settled.
	if admit && semanticLegAvailable {
		lexicalIDs = intersect(lexicalIDs, vectorIDs)
	}

	fused := recall.FuseScored(vectorIDs, lexicalIDs)
	ids := make([]string, len(fused))
	scoreByID := make(map[string]float64, len(fused))
	for i, fc := range fused {
		ids[i] = fc.ID
		scoreByID[fc.ID] = fc.Score
	}

	live, err := s.units.LiveByIDs(ctx, ids)
	if err != nil {
		return nil, false, fmt.Errorf("recall: resolve live candidates: %w", err)
	}

	scoredUnits := make([]ScoredUnit, len(live))
	for i, u := range live {
		scoredUnits[i] = ScoredUnit{Unit: u, Score: scoreByID[u.ID]}
	}
	return scoredUnits, semanticLegAvailable, nil
}

// LiveByIDs is a thin passthrough to the ports.UnitRepo already injected
// here (D9's units field). It exists so internal/httpapi's read-only unit
// routes (GET /units/{id}, GET /units?ids=, design D10, spec R2.6) can
// resolve ids without internal/httpapi importing internal/ports directly —
// design's own dependency-rule check (design.md §4, "Dependency-rule check")
// sanctions only internal/brain, internal/core/unit and crypto/subtle for
// that package, not internal/ports. It adds no behaviour of its own: I02's
// positive live filter is LiveByIDs' own property, unchanged by this
// indirection — a non-live id and an unknown id are equally absent from its
// return value, which is what lets both answer the identical 404 upstream.
func (s *RecallService) LiveByIDs(ctx context.Context, ids []string) ([]unit.Unit, error) {
	return s.units.LiveByIDs(ctx, ids)
}

// ForText is ScoredFor with its scores dropped — the shape both entrances
// design D9 names actually return to their own callers. A projection of
// ScoredFor, not a second implementation, for the same reason recall.Fuse is
// a projection of recall.FuseScored (design D1).
func (s *RecallService) ForText(ctx context.Context, text string) ([]unit.Unit, bool, error) {
	// **The answering entrance, and the only one that admits** — ADR-0020.
	// Both callers of ForText are reader-facing: the HTTP recall endpoint
	// and the chat's own recall Kind. A bad match reaching either is
	// presented to a person as a fact.
	scored, ok, err := s.scoredFor(ctx, text, true)
	if err != nil {
		return nil, false, err
	}
	units := make([]unit.Unit, len(scored))
	for i, su := range scored {
		units[i] = su.Unit
	}
	return units, ok, nil
}

// intersect keeps the members of keep that appear in admitted, in keep's
// own order — the lexical leg's ranking, restricted to what was admitted.
//
// A map rather than a nested loop because both lists are RecallTopK long
// and this runs on every recall; a slice scan would be fine today and is
// the kind of thing that stops being fine quietly.
func intersect(keep, admitted []string) []string {
	if len(keep) == 0 || len(admitted) == 0 {
		return nil
	}
	ok := make(map[string]bool, len(admitted))
	for _, id := range admitted {
		ok[id] = true
	}
	out := make([]string, 0, len(keep))
	for _, id := range keep {
		if ok[id] {
			out = append(out, id)
		}
	}
	return out
}
