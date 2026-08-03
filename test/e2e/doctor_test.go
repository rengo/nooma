//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoctorOnAHealthyVault is spec R13.1 and R13.3: every check reported, exit
// zero.
func TestDoctorOnAHealthyVault(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")

	stdout, stderr, err := nooma(t, home, work, "doctor", vault)
	if err != nil {
		t.Fatalf("doctor on a healthy vault: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	for _, check := range []string{"configuration", "permissions", "integrity", "schema", "bind"} {
		if !strings.Contains(strings.ToLower(stdout), check) {
			t.Errorf("doctor does not report a %q check:\n%s", check, stdout)
		}
	}
}

// TestDoctorReportsEveryProblem is spec R13.2, and it is what proves D10's
// checks-as-data actually delivers.
//
// Two independent faults, and both must appear. A doctor written as a sequence of
// early returns would report the first and send the user round again — which is
// the opposite of what docs/01-architecture.md means when it calls doctor "what
// makes the binary feel cared for".
func TestDoctorReportsEveryProblem(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")

	// Fault one: a configuration that cannot be satisfied — a non-loopback bind
	// with no auth token (ADR-0007).
	// Fault two: the database the config points at is gone.
	cfg := filepath.Join(vault, "nooma.yml")
	if err := os.WriteFile(cfg, []byte("server:\n  bind: 0.0.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(vault, "nooma.db")); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := nooma(t, home, work, "doctor", vault)
	if err == nil {
		t.Fatal("doctor exited zero on a vault with two faults")
	}

	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "auth_token_env") {
		t.Errorf("doctor does not report the ADR-0007 problem:\n%s", stdout)
	}
	if !strings.Contains(lower, "integrity") && !strings.Contains(lower, "database") {
		t.Errorf("doctor does not report the missing database:\n%s", stdout)
	}
}

// TestDoctorExitCodeIsUsableInAScript is spec R13.3. `nooma doctor && deploy` has
// to mean something.
func TestDoctorExitCodeIsUsableInAScript(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()

	healthy := initVault(t, home, work, "healthy.nooma")
	if _, _, err := nooma(t, home, work, "doctor", healthy); err != nil {
		t.Errorf("doctor on a healthy vault exited non-zero: %v", err)
	}

	broken := initVault(t, home, work, "broken.nooma")
	if err := os.WriteFile(filepath.Join(broken, "nooma.yml"), []byte("server:\n  bind: 0.0.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := nooma(t, home, work, "doctor", broken); err == nil {
		t.Error("doctor on a broken vault exited zero")
	}
}

// TestDoctorMakesNoNetworkCall is spec R13.4 and non-negotiable #5, asserted the
// only way a black-box test can: a vault configured with a provider pointing at
// an address nothing is listening on must still finish promptly and must not
// report anything about reachability.
//
// M0's doctor checks configuration, permissions, integrity and the effective
// bind. Provider connectivity needs providers (M1) and hardware assessment needs
// a decision that is still open (due before M6). A check that cannot be
// implemented honestly is worse than an absent one, because its passing means
// nothing.
//
// tasks: binds all three of tasksM1Consumes — design D18b row 1 (16b) now
// FAILs a configured-but-partially-bound vault (the exact C9 shape), so a
// fixture naming providers: with no tasks: at all would trip that new check
// and fail this test for an unrelated reason. capture_processing and
// relation_evaluation bind to a whisper_cpp-typed provider deliberately:
// buildProvider (wiring.go) has no client for that type, so checkLLMQuality
// resolves zero tasks and never dials anything — audio is never reachable
// over HTTP at all, only local. embedding binds to local (the unreachable
// ollama endpoint), which checkLLMQuality never touches — only R6.3's own
// runtime consistency check (row 2) reads the vault, not the network.
func TestDoctorMakesNoNetworkCall(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")

	cfg := "providers:\n" +
		"  local:\n    type: ollama\n    endpoint: http://127.0.0.1:1\n    model: m\n" +
		"  audio:\n    type: whisper_cpp\n    binary_path: /nonexistent\n    model_path: /nonexistent\n" +
		"tasks:\n" +
		"  capture_processing:\n    provider: audio\n" +
		"  relation_evaluation:\n    provider: audio\n" +
		"  embedding:\n    provider: local\n"
	if err := os.WriteFile(filepath.Join(vault, "nooma.yml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := nooma(t, home, work, "doctor", vault)
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, stdout)
	}
	for _, absent := range []string{"reachable", "unreachable", "connect", "hardware"} {
		if strings.Contains(strings.ToLower(stdout), absent) {
			t.Errorf("doctor reports %q, which M0 cannot honestly check:\n%s", absent, stdout)
		}
	}
}

// TestDoctorWorksOnAHeldVault is R8.4: doctor is read-only, so it must run
// against a vault a writer holds.
func TestDoctorWorksOnAHeldVault(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	holdVaultLock(t, vault)

	if _, stderr, err := nooma(t, home, work, "doctor", vault); err != nil {
		t.Fatalf("doctor on a held vault: %v\nstderr: %s", err, stderr)
	}
}

// TestDoctorNeverPrintsASecret is R4.2 at the command level.
func TestDoctorNeverPrintsASecret(t *testing.T) {
	const sentinel = "sk-live-DO-NOT-LEAK-7b1e"

	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	if err := os.WriteFile(filepath.Join(vault, ".env"), []byte("ANTHROPIC_API_KEY="+sentinel+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, _ := nooma(t, home, work, "doctor", vault)
	if strings.Contains(stdout, sentinel) || strings.Contains(stderr, sentinel) {
		t.Fatalf("doctor leaked a secret value:\n%s\n%s", stdout, stderr)
	}
}
