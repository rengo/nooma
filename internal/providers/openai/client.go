// Package openai implements ports.LLMProvider over OpenAI's Chat
// Completions API (POST /v1/chat/completions) — design D7, spec R6.1.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rengo/nooma/internal/ports"
)

const defaultBaseURL = "https://api.openai.com"

// Client is OpenAI's ports.LLMProvider implementation.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

var _ ports.LLMProvider = (*Client)(nil)

// NewClient returns a Client that authenticates with apiKey and requests
// model. baseURL is overridable so tests point at an httptest.Server instead
// of api.openai.com — an in-process loopback listener, not "the network" in
// docs/06-harness.md §3's sense (design §6). Passing "" for baseURL uses the
// real OpenAI endpoint; passing nil for httpClient uses http.DefaultClient.
func NewClient(baseURL, apiKey, model string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, apiKey: apiKey, model: model, httpClient: httpClient}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	// ResponseFormat is omitted entirely unless the caller asked for JSON.
	// A pointer rather than a value with omitempty, because an empty struct
	// is not empty to encoding/json and would serialise as
	// "response_format":{"type":""} — a field the vendor would reject on a
	// request that never wanted it.
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

// responseFormat is OpenAI's JSON-mode envelope. json_object rather than
// json_schema: a schema would have to declare every property as required
// (strict mode admits no optional fields), which turns eleven of
// classify's optional absences into present-nulls the decoder degrades —
// and classify's structured_data accepts any JSON value by contract, a
// shape strict mode cannot express at all.
type responseFormat struct {
	Type string `json:"type"`
}

// jsonObjectFormat is the one value this adapter sends.
const jsonObjectFormat = "json_object"

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
}

type choice struct {
	Message chatMessage `json:"message"`
}

// Complete sends req.Prompt as a single user message and returns the
// vendor's raw message content — never parsed into a classification
// (design D7; I14's degradation rule is core/classify's, Phase B's).
func (c *Client) Complete(ctx context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	request := chatRequest{
		Model:    c.model,
		Messages: []chatMessage{{Role: "user", Content: req.Prompt}},
	}
	if req.JSONOnly {
		request.ResponseFormat = &responseFormat{Type: jsonObjectFormat}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("openai: encoding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("openai: building request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("openai: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("openai: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ports.LLMResponse{}, fmt.Errorf("openai: request failed with status %d: %s", resp.StatusCode, respBody)
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return ports.LLMResponse{}, fmt.Errorf("openai: decoding response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return ports.LLMResponse{}, fmt.Errorf("openai: response has no choices")
	}

	return ports.LLMResponse{Text: parsed.Choices[0].Message.Content, Model: parsed.Model}, nil
}
