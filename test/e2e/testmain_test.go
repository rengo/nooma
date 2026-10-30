//go:build e2e

package e2e

import (
	"os"
	"testing"

	"github.com/rengo/nooma/test/support/sandbox"
)

// TestMain runs the end-to-end tests away from the machine's vaults. Each
// child binary is also given its own HOME and an empty NOOMA_VAULT (noomaCmd);
// this covers the test process itself, and any child started without them.
func TestMain(m *testing.M) { os.Exit(sandbox.Main(m)) }
