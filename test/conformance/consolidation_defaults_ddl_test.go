// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"strconv"
	"testing"

	"github.com/rengo/nooma/internal/core/consolidation"
)

// TestConsolidationDefaultWeightThresholdMatchesMigration pins
// consolidation.DefaultWeightThreshold against migration 0002's
// config.weight_threshold column DEFAULT (spec R2.5), the same
// DDL-pinning mechanism relation_thresholds_ddl_test.go and
// focus_margin_ddl_test.go already use for their own Go constants.
//
// Not a missing-symbol TDD red step: consolidation.DefaultWeightThreshold
// already exists (this PR's own task 2.7) and already equals 0.5, matching
// the migration default — this test is the permanent guard against a
// future drift between the two.
//
// L2 rather than L1: it reads migration files off disk, and depguard
// denies os to internal/core/** with no $test selector.
func TestConsolidationDefaultWeightThresholdMatchesMigration(t *testing.T) {
	body, ok := extractTableBody(migrationSQLText(t), "config")
	if !ok {
		t.Fatal("no CREATE TABLE config found in the migrations — this test cannot " +
			"pass vacuously, so the missing table is the failure")
	}

	literal := columnDefault(body, "weight_threshold")
	if literal == "" {
		t.Fatal("config.weight_threshold declares no DEFAULT in the migrations — R2.5 " +
			"rests on that default existing, so its absence is the failure, not a skip")
	}

	want, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		t.Fatalf("config.weight_threshold DEFAULT %q does not parse as a float: %v", literal, err)
	}

	if consolidation.DefaultWeightThreshold != want {
		t.Errorf("consolidation.DefaultWeightThreshold = %v, but migration 0002 declares "+
			"config.weight_threshold DEFAULT %v — R2.5 requires one number, not two that "+
			"drift. Fix the constant, or write the next migration; a published migration "+
			"is never modified.", consolidation.DefaultWeightThreshold, want)
	}
}

// TestConsolidationDefaultGoalStagnationDaysMatchesMigration pins
// consolidation.DefaultGoalStagnationDays against migration 0002's
// config.goal_stagnation_days column DEFAULT (spec R5.4, task 5.8).
//
// Not a missing-symbol TDD red step: consolidation.DefaultGoalStagnationDays
// already exists (this PR's own task 5.1) and already equals 21, matching
// the migration default — this test is the permanent guard against a
// future drift between the two. It does NOT pin which schema home
// goal_stagnation_days ultimately reads from at runtime (design.md §9 Q3:
// two homes exist, config.goal_stagnation_days and calibration's own
// example key — m2c decides).
func TestConsolidationDefaultGoalStagnationDaysMatchesMigration(t *testing.T) {
	body, ok := extractTableBody(migrationSQLText(t), "config")
	if !ok {
		t.Fatal("no CREATE TABLE config found in the migrations — this test cannot " +
			"pass vacuously, so the missing table is the failure")
	}

	literal := columnDefault(body, "goal_stagnation_days")
	if literal == "" {
		t.Fatal("config.goal_stagnation_days declares no DEFAULT in the migrations — R5.4 " +
			"rests on that default existing, so its absence is the failure, not a skip")
	}

	want, err := strconv.Atoi(literal)
	if err != nil {
		t.Fatalf("config.goal_stagnation_days DEFAULT %q does not parse as an int: %v", literal, err)
	}

	if consolidation.DefaultGoalStagnationDays != want {
		t.Errorf("consolidation.DefaultGoalStagnationDays = %v, but migration 0002 declares "+
			"config.goal_stagnation_days DEFAULT %v — R5.4 requires one number, not two that "+
			"drift. Fix the constant, or write the next migration; a published migration "+
			"is never modified.", consolidation.DefaultGoalStagnationDays, want)
	}
}

// TestConsolidationDefaultMentalLoadThresholdMatchesMigration pins
// consolidation.DefaultMentalLoadThreshold against migration 0002's
// config.mental_load_threshold column DEFAULT (spec R5.4, task 5.8).
//
// Not a missing-symbol TDD red step: consolidation.DefaultMentalLoadThreshold
// already exists (this PR's own task 5.3) and already equals 7, matching
// the migration default.
//
// This file now pins three DDL-backed Default* constants total
// (DefaultWeightThreshold from PR 2, plus these two), not four —
// consolidation.BeliefMergeCosine (PR 4's fourth Default*-shaped value) has
// no schema DEFAULT to pin against (it is not a config column) and is
// asserted by literal equality instead, in derive_test.go, not here.
func TestConsolidationDefaultMentalLoadThresholdMatchesMigration(t *testing.T) {
	body, ok := extractTableBody(migrationSQLText(t), "config")
	if !ok {
		t.Fatal("no CREATE TABLE config found in the migrations — this test cannot " +
			"pass vacuously, so the missing table is the failure")
	}

	literal := columnDefault(body, "mental_load_threshold")
	if literal == "" {
		t.Fatal("config.mental_load_threshold declares no DEFAULT in the migrations — R5.4 " +
			"rests on that default existing, so its absence is the failure, not a skip")
	}

	want, err := strconv.Atoi(literal)
	if err != nil {
		t.Fatalf("config.mental_load_threshold DEFAULT %q does not parse as an int: %v", literal, err)
	}

	if consolidation.DefaultMentalLoadThreshold != want {
		t.Errorf("consolidation.DefaultMentalLoadThreshold = %v, but migration 0002 declares "+
			"config.mental_load_threshold DEFAULT %v — R5.4 requires one number, not two that "+
			"drift. Fix the constant, or write the next migration; a published migration "+
			"is never modified.", consolidation.DefaultMentalLoadThreshold, want)
	}
}
