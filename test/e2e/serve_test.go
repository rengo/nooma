//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/store/vaultlock"
)

// freePort asks the kernel for a port and gives it straight back, so the test
// configures a port nothing else is using. There is a race in principle — the
// port could be taken between the close and the server's bind — but the
// alternative is a hardcoded port, which races with every other test and with
// whatever the developer happens to be running.
func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// startServe launches `nooma serve` and waits until it actually answers.
//
// Waiting for a response rather than sleeping is what keeps this from being flaky
// by construction: the test proceeds when the server is genuinely up, or fails
// saying it never came up.
func startServe(t *testing.T, home, vault string, port int) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(binaryPath(t), "serve", vault)
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "NOOMA_VAULT=")
	var errOut strings.Builder
	cmd.Stderr = &errOut
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err == nil {
			_ = resp.Body.Close()
			return cmd
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("serve never answered on port %d\nstderr: %s", port, errOut.String())
	return nil
}

func writeConfig(t *testing.T, vault, document string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(vault, "nooma.yml"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestServeAnswersBothSurfaces is spec R11.1 end to end: the compiled binary,
// a real vault, a real socket.
func TestServeAnswersBothSurfaces(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n", port))

	startServe(t, home, vault, port)

	for _, path := range []string{"/", "/ui"} {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// TestServeHoldsTheWriteLock is spec R8.1 and R8.2 from the outside: while serve
// runs it holds the vault, and a second serve refuses by naming the holder.
func TestServeHoldsTheWriteLock(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n", port))

	cmd := startServe(t, home, vault, port)

	pid, held, err := vaultlock.ReadHolder(vault)
	if err != nil {
		t.Fatalf("ReadHolder: %v", err)
	}
	if !held || pid != cmd.Process.Pid {
		t.Errorf("the vault reports holder (pid=%d held=%v), want the serving process %d", pid, held, cmd.Process.Pid)
	}

	// A second serve must refuse, and say who has it.
	second := exec.Command(binaryPath(t), "serve", vault)
	second.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "NOOMA_VAULT=")
	var errOut strings.Builder
	second.Stderr = &errOut
	if err := second.Run(); err == nil {
		t.Fatal("a second serve started against a held vault")
	}
	if !strings.Contains(errOut.String(), fmt.Sprint(cmd.Process.Pid)) {
		t.Errorf("the refusal does not name the holding PID %d:\n%s", cmd.Process.Pid, errOut.String())
	}
}

// TestServeReleasesTheLockOnSignal is spec R11.5 and R8.1's other half. The
// kernel would release the lock on exit anyway; this asserts the process exits
// CLEANLY — status zero — so a supervisor does not read a normal shutdown as a
// crash.
func TestServeReleasesTheLockOnSignal(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n", port))

	cmd := startServe(t, home, vault, port)

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("serve exited non-zero on SIGINT: %v", err)
	}

	if _, held, err := vaultlock.ReadHolder(vault); err != nil {
		t.Fatalf("ReadHolder after shutdown: %v", err)
	} else if held {
		t.Error("the vault is still reported as held after a clean shutdown")
	}
}

// TestServeRefusesToExposeWithoutAToken is spec R11.2, and the second assertion
// is the one that matters: nothing may be listening afterwards. A server that
// binds and then complains has already exposed the port.
func TestServeRefusesToExposeWithoutAToken(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 0.0.0.0\n  http_port: %d\n", port))

	_, stderr, err := nooma(t, home, work, "serve", vault)
	if err == nil {
		t.Fatal("serve started on a non-loopback bind with no auth token")
	}
	if !strings.Contains(stderr, "auth_token_env") {
		t.Errorf("the refusal does not name what the user must set:\n%s", stderr)
	}

	// Nothing may have been opened, even briefly.
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Errorf("something is listening on port %d after serve refused to start", port)
	}
}
