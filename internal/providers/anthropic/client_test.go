package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// TestClient_CompleteSendsAuthHeaderAndParsesResponse is R6.1: the client
// speaks Anthropic's Messages API (POST /v1/messages, x-api-key +
// anthropic-version headers) and returns ports.LLMResponse{Text, Model} —
// Text is the vendor's raw text, never parsed (design D7), and Model is what
// the response says actually answered, not what was requested.
func TestClient_CompleteSendsAuthHeaderAndParsesResponse(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey, gotVersion, gotContentType string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body sent by the client: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"claude-sonnet-4-5-20250929","content":[{"type":"text","text":"hello from claude"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "sk-ant-test-key", "claude-sonnet-4-5", server.Client())

	got, err := client.Complete(context.Background(), ports.LLMRequest{Prompt: "classify this", Task: "classify"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotPath != "/v1/messages" {
		t.Errorf("request path = %q, want /v1/messages", gotPath)
	}
	if gotAPIKey != "sk-ant-test-key" {
		t.Errorf("x-api-key header = %q, want the configured key", gotAPIKey)
	}
	if gotVersion == "" {
		t.Error("anthropic-version header is empty; the Messages API requires it")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["model"] != "claude-sonnet-4-5" {
		t.Errorf("request body model = %v, want the client's configured model", gotBody["model"])
	}
	messages, _ := gotBody["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("request body messages = %v, want exactly one message", gotBody["messages"])
	}
	first, _ := messages[0].(map[string]any)
	if first["content"] != "classify this" {
		t.Errorf("request message content = %v, want the exact prompt text (LLMRequest.Prompt)", first["content"])
	}

	if got.Text != "hello from claude" {
		t.Errorf("Text = %q, want the vendor's raw text, unparsed", got.Text)
	}
	if got.Model != "claude-sonnet-4-5-20250929" {
		t.Errorf("Model = %q, want what the response says actually answered, not the requested name", got.Model)
	}
}

// TestClient_CompleteSurfacesVendorErrorStatus is R6.1's shape: a non-2xx
// vendor response becomes a Go error, never a successful LLMResponse
// carrying the error text as if it were a completion.
func TestClient_CompleteSurfacesVendorErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "sk-ant-test-key", "claude-sonnet-4-5", server.Client())

	_, err := client.Complete(context.Background(), ports.LLMRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("Complete returned a nil error for a 429 vendor response")
	}
}
