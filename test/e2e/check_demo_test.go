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
func TestCheckDemo_FiresATimerAndExpiresATrigger(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "demo.nooma")

	seedDueWork(t, vaultDBPath(t, vault))

	stdout, stderr, err := nooma(t, home, work, "check", vault)
	if err != nil {
		t.Fatalf("check: %v\nstderr: %s", err, stderr)
	}

	for _, want := range []string{"scanned", "expired 1 trigger(s)", "fired 1 timer(s)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("check output does not report %q:\n%s", want, stdout)
		}
	}

	// The story, in the vault's own words.
	rows := readDecisionLog(t, vault)
	byAction := map[ports.DecisionAction]ports.Decision{}
	for _, row := range rows {
		byAction[row.Action] = row
	}

	for _, want := range []ports.DecisionAction{
		ports.ActionCheckTriggerExpired,
		ports.ActionCheckTimerFired,
	} {
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

	if len(rows) != 2 {
		t.Errorf("decision_log holds %d rows, want exactly 2: %v", len(rows), actionsOf(rows))
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
	for _, want := range []string{"dry run", "would expire 1 trigger(s)", "would fire 1 timer(s)"} {
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
	for _, want := range []string{"expired 1 trigger(s)", "fired 1 timer(s)"} {
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
	staleAt := now.Add(-time.Duration(prospection.TriggerStalenessHours+2) * time.Hour)
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
// structural: this change opens no channel and speaks to no network.
//
// It scans the Go source tree for the Telegram transport's own markers —
// the API host and the two methods ADR-0014 names — and for a channel
// implementation under internal/channels beyond its package doc. Telegram
// is m3c's, and a due scan that quietly grew a way to speak would be a
// scope breach this repository has no other gate against.
//
// One limit, stated rather than implied: this reads the tree, not a diff.
// It cannot say "this PR added no channel file" — only that none exists.
// Telegram's CONFIGURATION surface (bot_token_env, allowed_chat_ids) has
// existed in internal/config since M0 and is deliberately not scanned for:
// config declaring a field is not the binary speaking.
func TestCheckDemo_ShipsNoTelegramTransport(t *testing.T) {
	root := repoRootForCheckDemo(t)
	markers := []string{"api.telegram.org", "getUpdates", "sendMessage"}

	scanned := 0
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
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
	if err != nil {
		t.Fatalf("scanning internal/: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned zero Go files — nothing was checked")
	}

	// internal/channels holds its package doc and nothing else yet.
	entries, err := os.ReadDir(filepath.Join(root, "internal", "channels"))
	if err != nil {
		t.Fatalf("reading internal/channels: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "doc.go" {
			t.Errorf("internal/channels holds %s — a channel implementation is m3c's, not this change's", entry.Name())
		}
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
