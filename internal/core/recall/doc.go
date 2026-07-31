// Package recall selects top-K vector matches by dot product and fuses
// vector and lexical result lists (RRF) — docs/02-cognitive-core.md §5
// step 2, ADR-0010, ADR-0012.
//
// See docs/06-harness.md §1 for the dependency rule.
//
// VectorQuery and VectorIndex (vector.go) were I21's conformance anchor
// (test/conformance/i21_vector_search_filters_on_model_test.go) until
// m1b-pipeline PR 8a added them and promoted that test into the untagged
// L2 suite in the same PR (design D10, spec R2.8).
package recall
