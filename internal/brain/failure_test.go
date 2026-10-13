package brain_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
)

// Describe is the one place a capture error becomes words, a stable code and
// an HTTP status; the API, the web UI and the CLI all speak what it returns.
func TestDescribe(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantStatus int
		contains   []string
	}{
		{"key missing", ports.KeyMissing("openai", "OPENAI_API_KEY"), "provider_key_missing", 503,
			[]string{"OpenAI", "OPENAI_API_KEY", ".env", "environment", "restart"}},
		{"unreachable", &ports.ProviderError{Provider: "anthropic", Kind: ports.FailureUnreachable}, "provider_unreachable", 502,
			[]string{"Anthropic", "could not be reached"}},
		{"key rejected", ports.StatusFailure("openai", 401), "provider_key_rejected", 502,
			[]string{"OpenAI", "rejected", "key"}},
		{"rate limited", ports.StatusFailure("openai", 429), "provider_rate_limited", 503,
			[]string{"OpenAI", "rate limit"}},
		{"timeout", &ports.ProviderError{Provider: "ollama", Kind: ports.FailureTimeout}, "provider_timeout", 504,
			[]string{"Ollama", "too long"}},
		{"other status", ports.StatusFailure("openai", 500), "provider_failed", 502,
			[]string{"OpenAI", "500"}},
		{"model output", fmt.Errorf("capture: decode: %w: %w", brain.ErrModelOutput, errors.New("no fields")), "model_output_unusable", 502,
			[]string{"could not be understood", "nothing was saved"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := brain.Describe(fmt.Errorf("capture: classify completion: %w", tt.err))
			if !ok {
				t.Fatal("Describe did not recognise the error")
			}
			if f.Code != tt.wantCode || f.Status != tt.wantStatus {
				t.Errorf("got code %q status %d, want %q %d", f.Code, f.Status, tt.wantCode, tt.wantStatus)
			}
			for _, s := range tt.contains {
				if !strings.Contains(f.Message, s) {
					t.Errorf("message %q lacks %q", f.Message, s)
				}
			}
		})
	}
}

func TestDescribeLeavesOtherErrorsAlone(t *testing.T) {
	if _, ok := brain.Describe(errors.New("disk is on fire")); ok {
		t.Error("an unrelated error was described as a provider or model failure")
	}
}
