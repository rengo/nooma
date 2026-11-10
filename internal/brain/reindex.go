package brain

import (
	"context"
	"fmt"

	"github.com/rengo/nooma/internal/ports"
)

const (
	// reindexPage is how many ids one read of the work list returns.
	reindexPage = 100
	// reindexFailureStreak is how many refusals in a row stop a run, so a
	// provider that is down is not called once per unit (design D4).
	reindexFailureStreak = 3
)

// ErrReindexProviderUnavailable stops a reindex the provider refused
// reindexFailureStreak times in a row.
var ErrReindexProviderUnavailable = fmt.Errorf("the embedding provider refused %d units in a row and looks unavailable", reindexFailureStreak)

// ReindexService embeds the live units lacking a model's vector: nooma
// reindex (ADR-0058 point 4). Each unit is one provider call and one write,
// so a run stopped anywhere keeps what it wrote and the next one lists only
// what is left; nothing is paid twice and no other model's vector is touched.
type ReindexService struct {
	clock  ports.Clock
	units  ports.UnitRepo
	embeds ports.EmbeddingRepo
	embed  ports.EmbeddingProvider
}

// NewReindexService wires a ReindexService.
func NewReindexService(clock ports.Clock, units ports.UnitRepo, embeds ports.EmbeddingRepo, embed ports.EmbeddingProvider) *ReindexService {
	return &ReindexService{clock: clock, units: units, embeds: embeds, embed: embed}
}

// ReindexResult is what a run did: Missing is how many live units lacked the
// model's vector at the start; Failed the units refused, LastFailure the last.
type ReindexResult struct {
	Missing, Embedded, Failed int
	LastFailure               error
}

// Reindex embeds, from its content, every live unit with no vector under
// model, by id, and stores the vector under model. progress, when not nil,
// hears how many of Missing are done after each unit. A refused unit is
// counted and passed; reindexFailureStreak refusals in a row stop the run
// with ErrReindexProviderUnavailable. An answer naming another model stops
// it too: storing it under model would mix two models' vectors (I21). A
// cancelled ctx stops the run between units. Whatever stops it, every
// vector already written stays.
func (s *ReindexService) Reindex(ctx context.Context, model string, progress func(done, total int)) (ReindexResult, error) {
	var res ReindexResult
	missing, err := s.embeds.CountLiveWithoutEmbedding(ctx, model)
	if err != nil {
		return res, fmt.Errorf("reindex: %w", err)
	}
	res.Missing = missing
	now := s.clock.Now()
	streak := 0

	for after := ""; ; {
		ids, err := s.embeds.LiveWithoutEmbedding(ctx, model, after, reindexPage)
		if err != nil {
			return res, fmt.Errorf("reindex: %w", err)
		}
		if len(ids) == 0 {
			return res, nil
		}
		after = ids[len(ids)-1]
		live, err := s.units.LiveByIDs(ctx, ids)
		if err != nil {
			return res, fmt.Errorf("reindex: %w", err)
		}
		for _, u := range live {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			ev, err := s.embed.Embed(ctx, ports.EmbedRequest{Text: u.Content})
			if ctxErr := ctx.Err(); ctxErr != nil {
				return res, ctxErr
			}
			if err != nil {
				res.Failed++
				res.LastFailure = fmt.Errorf("unit %s: %w", u.ID, err)
				if streak++; streak == reindexFailureStreak {
					return res, fmt.Errorf("%w: %w", ErrReindexProviderUnavailable, res.LastFailure)
				}
			} else {
				if ev.Model != model {
					return res, fmt.Errorf("the provider answered for model %q, not %q; nothing was stored under either", ev.Model, model)
				}
				if err := s.embeds.Put(ctx, ports.Embedding{UnitID: u.ID, Model: model, Vector: ev.Vector, At: now}); err != nil {
					return res, fmt.Errorf("reindex: storing unit %s: %w", u.ID, err)
				}
				res.Embedded++
				streak = 0
			}
			if progress != nil {
				progress(res.Embedded+res.Failed, res.Missing)
			}
		}
	}
}
