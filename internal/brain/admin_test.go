package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

var adminNow = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// adminSeed is FX-C: every column a distinct non-default value, so a write
// that moves another column, or resets the row, changes something a test sees.
func adminSeed() ports.VaultConfig {
	w, m, g, l, e := 0.7, 0.3, 40, 9, false
	return ports.VaultConfig{WeightThreshold: &w, HysteresisMargin: &m, GoalStagnationDays: &g, MentalLoadThreshold: &l, ConsolidationEnabled: &e}
}

type adminWorld struct {
	cfg *memrepo.Config
	rel *memrepo.Relations
	log *memrepo.DecisionLog
	svc *AdminService
}

// newAdminWorld builds the service over memrepo; seeded false is the second
// fixture: a vault with no config row.
func newAdminWorld(t *testing.T, seeded bool) adminWorld {
	t.Helper()
	w := adminWorld{cfg: memrepo.NewConfig(), rel: memrepo.NewRelations(), log: memrepo.NewDecisionLog()}
	if seeded {
		w.cfg.SeedConfig(t, adminSeed())
	}
	w.svc = NewAdminService(fixedClock{now: adminNow}, &seqIDs{}, w.cfg, w.rel, w.log)
	return w
}

func (w adminWorld) rows(t *testing.T) []ports.Decision {
	t.Helper()
	rows, err := w.log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	return rows
}

func (w adminWorld) load(t *testing.T) ports.VaultConfig {
	t.Helper()
	c, err := w.cfg.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func isRange(err error) bool {
	var re *ConfigRangeError
	return errors.As(err, &re)
}

func TestParseConfigField_IsTheClosedSetOfFive(t *testing.T) {
	for _, name := range []string{"weight_threshold", "hysteresis_margin", "goal_stagnation_days", "mental_load_threshold", "consolidation_enabled"} {
		if f, err := ParseConfigField(name); err != nil || string(f) != name {
			t.Errorf("ParseConfigField(%q) = %q, %v, want the field", name, f, err)
		}
	}
	for _, name := range []string{"relation_thresholds", "consolidation_last_run_at", "bogus", "", "Weight_Threshold"} {
		if _, err := ParseConfigField(name); !errors.Is(err, ErrUnknownConfigField) {
			t.Errorf("ParseConfigField(%q) error = %v, want ErrUnknownConfigField", name, err)
		}
	}
}

func TestAdmin_UpdateOfAFieldOutsideTheFiveWritesNothing(t *testing.T) {
	w := newAdminWorld(t, true)
	if _, err := w.svc.Update(context.Background(), ConfigField("relation_thresholds"), "0.5"); !errors.Is(err, ErrUnknownConfigField) {
		t.Fatalf("Update error = %v, want ErrUnknownConfigField", err)
	}
	if !reflect.DeepEqual(w.load(t), adminSeed()) || len(w.rows(t)) != 0 {
		t.Error("an unknown field changed the config or logged")
	}
}

// C3: the accepted set is exactly what Resolve returns unchanged.
func TestAdmin_Ranges(t *testing.T) {
	above2 := fmt.Sprint(math.Nextafter(2, 3))
	for _, tc := range []struct {
		field ConfigField
		raw   string
		ok    bool
	}{
		{ConfigHysteresisMargin, "0", true}, {ConfigHysteresisMargin, "0.5", true}, {ConfigHysteresisMargin, "-0.01", false},
		{ConfigHysteresisMargin, "NaN", false}, {ConfigHysteresisMargin, "+Inf", false}, {ConfigHysteresisMargin, "-Inf", false},
		{ConfigHysteresisMargin, "text", false}, {ConfigHysteresisMargin, "", false},
		{ConfigWeightThreshold, "0", true}, {ConfigWeightThreshold, "2.0", true}, {ConfigWeightThreshold, above2, false},
		{ConfigWeightThreshold, "-0.1", false}, {ConfigWeightThreshold, "NaN", false}, {ConfigWeightThreshold, "Inf", false},
		{ConfigGoalStagnationDays, "1", true}, {ConfigGoalStagnationDays, "0", false}, {ConfigGoalStagnationDays, "-3", false},
		{ConfigGoalStagnationDays, "2.5", false},
		{ConfigMentalLoadThreshold, "1", true}, {ConfigMentalLoadThreshold, "0", false}, {ConfigMentalLoadThreshold, "x", false},
		{ConfigConsolidationEnabled, "true", true}, {ConfigConsolidationEnabled, "false", true},
		// C12: exactly true or false, not ParseBool's spellings.
		{ConfigConsolidationEnabled, "1", false}, {ConfigConsolidationEnabled, "yes", false}, {ConfigConsolidationEnabled, "True", false},
		{ConfigConsolidationEnabled, "T", false},
	} {
		t.Run(fmt.Sprintf("%s=%q", tc.field, tc.raw), func(t *testing.T) {
			w := newAdminWorld(t, true)
			_, err := w.svc.Update(context.Background(), tc.field, tc.raw)
			switch {
			case tc.ok && err != nil:
				t.Errorf("Update refused an accepted value: %v", err)
			case !tc.ok && !isRange(err):
				t.Errorf("Update error = %v, want a *ConfigRangeError", err)
			}
		})
	}
}

// C16: a refused input changes nothing and logs nothing.
func TestAdmin_RejectedInputWritesNothing(t *testing.T) {
	w := newAdminWorld(t, true)
	for _, tc := range []struct {
		field ConfigField
		raw   string
	}{{ConfigHysteresisMargin, "-1"}, {ConfigWeightThreshold, "9"}, {ConfigGoalStagnationDays, "0"}, {ConfigMentalLoadThreshold, "abc"}, {ConfigConsolidationEnabled, "1"}} {
		if _, err := w.svc.Update(context.Background(), tc.field, tc.raw); !isRange(err) {
			t.Fatalf("%s=%q: error = %v, want a range error", tc.field, tc.raw, err)
		}
	}
	if !reflect.DeepEqual(w.load(t), adminSeed()) || len(w.rows(t)) != 0 {
		t.Error("a refused input changed the config or logged")
	}
}

// C4: a value already in force is a no-op, judged against the effective value.
func TestAdmin_SameValueLogsNothing(t *testing.T) {
	w := newAdminWorld(t, true)
	res, err := w.svc.Update(context.Background(), ConfigHysteresisMargin, "0.3")
	if err != nil || res.Changed {
		t.Fatalf("Update = %+v, %v, want unchanged and no error", res, err)
	}
	if !reflect.DeepEqual(w.load(t), adminSeed()) || len(w.rows(t)) != 0 || !w.cfg.UpdatedAt(t).IsZero() {
		t.Error("a same-value submission wrote or logged")
	}
}

func TestAdmin_DefaultOnAbsentRowWritesNothing(t *testing.T) {
	w := newAdminWorld(t, false)
	for _, tc := range []struct {
		field ConfigField
		raw   string
	}{
		{ConfigWeightThreshold, fmt.Sprint(consolidation.DefaultWeightThreshold)}, {ConfigHysteresisMargin, fmt.Sprint(focus.DefaultHysteresisMargin)},
		{ConfigGoalStagnationDays, fmt.Sprint(consolidation.DefaultGoalStagnationDays)}, {ConfigMentalLoadThreshold, fmt.Sprint(consolidation.DefaultMentalLoadThreshold)},
		{ConfigConsolidationEnabled, "true"},
	} {
		if res, err := w.svc.Update(context.Background(), tc.field, tc.raw); err != nil || res.Changed {
			t.Fatalf("%s=%s: Update = %+v, %v, want unchanged", tc.field, tc.raw, res, err)
		}
	}
	if w.cfg.RowExists(t) || len(w.rows(t)) != 0 {
		t.Error("submitting the defaults on a vault with no config row created a row or logged")
	}
}

// C5: an unset field written to a non-default logs the default as previous.
func TestAdmin_UnsetFieldWriteLogs(t *testing.T) {
	w := newAdminWorld(t, false)
	res, err := w.svc.Update(context.Background(), ConfigGoalStagnationDays, "28")
	if err != nil || !res.Changed {
		t.Fatalf("Update = %+v, %v, want a change", res, err)
	}
	if g := w.load(t).GoalStagnationDays; g == nil || *g != 28 {
		t.Fatalf("GoalStagnationDays = %v, want 28", g)
	}
	rows := w.rows(t)
	if len(rows) != 1 {
		t.Fatalf("%d log rows, want 1", len(rows))
	}
	ctx := decodeAdminRow(t, rows[0])
	if ctx.Previous["goal_stagnation_days"] != float64(consolidation.DefaultGoalStagnationDays) || ctx.Next["goal_stagnation_days"] != float64(28) {
		t.Errorf("context = %+v, want previous the default %d and next 28", ctx, consolidation.DefaultGoalStagnationDays)
	}
	if want := "the previous value was the default"; !strings.Contains(rows[0].Rationale, want) {
		t.Errorf("rationale %q does not say %q", rows[0].Rationale, want)
	}
}

// C5b: a corrupt stored value is not in force; submitting the fallback repairs it.
func TestAdmin_CorruptStoredValueIsRepaired(t *testing.T) {
	w := newAdminWorld(t, false)
	bad := -5.0
	w.cfg.SeedConfig(t, ports.VaultConfig{HysteresisMargin: &bad})
	res, err := w.svc.Update(context.Background(), ConfigHysteresisMargin, "0")
	if err != nil || !res.Changed {
		t.Fatalf("Update = %+v, %v, want a repair", res, err)
	}
	if m := w.load(t).HysteresisMargin; m == nil || *m != 0 {
		t.Errorf("HysteresisMargin = %v, want 0", m)
	}
	rows := w.rows(t)
	if len(rows) != 1 {
		t.Fatalf("%d log rows, want 1", len(rows))
	}
	// previous is the value the runtime had in force (ResolveMargin's 0), not
	// the corrupt -5 that was stored, and the rationale says why.
	if c := decodeAdminRow(t, rows[0]); c.Previous["hysteresis_margin"] != float64(0) || c.Next["hysteresis_margin"] != float64(0) {
		t.Errorf("context = %+v, want previous 0 (in force) and next 0", c)
	}
	if want := "the stored value -5 was invalid, so 0 was in force"; !strings.Contains(rows[0].Rationale, want) {
		t.Errorf("rationale %q does not say %q", rows[0].Rationale, want)
	}
}

// -0 parses as a negative zero; it must be stored and logged as 0.
func TestAdmin_NegativeZeroIsStoredAndLoggedAsZero(t *testing.T) {
	w := newAdminWorld(t, true)
	if _, err := w.svc.Update(context.Background(), ConfigHysteresisMargin, "-0"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if m := w.load(t).HysteresisMargin; m == nil || math.Signbit(*m) {
		t.Errorf("HysteresisMargin = %v, want a positive zero", m)
	}
	rows := w.rows(t)
	if len(rows) != 1 || strings.Contains(string(rows[0].Context), "-0") || strings.Contains(rows[0].Rationale, "-0") {
		t.Errorf("rows = %+v, want one row that never prints -0", rows)
	}
}

// C6 and C14: each field moves exactly its own column, and logs one row keyed
// by that column with the previous value in force and the new one.
func TestAdmin_EachFieldMovesExactlyItsOwnColumnAndLogsItsKey(t *testing.T) {
	for _, tc := range []struct {
		field      ConfigField
		raw        string
		prev, next any
		want       func(c *ports.VaultConfig)
	}{
		{ConfigWeightThreshold, "0.9", 0.7, 0.9, func(c *ports.VaultConfig) { v := 0.9; c.WeightThreshold = &v }},
		{ConfigHysteresisMargin, "0.1", 0.3, 0.1, func(c *ports.VaultConfig) { v := 0.1; c.HysteresisMargin = &v }},
		{ConfigGoalStagnationDays, "14", float64(40), float64(14), func(c *ports.VaultConfig) { v := 14; c.GoalStagnationDays = &v }},
		{ConfigMentalLoadThreshold, "3", float64(9), float64(3), func(c *ports.VaultConfig) { v := 3; c.MentalLoadThreshold = &v }},
		{ConfigConsolidationEnabled, "true", false, true, func(c *ports.VaultConfig) { v := true; c.ConsolidationEnabled = &v }},
	} {
		t.Run(string(tc.field), func(t *testing.T) {
			w := newAdminWorld(t, true)
			if res, err := w.svc.Update(context.Background(), tc.field, tc.raw); err != nil || !res.Changed || res.Field != tc.field {
				t.Fatalf("Update = %+v, %v", res, err)
			}
			want := adminSeed()
			tc.want(&want)
			if got := w.load(t); !reflect.DeepEqual(got, want) {
				t.Errorf("config = %+v, want only %s moved", got, tc.field)
			}
			if !w.cfg.UpdatedAt(t).Equal(adminNow) {
				t.Errorf("updated_at = %v, want %v", w.cfg.UpdatedAt(t), adminNow)
			}
			rows := w.rows(t)
			if len(rows) != 1 || rows[0].Action != ports.ActionConfigUpdated || !rows[0].OccurredAt.Equal(adminNow) {
				t.Fatalf("rows = %+v, want one config.updated at the write's instant", rows)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(rows[0].Context, &raw); err != nil || len(raw) != 3 {
				t.Fatalf("context %s: want exactly fields, previous, next (%v)", rows[0].Context, err)
			}
			ctx := decodeAdminRow(t, rows[0])
			col := string(tc.field)
			if !slices.Equal(ctx.Fields, []string{col}) || len(ctx.Previous) != 1 || len(ctx.Next) != 1 ||
				ctx.Previous[col] != tc.prev || ctx.Next[col] != tc.next {
				t.Errorf("context = %+v, want fields [%s], previous %v, next %v", ctx, col, tc.prev, tc.next)
			}
		})
	}
}

type adminRowContext struct {
	Fields   []string       `json:"fields"`
	Previous map[string]any `json:"previous"`
	Next     map[string]any `json:"next"`
}

func decodeAdminRow(t *testing.T, d ports.Decision) adminRowContext {
	t.Helper()
	var c adminRowContext
	if err := json.Unmarshal(d.Context, &c); err != nil {
		t.Fatalf("context %s: %v", d.Context, err)
	}
	return c
}

type failingAdminSetters struct{ *memrepo.Config }

func (failingAdminSetters) SetMentalLoadThreshold(context.Context, int, time.Time) error {
	return errors.New("disk full")
}

// C7: write first, then record; a failed write logs nothing.
func TestAdmin_ApplyFailureLogsNothing(t *testing.T) {
	w := newAdminWorld(t, true)
	svc := NewAdminService(fixedClock{now: adminNow}, &seqIDs{}, failingAdminSetters{w.cfg}, w.rel, w.log)
	_, err := svc.Update(context.Background(), ConfigMentalLoadThreshold, "3")
	if err == nil || errors.Is(err, ErrWriteLanded) {
		t.Fatalf("Update error = %v, want a plain write failure", err)
	}
	if len(w.rows(t)) != 0 {
		t.Error("a failed write logged a change")
	}
}

func TestAdmin_LogFailureAfterWriteIsVisible(t *testing.T) {
	w := newAdminWorld(t, true)
	svc := NewAdminService(fixedClock{now: adminNow}, &seqIDs{}, w.cfg, w.rel, memrepo.NewFailingDecisionLog(errors.New("log down")))
	res, err := svc.Update(context.Background(), ConfigMentalLoadThreshold, "3")
	var landed *WriteLandedError
	if !errors.Is(err, ErrWriteLanded) || !errors.As(err, &landed) || !landed.Record || landed.Signal {
		t.Fatalf("Update error = %v, want ErrWriteLanded with only the record missing", err)
	}
	if !res.Changed {
		t.Error("a landed write must report Changed")
	}
	if l := w.load(t).MentalLoadThreshold; l == nil || *l != 3 {
		t.Errorf("MentalLoadThreshold = %v, want the saved 3 (not rolled back)", l)
	}
}

// C9 and C10.
func TestAdmin_ViewListsOnlyTenConsolidationEffectsAndNeverARunWhenNone(t *testing.T) {
	w := newAdminWorld(t, false)
	ctx := context.Background()
	for i := 0; i < 11; i++ {
		rec(t, w.log, fmt.Sprintf("e%02d", i), ports.ActionArchiveArchived, adminNow.Add(time.Duration(i)*time.Minute))
	}
	rec(t, w.log, "newer-capture", ports.ActionCaptureClassify, adminNow.Add(time.Hour))
	view, err := w.svc.View(ctx)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if len(view.RecentEffects) != AdminRecentEffectRows {
		t.Fatalf("%d effects, want %d", len(view.RecentEffects), AdminRecentEffectRows)
	}
	if view.RecentEffects[0].ID != "e10" || view.RecentEffects[9].ID != "e01" {
		t.Errorf("effects run %s..%s, want newest first e10..e01", view.RecentEffects[0].ID, view.RecentEffects[9].ID)
	}
	for _, r := range view.RecentEffects {
		if r.ID == "newer-capture" {
			t.Error("a capture row is listed among the consolidation effects")
		}
	}
	if view.LastRun != nil {
		t.Errorf("LastRun = %v, want nil (never)", view.LastRun)
	}
}

func rec(t *testing.T, log *memrepo.DecisionLog, id string, a ports.DecisionAction, at time.Time) {
	t.Helper()
	if err := log.Record(context.Background(), ports.Decision{ID: id, Action: a, Rationale: "r", OccurredAt: at}); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestAdmin_ViewShowsValuesInForceStoredRowRunAndLearnedThresholds(t *testing.T) {
	w := newAdminWorld(t, true)
	ran := adminNow.Add(-8 * time.Hour)
	if err := w.cfg.RecordConsolidationRun(context.Background(), ran); err != nil {
		t.Fatal(err)
	}
	w.rel.SeedThreshold(t, "same_topic", 0.4, 0.6)
	w.rel.SeedThreshold(t, "duplicate", 0.2, 0.8)
	view, err := w.svc.View(context.Background())
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	want := AdminSettings{WeightThreshold: 0.7, HysteresisMargin: 0.3, GoalStagnationDays: 40, MentalLoadThreshold: 9, ConsolidationEnabled: false}
	if view.Settings != want {
		t.Errorf("Settings = %+v, want %+v", view.Settings, want)
	}
	if view.LastRun == nil || !view.LastRun.Equal(ran) || view.Stored.GoalStagnationDays == nil {
		t.Errorf("LastRun = %v, Stored = %+v, want the run and the stored row", view.LastRun, view.Stored)
	}
	learned := []ports.LearnedThreshold{
		{RelationType: "duplicate", Thresholds: relation.Thresholds{Persist: 0.2, Surface: 0.8}, Origin: ports.ThresholdOriginLearned},
		{RelationType: "same_topic", Thresholds: relation.Thresholds{Persist: 0.4, Surface: 0.6}, Origin: ports.ThresholdOriginLearned},
	}
	if !reflect.DeepEqual(view.Learned, learned) || view.OtherTypes != relation.Resolve(nil) {
		t.Errorf("Learned = %+v, OtherTypes = %+v", view.Learned, view.OtherTypes)
	}
}

func TestAdmin_ViewOnAnEmptyVaultShowsTheDefaults(t *testing.T) {
	view, err := newAdminWorld(t, false).svc.View(context.Background())
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	want := AdminSettings{consolidation.DefaultWeightThreshold, focus.DefaultHysteresisMargin, consolidation.DefaultGoalStagnationDays, consolidation.DefaultMentalLoadThreshold, true}
	if view.Settings != want || len(view.Learned) != 0 || len(view.RecentEffects) != 0 {
		t.Errorf("View = %+v, want the defaults and nothing learned or logged", view)
	}
}

// C15: the row Update writes is readable in the activity view.
func TestAdmin_TheRowUpdateWritesDecodesInTheActivityView(t *testing.T) {
	w := newAdminWorld(t, true)
	if _, err := w.svc.Update(context.Background(), ConfigHysteresisMargin, "0.1"); err != nil {
		t.Fatal(err)
	}
	page, err := NewActivityService(w.log).Page(context.Background(), "config", nil)
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("Page = %+v, %v, want one config row", page, err)
	}
	got := page.Rows[0].Change
	if len(got) != 1 || got[0].Name != "hysteresis_margin" || got[0].Previous != "0.3" || got[0].Next != "0.1" {
		t.Errorf("Change = %+v, want hysteresis_margin 0.3 -> 0.1", got)
	}
}

// C17: the service's exported surface is View and Update, so no method can
// share a name with a port setter (G5's naming rule).
func TestAdminServiceExportsOnlyViewAndUpdate(t *testing.T) {
	typ := reflect.TypeOf(&AdminService{})
	var got []string
	for i := 0; i < typ.NumMethod(); i++ {
		got = append(got, typ.Method(i).Name)
	}
	if want := []string{"Update", "View"}; !slices.Equal(got, want) {
		t.Errorf("exported methods = %v, want %v", got, want)
	}
}
