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
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/sqlite/sqlitetest"
)

// TestWireBrain_ArmsAtTheStoredReminderPreferences: serve's capture reads
// the user's reminder preferences from the vault (ADR-0029, design D8). A
// capture wired without them arms at the defaults — two reminders — so one
// reminder three hours before is only reachable through the stored row.
// The provider is a loopback httptest.Server, not the network.
func TestWireBrain_ArmsAtTheStoredReminderPreferences(t *testing.T) {
	// The production clock reads the host's zone, so the event is stated on
	// that wall clock: 15:00 local keeps the three-hour lead at 12:00 local,
	// outside quiet hours (ADR-0030) whatever zone the host is in.
	eventAt := time.Date(2099, 1, 10, 15, 0, 0, 0, time.Local)
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/generate":
			_, _ = fmt.Fprintf(w, `{"model":"m","response":"{\"type\":\"event\",\"normalized_content\":\"Flight\",\"weight\":0.7,\"decay_rate\":0.05,\"event_at\":\"%s\"}","done":true}`, eventAt.Format(time.RFC3339))
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
	want := eventAt.Add(-3 * time.Hour)
	if len(armed) != 1 || !armed[0].FireAt.Equal(want) {
		t.Errorf("armed = %+v, want one reminder at %s — the stored three-hour lead, not the defaults", armed, want)
	}
}

// TestWireProactive_DefersToTheStoredQuietEnd: serve's due scan reads the
// user's quiet end from the vault (ADR-0030 point 5). A trigger due at
// 07:30 fires at 08:30 under the default night, so its staying armed is
// only reachable through the stored 09:00.
func TestWireProactive_DefersToTheStoredQuietEnd(t *testing.T) {
	deferToStoredQuietEnd(t, func(clock ports.Clock, db *sqlite.Vault, cfg *config.Config) *brain.CheckService {
		check, err := wireProactive(clock, db, cfg, func(string) (string, bool) { return "", false }, nil, nil, nil)
		if err != nil {
			t.Fatalf("wireProactive: %v", err)
		}
		return check
	})
}

// TestWireCheck_DefersToTheStoredQuietEnd: `nooma check` reads the same
// quiet end serve's pass does, so a manual scan never fires what serve
// would still hold.
func TestWireCheck_DefersToTheStoredQuietEnd(t *testing.T) {
	deferToStoredQuietEnd(t, func(clock ports.Clock, db *sqlite.Vault, _ *config.Config) *brain.CheckService {
		return wireCheck(clock, db)
	})
}

// deferToStoredQuietEnd arms a trigger due at 07:30 under a stored quiet
// end of 09:00 and asserts the check wire builds holds it at 08:30 and
// fires it at 09:00.
func deferToStoredQuietEnd(t *testing.T, wire func(ports.Clock, *sqlite.Vault, *config.Config) *brain.CheckService) {
	t.Helper()
	ctx := context.Background()
	db, cfg := gateVault(t)
	sqlitetest.SeedQuietHoursEnd(t, db, "09:00")
	day := func(hour, minute int) time.Time { return time.Date(2099, 1, 10, hour, minute, 0, 0, time.UTC) }
	fireAt, yearly := day(7, 30), prospection.RuleYearly
	if err := sqlite.NewTriggerRepo(db).Create(ctx, ports.Trigger{
		ID: "trg", Kind: ports.TriggerKindTimeBased, FireAt: &fireAt,
		Payload: ports.TriggerPayload{ActionText: "birthday", LeadDays: 7}, CreatedAt: day(0, 0),
		RecurrenceRule: &yearly, RecurrenceAnchor: &prospection.Anchor{Month: time.January, Day: 17},
	}); err != nil {
		t.Fatalf("arming: %v", err)
	}
	for _, tc := range []struct {
		now   time.Time
		fired int
	}{{day(8, 30), 0}, {day(9, 0), 1}} {
		report, err := wire(gateClock{now: tc.now}, db, cfg).Check(ctx, brain.CheckRequest{DryRun: true})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if report.TriggersFired != tc.fired {
			t.Errorf("at %s: fired %d, want %d — the scan must wait for the stored quiet end", tc.now.Format("15:04"), report.TriggersFired, tc.fired)
		}
	}
}
