package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// init registers "check" into main.go's dispatch table —
// consolidate.go:24-29's exact pattern.
func init() {
	commands["check"] = command{
		summary: "scan a vault for due triggers and timers",
		run:     runCheck,
	}
}

// runCheck is `nooma check [vault]`. It writes to the vault directly, so it
// takes the write lock itself before opening the store and fails naming the
// holder rather than hanging or writing concurrently — runConsolidate's own
// shape.
//
// Two differences from runConsolidate are deliberate, and named here
// against reflexive copy-paste:
//
//   - **No pre-lock provider resolution.** consolidate refuses before
//     taking the lock when a task it needs is unbound, because a pass that
//     silently skipped a phase would still stamp consolidation_last_run_at
//     as though it had run in full. A scan calls no model at all: there is
//     nothing to resolve and nothing to be half-done.
//   - **No --phase-style vocabulary flag.** A scan has no phases, so there
//     is no second entry point from untrusted CLI text into a closed
//     vocabulary here. --dry-run is a plain boolean, which is the whole of
//     this command's untrusted-input surface beyond the vault path itself.
func runCheck(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var dryRun bool
	fs.BoolVar(&dryRun, "dry-run", false, "report what the scan would do without writing anything")
	fs.Usage = func() { _, _ = fmt.Fprint(errOut, "usage: nooma check [--dry-run] [vault]\n") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("check takes at most one vault path, got %d", fs.NArg())
	}

	vault, err := config.ResolveVault(fs.Arg(0))
	if err != nil {
		return err
	}
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return err
	}

	// A dry run takes the lock too. It reads the same rows a writing scan
	// reads, and a preview computed against a vault another process is
	// halfway through changing is a preview of nothing.
	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		var inUse *vaultlock.InUseError
		if errors.As(err, &inUse) {
			return err
		}
		return fmt.Errorf("taking the vault lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		return err
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	report, err := wireCheck(db).Check(ctx, brain.CheckRequest{DryRun: dryRun})
	if err != nil {
		return err
	}

	return renderCheckReport(out, report, dryRun)
}

// renderCheckReport prints what the scan did, or would have done.
//
// One unconditional scanned-count line, then one line per non-empty
// outcome and silence otherwise — renderConsolidateReport's own posture. A
// scan that found nothing is the ordinary case, and printing "expired 0
// triggers" every few minutes would train the eye to skip the line that
// matters.
//
// The verbs change with the mode and the numbers do not. "expired" and
// "would expire" are different claims, and a preview that said "expired"
// would be lying about the vault.
func renderCheckReport(out io.Writer, report brain.CheckReport, dryRun bool) error {
	suffix := ""
	if dryRun {
		suffix = " (dry run — nothing was written)"
	}
	if _, err := fmt.Fprintf(out, "check: scanned %d trigger(s) and %d timer(s)%s\n",
		report.TriggersDue, report.TimersDue, suffix); err != nil {
		return err
	}

	for _, line := range []struct {
		count int
		wet   string
		dry   string
	}{
		{report.TriggersExpired, "expired %d trigger(s)", "would expire %d trigger(s)"},
		{report.TimersFired, "fired %d timer(s)", "would fire %d timer(s)"},
		{report.TimersCancelled, "cancelled %d timer(s)", "would cancel %d timer(s)"},
	} {
		if line.count == 0 {
			continue
		}
		format := line.wet
		if dryRun {
			format = line.dry
		}
		if _, err := fmt.Fprintf(out, "check: "+format+"\n", line.count); err != nil {
			return err
		}
	}
	return nil
}
