//go:build pendingimpl

// Package conformance — see test/conformance/doc.go for the package contract.
//
// This file is tagged pendingimpl (design.md §8) and is never compiled by
// the untagged build. It is compiled in isolation by `make pending-red`
// (scripts/pending-red.sh), whose job is to confirm this package FAILS to
// compile, and fails for the right reason.
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
// spec R6.2/R6.4, design.md §8.4. Requires docs/02-cognitive-core.md §5's
// "one model per search" wording to already exist — PR 1 of this change —
// which this test's own doc comment references, per tasks.md 5.3. Neither
// symbol exists yet — see internal/core/recall/doc.go and
// test/conformance/pending_symbols.txt. In this chain the RED is a compile
// error naming both symbols (`undefined: recall.VectorQuery` and/or
// `undefined: recall.VectorIndex`) — that IS the passing state of
// scripts/pending-red.sh (design §8.1/§8.2, D9), not a defect to fix. This
// test never turns green inside this change.
//
// Promotion: the PR that adds recall.VectorQuery and recall.VectorIndex
// must, in the SAME PR, drop the pendingimpl tag from this file, move it
// into the untagged L2 suite, and remove both lines from
// pending_symbols.txt (design §8.3/§8.5, spec R7.3). Promoting this test is
// necessary but not sufficient (spec R6.2): it proves the invariant is
// expressible, not enforced, so that same PR still needs its own,
// non-pending test for the actual filtering behaviour before I21 can be
// considered closed.
//
// Honest limitation (design §8.4, stated so a future reader does not
// over-trust this test): reflection proves the invariant is *expressible* —
// that a query carries a model and an index is keyed by one — not that
// every call site actually honours it. The behavioural half (that a search
// against a model-A index rejects or ignores a model-B query) arrives with
// M1's real implementation and its own, non-pending test.
//
// Assumed shape, to be adjusted at promotion time if M0/M1's real API
// differs: VectorQuery carries an exported string-kind Model field the
// caller sets to select which model's embeddings to search; VectorIndex is
// itself scoped to one model via its own exported string-kind Model field
// (the "vault can hold two models at once" case is then two VectorIndex
// values, one per model, never one index serving both).
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
