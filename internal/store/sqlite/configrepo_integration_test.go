//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// configFixtureTime is a fixed, whole-second UTC instant — this suite does
// not exercise clock behaviour beyond "which instant was passed", so a
// literal keeps every fixture identical.
var configFixtureTime = time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

// Deviation from tasks.md 6.1/6.3's literal text, disclosed: this file does
// NOT call repocontract.RunConfigRepoLoad or repocontract.RunRecordConsolidationRun
// against the real sqlite ConfigRepo. Both functions assert that an unseeded
// field decodes to nil (repocontract/configrepo.go's own doc comment: "at
// the fake's own zero-value level"), which is exactly what
// test/support/memrepo/config.go:52 already states sqlite cannot reproduce:
// migration 0002:63-67 declares weight_threshold, hysteresis_margin,
// consolidation_enabled, goal_stagnation_days and mental_load_threshold
// NOT NULL DEFAULT <value> — there is no way to store or lazily-create a
// real NULL in any of those five columns, so "left unset" on this adapter
// can only ever decode to the column's own SQL DEFAULT, never to nil. The
// two tests below assert the schema-honest equivalent: an ABSENT ROW (not
// an absent value) decodes to all-nil, and a lazily-created row decodes to
// the migration's real defaults, read off disk, never a Go literal
// duplicating them (design §3.4, spec R2.6).

// TestConfigRepo_LoadAbsentRowReturnsAllNilAndWritesNothing covers
// RunConfigRepoLoad's one subtest that IS schema-compatible: no row at all
// (spec R2.4, design §3.4).
func TestConfigRepo_LoadAbsentRowReturnsAllNilAndWritesNothing(t *testing.T) {
	v := openTestVault(t)
	repo := NewConfigRepo(v)

	got, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.WeightThreshold != nil || got.HysteresisMargin != nil || got.ConsolidationEnabled != nil ||
		got.GoalStagnationDays != nil || got.MentalLoadThreshold != nil || got.ConsolidationLastRunAt != nil {
		t.Errorf("Load(absent row) = %+v, want every field nil", got)
	}
	if configRowExists(t, v.db) {
		t.Error("Load(absent row) created a config row as a side effect — Load must write nothing " +
			"to seed the row (spec R2.4, owner ruling Q1); only RecordConsolidationRun may create it")
	}
}

// TestConfigRepo_LoadPresentRowReturnsStoredValuesIncludingCorruptOnes seeds
// every NOT NULL column directly (a real row can never leave one of them
// unset) plus an out-of-[0,1]-range WeightThreshold, and asserts Load
// returns them as-is — ConfigRepo never sanitizes or defaults (spec R2.4,
// design §3.4). The out-of-range value is the genuine discriminator: a
// plausible wrong Load "helpfully" clamps or defaults an out-of-range
// WeightThreshold, exactly consolidation.ResolveWeightThreshold's own job.
//
// Not math.NaN(): confirmed empirically (a throwaway repro against this
// exact driver, github.com/ncruces/go-sqlite3) that binding a NaN float64
// parameter into this NOT NULL REAL column fails with a NOT NULL constraint
// violation — the driver converts NaN to SQL NULL before the write reaches
// the column, so no write path through this driver's Exec/bind mechanism can
// ever produce a stored NaN in weight_threshold. UnitRepo.ApplyBoosts's own
// pre-BEGIN non-finite refusal (design §3.1(e)) already avoids relying on
// the driver here; an out-of-range-but-finite value is what a real corrupt
// row looks like on this adapter.
func TestConfigRepo_LoadPresentRowReturnsStoredValuesIncludingCorruptOnes(t *testing.T) {
	v := openTestVault(t)
	repo := NewConfigRepo(v)

	const corruptWeightThreshold = 5.5 // outside consolidation's [0, WeightCeiling] range
	_, err := v.db.ExecContext(context.Background(), `
INSERT INTO config (id, weight_threshold, hysteresis_margin, consolidation_enabled,
                     goal_stagnation_days, mental_load_threshold, consolidation_last_run_at, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?)`,
		corruptWeightThreshold, 0.2, 0, 30, 9, configFixtureTime.Format(unitTimeLayout), configFixtureTime.Format(unitTimeLayout),
	)
	if err != nil {
		t.Fatalf("seeding config row: %v", err)
	}

	got, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.WeightThreshold == nil || *got.WeightThreshold != corruptWeightThreshold {
		t.Errorf("Load().WeightThreshold = %v, want the stored out-of-range %v returned as-is", got.WeightThreshold, corruptWeightThreshold)
	}
	if got.HysteresisMargin == nil || *got.HysteresisMargin != 0.2 {
		t.Errorf("Load().HysteresisMargin = %v, want 0.2", got.HysteresisMargin)
	}
	if got.ConsolidationEnabled == nil || *got.ConsolidationEnabled != false {
		t.Errorf("Load().ConsolidationEnabled = %v, want false", got.ConsolidationEnabled)
	}
	if got.GoalStagnationDays == nil || *got.GoalStagnationDays != 30 {
		t.Errorf("Load().GoalStagnationDays = %v, want 30", got.GoalStagnationDays)
	}
	if got.MentalLoadThreshold == nil || *got.MentalLoadThreshold != 9 {
		t.Errorf("Load().MentalLoadThreshold = %v, want 9", got.MentalLoadThreshold)
	}
	if got.ConsolidationLastRunAt == nil || !got.ConsolidationLastRunAt.Equal(configFixtureTime) {
		t.Errorf("Load().ConsolidationLastRunAt = %v, want %v", got.ConsolidationLastRunAt, configFixtureTime)
	}
}

// TestConfigRepo_LoadMalformedConsolidationLastRunAtErrors is task 6.1's own
// sqlite-specific case: a consolidation_last_run_at TEXT value that does not
// parse as unitTimeLayout must error, never decode to nil — design §3.4's
// "a malformed value is an error, not nil" rule. Reading it as nil would
// turn SelectConnectSources into a whole-live-pool read instead of
// surfacing the corruption.
func TestConfigRepo_LoadMalformedConsolidationLastRunAtErrors(t *testing.T) {
	v := openTestVault(t)
	repo := NewConfigRepo(v)

	_, err := v.db.ExecContext(context.Background(),
		`INSERT INTO config (id, consolidation_last_run_at, updated_at) VALUES (1, ?, ?)`,
		"not-a-timestamp", configFixtureTime.Format(unitTimeLayout),
	)
	if err != nil {
		t.Fatalf("seeding malformed config row: %v", err)
	}

	if _, err := repo.Load(context.Background()); err == nil {
		t.Fatal("Load with a malformed consolidation_last_run_at returned a nil error, want an error")
	}
}

// TestConfigRepo_RecordConsolidationRunLazyCreateUsesMigrationDefaults is
// task 6.3's own SQL-specific case: writing to an absent row creates it with
// the migration's own SQL DEFAULTs on every other column, read off disk via
// migrationColumnDefault — never a Go literal duplicating them (spec R2.6,
// design §3.4/§5.2).
func TestConfigRepo_RecordConsolidationRunLazyCreateUsesMigrationDefaults(t *testing.T) {
	v := openTestVault(t)
	repo := NewConfigRepo(v)

	if err := repo.RecordConsolidationRun(context.Background(), configFixtureTime); err != nil {
		t.Fatalf("RecordConsolidationRun: %v", err)
	}

	got, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ConsolidationLastRunAt == nil || !got.ConsolidationLastRunAt.Equal(configFixtureTime) {
		t.Fatalf("Load().ConsolidationLastRunAt = %v, want %v", got.ConsolidationLastRunAt, configFixtureTime)
	}

	wantWeightThreshold := migrationColumnDefaultFloat(t, "weight_threshold")
	if got.WeightThreshold == nil || *got.WeightThreshold != wantWeightThreshold {
		t.Errorf("Load().WeightThreshold = %v, want the migration DEFAULT %v", got.WeightThreshold, wantWeightThreshold)
	}
	wantHysteresisMargin := migrationColumnDefaultFloat(t, "hysteresis_margin")
	if got.HysteresisMargin == nil || *got.HysteresisMargin != wantHysteresisMargin {
		t.Errorf("Load().HysteresisMargin = %v, want the migration DEFAULT %v", got.HysteresisMargin, wantHysteresisMargin)
	}
	wantConsolidationEnabled := migrationColumnDefaultInt(t, "consolidation_enabled") != 0
	if got.ConsolidationEnabled == nil || *got.ConsolidationEnabled != wantConsolidationEnabled {
		t.Errorf("Load().ConsolidationEnabled = %v, want the migration DEFAULT %v", got.ConsolidationEnabled, wantConsolidationEnabled)
	}
	wantGoalStagnationDays := migrationColumnDefaultInt(t, "goal_stagnation_days")
	if got.GoalStagnationDays == nil || *got.GoalStagnationDays != wantGoalStagnationDays {
		t.Errorf("Load().GoalStagnationDays = %v, want the migration DEFAULT %v", got.GoalStagnationDays, wantGoalStagnationDays)
	}
	wantMentalLoadThreshold := migrationColumnDefaultInt(t, "mental_load_threshold")
	if got.MentalLoadThreshold == nil || *got.MentalLoadThreshold != wantMentalLoadThreshold {
		t.Errorf("Load().MentalLoadThreshold = %v, want the migration DEFAULT %v", got.MentalLoadThreshold, wantMentalLoadThreshold)
	}
}

// TestConfigRepo_RecordConsolidationRunExistingRowChangesOnlyOneColumn
// proves the ON CONFLICT clause never re-applies a default to an
// already-seeded column: a plausible wrong RecordConsolidationRun could
// re-zero every other column on every write (a naive INSERT OR REPLACE, or
// an UPSERT with too wide a SET list) instead of touching only
// consolidation_last_run_at and updated_at.
func TestConfigRepo_RecordConsolidationRunExistingRowChangesOnlyOneColumn(t *testing.T) {
	v := openTestVault(t)
	repo := NewConfigRepo(v)
	ctx := context.Background()

	_, err := v.db.ExecContext(ctx, `
INSERT INTO config (id, weight_threshold, hysteresis_margin, consolidation_enabled,
                     goal_stagnation_days, mental_load_threshold, updated_at)
VALUES (1, 0.7, 0.1, 1, 30, 9, ?)`, configFixtureTime.Format(unitTimeLayout))
	if err != nil {
		t.Fatalf("seeding config row: %v", err)
	}

	first := configFixtureTime
	second := configFixtureTime.Add(24 * time.Hour)
	if err := repo.RecordConsolidationRun(ctx, first); err != nil {
		t.Fatalf("first RecordConsolidationRun: %v", err)
	}
	if err := repo.RecordConsolidationRun(ctx, second); err != nil {
		t.Fatalf("second RecordConsolidationRun: %v", err)
	}

	got, err := repo.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ConsolidationLastRunAt == nil || !got.ConsolidationLastRunAt.Equal(second) {
		t.Errorf("Load().ConsolidationLastRunAt = %v, want the second write's %v", got.ConsolidationLastRunAt, second)
	}
	if got.WeightThreshold == nil || *got.WeightThreshold != 0.7 {
		t.Errorf("Load().WeightThreshold = %v, want the seeded 0.7 untouched by two RecordConsolidationRun calls", got.WeightThreshold)
	}
	if got.GoalStagnationDays == nil || *got.GoalStagnationDays != 30 {
		t.Errorf("Load().GoalStagnationDays = %v, want the seeded 30 untouched", got.GoalStagnationDays)
	}
}

// configRowExists reports whether config's singleton row (id = 1) exists,
// independent of what Load returns — repocontract.ConfigHarness's own
// RowExists rationale: an absent row and an all-NULL row would otherwise be
// indistinguishable from Load's return value alone.
func configRowExists(t *testing.T, db *sql.DB) bool {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM config WHERE id = 1`).Scan(&n); err != nil {
		t.Fatalf("counting config rows: %v", err)
	}
	return n > 0
}

// migrationColumnDefaultFloat and migrationColumnDefaultInt read config's
// column DEFAULT directly off the embedded migration text (migrationFS,
// migrate.go:20) — the same DDL-pinning mechanism
// test/conformance/consolidation_defaults_ddl_test.go uses from outside this
// package, reimplemented locally here because that package's
// migrationSQLText/extractTableBody/columnDefault helpers are unexported and
// this file cannot import test/conformance without an import cycle risk
// through the sqlite package it exercises.
func migrationColumnDefaultFloat(t *testing.T, column string) float64 {
	t.Helper()
	literal := migrationColumnDefault(t, column)
	v, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		t.Fatalf("config.%s DEFAULT %q does not parse as a float: %v", column, literal, err)
	}
	return v
}

func migrationColumnDefaultInt(t *testing.T, column string) int {
	t.Helper()
	literal := migrationColumnDefault(t, column)
	v, err := strconv.Atoi(literal)
	if err != nil {
		t.Fatalf("config.%s DEFAULT %q does not parse as an int: %v", column, literal, err)
	}
	return v
}

func migrationColumnDefault(t *testing.T, column string) string {
	t.Helper()
	body, ok := extractConfigTableBody(t)
	if !ok {
		t.Fatal("no CREATE TABLE config found in the embedded migrations")
	}
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != column {
			continue
		}
		for i, f := range fields {
			if strings.EqualFold(f, "DEFAULT") && i+1 < len(fields) {
				return strings.TrimRight(fields[i+1], ",")
			}
		}
	}
	t.Fatalf("config.%s declares no DEFAULT in the migrations", column)
	return ""
}

// extractConfigTableBody concatenates every embedded migration file, sorted
// by name, and returns the text between the outermost parentheses of
// "CREATE TABLE config (...)" — the same depth-counted scan
// test/conformance/i13_learning_signal_test.go's extractTableBody uses.
func extractConfigTableBody(t *testing.T) (body string, ok bool) {
	t.Helper()

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		content, err := fs.ReadFile(migrationFS, "migrations/"+name)
		if err != nil {
			t.Fatalf("read migrations/%s: %v", name, err)
		}
		sb.Write(content)
		sb.WriteString("\n")
	}
	sqlText := sb.String()

	marker := "CREATE TABLE config"
	idx := strings.Index(sqlText, marker)
	if idx == -1 {
		return "", false
	}
	rest := sqlText[idx+len(marker):]
	open := strings.IndexByte(rest, '(')
	if open == -1 {
		return "", false
	}
	depth := 0
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return rest[open+1 : i], true
			}
		}
	}
	return "", false
}
