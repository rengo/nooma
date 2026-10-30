package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/sqlite/sqlitetest"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// doctorVault is a migrated vault with no provider configured, its config and
// the database path; seed, when not nil, writes into the vault before it is
// closed.
func doctorVault(t *testing.T, seed func(*sqlite.Vault)) (string, *config.Config, string) {
	t.Helper()
	vault := writeVault(t, "")
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		t.Fatalf("loadVaultConfig: %v", err)
	}
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	if seed != nil {
		seed(db)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return vault, cfg, dbPath
}

// withEmbeddingModel binds the embedding task to a provider naming model.
func withEmbeddingModel(cfg *config.Config, model string) *config.Config {
	c := *cfg
	c.Providers = map[string]config.Provider{"emb": {Type: "ollama", Model: model}}
	c.Tasks = map[string]config.TaskBinding{"embedding": {Provider: "emb"}}
	return &c
}

var doctorSeedAt = time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

// seedUnits creates a live unit per id, embedded under models[id] when set.
func seedUnits(t *testing.T, db *sqlite.Vault, models map[string]string, ids ...string) {
	t.Helper()
	ctx := context.Background()
	for _, id := range ids {
		u := unit.Unit{ID: id, Type: unit.TypeKnowledge, Content: "note " + id, Status: unit.StatusPool,
			Weight: 0.5, WeightDecayRate: 0.1, CreatedAt: doctorSeedAt, UpdatedAt: doctorSeedAt, LastTouchedAt: doctorSeedAt}
		if err := sqlite.NewUnitRepo(db).Create(ctx, u); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if model, ok := models[id]; ok {
			if err := sqlite.NewEmbeddingRepo(db).Put(ctx, ports.Embedding{UnitID: id, Model: model, Vector: []float32{1, 0}, At: doctorSeedAt}); err != nil {
				t.Fatalf("embed %s: %v", id, err)
			}
		}
	}
}

func fileSum(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// TestI55_DoctorWritesNothingAndLeavesTheSchemaWhereItWas: doctor opens the
// vault read-only. A vault behind this binary stays at its version, and the
// database file is byte for byte what it was, while doctor says serve will
// migrate it.
func TestI55_DoctorWritesNothingAndLeavesTheSchemaWhereItWas(t *testing.T) {
	target, err := sqlite.SchemaTarget()
	if err != nil {
		t.Fatal(err)
	}
	vault, _, dbPath := doctorVault(t, func(db *sqlite.Vault) {
		seedUnits(t, db, map[string]string{"u-1": "model-a"}, "u-1")
		sqlitetest.SetUserVersion(t, db, target-1)
	})
	before := fileSum(t, dbPath)

	var out, errOut bytes.Buffer
	_ = runDoctor([]string{vault}, &out, &errOut)

	if got := fileSum(t, dbPath); got != before {
		t.Error("the database file changed under doctor")
	}
	if version, err := sqlite.VerifySnapshot(context.Background(), dbPath); err != nil || version != target-1 {
		t.Errorf("schema after doctor = %d, %v; want %d, unmigrated", version, err, target-1)
	}
	want := "schema " + strconv.Itoa(target-1) + "; serve migrates it to " + strconv.Itoa(target)
	if !strings.Contains(out.String(), want) {
		t.Errorf("doctor's report lacks %q:\n%s", want, out.String())
	}
}

// TestCheckSchema_AheadOfTheBinaryFailsNamingTheUpgrade: a vault a newer
// binary migrated is one this binary cannot run; doctor says so, unchanged.
func TestCheckSchema_AheadOfTheBinaryFailsNamingTheUpgrade(t *testing.T) {
	target, err := sqlite.SchemaTarget()
	if err != nil {
		t.Fatal(err)
	}
	vault, cfg, _ := doctorVault(t, func(db *sqlite.Vault) { sqlitetest.SetUserVersion(t, db, target+1) })

	err = checkSchema(vault, cfg)
	if err == nil {
		t.Fatal("checkSchema passed a vault ahead of the binary")
	}
	if _, ok := err.(doctorOKDetail); ok {
		t.Fatalf("checkSchema reported %q as ok, want a FAIL", err)
	}
	for _, want := range []string{"schema " + strconv.Itoa(target+1), "up to " + strconv.Itoa(target), "upgrade nooma"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("checkSchema = %q, want it to say %q", err, want)
		}
	}
}

// TestCheckSchema_AtTheBinarysVersionIsPlainOK: nothing to say.
func TestCheckSchema_AtTheBinarysVersionIsPlainOK(t *testing.T) {
	vault, cfg, _ := doctorVault(t, nil)
	if err := checkSchema(vault, cfg); err != nil {
		t.Errorf("checkSchema on a current vault = %v, want nil", err)
	}
}

// TestCheckVaultLock_ReportsTheHolderAsInformationOnly: free or held, the lock
// is an ok line that says which; doctor runs beside serve.
func TestCheckVaultLock_ReportsTheHolderAsInformationOnly(t *testing.T) {
	vault, cfg, _ := doctorVault(t, nil)

	err := checkVaultLock(vault, cfg)
	if d, ok := err.(doctorOKDetail); !ok || !strings.Contains(d.Error(), "free") {
		t.Errorf("checkVaultLock on a free vault = %v, want an ok line saying it is free", err)
	}

	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	err = checkVaultLock(vault, cfg)
	want := "held by process " + strconv.Itoa(os.Getpid())
	if d, ok := err.(doctorOKDetail); !ok || !strings.Contains(d.Error(), want) {
		t.Errorf("checkVaultLock on a held vault = %v, want an ok line saying %q", err, want)
	}
}

// TestCheckVaultCoverage_CountsUnitsLackingTheConfiguredModel: a live unit
// whose vector is another model's is unreachable by meaning, as one with none
// is. The FAIL names the configured model, the count, the other models present
// and the fix, honestly not yet shipped.
func TestCheckVaultCoverage_CountsUnitsLackingTheConfiguredModel(t *testing.T) {
	vault, cfg, _ := doctorVault(t, func(db *sqlite.Vault) {
		seedUnits(t, db, map[string]string{"u-a": "model-a", "u-b": "model-b"}, "u-a", "u-b", "u-bare")
	})

	err := checkVaultCoverage(vault, withEmbeddingModel(cfg, "model-a"))
	if err == nil {
		t.Fatal("checkVaultCoverage passed a vault with two units lacking model-a")
	}
	if _, ok := err.(doctorOKDetail); ok {
		t.Fatalf("checkVaultCoverage reported %q as ok", err)
	}
	for _, want := range []string{"2 live units have no model-a vector", "vectors under other models: model-b on 1 live unit", "nooma reindex", "not in this version yet"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("checkVaultCoverage = %q, want it to say %q", err, want)
		}
	}
	// The configured model is the one they lack, never one of the others.
	if strings.Contains(err.Error(), "model-a on") {
		t.Errorf("checkVaultCoverage = %q, names the configured model among the others", err)
	}
}

// TestCheckVaultCoverage_OKWhenEveryLiveUnitHoldsTheModel, and with no
// embedding model configured it says it could not judge.
func TestCheckVaultCoverage_OKWhenEveryLiveUnitHoldsTheModel(t *testing.T) {
	vault, cfg, _ := doctorVault(t, func(db *sqlite.Vault) {
		seedUnits(t, db, map[string]string{"u-a": "model-a"}, "u-a")
	})
	if err := checkVaultCoverage(vault, withEmbeddingModel(cfg, "model-a")); err != nil {
		t.Errorf("checkVaultCoverage = %v, want nil", err)
	}
	err := checkVaultCoverage(vault, cfg)
	if d, ok := err.(doctorOKDetail); !ok || !strings.Contains(d.Error(), "no embedding model configured") {
		t.Errorf("checkVaultCoverage with no embedding task = %v, want an ok line saying no model is configured", err)
	}
}

// TestCheckSearchIndex_OKOnACoherentVault, and its FAIL wording names both
// kinds of gap and the fix.
func TestCheckSearchIndex_OKOnACoherentVault(t *testing.T) {
	vault, cfg, _ := doctorVault(t, func(db *sqlite.Vault) { seedUnits(t, db, nil, "u-1", "u-2") })
	if err := checkSearchIndex(vault, cfg); err != nil {
		t.Errorf("checkSearchIndex on a coherent vault = %v, want nil", err)
	}

	err := searchIndexError(sqlite.SearchIndexGaps{Missing: 2, Orphaned: 1})
	for _, want := range []string{"2 units are missing from the word index", "1 entry names no unit", "nooma reindex", "not in this version yet"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("searchIndexError = %v, want it to say %q", err, want)
		}
	}
	if err := searchIndexError(sqlite.SearchIndexGaps{}); err != nil {
		t.Errorf("searchIndexError(none) = %v, want nil", err)
	}
}

// TestWarnUnembedded_OneLoudLineWhenLiveUnitsLackTheModel: serve's boot says,
// once, how many live units recall can find only by their words.
func TestWarnUnembedded_OneLoudLineWhenLiveUnitsLackTheModel(t *testing.T) {
	_, cfg, dbPath := doctorVault(t, func(db *sqlite.Vault) {
		seedUnits(t, db, map[string]string{"u-a": "model-a", "u-b": "model-b"}, "u-a", "u-b", "u-bare")
	})
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	var buf bytes.Buffer
	warnUnembedded(context.Background(), db, withEmbeddingModel(cfg, "model-a"), &buf)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "WARNING:") ||
		!strings.Contains(lines[0], "2 live units have no model-a vector") || !strings.Contains(lines[0], "nooma doctor") {
		t.Errorf("boot warning = %q, want one WARNING line naming 2 units, model-a and nooma doctor", buf.String())
	}

	buf.Reset()
	warnUnembedded(context.Background(), db, cfg, &buf)
	if buf.Len() != 0 {
		t.Errorf("with no embedding model configured the boot warned: %q", buf.String())
	}

	_, cfg2, dbPath2 := doctorVault(t, func(db *sqlite.Vault) {
		seedUnits(t, db, map[string]string{"u-a": "model-a"}, "u-a")
	})
	db2, err := sqlite.Open(context.Background(), dbPath2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()
	buf.Reset()
	warnUnembedded(context.Background(), db2, withEmbeddingModel(cfg2, "model-a"), &buf)
	if buf.Len() != 0 {
		t.Errorf("a fully embedded vault warned: %q", buf.String())
	}
}

// TestServe_WarnsAtBootAboutUnembeddedUnits: runServe calls warnUnembedded on
// its error stream, read from the source as serve's other wiring gates are.
func TestServe_WarnsAtBootAboutUnembeddedUnits(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "serve.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && calleeName(c) == "warnUnembedded" && len(c.Args) == 4 {
			if id, ok := c.Args[3].(*ast.Ident); ok && id.Name == "errOut" {
				found = true
			}
		}
		return true
	})
	if !found {
		t.Error("serve.go never calls warnUnembedded(..., errOut)")
	}
}
