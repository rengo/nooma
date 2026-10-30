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

// TestDecodeExplainsTheRetiredScheduleKeys covers ADR-0025.
//
// `schedules.consolidate` and `schedules.proactive_check` were decoded and read
// by nobody from M0 until 2026-08-31, and docs/01-architecture.md documented
// them with working-looking cron expressions the whole time. Removing the fields
// is what makes the schema honest — but on its own it turns every vault that
// copied the documented example into a vault that will not start, reporting an
// unknown key, which reads as "you made a typo" rather than "this setting is
// gone".
//
// So the rejection stays and the message changes. The distinction this test
// exists to hold is that a retired key is not a mistyped one, and the error has
// to say which of the two happened.
func TestDecodeExplainsTheRetiredScheduleKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
	}{
		{
			name:     "the documented example, copied verbatim",
			document: "schedules:\n  consolidate: \"0 3 * * *\"\n  proactive_check: \"*/5 * * * *\"\n",
		},
		{
			name:     "only the nightly key",
			document: "schedules:\n  consolidate: \"0 3 * * *\"\n",
		},
		{
			name:     "the block with nothing under it",
			document: "schedules:\n",
		},
		{
			name:     "alongside keys that are still real",
			document: validDocument + "schedules:\n  consolidate: \"0 3 * * *\"\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Decode(strings.NewReader(tc.document))
			if err == nil {
				t.Fatal("Decode accepted `schedules` — a key nothing reads must not load")
			}
			if cfg != nil {
				t.Errorf("Decode returned a non-nil config alongside an error; R3.3 forbids a partially applied result")
			}

			msg := err.Error()
			if !strings.Contains(msg, "schedules") {
				t.Errorf("error does not name `schedules`, so the user cannot find the block to delete:\n%v", err)
			}
			if !strings.Contains(msg, "ADR-0025") {
				t.Errorf("error does not point at ADR-0025, so the user is told to delete something without being told why:\n%v", err)
			}
			if !strings.Contains(msg, "03:00") || !strings.Contains(msg, "5 minutes") {
				t.Errorf("error does not state the values that are now fixed, so the user cannot tell whether the schedule they wanted is the schedule they get:\n%v", err)
			}
			if strings.Contains(msg, "unknown field") {
				t.Errorf("error still reads as a typo report; a retired key is not a mistyped one:\n%v", err)
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

// TestDecodeRejectsASecondDocument: nooma reads one YAML document. A second
// was never decoded, so nothing in it was checked, a key that does not exist
// included, and `nooma export` copies the file as it is: whatever it said
// would travel in a bundle. The YAML library stops at an empty or
// comment-only document as if the input had ended, so a document behind one
// was neither decoded nor seen: the refusal reads the text, not the library.
// A marker alone, before the one document or after it with only blank lines
// and comments behind it, is not a second document.
func TestDecodeRejectsASecondDocument(t *testing.T) {
	for name, doc := range map[string]string{
		"a second document":                           "server:\n  bind: 127.0.0.1\n---\napi_key: sk-secret\n",
		"behind an empty document":                    "server:\n  bind: 127.0.0.1\n---\n---\napi_key: sk-secret\n",
		"behind a comment-only document":              "server:\n  bind: 127.0.0.1\n---\n# c\n---\napi_key: sk-secret\n",
		"after an end marker":                         "server:\n  bind: 127.0.0.1\n...\n---\napi_key: sk-secret\n",
		"after an end marker, no start marker":        "server:\n  bind: 127.0.0.1\n...\napi_key: sk-secret\n",
		"on the marker's own line":                    "server:\n  bind: 127.0.0.1\n--- sk-secret\n",
		"after a first document on its marker's line": "--- {server: {bind: 127.0.0.1}}\n---\napi_key: sk-secret\n",
		// An empty or comment-only FIRST document hid the real one: the file
		// decoded into an empty config, with nothing in it checked.
		"behind an empty first document":       "---\n---\nserver:\n  bind: 127.0.0.1\n",
		"behind a comment-only first document": "---\n# c\n---\nserver:\n  bind: 127.0.0.1\n",
		"with Windows line endings":            "server:\r\n  bind: 127.0.0.1\r\n---\r\napi_key: sk-secret\r\n",
	} {
		_, err := Decode(strings.NewReader(doc))
		if err == nil || !strings.Contains(err.Error(), "more than one YAML document") ||
			!strings.Contains(err.Error(), "keep your configuration in a single document, removing the extra `---` or `...` lines") {
			t.Errorf("%s: Decode = %v, want a refusal saying nooma reads one and what to remove", name, err)
		}
	}
	for name, doc := range map[string]string{
		"a leading marker":                "---\nserver:\n  bind: 127.0.0.1\n",
		"a trailing marker":               "server:\n  bind: 127.0.0.1\n---\n",
		"a trailing end marker":           "server:\n  bind: 127.0.0.1\n...\n",
		"a trailing marker and a comment": "server:\n  bind: 127.0.0.1\n---\n# old settings\n\n",
		"both markers":                    "---\nserver:\n  bind: 127.0.0.1\n...\n",
		"a marker-like line in a block":   "providers:\n  p:\n    type: ollama\n    model: |\n      ---\n      x\n",
		"a comment then one marker":       "# my vault\n---\nserver:\n  bind: 127.0.0.1\n",
		"a directive then a marker":       "%YAML 1.2\n---\nserver:\n  bind: 127.0.0.1\n",
		"an empty file":                   "",
		"comments only":                   "# nothing set yet\n",
	} {
		if _, err := Decode(strings.NewReader(doc)); err != nil {
			t.Errorf("%s: Decode = %v, want one document accepted", name, err)
		}
	}
}

// TestDecodeReadsAFileThatStartsWithAByteOrderMark: an editor that saves UTF-8
// with a BOM must not make the first key unreadable.
func TestDecodeReadsAFileThatStartsWithAByteOrderMark(t *testing.T) {
	cfg, err := Decode(strings.NewReader("\ufeffserver:\n  bind: 127.0.0.1\n"))
	if err != nil {
		t.Fatalf("Decode of a file with a BOM = %v, want it read", err)
	}
	if cfg.Server.Bind == nil || *cfg.Server.Bind != "127.0.0.1" {
		t.Errorf("server.bind = %v, want 127.0.0.1", cfg.Server.Bind)
	}
}
