package ui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
)

// stubUnitsReader answers Browse with a fixed page and Detail with a fixed
// (unused, in this file) result — this package's own tests own /ui/units'
// rendering, not UnitsService's assembly (internal/brain's own tests cover
// that), the same split today_test.go's fixedToday already takes.
type stubUnitsReader struct {
	page ports.BrowsePage
	err  error
}

func (s stubUnitsReader) Browse(context.Context, []unit.Type, *ports.BrowseCursor) (ports.BrowsePage, error) {
	return s.page, s.err
}

func (stubUnitsReader) Detail(context.Context, string) (brain.UnitDetail, bool, error) {
	return brain.UnitDetail{}, false, nil
}

// stubSearcher answers ForText with a fixed, already-ordered result —
// RecallService's own ordering assembly is I22's proof
// (i22_browse_search_test.go), not this file's job.
type stubSearcher struct {
	units []unit.Unit
	ok    bool
	err   error
}

func (s stubSearcher) ForText(context.Context, string) ([]unit.Unit, bool, error) {
	return s.units, s.ok, s.err
}

// unitsRequest builds a GET /ui/units request with req.Pattern set as
// net/http's own ServeMux would set it when this handler is reached
// through the real mux — today_test.go's own precedent (task 3.3) for
// driving ui.Handler directly, bypassing httptest.NewServer.
func unitsRequest(rawQuery string) *http.Request {
	target := "/ui/units"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Pattern = "GET /ui/units"
	return req
}

func fixedBrowsePage() ports.BrowsePage {
	created := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	return ports.BrowsePage{
		Units: []unit.Unit{
			{ID: "unit-1", Type: unit.TypeTask, Content: "Call the dentist", CreatedAt: created},
			{ID: "unit-2", Type: unit.TypeKnowledge, Content: "Recipe for bread", CreatedAt: created},
		},
		Next: &ports.BrowseCursor{CreatedAt: created, ID: "unit-2"},
	}
}

// TestUnitsView_OnePageWithNextLink is task 3.1: a page with a Next cursor
// renders every unit's id and a "more" hx-get link carrying that cursor's
// own after_created/after_id.
func TestUnitsView_OnePageWithNextLink(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{Units: stubUnitsReader{page: fixedBrowsePage()}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitsRequest(""))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		`data-unit-id="unit-1"`, "Call the dentist", "<span>task</span>",
		`data-unit-id="unit-2"`, "Recipe for bread", "<span>knowledge</span>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
	if i1, i2 := strings.Index(page, "unit-1"), strings.Index(page, "unit-2"); i1 < 0 || i2 < 0 || i1 > i2 {
		t.Errorf("unit-1 (%d) does not precede unit-2 (%d) — Browse's own order was not preserved", i1, i2)
	}
	for _, want := range []string{"hx-get=", "after_created=2026-09-20T10%3A00%3A00Z", "after_id=unit-2"} {
		if !strings.Contains(page, want) {
			t.Errorf("page's \"more\" link does not carry %q:\n%s", want, page)
		}
	}
}

// TestUnitsView_FragmentOnHXRequest is task 3.1: an HX-Request carries the
// rows fragment alone — no <nav>, the full page's own layout marker — while
// still carrying the same rows a full page would.
func TestUnitsView_FragmentOnHXRequest(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{Units: stubUnitsReader{page: fixedBrowsePage()}})
	req := unitsRequest("")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units (HX-Request) = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	if strings.Contains(page, "<nav") {
		t.Error("HX-Request response carries <nav> — this must be the rows fragment alone, not the full page")
	}
	if !strings.Contains(page, `data-unit-id="unit-1"`) {
		t.Errorf("HX-Request response does not carry the rows:\n%s", page)
	}
}

// TestUnitsView_UnknownTypeIs400 is task 3.1: unit.ParseType's own rejection
// reaches the caller as 400, naming the query's own bad value.
func TestUnitsView_UnknownTypeIs400(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{Units: stubUnitsReader{page: fixedBrowsePage()}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitsRequest("type=bogus"))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("GET /ui/units?type=bogus = %d, want 400", rec.Code)
	}
}

// TestUnitsView_HalfCursorIs400 is task 3.1: after_created and after_id are
// both-or-neither — one present without the other is 400, in either
// direction.
func TestUnitsView_HalfCursorIs400(t *testing.T) {
	t.Parallel()

	for _, rawQuery := range []string{
		"after_created=2026-09-20T10:00:00Z",
		"after_id=unit-2",
	} {
		rawQuery := rawQuery
		t.Run(rawQuery, func(t *testing.T) {
			t.Parallel()
			h := ui.New(ui.Deps{Units: stubUnitsReader{page: fixedBrowsePage()}})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, unitsRequest(rawQuery))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("GET /ui/units?%s = %d, want 400", rawQuery, rec.Code)
			}
		})
	}
}

// TestUnitsView_MalformedCursorIs400 is task 3.1: a non-RFC3339
// after_created, or either key present with an empty value, is 400 — not a
// silently-ignored cursor.
func TestUnitsView_MalformedCursorIs400(t *testing.T) {
	t.Parallel()

	for _, rawQuery := range []string{
		"after_created=not-a-date&after_id=unit-2",
		"after_created=&after_id=unit-2",
		"after_created=2026-09-20T10:00:00Z&after_id=",
	} {
		rawQuery := rawQuery
		t.Run(rawQuery, func(t *testing.T) {
			t.Parallel()
			h := ui.New(ui.Deps{Units: stubUnitsReader{page: fixedBrowsePage()}})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, unitsRequest(rawQuery))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("GET /ui/units?%s = %d, want 400", rawQuery, rec.Code)
			}
		})
	}
}

// TestUnitsView_SearchRendersRecallOrder is task 3.1: a q parameter
// dispatches to Searcher.ForText, rendered in the order it returned —
// never re-sorted — and never reaches UnitsReader.Browse at all (Units is
// deliberately left nil here; a call into it would panic, which this test
// would report as a failure, not a false pass).
func TestUnitsView_SearchRendersRecallOrder(t *testing.T) {
	t.Parallel()

	search := stubSearcher{ok: true, units: []unit.Unit{
		{ID: "second-match", Content: "Water the plants on Tuesday"},
		{ID: "first-match", Content: "Buy more plant food"},
	}}
	h := ui.New(ui.Deps{Search: search})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitsRequest("q=plants"))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units?q=plants = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{`data-unit-id="second-match"`, `data-unit-id="first-match"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
	if i1, i2 := strings.Index(page, "second-match"), strings.Index(page, "first-match"); i1 < 0 || i2 < 0 || i1 > i2 {
		t.Errorf("search order was not preserved: second-match (%d), first-match (%d)", i1, i2)
	}
}

// TestUnitsView_NilSearchIs503 is task 3.1: a q parameter with no Searcher
// wired answers 503, captureHandler's own nil-dependency posture
// (internal/httpapi/capture.go), never a panic on a nil interface call.
func TestUnitsView_NilSearchIs503(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitsRequest("q=plants"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /ui/units?q=plants with no Searcher = %d, want 503", rec.Code)
	}
}

// TestUnitsView_NilUnitsIs503 is browse's own nil-dependency posture,
// TodayReader's precedent (TestTodayView_NilTodayReaderAnswers503) applied
// to Units: wireUnits is wired unconditionally in production, but a test
// fixture or a future refactor that leaves Deps.Units nil must not panic.
func TestUnitsView_NilUnitsIs503(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitsRequest(""))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /ui/units with no UnitsReader = %d, want 503", rec.Code)
	}
}
