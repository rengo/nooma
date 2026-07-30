//go:build integration

// See test/integration/doc.go for what this package is and when it runs.
package integration

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/store/vaultlock"
)

// The child process is this same test binary, re-executed with these variables
// set. It is the only honest way to test a lock: the mechanism is enforced by the
// kernel *between processes*, so two goroutines in one process would prove
// nothing — on unix they share the same open file description and flock would
// happily grant both.
const (
	childEnv      = "NOOMA_LOCK_CHILD"
	childVaultEnv = "NOOMA_LOCK_VAULT"
)

// TestMain lets the test binary act as the lock-holding child when asked.
func TestMain(m *testing.M) {
	if dir := os.Getenv(childVaultEnv); os.Getenv(childEnv) != "" && dir != "" {
		holdForever(dir)
		return
	}
	os.Exit(m.Run())
}

func holdForever(dir string) {
	lock, err := vaultlock.Acquire(dir)
	if err != nil {
		_, _ = os.Stderr.WriteString("child could not acquire: " + err.Error())
		os.Exit(2)
	}
	// Announce on stdout that the lock is held, then block until killed.
	_, _ = os.Stdout.WriteString("held\n")
	defer func() { _ = lock.Release() }()
	select {}
}

// startHolder launches a child holding the vault's lock and returns it once the
// child has confirmed the lock is actually held. Waiting for that confirmation
// rather than sleeping is what keeps this test from being flaky by construction.
func startHolder(t *testing.T, dir string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), childEnv+"=1", childVaultEnv+"="+dir)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	ready := make(chan struct{})
	go func() {
		buf := make([]byte, 5)
		if _, err := stdout.Read(buf); err == nil {
			close(ready)
		}
	}()

	select {
	case <-ready:
	case <-time.After(30 * time.Second):
		t.Fatal("the child never reported holding the lock")
	}
	return cmd
}

func vaultDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nooma.yml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestSecondWriterFails is spec R8.2. A second `nooma serve` over a held vault
// must fail clearly and touch nothing.
func TestSecondWriterFails(t *testing.T) {
	dir := vaultDir(t)
	child := startHolder(t, dir)

	lock, err := vaultlock.Acquire(dir)
	if err == nil {
		_ = lock.Release()
		t.Fatal("a second writer acquired a lock the first one holds")
	}

	if !strings.Contains(err.Error(), strconv.Itoa(child.Process.Pid)) {
		t.Errorf("the error does not name the holding PID %d, which is the user's next action:\n%v",
			child.Process.Pid, err)
	}

	var inUse *vaultlock.InUseError
	if !errors.As(err, &inUse) {
		t.Errorf("the error is not a *vaultlock.InUseError, so a caller cannot distinguish\n"+
			"'the vault is busy' from a real I/O failure:\n%v", err)
	}
}

// TestReadHolderTakesNoLock is spec R8.4. `status` and `doctor` are read-only and
// must work on a vault a writer holds — reporting the holder, not refusing.
//
// This is the requirement that forced the PID and the lock byte apart: on Windows
// an exclusive byte range genuinely blocks other processes from reading those
// bytes, so a PID stored where the lock lives would make this impossible.
func TestReadHolderTakesNoLock(t *testing.T) {
	dir := vaultDir(t)
	child := startHolder(t, dir)

	pid, held, err := vaultlock.ReadHolder(dir)
	if err != nil {
		t.Fatalf("ReadHolder on a held vault: %v", err)
	}
	if !held {
		t.Fatal("ReadHolder reports the vault as free while a child holds it")
	}
	if pid != child.Process.Pid {
		t.Errorf("ReadHolder reports PID %d, want the holder's %d", pid, child.Process.Pid)
	}
}

// TestAcquireReleaseRoundTrip is the plain path, and the control for everything
// above: if Acquire always failed, every rejection test would pass.
func TestAcquireReleaseRoundTrip(t *testing.T) {
	dir := vaultDir(t)

	first, err := vaultlock.Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire on a free vault: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	second, err := vaultlock.Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}

	pid, held, err := vaultlock.ReadHolder(dir)
	if err != nil {
		t.Fatalf("ReadHolder after release: %v", err)
	}
	if held {
		t.Errorf("ReadHolder reports the vault held by %d after it was released", pid)
	}
}

// TestLockSurvivesSIGKILL is spec R8.3, and it is the requirement that eliminated
// a plain PID file: that file outlives its process, so after a kill -9 the vault
// would be permanently unusable until somebody deleted it by hand. An OS advisory
// lock is released by the kernel however the process dies.
func TestLockSurvivesSIGKILL(t *testing.T) {
	dir := vaultDir(t)
	child := startHolder(t, dir)

	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Process.Wait(); err != nil {
		t.Fatal(err)
	}

	// The kernel releases on process death, but not necessarily before Wait
	// returns on every platform, so retry briefly rather than asserting once.
	var lastErr error
	for i := 0; i < 100; i++ {
		lock, err := vaultlock.Acquire(dir)
		if err == nil {
			if err := lock.Release(); err != nil {
				t.Fatalf("Release after reacquiring: %v", err)
			}
			return
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the vault stayed locked after its holder was killed; recovery would need manual\n"+
		"deletion of the lock file, which spec R8.3 forbids as the documented procedure:\n%v", lastErr)
}

// TestReadHolderDuringAcquire is the regression guard for D4's single-write rule.
//
// An earlier design zeroed the PID region and then wrote the PID, two operations.
// ReadHolder takes no lock and runs at any instant, so a reader landing between
// them would have seen a complete, self-consistent, WRONG answer — "no holder" —
// while the lock was genuinely held. One WriteAt of the full region means that
// intermediate state does not exist to be observed.
func TestReadHolderDuringAcquire(t *testing.T) {
	dir := vaultDir(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			pid, held, err := vaultlock.ReadHolder(dir)
			if err != nil {
				continue
			}
			if held && pid <= 0 {
				t.Errorf("ReadHolder reported the vault held by PID %d — a partial write was observed", pid)
				return
			}
		}
	}()

	for i := 0; i < 200; i++ {
		lock, err := vaultlock.Acquire(dir)
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("Acquire in the churn loop: %v", err)
		}
		if err := lock.Release(); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("Release in the churn loop: %v", err)
		}
	}
	close(stop)
	wg.Wait()
}
