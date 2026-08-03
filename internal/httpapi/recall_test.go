package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// newTestRecallFixtures returns the raw memrepo/fakeprovider pieces a
// *brain.RecallService is built over — mirroring newTestCaptureService's own
// wiring (capture_test.go), restated here for the same reason: internal/httpapi
// may not import test/conformance (docs/06-harness.md §1's dependency rule
// runs adapter -> brain -> core, never adapter -> test). Returned raw,
// rather than pre-wired into a service, because embeddings.LoadIndex snapshots
// whatever is in the embeddings store at the moment it is called (design D1's
// own "the Index is a snapshot" shape, i22's own test order) — a caller must
// seed units/embeddings first and call buildRecallService after, never before.
func newTestRecallFixtures(t *testing.T) (*memrepo.Units, *memrepo.Embeddings, *memrepo.Lexical, *fakeprovider.Fake) {
	t.Helper()
	return memrepo.NewUnits(), memrepo.NewEmbeddings(), memrepo.NewLexical(), fakeprovider.NewEmbeddingFake(embedFakeModel)
}

// buildRecallService loads embeddings' current snapshot into an Index and
// wires a *brain.RecallService over it — call only after every embedding a
// test needs is already seeded. NewRecallService's own signature takes no
// ports.LLMProvider and no ports.DecisionLog at all — this is what makes "no
// classify call, no decision row" a structural fact for every route built
// over the returned service, not a runtime check (spec R2.4, R2.7).
func buildRecallService(t *testing.T, units *memrepo.Units, embeddings *memrepo.Embeddings, lexical *memrepo.Lexical, embed *fakeprovider.Fake) *brain.RecallService {
	t.Helper()
	idx, err := embeddings.LoadIndex(context.Background(), embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	return brain.NewRecallService(brain.NewIndex(idx), lexical, units, embed)
}

// newTestRecallService is the common case: an empty, ready-to-seed
// RecallService plus its own units/embeddings/lexical fakes, for tests that
// need no vector-leg fixtures (R2.6/R2.7's unit-read tests, which seed only
// units, never embeddings).
func newTestRecallService(t *testing.T) (*brain.RecallService, *memrepo.Units, *memrepo.Embeddings, *memrepo.Lexical) {
	t.Helper()
	units, embeddings, lexical, embed := newTestRecallFixtures(t)
	return buildRecallService(t, units, embeddings, lexical, embed), units, embeddings, lexical
}

func postRecall(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/recall", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestRecallHandler_EmbedsAndReturnsScoredUnits is R2.4's own L2 handler
// test: POST /recall embeds the query via the same RecallService.ForText
// capture already calls (design D9), never classify, and its response
// carries the same units + semantic_leg_available shape an `outcome:
// recalled` capture would (design D10 §5.2). No fakeprovider.LLMProvider is
// wired anywhere reachable from this handler — NewRecallService's own
// signature (index, lex, units, embed) has no ports.LLMProvider parameter —
// so "no LLM completion call occurs" holds structurally: there is no
// Complete method this handler could reach even if it wanted to.
func TestRecallHandler_EmbedsAndReturnsScoredUnits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	units, embeddings, lexical, embed := newTestRecallFixtures(t)

	const query = "how do I fix the leaky faucet"

	vec, err := embed.Embed(ctx, ports.EmbedRequest{Text: query})
	if err != nil {
		t.Fatalf("deriving query's vector: %v", err)
	}
	if err := units.Create(ctx, poolUnitFixture("vector-match", "unrelated stored words")); err != nil {
		t.Fatalf("seeding vector-match: %v", err)
	}
	if err := embeddings.Put(ctx, ports.Embedding{UnitID: "vector-match", Model: embedFakeModel, Vector: vec.Vector}); err != nil {
		t.Fatalf("seeding vector-match's embedding: %v", err)
	}

	svc := buildRecallService(t, units, embeddings, lexical, embed)
	h := Handler(Deps{Version: "test", Recall: svc})

	rec := postRecall(t, h, `{"query": "how do I fix the leaky faucet"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got recallResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v; body = %s", err, rec.Body.String())
	}
	if !got.SemanticLegAvailable {
		t.Errorf("semantic_leg_available = %v, want true — the embedding provider did not fail here", got.SemanticLegAvailable)
	}
	if len(got.Units) == 0 {
		t.Fatal("units is empty — this test would pass vacuously")
	}
	if got.Units[0].ID != "vector-match" {
		t.Errorf("units[0].ID = %q, want %q", got.Units[0].ID, "vector-match")
	}
}

// TestRecallHandler_UnwiredRecallServiceReturns503 applies 13b's own nil-
// dependency pattern (capture.go's d.Capture == nil branch) to d.Recall:
// cmd/nooma/serve.go leaves Recall nil in the identical transitional window
// until 13d's full wiring lands, and an authenticated POST /recall reaching
// that window must not nil-panic the process — it must answer honestly that
// this endpoint is not wired in this build. 503, not 500 (nothing went
// wrong) and not 404 (the route exists).
func TestRecallHandler_UnwiredRecallServiceReturns503(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test"}) // Recall is nil, deliberately.

	rec := postRecall(t, h, `{"query": "irrelevant — Recall is nil, this must never reach it"}`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v; body = %s", err, rec.Body.String())
	}
	if _, ok := got["error"]; !ok {
		t.Errorf("response has no error field: %s", rec.Body.String())
	}
}

// TestRecallHandler_EmptyQueryIs400 mirrors captureHandler's own
// empty-text guard: a caller supplying no query gets a plain 400, not a
// recall over an empty string.
func TestRecallHandler_EmptyQueryIs400(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := newTestRecallService(t)
	h := Handler(Deps{Version: "test", Recall: svc})

	rec := postRecall(t, h, `{"query": ""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// poolUnitFixture is this package's own minimal pool-status unit builder —
// test/conformance's own poolUnit is unreachable here (internal/httpapi may
// not import test/conformance), the same restatement capture_test.go's own
// fixture helpers already make for this package.
func poolUnitFixture(id, content string) unit.Unit {
	return unit.Unit{
		ID:      id,
		Type:    unit.TypeKnowledge,
		Status:  unit.StatusPool,
		Content: content,
		Source:  "chat",
	}
}
