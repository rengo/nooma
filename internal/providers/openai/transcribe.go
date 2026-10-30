package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/rengo/nooma/internal/ports"
)

// TranscriptionModel is the model every transcription asks for, whatever
// the provider entry's own model names (ADR-0062): binding
// audio_transcription to the provider the chat task already uses works with
// no second entry. gpt-4o-mini-transcribe over whisper-1: half the price per
// minute, a lower word error rate on OpenAI's own multilingual evaluation,
// Spanish included, and it reads Telegram's OGG/Opus as uploaded.
const TranscriptionModel = "gpt-4o-mini-transcribe"

// Transcribe uploads req.Audio to POST /v1/audio/transcriptions and returns
// the transcript raw. The audio is sent from memory and never written down.
func (c *Client) Transcribe(ctx context.Context, req ports.TranscribeRequest) (ports.TranscribeResponse, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("model", TranscriptionModel); err != nil {
		return ports.TranscribeResponse{}, fmt.Errorf("openai: encoding transcription: %w", err)
	}
	part, err := form.CreateFormFile("file", "voice."+req.Format)
	if err == nil {
		_, err = part.Write(req.Audio)
	}
	if err == nil {
		err = form.Close()
	}
	if err != nil {
		return ports.TranscribeResponse{}, fmt.Errorf("openai: encoding transcription: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/audio/transcriptions", &body)
	if err != nil {
		return ports.TranscribeResponse{}, fmt.Errorf("openai: building transcription request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", form.FormDataContentType())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ports.TranscribeResponse{}, ports.TransportFailure("openai", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.TranscribeResponse{}, fmt.Errorf("openai: reading transcription: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ports.TranscribeResponse{}, ports.StatusFailure("openai", resp.StatusCode)
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return ports.TranscribeResponse{}, fmt.Errorf("openai: decoding transcription: %w", err)
	}
	return ports.TranscribeResponse{Text: parsed.Text, Model: TranscriptionModel}, nil
}
