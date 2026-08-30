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

// TestCapture_RunsHybridRecallForCandidates is spec R4.4's own scenario
// (design D4, D5): capture runs the one RRF-fused mechanism core/recall
// already proves at L1 (TestRecallCorpusFusesToItsExpectedRanking) rather
// than a second implementation of it, and the new unit never appears in its
// own candidate list.
//
// Three pre-seeded units control the two legs independently, so the fused
// order is not a number anyone had to guess:
//   - "cand-vector" carries the exact vector the new capture's own content
//     will embed to (fakeprovider's embedder is a pure function of text —
//     same text, same vector) and is seeded into no lexical index at all, so
//     it can only ever surface via the vector leg.
//   - "cand-lexical" is seeded only into the lexical fake, with the new
//     capture's own words, so it can only ever surface via the lexical leg.
//   - "cand-noise" is seeded into neither leg, so it must never appear.
//
// Both real candidates rank first in their own leg, at RRF's rank 1, with
// equal weight (WeightVector == WeightLexical == 1.0) — an exact score tie
// design D5's own tie-break rule resolves by earliest list in argument
// order, vector before lexical. Rather than asserting that order by
// assertion-author arithmetic (the C12 mistake this suite already learned
// from once), this test asks recall.Fuse itself, over the same two
// single-candidate lists, what the answer is.
//
// PR 11c wires the relation judge behind a non-empty candidate list (design
// D4's diagram tail), so this capture now makes a second Complete call —
// llm is scripted with a second case id, "relation-no-match-for-dry-
// cleaning" (outcome "new"), so the judge's own verdict never disturbs this
// test's own assertions, which are about Candidates, not about what the
// judge decided to do with them (that is capture_relation_judge_test.go's
// job).
func TestCapture_RunsHybridRecallForCandidates(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()

	const newContent = "Pick up the dry cleaning"

	// The new capture's own content will normalize to newContent (the
	// "classify-pick-up-dry-cleaning" fixture, already exercised by
	// TestCapture_OrdinaryClassificationPersistsAUnit) — a separate Fake
	// instance derives the identical deterministic vector for it, without
	// touching the EmbedCalls() counter on the instance the pipeline itself
	// uses.
	setupEmbed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	matchVector, err := setupEmbed.Embed(ctx, ports.EmbedRequest{Text: newContent})
	if err != nil {
		t.Fatalf("deriving the match vector: %v", err)
	}

	if err := units.Create(ctx, poolUnit("cand-vector", "unrelated stored text")); err != nil {
		t.Fatalf("seeding cand-vector: %v", err)
	}
	if err := embeddings.Put(ctx, ports.Embedding{UnitID: "cand-vector", Model: embedFakeModel, Vector: matchVector.Vector, At: now}); err != nil {
		t.Fatalf("seeding cand-vector's embedding: %v", err)
	}

	if err := units.Create(ctx, poolUnit("cand-lexical", newContent)); err != nil {
		t.Fatalf("seeding cand-lexical: %v", err)
	}
	lexical.SeedLexical(t, "cand-lexical", newContent)

	if err := units.Create(ctx, poolUnit("cand-noise", "water the office plants")); err != nil {
		t.Fatalf("seeding cand-noise: %v", err)
	}

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}

	relations := memrepo.NewRelations()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning", "relation-no-match-for-dry-cleaning")
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5)

	result, err := svc.Capture(ctx, brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil", err)
	}

	for _, id := range result.Candidates {
		if id == result.UnitID {
			t.Fatalf("Candidates = %v, contains the just-persisted unit %q — spec R4.4's own MUST is that a unit never candidates itself", result.Candidates, result.UnitID)
		}
		if id == "cand-noise" {
			t.Fatalf("Candidates = %v, contains %q, which neither recall leg was ever seeded to find", result.Candidates, id)
		}
	}

	want := recall.Fuse([]string{"cand-vector"}, []string{"cand-lexical"})
	if len(result.Candidates) != len(want) {
		t.Fatalf("Candidates = %v, want %v (recall.Fuse's own tie-break over the two real single-candidate legs)", result.Candidates, want)
	}
	for i := range want {
		if result.Candidates[i] != want[i] {
			t.Errorf("Candidates[%d] = %q, want %q\n got: %v\nwant: %v", i, result.Candidates[i], want[i], result.Candidates, want)
		}
	}
}

// TestCapture_EmptyVaultMakesNoExtraLLMCalls is design D4's own stated
// property (task 10b.5, closed by task 11c.5): a capture into an otherwise-
// empty vault produces an empty candidate list, and the judge is never
// called over an empty candidate list. llm is scripted with exactly one
// case id, and fakeprovider.Fake.Complete fails the test immediately on any
// call beyond the script (fakeprovider.go:66-69) — the implicit half of this
// property. len(llm.SeenPrompts()) != 1 below is task 11c.5's own explicit
// half: a direct, numeric assertion that no Complete call happened beyond
// capture_processing, not merely the absence of a t.Fatalf.
func TestCapture_EmptyVaultMakesNoExtraLLMCalls(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()
	relations := memrepo.NewRelations()

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}

	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning")
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5)

	result, err := svc.Capture(ctx, brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil", err)
	}
	if got := len(llm.SeenPrompts()); got != 1 {
		t.Fatalf("llm.SeenPrompts() has %d entries, want exactly 1 (capture_processing only) — the relation judge must never be called over an empty candidate list (design D4, task 11c.5)", got)
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("Candidates = %v, want empty — nothing else was ever captured into this vault", result.Candidates)
	}
}

// poolUnit builds a minimal, live unit for direct repo seeding — this
// file's tests only need Status, Content and a stable ID to control what
// each recall leg can find; every other field is the zero value.
func poolUnit(id, content string) unit.Unit {
	return unit.Unit{
		ID:      id,
		Type:    unit.TypeKnowledge,
		Status:  unit.StatusPool,
		Content: content,
		Source:  "chat",
	}
}
