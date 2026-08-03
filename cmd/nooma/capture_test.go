package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// writeVault creates a minimal vault — just nooma.yml, nothing else —
// enough for config.ResolveVault/loadVaultConfig, the same two calls
// runCapture makes. A real vault carries .env, nooma.db and more, but
// `nooma capture` reads none of that (spec R3.1's own MUST NOT: it never
// opens the database).
func writeVault(t *testing.T, document string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.ConfigFileName), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// splitHostPort pulls the host and port an httptest.Server actually bound,
// so a test's own nooma.yml can point at it precisely.
func splitHostPort(t *testing.T, rawURL string) (host, port string) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname(), u.Port()
}

// freeTCPPort asks the kernel for a port and gives it back immediately, so a
// test can point `nooma capture` at an address nothing is listening on.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// TestCaptureIsDispatched is 14a.1's first half: `cmd/nooma` gains a
// `capture` subcommand in main.go's dispatch table, following
// init/status/doctor/serve's own convention — usage cannot drift from
// reality because it is generated from this table (main.go's own doc
// comment), so adding the entry is what makes both true at once.
func TestCaptureIsDispatched(t *testing.T) {
	t.Parallel()
	cmd, ok := commands["capture"]
	if !ok {
		t.Fatal(`commands["capture"] is missing — nooma capture must join the dispatch table (main.go)`)
	}
	if cmd.run == nil {
		t.Fatal("capture has no run function wired")
	}
}

// TestCaptureSendsPostCaptureToTheRunningServer is 14a.1's request/response
// half, driven against an httptest-backed fake server: the subcommand sends
// POST /capture carrying the text argument, and the printed summary names
// the stored unit — the text is the product (16a-i's own lesson), so this
// asserts the rendered string, not only the decoded struct behind it.
func TestCaptureSendsPostCaptureToTheRunningServer(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"outcome":"stored","unit_id":"u-1","embedded":true}`))
	}))
	t.Cleanup(srv.Close)

	host, port := splitHostPort(t, srv.URL)
	vault := writeVault(t, fmt.Sprintf("server:\n  bind: %s\n  http_port: %s\n", host, port))

	var out, errOut bytes.Buffer
	if err := runCapture([]string{"pick up the dry cleaning", vault}, &out, &errOut); err != nil {
		t.Fatalf("runCapture: %v\nstderr: %s", err, errOut.String())
	}

	if gotMethod != http.MethodPost || gotPath != "/capture" {
		t.Errorf("request = %s %s, want POST /capture", gotMethod, gotPath)
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("decoding the request body %q: %v", gotBody, err)
	}
	if body.Text != "pick up the dry cleaning" {
		t.Errorf("request body text = %q, want the captured text", body.Text)
	}

	if got := out.String(); !strings.Contains(got, "u-1") || !strings.Contains(got, "embedded") {
		t.Errorf("stdout = %q, want it to name the stored unit and say it was embedded", got)
	}
}

// TestDialAddressTranslatesWildcardBinds is design D11's own stated risk:
// "0.0.0.0 and :: are what a server listens on, not what a client connects
// to". A literal http://0.0.0.0:7777 works on some stacks and not others —
// the worst kind of bug, and exactly the kind that would only appear on a
// non-loopback configuration nobody runs in CI. This pins the pure
// translation directly, so it cannot depend on a platform's own dialing
// quirks toward 0.0.0.0.
func TestDialAddressTranslatesWildcardBinds(t *testing.T) {
	t.Parallel()

	tests := []struct{ bind, want string }{
		{"0.0.0.0", "127.0.0.1"},
		{"::", "127.0.0.1"},
		{"127.0.0.1", "127.0.0.1"},
		{"192.168.1.5", "192.168.1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.bind, func(t *testing.T) {
			t.Parallel()
			bind := tt.bind
			port := 7777
			cfg := &config.Config{Server: config.Server{Bind: &bind, HTTPPort: &port}}

			host, _, err := net.SplitHostPort(dialAddress(cfg))
			if err != nil {
				t.Fatalf("dialAddress: %v", err)
			}
			if host != tt.want {
				t.Errorf("dialAddress(bind=%q) host = %q, want %q — a wildcard bind is not a dial address (design D11)", tt.bind, host, tt.want)
			}
		})
	}
}

// TestCaptureDiagnosesNoServerRunning is design D11's first row: nothing
// holds the vault, and dialing fails — the message must say plainly that no
// server is running and point at `nooma serve`, never a bare "connection
// refused".
func TestCaptureDiagnosesNoServerRunning(t *testing.T) {
	t.Parallel()

	port := freeTCPPort(t)
	vault := writeVault(t, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n", port))

	var out, errOut bytes.Buffer
	err := runCapture([]string{"anything", vault}, &out, &errOut)
	if err == nil {
		t.Fatal("runCapture succeeded against a port nothing is listening on")
	}
	for _, want := range []string{
		"no nooma server is running for vault",
		vault,
		fmt.Sprintf("127.0.0.1:%d", port),
		"nooma serve",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "holds vault") {
		t.Errorf("error %q wrongly claims a process holds the vault", err)
	}
}

// TestCaptureDiagnosesHeldButUnreachable is design D11's second row: a
// process holds the vault's lock but nothing answers at the configured
// address — the message must name the holding pid and point at
// server.bind/server.http_port, and it must NOT say "no server is running",
// which would send a user with a moved bind looking in the wrong place.
func TestCaptureDiagnosesHeldButUnreachable(t *testing.T) {
	t.Parallel()

	port := freeTCPPort(t)
	vault := writeVault(t, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n", port))

	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		t.Fatalf("acquiring the vault lock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	var out, errOut bytes.Buffer
	err = runCapture([]string{"anything", vault}, &out, &errOut)
	if err == nil {
		t.Fatal("runCapture succeeded against a held vault with nothing answering")
	}
	pid := os.Getpid()
	for _, want := range []string{
		fmt.Sprintf("pid %d", pid),
		vault,
		"nothing answered",
		"server.bind",
		"server.http_port",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "no nooma server is running") {
		t.Errorf("error %q wrongly claims no server is running, when this vault IS held", err)
	}
}

// TestCaptureSucceedsRegardlessOfLockState is design D11's third row:
// "either" — the diagnosis in the two tests above never runs when the dial
// itself succeeds, so a held-but-reachable vault (the ordinary case: serve
// itself holds the lock while it answers) must not be penalized by it.
func TestCaptureSucceedsRegardlessOfLockState(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"outcome":"stored","unit_id":"u-2","embedded":false}`))
	}))
	t.Cleanup(srv.Close)
	host, port := splitHostPort(t, srv.URL)
	vault := writeVault(t, fmt.Sprintf("server:\n  bind: %s\n  http_port: %s\n", host, port))

	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		t.Fatalf("acquiring the vault lock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	var out, errOut bytes.Buffer
	if err := runCapture([]string{"anything", vault}, &out, &errOut); err != nil {
		t.Fatalf("runCapture failed against a held-but-reachable vault: %v\nstderr: %s", err, errOut.String())
	}
}

// TestCaptureNoHeaderWhenNoTokenConfigured is 14a.3's loopback case: no
// server.auth_token_env at all means no friction — no Authorization header
// on the wire.
func TestCaptureNoHeaderWhenNoTokenConfigured(t *testing.T) {
	t.Parallel()

	var gotAuth string
	seen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"outcome":"stored","unit_id":"u-3"}`))
	}))
	t.Cleanup(srv.Close)
	host, port := splitHostPort(t, srv.URL)
	vault := writeVault(t, fmt.Sprintf("server:\n  bind: %s\n  http_port: %s\n", host, port))

	var out, errOut bytes.Buffer
	if err := runCapture([]string{"anything", vault}, &out, &errOut); err != nil {
		t.Fatalf("runCapture: %v\nstderr: %s", err, errOut.String())
	}
	if !seen {
		t.Fatal("the server never received a request")
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty (no auth_token_env configured)", gotAuth)
	}
}

// TestCaptureSendsBearerTokenWhenConfigured is 14a.3's configured-and-set
// case: server.auth_token_env is set and the named variable holds a value,
// so the request carries it as Authorization: Bearer <value> — the same
// credential, read the same way httpapi.ResolveToken reads it for `serve`
// itself.
//
// Not parallel: t.Setenv forbids it.
func TestCaptureSendsBearerTokenWhenConfigured(t *testing.T) {
	t.Setenv("NOOMA_CAPTURE_TEST_TOKEN", "s3cret-token")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"outcome":"stored","unit_id":"u-4"}`))
	}))
	t.Cleanup(srv.Close)
	host, port := splitHostPort(t, srv.URL)
	vault := writeVault(t, fmt.Sprintf("server:\n  bind: %s\n  http_port: %s\n  auth_token_env: NOOMA_CAPTURE_TEST_TOKEN\n", host, port))

	var out, errOut bytes.Buffer
	if err := runCapture([]string{"anything", vault}, &out, &errOut); err != nil {
		t.Fatalf("runCapture: %v\nstderr: %s", err, errOut.String())
	}
	if want := "Bearer s3cret-token"; gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

// TestCaptureRefusesBeforeSendingWhenAuthVariableIsUnset is 14a.3's sharpest
// case, and the one this PR's own break-experiment #1 exists for:
// server.auth_token_env names a variable the environment does not hold, so
// `nooma capture` must refuse BEFORE sending — never send first and
// discover the 401 afterward, which would have already put the user's text
// on the wire unauthenticated (design D11). The requests counter is the
// proof: a version that sends first and checks the response would still
// fail (a 401 surfaces as an error), but it would do so only AFTER the
// server saw the request — this test catches that ordering bug specifically,
// not merely "an error was returned".
func TestCaptureRefusesBeforeSendingWhenAuthVariableIsUnset(t *testing.T) {
	t.Parallel()

	const unsetVar = "NOOMA_CAPTURE_MISSING_TOKEN"
	if _, set := os.LookupEnv(unsetVar); set {
		t.Fatalf("test precondition violated: %s is set in this environment", unsetVar)
	}

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	host, port := splitHostPort(t, srv.URL)
	vault := writeVault(t, fmt.Sprintf("server:\n  bind: %s\n  http_port: %s\n  auth_token_env: %s\n", host, port, unsetVar))

	var out, errOut bytes.Buffer
	err := runCapture([]string{"my private memory", vault}, &out, &errOut)
	if err == nil {
		t.Fatal("runCapture succeeded with an unset auth_token_env variable")
	}
	if !strings.Contains(err.Error(), unsetVar) {
		t.Errorf("error %q does not name the unset variable", err)
	}
	if requests != 0 {
		t.Fatalf(
			"the server received %d request(s); nooma capture must refuse BEFORE sending when the token is unset (design D11) — the request must never leave",
			requests)
	}
}

// TestCaptureNeverAcquiresTheVaultLock is 14a.4's MUST NOT (spec R3.1):
// nooma capture never opens the vault's database directly and never takes
// (or attempts) the vault's write lock — that is `nooma serve`'s exclusive
// resource. Parses capture.go's own AST rather than grepping text, the same
// precision test/conformance/store_no_direct_clock_read_test.go already
// uses for an analogous MUST NOT, so a mention inside a comment does not
// read as a call.
func TestCaptureNeverAcquiresTheVaultLock(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "capture.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing capture.go: %v", err)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Acquire" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "vaultlock" {
			return true
		}
		t.Errorf(
			"%s: vaultlock.Acquire — nooma capture must never take the vault's write lock "+
				"(spec R3.1 MUST NOT); that is nooma serve's exclusive resource",
			fset.Position(call.Pos()))
		return true
	})
}
