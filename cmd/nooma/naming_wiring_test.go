package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/store/sqlite"
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

// TestWireConsolidate_TheNightNamesAUnitWithTheCaptureProvider: the service
// wireConsolidate builds, not one a test assembles, has the capture provider bound
// for the title phase. Without that call in the wiring the real `nooma consolidate`
// and serve's nightly pass would skip the phase in silence, which no test over
// brain.NewConsolidateService can see. The provider is a loopback httptest.Server.
func TestWireConsolidate_TheNightNamesAUnitWithTheCaptureProvider(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/generate":
			_, _ = fmt.Fprint(w, `{"model":"m","response":"{\"title\":\"Reunión con Juan\"}","done":true}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(llm.Close)
	vault := writeVault(t, fmt.Sprintf(`providers:
  local:
    type: ollama
    model: m
    endpoint: %s
tasks:
  capture_processing: {provider: local}
  relation_evaluation: {provider: local}
  belief_derivation: {provider: local}
  embedding: {provider: local}
`, llm.URL))
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		t.Fatalf("loadVaultConfig: %v", err)
	}
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	units := sqlite.NewUnitRepo(db)
	event := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	at := time.Now().UTC().Truncate(time.Second)
	if err := units.Create(ctx, unit.Unit{ID: "u1", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "meeting with Juan",
		EventAt: &event, Source: "chat", Weight: 1, LastTouchedAt: at, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc, err := wireConsolidate(ctx, db, cfg, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("wireConsolidate: %v", err)
	}
	phase := consolidation.PhaseTitle
	if _, err := svc.Consolidate(ctx, brain.ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	got, err := units.ByID(ctx, "u1")
	if err != nil || got.Title != "Reunión con Juan" {
		t.Errorf("unit title = %q, %v; want the one the capture provider gave", got.Title, err)
	}
}
