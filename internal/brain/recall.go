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
}

// NewRecallService wires a RecallService over the ports its two legs need.
func NewRecallService(index *Index, lex ports.LexicalSearch, units ports.UnitRepo) *RecallService {
	return &RecallService{index: index, lex: lex, units: units}
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
