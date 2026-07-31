// Package anthropic implements ports.LLMProvider over Anthropic's Messages
// API (POST /v1/messages) — design D7, spec R6.1.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rengo/nooma/internal/ports"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
	// defaultMaxTokens caps a single Messages API call. Phase A does not
	// tune this per task — that is Phase B's routing concern.
	defaultMaxTokens = 4096
)

// Client is Anthropic's ports.LLMProvider implementation.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

var _ ports.LLMProvider = (*Client)(nil)

// NewClient returns a Client that authenticates with apiKey and requests
// model. baseURL is overridable so tests point at an httptest.Server instead
// of api.anthropic.com — an in-process loopback listener, not "the network"
// in docs/06-harness.md §3's sense (design §6). Passing "" for baseURL uses
// the real Anthropic endpoint; passing nil for httpClient uses
// http.DefaultClient.
func NewClient(baseURL, apiKey, model string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, apiKey: apiKey, model: model, httpClient: httpClient}
}

type messagesRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Complete sends req.Prompt as a single user message and returns the
// vendor's raw text — never parsed into a classification (design D7; I14's
// degradation rule is core/classify's, Phase B's).
func (c *Client) Complete(ctx context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	body, err := json.Marshal(messagesRequest{
		Model:     c.model,
		MaxTokens: defaultMaxTokens,
		Messages:  []chatMessage{{Role: "user", Content: req.Prompt}},
	})
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("anthropic: encoding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("anthropic: building request: %w", err)
	}
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.LLMResponse{}, fmt.Errorf("anthropic: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ports.LLMResponse{}, fmt.Errorf("anthropic: request failed with status %d: %s", resp.StatusCode, respBody)
	}

	var parsed messagesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return ports.LLMResponse{}, fmt.Errorf("anthropic: decoding response: %w", err)
	}

	var text string
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	return ports.LLMResponse{Text: text, Model: parsed.Model}, nil
}
