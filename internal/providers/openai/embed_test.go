package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// var _ ports.EmbeddingProvider = (*Client)(nil) is design D17's compile-time
// proof: the chat-vs-embedding model difference is a configuration fact (two
// `providers:` entries of type openai, D15), not a type-system one — one
// Client per provider package, one method per port, the same shape
// ollama.Client already carries for both ports.
var _ ports.EmbeddingProvider = (*Client)(nil)

// TestClient_EmbedSendsAuthHeaderAndReturnsEchoedModel is R6.1 / design D17:
// the client speaks OpenAI's POST /v1/embeddings with a "Bearer <key>"
// Authorization header (the same header and the same apiKey field Complete
// uses) and a request body of {"model", "input"}, and returns the RESPONSE's
// model — what actually answered — never the one requested. I21 filters
// vector search on unit_embeddings.model, so storing the requested name when
// the provider silently served a different one would make two incompatible
// vectors compare as if they were the same model.
func TestClient_EmbedSendsAuthHeaderAndReturnsEchoedModel(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuth, gotContentType string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body sent by the client: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"text-embedding-3-small","data":[{"embedding":[0.01,-0.002,0.3]}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "sk-test-key", "text-embedding-3-small-requested", server.Client())

	got, err := client.Embed(context.Background(), ports.EmbedRequest{Text: "remember this"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if gotPath != "/v1/embeddings" {
		t.Errorf("request path = %q, want /v1/embeddings", gotPath)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer sk-test-key")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["model"] != "text-embedding-3-small-requested" {
		t.Errorf("request body model = %v, want the client's configured model", gotBody["model"])
	}
	if gotBody["input"] != "remember this" {
		t.Errorf("request body input = %v, want the exact text (EmbedRequest.Text)", gotBody["input"])
	}
	if _, ok := gotBody["dimensions"]; ok {
		t.Errorf("request body carries a %q field, want it absent — design D17: no Dim field on ports.EmbedResponse, no truncation knob in scope", "dimensions")
	}

	want := []float32{0.01, -0.002, 0.3}
	if len(got.Vector) != len(want) {
		t.Fatalf("Vector = %v, want length %d", got.Vector, len(want))
	}
	for i, v := range want {
		if got.Vector[i] != v {
			t.Errorf("Vector[%d] = %v, want %v", i, got.Vector[i], v)
		}
	}
	if got.Model != "text-embedding-3-small" {
		t.Errorf("Model = %q, want the response body's model (the echoed model), not the requested one", got.Model)
	}
}

// TestClient_EmbedFailsWhenDataIsEmpty is design D17's second copied
// behaviour: an empty data array is a Go error, never a zero-value
// EmbedResponse. A zero vector that flows onward is worse than a failure —
// it scores against everything.
func TestClient_EmbedFailsWhenDataIsEmpty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"text-embedding-3-small","data":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "sk-test-key", "text-embedding-3-small", server.Client())

	got, err := client.Embed(context.Background(), ports.EmbedRequest{Text: "remember this"})
	if err == nil {
		t.Fatalf("Embed returned a nil error for an empty data array; got %+v, a caller would store this as a valid embedding", got)
	}
}

// TestClient_EmbedSurfacesVendorErrorStatus is design D17's third copied
// behaviour: a non-200 status is an error carrying the body — OpenAI's quota
// and model-not-found messages are the useful part, and they arrive in the
// body.
func TestClient_EmbedSurfacesVendorErrorStatus(t *testing.T) {
	t.Parallel()

	const body = `{"error":{"type":"invalid_request_error","message":"model 'nope' not found"}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := NewClient(server.URL, "sk-test-key", "nope", server.Client())

	_, err := client.Embed(context.Background(), ports.EmbedRequest{Text: "hi"})
	if err == nil {
		t.Fatal("Embed returned a nil error for a 400 vendor response")
	}
	if !strings.Contains(err.Error(), body) {
		t.Errorf("error = %q, want it to carry the vendor's response body %q", err.Error(), body)
	}
}
