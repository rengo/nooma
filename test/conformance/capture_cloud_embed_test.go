// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/providers/openai"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestCapture_CloudEmbedderMakesTheDistinctionObservable is spec R6.3's L2
// half: at least one conformance test asserts that a capture against a
// Cloud-configured vault produces an embedded unit — not merely that
// capture succeeds, which m1b-pipeline's own degradation design already
// guarantees regardless of whether any embedder exists at all.
//
// Deliberately NOT fakeprovider.NewEmbeddingFake, which every other embed
// test in this package already uses (capture_embed_test.go): C9's own gap
// was that a fake embedder shape was always enough to make
// CaptureResult.Embedded true, and nothing distinguished "an embedder
// exists" from "the one Cloud is actually supposed to use exists". This
// test wires the real internal/providers/openai.Client — the same type
// PR 15's wizard binds tasks.embedding to (R6.2) — over a loopback
// httptest.Server, and proves the true/false distinction is observable
// through it specifically. R8.1 proves the same distinction once more
// end to end through the compiled binary (test/e2e).
func TestCapture_CloudEmbedderMakesTheDistinctionObservable(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC)
	// Scripted twice: both subtests below capture the identical text, and
	// fakeprovider consumes one scripted case id per Complete call.
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning", "classify-pick-up-dry-cleaning")

	newService := func(t *testing.T, embed *openai.Client) *brain.CaptureService {
		t.Helper()
		embeddings := memrepo.NewEmbeddings()
		idx, err := embeddings.LoadIndex(ctx, "text-embedding-3-small")
		if err != nil {
			t.Fatalf("embeddings.LoadIndex: %v", err)
		}
		return brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(),
			embeddings, memrepo.NewLexical(), memrepo.NewRelations(), memrepo.NewDecisionLog(),
			llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())
	}

	t.Run("the openai embeddings endpoint answers", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"text-embedding-3-small","data":[{"embedding":[0.1,0.2,0.3]}]}`))
		}))
		t.Cleanup(srv.Close)
		embed := openai.NewClient(srv.URL, "sk-test-key", "text-embedding-3-small", srv.Client())

		result, err := newService(t, embed).Capture(ctx, brain.CaptureInput{
			Text:    "Pick up the dry cleaning on Friday",
			Channel: "chat",
		})
		if err != nil {
			t.Fatalf("Capture error = %v, want nil", err)
		}
		if !result.Embedded {
			t.Error("CaptureResult.Embedded = false, want true — the real openai.Client answered")
		}
	})

	t.Run("the openai embeddings endpoint is unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		srv.Close() // closed before any request: every call against it fails to connect.
		embed := openai.NewClient(srv.URL, "sk-test-key", "text-embedding-3-small", srv.Client())

		result, err := newService(t, embed).Capture(ctx, brain.CaptureInput{
			Text:    "Pick up the dry cleaning on Friday",
			Channel: "chat",
		})
		if err != nil {
			t.Fatalf("Capture error = %v, want nil — an embedding-provider outage must not refuse the capture (design D8)", err)
		}
		if result.Embedded {
			t.Error("CaptureResult.Embedded = true, want false — the openai endpoint was unreachable")
		}
	})
}
