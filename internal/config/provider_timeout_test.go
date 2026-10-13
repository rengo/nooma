package config

import (
	"strings"
	"testing"
	"time"
)

// A provider has a deadline for each call: 60s unless the vault says
// otherwise, so a hung provider ends in a timeout instead of a hung capture.
func TestProviderTimeout(t *testing.T) {
	t.Parallel()

	if DefaultProviderTimeout != 60*time.Second {
		t.Errorf("DefaultProviderTimeout = %v, want 60s (doc 02 §13)", DefaultProviderTimeout)
	}
	if got := (Provider{}).CallTimeout(); got != DefaultProviderTimeout {
		t.Errorf("unset timeout = %v, want the default", got)
	}
	if got := (Provider{Timeout: "250ms"}).CallTimeout(); got != 250*time.Millisecond {
		t.Errorf("timeout 250ms = %v", got)
	}
}

func TestValidateRejectsAnUnusableProviderTimeout(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"soon", "0s", "-5s", "30"} {
		cfg := decoded(t, "providers:\n  p:\n    type: ollama\n    timeout: \""+bad+"\"\n")
		err := cfg.Validate(t.TempDir(), noEnv)
		if err == nil || !strings.Contains(err.Error(), "providers.p.timeout") {
			t.Errorf("timeout %q: Validate = %v, want an error naming providers.p.timeout", bad, err)
		}
	}
	cfg := decoded(t, "providers:\n  p:\n    type: ollama\n    timeout: 2m\n")
	if err := cfg.Validate(t.TempDir(), noEnv); err != nil {
		t.Errorf("timeout 2m rejected: %v", err)
	}
}
