package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/providers/openai"
)

func noEnv(string) (string, bool) { return "", false }

// A provider whose key is not in the environment builds, and every call it
// makes says which variable was missing. Failing at call time (not at build
// time) keeps serve starting on a half-configured vault, which is the posture
// the nil-Deps 503 already takes.
func TestBuildProviderWithoutKeyFailsEachCallNamingTheVariable(t *testing.T) {
	for _, typ := range []string{"openai", "anthropic"} {
		client, err := buildProvider(config.Provider{Type: typ, APIKeyEnv: "THE_KEY", Model: "m"}, noEnv)
		if err != nil {
			t.Fatalf("%s: buildProvider: %v", typ, err)
		}
		llm, ok := client.(ports.LLMProvider)
		if !ok {
			t.Fatalf("%s: guard is not an LLMProvider", typ)
		}
		_, err = llm.Complete(context.Background(), ports.LLMRequest{Prompt: "p"})
		var pe *ports.ProviderError
		if !errors.As(err, &pe) || pe.Kind != ports.FailureKeyMissing || pe.EnvVar != "THE_KEY" || pe.Provider != typ {
			t.Errorf("%s: Complete error = %v, want key_missing naming THE_KEY", typ, err)
		}
	}
}

// The guard must not change which ports a provider type satisfies:
// resolveTaskProviders refuses an anthropic embedding binding by asserting
// the port, and a guard that implemented Embed would let it through.
func TestKeyGuardKeepsTheProvidersPortSet(t *testing.T) {
	anth, _ := buildProvider(config.Provider{Type: "anthropic", APIKeyEnv: "K"}, noEnv)
	if _, isEmbed := anth.(ports.EmbeddingProvider); isEmbed {
		t.Error("anthropic key guard satisfies EmbeddingProvider; the real client does not")
	}
	oai, _ := buildProvider(config.Provider{Type: "openai", APIKeyEnv: "K"}, noEnv)
	emb, isEmbed := oai.(ports.EmbeddingProvider)
	if !isEmbed {
		t.Fatal("openai key guard lost EmbeddingProvider")
	}
	_, err := emb.Embed(context.Background(), ports.EmbedRequest{Text: "t"})
	var pe *ports.ProviderError
	if !errors.As(err, &pe) || pe.Kind != ports.FailureKeyMissing {
		t.Errorf("Embed error = %v, want key_missing", err)
	}
}

func TestBuildProviderWithKeyBuildsTheRealClient(t *testing.T) {
	env := func(string) (string, bool) { return "sk-x", true }
	client, _ := buildProvider(config.Provider{Type: "openai", APIKeyEnv: "K"}, env)
	if _, ok := client.(*openai.Client); !ok {
		t.Errorf("client = %T, want *openai.Client", client)
	}
	// An empty value is as missing as an absent one.
	empty := func(string) (string, bool) { return "", true }
	client, _ = buildProvider(config.Provider{Type: "openai", APIKeyEnv: "K"}, empty)
	if _, ok := client.(*openai.Client); ok {
		t.Error("an empty key built the real client")
	}
}

func TestProviderKeyProblemsNamesEveryMissingVariableAndNoValue(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"chat":   {Type: "openai", APIKeyEnv: "OPENAI_API_KEY"},
			"other":  {Type: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
			"local":  {Type: "ollama"},
			"unused": {Type: "openai", APIKeyEnv: "UNUSED_KEY"},
		},
		Tasks: map[string]config.TaskBinding{
			"capture_processing": {Provider: "chat"},
			"chat":               {Provider: "other"},
			"embedding":          {Provider: "local"},
		},
	}
	env := func(name string) (string, bool) {
		if name == "ANTHROPIC_API_KEY" {
			return "sk-ant-secret-value", true
		}
		return "", false
	}
	problems := providerKeyProblems(cfg, env)
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly the one missing key a task uses", problems)
	}
	for _, want := range []string{"OPENAI_API_KEY", "openai", ".env", "environment"} {
		if !strings.Contains(problems[0], want) {
			t.Errorf("problem %q lacks %q", problems[0], want)
		}
	}
	if strings.Contains(strings.Join(problems, ""), "sk-ant-secret-value") {
		t.Error("a key's value leaked into the report")
	}
	t.Setenv("OPENAI_API_KEY", "")
	if err := checkProviderKeys("", cfg); err == nil {
		t.Error("the doctor check passed with a task's key missing")
	}
}
