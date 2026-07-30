package config

import (
	"strings"
	"testing"
)

// TestApplyDefaults is spec R3.4: exactly four keys may be absent, and each takes
// a documented default. The values are asserted against the exported constants
// so that changing a default is a visible edit rather than a silent drift from
// what docs/01-architecture.md shows a user.
func TestApplyDefaults(t *testing.T) {
	t.Parallel()

	t.Run("an empty document takes every default", func(t *testing.T) {
		t.Parallel()

		cfg, err := Decode(strings.NewReader("{}\n"))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		cfg.ApplyDefaults()

		if got := *cfg.Server.Bind; got != DefaultBind {
			t.Errorf("Server.Bind: got %q, want %q", got, DefaultBind)
		}
		if got := *cfg.Server.HTTPPort; got != DefaultHTTPPort {
			t.Errorf("Server.HTTPPort: got %d, want %d", got, DefaultHTTPPort)
		}
		if got := *cfg.Server.UI; got != DefaultUI {
			t.Errorf("Server.UI: got %v, want %v", got, DefaultUI)
		}
		if got := *cfg.Database.Path; got != DefaultDatabasePath {
			t.Errorf("Database.Path: got %q, want %q", got, DefaultDatabasePath)
		}
	})

	// The false case is the one a pre-populated struct would get wrong, which is
	// why the fields are pointers: `ui: false` must not read as "absent, use the
	// default true".
	t.Run("a set value survives, including a false one", func(t *testing.T) {
		t.Parallel()

		cfg, err := Decode(strings.NewReader("server:\n  bind: 0.0.0.0\n  http_port: 9000\n  ui: false\n"))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		cfg.ApplyDefaults()

		if got := *cfg.Server.Bind; got != "0.0.0.0" {
			t.Errorf("Server.Bind: got %q, want the configured 0.0.0.0", got)
		}
		if got := *cfg.Server.HTTPPort; got != 9000 {
			t.Errorf("Server.HTTPPort: got %d, want the configured 9000", got)
		}
		if *cfg.Server.UI {
			t.Error("Server.UI: got true, want the configured false — a false value must not read as absent")
		}
	})

	t.Run("applying twice changes nothing", func(t *testing.T) {
		t.Parallel()

		cfg, err := Decode(strings.NewReader("server:\n  http_port: 9000\n"))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		cfg.ApplyDefaults()
		cfg.ApplyDefaults()

		if got := *cfg.Server.HTTPPort; got != 9000 {
			t.Errorf("Server.HTTPPort: got %d after two applications, want 9000", got)
		}
	})
}

// TestDecodeRejectsLiteralCredentials is spec R4.1, and it is structural rather
// than a review rule: the schema has no field anywhere that can hold a
// credential, so a user who pastes a key straight into nooma.yml is stopped by
// R3.2's unknown-key rejection. If someone ever adds an `api_key` field, this is
// what fails.
func TestDecodeRejectsLiteralCredentials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		wantKey  string
	}{
		{"provider api_key", "providers:\n  p:\n    type: anthropic\n    api_key: sk-live-secret\n", "api_key"},
		{"telegram bot_token", "channels:\n  telegram:\n    enabled: true\n    bot_token: 123:ABC\n", "bot_token"},
		{"server auth_token", "server:\n  auth_token: hunter2\n", "auth_token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Decode(strings.NewReader(tc.document)); err == nil {
				t.Fatalf("Decode accepted %s — a secret would be committable inside the vault", tc.name)
			} else if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("error does not name %q:\n%v", tc.wantKey, err)
			}
		})
	}
}

// TestSummaryNeverContainsASecretValue is spec R4.2. The config holds only
// variable *names*, so today this passes by construction — which is the point of
// locking it in now. The day somebody resolves a name to its value to make the
// summary friendlier, this test is what stops it reaching a terminal, a log or a
// pasted bug report.
func TestSummaryNeverContainsASecretValue(t *testing.T) {
	const sentinel = "sk-live-DO-NOT-LEAK-8f2a1c"

	t.Setenv("ANTHROPIC_API_KEY", sentinel)
	t.Setenv("TELEGRAM_BOT_TOKEN", sentinel)
	t.Setenv("NOOMA_AUTH_TOKEN", sentinel)

	cfg, err := Decode(strings.NewReader(`
server:
  bind: 0.0.0.0
  auth_token_env: NOOMA_AUTH_TOKEN
providers:
  claude_cloud:
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    model: claude-sonnet-4-5
channels:
  telegram:
    enabled: true
    bot_token_env: TELEGRAM_BOT_TOKEN
    allowed_chat_ids: [123456789]
`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	cfg.ApplyDefaults()

	summary := cfg.Summary()
	if strings.Contains(summary, sentinel) {
		t.Fatalf("Summary leaked a secret value:\n%s", summary)
	}

	// The names must be there. A summary that hid them too would pass the leak
	// assertion while being useless to the person running `nooma doctor`.
	for _, name := range []string{"NOOMA_AUTH_TOKEN", "ANTHROPIC_API_KEY", "TELEGRAM_BOT_TOKEN"} {
		if !strings.Contains(summary, name) {
			t.Errorf("Summary omits the variable name %q, so the user cannot tell what to set:\n%s", name, summary)
		}
	}
}

// TestSummaryReportsTheEffectiveConfiguration is the useful half of R12.1: the
// values `nooma status` and `nooma doctor` have to show.
func TestSummaryReportsTheEffectiveConfiguration(t *testing.T) {
	t.Parallel()

	cfg, err := Decode(strings.NewReader("server:\n  bind: 0.0.0.0\n  http_port: 9000\n  ui: false\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	cfg.ApplyDefaults()

	summary := cfg.Summary()
	for _, want := range []string{"0.0.0.0", "9000"} {
		if !strings.Contains(summary, want) {
			t.Errorf("Summary omits %q:\n%s", want, summary)
		}
	}
	if !strings.Contains(summary, "exposed") {
		t.Errorf("Summary does not say whether a non-loopback bind is exposed, which ADR-0007 makes doctor's job:\n%s", summary)
	}
}
