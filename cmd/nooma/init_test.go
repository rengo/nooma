package main

import (
	"regexp"
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

// TestEnvSkeletonNamesTheKeysTheWizardConfigured is the coherence the
// skeleton owes the wizard that just ran.
//
// The file is written for one purpose: to be the place the user pastes the
// key into. A hint naming a variable nothing reads sends them to edit a
// line that will never be looked up, and the failure surfaces much later
// as an unconfigured provider rather than as a typo they can see.
//
// Mutation: return to a fixed skeleton naming one vendor and the cloud
// cases fail — both the default name and the overridden one.
func TestEnvSkeletonNamesTheKeysTheWizardConfigured(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   []string
		absent []string
	}{
		{
			// "1" chooses Cloud; the blank line accepts the default name.
			name:   "cloud, default name",
			in:     "1\n\n",
			want:   []string{"# OPENAI_API_KEY="},
			absent: []string{"ANTHROPIC_API_KEY"},
		},
		{
			// A name the user chose. Nothing can guess this one, which is
			// exactly why the skeleton has to be built from the choices
			// rather than written in advance.
			name:   "cloud, overridden name",
			in:     "1\nMY_OWN_KEY\n",
			want:   []string{"# MY_OWN_KEY="},
			absent: []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY"},
		},
		{
			// Ollama runs locally and needs no key at all. Suggesting one
			// invents a requirement this vault does not have.
			name:   "ollama needs no provider key",
			in:     "2\n",
			absent: []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"},
		},
		{
			name:   "skipped setup names no vendor",
			in:     "\n",
			absent: []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			choices, _ := promptProviderSetup(strings.NewReader(tt.in), &out)
			skeleton := envSkeleton(choices)

			for _, want := range tt.want {
				if !strings.Contains(skeleton, want) {
					t.Errorf("the .env skeleton does not offer %q — the wizard configured it and this file is where the value goes:\n%s", want, skeleton)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(skeleton, absent) {
					t.Errorf("the .env skeleton names %q, which this wizard path never configured:\n%s", absent, skeleton)
				}
			}
		})
	}
}

// TestEnvSkeletonOffersTheVariableTheWizardPrinted pins the two outputs to
// each other rather than to a constant.
//
// The cloud path tells the user, on stdout, exactly which variable to add.
// Asserting that both name the same thing is what keeps them from drifting
// apart again — a test comparing each to its own hardcoded expectation
// would pass with the two disagreeing, which is the state this fixes.
//
// Mutation: change either the printed name or the skeleton's independently
// and this fails.
func TestEnvSkeletonOffersTheVariableTheWizardPrinted(t *testing.T) {
	var out strings.Builder
	choices, _ := promptProviderSetup(strings.NewReader("1\nCHOSEN_BY_THE_USER\n"), &out)

	printed := regexp.MustCompile(`as ([A-Za-z_][A-Za-z0-9_]*)=`).FindStringSubmatch(out.String())
	if printed == nil {
		t.Fatalf("the cloud path printed no variable name to add to .env:\n%s", out.String())
	}

	if want := "# " + printed[1] + "="; !strings.Contains(envSkeleton(choices), want) {
		t.Errorf("the wizard told the user to add %s= but the .env skeleton offers no %q line:\n%s", printed[1], want, envSkeleton(choices))
	}
}

// TestEnvSkeletonAlwaysOffersTheBotToken guards the one hint that is not
// derived from the wizard.
//
// There is no wizard step for Telegram, so nothing in choices can carry
// this name. It stays a fixed suggestion precisely because nothing
// contradicts it — and it has to survive every path, including the ones
// that configure no provider key at all.
func TestEnvSkeletonAlwaysOffersTheBotToken(t *testing.T) {
	for _, in := range []string{"1\n\n", "2\n", "\n"} {
		var out strings.Builder
		choices, _ := promptProviderSetup(strings.NewReader(in), &out)
		if s := envSkeleton(choices); !strings.Contains(s, "# TELEGRAM_BOT_TOKEN=") {
			t.Errorf("input %q: the .env skeleton offers no bot-token line:\n%s", in, s)
		}
	}
}
