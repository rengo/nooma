package ports_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func TestStatusFailureClassifiesByStatus(t *testing.T) {
	cases := []struct {
		status int
		want   ports.FailureKind
	}{
		{401, ports.FailureKeyRejected},
		{403, ports.FailureKeyRejected},
		{429, ports.FailureRateLimited},
		{500, ports.FailureOther},
		{400, ports.FailureOther},
	}
	for _, c := range cases {
		got := ports.StatusFailure("openai", c.status)
		if got.Kind != c.want || got.Status != c.status || got.Provider != "openai" {
			t.Errorf("status %d: got %+v, want kind %q", c.status, got, c.want)
		}
	}
}

func TestTransportFailureSeparatesTimeoutFromUnreachable(t *testing.T) {
	for name, err := range map[string]error{
		"deadline":      fmt.Errorf("wrapped: %w", context.DeadlineExceeded),
		"net timeout":   fmt.Errorf("wrapped: %w", timeoutErr{}),
		"client budget": fmt.Errorf("wrapped: %w", errors.Join(timeoutErr{})),
	} {
		if got := ports.TransportFailure("anthropic", err); got.Kind != ports.FailureTimeout {
			t.Errorf("%s: kind = %q, want timeout", name, got.Kind)
		}
	}
	got := ports.TransportFailure("ollama", errors.New("connection refused"))
	if got.Kind != ports.FailureUnreachable {
		t.Errorf("kind = %q, want unreachable", got.Kind)
	}
}

func TestProviderErrorIsFoundThroughWrapping(t *testing.T) {
	inner := ports.KeyMissing("openai", "OPENAI_API_KEY")
	wrapped := fmt.Errorf("capture: classify completion: %w", inner)
	var pe *ports.ProviderError
	if !errors.As(wrapped, &pe) || pe.EnvVar != "OPENAI_API_KEY" || pe.Kind != ports.FailureKeyMissing {
		t.Fatalf("errors.As did not recover the provider error: %+v", pe)
	}
}

func TestProviderErrorTextCarriesNoResponseBody(t *testing.T) {
	// A status failure has nowhere to put a vendor body: the error text is
	// logged, and a 400 body can echo the prompt, which is captured text.
	msg := ports.StatusFailure("openai", 400).Error()
	if msg != "openai: failed (status 400)" {
		t.Errorf("message = %q", msg)
	}
}

// A caller that went away is not a provider that is down: a canceled context
// is its own class, so serve does not log "unreachable" for a closed tab.
func TestTransportFailureSeparatesCancellationFromUnreachable(t *testing.T) {
	got := ports.TransportFailure("openai", fmt.Errorf("Post: %w", context.Canceled))
	if got.Kind != ports.FailureCanceled {
		t.Errorf("kind = %q, want canceled", got.Kind)
	}
}
