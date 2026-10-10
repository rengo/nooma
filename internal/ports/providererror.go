package ports

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// FailureKind is the closed vocabulary of why a provider call failed. It is
// what a surface turns into a plain sentence, so it names the class of
// failure and nothing a vendor said.
type FailureKind string

const (
	FailureKeyMissing  FailureKind = "key_missing"  // the api_key_env holds nothing
	FailureUnreachable FailureKind = "unreachable"  // no answer at all (refused, no route, DNS)
	FailureKeyRejected FailureKind = "key_rejected" // the vendor answered 401 or 403
	FailureRateLimited FailureKind = "rate_limited" // the vendor answered 429
	FailureTimeout     FailureKind = "timeout"      // the call outlived its deadline
	FailureOther       FailureKind = "failed"       // any other non-success answer
)

// ProviderError is what every provider adapter returns when a call fails.
// Callers find it through any wrapping with errors.As.
//
// It carries no response body and no credential: its text is logged, and a
// vendor's error body can echo the prompt, which is the user's captured text.
type ProviderError struct {
	// Provider is the adapter's type: "openai", "anthropic", "ollama".
	Provider string
	Kind     FailureKind
	// Status is the vendor's HTTP status, or 0 when there was none.
	Status int
	// EnvVar names the missing variable; set only for FailureKeyMissing.
	EnvVar string
	// Err is the transport error underneath, if any.
	Err error
}

func (e *ProviderError) Error() string {
	switch {
	case e.Kind == FailureKeyMissing:
		return fmt.Sprintf("%s: key missing (%s is not set)", e.Provider, e.EnvVar)
	case e.Status != 0:
		return fmt.Sprintf("%s: %s (status %d)", e.Provider, e.Kind, e.Status)
	case e.Err != nil:
		return fmt.Sprintf("%s: %s: %v", e.Provider, e.Kind, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Provider, e.Kind)
}

func (e *ProviderError) Unwrap() error { return e.Err }

// KeyMissing reports that provider's api_key_env, envVar, holds no value.
func KeyMissing(provider, envVar string) *ProviderError {
	return &ProviderError{Provider: provider, Kind: FailureKeyMissing, EnvVar: envVar}
}

// StatusFailure classifies a non-success HTTP answer. The body is
// deliberately not a parameter.
func StatusFailure(provider string, status int) *ProviderError {
	// The numbers, not net/http's names: a port does not speak HTTP.
	kind := FailureOther
	switch status {
	case 401, 403:
		kind = FailureKeyRejected
	case 429:
		kind = FailureRateLimited
	}
	return &ProviderError{Provider: provider, Kind: kind, Status: status}
}

// TransportFailure classifies an error from sending the request.
func TransportFailure(provider string, err error) *ProviderError {
	kind := FailureUnreachable
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		kind = FailureTimeout
	}
	return &ProviderError{Provider: provider, Kind: kind, Err: err}
}
