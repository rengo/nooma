package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// TestClient_CompleteSendsStreamFalseAndParsesResponse is R6.1: the client
// speaks Ollama's generate API (POST /api/generate) with "stream": false
// sent explicitly — Ollama defaults stream to true, and an omitted stream
// field would return a streamed body instead of a single JSON object,
// producing a decode error at runtime that a response-only test would never
// catch. The client returns ports.LLMResponse{Text, Model} — Text is the
// vendor's raw "response" field, never parsed (design D7), and Model is what
// the response body says actually answered, not what was requested.
func TestClient_CompleteSendsStreamFalseAndParsesResponse(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"model":"llama3.1:8b","created_at":"2026-07-31T00:00:00Z","response":"hello from llama","done":true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "llama3.1", server.Client())

	got, err := client.Complete(context.Background(), ports.LLMRequest{Prompt: "classify this", Task: "classify"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotPath != "/api/generate" {
		t.Errorf("request path = %q, want /api/generate", gotPath)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty — ollama does not use one, do not invent it", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["model"] != "llama3.1" {
		t.Errorf("request body model = %v, want the client's configured model", gotBody["model"])
	}
	if gotBody["prompt"] != "classify this" {
		t.Errorf("request body prompt = %v, want the exact prompt text (LLMRequest.Prompt)", gotBody["prompt"])
	}
	streamVal, streamPresent := gotBody["stream"]
	if !streamPresent {
		t.Fatal("request body has no \"stream\" field — ollama defaults stream to true when it is omitted, which returns a streamed body instead of a single JSON object")
	}
	if streamVal != false {
		t.Errorf("request body stream = %v, want false — omitting or setting it true would stream the response instead of returning a single JSON object", streamVal)
	}

	if got.Text != "hello from llama" {
		t.Errorf("Text = %q, want the vendor's raw \"response\" field, unparsed", got.Text)
	}
	if got.Model != "llama3.1:8b" {
		t.Errorf("Model = %q, want what the response says actually answered, not the requested name", got.Model)
	}
}

// TestClient_CompleteSurfacesVendorErrorStatus is R6.1's shape: a non-2xx
// vendor response becomes a Go error, never a successful LLMResponse
// carrying the error text as if it were a completion.
func TestClient_CompleteSurfacesVendorErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"model 'llama3.1' not found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "llama3.1", server.Client())

	_, err := client.Complete(context.Background(), ports.LLMRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("Complete returned a nil error for a 500 vendor response")
	}
}
