package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/sqlite/sqlitetest"
)

// TestWireBrain_ArmsAtTheStoredReminderPreferences: serve's capture reads
// the user's reminder preferences from the vault (ADR-0029, design D8). A
// capture wired without them arms at the defaults — two reminders — so one
// reminder three hours before is only reachable through the stored row.
// The provider is a loopback httptest.Server, not the network.
func TestWireBrain_ArmsAtTheStoredReminderPreferences(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/generate":
			_, _ = fmt.Fprint(w, `{"model":"m","response":"{\"type\":\"event\",\"normalized_content\":\"Flight\",\"weight\":0.7,\"decay_rate\":0.05,\"event_at\":\"2099-01-10T10:00:00Z\"}","done":true}`)
		case "/api/embed":
			_, _ = fmt.Fprint(w, `{"model":"m","embeddings":[[0.1,0.2,0.3]]}`)
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
  chat: {provider: local}
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

	sqlitetest.SeedReminderLeads(t, db, `[180]`)

	capture, _, err := wireBrain(ctx, db, cfg, func(string) (string, bool) { return "", false })
	if err != nil || capture == nil {
		t.Fatalf("wireBrain = %v, %v, want a wired capture", capture, err)
	}
	result, err := capture.Capture(ctx, brain.CaptureInput{Text: "flight on 10 January 2099 at 10:00", Channel: "chat"})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	armed, err := sqlite.NewTriggerRepo(db).ArmedForUnit(ctx, result.UnitID)
	if err != nil {
		t.Fatalf("ArmedForUnit: %v", err)
	}
	want := time.Date(2099, 1, 10, 7, 0, 0, 0, time.UTC)
	if len(armed) != 1 || !armed[0].FireAt.Equal(want) {
		t.Errorf("armed = %+v, want one reminder at %s — the stored three-hour lead, not the defaults", armed, want)
	}
}
