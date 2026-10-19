//go:build integration

package sqlite

import "testing"

// TestMigration0011AddsAnEventsOwnLeads: the migration adds units.reminder_leads,
// nullable, so every unit from before follows the user's preferences (I40,
// ADR-0038).
func TestMigration0011AddsAnEventsOwnLeads(t *testing.T) {
	db := backfilled(t, func(exec func(string, ...any)) { seedLegacyUnit(exec, "old") })
	var typ string
	var notNull int
	if err := db.QueryRow(`SELECT type, "notnull" FROM pragma_table_info('units') WHERE name = 'reminder_leads'`).
		Scan(&typ, &notNull); err != nil || typ != "TEXT" || notNull != 0 {
		t.Fatalf("units.reminder_leads is %q notnull=%d, %v; want a nullable TEXT", typ, notNull, err)
	}
	var leads *string
	if err := db.QueryRow(`SELECT reminder_leads FROM units WHERE id = 'old'`).Scan(&leads); err != nil || leads != nil {
		t.Errorf("a unit from before the migration has reminder_leads %v, %v; want NULL", leads, err)
	}
}
