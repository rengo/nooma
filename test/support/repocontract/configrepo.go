package repocontract

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// ConfigHarness is what a ports.ConfigRepo implementation must offer the
// contract so it can put a config row in front of it directly.
//
// SeedConfig exists for the same reason repocontract.RelationHarness's
// SeedThreshold does (design §4.2's own precedent): ports.ConfigRepo has
// exactly one write method, RecordConsolidationRun (spec R2.6), and it
// sets only ConsolidationLastRunAt. Without a seed hook, Load's "every
// field carries the column's stored value as stored, including a corrupt
// one" contract (spec R2.4) could never construct a fixture that stores
// anything in the other five columns at all, corrupt or not.
type ConfigHarness interface {
	ports.ConfigRepo

	// SeedConfig inserts cfg directly into the backing store, bypassing
	// ports.ConfigRepo entirely. A nil field in cfg is left unset — the
	// store's own zero value / NULL for that column. Calling SeedConfig
	// more than once against the same harness replaces the row.
	SeedConfig(t *testing.T, cfg ports.VaultConfig)

	// RowExists reports whether the config singleton row exists in the
	// backing store, independent of what Load returns. Load's "an absent
	// row returns an all-nil VaultConfig" contract (spec R2.4) is
	// satisfiable by two different implementations: one that writes
	// nothing on an absent read (correct — owner ruling Q1), and one that
	// lazily creates an all-NULL row on the way out of Load (incorrect,
	// but indistinguishable from Load's return value alone — an absent
	// row and an all-NULL row both decode to every field nil). RowExists
	// is what tells them apart.
	RowExists(t *testing.T) bool

	// UpdatedAt reports the singleton row's updated_at, so a setter's
	// "updated_at becomes at" claim can be checked on both adapters.
	UpdatedAt(t *testing.T) time.Time
}

// RunConfigRepoLoad runs the ports.ConfigRepo.Load contract against a
// fresh repository instance, built by newRepo for every subtest. newRepo
// must return a repository holding no config row.
//
// Spec R2.4; design §3.4. An absent row returns an all-nil VaultConfig and
// a nil error; a present row returns every field as stored, including a
// corrupt one — ConfigRepo never sanitizes or defaults.
func RunConfigRepoLoad(t *testing.T, newRepo func(t *testing.T) ConfigHarness) {
	t.Helper()

	t.Run("an absent row returns an all-nil VaultConfig and a nil error", func(t *testing.T) {
		repo := newRepo(t)

		got, err := repo.Load(context.Background())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.WeightThreshold != nil || got.HysteresisMargin != nil || got.ConsolidationEnabled != nil ||
			got.GoalStagnationDays != nil || got.MentalLoadThreshold != nil || got.ConsolidationLastRunAt != nil {
			t.Errorf("Load(absent row) = %+v, want every field nil — an unwritten config row is the "+
				"normal state of a vault, not a failure", got)
		}
		if repo.RowExists(t) {
			t.Errorf("Load(absent row) created a config row as a side effect — Load must write " +
				"nothing to seed the row (spec R2.4, owner ruling Q1); only " +
				"RecordConsolidationRun may create it")
		}
	})

	// The genuine discriminator: a plausible wrong ConfigRepo "helpfully"
	// maps a non-finite WeightThreshold to the default or to nil on the
	// way out of Load, the same job consolidation.ResolveWeightThreshold
	// already owns (design §3.4). Only a corrupt fixture — not merely a
	// valid one — tells the correct "return it as stored" behaviour apart
	// from that wrong one; a fixture using only well-formed numbers would
	// pass a sanitizing Load exactly as easily as a faithful one.
	t.Run("a present row returns every field as stored, including a corrupt WeightThreshold", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		corrupt := math.NaN()
		goalDays := 30
		mentalLoad := 9
		enabled := false
		lastRun := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
		repo.SeedConfig(t, ports.VaultConfig{
			WeightThreshold:        &corrupt,
			GoalStagnationDays:     &goalDays,
			MentalLoadThreshold:    &mentalLoad,
			ConsolidationEnabled:   &enabled,
			ConsolidationLastRunAt: &lastRun,
			// HysteresisMargin deliberately left nil — proves a seeded row
			// can still carry a nil column, not just an absent row.
		})

		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.WeightThreshold == nil || !math.IsNaN(*got.WeightThreshold) {
			t.Errorf("Load().WeightThreshold = %v, want the stored NaN returned as-is — ConfigRepo "+
				"must never sanitize or default a corrupt value itself (spec R2.4)", got.WeightThreshold)
		}
		if got.GoalStagnationDays == nil || *got.GoalStagnationDays != goalDays {
			t.Errorf("Load().GoalStagnationDays = %v, want %d", got.GoalStagnationDays, goalDays)
		}
		if got.MentalLoadThreshold == nil || *got.MentalLoadThreshold != mentalLoad {
			t.Errorf("Load().MentalLoadThreshold = %v, want %d", got.MentalLoadThreshold, mentalLoad)
		}
		if got.ConsolidationEnabled == nil || *got.ConsolidationEnabled != enabled {
			t.Errorf("Load().ConsolidationEnabled = %v, want %v", got.ConsolidationEnabled, enabled)
		}
		if got.ConsolidationLastRunAt == nil || !got.ConsolidationLastRunAt.Equal(lastRun) {
			t.Errorf("Load().ConsolidationLastRunAt = %v, want %v", got.ConsolidationLastRunAt, lastRun)
		}
		if got.HysteresisMargin != nil {
			t.Errorf("Load().HysteresisMargin = %v, want nil — the seed left this column unset",
				*got.HysteresisMargin)
		}
	})
}

// RunRecordConsolidationRun runs the ports.ConfigRepo.RecordConsolidationRun
// contract against a fresh repository instance, built by newRepo for every
// subtest. newRepo must return a repository holding no config row.
//
// Spec R2.6; design §3.4. Writing to an absent row creates it with
// consolidation_last_run_at set to the given instant and every other field
// still nil (mirroring the migration's own SQL DEFAULTs at the fake's own
// zero-value level — the fake proves the shape of the lazy-create, PR 6's
// sqlite implementation proves the SQL DEFAULTs themselves). Writing to an
// existing row changes only that one field.
func RunRecordConsolidationRun(t *testing.T, newRepo func(t *testing.T) ConfigHarness) {
	t.Helper()

	t.Run("writing to an absent row creates it with every other field still nil", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		at := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

		if err := repo.RecordConsolidationRun(ctx, at); err != nil {
			t.Fatalf("RecordConsolidationRun: %v", err)
		}

		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.ConsolidationLastRunAt == nil || !got.ConsolidationLastRunAt.Equal(at) {
			t.Fatalf("Load().ConsolidationLastRunAt = %v, want %v", got.ConsolidationLastRunAt, at)
		}
		if got.WeightThreshold != nil || got.HysteresisMargin != nil || got.ConsolidationEnabled != nil ||
			got.GoalStagnationDays != nil || got.MentalLoadThreshold != nil {
			t.Errorf("Load() after a lazy-create = %+v, want every field but "+
				"ConsolidationLastRunAt still nil at the fake's own zero-value level", got)
		}
	})

	// The boundary this case exists for: a plausible wrong
	// RecordConsolidationRun could re-zero every other seeded field on
	// every write (a naive INSERT OR REPLACE with no ON CONFLICT DO
	// UPDATE, or a fake that always overwrites the whole struct) instead
	// of touching only consolidation_last_run_at. Seeding the other five
	// fields first, then writing twice, is what tells "changes only this
	// one column" apart from "replaces the row".
	t.Run("writing to an existing row changes only consolidation_last_run_at", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		goalDays := 30
		repo.SeedConfig(t, ports.VaultConfig{GoalStagnationDays: &goalDays})

		first := time.Date(2026, 8, 8, 3, 0, 0, 0, time.UTC)
		second := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
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
		if got.GoalStagnationDays == nil || *got.GoalStagnationDays != goalDays {
			t.Errorf("Load().GoalStagnationDays = %v, want the seeded %d untouched by two "+
				"RecordConsolidationRun calls", got.GoalStagnationDays, goalDays)
		}
		if u := repo.UpdatedAt(t); !u.Equal(second) {
			t.Errorf("updated_at = %v, want the second write's instant %v", u, second)
		}
	})
}

// RunSetReminderPrefs runs the ports.ConfigRepo.SetReminderPrefs contract
// (ui-preferences design D2). The four preference columns are nullable on
// every adapter, so unlike the two suites above this one runs against the
// real store too: a nil field of the update leaves its column as it is, a
// non-nil one writes it, and no other column moves.
func RunSetReminderPrefs(t *testing.T, newRepo func(t *testing.T) ConfigHarness) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	str := func(s string) *string { return &s }

	t.Run("an absent row is created holding only the fields written", func(t *testing.T) {
		repo := newRepo(t)
		if err := repo.SetReminderPrefs(ctx, ports.ReminderPrefsUpdate{QuietHoursEnd: str("08:30")}, at); err != nil {
			t.Fatalf("SetReminderPrefs: %v", err)
		}
		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.QuietHoursEnd == nil || *got.QuietHoursEnd != "08:30" {
			t.Errorf("QuietHoursEnd = %v, want 08:30", got.QuietHoursEnd)
		}
		if got.EventReminderLeads != nil || got.DateOnlyReminderAt != nil || got.EveningReminderAt != nil || got.ConsolidationLastRunAt != nil {
			t.Errorf("Load() = %+v, want only QuietHoursEnd written", got)
		}
	})

	t.Run("an existing row changes only the fields written, all four at once", func(t *testing.T) {
		repo := newRepo(t)
		goalDays := 30
		repo.SeedConfig(t, ports.VaultConfig{GoalStagnationDays: &goalDays, DateOnlyReminderAt: str("10:00"), EveningReminderAt: str("21:00")})
		if err := repo.SetReminderPrefs(ctx, ports.ReminderPrefsUpdate{
			EventReminderLeads: str("[1440,60]"), DateOnlyReminderAt: str("08:00"), QuietHoursEnd: str("06:00"), EveningReminderAt: str("23:00"),
		}, at); err != nil {
			t.Fatalf("SetReminderPrefs: %v", err)
		}
		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		for name, c := range map[string]struct {
			got  *string
			want string
		}{
			"EventReminderLeads": {got.EventReminderLeads, "[1440,60]"},
			"DateOnlyReminderAt": {got.DateOnlyReminderAt, "08:00"},
			"QuietHoursEnd":      {got.QuietHoursEnd, "06:00"},
			"EveningReminderAt":  {got.EveningReminderAt, "23:00"},
		} {
			if c.got == nil || *c.got != c.want {
				t.Errorf("%s = %v, want %s", name, c.got, c.want)
			}
		}
		if got.GoalStagnationDays == nil || *got.GoalStagnationDays != goalDays {
			t.Errorf("GoalStagnationDays = %v, want the seeded %d untouched", got.GoalStagnationDays, goalDays)
		}
		if u := repo.UpdatedAt(t); !u.Equal(at) {
			t.Errorf("updated_at = %v, want the write's instant %v", u, at)
		}
	})

	t.Run("the remind-me lead is written alone, and an update without it keeps it", func(t *testing.T) {
		repo := newRepo(t)
		lead := func(m int) *int { return &m }
		repo.SeedConfig(t, ports.VaultConfig{EventReminderLeads: str("[300]"), QuietHoursEnd: str("05:00")})
		if err := repo.SetReminderPrefs(ctx, ports.ReminderPrefsUpdate{RemindMeLeadMinutes: lead(30)}, at); err != nil {
			t.Fatalf("SetReminderPrefs: %v", err)
		}
		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.RemindMeLeadMinutes == nil || *got.RemindMeLeadMinutes != 30 {
			t.Errorf("RemindMeLeadMinutes = %v, want 30", got.RemindMeLeadMinutes)
		}
		if got.EventReminderLeads == nil || *got.EventReminderLeads != "[300]" || got.QuietHoursEnd == nil || *got.QuietHoursEnd != "05:00" {
			t.Errorf("Load() = %+v, want the other preferences untouched by a lead-only write", got)
		}
		if err := repo.SetReminderPrefs(ctx, ports.ReminderPrefsUpdate{QuietHoursEnd: str("06:00")}, at); err != nil {
			t.Fatalf("SetReminderPrefs: %v", err)
		}
		if got, _ := repo.Load(ctx); got.RemindMeLeadMinutes == nil || *got.RemindMeLeadMinutes != 30 {
			t.Errorf("RemindMeLeadMinutes = %v, want the stored 30 kept by an update that leaves it out", got.RemindMeLeadMinutes)
		}
	})

	// One subtest per column: all four seeded, an update that names every
	// column but one, and the one left out keeps its value. Each COALESCE is
	// what the subtest for its column guards.
	seeded := ports.VaultConfig{EventReminderLeads: str("[300]"), DateOnlyReminderAt: str("10:00"), QuietHoursEnd: str("05:00"), EveningReminderAt: str("21:00")}
	for _, col := range []struct {
		name  string
		leave func(u *ports.ReminderPrefsUpdate)
		read  func(c ports.VaultConfig) *string
		want  string
	}{
		{"event_reminder_leads", func(u *ports.ReminderPrefsUpdate) { u.EventReminderLeads = nil }, func(c ports.VaultConfig) *string { return c.EventReminderLeads }, "[300]"},
		{"date_only_reminder_at", func(u *ports.ReminderPrefsUpdate) { u.DateOnlyReminderAt = nil }, func(c ports.VaultConfig) *string { return c.DateOnlyReminderAt }, "10:00"},
		{"quiet_hours_end", func(u *ports.ReminderPrefsUpdate) { u.QuietHoursEnd = nil }, func(c ports.VaultConfig) *string { return c.QuietHoursEnd }, "05:00"},
		{"evening_reminder_at", func(u *ports.ReminderPrefsUpdate) { u.EveningReminderAt = nil }, func(c ports.VaultConfig) *string { return c.EveningReminderAt }, "21:00"},
	} {
		t.Run("a stored "+col.name+" left out of an update keeps its value", func(t *testing.T) {
			repo := newRepo(t)
			repo.SeedConfig(t, seeded)
			u := ports.ReminderPrefsUpdate{EventReminderLeads: str("[60]"), DateOnlyReminderAt: str("08:00"), QuietHoursEnd: str("06:00"), EveningReminderAt: str("23:00")}
			col.leave(&u)
			if err := repo.SetReminderPrefs(ctx, u, at); err != nil {
				t.Fatalf("SetReminderPrefs: %v", err)
			}
			got, err := repo.Load(ctx)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if v := col.read(got); v == nil || *v != col.want {
				t.Errorf("%s = %v, want the stored %s kept", col.name, v, col.want)
			}
		})
	}
}

// RunConfigSetters runs the contract of the five admin setters
// (m4e2-admin design §6, C1). FX-C: every column is seeded to a distinct
// non-default value before a setter runs, so a setter that writes another
// column, or resets the row, moves something the test sees.
func RunConfigSetters(t *testing.T, newRepo func(t *testing.T) ConfigHarness) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	str := func(s string) *string { return &s }
	f64 := func(v float64) *float64 { return &v }
	num := func(v int) *int { return &v }
	flag := func(v bool) *bool { return &v }
	lastRun := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

	seeded := func() ports.VaultConfig {
		return ports.VaultConfig{
			WeightThreshold: f64(0.7), HysteresisMargin: f64(0.11), ConsolidationEnabled: flag(false),
			GoalStagnationDays: num(30), MentalLoadThreshold: num(9), ConsolidationLastRunAt: &lastRun,
			EventReminderLeads: str("[300]"), DateOnlyReminderAt: str("10:00"),
			QuietHoursEnd: str("05:00"), EveningReminderAt: str("21:00"),
		}
	}

	// M5 link 5 (doc 02 §9, ADR-0055): the goal cadence's one write.
	t.Run("LearnGoalStagnationDays writes the value and its stamp, unpinned", func(t *testing.T) {
		repo := newRepo(t)
		repo.SeedConfig(t, seeded())
		if err := repo.LearnGoalStagnationDays(ctx, 28, at); err != nil {
			t.Fatalf("LearnGoalStagnationDays: %v", err)
		}
		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got.GoalStagnationDays == nil || *got.GoalStagnationDays != 28 || got.GoalStagnationPinned ||
			got.GoalCalibratedAt == nil || !got.GoalCalibratedAt.Equal(at) {
			t.Errorf("Load() = days %v, pinned %v, calibrated %v; want 28, unpinned, at %v", got.GoalStagnationDays, got.GoalStagnationPinned, got.GoalCalibratedAt, at)
		}
	})
	t.Run("PinGoalStagnationDays sets the value and pins it; ReleaseGoalStagnationDays unpins and stamps", func(t *testing.T) {
		repo := newRepo(t)
		if err := repo.LearnGoalStagnationDays(ctx, 28, at); err != nil {
			t.Fatal(err)
		}
		undone := at.Add(time.Hour)
		if err := repo.PinGoalStagnationDays(ctx, 21, undone); err != nil {
			t.Fatalf("PinGoalStagnationDays: %v", err)
		}
		got, _ := repo.Load(ctx)
		if *got.GoalStagnationDays != 21 || !got.GoalStagnationPinned {
			t.Fatalf("after the pin: days %v, pinned %v; want 21, pinned", *got.GoalStagnationDays, got.GoalStagnationPinned)
		}
		released := at.Add(2 * time.Hour)
		if err := repo.ReleaseGoalStagnationDays(ctx, released); err != nil {
			t.Fatalf("ReleaseGoalStagnationDays: %v", err)
		}
		got, _ = repo.Load(ctx)
		if *got.GoalStagnationDays != 21 || got.GoalStagnationPinned || got.GoalCalibratedAt == nil || !got.GoalCalibratedAt.Equal(released) {
			t.Errorf("after the release: days %v, pinned %v, calibrated %v; want 21, unpinned, at %v", *got.GoalStagnationDays, got.GoalStagnationPinned, got.GoalCalibratedAt, released)
		}
		if err := repo.ReleaseGoalStagnationDays(ctx, released); !errors.Is(err, ports.ErrNotPinned) {
			t.Errorf("releasing it again = %v, want ErrNotPinned", err)
		}
	})

	t.Run("LearnGoalStagnationDays refuses a pinned value and changes nothing", func(t *testing.T) {
		repo := newRepo(t)
		if err := repo.SetGoalStagnationDays(ctx, 45, at); err != nil {
			t.Fatal(err)
		}
		if err := repo.LearnGoalStagnationDays(ctx, 52, at.Add(time.Hour)); !errors.Is(err, ports.ErrGoalStagnationPinned) {
			t.Fatalf("LearnGoalStagnationDays over a pin = %v, want ErrGoalStagnationPinned", err)
		}
		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if *got.GoalStagnationDays != 45 || !got.GoalStagnationPinned || got.GoalCalibratedAt != nil {
			t.Errorf("Load() = days %v, pinned %v, calibrated %v; want the user's 45, pinned, never calibrated", *got.GoalStagnationDays, got.GoalStagnationPinned, got.GoalCalibratedAt)
		}
	})

	for _, c := range []struct {
		name  string
		set   func(r ConfigHarness) error
		moves func(c *ports.VaultConfig)
	}{
		{"SetWeightThreshold", func(r ConfigHarness) error { return r.SetWeightThreshold(ctx, 0.9, at) },
			func(c *ports.VaultConfig) { c.WeightThreshold = f64(0.9) }},
		{"SetHysteresisMargin", func(r ConfigHarness) error { return r.SetHysteresisMargin(ctx, 0.25, at) },
			func(c *ports.VaultConfig) { c.HysteresisMargin = f64(0.25) }},
		// An Admin save is the user's word: it pins the value (client
		// answer 6 of 2026-10-27), so the learn pass never writes it.
		{"SetGoalStagnationDays", func(r ConfigHarness) error { return r.SetGoalStagnationDays(ctx, 45, at) },
			func(c *ports.VaultConfig) { c.GoalStagnationDays = num(45); c.GoalStagnationPinned = true }},
		{"SetMentalLoadThreshold", func(r ConfigHarness) error { return r.SetMentalLoadThreshold(ctx, 12, at) },
			func(c *ports.VaultConfig) { c.MentalLoadThreshold = num(12) }},
		{"SetConsolidationEnabled", func(r ConfigHarness) error { return r.SetConsolidationEnabled(ctx, true, at) },
			func(c *ports.VaultConfig) { c.ConsolidationEnabled = flag(true) }},
	} {
		t.Run(c.name+" moves only its own column and stamps updated_at", func(t *testing.T) {
			repo := newRepo(t)
			repo.SeedConfig(t, seeded())
			before, err := repo.Load(ctx)
			if err != nil {
				t.Fatalf("Load (before): %v", err)
			}
			if err := c.set(repo); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			got, err := repo.Load(ctx)
			if err != nil {
				t.Fatalf("Load (after): %v", err)
			}
			want := before
			c.moves(&want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Load() after %s = %+v, want only its own column changed: %+v", c.name, got, want)
			}
			if u := repo.UpdatedAt(t); !u.Equal(at) {
				t.Errorf("updated_at = %v, want the write's instant %v", u, at)
			}
		})

		t.Run(c.name+" creates an absent row", func(t *testing.T) {
			repo := newRepo(t)
			if err := c.set(repo); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if !repo.RowExists(t) {
				t.Errorf("%s on an absent row left no row", c.name)
			}
			if u := repo.UpdatedAt(t); !u.Equal(at) {
				t.Errorf("updated_at = %v, want %v", u, at)
			}
		})
	}
}
