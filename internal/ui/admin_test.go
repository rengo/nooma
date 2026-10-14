package ui_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

func adminPost(t *testing.T, admin ui.Admin, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/ui/admin", strings.NewReader(body))
	req.Pattern = "POST /ui/admin"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Admin: admin}).ServeHTTP(rec, req)
	return rec
}

func TestAdminPost_SavesTheNamedFieldAndShowsTheNewValueInForce(t *testing.T) {
	t.Parallel()
	after := defaultAdminView()
	after.Settings.WeightThreshold = 0.6
	fake := &fakeAdmin{view: after, result: brain.UpdateResult{Field: brain.ConfigWeightThreshold, Changed: true}}

	rec := adminPost(t, fake, "field=weight_threshold&value=0.6", "HX-Request", "true", "HX-Boosted", "true")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(fake.updates) != 1 || fake.updates[0] != (adminUpdate{brain.ConfigWeightThreshold, "0.6"}) {
		t.Fatalf("Update calls = %v, want exactly one for weight_threshold with the raw text", fake.updates)
	}
	assertContains(t, rec.Body.String(), `data-outcome="saved"`, "Saved.", `name="value" value="0.6"`)
	if got := rec.Header().Get("HX-Replace-Url"); got != "/ui/admin" {
		t.Errorf("HX-Replace-Url = %q, want /ui/admin so a reload does not resubmit", got)
	}
}

func TestAdminPost_AValueAlreadyInForceSaysNothingChanged(t *testing.T) {
	t.Parallel()
	fake := &fakeAdmin{view: defaultAdminView(), result: brain.UpdateResult{Field: brain.ConfigGoalStagnationDays}}
	rec := adminPost(t, fake, "field=goal_stagnation_days&value=21")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertContains(t, rec.Body.String(), "Nothing changed")
	if strings.Contains(rec.Body.String(), "Saved.") {
		t.Error("a no-op is reported as saved")
	}
}

// C21: a name outside the five settings is answered before the brain is
// reached, so no mapping of Update's error can let one through.
func TestAdminPost_UnknownFieldNeverCallsUpdate(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"relation thresholds":       "field=relation_thresholds&value=0.5",
		"consolidation last run at": "field=consolidation_last_run_at&value=2026-01-01",
		"a bogus name":              "field=bogus&value=1",
		"an empty name":             "field=&value=1",
		"no name at all":            "value=1",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeAdmin{view: defaultAdminView()}
			rec := adminPost(t, fake, body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if len(fake.updates) != 0 {
				t.Errorf("Update was called %d time(s), want 0", len(fake.updates))
			}
		})
	}
}

func TestAdminPost_ARefusedValueIs400AndTheFormComesBackWithTheMessageAndTheText(t *testing.T) {
	t.Parallel()
	refused := &brain.ConfigRangeError{Field: brain.ConfigWeightThreshold, Raw: "abc<b>", Message: "enter a number from 0 to 2"}
	rec := adminPost(t, &fakeAdmin{view: defaultAdminView(), err: refused}, "field=weight_threshold&value=abc%3Cb%3E")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, `data-outcome="invalid"`, "Nothing was saved.", `data-error="weight_threshold"`, "enter a number from 0 to 2",
		`name="value" value="abc&lt;b&gt;"`)
	// The other settings still show what is in force.
	assertContains(t, body, `name="value" value="0.05"`)
}

func TestAdminPost_AFailureIs500WithoutItsCause(t *testing.T) {
	t.Parallel()
	rec := adminPost(t, &fakeAdmin{view: defaultAdminView(), err: errors.New("disk is on fire")}, "field=weight_threshold&value=0.6")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "disk is on fire") {
		t.Errorf("the cause reached the browser: %q", rec.Body.String())
	}
}

func TestAdminPost_RefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	fake := &fakeAdmin{view: defaultAdminView()}
	if rec := adminPost(t, nil, "field=weight_threshold&value=0.6"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil Admin: status = %d, want 503", rec.Code)
	}
	huge := "field=weight_threshold&value=" + strings.Repeat("9", 100<<10)
	if rec := adminPost(t, fake, huge); rec.Code != http.StatusBadRequest || len(fake.updates) != 0 {
		t.Errorf("a body over the limit: status = %d, calls = %d; want 400 and none", rec.Code, len(fake.updates))
	}
}

// C19. Not parallel: it swaps slog's default logger.
func TestAdminPost_WriteLandedRendersSuccessNoticeAndWarns(t *testing.T) {
	rec := &recordingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(prev) })

	landed := fmt.Errorf("admin: %w", &brain.WriteLandedError{Record: true, Err: errors.New("log: disk is on fire")})
	after := defaultAdminView()
	after.Settings.MentalLoadThreshold = 9
	resp := adminPost(t, &fakeAdmin{view: after, result: brain.UpdateResult{Field: brain.ConfigMentalLoadThreshold, Changed: true}, err: landed},
		"field=mental_load_threshold&value=9")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the value is stored", resp.Code)
	}
	body := resp.Body.String()
	assertContains(t, body, `data-outcome="saved-with-notice"`, "Saved, but the activity record of it could not be written.", `name="value" value="9"`)
	if strings.Contains(body, "disk is on fire") || strings.Contains(body, "internal error") {
		t.Errorf("the cause or a generic error reached the browser:\n%s", body)
	}
	if len(rec.records) != 1 || rec.records[0].Level != slog.LevelWarn {
		t.Fatalf("slog records = %v, want exactly one WARN", rec.records)
	}
	var cause string
	rec.records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "err" {
			cause = a.Value.String()
		}
		return true
	})
	if !strings.Contains(cause, "disk is on fire") {
		t.Errorf("the warning's err = %q, want the cause", cause)
	}
}

// A write that landed is still reported when the page cannot be re-read.
func TestAdminPost_ASavedWriteIsReportedEvenIfThePageCannotBeReRead(t *testing.T) {
	t.Parallel()
	fake := &fakeAdmin{viewErr: errors.New("disk is on fire"), result: brain.UpdateResult{Changed: true}}
	rec := adminPost(t, fake, "field=weight_threshold&value=0.6")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the value is stored", rec.Code)
	}
	assertContains(t, rec.Body.String(), `data-outcome="saved"`)
	if strings.Contains(rec.Body.String(), "disk is on fire") {
		t.Error("the cause reached the browser")
	}
}
