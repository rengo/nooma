//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/fakeprovider"
)

// daysClock is a ports.Clock the walk moves.
type daysClock struct{ now time.Time }

func (c *daysClock) Now() time.Time { return c.now }

// daysIDs is a sequential ports.IDGen with readable ids however many it makes.
type daysIDs struct{ n int }

func (g *daysIDs) New() string { g.n++; return fmt.Sprintf("id-%04d", g.n) }

// daysChannel records each message with the local minute it went out at.
type daysChannel struct {
	clock *daysClock
	zone  *time.Location
	sent  []string
}

func (c *daysChannel) Name() string                                            { return "test" }
func (c *daysChannel) Receive(context.Context) ([]ports.ChannelMessage, error) { return nil, nil }
func (c *daysChannel) Confirm(context.Context, string) error                   { return nil }
func (c *daysChannel) Close() error                                            { return nil }
func (c *daysChannel) Send(_ context.Context, _ ports.ConversationID, text string) error {
	c.sent = append(c.sent, c.clock.now.In(c.zone).Format("Mon 2006-01-02 15:04")+"  "+text)
	return nil
}

// TestRecurringReminder_DaysPassOverSQLite is the conformance days-pass walk
// (I17, doc 02 §7, ADR-0034) over a real migrated vault: a daily 20:00
// reminder captured through the real CaptureService, two weeks of check passes
// every 30 minutes, and the server down four days in the middle. It rings each
// evening, once late on return, and the vault ends with one armed row on the
// one unit: every fire and every expiry re-armed in one transaction.
func TestRecurringReminder_DaysPassOverSQLite(t *testing.T) {
	ctx := context.Background()
	zone := time.FixedZone("UTC-3", -3*60*60)
	start := time.Date(2026, 10, 16, 10, 0, 0, 0, zone)
	downFrom, downUntil := time.Date(2026, 10, 20, 12, 0, 0, 0, zone), time.Date(2026, 10, 24, 21, 0, 0, 0, zone)

	v, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	units, embeddings, triggers := sqlite.NewUnitRepo(v), sqlite.NewEmbeddingRepo(v), sqlite.NewTriggerRepo(v)
	decisions := sqlite.NewDecisionLog(v)
	idx, err := embeddings.LoadIndex(ctx, consolidateITEmbedModel)
	if err != nil {
		t.Fatal(err)
	}

	cases := t.TempDir()
	raw, err := json.Marshal(map[string]string{
		"id": "daily-pills", "provider": "openai", "model": "gpt-4o-mini", "task": "classify", "message": "Take the pills",
		"response": `{"type":"recurring_reminder","normalized_content":"Take the pills","weight":0.6,"decay_rate":0.02,` +
			`"event_at":"2026-10-15T20:00:00-03:00","recurrence_rule":"daily","interrupt_level":0.8}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cases, "daily-pills.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, cases, "daily-pills")

	clock, ids := &daysClock{now: start}, &daysIDs{}
	ch := &daysChannel{clock: clock, zone: zone}
	capture := brain.NewCaptureService(clock, ids, units, embeddings, sqlite.NewSearch(v), sqlite.NewRelationRepo(v), decisions,
		llm, llm, llm, fakeprovider.NewEmbeddingFake(consolidateITEmbedModel), brain.NewIndex(idx), sqlite.NewSignalRepo(v),
		triggers, sqlite.NewTimerRepo(v), 0.5, nil)
	check := brain.NewCheckService(clock, triggers, sqlite.NewTimerRepo(v), ids, decisions, ch, units, sqlite.NewStateRepo(v),
		nil, "12449194", nil, nil)

	result, err := capture.Capture(ctx, brain.CaptureInput{Text: "Take the pills", Channel: "chat"})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	for at := start; at.Before(start.AddDate(0, 0, 14)); at = at.Add(30 * time.Minute) {
		if !at.Before(downFrom) && at.Before(downUntil) {
			continue
		}
		clock.now = at
		if _, err := check.Check(ctx, brain.CheckRequest{}); err != nil {
			t.Fatalf("Check at %v: %v", at, err)
		}
	}

	want := []string{
		"Fri 2026-10-16 20:00  Reminder: Take the pills — Fri 16 Oct",
		"Sat 2026-10-17 20:00  Reminder: Take the pills — Sat 17 Oct",
		"Sun 2026-10-18 20:00  Reminder: Take the pills — Sun 18 Oct",
		"Mon 2026-10-19 20:00  Reminder: Take the pills — Mon 19 Oct",
		"Sat 2026-10-24 21:30  Reminder: Take the pills — Sat 24 Oct (This is later than intended.)",
		"Sun 2026-10-25 20:00  Reminder: Take the pills — Sun 25 Oct",
		"Mon 2026-10-26 20:00  Reminder: Take the pills — Mon 26 Oct",
		"Tue 2026-10-27 20:00  Reminder: Take the pills — Tue 27 Oct",
		"Wed 2026-10-28 20:00  Reminder: Take the pills — Wed 28 Oct",
		"Thu 2026-10-29 20:00  Reminder: Take the pills — Thu 29 Oct",
	}
	if !slices.Equal(ch.sent, want) {
		t.Errorf("sent:\n%s\nwant:\n%s", fmt.Sprint(ch.sent), fmt.Sprint(want))
	}
	armed, err := triggers.ArmedForUnit(ctx, result.UnitID)
	if err != nil || len(armed) != 1 || armed[0].RecurrenceRule == nil ||
		!armed[0].FireAt.Equal(time.Date(2026, 10, 30, 20, 0, 0, 0, zone)) {
		t.Errorf("armed rows of the unit = %+v (%v), want one recurring row for Fri 30 Oct 20:00", armed, err)
	}
	if live, err := triggers.ArmedUnitIDs(ctx); err != nil || !slices.Equal(live, []string{result.UnitID}) {
		t.Errorf("units with an armed row = %v (%v), want only %q", live, err, result.UnitID)
	}
}
