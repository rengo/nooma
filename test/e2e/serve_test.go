//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
func startServe(t *testing.T, home, vault string, port int, extraArgs ...string) *exec.Cmd {
	t.Helper()

	args := append([]string{"serve"}, extraArgs...)
	args = append(args, vault)
	cmd := exec.Command(binaryPath(t), args...)
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

// TestServeNoUI is spec R4's exit criterion, end to end: `--no-ui` and
// `server.ui: false` each unmount `/ui` on the compiled binary, with
// `POST /capture` unaffected (design m4a §3.9, §7's PR 3 row).
//
// The third case composes R4's second arm end to end: `--no-ui` AND a token
// configured together, which the first two cases never combine (m4a
// tasks.md's PR 3 Verify block named this gap; both reviewers had to verify
// it by hand instead of a test proving it). Mutation that third case
// catches: `runServe` not passing the resolved token through to
// `httpapi.Deps` when `noUI` is true — `POST /capture` would then wrongly
// answer 401 for a valid token, because `requireToken` would have become a
// no-op instead of checking it; or the unmounted `/ui` falling through to
// the mux's own 404 instead of `requireToken`'s 401 for that token state
// (spec R4, `server.go`'s existing token-state truth table).
func TestServeNoUI(t *testing.T) {
	cases := []struct {
		name      string
		config    string
		extraArgs []string
		token     string
	}{
		{
			name:      "--no-ui flag",
			config:    "server:\n  bind: 127.0.0.1\n  http_port: %d\n",
			extraArgs: []string{"--no-ui"},
		},
		{
			name:   "server.ui: false",
			config: "server:\n  bind: 127.0.0.1\n  http_port: %d\n  ui: false\n",
		},
		{
			name:      "--no-ui with a token configured",
			config:    "server:\n  bind: 127.0.0.1\n  http_port: %d\n  auth_token_env: NOOMA_SERVE_NO_UI_TEST_TOKEN\n",
			extraArgs: []string{"--no-ui"},
			token:     "no-ui-e2e-token",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.token != "" {
				t.Setenv("NOOMA_SERVE_NO_UI_TEST_TOKEN", tc.token)
			}

			home, work := t.TempDir(), t.TempDir()
			vault := initVault(t, home, work, "pablo.nooma")
			port := freePort(t)
			writeConfig(t, vault, fmt.Sprintf(tc.config, port))

			startServe(t, home, vault, port, tc.extraArgs...)

			uiResp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ui", port))
			if err != nil {
				t.Fatalf("GET /ui: %v", err)
			}
			defer func() { _ = uiResp.Body.Close() }()
			wantUIStatus := http.StatusNotFound
			if tc.token != "" {
				wantUIStatus = http.StatusUnauthorized
			}
			if uiResp.StatusCode != wantUIStatus {
				t.Errorf("GET /ui = %d, want %d (unmounted, same status an unknown path gets for this token state)", uiResp.StatusCode, wantUIStatus)
			}

			captureBody, err := json.Marshal(map[string]string{"text": "pick up the dry cleaning"})
			if err != nil {
				t.Fatal(err)
			}
			captureReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/capture", port), bytes.NewReader(captureBody))
			if err != nil {
				t.Fatal(err)
			}
			captureReq.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				captureReq.Header.Set("Authorization", "Bearer "+tc.token)
			}
			captureResp, err := http.DefaultClient.Do(captureReq)
			if err != nil {
				t.Fatalf("POST /capture: %v", err)
			}
			defer func() { _ = captureResp.Body.Close() }()
			if tc.token != "" {
				// The API still behaves under --no-ui with a token
				// configured: a valid token must not be rejected. A 503
				// (no providers configured) is expected and fine — 401
				// is the only wrong answer here.
				if captureResp.StatusCode == http.StatusUnauthorized {
					t.Errorf("POST /capture with a valid token = 401, want anything but 401 (--no-ui must not drop the token)")
				}
			} else if captureResp.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("POST /capture = %d, want %d (Capture is nil — no providers configured; --no-ui must not change this)", captureResp.StatusCode, http.StatusServiceUnavailable)
			}
		})
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
	// Not skipped because the behavior is absent on Windows — it is
	// skipped because this harness cannot deliver the signal there.
	// os.Process.Signal returns "not supported by windows" for anything
	// but Kill: Windows has no POSIX signals, and a console Ctrl+C is
	// delivered with GenerateConsoleCtrlEvent to a process GROUP, which
	// means creating the child in its own group and calling into
	// golang.org/x/sys/windows to raise the event. Killing it instead
	// would assert nothing — this test exists to prove the exit is CLEAN,
	// and a killed process never exits cleanly.
	//
	// What stays unverified on Windows, stated rather than implied: that
	// `nooma serve` shuts down with status zero and releases the vault
	// lock on Ctrl+C (spec R11.5, R8.1). Recorded as debt in tasks.md
	// §7.7. Everything else about the lock — that it is taken, that a
	// second serve is refused and names the holder — is covered by the
	// tests above, which do run here.
	if runtime.GOOS == "windows" {
		t.Skip("os.Process.Signal cannot deliver an interrupt to another process on windows; see the comment above for what this leaves unverified")
	}

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
