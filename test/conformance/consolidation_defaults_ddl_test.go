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
