package conformance

import (
	"strconv"
	"testing"

	"github.com/rengo/nooma/internal/core/relation"
)

// TestRelationThresholdDefaultsMatchMigration pins relation's two default
// thresholds to migration 0002's relation_thresholds column DEFAULTs —
// design D7, Q1.
//
// The comparison is over PARSED FLOATS, never source text (the same warning
// classify_priors_ddl_test.go carries): SQL writes 0.3 and 0.5, Go writes
// 0.30 and 0.50, and a string comparison would fail on formatting while
// passing on a real divergence like 0.1 vs 0.10.
//
// L2 rather than L1: it reads migration files off disk, and depguard denies
// os to internal/core/** with no $test selector (design D11).
func TestRelationThresholdDefaultsMatchMigration(t *testing.T) {
	body, ok := extractTableBody(migrationSQLText(t), "relation_thresholds")
	if !ok {
		t.Fatal("no CREATE TABLE relation_thresholds found in the migrations — this test " +
			"cannot pass vacuously, so the missing table is the failure")
	}

	tests := []struct {
		column string
		got    float64
		goName string
	}{
		{"min_confidence_to_persist", relation.DefaultMinConfidenceToPersist, "relation.DefaultMinConfidenceToPersist"},
		{"min_confidence_to_surface", relation.DefaultMinConfidenceToSurface, "relation.DefaultMinConfidenceToSurface"},
	}

	for _, tc := range tests {
		t.Run(tc.column, func(t *testing.T) {
			literal := columnDefault(body, tc.column)
			if literal == "" {
				t.Fatalf("relation_thresholds.%s declares no DEFAULT in migration 0002 — "+
					"Q1 rests on that default existing, so its absence is the failure, not a skip",
					tc.column)
			}

			want, err := strconv.ParseFloat(literal, 64)
			if err != nil {
				t.Fatalf("relation_thresholds.%s DEFAULT %q does not parse as a float: %v",
					tc.column, literal, err)
			}

			if tc.got != want {
				t.Errorf("%s = %v, but migration 0002 declares relation_thresholds.%s DEFAULT %v — "+
					"Q1 requires one number, not two that drift. Fix the constant, or write the "+
					"next migration; a published migration is never modified.",
					tc.goName, tc.got, tc.column, want)
			}
		})
	}
}
