package ui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
)

// fakeAdmin is ui.Admin over a fixed view: Update records what it was given
// and answers result and err.
type fakeAdmin struct {
	view    brain.AdminView
	viewErr error
	result  brain.UpdateResult
	err     error
	updates []adminUpdate
}

type adminUpdate struct {
	field brain.ConfigField
	raw   string
}

func (f *fakeAdmin) View(context.Context) (brain.AdminView, error) { return f.view, f.viewErr }

func (f *fakeAdmin) Update(_ context.Context, field brain.ConfigField, raw string) (brain.UpdateResult, error) {
	f.updates = append(f.updates, adminUpdate{field, raw})
	return f.result, f.err
}

// defaultAdminView is a vault that never set anything: every default in
// force, no run yet, nothing learned.
func defaultAdminView() brain.AdminView {
	return brain.AdminView{
		Settings:   brain.AdminSettings{WeightThreshold: 0.5, HysteresisMargin: 0.05, GoalStagnationDays: 21, MentalLoadThreshold: 7, ConsolidationEnabled: true},
		OtherTypes: relation.Thresholds{Persist: 0.6, Surface: 0.8},
	}
}

func adminGet(t *testing.T, admin ui.Admin) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/ui/admin", nil)
	req.Pattern = "GET /ui/admin"
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Admin: admin}).ServeHTTP(rec, req)
	return rec
}

func TestAdminPage_ShowsTheFiveSettingsInForceAndNothingElseEditable(t *testing.T) {
	t.Parallel()
	rec := adminGet(t, &fakeAdmin{view: defaultAdminView()})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body,
		`<a href="/ui/admin" aria-current="page">Admin</a>`,
		`<h2>ADMIN</h2>`,
		`name="field" value="weight_threshold"`, `name="value" value="0.5"`,
		`name="field" value="hysteresis_margin"`, `name="value" value="0.05"`,
		`name="field" value="goal_stagnation_days"`, `name="value" value="21"`,
		`name="field" value="mental_load_threshold"`, `name="value" value="7"`,
		`name="field" value="consolidation_enabled"`, `<option value="true" selected>`,
	)
	if n := strings.Count(body, `name="field"`); n != 5 {
		t.Errorf("%d editable settings, want exactly 5", n)
	}
	for _, never := range []string{"relation_thresholds", "consolidation_last_run_at"} {
		if strings.Contains(body, `value="`+never+`"`) {
			t.Errorf("the page offers %q as an editable field", never)
		}
	}
}

func TestAdminPage_ShowsJobStatusLabelledForWhatItIs(t *testing.T) {
	t.Parallel()
	view := defaultAdminView()
	rec := adminGet(t, &fakeAdmin{view: view})
	assertContains(t, rec.Body.String(),
		"Last full consolidation", "never",
		"Recent effects of the nightly pass", "Nothing recorded yet",
	)

	ran := time.Date(2026, 9, 1, 3, 15, 0, 0, time.UTC)
	view.LastRun = &ran
	view.Settings.ConsolidationEnabled = false
	view.RecentEffects = []ports.DecisionRow{
		{Decision: ports.Decision{ID: "d-2", Action: ports.ActionArchiveArchived, Rationale: "went cold: second", OccurredAt: ran}},
		{Decision: ports.Decision{ID: "d-1", Action: ports.ActionArchiveArchived, Rationale: "went cold: first", OccurredAt: ran.Add(-time.Hour)}},
	}
	body := adminGet(t, &fakeAdmin{view: view}).Body.String()
	assertContains(t, body, "2026-09-01 03:15", `<option value="false" selected>`, "went cold: second", "went cold: first")
	if strings.Index(body, "went cold: second") > strings.Index(body, "went cold: first") {
		t.Error("effects are not in the order the view gave them (newest first)")
	}
	if strings.Contains(body, "never") {
		t.Error("a consolidation that ran is still shown as never")
	}
}

func TestAdminPage_ShowsLearnedThresholdsReadOnlyWithTheLearnedNote(t *testing.T) {
	t.Parallel()
	view := defaultAdminView()
	view.Learned = []ports.LearnedThreshold{{RelationType: "supports", Thresholds: relation.Thresholds{Persist: 0.55, Surface: 0.75}}}
	body := adminGet(t, &fakeAdmin{view: view}).Body.String()
	assertContains(t, body,
		"learned by the nightly learning pass", "not editable here",
		"supports", "0.55", "0.75",
		"every other type", "0.6", "0.8",
	)
	if n := strings.Count(body, `name="field"`); n != 5 {
		t.Errorf("%d fields, want 5: a learned threshold must have no input", n)
	}
}

func TestAdminPage_AnswersPlainlyWhenItCannotRender(t *testing.T) {
	t.Parallel()
	if rec := adminGet(t, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil Admin: status = %d, want 503", rec.Code)
	}
	rec := adminGet(t, &fakeAdmin{viewErr: context.DeadlineExceeded})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failing View: status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "deadline") {
		t.Errorf("the error text reached the browser: %q", rec.Body.String())
	}
}
