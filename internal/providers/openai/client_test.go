package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// TestClient_CompleteSendsAuthHeaderAndParsesResponse is R6.1: the client
// speaks OpenAI's Chat Completions API (POST /v1/chat/completions, a
// "Bearer <key>" Authorization header) and returns
// ports.LLMResponse{Text, Model} — Text is the vendor's raw message content,
// never parsed (design D7), and Model is what the response says actually
// answered, not what was requested.
func TestClient_CompleteSendsAuthHeaderAndParsesResponse(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"model":"gpt-4o-2024-08-06","choices":[{"index":0,"message":{"role":"assistant","content":"hello from gpt"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "sk-test-key", "gpt-4o", server.Client())

	got, err := client.Complete(context.Background(), ports.LLMRequest{Prompt: "classify this", Task: "classify"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotPath != "/v1/chat/completions" {
		t.Errorf("request path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer sk-test-key")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["model"] != "gpt-4o" {
		t.Errorf("request body model = %v, want the client's configured model", gotBody["model"])
	}
	messages, _ := gotBody["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("request body messages = %v, want exactly one message", gotBody["messages"])
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "user" {
		t.Errorf("request message role = %v, want user", first["role"])
	}
	if first["content"] != "classify this" {
		t.Errorf("request message content = %v, want the exact prompt text (LLMRequest.Prompt)", first["content"])
	}

	if got.Text != "hello from gpt" {
		t.Errorf("Text = %q, want the vendor's raw message content, unparsed", got.Text)
	}
	if got.Model != "gpt-4o-2024-08-06" {
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
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_exceeded","message":"rate limited"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "sk-test-key", "gpt-4o", server.Client())

	_, err := client.Complete(context.Background(), ports.LLMRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("Complete returned a nil error for a 429 vendor response")
	}
}

// TestClient_JSONOnlyAsksForJSONMode is ports.LLMRequest.JSONOnly reaching
// the wire, and its absence staying absent.
//
// The flag says what the CALLER will do with the answer — parse it as JSON
// — not how a vendor should be asked. Each adapter renders that intent in
// its own dialect; this one uses response_format, which is OpenAI's, and a
// request without the flag must not carry the field at all. The absence
// assertion follows embed_test.go's "dimensions" precedent: a knob that
// appears when nobody asked for it is as wrong as one that never appears.
//
// Mutation: send response_format unconditionally and the second subtest
// fails; drop it entirely and the first does.
func TestClient_JSONOnlyAsksForJSONMode(t *testing.T) {
	bodyFor := func(t *testing.T, req ports.LLMRequest) map[string]any {
		t.Helper()

		var gotBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decoding request body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"gpt-4o","choices":[{"message":{"role":"assistant","content":"{}"}}]}`))
		}))
		defer server.Close()

		if _, err := NewClient(server.URL, "sk-test-key", "gpt-4o", server.Client()).
			Complete(context.Background(), req); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		return gotBody
	}

	t.Run("asked for", func(t *testing.T) {
		body := bodyFor(t, ports.LLMRequest{Prompt: "answer with one JSON object", Task: "classify", JSONOnly: true})

		rf, ok := body["response_format"].(map[string]any)
		if !ok {
			t.Fatalf("request body carries no response_format object: %v", body["response_format"])
		}
		if rf["type"] != "json_object" {
			t.Errorf("response_format.type = %v, want %q", rf["type"], "json_object")
		}
	})

	t.Run("not asked for", func(t *testing.T) {
		body := bodyFor(t, ports.LLMRequest{Prompt: "say something", Task: "timer_rephrase"})

		if _, ok := body["response_format"]; ok {
			t.Errorf("request body carries response_format for a caller that never asked for "+
				"JSON — a free-text task forced into JSON mode answers in a shape nothing "+
				"parses: %v", body["response_format"])
		}
	})
}
