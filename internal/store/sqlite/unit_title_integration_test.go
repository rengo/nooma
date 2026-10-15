//go:build integration

package sqlite

import (
	"testing"
)

// TestMigration0009AddsUnitTitle: the migration adds one nullable TEXT column,
// units.title, and leaves every unit it finds without a title (spec R12; there
// is no backfill, a title cannot be recovered from the content).
func TestMigration0009AddsUnitTitle(t *testing.T) {
	db := backfilled(t, func(exec func(string, ...any)) { seedLegacyUnit(exec, "old") })

	var name, typ string
	var notNull int
	if err := db.QueryRow(`SELECT name, type, "notnull" FROM pragma_table_info('units') WHERE name = 'title'`).
		Scan(&name, &typ, &notNull); err != nil {
		t.Fatalf("units.title is not a column: %v", err)
	}
	if typ != "TEXT" || notNull != 0 {
		t.Errorf("units.title is %s notnull=%d, want a nullable TEXT", typ, notNull)
	}
	var title *string
	if err := db.QueryRow(`SELECT title FROM units WHERE id = 'old'`).Scan(&title); err != nil || title != nil {
		t.Errorf("a unit from before the migration has title %v, %v; want NULL", title, err)
	}
}
