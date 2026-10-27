package brain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
)

// AdminRecentEffectRows is how many consolidation effects the admin page
// lists. A transport constant of the ActivityPageSize class: it shapes a
// list and decides nothing doc 02 governs.
const AdminRecentEffectRows = 10

// AdminService is the admin page's door (m4e2-admin): it reads the settings
// and job status, and writes the five settings of ConfigField, each logged.
// It exports View and Update and nothing else.
type AdminService struct {
	clock ports.Clock
	ids   ports.IDGen
	cfg   ports.ConfigRepo
	rels  ports.RelationRepo
	log   ports.DecisionLog
}

// NewAdminService wires an AdminService.
func NewAdminService(clock ports.Clock, ids ports.IDGen, cfg ports.ConfigRepo, rels ports.RelationRepo, log ports.DecisionLog) *AdminService {
	return &AdminService{clock: clock, ids: ids, cfg: cfg, rels: rels, log: log}
}

// AdminSettings is the five settings' values in force.
type AdminSettings struct {
	WeightThreshold      float64
	HysteresisMargin     float64
	GoalStagnationDays   int
	MentalLoadThreshold  int
	ConsolidationEnabled bool
}

// AdminView is everything /ui/admin shows.
type AdminView struct {
	// Settings is each value in force; Stored is the row as stored, so a
	// column never set (nil) can be told from one set to its default.
	Settings AdminSettings
	Stored   ports.VaultConfig
	// LastRun is the last full consolidation; nil means never.
	LastRun *time.Time
	// Learned is every learned relation threshold pair, by relation type;
	// OtherTypes is the pair every other type uses.
	Learned    []ports.LearnedThreshold
	OtherTypes relation.Thresholds
	// RecentEffects are the newest consolidation effects, newest first. A
	// quiet night leaves none: they are evidence of activity, not of
	// liveness.
	RecentEffects []ports.DecisionRow
}

// UpdateResult is what an Update did. Changed is false when the submitted
// value was already in force and nothing was written.
type UpdateResult struct {
	Field   ConfigField
	Changed bool
}

// View reads the admin page: each setting in force, the row as stored, the
// last full consolidation, the learned relation thresholds and the newest
// consolidation effects. It writes nothing.
func (s *AdminService) View(ctx context.Context) (AdminView, error) {
	stored, err := s.cfg.Load(ctx)
	if err != nil {
		return AdminView{}, fmt.Errorf("admin: read config: %w", err)
	}
	learned, err := s.rels.LearnedThresholds(ctx)
	if err != nil {
		return AdminView{}, fmt.Errorf("admin: read learned thresholds: %w", err)
	}
	effects, err := s.log.Before(ctx, nil, "consolidate.", AdminRecentEffectRows)
	if err != nil {
		return AdminView{}, fmt.Errorf("admin: read recent effects: %w", err)
	}
	return AdminView{
		Settings: AdminSettings{
			WeightThreshold:      consolidation.ResolveWeightThreshold(stored.WeightThreshold),
			HysteresisMargin:     focus.ResolveMargin(stored.HysteresisMargin),
			GoalStagnationDays:   consolidation.ResolveGoalStagnationDays(stored.GoalStagnationDays),
			MentalLoadThreshold:  consolidation.ResolveMentalLoadThreshold(stored.MentalLoadThreshold),
			ConsolidationEnabled: consolidation.ResolveConsolidationEnabled(stored.ConsolidationEnabled),
		},
		Stored:        stored,
		LastRun:       stored.ConsolidationLastRunAt,
		Learned:       learned,
		OtherTypes:    relation.Resolve(nil),
		RecentEffects: effects,
	}, nil
}

// Update parses raw for field, refuses it with a *ConfigRangeError unless the
// field's Resolve function would return it unchanged, and writes it through
// the logged door. A value already in force writes and logs nothing; a
// stored value outside the accepted set is not in force, so submitting the
// value the runtime fell back to repairs the column. A field outside the
// five is ErrUnknownConfigField, before any read.
func (s *AdminService) Update(ctx context.Context, field ConfigField, raw string) (UpdateResult, error) {
	if _, err := ParseConfigField(string(field)); err != nil {
		return UpdateResult{}, err
	}
	stored, err := s.cfg.Load(ctx)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("admin: read config: %w", err)
	}
	switch field {
	case ConfigWeightThreshold:
		return updateField(ctx, field, raw, stored.WeightThreshold, parseFloatField, consolidation.ResolveWeightThreshold, s.applyWeightThreshold)
	case ConfigHysteresisMargin:
		return updateField(ctx, field, raw, stored.HysteresisMargin, parseFloatField, focus.ResolveMargin, s.applyHysteresisMargin)
	case ConfigGoalStagnationDays:
		res, err := updateField(ctx, field, raw, stored.GoalStagnationDays, parseIntField, consolidation.ResolveGoalStagnationDays, s.applyGoalStagnationDays)
		if err != nil || res.Changed || stored.GoalStagnationPinned {
			return res, err
		}
		// Saved unchanged to keep it: still the user's word, so it pins (ADR-0055).
		v, _ := parseIntField(field, raw) // updateField already accepted it
		err = s.applyGoalStagnationDays(ctx, v, v, " (saved unchanged to keep it, so it is now pinned and the learner leaves it alone)")
		return UpdateResult{Field: field, Changed: err == nil || errors.Is(err, ErrWriteLanded)}, err
	case ConfigMentalLoadThreshold:
		return updateField(ctx, field, raw, stored.MentalLoadThreshold, parseIntField, consolidation.ResolveMentalLoadThreshold, s.applyMentalLoadThreshold)
	}
	return updateField(ctx, field, raw, stored.ConsolidationEnabled, parseBoolField, consolidation.ResolveConsolidationEnabled, s.applyConsolidationEnabled)
}

// updateField is Update for one field of type T: parse, accept iff resolve
// returns the value unchanged, judge "unchanged" against the value in force
// (never the stored pointer alone: an absent row is not a change), then
// apply.
func updateField[T comparable](
	ctx context.Context, field ConfigField, raw string, stored *T,
	parse func(ConfigField, string) (T, error),
	resolve func(*T) T,
	apply func(ctx context.Context, previous, v T, note string) error,
) (UpdateResult, error) {
	v, err := parse(field, raw)
	if err != nil {
		return UpdateResult{}, err
	}
	if resolve(&v) != v {
		return UpdateResult{}, field.refuse(raw)
	}
	inForce := resolve(stored)
	if inForce == v && (stored == nil || *stored == v) {
		return UpdateResult{Field: field}, nil
	}
	note := ""
	switch {
	case stored == nil:
		note = " (the previous value was the default)"
	case *stored != inForce:
		note = fmt.Sprintf(" (the stored value %v was invalid, so %v was in force)", *stored, inForce)
	}
	err = apply(ctx, inForce, v, note)
	return UpdateResult{Field: field, Changed: err == nil || errors.Is(err, ErrWriteLanded)}, err
}
