// Package vaultlock enforces the single-writer rule of
// docs/03-data-model.md §"Operational properties of the vault": the binary takes
// a lockfile in the vault on startup, and a second `nooma serve` over the same
// vault fails clearly rather than corrupting anything.
//
// The mechanism is an OS advisory lock, not a PID file. A PID file outlives the
// process that wrote it, so after a kill -9 the vault would stay unusable until
// somebody deleted it by hand — and adding staleness detection means asking "is
// PID 1234 alive?", which is the same amount of platform-specific code plus a
// genuine race: PIDs are recycled, and a wrong answer there silently permits two
// writers. The mechanism built to prevent corruption would become the thing that
// permits it. A kernel-held lock is released when the process dies, however it
// dies (spec R8.3).
package vaultlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// LockFileName is the lockfile inside the vault, named by
// docs/03-data-model.md.
const LockFileName = "nooma.lock"

// The PID region and the lock byte deliberately do not overlap, and that detail
// is load-bearing rather than tidy.
//
// `flock` is whole-file and advisory: it gates no read or write by anyone. But
// Windows `LockFileEx` locks a *byte range*, and an exclusive range genuinely
// blocks other processes from reading those bytes. If the PID lived where the
// lock does, `nooma status` and `nooma doctor` could not read the holder on
// Windows — and spec R8.4 requires exactly that. So the PID occupies the first
// pidRegionSize bytes and the lock sits on one byte beyond them, which is legally
// beyond end-of-file on Windows.
const (
	pidRegionSize = 1024
	lockByteStart = pidRegionSize
	lockByteLen   = 1
)

// InUseError reports that another process holds the vault.
//
// It is a distinct type so a caller can tell "the vault is busy" — a normal
// outcome with an obvious remedy — from a real I/O failure, which is not.
type InUseError struct {
	Vault string
	PID   int
}

func (e *InUseError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("vault %s is already in use by process %d", e.Vault, e.PID)
	}
	return fmt.Sprintf("vault %s is already in use by another process", e.Vault)
}

// Lock is a held vault lock. Release it, or let the process exit.
type Lock struct {
	file  *os.File
	vault string
}

// Acquire takes the vault's write lock.
//
// The order here is load-bearing and runs opposite to how it reads at first
// glance: **the lock is taken before the PID is written**.
//
// Neither platform's lock protects the PID region — `flock` gates nothing at all,
// and `LockFileEx` covers only the byte beyond it. So if the PID were written
// first, the process that *loses* the race (spec R8.2's everyday scenario: a
// second `nooma serve` on a held vault) would overwrite the real holder's PID
// with its own before discovering it lost, and `status` would then report a dead
// process as the holder. That breaks R8.2 and R8.4 inside the very mechanism
// built to report the holder truthfully.
func Acquire(vaultDir string) (*Lock, error) {
	path := filepath.Join(vaultDir, LockFileName)

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	if err := tryLock(file); err != nil {
		defer func() { _ = file.Close() }()
		if errors.Is(err, errWouldBlock) {
			// Read the holder's PID from the file we did not write to. Nothing
			// about the losing process is true yet, so nothing about it is
			// recorded.
			pid, _, _ := ReadHolder(vaultDir)
			return nil, &InUseError{Vault: vaultDir, PID: pid}
		}
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}

	if err := writePID(file, os.Getpid()); err != nil {
		_ = unlock(file)
		_ = file.Close()
		return nil, err
	}

	return &Lock{file: file, vault: vaultDir}, nil
}

// Release drops the lock and clears the PID region.
//
// The kernel would release it on exit anyway; the explicit release exists so that
// a test can observe a released lock rather than a released-by-luck one, and so
// `serve` leaves no stale PID behind for `status` to report.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	// Clear the PID before unlocking, so no window exists in which the lock is
	// free and a stale PID still readable.
	clearErr := writePID(l.file, 0)
	unlockErr := unlock(l.file)
	closeErr := l.file.Close()
	l.file = nil

	return errors.Join(clearErr, unlockErr, closeErr)
}

// ReadHolder reports the PID recorded in the vault's lockfile, and whether the
// vault is held.
//
// **It takes no lock at all.** That is the whole of spec R8.4: an API where
// reading the holder required acquiring anything would make "status works on a
// held vault" impossible to satisfy without a comment asking people to be
// careful.
//
// held is false when there is no lockfile, or when its PID region is empty —
// which is what Release leaves behind.
func ReadHolder(vaultDir string) (pid int, held bool, err error) {
	path := filepath.Join(vaultDir, LockFileName)

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("reading %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, pidRegionSize)
	n, err := file.ReadAt(buf, 0)
	if n == 0 && err != nil {
		return 0, false, nil
	}

	text := string(buf[:n])
	for i, b := range buf[:n] {
		if b == 0 {
			text = string(buf[:i])
			break
		}
	}
	if text == "" {
		return 0, false, nil
	}

	pid, convErr := strconv.Atoi(text)
	if convErr != nil || pid <= 0 {
		// A lockfile written by something else, or truncated. Reporting "no
		// holder" is safer than reporting a number we cannot vouch for; the lock
		// itself, not this file, is the truth about whether the vault is busy.
		return 0, false, nil
	}
	return pid, true, nil
}

// writePID replaces the whole PID region in one WriteAt.
//
// One write of the *full* region, not a short prefix, and the buffer is freshly
// allocated so every byte past the digits is already zero. That is what makes the
// region self-cleaning: a shorter PID cannot leave a previous holder's digits
// behind, and there is no separate zeroing pass for a lock-free reader to land in
// the middle of. `ReadHolder` still stops at the first NUL — defensive, not
// required by this, so that a truncated or foreign lockfile degrades to "no
// holder" instead of to a garbage PID.
func writePID(file *os.File, pid int) error {
	buf := make([]byte, pidRegionSize)
	if pid > 0 {
		copy(buf, strconv.Itoa(pid))
	}
	if _, err := file.WriteAt(buf, 0); err != nil {
		return fmt.Errorf("recording the lock holder: %w", err)
	}
	return file.Sync()
}
