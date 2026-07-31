package recall

import (
	"reflect"
	"testing"
)

// TestFuse_ReproducesADR0010ByHand hand-computes ADR-0010's formula —
// score(d) = Σ w_i/(k + rank_i(d)), 1-indexed ranks — over two lists with no
// engineered ties, including an id present in only one list.
//
//	a: list0 rank1 only         -> 1/61                     ≈ 0.016393
//	b: list0 rank2, list1 rank1 -> 1/62 + 1/61               ≈ 0.032522
//	c: list0 rank3, list1 rank2 -> 1/63 + 1/62               ≈ 0.032002
//	d: list1 rank3 only         -> 1/63                      ≈ 0.015873
//
// b > c > a > d — a and d each contribute a single term, per ADR-0010's own
// wording ("documents appearing in only one list contribute a single
// term"), and neither is dropped from the fused output.
func TestFuse_ReproducesADR0010ByHand(t *testing.T) {
	got := Fuse(
		[]string{"a", "b", "c"},
		[]string{"b", "c", "d"},
	)
	want := []string{"b", "c", "a", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fuse() = %v, want %v", got, want)
	}
}

// TestFuse_BreaksTiesDeterministically pins design D5's three-level
// tie-break (score, then earliest list in argument order, then
// lexicographic) with a fixture engineered to produce two EXACT float ties,
// not a fluke of today's constants.
//
// Tie 1 — x vs y, resolved by the lexicographic level:
// x is list0 rank1 + list1 rank2 = 1/61 + 1/62.
// y is list0 rank2 + list1 rank1 = 1/62 + 1/61.
// Both sums add the exact same two float64 terms in a different order —
// floating-point addition is commutative for two operands, so the two
// totals compare bit-for-bit equal, not merely "close". Both x and y also
// first appear in list0 (list index 0), so the second tie-break level
// (earliest list in argument order) ties too, and only the lexicographic
// level ("x" < "y") separates them.
//
// Tie 2 — z vs w, resolved by the argument-order level:
// z is list0 rank3 only = 1/63 (a single-list contribution).
// w is list1 rank3 only = 1/63 (a single-list contribution) — the same
// value by construction, both being RRFK+3 in the denominator with weight
// 1.0. z's only (and therefore earliest) list is list0 (index 0); w's only
// list is list1 (index 1). The score tie holds, but list0 < list1
// resolves it without reaching the lexicographic level — "w" < "z"
// alphabetically, the opposite of the expected order, so this case would
// fail if the implementation fell through to lexicographic instead of
// stopping at the argument-order level.
func TestFuse_BreaksTiesDeterministically(t *testing.T) {
	got := Fuse(
		[]string{"x", "y", "z"},
		[]string{"y", "x", "w"},
	)
	want := []string{"x", "y", "z", "w"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fuse() = %v, want %v", got, want)
	}
}

// TestFuse_SingleList proves Fuse works over one list — the degenerate
// variadic case ADR-0010's "documents appearing in only one list" wording
// covers when there happens to be only one list at all.
func TestFuse_SingleList(t *testing.T) {
	got := Fuse([]string{"p", "q"})
	want := []string{"p", "q"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fuse() = %v, want %v", got, want)
	}
}

// TestFuse_ThirdListDefaultsToWeightOne proves a list beyond the two Phase B
// ever passes (vector, lexical) still participates in fusion, at weight 1.0
// — the branch fuseWeight's own default case exists for, since ADR-0010's
// formula generalizes to N lists even though only two are named constants
// today.
func TestFuse_ThirdListDefaultsToWeightOne(t *testing.T) {
	got := Fuse(
		[]string{"a"},
		[]string{"b"},
		[]string{"a"},
	)
	// a: list0 rank1 (1/61) + list2 rank1 at the default weight (1/61) = 2/61.
	// b: list1 rank1 only (1/61). a's two contributions outscore b's one.
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fuse() = %v, want %v", got, want)
	}
}

// TestFuseConstants pins the four named constants ADR-0010:48-49 and design
// D5 require — k and each list's relative weight must be named constants in
// exactly one place, not literals repeated at call sites.
func TestFuseConstants(t *testing.T) {
	if RRFK != 60 {
		t.Errorf("RRFK = %v, want 60 (ADR-0010, already listed in doc 02 §13)", RRFK)
	}
	if RecallTopK != 20 {
		t.Errorf("RecallTopK = %v, want 20 (design D5, doc 02 §13 new row)", RecallTopK)
	}
	if WeightVector != 1.0 {
		t.Errorf("WeightVector = %v, want 1.0 (design D5, doc 02 §13 new row)", WeightVector)
	}
	if WeightLexical != 1.0 {
		t.Errorf("WeightLexical = %v, want 1.0 (design D5, doc 02 §13 new row)", WeightLexical)
	}
}
