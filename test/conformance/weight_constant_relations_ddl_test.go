package conformance

import (
	"strconv"
	"testing"

	"github.com/rengo/nooma/internal/core/weight"
)

// TestWeightConstantRelationsMatchMigrationDefault pins R2.4 and R2.7's
// two constant relations from internal/core/weight against
// config.weight_threshold's migration DEFAULT (design D4, ruling 4) — not
// against a Go constant, because m2a declares none for that column.
// weight_threshold's Go home is m2b's feat/core-consolidation-expire-archive
// (proposal §5.1); m2a only reads the DDL text off disk to prove its own
// constants stay consistent with it.
//
// Both assertions are INEQUALITIES over the DEFAULTS, not equalities:
// weight_threshold is marked ⚙ recalibratable per user in doc 02 §13, so a
// user who raises it (say, to 0.8) breaks the *relation* without breaking
// the *code*. This test constrains the shipped defaults and says so.
//
// Not a missing-symbol TDD red step (tasks.md's own intro, 2b.3): every
// constant named here already exists by the time this file lands —
// weight.ReviveGain and weight.WeightCeiling from PR 2a,
// weight.ResurfaceAttenuation and weight.ResurfaceMaxHops from this PR's
// own spread.go stub — and both inequalities already hold at the chosen
// defaults (0.35*2.0=0.70>0.5; 0.5^2*2.0=0.5<=0.5). What this test proves
// instead of a red step: it is the permanent guard against a future
// recalibration of any of the four constants silently breaking either
// relation, run every time the suite runs rather than only when someone
// remembers to check by hand.
//
// L2 rather than L1: it reads migration files off disk, and depguard
// denies os to internal/core/** with no $test selector.
func TestWeightConstantRelationsMatchMigrationDefault(t *testing.T) {
	body, ok := extractTableBody(migrationSQLText(t), "config")
	if !ok {
		t.Fatal("no CREATE TABLE config found in the migrations — this test cannot " +
			"pass vacuously, so the missing table is the failure")
	}

	literal := columnDefault(body, "weight_threshold")
	if literal == "" {
		t.Fatal("config.weight_threshold declares no DEFAULT in the migrations — R2.4 " +
			"and R2.7 rest on that default existing, so its absence is the failure, not a skip")
	}
	threshold, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		t.Fatalf("config.weight_threshold DEFAULT %q does not parse as a float: %v", literal, err)
	}

	t.Run("R2.4_OneReviveAlwaysClearsTheArchiveBand", func(t *testing.T) {
		revive := weight.ReviveGain * weight.WeightCeiling
		if !(revive > threshold) {
			t.Errorf("weight.ReviveGain * weight.WeightCeiling = %v, want strictly greater "+
				"than config.weight_threshold's default %v — one direct revive must always "+
				"clear the archive band at the shipped defaults (R2.4)", revive, threshold)
		}
	})

	t.Run("R2.7_ResurfaceAloneCannotClearTheArchiveBandAtMaxHops", func(t *testing.T) {
		maxGain := 1.0
		for i := 0; i < weight.ResurfaceMaxHops; i++ {
			maxGain *= weight.ResurfaceAttenuation
		}
		maxTarget := maxGain * weight.WeightCeiling
		if !(maxTarget <= threshold) {
			t.Errorf("weight.ResurfaceAttenuation^weight.ResurfaceMaxHops * weight.WeightCeiling "+
				"= %v, want less than or equal to config.weight_threshold's default %v — "+
				"spreading activation alone must never lift a unit above the archive band at "+
				"maximum hop distance (R2.7)", maxTarget, threshold)
		}
	})
}
