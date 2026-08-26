//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/fakechannel"
)

// TestM3Demo_ASimulatedDay is M3's own exit criterion, and the only place
// its five acts are asserted together.
//
// Together is the claim. Each act has its own test at L1 or L2; what no
// other test says is that one vault, one clock and one channel carry a
// trigger from armed to pushed, a quieter one from armed to held to
// digested, and a timer from pending to worded-at-delivery — with
// decision_log telling all of it and nothing delivered during quiet hours
// except the timer.
//
// The clock is fixed rather than real, which is what makes the day
// simulable at all: five acts spread over morning and evening cannot be
// observed by a test that waits. G22's lesson applies in reverse here —
// where a fixture CAN inject a clock, it must, and only the fixtures the
// shipped binary owns are stuck with the wall clock.
func TestM3Demo_ASimulatedDay(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "demo.nooma")
	dbPath := vaultDBPath(t, vault)

	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var (
		triggers  = sqlite.NewTriggerRepo(db)
		timers    = sqlite.NewTimerRepo(db)
		decisions = sqlite.NewDecisionLog(db)
		units     = sqlite.NewUnitRepo(db)
		state     = sqlite.NewStateRepo(db)
		channel   = fakechannel.New()
	)

	// A Wednesday. Every instant below is an offset from it, so the day is
	// one arithmetic and not five literals.
	morning := time.Date(2026, 8, 5, prospection.DigestHour, 5, 0, 0, time.UTC)
	quietHour := time.Date(2026, 8, 5, prospection.QuietHoursStartHour+1, 0, 0, 0, time.UTC)

	// Act 1 — two triggers are armed: one loud, one quiet.
	seedUnit(t, db, "unit-loud")
	seedUnit(t, db, "unit-quiet")
	loud, quiet := 0.9, 0.2
	for _, seed := range []struct {
		id, unit string
		level    *float64
		text     string
	}{
		{"trg-loud", "unit-loud", &loud, "call the clinic back"},
		{"trg-quiet", "unit-quiet", &quiet, "water the plants"},
	} {
		fireAt := morning.Add(-time.Minute)
		unitID := seed.unit
		if err := triggers.Create(ctx, ports.Trigger{
			ID: seed.id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
			InterruptLevel: seed.level, FireAt: &fireAt,
			Payload:   ports.TriggerPayload{ActionText: seed.text},
			CreatedAt: morning.Add(-24 * time.Hour),
		}); err != nil {
			t.Fatalf("arming %s: %v", seed.id, err)
		}
	}

	// Act 2 — a timer, due during quiet hours, and late enough to say so.
	if err := timers.Create(ctx, ports.Timer{
		ID:         "tmr-oven",
		FireAt:     quietHour.Add(-time.Duration(prospection.DelayCaveatMinutes) * time.Minute),
		ActionText: strPtr("take the bread out of the oven"),
		CreatedAt:  quietHour.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("arming the timer: %v", err)
	}

	// ONE id generator across every pass. A fresh one per pass restarts
	// its counter and collides with the previous pass's decision_log
	// rows, which is a "decision already exists" the demo earned on its
	// first run — and a reminder that a simulated day is several passes
	// against one vault, not several vaults.
	ids := &demoIDs{}

	check := func(now time.Time) brain.CheckReport {
		t.Helper()
		report, err := brain.NewCheckService(demoClock{now: now}, triggers, timers,
			ids, decisions, channel, units, state, nil, "12449194").
			Check(ctx, brain.CheckRequest{})
		if err != nil {
			t.Fatalf("check at %s: %v", now.Format("15:04"), err)
		}
		return report
	}

	// Act 3 — the quiet-hours pass. The timer fires and is delivered; the
	// triggers are deferred, because I16's one exception is the timer.
	quietReport := check(quietHour)
	if quietReport.TimersFired != 1 {
		t.Fatalf("the timer did not fire during quiet hours (%+v) — it is I16's one exception", quietReport)
	}
	if quietReport.TriggersDelivered != 0 {
		t.Fatalf("a trigger was delivered during quiet hours (%+v)", quietReport)
	}

	// Act 4 — the morning pass. The loud one is pushed; the quiet one
	// fires and waits for the digest, which the same pass assembles.
	morningReport := check(morning)
	if morningReport.TriggersDelivered != 1 {
		t.Fatalf("want exactly one pushed trigger, got %+v", morningReport)
	}
	if morningReport.DigestCarried == 0 {
		t.Fatalf("the digest carried nothing (%+v) — the quiet trigger should have reached it", morningReport)
	}

	// Act 5 — everything the user actually received.
	texts := make([]string, 0)
	for _, m := range channel.Sent(t) {
		texts = append(texts, m.Text)
	}
	all := strings.Join(texts, "\n")
	for _, want := range []string{
		"take the bread out of the oven", // the timer, in the user's own words (no provider bound)
		"later than you asked",           // and it says it was late
		"call the clinic back",           // the push
		"water the plants",               // the digest
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the user never received %q. Everything sent:\n%s", want, all)
		}
	}

	// decision_log tells the whole story.
	rows, err := decisions.Since(ctx, quietHour.Add(-24*time.Hour), -1)
	if err != nil {
		t.Fatalf("reading decision_log: %v", err)
	}
	seen := map[ports.DecisionAction]int{}
	for _, row := range rows {
		seen[row.Action]++
		if strings.TrimSpace(row.Rationale) == "" {
			t.Errorf("a %q row has no rationale — doc 02 §11 requires a sentence a person can read", row.Action)
		}
	}
	for _, want := range []ports.DecisionAction{
		ports.ActionCheckTimerFired,
		ports.ActionCheckTriggerFired,
		ports.ActionCheckTriggerDelivered,
		ports.ActionCheckDigestSent,
	} {
		if seen[want] == 0 {
			t.Errorf("decision_log has no %q row; it holds %v", want, seen)
		}
	}
}

func strPtr(s string) *string { return &s }

// seedUnit stores one live unit, so a trigger's unit_id has a row to
// reference — triggers.unit_id REFERENCES units(id) and the vault opens
// with foreign_keys=on.
//
// It goes through UnitRepo rather than raw SQL, unlike the contract
// suite's own harness. The reason differs with the layer: a contract must
// not depend on another port's behaviour to set one up, but a demo is
// asserting that the whole binary's parts fit together, and reaching past
// one of them to place a fixture would be assembling a vault this code
// could not have produced.
func seedUnit(t *testing.T, db *sqlite.Vault, id string) {
	t.Helper()

	at := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	if err := sqlite.NewUnitRepo(db).Create(context.Background(), unit.Unit{
		ID: id, Type: unit.TypeTask, Content: "demo fixture", Status: unit.StatusPool,
		Weight: 1.0, WeightDecayRate: 0.01, LastTouchedAt: at,
		Source: "chat", CreatedAt: at, UpdatedAt: at,
	}); err != nil {
		t.Fatalf("seeding unit %s: %v", id, err)
	}
}

// demoClock and demoIDs are this file's own fakes: test/e2e has none, and
// the L4 suite deliberately shares no fixtures with test/conformance —
// they are different packages proving different things.
type demoClock struct{ now time.Time }

func (c demoClock) Now() time.Time { return c.now }
