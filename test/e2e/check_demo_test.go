//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestCheckDemo_FiresATimerAndExpiresATrigger is this change's own exit
// criterion, executable: the compiled binary, a real vault, and
// decision_log telling the story afterwards.
//
// Both fixtures are required together. With only the timer, the "expired"
// half is unfalsifiable; with only the trigger, the "fired" half is. They
// are also each other's control: a scan that expired the timer or fired the
// trigger would satisfy neither assertion, so the two rows pin each other's
// verdict rather than merely coexisting.
//
// The trigger's half is asserted only outside quiet hours — see the
// comment at the assertion. Inside the window the assertion inverts rather
// than being skipped: the trigger must NOT have expired.
func TestCheckDemo_FiresATimerAndExpiresATrigger(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "demo.nooma")

	seedDueWork(t, vaultDBPath(t, vault))

	stdout, stderr, err := nooma(t, home, work, "check", vault)
	if err != nil {
		t.Fatalf("check: %v\nstderr: %s", err, stderr)
	}

	// The timer half holds at any hour; the trigger half does not, and the
	// reason is I15/I16's interaction. Quiet hours are evaluated BEFORE
	// staleness, so inside the window an overdue trigger is deferred
	// rather than expired and the scan correctly leaves it armed. A timer
	// is the one push exception to quiet hours (doc 02 §7).
	//
	// This test asserted both unconditionally when it was written, which
	// made it fail every night between QuietHoursStartHour and
	// QuietHoursEndHour — found by CI at 01:45 UTC, not by the author at
	// his desk in the afternoon.
	inQuietHours := prospection.InQuietHours(time.Now())

	for _, want := range []string{"scanned", "fired 1 timer(s)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("check output does not report %q:\n%s", want, stdout)
		}
	}
	if inQuietHours {
		if strings.Contains(stdout, "expired") {
			t.Errorf("check expired a trigger during quiet hours:\n%s", stdout)
		}
	} else if !strings.Contains(stdout, "expired 1 trigger(s)") {
		t.Errorf("check output does not report %q:\n%s", "expired 1 trigger(s)", stdout)
	}

	// The story, in the vault's own words.
	rows := readDecisionLog(t, vault)
	byAction := map[ports.DecisionAction]ports.Decision{}
	for _, row := range rows {
		byAction[row.Action] = row
	}

	wantActions := []ports.DecisionAction{ports.ActionCheckTimerFired}
	if !inQuietHours {
		wantActions = append(wantActions, ports.ActionCheckTriggerExpired)
	}
	for _, want := range wantActions {
		row, found := byAction[want]
		if !found {
			t.Errorf("decision_log has no %q row; it holds %v", want, actionsOf(rows))
			continue
		}
		if strings.TrimSpace(row.Rationale) == "" {
			t.Errorf("%q row has an empty Rationale — doc 02 §11 requires a human-readable sentence", want)
		}
		if len(row.Context) == 0 {
			t.Errorf("%q row has an empty Context", want)
		}
	}

	if len(rows) != len(wantActions) {
		t.Errorf("decision_log holds %d rows, want exactly %d (in quiet hours: %v): %v",
			len(rows), len(wantActions), inQuietHours, actionsOf(rows))
	}
}

// TestCheckDemo_DryRunChangesNothing is owner decision Q1 at the outermost
// layer: the shipped binary, the real flag, and a vault that is provably
// untouched afterwards — proved by running the real scan next and watching
// it still find both rows.
func TestCheckDemo_DryRunChangesNothing(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "dry.nooma")
	seedDueWork(t, vaultDBPath(t, vault))

	stdout, stderr, err := nooma(t, home, work, "check", "--dry-run", vault)
	if err != nil {
		t.Fatalf("check --dry-run: %v\nstderr: %s", err, stderr)
	}
	inQuietHours := prospection.InQuietHours(time.Now())
	wantLines := []string{"dry run", "would fire 1 timer(s)"}
	if !inQuietHours {
		wantLines = append(wantLines, "would expire 1 trigger(s)")
	}
	for _, want := range wantLines {
		if !strings.Contains(stdout, want) {
			t.Errorf("dry-run output does not report %q:\n%s", want, stdout)
		}
	}
	if rows := readDecisionLog(t, vault); len(rows) != 0 {
		t.Fatalf("--dry-run wrote %v to decision_log, want nothing", actionsOf(rows))
	}

	// The real run finds exactly the same work waiting, which it could
	// only do if the preview wrote nothing.
	wet, stderr, err := nooma(t, home, work, "check", vault)
	if err != nil {
		t.Fatalf("check: %v\nstderr: %s", err, stderr)
	}
	wantWet := []string{"fired 1 timer(s)"}
	if !inQuietHours {
		wantWet = append(wantWet, "expired 1 trigger(s)")
	}
	for _, want := range wantWet {
		if !strings.Contains(wet, want) {
			t.Errorf("the real run does not report %q — the dry run did not leave the vault alone:\n%s", want, wet)
		}
	}
}

// seedDueWork writes one trigger overdue past its staleness window and one
// timer due a minute ago into an already-initialised vault.
//
// The instants are offsets from the real clock because the shipped binary
// reads the system clock and no test can inject one into it — which is the
// point of running at this layer. They are multiples of the staleness
// constants, so a recalibration needs no edit here.
func seedDueWork(t *testing.T, dbPath string) {
	t.Helper()

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("the initialised vault has no database at %s: %v", dbPath, err)
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	// Two days back, not staleness+2h. DeliverableFrom shifts a fire_at
	// that fell inside quiet hours to that day's 07:00, so a
	// staleness+2h offset is only reliably stale at some hours of the
	// day. Two days is stale from any shift — the same wall-clock
	// fragility G22 recorded, met again at a different offset.
	staleAt := now.Add(-48 * time.Hour)
	dueAt := now.Add(-time.Minute)

	if err := sqlite.NewTriggerRepo(db).Create(ctx, ports.Trigger{
		ID: "trg-overdue", Kind: ports.TriggerKindTimeBased, FireAt: &staleAt, CreatedAt: staleAt,
	}); err != nil {
		t.Fatalf("seeding the trigger: %v", err)
	}
	if err := sqlite.NewTimerRepo(db).Create(ctx, ports.Timer{
		ID: "tmr-due", FireAt: dueAt, CreatedAt: dueAt,
	}); err != nil {
		t.Fatalf("seeding the timer: %v", err)
	}
}

func actionsOf(rows []ports.Decision) []ports.DecisionAction {
	actions := make([]ports.DecisionAction, 0, len(rows))
	for _, row := range rows {
		actions = append(actions, row.Action)
	}
	return actions
}

// TestCheckDemo_ShipsNoTelegramTransport is §9's boundary note, made
// structural — narrowed by m3c, and the narrowing is the point.
//
// As m3b wrote it, this scanned all of internal/** for the Telegram
// transport's markers and asserted zero occurrences, because m3b opened no
// channel. m3c opens one, so that claim is now false by design and the
// test would fail for the right reason on correct code.
//
// **Narrowed rather than deleted**, because the half still worth holding
// is the one that was always the real subject: a channel adapter existing
// must not mean the DECISION layer learned a vendor's name. So the scan
// keeps internal/brain, internal/scheduler and internal/core — where doc
// 02:653's claim lives — and gives up internal/channels, where naming
// Telegram is the whole job.
//
// The host literal itself moved out of this file for the same reason it
// moved out of every test file: telegram_host_literal_test.go asserts the
// literal appears in NO _test.go anywhere, and this file used to be the
// one exception. The markers below are assembled from parts, as that test
// does, and its doc comment explains why the seam is correct there.
//
// Two limits, stated rather than implied. This reads the tree, not a diff:
// it cannot say "this PR added no channel file", only that none exists in
// the paths it covers. And Telegram's CONFIGURATION surface
// (bot_token_env, allowed_chat_ids) has been in internal/config since M0
// and is deliberately not scanned for — config declaring a field is not
// the binary speaking.
func TestCheckDemo_ShipsNoTelegramTransport(t *testing.T) {
	root := repoRootForCheckDemo(t)
	markers := []string{"api." + "telegram" + ".org", "get" + "Updates", "send" + "Message"}

	scanned := 0
	walk := func(dir string) error {
		return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			for _, marker := range markers {
				if strings.Contains(string(body), marker) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s mentions %q — the Telegram transport is m3c's, and this change opens no channel", rel, marker)
				}
			}
			return nil
		})
	}

	// The decision layer, and only it. internal/channels is deliberately
	// absent — see this test's doc comment.
	for _, rel := range []string{"brain", "scheduler", "core"} {
		if err := walk(filepath.Join(root, "internal", rel)); err != nil {
			t.Fatalf("scanning internal/%s: %v", rel, err)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned zero Go files — nothing was checked")
	}
}

// repoRootForCheckDemo walks up from this file to the module root.
func repoRootForCheckDemo(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the test's working directory")
		}
		dir = parent
	}
}
