package focus

// DefaultHysteresisMargin is doc 02 §13's `hysteresis_margin (focus,
// relative)` default (spec R4.4, design D4's surviving half, ruling 5).
// Pinned to migration 0002's config.hysteresis_margin column DEFAULT by
// test/conformance/focus_margin_ddl_test.go — PR 4b's own file, since this
// PR declares the constant and its meaning, not the DDL pin itself.
const DefaultHysteresisMargin = 0.05

// ResolveMargin is relation.Resolve's shape verbatim
// (internal/core/relation/thresholds.go:26-38): a nil configured means
// config.hysteresis_margin's row has never been written, and a non-nil one
// passes through unchanged (spec R4.4).
//
// STUB (RED commit, design D11): returns the zero value unconditionally.
// The implementation lands in the paired GREEN commit.
func ResolveMargin(configured *float64) float64 {
	return 0
}

// Displaces implements doc 02 §3's anti-jitter hysteresis (spec R4.3,
// design D8): a challenger displaces an incumbent from the focus only when
// it exceeds the incumbent by MORE than margin, relative.
//
// STUB (RED commit, design D11): returns the zero value unconditionally.
// The implementation lands in the paired GREEN commit.
func Displaces(challenger, incumbent, margin float64) bool {
	return false
}
