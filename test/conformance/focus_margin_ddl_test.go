// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"strconv"
	"testing"

	"github.com/rengo/nooma/internal/core/focus"
)

// TestFocusMarginDefaultMatchesMigration pins focus.DefaultHysteresisMargin
// against migration 0002's config.hysteresis_margin column DEFAULT (spec
// R4.4, design D4's surviving half, ruling 5) — the same DDL-pinning
// mechanism relation_thresholds_ddl_test.go and
// weight_constant_relations_ddl_test.go already use for their own Go
// constants.
//
// Not a missing-symbol TDD red step: focus.DefaultHysteresisMargin already
// exists (PR 4a) and already equals 0.05, matching the migration default —
// this test is the permanent guard against a future drift between the two,
// run every time the suite runs rather than only when someone remembers to
// check by hand (the same framing weight_constant_relations_ddl_test.go's
// own doc comment states for its own constants).
//
// L2 rather than L1: it reads migration files off disk, and depguard denies
// os to internal/core/** with no $test selector.
func TestFocusMarginDefaultMatchesMigration(t *testing.T) {
	body, ok := extractTableBody(migrationSQLText(t), "config")
	if !ok {
		t.Fatal("no CREATE TABLE config found in the migrations — this test cannot " +
			"pass vacuously, so the missing table is the failure")
	}

	literal := columnDefault(body, "hysteresis_margin")
	if literal == "" {
		t.Fatal("config.hysteresis_margin declares no DEFAULT in the migrations — R4.4 " +
			"rests on that default existing, so its absence is the failure, not a skip")
	}

	want, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		t.Fatalf("config.hysteresis_margin DEFAULT %q does not parse as a float: %v", literal, err)
	}

	if focus.DefaultHysteresisMargin != want {
		t.Errorf("focus.DefaultHysteresisMargin = %v, but migration 0002 declares "+
			"config.hysteresis_margin DEFAULT %v — R4.4 requires one number, not two "+
			"that drift. Fix the constant, or write the next migration; a published "+
			"migration is never modified.", focus.DefaultHysteresisMargin, want)
	}
}
