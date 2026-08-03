// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"errors"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI22_RecallOneMechanismTwoEntrances proves invariant I22
// (docs/02-cognitive-core.md §5 step 2, design D9): capture's own `type:
// recall` entrance (12g) and the standalone `/recall` route (13c) call the
// exact same brain.RecallService.ForText/ScoredFor with the exact same raw
// text, never classify.NormalizedContent (spec R2.5, Q3b).
//
// Neither entrance is wired into production yet (12g, 13c both merge after
// this PR), so this pins the contract at the one place both will call into:
// entranceCapture and entranceRecallRoute below are two call sites of the
// exact shape each future caller will have — one reading CaptureInput.Text,
// one taking a bare query string (/recall never runs classify, spec R2.4)
// — over one shared RecallService and corpus. Deliberately not two unit
// tests that happen to agree: the "diverges" subtest reproduces D9's own
// named failure mode (a call site substituting normalized content for raw
// text) and shows this test catches it rather than passing vacuously.
func TestI22_RecallOneMechanismTwoEntrances(t *testing.T) {
	ctx := context.Background()

	const rawText = "how do I fix the leaky faucet"
	// normalizedText stands in for what classify.NormalizedContent would
	// carry for the same capture — deliberately sharing no token with
	// rawText, so a call site that searches this instead of rawText is
	// caught by both legs, not just one.
	const normalizedText = "please repair kitchen tap"

	units := memrepo.NewUnits()
	lexical := memrepo.NewLexical()
	embeddings := memrepo.NewEmbeddings()
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	// vector-match is findable only via the vector leg, for rawText
	// specifically — its stored embedding is rawText's own deterministic
	// vector, and it is never lexically seeded.
	vec, err := embed.Embed(ctx, ports.EmbedRequest{Text: rawText})
	if err != nil {
		t.Fatalf("deriving rawText's vector: %v", err)
	}
	if err := units.Create(ctx, poolUnit("vector-match", "unrelated stored words")); err != nil {
		t.Fatalf("seeding vector-match: %v", err)
	}
	if err := embeddings.Put(ctx, ports.Embedding{UnitID: "vector-match", Model: embedFakeModel, Vector: vec.Vector}); err != nil {
		t.Fatalf("seeding vector-match's embedding: %v", err)
	}

	// lexicalMatch is findable only via the lexical leg, seeded under
	// rawText's own tokens and embedded under nothing the vector leg could
	// ever return.
	if err := units.Create(ctx, poolUnit("lexical-match", rawText)); err != nil {
		t.Fatalf("seeding lexical-match: %v", err)
	}
	lexical.SeedLexical(t, "lexical-match", rawText)

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewRecallService(brain.NewIndex(idx), lexical, units, embed)

	// entranceCapture mirrors 12g's own future capture-time recall fork:
	// it holds a CaptureInput, and design D9 forces its raw Text field into
	// ForText, never NormalizedContent.
	entranceCapture := func(in brain.CaptureInput) ([]unit.Unit, bool, error) {
		return svc.ForText(ctx, in.Text)
	}
	// entranceRecallRoute mirrors 13c's own future /recall handler: a bare
	// query string, since /recall never runs classify (spec R2.4).
	entranceRecallRoute := func(query string) ([]unit.Unit, bool, error) {
		return svc.ForText(ctx, query)
	}

	gotCapture, semCapture, err := entranceCapture(brain.CaptureInput{Text: rawText, Channel: "chat"})
	if err != nil {
		t.Fatalf("entranceCapture: %v", err)
	}
	gotRoute, semRoute, err := entranceRecallRoute(rawText)
	if err != nil {
		t.Fatalf("entranceRecallRoute: %v", err)
	}

	if len(gotCapture) == 0 {
		t.Fatal("entranceCapture returned nothing — this test would pass vacuously")
	}
	if !semCapture || !semRoute {
		t.Errorf("semantic_leg_available = (%v, %v), want (true, true) — the embedding provider did not fail here", semCapture, semRoute)
	}
	assertSameUnitOrder(t, gotCapture, gotRoute)

	t.Run("an entrance that substitutes normalized content for raw text diverges", func(t *testing.T) {
		// entranceCaptureBuggy reproduces D9's own named mistake: it holds
		// the same CaptureInput but searches its own NormalizedContent
		// stand-in instead of Text.
		entranceCaptureBuggy := func(in brain.CaptureInput, normalized string) ([]unit.Unit, bool, error) {
			return svc.ForText(ctx, normalized)
		}
		gotBuggy, _, err := entranceCaptureBuggy(brain.CaptureInput{Text: rawText, Channel: "chat"}, normalizedText)
		if err != nil {
			t.Fatalf("entranceCaptureBuggy: %v", err)
		}
		if sameUnitOrder(gotBuggy, gotRoute) {
			t.Fatal("a capture-entrance searching normalized content instead of raw text agreed with /recall anyway — this corpus does not exercise D9's divergence; strengthen it")
		}
	})

	t.Run("degrades identically when the embedding leg fails", func(t *testing.T) {
		failingEmbed := fakeprovider.NewEmbeddingFakeWithError(embedFakeModel, errors.New("embedding provider unreachable"))
		degraded := brain.NewRecallService(brain.NewIndex(idx), lexical, units, failingEmbed)

		gotA, okA, err := degraded.ForText(ctx, rawText) // capture's entrance shape
		if err != nil {
			t.Fatalf("degraded ForText (capture shape): %v", err)
		}
		gotB, okB, err := degraded.ForText(ctx, rawText) // /recall's entrance shape
		if err != nil {
			t.Fatalf("degraded ForText (recall-route shape): %v", err)
		}
		if okA || okB {
			t.Errorf("semantic_leg_available = (%v, %v), want (false, false) — an embedding failure must degrade to the lexical leg alone (design D9)", okA, okB)
		}
		if len(gotA) == 0 {
			t.Fatal("the degraded lexical-only leg found nothing — vacuous")
		}
		assertSameUnitOrder(t, gotA, gotB)
	})
}

// sameUnitOrder reports whether a and b name the same units in the same
// order.
func sameUnitOrder(a, b []unit.Unit) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

// assertSameUnitOrder fails t unless a and b name the same units in order.
func assertSameUnitOrder(t *testing.T, a, b []unit.Unit) {
	t.Helper()
	if !sameUnitOrder(a, b) {
		ids := func(us []unit.Unit) []string {
			out := make([]string, len(us))
			for i, u := range us {
				out[i] = u.ID
			}
			return out
		}
		t.Errorf("got %v, want the same ordered ids as %v", ids(a), ids(b))
	}
}
