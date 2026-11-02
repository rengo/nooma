package harness

import (
	"os"
	"testing"

	"github.com/rengo/nooma/test/support/sandbox"
)

// TestMain runs this package's tests away from the machine's vaults: they
// import config or exec, and a command given no vault resolves a real one
// (docs/06-harness.md §3).
func TestMain(m *testing.M) { os.Exit(sandbox.Main(m)) }
