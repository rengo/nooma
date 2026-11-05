package main

import (
	"testing"

	"github.com/rengo/nooma/internal/config"
)

// TestResolveNamerIsTheCaptureProviderAndOptional: the night's title phase asks
// the provider bound to capture_processing (ADR-0065), and a vault that binds none
// still sleeps, with that phase writing nothing: unlike connect's judge, a missing
// namer is not a refusal.
func TestResolveNamerIsTheCaptureProviderAndOptional(t *testing.T) {
	none := func(string) (string, bool) { return "", false }
	bound := &config.Config{
		Providers: map[string]config.Provider{"local": {Type: "ollama", Model: "test-model"}},
		Tasks:     map[string]config.TaskBinding{"capture_processing": {Provider: "local"}},
	}
	if resolveNamer(bound, none) == nil {
		t.Error("resolveNamer = nil with capture_processing bound, want its provider")
	}

	for name, cfg := range map[string]*config.Config{
		"unbound":     {Providers: bound.Providers, Tasks: map[string]config.TaskBinding{}},
		"no provider": {Providers: map[string]config.Provider{}, Tasks: bound.Tasks},
		"unbuildable": {Providers: map[string]config.Provider{"local": {Type: "whisper_cpp"}}, Tasks: bound.Tasks},
	} {
		if resolveNamer(cfg, none) != nil {
			t.Errorf("%s: resolveNamer returned a provider, want nil so the phase writes nothing", name)
		}
	}
}
