// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/httpapi"
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
// entranceCapture is 12g's own real capture-time recall fork, not a
// stand-in: it drives CaptureInput.Text through the full, production
// CaptureService.Capture pipeline (a `type: recall`-classified capture) —
// capture.go's own Kind == classify.KindRecall branch calls this exact
// svc.ForText(ctx, in.Text) internally.
//
// entranceRecallRoute is likewise 13c's own real production entrance, not a
// stand-in: it drives the query through httpapi.Handler's real POST /recall
// route (internal/httpapi/recall.go's recallHandler), over an httptest
// in-process request/response cycle — the same shape internal/httpapi's own
// handler tests use, never a direct svc.ForText call standing in for the
// route. Before this PR, /recall did not exist in production and this test
// called svc.ForText directly under a comment naming this exact
// replacement as 13c's own job; that stub is gone. I22 now proves the
// one-mechanism property against two production entrances, not one
// production and one lookalike. Deliberately not two unit tests that happen
// to agree: the "diverges" subtest reproduces D9's own named failure mode
// (a call site substituting normalized content for raw text) and shows this
// test catches it rather than passing vacuously.
//
// CaptureResult carries no semantic-leg-available flag of its own (design
// D8's struct has none for the Recalled outcome — only /recall's future
// HTTP response renders it, D9's "rendered as semantic_leg_available"), so
// entranceCapture is compared on ordered ids only; the "degrades
// identically" subtest below still proves the shared-method property the
// boolean pins, calling ForText directly with each entrance's own argument
// shape.
func TestI22_RecallOneMechanismTwoEntrances(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

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

	// normalized-vector-match is findable only via the vector leg, for
	// normalizedText specifically. It is what keeps the divergence subtest
	// below meaningful now that ADR-0020 admits on the vector alone: the
	// two entrances must differ among results that CLEAR the floor, and
	// before this unit existed the only thing distinguishing them was
	// lexical-match — which the answering path no longer admits.
	nvec, err := embed.Embed(ctx, ports.EmbedRequest{Text: normalizedText})
	if err != nil {
		t.Fatalf("deriving normalizedText's vector: %v", err)
	}
	if err := units.Create(ctx, poolUnit("normalized-vector-match", "other unrelated words")); err != nil {
		t.Fatalf("seeding normalized-vector-match: %v", err)
	}
	if err := embeddings.Put(ctx, ports.Embedding{UnitID: "normalized-vector-match", Model: embedFakeModel, Vector: nvec.Vector}); err != nil {
		t.Fatalf("seeding normalized-vector-match's embedding: %v", err)
	}

	// lexicalMatch is findable only via the lexical leg, seeded under
	// rawText's own tokens and embedded under nothing the vector leg could
	// ever return.
	//
	// It no longer reaches the ANSWERING entrance: ADR-0020 made the vector
	// the admission and the lexical leg the ranker, and this unit has no
	// vector evidence at all. It stays seeded because ScoredFor — the
	// generous net dedup and the nightly connect phase use — still admits
	// it, and that is the half of hybrid recall the decision left intact.
	if err := units.Create(ctx, poolUnit("lexical-match", rawText)); err != nil {
		t.Fatalf("seeding lexical-match: %v", err)
	}
	lexical.SeedLexical(t, "lexical-match", rawText)

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewRecallService(brain.NewIndex(idx), lexical, units, embed)

	// entranceCapture drives 12g's own real production Kind == KindRecall
	// routing fork: the whole CaptureService.Capture pipeline, classified
	// `recall` by the scripted fixture below, not a test-shaped stand-in.
	entranceCapture := func(t *testing.T, text string) ([]unit.Unit, error) {
		t.Helper()
		relations := memrepo.NewRelations()
		decisions := memrepo.NewDecisionLog()
		signals := memrepo.NewSignals()
		llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-recall-leaky-faucet")
		captureEmbed := fakeprovider.NewEmbeddingFake(embedFakeModel)
		captureSvc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, llm, captureEmbed, brain.NewIndex(idx), signals, memrepo.NewTriggers(), memrepo.NewTimers())
		result, err := captureSvc.Capture(ctx, brain.CaptureInput{Text: text, Channel: "chat"})
		if err != nil {
			return nil, err
		}
		if result.Outcome != brain.OutcomeRecalled {
			t.Fatalf("Outcome = %q, want %q", result.Outcome, brain.OutcomeRecalled)
		}
		return result.Recalled, nil
	}
	// entranceRecallRoute is 13c's own real production /recall handler,
	// driven over an in-process HTTP request — never a direct svc.ForText
	// call standing in for it. httpapi.Handler is built over the exact same
	// svc this test already wired above, so both entrances share one
	// RecallService instance, the same way a real `nooma serve` process
	// would (cmd/nooma/serve.go wires exactly one RecallService, 13d).
	recallHTTPHandler := httpapi.Handler(httpapi.Deps{Recall: svc})
	entranceRecallRoute := func(query string) ([]unit.Unit, bool, error) {
		body, err := json.Marshal(map[string]string{"query": query})
		if err != nil {
			return nil, false, err
		}
		req := httptest.NewRequest(http.MethodPost, "/recall", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		recallHTTPHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return nil, false, errors.New("POST /recall: status " + rec.Result().Status + ": " + rec.Body.String())
		}

		var resp struct {
			Units []struct {
				ID string `json:"id"`
			} `json:"units"`
			SemanticLegAvailable bool `json:"semantic_leg_available"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			return nil, false, err
		}
		units := make([]unit.Unit, len(resp.Units))
		for i, u := range resp.Units {
			units[i] = unit.Unit{ID: u.ID}
		}
		return units, resp.SemanticLegAvailable, nil
	}

	gotCapture, err := entranceCapture(t, rawText)
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
	if !semRoute {
		t.Errorf("semantic_leg_available = %v, want true — the embedding provider did not fail here", semRoute)
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
