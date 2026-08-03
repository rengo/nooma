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
func (s *RecallService) ScoredFor(ctx context.Context, text string) ([]ScoredUnit, bool, error) {
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
			vectorIDs = make([]string, len(scored))
			for i, sc := range scored {
				vectorIDs[i] = sc.ID
			}
			semanticLegAvailable = true
		}
	}

	lexicalIDs, err := s.lex.SearchLexical(ctx, recall.Tokenize(text), recall.RecallTopK)
	if err != nil {
		return nil, false, fmt.Errorf("recall: lexical leg: %w", err)
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

// ForText is ScoredFor with its scores dropped — the shape both entrances
// design D9 names actually return to their own callers. A projection of
// ScoredFor, not a second implementation, for the same reason recall.Fuse is
// a projection of recall.FuseScored (design D1).
func (s *RecallService) ForText(ctx context.Context, text string) ([]unit.Unit, bool, error) {
	scored, ok, err := s.ScoredFor(ctx, text)
	if err != nil {
		return nil, false, err
	}
	units := make([]unit.Unit, len(scored))
	for i, su := range scored {
		units[i] = su.Unit
	}
	return units, ok, nil
}
