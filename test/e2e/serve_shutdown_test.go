//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/scheduler"
)

// shutdownGraceBudget mirrors cmd/nooma/serve.go's own unexported
// shutdownGrace (10s) — restated here, not imported, since it is a
// package-private const in `package main` and this file lives in a
// separate process boundary by construction (test/e2e drives the compiled
// binary as a subprocess, never cmd/nooma directly). shutdownGraceCeiling
// adds a margin for process-spawn/OS-scheduling overhead this fixture
// cannot eliminate — the assertion below is "exits within shutdownGrace",
// not "exits in exactly shutdownGrace". waitBudget is a much larger,
// separate bound used only to fail every blocking wait in this file
// cleanly and quickly (seconds to tens of seconds) instead of ever
// wedging on the harness's own multi-minute test timeout — every channel
// receive and every process wait below is guarded by one of these two
// bounds, never left unbounded.
const (
	shutdownGraceBudget  = 10 * time.Second
	shutdownGraceCeiling = shutdownGraceBudget + 5*time.Second
	waitBudget           = shutdownGraceCeiling + 20*time.Second
)

// blockingConsolidateLLM wraps an upstream mockConsolidateLLM server
// (proxied through, unchanged) with a one-shot block: once armed, the
// FIRST request it receives after arming does not answer at all — it
// blocks on that request's own r.Context() (bounded by waitBudget, so a
// missing cancellation signal fails the test instead of wedging this
// handler, and this server's own Close in t.Cleanup, forever) and signals
// cancelled the moment r.Context().Done() fires, matching design §3.5's
// own "cancellation is not instantaneous" caution: this is the fake
// observing cancellation directly at the network layer, not the test
// guessing a sleep duration or racing a real SQL close. Every other
// request — before arming, and every one after the first blocked one — is
// proxied straight through, so a fixture's own seeding capture (which
// reuses this same URL, per consolidateConfig binding all four tasks to
// one provider) is never blocked, and neither is a follow-up pass run
// after the blocked call unblocks.
type blockingConsolidateLLM struct {
	srv       *httptest.Server
	armed     atomic.Bool
	triggered atomic.Bool
	started   chan struct{}
	cancelled chan struct{}
}

func newBlockingConsolidateLLM(t *testing.T, upstream string) *blockingConsolidateLLM {
	t.Helper()

	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstreamURL)

	b := &blockingConsolidateLLM{
		started:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.armed.Load() && b.triggered.CompareAndSwap(false, true) {
			// Go's net/http server only starts watching a connection for an
			// early client close (the mechanism that cancels r.Context())
			// once the request body is fully drained — verified directly
			// with a standalone probe: without this drain, r.Context()
			// never cancels at all, even 10+ seconds after the client side
			// aborts, and httptest.Server.Close later reports the
			// connection permanently "active". With it, cancellation is
			// observed in well under a millisecond after the client
			// cancels. ollama.Client's own JSON body is small and already
			// fully sent by the time this handler runs, so draining it
			// here has no effect on the request this fixture cares about.
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			close(b.started)
			select {
			case <-r.Context().Done():
				close(b.cancelled)
			case <-time.After(waitBudget):
				// Bounded deliberately: the test's own bounded wait on
				// b.cancelled (not this select) is what reports a missing
				// cancellation signal as a test failure. This branch only
				// exists so THIS goroutine, and this server's Close in
				// t.Cleanup, cannot hang past waitBudget regardless.
			}
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(b.srv.Close)
	return b
}

// waitForExit blocks on cmd.Wait() for at most waitBudget. A real hang —
// the child process never exiting — fails the test with a clear message
// in low tens of seconds instead of consuming the whole test binary's
// multi-minute timeout with no diagnosis, and force-kills the child so no
// process is left running past the failing test.
func waitForExit(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(waitBudget):
		_ = cmd.Process.Kill()
		t.Fatalf("serve did not exit within %s of SIGTERM — force-killed; this is a hang, not the shutdownGrace timeout the test is measuring", waitBudget)
		return nil // unreachable: t.Fatalf stops this goroutine
	}
}

// sigtermFixture is what sigtermMidPass hands back: everything the three
// TestServe_SIGTERM_* tests below need, so each stays a short, honestly
// separate top-level test (matching tasks.md's own 7.1/7.3/7.4 naming)
// without three independent copies of the same real-process dance.
type sigtermFixture struct {
	home, work, vault string
	blocking          *blockingConsolidateLLM
	cmd               *exec.Cmd
	errOut            *strings.Builder
	sentAt            time.Time
}

// sigtermMidPass starts a fresh vault, seeds one captured unit through a
// throwaway first `serve` instance (killed well before its own boot
// catch-up could fire), then starts a SECOND `serve` process against the
// same vault. consolidation_last_run_at was never written, so CatchUpDue
// (m2d R2.1) is still true for this second process, and its own boot
// catch-up fires BootConsolidationDelay (120s) after ITS OWN start — the
// only trigger this fixture can reach without a real 03:00 local clock or
// a way to override either scheduler constant, neither of which exists
// (both are `internal/scheduler` package consts, not configurable). The
// fake LLM is armed only once the second process starts, so the seeding
// capture above is never blocked. Once the fake reports the pass reached
// a real LLM call (blocking.started), SIGTERM is sent — this is what makes
// "a pass in flight" an observed state, not a guessed sleep duration.
//
// Cost, disclosed rather than hidden: this fixture's own wait for the
// boot catch-up delay makes every test built on it take a little over two
// real minutes. No shorter path exists inside this PR's own diff scope
// (cmd/nooma/serve.go plus this file) — internal/scheduler's constants are
// deliberately not test-configurable (design §5.1's "the constants"), and
// making them so is out of this link's scope.
func sigtermMidPass(t *testing.T) *sigtermFixture {
	t.Helper()

	llm := mockConsolidateLLM(t)
	blocking := newBlockingConsolidateLLM(t, llm.URL)

	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n%s", port, consolidateConfig(blocking.srv.URL)))

	seed := startServe(t, home, vault, port)
	stdout, stderr, err := nooma(t, home, work, "capture", "Pick up the dry cleaning on Friday", vault)
	if err != nil {
		t.Fatalf("seeding capture: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	_ = seed.Process.Kill()
	_ = seed.Wait()

	blocking.armed.Store(true)

	cmd, errOut := startServeCapturingStderr(t, home, vault, port)

	select {
	case <-blocking.started:
	case <-time.After(scheduler.BootConsolidationDelay + 30*time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("the boot catch-up pass never reached the fake LLM within %s of starting serve\nstderr: %s", scheduler.BootConsolidationDelay+30*time.Second, errOut.String())
	}

	sentAt := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	return &sigtermFixture{home: home, work: work, vault: vault, blocking: blocking, cmd: cmd, errOut: errOut, sentAt: sentAt}
}

// TestServe_SIGTERM_PassInFlight_ExitsWithinGrace is spec R3.3 / design
// §3.5 (D5) end to end (tasks.md 7.1): SIGTERM sent while the boot
// catch-up pass is blocked inside a real outbound HTTP call must exit the
// process within shutdownGrace, the fake LLM must observe ctx cancellation
// on that in-flight request, and — the property that actually
// distinguishes a joined shutdown from an unjoined one — the scheduler's
// own abort line must have reached the process log BEFORE the process
// exits, not raced away by it.
func TestServe_SIGTERM_PassInFlight_ExitsWithinGrace(t *testing.T) {
	f := sigtermMidPass(t)

	waitErr := waitForExit(t, f.cmd)
	elapsed := time.Since(f.sentAt)

	if waitErr != nil {
		t.Errorf("serve exited non-zero after SIGTERM mid-pass: %v\nstderr: %s", waitErr, f.errOut.String())
	}
	if elapsed > shutdownGraceCeiling {
		t.Errorf("serve took %s to exit after SIGTERM with a pass in flight, want <= %s (shutdownGrace + margin)", elapsed, shutdownGraceCeiling)
	}

	select {
	case <-f.blocking.cancelled:
	case <-time.After(shutdownGraceCeiling):
		t.Error("the fake LLM's in-flight request never observed ctx cancellation")
	}

	if !strings.Contains(f.errOut.String(), "scheduler: pass aborted (catchup):") {
		t.Errorf("runServe exited without the scheduler's own abort line reaching the process log — the pass goroutine was not joined before the vault closed:\n%s", f.errOut.String())
	}
}
