package config

import (
	"strings"
	"testing"
)

// validDocument is the shape docs/01-architecture.md §"Configuration — nooma.yml"
// documents, trimmed to the keys these tests need. The full document is the
// fixture of the L2 config↔doc gate (spec R9.1, slice 4), not of these tests:
// here the point is what happens to a document that is *almost* right.
const validDocument = `
server:
  bind: 127.0.0.1
  http_port: 7777
  ui: true
  auth_token_env: NOOMA_AUTH_TOKEN
database:
  path: ./nooma.db
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
schedules:
  consolidate: "0 3 * * *"
  proactive_check: "*/5 * * * *"
`

// TestDecodeAcceptsTheDocumentedShape is the control for every rejection test
// below. Without it, a Decode that rejected *everything* would make the whole
// table pass — the failure mode docs/06-harness.md §4 calls a vacuous green.
func TestDecodeAcceptsTheDocumentedShape(t *testing.T) {
	t.Parallel()

	cfg, err := Decode(strings.NewReader(validDocument))
	if err != nil {
		t.Fatalf("Decode rejected the documented shape: %v", err)
	}
	if cfg == nil {
		t.Fatal("Decode returned a nil config and a nil error")
	}
	if got := len(cfg.Providers); got != 1 {
		t.Errorf("Providers: got %d entries, want 1", got)
	}
	if got := len(cfg.Tasks); got != 1 {
		t.Errorf("Tasks: got %d entries, want 1", got)
	}
}

// TestDecodeRejectsUnknownKey is spec R3.2: an unknown key is a load error, and
// the error names the offending key.
//
// One case per nesting level, because "unknown key" is not one behavior — it is
// the decoder recursing correctly. A strict decoder that only policed the top
// level would pass a single-case test and silently accept `server: {http_prot:
// 7777}`, which is the typo a user actually makes.
//
// The two map-keyed sections (`providers`, `tasks`) are here for the same
// reason and are the subtle ones: their *keys* are user-chosen names, so
// strictness can only reach the fields inside each entry. A decoder that treated
// a map value as opaque would accept anything under `providers.claude_cloud`.
func TestDecodeRejectsUnknownKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		wantKey  string
	}{
		{
			name:     "top level",
			document: "sevrer:\n  ui: true\n",
			wantKey:  "sevrer",
		},
		{
			name:     "inside server",
			document: "server:\n  http_prot: 7777\n",
			wantKey:  "http_prot",
		},
		{
			name:     "inside database",
			document: "database:\n  paht: ./nooma.db\n",
			wantKey:  "paht",
		},
		{
			name:     "inside a named provider",
			document: "providers:\n  claude_cloud:\n    type: anthropic\n    api_key: sk-secret\n",
			wantKey:  "api_key",
		},
		{
			name:     "inside a named task",
			document: "tasks:\n  chat:\n    provdier: claude_cloud\n",
			wantKey:  "provdier",
		},
		{
			name:     "inside channels.telegram",
			document: "channels:\n  telegram:\n    enabled: true\n    allowed_chats: [1]\n",
			wantKey:  "allowed_chats",
		},
		{
			name:     "inside schedules",
			document: "schedules:\n  consolidat: \"0 3 * * *\"\n",
			wantKey:  "consolidat",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Decode(strings.NewReader(tc.document))
			if err == nil {
				t.Fatalf("Decode accepted an unknown key %q — R3.2 requires a load error", tc.wantKey)
			}
			if cfg != nil {
				t.Errorf("Decode returned a non-nil config alongside an error; R3.3 forbids a partially applied result")
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("error does not name the offending key %q, so the user cannot find it:\n%v", tc.wantKey, err)
			}
		})
	}
}

// TestDecodeRejectsDuplicateKey guards a property that comes from the decoder's
// default behavior rather than from its strict mode, which is exactly why it
// needs a test: nothing in this package's own code enforces it, so a future
// change of decoder or of options could drop it silently.
func TestDecodeRejectsDuplicateKey(t *testing.T) {
	t.Parallel()

	_, err := Decode(strings.NewReader("server:\n  http_port: 7777\n  http_port: 8888\n"))
	if err == nil {
		t.Fatal("Decode accepted a duplicated key; the later value would silently win")
	}
	if !strings.Contains(err.Error(), "http_port") {
		t.Errorf("error does not name the duplicated key:\n%v", err)
	}
}

// TestDecodeRejectsWrongType is spec R3.3. The assertion on the key name is the
// substance: a message that reports only "cannot unmarshal string into int"
// leaves the user counting lines, which is why design D1 chose this decoder
// over the alternative.
func TestDecodeRejectsWrongType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		wantKey  string
	}{
		{"string where int is expected", "server:\n  http_port: not-a-number\n", "http_port"},
		{"int where bool is expected", "server:\n  ui: 7777\n", "ui"},
		{"scalar where mapping is expected", "providers: claude_cloud\n", "providers"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Decode(strings.NewReader(tc.document))
			if err == nil {
				t.Fatalf("Decode accepted %s", tc.name)
			}
			if cfg != nil {
				t.Error("Decode returned a non-nil config alongside an error")
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("error does not name %q, so the user must count lines to find it:\n%v", tc.wantKey, err)
			}
		})
	}
}

// TestDecodeRejectsMalformedYAML is the other half of R3.3: a syntax error is a
// load error carrying a location, not a zero config and a nil error.
func TestDecodeRejectsMalformedYAML(t *testing.T) {
	t.Parallel()

	cfg, err := Decode(strings.NewReader("server:\n  bind: [unclosed\n"))
	if err == nil {
		t.Fatal("Decode accepted malformed YAML")
	}
	if cfg != nil {
		t.Error("Decode returned a non-nil config alongside an error")
	}
}

// TestDecodePreservesAbsence is design D-level, and it is why the four
// defaultable fields are pointers: R3.4 lets them be absent, and `status` should
// be able to report which values the user actually chose rather than which ones
// happen to equal the default. Applying defaults during decode would erase that
// distinction irrecoverably.
func TestDecodePreservesAbsence(t *testing.T) {
	t.Parallel()

	cfg, err := Decode(strings.NewReader("database:\n  path: ./nooma.db\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cfg.Server.Bind != nil {
		t.Errorf("Server.Bind: got %q, want nil for an absent key", *cfg.Server.Bind)
	}
	if cfg.Server.HTTPPort != nil {
		t.Errorf("Server.HTTPPort: got %d, want nil for an absent key", *cfg.Server.HTTPPort)
	}
	if cfg.Server.UI != nil {
		t.Errorf("Server.UI: got %v, want nil for an absent key", *cfg.Server.UI)
	}
	if cfg.Database.Path == nil {
		t.Error("Database.Path: got nil for a key the document sets")
	}
}

// TestDecodeAcceptsAnEmptyDocument covers a realistic user state that is easy to
// leave untested: a nooma.yml that exists and says nothing. Every key of R3.4 is
// absent-allowed and the rest are optional at decode time, so the document that
// chooses nothing must decode to the config that chose nothing — not to an
// error. The decoder signals it as EOF, which is why Decode has a branch for it;
// this is that branch's only cover.
func TestDecodeAcceptsAnEmptyDocument(t *testing.T) {
	t.Parallel()

	for _, document := range []string{"", "\n", "# only a comment\n"} {
		cfg, err := Decode(strings.NewReader(document))
		if err != nil {
			t.Fatalf("Decode(%q): %v", document, err)
		}
		if cfg == nil {
			t.Fatalf("Decode(%q) returned nil config and nil error", document)
		}
		if cfg.Server.Bind != nil {
			t.Errorf("Decode(%q): Server.Bind should be absent before ApplyDefaults", document)
		}
	}
}
