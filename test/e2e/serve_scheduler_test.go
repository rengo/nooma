//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
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
