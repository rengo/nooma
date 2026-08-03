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

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// embedResponse mirrors OpenAI's POST /v1/embeddings shape: data is an array
// of objects, one per input, each carrying the vector under "embedding".
type embedResponse struct {
	Model string `json:"model"`
	Data  []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed sends req.Text to OpenAI's /v1/embeddings and returns the single
// vector it produced. Deliberately absent, per design D17:
//
//   - the optional "dimensions" request parameter — ports.EmbedResponse has
//     no Dim field (the dimension is len(Vector)), so a truncation knob has
//     no consumer and no §13 calibration row;
//   - normalization — internal/store/sqlite already calls recall.Normalize
//     at the storage boundary for every embedding (m1b D6); this client
//     leaves it there so there is exactly one place a reader looks to know
//     the vector is unit-normalized. recall.Normalize is idempotent (a
//     second pass divides an already-unit vector by a norm of ~1), so
//     normalizing here too would not corrupt anything — it would just be a
//     redundant pass over the vector with no owner. Do not add it back.
func (c *Client) Embed(ctx context.Context, req ports.EmbedRequest) (ports.EmbedResponse, error) {
	body, err := json.Marshal(embedRequest{
		Model: c.model,
		Input: req.Text,
	})
	if err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("openai: encoding embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("openai: building embed request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("openai: embed request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("openai: reading embed response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ports.EmbedResponse{}, fmt.Errorf("openai: embed request failed with status %d: %s", resp.StatusCode, respBody)
	}

	var parsed embedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return ports.EmbedResponse{}, fmt.Errorf("openai: decoding embed response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return ports.EmbedResponse{}, fmt.Errorf("openai: embed response carried no vector for model %q", parsed.Model)
	}

	return ports.EmbedResponse{Vector: parsed.Data[0].Embedding, Model: parsed.Model}, nil
}
