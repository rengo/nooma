// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"reflect"
	"testing"

	"github.com/rengo/nooma/internal/core/recall"
)

// TestI21_VectorSearchFiltersOnModel proves invariant I21
// (docs/02-cognitive-core.md §5, "One model per search"): vector similarity
// is only defined between embeddings produced by the same model. A vault
// can hold two models at once while a reindex is in progress, so every
// vector search filters by model, and vectors from two models are never
// compared or fused.
//
// Anchor: recall.VectorQuery / recall.VectorIndex (internal/core/recall),
// spec R6.2/R6.4, design.md §8.4.
//
// Promoted: this file was `//go:build pendingimpl` until the PR that added
// recall.VectorQuery and recall.VectorIndex (m1b-pipeline PR 8a) dropped
// the tag, moved this test into the untagged L2 suite, and removed both
// lines from pending_symbols.txt in the same PR (spec R2.8, design D10).
// Promoting this test alone was not sufficient (spec R2.3): it proves the
// invariant is *expressible*, not enforced — that same PR's own
// vector_test.go (internal/core/recall) carries the non-pending test for
// the actual filtering behaviour, TestSearch's "model mismatch refuses
// (I21)" case.
//
// Honest limitation (design §8.4, stated so a future reader does not
// over-trust this test): reflection proves the invariant is *expressible* —
// that a query carries a model and an index is keyed by one — not that
// every call site actually honours it. The behavioural half (that a search
// against a model-A index never surfaces a model-B entry) is
// recall.TestSearch's own job now, not this file's.
//
// Shape, as it actually shipped: VectorQuery carries an exported
// string-kind Model field the caller sets to select which model's
// embeddings to search; VectorIndex is itself scoped to one model via its
// own exported string-kind Model field — a vault holding two models holds
// two VectorIndex values, one per model, never one index serving both.
func TestI21_VectorSearchFiltersOnModel(t *testing.T) {
	t.Run("VectorQuery carries a Model", func(t *testing.T) {
		queryType := reflect.TypeOf(recall.VectorQuery{})

		field, ok := queryType.FieldByName("Model")
		if !ok {
			t.Fatal(
				"recall.VectorQuery has no exported Model field — every vector " +
					"search must filter on model (docs/02-cognitive-core.md §5, " +
					"'one model per search')",
			)
		}
		if field.Type.Kind() != reflect.String {
			t.Errorf(
				"recall.VectorQuery.Model has kind %s, want a string-kind model "+
					"identifier, so a query cannot silently carry a numeric or "+
					"struct value that two different models could coincide on",
				field.Type.Kind(),
			)
		}
	})

	t.Run("VectorIndex is keyed by model", func(t *testing.T) {
		indexType := reflect.TypeOf(recall.VectorIndex{})

		field, ok := indexType.FieldByName("Model")
		if !ok {
			t.Fatal(
				"recall.VectorIndex has no exported Model field — an index not " +
					"keyed by model could serve embeddings from two different " +
					"models to the same search (docs/02-cognitive-core.md §5)",
			)
		}
		if field.Type.Kind() != reflect.String {
			t.Errorf(
				"recall.VectorIndex.Model has kind %s, want a string-kind model identifier",
				field.Type.Kind(),
			)
		}
	})
}
