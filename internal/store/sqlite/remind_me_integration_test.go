//go:build integration

package sqlite

import (
	"testing"
)

// TestMigration0010AddsRemindMeAndTheLead: the migration adds units.remind_me
// (NOT NULL, default 0, so every unit from before reads as not asked) and
// config.remind_me_lead_minutes (nullable, so the lead resolves to its default
// until the user chooses; spec R3).
func TestMigration0010AddsRemindMeAndTheLead(t *testing.T) {
	db := backfilled(t, func(exec func(string, ...any)) { seedLegacyUnit(exec, "old") })

	for _, c := range []struct {
		table, column, typ string
		notNull            int
	}{
		{"units", "remind_me", "INTEGER", 1},
		{"config", "remind_me_lead_minutes", "INTEGER", 0},
	} {
		var typ string
		var notNull int
		if err := db.QueryRow(`SELECT type, "notnull" FROM pragma_table_info('`+c.table+`') WHERE name = ?`, c.column).
			Scan(&typ, &notNull); err != nil {
			t.Fatalf("%s.%s is not a column: %v", c.table, c.column, err)
		}
		if typ != c.typ || notNull != c.notNull {
			t.Errorf("%s.%s is %s notnull=%d, want %s notnull=%d", c.table, c.column, typ, notNull, c.typ, c.notNull)
		}
	}
	var remindMe int
	if err := db.QueryRow(`SELECT remind_me FROM units WHERE id = 'old'`).Scan(&remindMe); err != nil || remindMe != 0 {
		t.Errorf("a unit from before the migration has remind_me %d, %v; want 0", remindMe, err)
	}
}
