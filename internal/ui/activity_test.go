package ui_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
)

const activityGetPattern = "GET /ui/activity"

// fakeActivity answers a fixed page (or error) and records each Page call.
type fakeActivity struct {
	page  brain.ActivityPage
	err   error
	kinds []string
	befor []*ports.DecisionCursor
}

func (f *fakeActivity) Page(_ context.Context, kind string, before *ports.DecisionCursor) (brain.ActivityPage, error) {
	f.kinds = append(f.kinds, kind)
	f.befor = append(f.befor, before)
	return f.page, f.err
}

func activityGet(rawQuery string) *http.Request {
	target := "/ui/activity"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Pattern = activityGetPattern
	return req
}

func serveActivity(deps ui.Deps, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	ui.New(deps).ServeHTTP(rec, req)
	return rec
}

var activityAt = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func activityRow(id string, action ports.DecisionAction, seq int64, change ...brain.ChangedField) brain.ActivityRow {
	return brain.ActivityRow{
		DecisionRow: ports.DecisionRow{
			Decision: ports.Decision{ID: id, Action: action, Rationale: "because " + id, OccurredAt: activityAt},
			Seq:      seq,
		},
		Change: change,
	}
}

func TestActivityView_RendersRowsInTheOrderBrainGaveThem(t *testing.T) {
	t.Parallel()
	fake := &fakeActivity{page: brain.ActivityPage{Rows: []brain.ActivityRow{
		activityRow("row-b", ports.ActionCaptureUnitCreated, 9),
		activityRow("row-a", ports.ActionCheckTimerFired, 8),
	}}}

	rec := serveActivity(ui.Deps{Activity: fake}, activityGet(""))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/activity = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"<h2>ACTIVITY</h2>", "capture.unit.created", "because row-b", "2026-09-01T12:00:00Z"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
	if b, a := strings.Index(body, `data-decision-id="row-b"`), strings.Index(body, `data-decision-id="row-a"`); b < 0 || a < 0 || b > a {
		t.Errorf("rows are not in brain's order (row-b at %d, row-a at %d)", b, a)
	}
	if len(fake.kinds) != 1 || fake.kinds[0] != "" || fake.befor[0] != nil {
		t.Errorf("Page called with kinds=%v before=%v, want one call with no kind and no cursor", fake.kinds, fake.befor)
	}
}

func TestActivityView_CorrectionShowsPreviousBesideNext(t *testing.T) {
	t.Parallel()
	fake := &fakeActivity{page: brain.ActivityPage{Rows: []brain.ActivityRow{
		activityRow("c-1", ports.ActionCorrectionApplied, 3,
			brain.ChangedField{Name: "event_at", Previous: "2026-09-01T10:00:00Z", Next: "2026-09-02T10:00:00Z"}),
		activityRow("cfg-1", ports.ActionBeliefEdited, 2,
			brain.ChangedField{Name: "goal_stagnation_days", Previous: "21", Next: "28"},
			brain.ChangedField{Name: "consolidation_enabled", Previous: "true", Next: "false"},
			brain.ChangedField{Name: "weight_threshold", Previous: "0.5", Next: "0.6"}),
		activityRow("plain-1", ports.ActionCaptureUnitCreated, 1),
	}}}

	body := serveActivity(ui.Deps{Activity: fake}, activityGet("")).Body.String()

	for _, want := range []string{
		"event_at: 2026-09-01 10:00 → 2026-09-02 10:00",
		"goal_stagnation_days: 21 → 28",
		"consolidation_enabled: true → false",
		"weight_threshold: 0.5 → 0.6",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not show %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "21.0") || strings.Contains(body, "28.0") {
		t.Errorf("a number renders with a trailing .0:\n%s", body)
	}
	plain := between(t, body, `data-decision-id="plain-1"`, "</li>")
	if strings.Contains(plain, "data-change") || strings.Contains(plain, "→") {
		t.Errorf("a row with no change renders a change block:\n%s", plain)
	}
}

func TestActivityView_OlderLinkOnlyWithANextCursor(t *testing.T) {
	t.Parallel()
	rows := []brain.ActivityRow{activityRow("r-1", ports.ActionCheckTimerFired, 7)}

	last := serveActivity(ui.Deps{Activity: &fakeActivity{page: brain.ActivityPage{Rows: rows}}}, activityGet("kind=check")).Body.String()
	if strings.Contains(last, "Older") {
		t.Errorf("the last page offers an older link:\n%s", last)
	}

	next := &ports.DecisionCursor{OccurredAt: activityAt, Seq: 7}
	body := serveActivity(ui.Deps{Activity: &fakeActivity{page: brain.ActivityPage{Rows: rows, Next: next}}}, activityGet("kind=check")).Body.String()
	m := regexp.MustCompile(`<a href="([^"]*)"[^>]*>Older</a>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no older link on a page with a next cursor:\n%s", body)
	}
	u, err := url.Parse(strings.ReplaceAll(m[1], "&amp;", "&"))
	if err != nil {
		t.Fatalf("older href %q: %v", m[1], err)
	}
	q := u.Query()
	if u.Path != "/ui/activity" || q.Get("before_at") != "2026-09-01T12:00:00Z" || q.Get("before_seq") != "7" || q.Get("kind") != "check" {
		t.Errorf("older href = %q, want /ui/activity with before_at, before_seq=7 and the kind filter kept", m[1])
	}
}

func TestActivityView_PassesKindAndCursorToBrain(t *testing.T) {
	t.Parallel()
	fake := &fakeActivity{}

	rec := serveActivity(ui.Deps{Activity: fake}, activityGet("kind=relation&before_at=2026-09-01T12%3A00%3A00Z&before_seq=42"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	want := ports.DecisionCursor{OccurredAt: activityAt, Seq: 42}
	if len(fake.kinds) != 1 || fake.kinds[0] != "relation" || fake.befor[0] == nil || *fake.befor[0] != want {
		t.Errorf("Page(kind=%v, before=%v), want relation and %+v", fake.kinds, fake.befor, want)
	}
}

func TestActivityView_BadRequestsAre400AndReadNothing(t *testing.T) {
	t.Parallel()
	for name, q := range map[string]string{
		"half cursor, at only":  "before_at=2026-09-01T12%3A00%3A00Z",
		"half cursor, seq only": "before_seq=4",
		"bad timestamp":         "before_at=yesterday&before_seq=4",
		"bad seq":               "before_at=2026-09-01T12%3A00%3A00Z&before_seq=four",
		"zero seq":              "before_at=2026-09-01T12%3A00%3A00Z&before_seq=0",
		"negative seq":          "before_at=2026-09-01T12%3A00%3A00Z&before_seq=-3",
		"empty cursor values":   "before_at=&before_seq=",
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeActivity{}
			rec := serveActivity(ui.Deps{Activity: fake}, activityGet(q))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if len(fake.kinds) != 0 {
				t.Errorf("Page was called %d time(s) for a bad cursor, want 0", len(fake.kinds))
			}
		})
	}

	t.Run("unknown kind", func(t *testing.T) {
		fake := &fakeActivity{err: brain.ErrUnknownActivityKind}
		rec := serveActivity(ui.Deps{Activity: fake}, activityGet("kind=bogus"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

func TestActivityView_KindOptionsAreTheVocabularyFamilies(t *testing.T) {
	t.Parallel()
	body := serveActivity(ui.Deps{Activity: &fakeActivity{}}, activityGet("kind=check")).Body.String()

	sel := between(t, body, `<select id="kind" name="kind">`, "</select>")
	opts := regexp.MustCompile(`<option value="([^"]*)"( selected)?>`).FindAllStringSubmatch(sel, -1)
	var got []string
	selected := ""
	for _, o := range opts {
		got = append(got, o[1])
		if o[2] != "" {
			selected = o[1]
		}
	}
	want := append([]string{""}, brain.ActivityFamilies(ports.AllDecisionActions())...)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("options = %q, want all-plus-families %q", got, want)
	}
	if selected != "check" {
		t.Errorf("selected option = %q, want the current kind check", selected)
	}
}

// G8: the page holds no form that posts and no hx-post — pre-images are
// shown, never offered back. The GET filter form is the only form.
func TestActivityView_HasNoMutatingForm(t *testing.T) {
	t.Parallel()
	fake := &fakeActivity{page: brain.ActivityPage{
		Rows: []brain.ActivityRow{activityRow("c-1", ports.ActionCorrectionApplied, 3,
			brain.ChangedField{Name: "event_at", Previous: "a", Next: "b"})},
		Next: &ports.DecisionCursor{OccurredAt: activityAt, Seq: 3},
	}}

	body := strings.ToLower(serveActivity(ui.Deps{Activity: fake}, activityGet("")).Body.String())

	for _, banned := range []string{`method="post"`, "method=post", "hx-post", "hx-put", "hx-delete", "<button"} {
		if banned == "<button" {
			// The filter form's submit is the only button.
			if n := strings.Count(body, banned); n != 1 {
				t.Errorf("page has %d buttons, want exactly the filter's submit", n)
			}
			continue
		}
		if strings.Contains(body, banned) {
			t.Errorf("page contains %q — activity is read-only", banned)
		}
	}
	if !strings.Contains(body, `<form method="get" action="/ui/activity">`) {
		t.Errorf("the filter form is not a plain GET form:\n%s", body)
	}
}

func TestActivityView_NavLinksToActivity(t *testing.T) {
	t.Parallel()
	body := serveActivity(ui.Deps{Activity: &fakeActivity{}}, activityGet("")).Body.String()
	if !strings.Contains(body, `<a href="/ui/activity" aria-current="page">Activity</a>`) {
		t.Errorf("the nav has no Activity link:\n%s", body)
	}
}

func TestActivityView_NilActivityAnswers503(t *testing.T) {
	t.Parallel()
	if rec := serveActivity(ui.Deps{}, activityGet("")); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestActivityView_BrainFailureAnswers500WithoutTheDetail(t *testing.T) {
	t.Parallel()
	rec := serveActivity(ui.Deps{Activity: &fakeActivity{err: errors.New("disk /secret/path failed")}}, activityGet(""))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "/secret/path") {
		t.Errorf("the 500 reflects the internal error: %s", rec.Body.String())
	}
}
