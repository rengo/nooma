package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// write is the one door every admin config change goes through (m4e2-admin
// design §3.1). It reads the clock once, runs apply (the port setter, always
// inside a closure passed here: test/conformance's G5 fails a setter called
// anywhere else), and only then records the config.updated row with the same
// instant, so a failed write logs nothing. If the row cannot be written after
// the value landed, the value stays and the error wraps ErrWriteLanded.
//
// previous is the value in force before the write, next the value now
// stored; note qualifies previous when it was not a stored valid value.
func (s *AdminService) write(ctx context.Context, field ConfigField, previous, next any, note string, apply func(at time.Time) error) error {
	now := s.clock.Now()
	if err := apply(now); err != nil {
		return fmt.Errorf("admin: write %s: %w", field, err)
	}
	col := string(field)
	contextJSON, err := json.Marshal(struct {
		Fields   []string       `json:"fields"`
		Previous map[string]any `json:"previous"`
		Next     map[string]any `json:"next"`
	}{[]string{col}, map[string]any{col: previous}, map[string]any{col: next}})
	if err == nil {
		err = s.log.Record(ctx, ports.Decision{
			ID:         s.ids.New(),
			Action:     ports.ActionConfigUpdated,
			Rationale:  fmt.Sprintf("the user changed %s from %v to %v%s", col, previous, next, note),
			Context:    contextJSON,
			OccurredAt: now,
		})
	}
	if err != nil {
		return &WriteLandedError{Record: true, Err: fmt.Errorf("admin: record %s change: %w", col, err)}
	}
	return nil
}

// The five appliers. Each hands its own port setter to write; none is named
// Set..., so no AdminService method can be mistaken for a port setter.

func (s *AdminService) applyWeightThreshold(ctx context.Context, previous, v float64, note string) error {
	return s.write(ctx, ConfigWeightThreshold, previous, v, note, func(at time.Time) error {
		return s.cfg.SetWeightThreshold(ctx, v, at)
	})
}

func (s *AdminService) applyHysteresisMargin(ctx context.Context, previous, v float64, note string) error {
	return s.write(ctx, ConfigHysteresisMargin, previous, v, note, func(at time.Time) error {
		return s.cfg.SetHysteresisMargin(ctx, v, at)
	})
}

func (s *AdminService) applyGoalStagnationDays(ctx context.Context, previous, v int, note string) error {
	return s.write(ctx, ConfigGoalStagnationDays, previous, v, note, func(at time.Time) error {
		return s.cfg.SetGoalStagnationDays(ctx, v, at)
	})
}

func (s *AdminService) applyMentalLoadThreshold(ctx context.Context, previous, v int, note string) error {
	return s.write(ctx, ConfigMentalLoadThreshold, previous, v, note, func(at time.Time) error {
		return s.cfg.SetMentalLoadThreshold(ctx, v, at)
	})
}

func (s *AdminService) applyConsolidationEnabled(ctx context.Context, previous, v bool, note string) error {
	return s.write(ctx, ConfigConsolidationEnabled, previous, v, note, func(at time.Time) error {
		return s.cfg.SetConsolidationEnabled(ctx, v, at)
	})
}
