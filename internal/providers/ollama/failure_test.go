package ollama

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// calls runs every call this adapter makes against client, so one table
// covers Complete and Embed alike.
func calls(client *Client) map[string]func() error {
	return map[string]func() error{
		"complete": func() error {
			_, err := client.Complete(context.Background(), ports.LLMRequest{Prompt: "hi"})
			return err
		},
		"embed": func() error {
			_, err := client.Embed(context.Background(), ports.EmbedRequest{Text: "hi"})
			return err
		},
	}
}

// TestFailuresAreProviderErrorsThatLeakNothing is the plain-errors contract:
// every failure is a *ports.ProviderError classed by what happened, and its
// text carries neither the vendor's body (which can echo the prompt) nor the
// key.
func TestFailuresAreProviderErrorsThatLeakNothing(t *testing.T) {
	t.Parallel()

	const secretBody = "echo of the captured text: buy lilies"
	for _, tc := range []struct {
		status int
		want   ports.FailureKind
	}{
		{http.StatusUnauthorized, ports.FailureKeyRejected},
		{http.StatusTooManyRequests, ports.FailureRateLimited},
		{http.StatusInternalServerError, ports.FailureOther},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(secretBody))
		}))
		client := NewClient(server.URL, "m", server.Client())
		for name, call := range calls(client) {
			err := call()
			var pe *ports.ProviderError
			if !errors.As(err, &pe) {
				t.Fatalf("%s %d: error %v is not a *ports.ProviderError", name, tc.status, err)
			}
			if pe.Kind != tc.want || pe.Provider != "ollama" || pe.Status != tc.status {
				t.Errorf("%s %d: got %+v, want kind %q", name, tc.status, pe, tc.want)
			}
			if strings.Contains(err.Error(), secretBody) || strings.Contains(err.Error(), "sk-test-key") {
				t.Errorf("%s %d: error text leaks: %q", name, tc.status, err)
			}
		}
		server.Close()
	}
}

func TestUnreachableAndTimeoutAreTold(t *testing.T) {
	t.Parallel()

	down := httptest.NewServer(http.NotFoundHandler())
	url := down.URL
	down.Close()
	client := NewClient(url, "m", http.DefaultClient)
	for name, call := range calls(client) {
		var pe *ports.ProviderError
		if err := call(); !errors.As(err, &pe) || pe.Kind != ports.FailureUnreachable {
			t.Errorf("%s: got %v, want unreachable", name, err)
		}
	}

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer slow.Close()
	defer close(release)
	client = NewClient(slow.URL, "m", &http.Client{Timeout: 50 * time.Millisecond})
	for name, call := range calls(client) {
		var pe *ports.ProviderError
		if err := call(); !errors.As(err, &pe) || pe.Kind != ports.FailureTimeout {
			t.Errorf("%s: got %v, want timeout", name, err)
		}
	}
}
