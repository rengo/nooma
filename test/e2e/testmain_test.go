//go:build e2e

package e2e

import (
	"os"
	"testing"

	"github.com/rengo/nooma/test/support/sandbox"
)

// TestMain runs the end-to-end tests away from the machine's vaults. Each
// child binary is also started through sandbox.Child, with its own HOME and an
// empty NOOMA_VAULT; this covers the test process itself.
func TestMain(m *testing.M) { os.Exit(sandbox.Main(m)) }
