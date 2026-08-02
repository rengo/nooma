// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI02_RecallExcludesSupersededAndIncomplete is spec R7.1's assignment of
// I02 to this pipeline, and design D5's own claim that the LiveByIDs
// boundary is where I02 holds "for the whole mechanism", not merely inside
// sqlite.UnitRepo (already proven at L3 by
// internal/store/sqlite/search_integration_test.go's
// TestSearch_ReturnsOnlyPoolUnits — a different half of the same invariant).
//
// Neither recall leg's in-memory fake — memrepo.Embeddings/brain.Index for
// the vector leg, memrepo.Lexical for the lexical one — has any notion of
// unit.Status at all, unlike the real FTS5 leg's "belt-and-braces" SQL
// predicate (design D5). That is deliberate here: it means the only way
// this test can pass is if internal/brain itself reaches
// ports.UnitRepo.LiveByIDs before returning a candidate list, which is
// exactly the boundary this test exists to prove — not a coincidence of
// weak fakes.
func TestI02_RecallExcludesSupersededAndIncomplete(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()

	const newContent = "Pick up the dry cleaning"

	setupEmbed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	matchVector, err := setupEmbed.Embed(ctx, ports.EmbedRequest{Text: newContent})
	if err != nil {
		t.Fatalf("deriving the match vector: %v", err)
	}

	// Both non-live units are findable by BOTH legs — an exact vector match
	// with what the new capture's own content will embed to, and full
	// lexical overlap with it — so if the fused output ever surfaced
	// either, this test would catch it regardless of which leg would have
	// carried it.
	seedNonLive := func(id string, status unit.Status) {
		t.Helper()
		if err := units.Create(ctx, unit.Unit{
			ID:      id,
			Type:    unit.TypeTask,
			Status:  status,
			Content: newContent,
			Source:  "chat",
		}); err != nil {
			t.Fatalf("seeding %s unit %q: %v", status, id, err)
		}
		if err := embeddings.Put(ctx, ports.Embedding{UnitID: id, Model: embedFakeModel, Vector: matchVector.Vector, At: now}); err != nil {
			t.Fatalf("seeding %s unit %q's embedding: %v", status, id, err)
		}
		lexical.SeedLexical(t, id, newContent)
	}
	seedNonLive("superseded-1", unit.StatusSuperseded)
	seedNonLive("incomplete-1", unit.StatusIncomplete)

	// Prove both legs, unfiltered, actually carry these ids — otherwise
	// their absence from the pipeline's own output would prove nothing
	// about I02; it might just mean they never matched.
	rawVectorHits, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	for _, id := range []string{"superseded-1", "incomplete-1"} {
		if !slicesContain(rawVectorHits.IDs, id) {
			t.Fatalf("the seeded vector index %v does not carry %q — this test's own setup is broken, not the pipeline under test", rawVectorHits.IDs, id)
		}
	}
	rawLexicalHits, err := lexical.SearchLexical(ctx, recall.Tokenize(newContent), recall.RecallTopK)
	if err != nil {
		t.Fatalf("lexical.SearchLexical: %v", err)
	}
	for _, id := range []string{"superseded-1", "incomplete-1"} {
		if !slicesContain(rawLexicalHits, id) {
			t.Fatalf("the seeded lexical fake %v does not carry %q — this test's own setup is broken, not the pipeline under test", rawLexicalHits, id)
		}
	}

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}

	relations := memrepo.NewRelations()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning")
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx))

	result, err := svc.Capture(ctx, brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil", err)
	}

	for _, id := range result.Candidates {
		if id == "superseded-1" || id == "incomplete-1" {
			t.Fatalf("Candidates = %v, contains a non-live unit %q — I02 must hold inside the brain pipeline, not only inside sqlite.UnitRepo", result.Candidates, id)
		}
	}
}

func slicesContain(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
