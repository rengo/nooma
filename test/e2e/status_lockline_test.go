//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"
)

// TestLockLineIgnoresPIDDigitsElsewhere pins the matcher the lock-holder test
// relies on: a PID whose digits sit in the path, size or port is not a holder.
func TestLockLineIgnoresPIDDigitsElsewhere(t *testing.T) {
	const pid = 477687809
	free := "vault:     /tmp/TestX" + strconv.Itoa(pid) + "/002/free.nooma\nsize:      204.0 KiB\nlock:      free\n"
	held := "vault:     /tmp/x\nlock:      held by process " + strconv.Itoa(pid) + "\n"
	tooLong := "lock:      held by process " + strconv.Itoa(pid) + "1\n"

	// The old assertion is fooled by the path; the new one is not.
	if !strings.Contains(free, strconv.Itoa(pid)) {
		t.Fatal("crafted output does not contain the PID; the probe proves nothing")
	}
	if lockLineNamesPID(free, pid) {
		t.Error("a PID inside the path was read as the holder")
	}
	if !lockLineNamesPID(held, pid) {
		t.Error("the holder on the lock line was not found")
	}
	if lockLineNamesPID(tooLong, pid) {
		t.Error("a longer number containing the PID was read as the holder")
	}
	if lockLine(free) != "lock:      free" {
		t.Errorf("lock line = %q", lockLine(free))
	}
}
