package main

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/config"
)

// TestNewEnvVarName is spec R4.3's own type-level guarantee, table-driven:
// no real-shaped API key can survive this constructor, and every real
// POSIX-shaped variable name does.
func TestNewEnvVarName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"real anthropic key", "sk-ant-api03-abc123XYZ", false},
		{"real openai key", "sk-proj-abc123XYZ", false},
		{"documented posix name", "OPENAI_API_KEY", true},
		{"leading underscore", "_MY_KEY", true},
		{"lowercase rejected", "openai_api_key", false},
		{"empty rejected", "", false},
		{"starts with a digit", "1KEY", false},
		{"hyphen rejected", "OPENAI-API-KEY", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewEnvVarName(tt.in)
			if (err == nil) != tt.ok {
				t.Errorf("NewEnvVarName(%q) error = %v, want ok=%v", tt.in, err, tt.ok)
			}
		})
	}
}

// TestPromptProviderSetupSkipsOnEOF is what keeps TestFreshVaultIsLoadable
// (test/e2e/init_test.go) passing unchanged: exec.Cmd hands a child process
// whose Stdin was never set an immediately-closed /dev/null, so every
// pre-existing e2e test that never scripted stdin reads EOF here and gets
// exactly M0's own commented placeholder back.
func TestPromptProviderSetupSkipsOnEOF(t *testing.T) {
	var out strings.Builder
	choices, bindings := promptProviderSetup(strings.NewReader(""), &out)

	if choices != nil || bindings != nil {
		t.Fatalf("EOF input produced choices=%v bindings=%v, want nil, nil", choices, bindings)
	}
	const wantPlaceholder = "# providers:           # added in M1, when nooma starts calling models\n# tasks:\n"
	if got := renderProviders(choices, bindings); got != wantPlaceholder {
		t.Errorf("renderProviders(nil, nil) = %q, want M0's exact placeholder %q", got, wantPlaceholder)
	}
}

// TestPromptProviderSetupCloudPathWritesTwoOpenAIEntries is 15.2: the wizard
// flow driving the Cloud path with scripted input, asserting two distinct
// openai providers (chat model vs. embedding model) and bindings covering
// every task tasksM1Consumes names — R6.2's own binding, performed by this
// PR's diff.
func TestPromptProviderSetupCloudPathWritesTwoOpenAIEntries(t *testing.T) {
	var out strings.Builder
	// "1" chooses Cloud; the blank line accepts the default env var name.
	choices, bindings := promptProviderSetup(strings.NewReader("1\n\n"), &out)

	if len(choices) != 2 {
		t.Fatalf("Cloud path returned %d providers, want 2 (chat + embedding): %+v", len(choices), choices)
	}
	for _, c := range choices {
		if c.Type != "openai" {
			t.Errorf("provider %q has type %q, want openai", c.Name, c.Type)
		}
		if c.APIKeyEnv != defaultOpenAIKeyEnv {
			t.Errorf("provider %q APIKeyEnv = %q, want the default %q", c.Name, c.APIKeyEnv, defaultOpenAIKeyEnv)
		}
	}
	if choices[0].Model == choices[1].Model {
		t.Error("both providers carry the same model — a chat model is not an embedding model (design D15)")
	}

	for _, task := range []string{"capture_processing", "relation_evaluation", "embedding"} {
		if _, bound := bindings[task]; !bound {
			t.Errorf("Cloud path did not bind %q", task)
		}
	}
	if bindings["embedding"] == bindings["capture_processing"] {
		t.Error("embedding and capture_processing bind the same provider entry, want two distinct openai entries")
	}

	if !strings.Contains(out.String(), "OPENAI_API_KEY") {
		t.Errorf("the wizard's own output does not name the key's environment variable, want it to guide the user to set it:\n%s", out.String())
	}
}

// TestPromptProviderSetupCloudPathNeverAcceptsAKeyValue is spec R4.3's
// structural guarantee proven against the wizard's ONE remaining input:
// a user who pastes their real key where the env var name is asked cannot
// make it into a providerChoice at all — NewEnvVarName rejects it, and the
// wizard falls back to the documented default rather than holding the
// pasted value anywhere.
func TestPromptProviderSetupCloudPathNeverAcceptsAKeyValue(t *testing.T) {
	const pastedKey = "sk-proj-not-a-real-key-0123456789"

	var out strings.Builder
	choices, _ := promptProviderSetup(strings.NewReader("1\n"+pastedKey+"\n"), &out)

	for _, c := range choices {
		if string(c.APIKeyEnv) == pastedKey {
			t.Fatalf("provider %q APIKeyEnv = %q — the pasted key value reached a providerChoice", c.Name, c.APIKeyEnv)
		}
		if c.APIKeyEnv != defaultOpenAIKeyEnv {
			t.Errorf("provider %q APIKeyEnv = %q, want the default %q (the pasted value must be rejected, not stored)", c.Name, c.APIKeyEnv, defaultOpenAIKeyEnv)
		}
	}
	if strings.Contains(renderProviders(choices, bindTasks("openai_embed", "openai_chat")), pastedKey) {
		t.Fatal("the rendered providers: block contains the pasted key value")
	}
}

// TestPromptProviderSetupOllamaPathBindsOneEntry is 15.3: the Ollama path
// binds every task tasksM1Consumes names to the one ollama entry it writes,
// and offers no embedded llama.cpp option (ADR-0002).
func TestPromptProviderSetupOllamaPathBindsOneEntry(t *testing.T) {
	var out strings.Builder
	choices, bindings := promptProviderSetup(strings.NewReader("2\n"), &out)

	if len(choices) != 1 {
		t.Fatalf("Ollama path returned %d providers, want 1: %+v", len(choices), choices)
	}
	if choices[0].Type != "ollama" {
		t.Errorf("provider type = %q, want ollama", choices[0].Type)
	}
	for _, task := range tasksM1Consumes {
		if bindings[task] != choices[0].Name {
			t.Errorf("task %q binds %q, want the one ollama entry %q", task, bindings[task], choices[0].Name)
		}
	}
}

// TestWizardPopulatedVaultDecodesAndValidates is 15.4: TestFreshVaultIsLoadable
// (test/e2e/init_test.go) shaped coverage extended to a wizard-populated
// vault for both paths, at L2 — exercising the wizard's own functions
// directly rather than the compiled binary, so it stays cheap. Every task
// the wizard binds must name a provider present in the providers: map it
// also wrote (internal/config/validate.go's checkTaskProviders).
func TestWizardPopulatedVaultDecodesAndValidates(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"cloud", "1\n\n"},
		{"ollama", "2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			choices, bindings := promptProviderSetup(strings.NewReader(tt.in), &out)
			yml := defaultConfig(renderProviders(choices, bindings))

			cfg, err := config.Decode(strings.NewReader(yml))
			if err != nil {
				t.Fatalf("the %s-path config does not decode:\n%v\n%s", tt.name, err, yml)
			}
			cfg.ApplyDefaults()
			if err := cfg.Validate(t.TempDir(), func(string) (string, bool) { return "", false }); err != nil {
				t.Fatalf("the %s-path config does not validate:\n%v\n%s", tt.name, err, yml)
			}

			for _, task := range tasksM1Consumes {
				binding, bound := cfg.Tasks[task]
				if !bound {
					t.Errorf("task %q is not bound in the %s path's config", task, tt.name)
					continue
				}
				if _, present := cfg.Providers[binding.Provider]; !present {
					t.Errorf("task %q binds provider %q, which is not present in providers:", task, binding.Provider)
				}
			}
		})
	}
}
