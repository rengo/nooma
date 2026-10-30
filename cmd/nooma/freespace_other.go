//go:build !unix && !windows

package main

// freeSpace cannot be read on this platform: import warns and goes on.
func freeSpace(string) (uint64, bool) { return 0, false }
