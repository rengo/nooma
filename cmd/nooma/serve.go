package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// shutdownGrace is how long an in-flight request has to finish once a signal
// arrives. Long enough that a slow response is not cut off, short enough that a
// supervisor restarting the service does not wait on a hung connection.
const shutdownGrace = 10 * time.Second

// runServe starts everything M0 has: the HTTP API and the UI placeholder, over a
// vault this process holds exclusively.
//
// The order of the first four steps is the whole safety of this command, and each
// step exists to fail before the next one can do damage:
//
//  1. resolve the vault, so the rest talks about a known place;
//  2. load and validate its configuration;
//  3. DECIDE the binding — ADR-0007's refusal happens here, before any socket
//     exists, because a server that binds and then complains has already exposed
//     the port;
//  4. take the write lock, so a second serve refuses instead of two processes
//     writing to one brain.
//
// Only then does anything open.
func runServe(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { _, _ = fmt.Fprint(errOut, "usage: nooma serve [vault]\n") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("serve takes at most one vault path, got %d", fs.NArg())
	}

	vault, err := config.ResolveVault(fs.Arg(0))
	if err != nil {
		return err
	}

	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return err
	}
	if err := cfg.Validate(vault, os.LookupEnv); err != nil {
		return err
	}

	addr, err := httpapi.DecideBinding(cfg, os.LookupEnv)
	if err != nil {
		return err
	}
	token, _ := httpapi.ResolveToken(cfg, os.LookupEnv)

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
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// wireBrain resolves config -> providers -> repos -> Index -> services
	// (design D10, D18a) once, here, at vault open — not reconstructed per
	// request. Capture and Recall come back nil, with no error, whenever the
	// vault's own tasks:/providers: cannot resolve all of tasksM1Consumes
	// (cmd/nooma/tasks.go) — an unconfigured or half-configured vault, not a
	// startup failure. A nil Capture or Recall reaching a live request is
	// still not a crash: captureHandler/recallHandler/unitByIDHandler each
	// check for it and answer 503, so this binary honestly reports "not
	// wired yet" instead of taking the process down for the caller who did
	// everything right.
	capture, recall, err := wireBrain(context.Background(), db, cfg, os.LookupEnv)
	if err != nil {
		return fmt.Errorf("wiring the capture/recall pipeline: %w", err)
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.Handler(httpapi.Deps{Version: buildString(), Capture: capture, Recall: recall, Token: token}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Signals are registered before the listener starts, so a signal arriving
	// during startup is not lost.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		_, _ = fmt.Fprintf(out, "nooma serving %s on http://%s\n", vault, addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	// A clean shutdown, and the exit status says so. The kernel would release the
	// lock and close the database on exit anyway; doing it explicitly is what lets
	// a supervisor tell a normal stop from a crash, and what lets the test observe
	// a released lock rather than a released-by-luck one.
	_, _ = fmt.Fprintln(out, "\nshutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	return <-errc
}
