package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

var _ ports.TranscriptionProvider = (*Client)(nil)

// TestClient_TranscribeUploadsTheAudioAsMultipart: POST
// /v1/audio/transcriptions with the bearer key, the audio as a file named by
// its format, and TranscriptionModel whatever the provider's own model is;
// the transcript comes back raw.
func TestClient_TranscribeUploadsTheAudioAsMultipart(t *testing.T) {
	t.Parallel()
	var path, auth, model, filename, audio string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not a multipart form: %v", err)
			return
		}
		model = r.FormValue("model")
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Errorf("no file part: %v", err)
			return
		}
		b, _ := io.ReadAll(f)
		filename, audio = h.Filename, string(b)
		_, _ = w.Write([]byte(`{"text":" Comprar pan mañana "}`))
	}))
	t.Cleanup(server.Close)

	c := NewClient(server.URL, "sk-test", "gpt-4o-mini", server.Client())
	resp, err := c.Transcribe(context.Background(), ports.TranscribeRequest{Audio: []byte("OggS"), Format: "ogg"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/audio/transcriptions" || auth != "Bearer sk-test" {
		t.Errorf("path %q auth %q", path, auth)
	}
	if model != TranscriptionModel || filename != "voice.ogg" || audio != "OggS" {
		t.Errorf("model %q, file %q holding %q; want %s, voice.ogg, the audio", model, filename, audio, TranscriptionModel)
	}
	if resp.Text != " Comprar pan mañana " || resp.Model != TranscriptionModel {
		t.Errorf("response %+v", resp)
	}
}

// TestClient_TranscribeFailsTyped: a non-200 is the same typed provider
// failure every other call returns.
func TestClient_TranscribeFailsTyped(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	_, err := NewClient(server.URL, "sk", "", server.Client()).Transcribe(context.Background(), ports.TranscribeRequest{Audio: []byte("x"), Format: "ogg"})
	var pe *ports.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a *ports.ProviderError", err)
	}
}
