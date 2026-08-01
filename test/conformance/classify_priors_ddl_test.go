package conformance

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/classify"
)

// columnDefault returns the literal following DEFAULT on the line declaring
// columnName inside a CREATE TABLE body, or "" if the column is absent or
// declares no default. The scan is line-oriented because migration 0001
// declares one column per line; a column whose default moved onto a
// continuation line would return "" and fail the test loudly rather than
// silently matching the wrong line.
func columnDefault(tableBody, columnName string) string {
	for _, line := range strings.Split(tableBody, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != columnName {
			continue
		}
		for i, f := range fields {
			if strings.EqualFold(f, "DEFAULT") && i+1 < len(fields) {
				return strings.TrimRight(fields[i+1], ",")
			}
		}
		return ""
	}
	return ""
}

// TestClassifyPriorsMatchMigrationDefaults pins classify's two base priors to
// migration 0001's own column DEFAULTs — design D3.
//
// units.weight and units.weight_decay_rate are NOT NULL, so a classification
// that degraded either one cannot be persisted as null: something must supply
// a value. Doc 02 §13 names the knob ("prior per type, base 0.01/day") and
// enumerates no per-type table anywhere, and doc 02 §2 makes personalizing
// the value the model's job, performed through the prompt — not a table in
// Go. So classify declares exactly two numbers, and they are the two the
// schema already declares. This test is what keeps them one number rather
// than two that drift.
//
// The comparison is over PARSED FLOATS, never source text (design D3's own
// warning): SQL writes 1.0 and 0.01, Go may render either differently, and a
// string comparison would fail on formatting while passing on a real
// divergence like 0.1 vs 0.10.
//
// L2 rather than L1: it reads migration files off disk, and depguard denies
// os to internal/core/** with no $test selector (design D11).
func TestClassifyPriorsMatchMigrationDefaults(t *testing.T) {
	body, ok := extractTableBody(migrationSQLText(t), "units")
	if !ok {
		t.Fatal("no CREATE TABLE units found in the migrations — this test cannot " +
			"pass vacuously, so the missing table is the failure")
	}

	tests := []struct {
		column string
		got    float64
		goName string
	}{
		{"weight", classify.PriorWeight, "classify.PriorWeight"},
		{"weight_decay_rate", classify.PriorDecayRate, "classify.PriorDecayRate"},
	}

	for _, tc := range tests {
		t.Run(tc.column, func(t *testing.T) {
			literal := columnDefault(body, tc.column)
			if literal == "" {
				t.Fatalf("units.%s declares no DEFAULT in migration 0001 — design D3 rests "+
					"on that default existing, so its absence is the failure, not a skip",
					tc.column)
			}

			want, err := strconv.ParseFloat(literal, 64)
			if err != nil {
				t.Fatalf("units.%s DEFAULT %q does not parse as a float: %v",
					tc.column, literal, err)
			}

			if tc.got != want {
				t.Errorf("%s = %v, but migration 0001 declares units.%s DEFAULT %v — "+
					"design D3 requires one number, not two that drift. Fix the constant, "+
					"or write the next migration; a published migration is never modified.",
					tc.goName, tc.got, tc.column, want)
			}
		})
	}
}
