//go:build windows

package main

import "golang.org/x/sys/windows"

// freeSpace is the bytes free to this user on dir's volume, and whether the
// platform could say.
func freeSpace(dir string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, false
	}
	return free, true
}
