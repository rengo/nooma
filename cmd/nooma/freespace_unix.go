//go:build unix

package main

import "golang.org/x/sys/unix"

// freeSpace is the bytes free to an unprivileged writer on dir's filesystem,
// and whether the platform could say.
func freeSpace(dir string) (uint64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true //nolint:gosec,unconvert // Bsize is int64 on linux and uint32 on darwin; a block size is never negative
}
