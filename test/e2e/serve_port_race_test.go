//go:build e2e

package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// occupyPort stands a foreign HTTP listener on a kernel-chosen loopback port,
// reproducing deterministically what the freePort window allows by chance: a
// stranger answering on the port a test is about to hand to `nooma serve`. Its
// body is deliberately not nooma's GET / document.
func occupyPort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "a stranger, not nooma\n")
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return l.Addr().(*net.TCPAddr).Port
}

// serveVaultOn builds an isolated vault whose config points serve at port.
func serveVaultOn(t *testing.T, port int) (home, vault string) {
	t.Helper()

	home, work := t.TempDir(), t.TempDir()
	vault = initVault(t, home, work, "pablo.nooma")
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n", port))
	return home, vault
}

// TestLaunchServe_ForeignListenerOnThePort_FailsFastNamingTheBindError is the
// flake's mechanism pinned down: serve loses its port, dies on `bind: address
// already in use`, and a stranger keeps answering on that port. Readiness used to
// accept the stranger's answer (or poll until a late, misleading failure); it
// must now report the dead child at once, with serve's own stderr.
func TestLaunchServe_ForeignListenerOnThePort_FailsFastNamingTheBindError(t *testing.T) {
	t.Parallel()

	port := occupyPort(t)
	home, vault := serveVaultOn(t, port)

	start := time.Now()
	p, err := launchServe(t, home, vault, port)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("launchServe reported ready (pid %d) although a foreign listener owns port %d", p.Process.Pid, port)
	}
	var se *serveStartError
	if !errors.As(err, &se) || !se.bind {
		t.Fatalf("the failure is not classified as a lost bind:\n%v", err)
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("the failure does not carry serve's bind error:\n%v", err)
	}
	if elapsed > serveReadyBudget/2 {
		t.Errorf("failed after %s; a dead child must fail fast, not wait out the %s readiness budget", elapsed, serveReadyBudget)
	}
}

// TestStartServe_ForeignListenerOnThePort_RetriesOnAFreshPort is the recovery:
// the same collision, through the helper every test uses, ends with a serve that
// is really up on a different port, the vault's config pointing at it.
func TestStartServe_ForeignListenerOnThePort_RetriesOnAFreshPort(t *testing.T) {
	t.Parallel()

	taken := occupyPort(t)
	home, vault := serveVaultOn(t, taken)
	port := taken

	p := startServe(t, home, vault, &port)

	if port == taken {
		t.Fatalf("startServe kept port %d, which a foreign listener owns", taken)
	}
	select {
	case <-p.exited:
		t.Fatalf("the returned serve is not running: %v\nstderr: %s", p.waitErr, p.stderr.String())
	default:
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatalf("GET / on the retried port %d: %v", port, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Name != "nooma" {
		t.Errorf("port %d is not answered by nooma (name=%q, err=%v)", port, body.Name, err)
	}
}
