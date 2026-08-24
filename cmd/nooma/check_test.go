package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// TestCheckRefusesMoreThanOneVaultPath is the boundary R6.1 names and the
// one a copy-pasted command loses first: `nooma check a b` must refuse
// before it opens or locks anything, not quietly use the first argument.
//
// A silent no-op here is not a cosmetic defect — it would run a scan
// against a vault the user did not name while reporting success about one
// they did.
func TestCheckRefusesMoreThanOneVaultPath(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	err := runCheck([]string{writeVault(t, ""), writeVault(t, "")}, &out, &errOut)
	if err == nil {
		t.Fatal("runCheck accepted two vault paths")
	}
	if !strings.Contains(err.Error(), "at most one vault path") {
		t.Errorf("error %q does not say how many vault paths are allowed", err)
	}
}

// TestCheckFailsOnAMissingVault: the path is resolved before anything is
// opened, and the failure is config.ResolveVault's own — never a bare
// "file not found" from somewhere deeper.
func TestCheckFailsOnAMissingVault(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "not-a-vault")

	var out, errOut bytes.Buffer
	err := runCheck([]string{missing}, &out, &errOut)
	if err == nil {
		t.Fatal("runCheck succeeded against a vault that does not exist")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the path that was not found", err)
	}
}

// TestCheckFailsCleanlyOnAHeldVault: `nooma check` writes to the vault
// directly, so it takes the write lock itself — runConsolidate's own shape
// — and a held lock fails naming the holder instead of hanging or writing
// concurrently.
func TestCheckFailsCleanlyOnAHeldVault(t *testing.T) {
	t.Parallel()

	vault := writeCheckVault(t)

	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		t.Fatalf("acquiring the vault lock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	var out, errOut bytes.Buffer
	err = runCheck([]string{vault}, &out, &errOut)
	if err == nil {
		t.Fatal("runCheck succeeded against a held vault")
	}
	var inUse *vaultlock.InUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("error %v is not a vaultlock.InUseError — a held vault must say who holds it", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", os.Getpid())) {
		t.Errorf("error %q does not name the holding process", err)
	}
	if !strings.Contains(err.Error(), vault) {
		t.Errorf("error %q does not name the vault it could not take", err)
	}
}

// writeCheckVault builds a vault `nooma check` can actually open: nooma.yml
// plus a migrated database. Unlike `nooma capture`, check reads the store.
func writeCheckVault(t *testing.T) string {
	t.Helper()
	return writeVault(t, "")
}

// TestCheckDryRunTakesTheSameDecisionsAndWritesNothing is owner decision
// Q1's own wording, made checkable: --dry-run suppresses the effect, it
// does not branch the logic.
//
// The fixture is what makes that claim falsifiable. A dry run and a wet
// run are made over the SAME vault, in that order, and the counts they
// report must agree. An implementation that derived the preview from a
// second, independently-written code path — the natural way to build a
// --dry-run — would drift from the real scan the first time either side
// changed, and this fixture is shaped to catch exactly that: same seed,
// same instant's worth of staleness, same numbers.
//
// Between the two runs the vault is read back and must be untouched. That
// is the other half: identical numbers from a preview that had quietly
// already written would prove nothing at all.
func TestCheckDryRunTakesTheSameDecisionsAndWritesNothing(t *testing.T) {
	t.Parallel()

	vault, dbPath := seedCheckVault(t)

	var dryOut, dryErr bytes.Buffer
	if err := runCheck([]string{"--dry-run", vault}, &dryOut, &dryErr); err != nil {
		t.Fatalf("dry run: %v\nstderr: %s", err, dryErr.String())
	}

	// Nothing was written.
	if actions := decisionActions(t, dbPath); len(actions) != 0 {
		t.Errorf("--dry-run wrote %v, want nothing at all", actions)
	}

	// The preview said what it would do, in words that do not claim it did.
	//
	// Only the timer half is asserted unconditionally, and the reason is
	// I15/I16's own interaction: quiet hours are evaluated BEFORE
	// staleness, so during the window an overdue trigger is deferred
	// rather than expired and this scan correctly leaves it alone. A timer
	// is the one push exception to quiet hours (doc 02 §7), so it fires
	// whatever the hour. Asserting the trigger half unconditionally makes
	// this test fail every night between QuietHoursStartHour and
	// QuietHoursEndHour — which is exactly what it did.
	dry := dryOut.String()
	if !strings.Contains(dry, "would fire") {
		t.Errorf("dry-run output %q does not contain %q", dry, "would fire")
	}
	inQuietHours := prospection.InQuietHours(time.Now())
	if inQuietHours {
		if strings.Contains(dry, "would expire") {
			t.Errorf("dry-run output %q says it would expire a trigger during quiet hours — an item is never declared stale inside a window in which it was refused delivery", dry)
		}
	} else if !strings.Contains(dry, "would expire") {
		t.Errorf("dry-run output %q does not contain %q", dry, "would expire")
	}

	var wetOut, wetErr bytes.Buffer
	if err := runCheck([]string{vault}, &wetOut, &wetErr); err != nil {
		t.Fatalf("wet run: %v\nstderr: %s", err, wetErr.String())
	}

	// Same decisions — and this comparison carries the "vault untouched"
	// claim as well as the "same decision path" one. The wet run scans the
	// same vault after the dry run: if the dry run had quietly written,
	// the wet run would find nothing due and report zeros.
	if dryCounts, wetCounts := countsIn(dry), countsIn(wetOut.String()); dryCounts != wetCounts {
		t.Errorf("dry run reported counts %v, wet run reported %v — --dry-run must take the identical decision path, not a second one",
			dryCounts, wetCounts)
	}

	// And this time it happened — to the timer always, and to the trigger
	// only outside quiet hours, for the reason stated above.
	got := decisionActions(t, dbPath)
	want := map[ports.DecisionAction]bool{ports.ActionCheckTimerFired: true}
	if !inQuietHours {
		want[ports.ActionCheckTriggerExpired] = true
	}
	if len(got) != len(want) {
		t.Fatalf("the real run wrote %v, want exactly %d effect(s) (in quiet hours: %v)", got, len(want), inQuietHours)
	}
	for _, action := range got {
		if !want[action] {
			t.Errorf("the real run wrote %q, which is not one of the expected effects", action)
		}
	}
}

// countsIn pulls every integer out of a report, in order, so two reports
// can be compared on their numbers without being pinned to their wording —
// the dry run says "would expire" where the wet one says "expired", and
// that difference is the point rather than a mismatch.
func countsIn(report string) string {
	var digits []string
	for _, field := range strings.Fields(report) {
		if _, err := strconv.Atoi(field); err == nil {
			digits = append(digits, field)
		}
	}
	return strings.Join(digits, ",")
}

// seedCheckVault builds a real, migrated vault holding exactly two rows:
// one trigger overdue past its staleness window, and one timer due right
// now and not yet stale.
//
// Both are required, together. With only the timer the "expired" half of
// every assertion becomes unfalsifiable; with only the trigger, the "fired"
// half does. The scan's two loops are separate code, and a fixture that
// exercises one says nothing about the other.
func seedCheckVault(t *testing.T) (vault, dbPath string) {
	t.Helper()

	vault = writeVault(t, "")
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		t.Fatalf("loadVaultConfig: %v", err)
	}
	dbPath, err = cfg.DatabasePath(vault)
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// The instants are offsets from the real clock, because runCheck reads
	// the system clock and this test cannot inject one. Expressed as
	// multiples of the staleness constants, so a recalibration needs no
	// edit here.
	now := time.Now().UTC()
	// Two days back, not staleness+2h. DeliverableFrom shifts a fire_at
	// that fell inside quiet hours to that day's 07:00, so a
	// staleness+2h offset is only reliably stale at some hours of the
	// day — the same wall-clock fragility G22 recorded. Two days is stale
	// from any shift.
	staleAt := now.Add(-48 * time.Hour)
	dueAt := now.Add(-time.Minute)

	if err := sqlite.NewTriggerRepo(db).Create(ctx, ports.Trigger{
		ID: "trg-stale", Kind: ports.TriggerKindTimeBased, FireAt: &staleAt, CreatedAt: staleAt,
	}); err != nil {
		t.Fatalf("seeding the trigger: %v", err)
	}
	if err := sqlite.NewTimerRepo(db).Create(ctx, ports.Timer{
		ID: "tmr-due", FireAt: dueAt, CreatedAt: dueAt,
	}); err != nil {
		t.Fatalf("seeding the timer: %v", err)
	}

	return vault, dbPath
}

// decisionActions reads the vault's own audit trail — what was written, as
// opposed to what a report claimed was written.
func decisionActions(t *testing.T, dbPath string) []ports.DecisionAction {
	t.Helper()

	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := sqlite.NewDecisionLog(db).Since(context.Background(), time.Now().Add(-24*time.Hour), -1)
	if err != nil {
		t.Fatalf("decision_log Since: %v", err)
	}

	actions := make([]ports.DecisionAction, 0, len(rows))
	for _, row := range rows {
		actions = append(actions, row.Action)
	}
	return actions
}
