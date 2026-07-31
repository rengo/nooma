package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// var _ ports.EmbeddingProvider = (*Client)(nil) is the compile-time proof
// C4's resolution above requires: the ollama client is the one that
// implements EmbeddingProvider in Phase A (doc 01's tasks.embedding is bound
// to local_llama, whose type is ollama).
var _ ports.EmbeddingProvider = (*Client)(nil)

// TestClient_EmbedSendsRequestAndUnwrapsFirstVector is R6.2: the client
// speaks Ollama's current embedding endpoint, POST /api/embed (not the
// deprecated, singular /api/embeddings), with body {"model", "input"}, and
// unwraps the vendor's array-of-vectors response into the one vector this
// single-text request produced. embeddings is an array of vectors, one per
// input — a single input still comes back as embeddings[0], never as a bare
// vector, and this test pins that unwrap explicitly.
func TestClient_EmbedSendsRequestAndUnwrapsFirstVector(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuth string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body sent by the client: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"nomic-embed-text","embeddings":[[0.01,-0.002,0.3]]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "nomic-embed-text", server.Client())

	got, err := client.Embed(context.Background(), ports.EmbedRequest{Text: "remember this"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if gotPath != "/api/embed" {
		t.Errorf("request path = %q, want /api/embed — the current endpoint, not the deprecated /api/embeddings", gotPath)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty — ollama does not use one, do not invent it", gotAuth)
	}
	if gotBody["model"] != "nomic-embed-text" {
		t.Errorf("request body model = %v, want the client's configured model", gotBody["model"])
	}
	if gotBody["input"] != "remember this" {
		t.Errorf("request body input = %v, want the exact text (EmbedRequest.Text)", gotBody["input"])
	}

	want := []float32{0.01, -0.002, 0.3}
	if len(got.Vector) != len(want) {
		t.Fatalf("Vector = %v, want length %d (the single input's vector, unwrapped from embeddings[0])", got.Vector, len(want))
	}
	for i, v := range want {
		if got.Vector[i] != v {
			t.Errorf("Vector[%d] = %v, want %v", i, got.Vector[i], v)
		}
	}
	if got.Model != "nomic-embed-text" {
		t.Errorf("Model = %q, want the response body's model, what actually answered", got.Model)
	}
}

// TestClient_EmbedFailsWhenEmbeddingsIsEmpty is this project's recurring
// defect family: a provider returning no vector must surface as a Go error,
// never as an EmbedResponse with a nil Vector that a caller would store as a
// valid embedding.
func TestClient_EmbedFailsWhenEmbeddingsIsEmpty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"nomic-embed-text","embeddings":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "nomic-embed-text", server.Client())

	got, err := client.Embed(context.Background(), ports.EmbedRequest{Text: "remember this"})
	if err == nil {
		t.Fatalf("Embed returned a nil error for an empty embeddings array; got %+v, a caller would store this as a valid embedding", got)
	}
}

// TestClient_EmbedSurfacesVendorErrorStatus is R6.2's shape, mirroring
// Complete's: a non-2xx vendor response becomes a Go error, never a
// successful EmbedResponse.
func TestClient_EmbedSurfacesVendorErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"model 'nomic-embed-text' not found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "nomic-embed-text", server.Client())

	_, err := client.Embed(context.Background(), ports.EmbedRequest{Text: "hi"})
	if err == nil {
		t.Fatal("Embed returned a nil error for a 500 vendor response")
	}
}
