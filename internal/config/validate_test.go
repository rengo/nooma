package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// noEnv is an environment that has nothing set. Most validation cases do not
// depend on the environment, and passing this states that rather than leaving a
// nil to wonder about.
func noEnv(string) (string, bool) { return "", false }

func envWith(pairs ...string) func(string) (string, bool) {
	set := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		set[pairs[i]] = pairs[i+1]
	}
	return func(k string) (string, bool) { v, ok := set[k]; return v, ok }
}

func decoded(t *testing.T, document string) *Config {
	t.Helper()

	cfg, err := Decode(strings.NewReader(document))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	cfg.ApplyDefaults()
	return cfg
}

// TestValidateAcceptsTheDocumentedShape is the control. Without it, a Validate
// that rejected everything would make every rejection case below pass.
func TestValidateAcceptsTheDocumentedShape(t *testing.T) {
	t.Parallel()

	cfg := decoded(t, `
server:
  bind: 127.0.0.1
providers:
  claude_cloud:
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    model: claude-sonnet-4-5
tasks:
  chat: { provider: claude_cloud }
channels:
  telegram:
    enabled: true
    bot_token_env: TELEGRAM_BOT_TOKEN
    allowed_chat_ids: [123456789]
`)

	err := cfg.Validate("/vault", envWith("TELEGRAM_BOT_TOKEN", "123:ABC"))
	if err != nil {
		t.Fatalf("Validate rejected a valid configuration: %v", err)
	}
}

// TestValidateTelegramRequiresAllowedChatIDs is spec R5.1 and non-negotiable #7:
// a safe default is structural, not a warning. docs/01-architecture.md and
// ADR-0006 both say the channel does not start without that list, and this is
// where that stops being prose — in M0, two milestones before the adapter exists.
// Deferring it to M3 would leave the promise as somebody's future discipline.
func TestValidateTelegramRequiresAllowedChatIDs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		wantErr  bool
	}{
		{
			name:     "enabled with an empty list",
			document: "channels:\n  telegram:\n    enabled: true\n    bot_token_env: T\n    allowed_chat_ids: []\n",
			wantErr:  true,
		},
		{
			name:     "enabled with the key absent",
			document: "channels:\n  telegram:\n    enabled: true\n    bot_token_env: T\n",
			wantErr:  true,
		},
		{
			name:     "enabled with one id",
			document: "channels:\n  telegram:\n    enabled: true\n    bot_token_env: T\n    allowed_chat_ids: [1]\n",
			wantErr:  false,
		},
		{
			name:     "disabled with an empty list",
			document: "channels:\n  telegram:\n    enabled: false\n",
			wantErr:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := decoded(t, tc.document).Validate("/vault", envWith("T", "token"))
			switch {
			case tc.wantErr && err == nil:
				t.Fatal("Validate accepted an enabled Telegram channel with no allowed chat ids — anyone who finds the bot could talk to this brain")
			case !tc.wantErr && err != nil:
				t.Fatalf("Validate rejected a valid channel configuration: %v", err)
			case tc.wantErr && !strings.Contains(err.Error(), "allowed_chat_ids"):
				t.Errorf("error does not name allowed_chat_ids:\n%v", err)
			}
		})
	}
}

// TestValidateChecksEnvOnlyForEnabledComponents is spec R5.2, and the scoping is
// the point. M0 interprets no provider (R3.1), so failing on every unset provider
// key would make a configuration that is correct for M1 unloadable today — the
// loader would be enforcing a requirement the milestone does not have.
func TestValidateChecksEnvOnlyForEnabledComponents(t *testing.T) {
	t.Parallel()

	t.Run("enabled telegram with an unset token variable fails", func(t *testing.T) {
		t.Parallel()

		cfg := decoded(t, "channels:\n  telegram:\n    enabled: true\n    bot_token_env: TELEGRAM_BOT_TOKEN\n    allowed_chat_ids: [1]\n")
		err := cfg.Validate("/vault", noEnv)
		if err == nil {
			t.Fatal("Validate accepted an enabled channel whose token variable is unset")
		}
		if !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN") {
			t.Errorf("error does not name the variable the user has to set:\n%v", err)
		}
	})

	t.Run("disabled telegram with an unset token variable passes", func(t *testing.T) {
		t.Parallel()

		cfg := decoded(t, "channels:\n  telegram:\n    enabled: false\n    bot_token_env: TELEGRAM_BOT_TOKEN\n")
		if err := cfg.Validate("/vault", noEnv); err != nil {
			t.Fatalf("Validate rejected a disabled channel for a variable nothing reads: %v", err)
		}
	})

	t.Run("a provider key unset in M0 passes", func(t *testing.T) {
		t.Parallel()

		cfg := decoded(t, "providers:\n  claude_cloud:\n    type: anthropic\n    api_key_env: ANTHROPIC_API_KEY\n    model: m\n")
		if err := cfg.Validate("/vault", noEnv); err != nil {
			t.Fatalf("Validate rejected an unset provider key; M0 interprets no provider, so this configuration is correct today: %v", err)
		}
	})
}

// TestValidateAuthTokenForNonLoopbackBind is ADR-0007's half that belongs to
// config: if the bind is not loopback, the token variable is mandatory and must
// actually be set. The refusal to open a socket lives in slice 10; catching it
// here means `nooma doctor` can report it without starting a server.
func TestValidateAuthTokenForNonLoopbackBind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		env      func(string) (string, bool)
		wantErr  bool
	}{
		{"loopback with no token", "server:\n  bind: 127.0.0.1\n", noEnv, false},
		{"localhost with no token", "server:\n  bind: localhost\n", noEnv, false},
		{"default bind with no token", "{}\n", noEnv, false},
		{"non-loopback with no token key", "server:\n  bind: 0.0.0.0\n", noEnv, true},
		{"non-loopback with an unset token variable", "server:\n  bind: 0.0.0.0\n  auth_token_env: T\n", noEnv, true},
		{"non-loopback with a set token variable", "server:\n  bind: 0.0.0.0\n  auth_token_env: T\n", envWith("T", "secret"), false},
		{"a hostname that merely looks like loopback", "server:\n  bind: 127.0.0.1.evil\n", noEnv, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := decoded(t, tc.document).Validate("/vault", tc.env)
			switch {
			case tc.wantErr && err == nil:
				t.Fatal("Validate accepted a non-loopback bind with no usable auth token — ADR-0007 requires one")
			case !tc.wantErr && err != nil:
				t.Fatalf("Validate rejected a valid binding: %v", err)
			}
		})
	}
}

// TestDatabasePath is spec R5.3. The escape cases are the substance: the vault is
// one object that can be copied, moved and backed up as a unit, and a database
// outside it breaks that guarantee silently — the breakage only surfaces when a
// backup is restored somewhere else.
func TestDatabasePath(t *testing.T) {
	t.Parallel()

	const vault = "/home/pablo/pablo.nooma"

	cases := []struct {
		name    string
		path    string
		want    string
		wantErr string
	}{
		{name: "the documented default", path: "./nooma.db", want: filepath.Join(vault, "nooma.db")},
		{name: "a bare filename", path: "nooma.db", want: filepath.Join(vault, "nooma.db")},
		{name: "a subdirectory", path: "data/nooma.db", want: filepath.Join(vault, "data", "nooma.db")},
		{name: "a parent escape", path: "../outside.db", wantErr: "outside the vault"},
		{name: "a deep escape that cleans back out", path: "data/../../outside.db", wantErr: "outside the vault"},
		{name: "an absolute path elsewhere", path: "/tmp/outside.db", wantErr: "outside the vault"},
		{name: "an absolute path inside the vault", path: filepath.Join(vault, "nooma.db"), want: filepath.Join(vault, "nooma.db")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := decoded(t, "database:\n  path: "+tc.path+"\n")
			got, err := cfg.DatabasePath(vault)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("DatabasePath(%q) returned %q, want an error", tc.path, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error does not say the path is outside the vault:\n%v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("DatabasePath(%q): %v", tc.path, err)
			}
			if got != tc.want {
				t.Errorf("DatabasePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
			if !filepath.IsAbs(got) {
				t.Errorf("DatabasePath returned %q, which is relative; sqlite.Open rejects a relative path", got)
			}
		})
	}
}

// TestValidateRejectsUndocumentedNames covers what strict decoding structurally
// cannot: `providers` and `tasks` are maps, so their keys are user data and the
// decoder has no schema to check them against. Validation is the only place these
// can be caught.
func TestValidateRejectsUndocumentedNames(t *testing.T) {
	t.Parallel()

	t.Run("an unknown provider type", func(t *testing.T) {
		t.Parallel()

		cfg := decoded(t, "providers:\n  p:\n    type: openai_but_not_yet\n    model: m\n")
		err := cfg.Validate("/vault", noEnv)
		if err == nil {
			t.Fatal("Validate accepted an undocumented provider type")
		}
		if !strings.Contains(err.Error(), "openai_but_not_yet") {
			t.Errorf("error does not name the offending type:\n%v", err)
		}
	})

	t.Run("an unknown task name", func(t *testing.T) {
		t.Parallel()

		cfg := decoded(t, "tasks:\n  chatt: { provider: p }\n")
		err := cfg.Validate("/vault", noEnv)
		if err == nil {
			t.Fatal("Validate accepted an undocumented task name; a typo here would silently disable a brain task")
		}
		if !strings.Contains(err.Error(), "chatt") {
			t.Errorf("error does not name the offending task:\n%v", err)
		}
	})

	t.Run("every documented task name and provider type is accepted", func(t *testing.T) {
		t.Parallel()

		var b strings.Builder
		b.WriteString("providers:\n")
		for i, typ := range DocumentedProviderTypes {
			b.WriteString("  p" + string(rune('a'+i)) + ":\n    type: " + typ + "\n")
		}
		b.WriteString("tasks:\n")
		for _, name := range DocumentedTaskNames {
			b.WriteString("  " + name + ": { provider: pa }\n")
		}

		if err := decoded(t, b.String()).Validate("/vault", noEnv); err != nil {
			t.Fatalf("Validate rejected a document using only documented names — this is what keeps the lists honest: %v", err)
		}
	})
}

// TestValidateReportsEveryProblem is spec R5.4, and design D10 is why it holds
// structurally rather than by discipline: the checks are a slice of values, so
// "report everything" is the shape of the code rather than a rule somebody has to
// remember when adding the next check.
//
// `nooma doctor` exists to make the binary feel cared for. A doctor that reports
// one problem per run makes the user iterate.
func TestValidateReportsEveryProblem(t *testing.T) {
	t.Parallel()

	cfg := decoded(t, `
server:
  bind: 0.0.0.0
database:
  path: ../outside.db
providers:
  p:
    type: not_a_type
tasks:
  chatt: { provider: p }
channels:
  telegram:
    enabled: true
    bot_token_env: MISSING_TOKEN
`)

	err := cfg.Validate("/vault", noEnv)
	if err == nil {
		t.Fatal("Validate accepted a configuration with five independent problems")
	}

	for _, want := range []string{
		"auth_token_env",    // ADR-0007: non-loopback bind, no token
		"outside the vault", // R5.3
		"not_a_type",        // undocumented provider type
		"chatt",             // undocumented task name
		"allowed_chat_ids",  // R5.1
		"MISSING_TOKEN",     // R5.2
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the aggregate error omits %q, so the user would fix one problem and meet the next:\n%v", want, err)
		}
	}
}
