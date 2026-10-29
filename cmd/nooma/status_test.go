package main

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/sqlite/sqlitetest"
)

// TestStatus_LeavesAnOlderVaultsSchemaWhereItWas: nooma status reads the
// schema version read-only, as doctor does (I55): a vault behind the binary is
// reported at its own version and stays there, the file unchanged.
func TestStatus_LeavesAnOlderVaultsSchemaWhereItWas(t *testing.T) {
	target, err := sqlite.SchemaTarget()
	if err != nil {
		t.Fatal(err)
	}
	vault, _, dbPath := doctorVault(t, func(db *sqlite.Vault) { sqlitetest.SetUserVersion(t, db, target-1) })
	before := fileSum(t, dbPath)

	var out, errOut bytes.Buffer
	if err := runStatus([]string{vault}, &out, &errOut); err != nil {
		t.Fatalf("status: %v\n%s", err, errOut.String())
	}

	if want := "schema:    version " + strconv.Itoa(target-1) + "\n"; !strings.Contains(out.String(), want) {
		t.Errorf("status lacks %q:\n%s", want, out.String())
	}
	if version, err := sqlite.VerifySnapshot(context.Background(), dbPath); err != nil || version != target-1 {
		t.Errorf("schema after status = %d, %v; want %d, unmigrated", version, err, target-1)
	}
	if fileSum(t, dbPath) != before {
		t.Error("the database file changed under status")
	}
}
