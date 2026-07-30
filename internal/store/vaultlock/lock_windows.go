//go:build windows

package vaultlock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// errWouldBlock is the platform-neutral signal that another process holds the
// lock, as opposed to something having gone wrong.
var errWouldBlock = errors.New("vaultlock: already held")

// tryLock takes a non-blocking exclusive lock on ONE byte, at an offset past the
// PID region.
//
// This is where the layout in lock.go earns itself. Unlike flock, LockFileEx
// locks a byte *range*, and an exclusive range blocks other processes from
// reading those bytes — so a lock covering the PID would make ReadHolder
// impossible here, and spec R8.4 requires status and doctor to work on a held
// vault. Locking a single byte beyond the region leaves the PID readable by
// anyone while the vault is held.
//
// Locking beyond end-of-file is legal on Windows and does not extend the file.
func tryLock(file *os.File) error {
	var overlapped windows.Overlapped
	overlapped.Offset = uint32(lockByteStart)

	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, lockByteLen, 0, &overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errWouldBlock
	}
	return err
}

func unlock(file *os.File) error {
	var overlapped windows.Overlapped
	overlapped.Offset = uint32(lockByteStart)

	return windows.UnlockFileEx(
		windows.Handle(file.Fd()),
		0, lockByteLen, 0, &overlapped,
	)
}
