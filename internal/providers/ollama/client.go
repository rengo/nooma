// Package ollama implements ports.LLMProvider over Ollama's generate API
// (POST /api/generate) — design D7, spec R6.1.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rengo/nooma/internal/ports"
)

// defaultBaseURL is Ollama's own default local listener.
const defaultBaseURL = "http://localhost:11434"

// Client is Ollama's ports.LLMProvider implementation. It carries no API
// key: Ollama is typically an unauthenticated local endpoint, and this
// client does not invent an auth header it does not use.
type Client struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

var _ ports.LLMProvider = (*Client)(nil)

// NewClient returns a Client that requests model. baseURL is overridable so
// tests point at an httptest.Server instead of the real local listener — an
// in-process loopback listener, not "the network" in docs/06-harness.md
// §3's sense (design §6). Passing "" for baseURL uses Ollama's default local
// endpoint; passing nil for httpClient uses http.DefaultClient.
func NewClient(baseURL, model string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, model: model, httpClient: httpClient}
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	// Stream must be sent explicitly false. Ollama defaults it to true, and
	// an omitted field returns a streamed body instead of a single JSON
	// object — a decode error at runtime, not a request-shape one, which is
	// exactly why this field has no `omitempty`.
	Stream bool `json:"stream"`
	// Format is Ollama's own constrained-output field, and it is a
	// TOP-LEVEL string — not OpenAI's nested response_format envelope.
	// Ollama drops unknown top-level keys silently, so sending the OpenAI
	// shape here would produce a request that looks constrained and is not:
	// the one outcome worse than sending nothing.
	Format string `json:"format,omitempty"`
}

type generateResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// Complete sends req.Prompt to Ollama's /api/generate and returns the
// vendor's raw text — never parsed into a classification (design D7; I14's
// degradation rule is core/classify's, Phase B's).
func (c *Client) Complete(ctx context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	request := generateRequest{
		Model:  c.model,
		Prompt: req.Prompt,
		Stream: false,
	}
	if req.JSONOnly {
		request.Format = "json"
	}

	body, err := json.Marshal(request)
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("ollama: encoding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("ollama: building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("ollama: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("ollama: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ports.LLMResponse{}, fmt.Errorf("ollama: request failed with status %d: %s", resp.StatusCode, respBody)
	}

	var parsed generateResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return ports.LLMResponse{}, fmt.Errorf("ollama: decoding response: %w", err)
	}

	return ports.LLMResponse{Text: parsed.Response, Model: parsed.Model}, nil
}
