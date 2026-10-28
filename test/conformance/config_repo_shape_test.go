// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// TestConfigRepo_ExposesExactlyTheQ5Setters is m4e2-admin's G4: the admin
// page may change exactly five settings (design Q5), and the port is where
// that is closed. The method set is the readers and the two earlier writes,
// plus one setter per Q5 column, each taking a single scalar and an instant:
// a generic writer, a setter for relation thresholds or for
// consolidation_last_run_at, or a setter keyed by a string would all fail
// here before any caller could use them.
func TestConfigRepo_ExposesExactlyTheQ5Setters(t *testing.T) {
	for _, p := range configRepoShapeProblems(reflect.TypeOf((*ports.ConfigRepo)(nil)).Elem()) {
		t.Error(p)
	}
}

// Self-proof for G4: each shape the gate exists to refuse is fired on, and
// the five (plus the earlier methods) are silent.
type (
	q5Base interface {
		Load(context.Context) (ports.VaultConfig, error)
		RecordConsolidationRun(context.Context, time.Time) error
		SetReminderPrefs(context.Context, ports.ReminderPrefsUpdate, time.Time) error
	}
	q5Five interface {
		q5Base
		SetWeightThreshold(context.Context, float64, time.Time) error
		SetHysteresisMargin(context.Context, float64, time.Time) error
		SetGoalStagnationDays(context.Context, int, time.Time) error
		SetMentalLoadThreshold(context.Context, int, time.Time) error
		SetConsolidationEnabled(context.Context, bool, time.Time) error
		LearnGoalStagnationDays(context.Context, int, time.Time) error
		PinGoalStagnationDays(context.Context, int, time.Time) error
		ReleaseGoalStagnationDays(context.Context, time.Time) error
	}
	q5GenericWriter interface {
		q5Five
		SetConfig(context.Context, map[string]any, time.Time) error
	}
	q5RelationThreshold interface {
		q5Five
		SetRelationThreshold(context.Context, float64, time.Time) error
	}
	q5LastRunSetter interface {
		q5Five
		SetConsolidationLastRunAt(context.Context, time.Time, time.Time) error
	}
	q5StringKeyed interface {
		q5Base
		SetWeightThreshold(context.Context, string, time.Time) error
		SetHysteresisMargin(context.Context, float64, time.Time) error
		SetGoalStagnationDays(context.Context, int, time.Time) error
		SetMentalLoadThreshold(context.Context, int, time.Time) error
		SetConsolidationEnabled(context.Context, bool, time.Time) error
		LearnGoalStagnationDays(context.Context, int, time.Time) error
		PinGoalStagnationDays(context.Context, int, time.Time) error
		ReleaseGoalStagnationDays(context.Context, time.Time) error
	}
)

func TestConfigRepoShape_SelfProof(t *testing.T) {
	typeOf := func(p any) reflect.Type { return reflect.TypeOf(p).Elem() }
	for _, c := range []struct {
		name  string
		it    reflect.Type
		fires bool
	}{
		{"the five", typeOf((*q5Five)(nil)), false},
		{"a generic SetConfig(map...)", typeOf((*q5GenericWriter)(nil)), true},
		{"SetRelationThreshold", typeOf((*q5RelationThreshold)(nil)), true},
		{"SetConsolidationLastRunAt", typeOf((*q5LastRunSetter)(nil)), true},
		{"a string-key parameter", typeOf((*q5StringKeyed)(nil)), true},
	} {
		got := configRepoShapeProblems(c.it)
		if c.fires && len(got) == 0 {
			t.Errorf("%s: the gate stayed silent, want it to fire", c.name)
		}
		if !c.fires && len(got) != 0 {
			t.Errorf("%s: the gate fired: %v", c.name, got)
		}
	}
}

// configRepoShapeProblems describes how it departs from the Q5 shape: a
// method outside the allowed set, a missing one, or a setter whose signature
// is not (context.Context, <its scalar>, time.Time) error.
func configRepoShapeProblems(it reflect.Type) []string {
	scalar := map[string]reflect.Type{
		"SetWeightThreshold":      reflect.TypeOf(float64(0)),
		"SetHysteresisMargin":     reflect.TypeOf(float64(0)),
		"SetGoalStagnationDays":   reflect.TypeOf(int(0)),
		"SetMentalLoadThreshold":  reflect.TypeOf(int(0)),
		"SetConsolidationEnabled": reflect.TypeOf(false),
		// Not an Admin setter: the learn pass's own write (ADR-0055), shaped
		// like one, and outside the logged door, which is the Admin's.
		"LearnGoalStagnationDays": reflect.TypeOf(int(0)),
		// The summary's undo (ADR-0056), the Admin save's write.
		"PinGoalStagnationDays": reflect.TypeOf(int(0)),
	}
	// ReleaseGoalStagnationDays is the summary's release (ADR-0056).
	allowed := map[string]bool{"Load": true, "RecordConsolidationRun": true, "SetReminderPrefs": true, "ReleaseGoalStagnationDays": true}
	for name := range scalar {
		allowed[name] = true
	}
	ctxT := reflect.TypeOf((*context.Context)(nil)).Elem()
	timeT := reflect.TypeOf(time.Time{})
	errT := reflect.TypeOf((*error)(nil)).Elem()

	var problems []string
	have := map[string]bool{}
	for i := 0; i < it.NumMethod(); i++ {
		m := it.Method(i)
		have[m.Name] = true
		if !allowed[m.Name] {
			problems = append(problems, "method "+m.Name+" is outside the Q5 set")
			continue
		}
		want, isSetter := scalar[m.Name]
		if !isSetter {
			continue
		}
		ft := m.Type
		if ft.NumIn() != 3 || ft.In(0) != ctxT || ft.In(1) != want || ft.In(2) != timeT ||
			ft.NumOut() != 1 || ft.Out(0) != errT {
			problems = append(problems, fmt.Sprintf("%s is %s, want (context.Context, %s, time.Time) error", m.Name, ft, want))
		}
	}
	var missing []string
	for name := range allowed {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		problems = append(problems, "missing "+strings.Join(missing, ", "))
	}
	return problems
}
