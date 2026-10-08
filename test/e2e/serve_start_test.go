//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/store/vaultlock"
)

// maxServeAttempts bounds how often a start that lost its port to a foreign
// listener is retried on a fresh one. Three is plenty: losing the same race
// three times in a row means something other than the freePort window is wrong,
// and the last attempt's stderr is the evidence.
const maxServeAttempts = 3

// serveReadyBudget is how long a started serve may take to answer. It is
// unchanged from the loops it replaces: the fix is detecting a dead child,
// not waiting longer for a live one.
const serveReadyBudget = 30 * time.Second

// serveListeningPrefix is the stable stderr text serve writes once its own bind
// has succeeded (cmd/nooma/serve.go's listeningPrefix, which package main keeps
// out of this package's reach); the bound address follows it. It replaces a
// timed settle: serve takes the vault lock BEFORE it binds, so a lock plus an
// HTTP answer is also what a child about to die on a stranger's port looks like,
// and no delay is long enough under load. Only the child's own announcement of a
// bind that already happened proves the port is its own.
const serveListeningPrefix = "nooma: listening on "

// serveReapBudget bounds how long cleanup waits for a killed child to be
// reaped, so a stuck Wait fails the test loudly instead of hanging the suite.
const serveReapBudget = 15 * time.Second

// lockedBuffer is a stderr sink safe to read while the child is still writing:
// os/exec copies the pipe from its own goroutine, so a bare strings.Builder
// read mid-run is a data race.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// serveProc is a running `nooma serve` child. Its Wait is reaped exactly once,
// by a goroutine started at launch, so that readiness can notice the child
// dying; every later Wait returns that same result instead of failing with
// "Wait was already called".
type serveProc struct {
	*exec.Cmd
	stderr  *lockedBuffer
	exited  chan struct{}
	waitErr error
}

// Wait blocks until the child has exited and returns its exit status.
func (p *serveProc) Wait() error {
	<-p.exited
	return p.waitErr
}

// serveStartError is a serve that did not become ready. bind marks the one
// failure a fresh port can fix.
type serveStartError struct {
	port   int
	reason string
	stderr string
	bind   bool
}

func (e *serveStartError) Error() string {
	return fmt.Sprintf("serve on port %d: %s\nstderr: %s", e.port, e.reason, e.stderr)
}

// The two renderings of the OS error behind a lost bind. Go prints the
// syscall's own text after "bind: ", so it differs per platform:
//   - Unix: EADDRINUSE, "address already in use".
//   - Windows: WSAEADDRINUSE (10048), "Only one usage of each socket address
//     (protocol/network address/port) is normally permitted."
const (
	bindLostUnix    = "address already in use"
	bindLostWindows = "Only one usage of each socket address (protocol/network address/port) is normally permitted."
)

// isBindFailure reports whether serve's stderr says it lost the port, in
// either platform's wording. It matches on the "bind: " context so the same
// words elsewhere in stderr do not count.
func isBindFailure(stderr string) bool {
	return strings.Contains(stderr, "bind: "+bindLostUnix) ||
		strings.Contains(stderr, "bind: "+bindLostWindows)
}

// launchServe starts `nooma serve` once and returns when it is provably OUR
// child answering on port. It fails at once if the child exits first, instead
// of polling a port a stranger may be answering on.
func launchServe(t *testing.T, home, vault string, port int, extraArgs ...string) (*serveProc, error) {
	t.Helper()

	args := append([]string{"serve"}, extraArgs...)
	args = append(args, vault)
	cmd := exec.Command(binaryPath(t), args...)
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "NOOMA_VAULT=")
	p := &serveProc{Cmd: cmd, stderr: &lockedBuffer{}, exited: make(chan struct{})}
	cmd.Stderr = p.stderr
	// Once the child is gone, Wait must not block on a stderr pipe a
	// grandchild inherited.
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-p.exited:
		case <-time.After(serveReapBudget):
			t.Errorf("serve (pid %d) was not reaped within %s of being killed", cmd.Process.Pid, serveReapBudget)
		}
	})

	fail := func(reason string) (*serveProc, error) {
		stderr := p.stderr.String()
		return nil, &serveStartError{port: port, reason: reason, stderr: stderr, bind: isBindFailure(stderr)}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	deadline := time.Now().Add(serveReadyBudget)
	lastSeen := "no answer"
	for time.Now().Before(deadline) {
		select {
		case <-p.exited:
			return fail(fmt.Sprintf("exited before it answered (%v)", p.waitErr))
		default:
		}

		if resp, err := client.Get(url); err == nil {
			var body struct {
				Name string `json:"name"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			switch {
			case decodeErr == nil && body.Name == "nooma":
				// "nooma" is not proof of WHOSE nooma: a stranger can answer
				// with nooma's own document, and parallel tests run many
				// serves. The answer is credited to our child only when the
				// child itself said, on its own stderr, that it bound THIS
				// port: a bind that succeeded cannot be shared, so the
				// listener is ours. The vault lock naming its pid stays as a
				// second, independent check.
				if !announcedBind(p.stderr.String(), port) {
					lastSeen = "a nooma answer before our child announced binding this port"
					break
				}
				if !holdsVault(vault, p.Process.Pid) {
					lastSeen = "a nooma answer while the vault lock is not held by our child"
					break
				}
				return p, nil
			default:
				lastSeen = "an answer that is not nooma's GET / document"
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fail(fmt.Sprintf("never became ready within %s (last seen: %s)", serveReadyBudget, lastSeen))
}

// announcedBind reports whether stderr carries serve's own line saying it bound
// port: the one proof an answer on that port is the child's and not a
// stranger's. A line matches when it starts with serveListeningPrefix and ends
// with ":<port>", whatever host sits between (127.0.0.1, localhost, [::1]), so
// port 80 never matches 8080 and a caller on another host does not wait out the
// whole ready budget for a line that never matches.
func announcedBind(stderr string, port int) bool {
	suffix := fmt.Sprintf(":%d", port)
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, serveListeningPrefix) && strings.HasSuffix(line, suffix) {
			return true
		}
	}
	return false
}

// holdsVault reports whether the vault's write lock names pid as its holder.
func holdsVault(vault string, pid int) bool {
	holder, held, err := vaultlock.ReadHolder(vault)
	return err == nil && held && holder == pid
}

// retryable is the one decision startServeProc makes about a failed start: a
// lost bind is fixed by a fresh port, anything else (a held vault lock, a bad
// config, a crash) would only repeat and must fail at once with its own stderr.
func retryable(err error) bool {
	var se *serveStartError
	return errors.As(err, &se) && se.bind
}

// startServeProc launches serve and, when it loses its port to a foreign
// listener, retries the whole freePort -> config -> start sequence on a fresh
// one, up to maxServeAttempts. *port is updated to the port that finally
// served, so callers must read it after this returns. The vault and HOME stay
// exactly as the caller made them.
func startServeProc(t *testing.T, home, vault string, port *int, extraArgs ...string) *serveProc {
	t.Helper()

	var last error
	for attempt := 1; attempt <= maxServeAttempts; attempt++ {
		p, err := launchServe(t, home, vault, *port, extraArgs...)
		if err == nil {
			return p
		}
		last = err
		if !retryable(err) {
			t.Fatal(err)
		}
		if attempt < maxServeAttempts {
			next := freePort(t)
			t.Logf("serve lost port %d to another listener (attempt %d/%d), retrying on %d", *port, attempt, maxServeAttempts, next)
			retargetPort(t, vault, *port, next)
			*port = next
		}
	}
	t.Fatalf("serve could not bind a port after %d attempts: %v", maxServeAttempts, last)
	return nil
}

// retargetPort rewrites the vault's configured http_port in place, so the
// caller's own config document (providers, tasks, whatever else it wrote)
// survives a retry untouched.
func retargetPort(t *testing.T, vault string, from, to int) {
	t.Helper()

	path := filepath.Join(vault, "nooma.yml")
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	re := portLine(from)
	if !re.Match(doc) {
		t.Fatalf("cannot retarget %s: no whole %q line to rewrite", path, fmt.Sprintf("http_port: %d", from))
	}
	out := re.ReplaceAll(doc, []byte(fmt.Sprintf("${1}%d", to)))
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// portLine matches the http_port line holding exactly port, anchored on the
// whole line so 8080 never matches inside 80801.
func portLine(port int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`(?m)^([ \t]*http_port:[ \t]*)%d[ \t]*$`, port))
}
