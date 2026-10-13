package ui_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
)

// A provider or model failure on the capture and correction forms is said in
// words, with the status the failure deserves, and leaves one log line (class,
// provider, path) that holds neither the key nor the typed text.
//
// Not parallel: it swaps the process-wide slog default to read that line.
func TestCaptureForms_ProviderFailuresAreSaidPlainly(t *testing.T) {
	const text = "buy lilies for Marta"
	routes := []struct {
		name string
		req  func() *http.Request
		path string
	}{
		{"capture", func() *http.Request { return captureRequest("text=" + strings.ReplaceAll(text, " ", "+")) }, "/ui/capture"},
		{"correct", func() *http.Request { return correctRequest("unit-1", "text="+strings.ReplaceAll(text, " ", "+")) }, "/ui/units/unit-1/correct"},
	}
	for _, route := range routes {
		for _, tt := range []struct {
			err        error
			wantStatus int
			wantInBody string
			wantClass  string
		}{
			{ports.KeyMissing("openai", "OPENAI_API_KEY"), http.StatusServiceUnavailable, "OPENAI_API_KEY", "provider_key_missing"},
			{ports.StatusFailure("openai", 429), http.StatusServiceUnavailable, "rate limiting", "provider_rate_limited"},
			{brain.ErrModelOutput, http.StatusBadGateway, "nothing was saved", "model_output_unusable"},
		} {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))

			h := ui.New(ui.Deps{Capture: &fakeCapturer{err: tt.err}})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, route.req())
			slog.SetDefault(prev)

			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantInBody) {
				t.Errorf("%s/%s: got %d %q, want %d containing %q", route.name, tt.wantClass, rec.Code, rec.Body.String(), tt.wantStatus, tt.wantInBody)
			}
			line := buf.String()
			if strings.Count(line, "\n") != 1 || !strings.Contains(line, "class="+tt.wantClass) || !strings.Contains(line, "path="+route.path) {
				t.Errorf("%s/%s: want one log line with class and path, got %q", route.name, tt.wantClass, line)
			}
			if strings.Contains(line, text) || strings.Contains(rec.Body.String(), text) {
				t.Errorf("%s/%s: the typed text leaked", route.name, tt.wantClass)
			}
		}
	}
}
