//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestServe_ConcurrentConsolidate_StillRefused is spec R3.1 end to end: a
// `serve` process with the scheduler wired, running against a vault a
// second `nooma consolidate` invocation targets, is still refused with
// m2c R6.1's existing clean lock error — proving wireScheduler reuses
// serve's own vault/lock (cmd/nooma/serve.go:71-89) rather than taking a
// second vaultlock.Acquire, which would either deadlock against the lock
// serve itself holds or (if the lock were reentrant) silently create a
// second logical holder undetectable by this same refusal.
//
// Disclosed: this task's own stated red ("undefined: wiring.wireScheduler
// — package/binary does not compile") does not materialize. test/e2e
// compiles the real binary as a subprocess (test/e2e/init_test.go's
// binaryPath) and never imports cmd/nooma directly — cmd/nooma is
// `package main`, unimportable by any other package — so no reference to
// an unexported wireScheduler could ever appear in this file, compile-time
// or otherwise. Run against unmodified main (before wireScheduler existed
// at all), this test already PASSES: the refusal it asserts comes from
// vaultlock's pre-existing mutual exclusion (M1), not from anything this
// PR adds. It stays in this PR as a genuine regression guard for R3.1 —
// proving wireScheduler's addition does not change this observable
// behavior, i.e. that it never takes a second lock.
func TestServe_ConcurrentConsolidate_StillRefused(t *testing.T) {
	llm := mockConsolidateLLM(t)

	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n%s", port, consolidateConfig(llm.URL)))

	cmd := startServe(t, home, vault, port)

	_, stderr, err := nooma(t, home, work, "consolidate", vault)
	if err == nil {
		t.Fatal("consolidate succeeded against a vault a running serve process already holds")
	}
	if !strings.Contains(stderr, fmt.Sprint(cmd.Process.Pid)) {
		t.Errorf("the refusal does not name the holding PID %d:\n%s", cmd.Process.Pid, stderr)
	}
}

// startServeCapturingStderr mirrors startServe (serve_test.go) but also
// hands back the running process's stderr buffer, so this file's own
// unconfigured-vault test can assert on wireScheduler's one-line degrade
// explanation (design §6; non-negotiable: "a nil scheduler, nil error, one
// log line naming why") without widening startServe's own shared return
// shape for every other caller in this package. Small, disclosed
// duplication over a shared abstraction — the same trade this chain's
// earlier links already took for a package-local fixture (m2d PR 3a
// task 3a.2's own errConfigRepo, PR 5's fakeIDGen).
func startServeCapturingStderr(t *testing.T, home, vault string, port int) (*exec.Cmd, *strings.Builder) {
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
			return cmd, &errOut
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("serve never answered on port %d\nstderr: %s", port, errOut.String())
	return nil, nil
}

// TestServe_UnconfiguredVault_HTTPStillAnswers is spec R3.2 end to end: a
// vault whose task bindings cannot resolve ConsolidateService (the same
// class of refusal resolveConsolidateProviders already answers for `nooma
// consolidate`) leaves the scheduler unstarted rather than failing
// runServe outright — HTTP capture and recall stay available on such a
// vault exactly as they do today
// (TestServeCaptureAndRecallAreReachableButUnwired, capture_recall_test.go,
// unchanged by this PR: this fixture configures no providers/tasks at all,
// so wireBrain degrades to nil exactly as it always has, independently of
// this PR's own wireScheduler change).
//
// Red, observed for the stated reason before wireScheduler degraded
// gracefully (task 6.3/6.4): with wireScheduler propagating
// resolveConsolidateProviders' refusal as a hard error (task 6.2's own
// first, naive shape), runServe returned
// `wiring the scheduler: consolidate: task "relation_evaluation" has no
// provider bound...` and exited before the listener ever came up —
// startServe's own health-check loop timed out after 30s: "serve never
// answered on port <port>". Fixed by wireScheduler checking
// resolveConsolidateProviders itself, ahead of wireConsolidate's heavier
// call, mirroring resolveTaskProviders/wireBrain's own two-step shape
// (next commit).
func TestServe_UnconfiguredVault_HTTPStillAnswers(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n", port))

	_, errOut := startServeCapturingStderr(t, home, vault, port)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	captureBody, err := json.Marshal(map[string]string{"text": "pick up the dry cleaning"})
	if err != nil {
		t.Fatal(err)
	}
	captureResp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/capture", port), "application/json", bytes.NewReader(captureBody))
	if err != nil {
		t.Fatalf("POST /capture: %v", err)
	}
	defer func() { _ = captureResp.Body.Close() }()
	if captureResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("POST /capture = %d, want %d (Capture is nil — no providers configured)", captureResp.StatusCode, http.StatusServiceUnavailable)
	}

	recallBody, err := json.Marshal(map[string]string{"query": "dry cleaning"})
	if err != nil {
		t.Fatal(err)
	}
	recallResp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/recall", port), "application/json", bytes.NewReader(recallBody))
	if err != nil {
		t.Fatalf("POST /recall: %v", err)
	}
	defer func() { _ = recallResp.Body.Close() }()
	if recallResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("POST /recall = %d, want %d (Recall is nil — no providers configured)", recallResp.StatusCode, http.StatusServiceUnavailable)
	}

	if !strings.Contains(errOut.String(), "scheduler: consolidation not scheduled") {
		t.Errorf("wireScheduler's own degrade line is missing from the process log:\n%s", errOut.String())
	}
}
