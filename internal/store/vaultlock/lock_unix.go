//go:build !windows

package vaultlock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// errWouldBlock is the platform-neutral signal that another process holds the
// lock, as opposed to something having gone wrong.
var errWouldBlock = errors.New("vaultlock: already held")

// tryLock takes a non-blocking exclusive flock.
//
// flock is whole-file and purely advisory: it gates no ordinary read or write by
// any process, cooperating or not. That is why the PID region needs no special
// placement here — but it does on Windows, and the layout is shared, so this file
// simply ignores the byte offsets the other one needs.
//
// The lock is held by the open file description and released by the kernel when
// the process dies, however it dies. That is spec R8.3, for free.
func tryLock(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errWouldBlock
	}
	return err
}

func unlock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
