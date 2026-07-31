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

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// embedResponse mirrors Ollama's current /api/embed shape: embeddings is an
// array of vectors, one per input — a single input still comes back as
// embeddings[0], never as a bare vector. The deprecated, singular
// /api/embeddings endpoint returns {"embedding": [...]} instead; this client
// deliberately does not speak that one.
type embedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed sends req.Text to Ollama's /api/embed and returns the single vector
// it produced. A response carrying no vector — an empty embeddings array —
// is a Go error, never a zero-value EmbedResponse a caller could mistake for
// a valid embedding.
func (c *Client) Embed(ctx context.Context, req ports.EmbedRequest) (ports.EmbedResponse, error) {
	body, err := json.Marshal(embedRequest{
		Model: c.model,
		Input: req.Text,
	})
	if err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("ollama: encoding embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("ollama: building embed request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("ollama: embed request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("ollama: reading embed response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ports.EmbedResponse{}, fmt.Errorf("ollama: embed request failed with status %d: %s", resp.StatusCode, respBody)
	}

	var parsed embedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("ollama: decoding embed response: %w", err)
	}
	if len(parsed.Embeddings) == 0 {
		return ports.EmbedResponse{}, fmt.Errorf("ollama: embed response carried no vector for model %q", parsed.Model)
	}

	return ports.EmbedResponse{Vector: parsed.Embeddings[0], Model: parsed.Model}, nil
}
