//go:build e2e

package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

	return occupyPortAnswering(t, "a stranger, not nooma\n")
}

// occupyPortAnswering is occupyPort with a chosen body, so a test can make the
// stranger speak exactly like nooma's GET / document.
func occupyPortAnswering(t *testing.T, body string) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
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

// TestLaunchServe_ForeignListenerImpersonatingNooma_IsNotAccepted pins the
// ownership proof. A stranger answers with nooma's own GET / document, so the
// body alone cannot tell it from our child; only the child's own lock and
// liveness can. The helper must fail naming serve's bind error, not hand the
// stranger's port back as ready.
func TestLaunchServe_ForeignListenerImpersonatingNooma_IsNotAccepted(t *testing.T) {
	t.Parallel()

	port := occupyPortAnswering(t, `{"name":"nooma","version":"impostor","status":"ok"}`+"\n")
	home, vault := serveVaultOn(t, port)

	p, err := launchServe(t, home, vault, port)

	if err == nil {
		t.Fatalf("launchServe accepted a foreign nooma-shaped answer on port %d as ready (pid %d)", port, p.Process.Pid)
	}
	var se *serveStartError
	if !errors.As(err, &se) || !se.bind {
		t.Fatalf("the failure is not classified as a lost bind:\n%v", err)
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("the failure does not carry serve's bind error:\n%v", err)
	}
}

// TestStartServe_ForeignNoomaShapedListener_RetriesOnAFreshPort is the same
// impersonation through the helper every test uses: it must end on a fresh
// port with the real child, not on the stranger's.
func TestStartServe_ForeignNoomaShapedListener_RetriesOnAFreshPort(t *testing.T) {
	t.Parallel()

	taken := occupyPortAnswering(t, `{"name":"nooma","version":"impostor","status":"ok"}`+"\n")
	home, vault := serveVaultOn(t, taken)
	port := taken

	p := startServe(t, home, vault, &port)

	if port == taken {
		t.Fatalf("startServe kept port %d, which a foreign nooma-shaped listener owns", taken)
	}
	if !holdsVault(vault, p.Process.Pid) {
		t.Errorf("the returned serve (pid %d) does not hold the vault lock", p.Process.Pid)
	}
}

// TestIsBindFailure_ClassifiesServeStderr table-tests the decision to retry:
// only the two bind wordings count.
func TestIsBindFailure_ClassifiesServeStderr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"linux bind", "listen tcp 127.0.0.1:1: bind: address already in use", true},
		{"windows bind", "bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.", true},
		{"vault in use", "vault is in use by PID 42", false},
		{"invalid config", "config: http_port: must be between 1 and 65535", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isBindFailure(tt.stderr); got != tt.want {
				t.Errorf("isBindFailure(%q) = %v, want %v", tt.stderr, got, tt.want)
			}
		})
	}
}

// TestRetryable_OnlyALostBindIsRetried: a start that failed for any other
// reason must surface at once, not be masked by a retry on a fresh port.
func TestRetryable_OnlyALostBindIsRetried(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"lost bind", &serveStartError{bind: true}, true},
		{"wrapped lost bind", fmt.Errorf("outer: %w", &serveStartError{bind: true}), true},
		{"other serve failure", &serveStartError{bind: false}, false},
		{"not a serve error", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := retryable(tt.err); got != tt.want {
				t.Errorf("retryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestLaunchServe_VaultLockHeldByAnotherServe_FailsAtOnceAndIsNotRetryable is
// the integration case: a second serve on a vault already served dies on the
// lock, not on a bind, so the retry loop must not touch it.
func TestLaunchServe_VaultLockHeldByAnotherServe_FailsAtOnceAndIsNotRetryable(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	home, vault := serveVaultOn(t, port)
	first := startServe(t, home, vault, &port)

	start := time.Now()
	second, err := launchServe(t, home, vault, freePort(t))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("a second serve on a held vault reported ready (pid %d)", second.Process.Pid)
	}
	if retryable(err) {
		t.Errorf("a held vault lock is classified as a retryable bind failure:\n%v", err)
	}
	if elapsed > serveReadyBudget/2 {
		t.Errorf("failed after %s; a non-bind failure must surface at once", elapsed)
	}
	if !holdsVault(vault, first.Process.Pid) {
		t.Errorf("the first serve (pid %d) lost the vault lock", first.Process.Pid)
	}
}

// TestRetargetPort_RewritesOnlyAWholeLine covers the anchoring: a longer
// number sharing the prefix must be neither matched nor rewritten.
func TestRetargetPort_RewritesOnlyAWholeLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		from int
		to   int
		want string
		fail bool
	}{
		{"exact line", "server:\n  http_port: 8080\n  bind: x\n", 8080, 9000, "server:\n  http_port: 9000\n  bind: x\n", false},
		{"preserves neighbours", "a: 1\nserver:\n  http_port: 80\nb: 2\n", 80, 81, "a: 1\nserver:\n  http_port: 81\nb: 2\n", false},
		{"longer number with same prefix", "server:\n  http_port: 80801\n", 8080, 9000, "", true},
		{"trailing digit after match", "server:\n  http_port: 8080\n  other_http_port: 80801\n", 8080, 9000, "server:\n  http_port: 9000\n  other_http_port: 80801\n", false},
		{"absent", "server:\n  bind: x\n", 8080, 9000, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.fail {
				if portLine(tt.from).MatchString(tt.doc) {
					t.Fatalf("portLine(%d) matched %q, want no match", tt.from, tt.doc)
				}
				return
			}
			vault := t.TempDir()
			if err := os.WriteFile(filepath.Join(vault, "nooma.yml"), []byte(tt.doc), 0o644); err != nil {
				t.Fatal(err)
			}
			retargetPort(t, vault, tt.from, tt.to)
			got, err := os.ReadFile(filepath.Join(vault, "nooma.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("document after retarget:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}
