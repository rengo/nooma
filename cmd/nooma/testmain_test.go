package main

import (
	"os"
	"testing"

	"github.com/rengo/nooma/test/support/sandbox"
)

// TestMain runs this package's tests away from the machine's vaults: many of
// them call run() as the binary would, and a command given no vault must
// never resolve a real one (sandbox.Main).
func TestMain(m *testing.M) { os.Exit(sandbox.Main(m)) }
