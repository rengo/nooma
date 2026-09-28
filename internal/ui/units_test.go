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

// stubUnitsReader answers Browse with a fixed page and Detail with a fixed,
// independently configurable result — this package's own tests own
// /ui/units' and /ui/units/{id}'s rendering, not UnitsService's assembly
// (internal/brain's own tests cover that), the same split today_test.go's
// fixedToday already takes. detailFound defaults to false (unit_test.go's
// own NotFoundIs404 fixture is the zero value of this struct), so every
// existing Browse-only test in this file keeps its original Detail-unused
// behaviour unchanged. calledID, when non-nil, records the id Detail was
// last called with — unit_test.go's own TestUnitView_NotFoundIs404 needs
// this to prove serveUnit's own !found branch ran, not merely that
// ServeHTTP's unrelated default-404 arm did (both answer the same status
// code, so the status code alone cannot tell them apart).
type stubUnitsReader struct {
	page        ports.BrowsePage
	err         error
	detail      brain.UnitDetail
	detailFound bool
	detailErr   error
	calledID    *string
}

func (s stubUnitsReader) Browse(context.Context, []unit.Type, *ports.BrowseCursor) (ports.BrowsePage, error) {
	return s.page, s.err
}

func (s stubUnitsReader) Detail(_ context.Context, id string) (brain.UnitDetail, bool, error) {
	if s.calledID != nil {
		*s.calledID = id
	}
	return s.detail, s.detailFound, s.detailErr
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

// TestUnitsView_LastPageHasNoMoreLink is task 3.1's own last-page case:
// Browse answering with Next: nil renders no "More" link at all — the
// mutation that survives without this test is unitsRows' own "if next !=
// nil" collapsing to an unconditional render, which TestUnitsView_
// OnePageWithNextLink's Next-cursor fixture can never catch.
func TestUnitsView_LastPageHasNoMoreLink(t *testing.T) {
	t.Parallel()

	page := fixedBrowsePage()
	page.Next = nil

	h := ui.New(ui.Deps{Units: stubUnitsReader{page: page}})
	req := unitsRequest("")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units (last page) = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "hx-get") {
		t.Errorf("last page (Next: nil) still carries a paging hx-get attribute:\n%s", body)
	}
	if strings.Contains(body, "More") {
		t.Errorf("last page (Next: nil) still carries a \"More\" link:\n%s", body)
	}
}

// TestUnitsView_MoreLinkForwardsTypeFilter is task 3.1's own filter-paging
// case: the "more" link's own type-forwarding loop (moreURL) is what keeps
// a filtered browse from silently dropping its filter on the next page —
// deleting that loop still passes TestUnitsView_OnePageWithNextLink, which
// never requests a filtered page.
func TestUnitsView_MoreLinkForwardsTypeFilter(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{Units: stubUnitsReader{page: fixedBrowsePage()}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitsRequest("type=task&type=knowledge"))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units?type=task&type=knowledge = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"type=task", "type=knowledge",
		"after_created=2026-09-20T10%3A00%3A00Z", "after_id=unit-2",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("more link does not carry %q:\n%s", want, page)
		}
	}
}

// TestUnitsView_EscapesVaultContent is TestTodayView_EscapesVaultContent's
// own sibling for /ui/units (design §9's threat-matrix row on content
// injection): a unit's content is vault data, escaped by templ's default
// { expr } handling in both the browse list and the search results — the
// same unitsRows template renders both, so both call sites are pinned here
// rather than assuming one covers the other.
func TestUnitsView_EscapesVaultContent(t *testing.T) {
	t.Parallel()

	const payload = `<script>alert(1)</script>`
	const escaped = `&lt;script&gt;alert(1)&lt;/script&gt;`

	t.Run("browse", func(t *testing.T) {
		t.Parallel()

		page := fixedBrowsePage()
		page.Units[0].Content = payload

		h := ui.New(ui.Deps{Units: stubUnitsReader{page: page}})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, unitsRequest(""))

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/units = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if strings.Contains(body, payload) {
			t.Errorf("vault content reached the browse page unescaped:\n%s", body)
		}
		if !strings.Contains(body, escaped) {
			t.Errorf("vault content is not escaped as expected on the browse page:\n%s", body)
		}
	})

	t.Run("search", func(t *testing.T) {
		t.Parallel()

		search := stubSearcher{ok: true, units: []unit.Unit{
			{ID: "first-match", Content: payload},
		}}
		h := ui.New(ui.Deps{Search: search})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, unitsRequest("q=plants"))

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/units?q=plants = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if strings.Contains(body, payload) {
			t.Errorf("vault content reached the search results unescaped:\n%s", body)
		}
		if !strings.Contains(body, escaped) {
			t.Errorf("vault content is not escaped as expected in the search results:\n%s", body)
		}
	})
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

// TestUnitsView_RowsLinkToDetail is task 4.1: each browse row anchors to
// its own unit's detail path (PR 4's own overflow-cut candidate, kept in
// this PR — spec R3, design §5's units.templ row-anchor entry). A row that
// still renders its content as bare text, with no link to
// /ui/units/{id}, is exactly the regression this pins.
func TestUnitsView_RowsLinkToDetail(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{Units: stubUnitsReader{page: fixedBrowsePage()}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitsRequest(""))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		`<a href="/ui/units/unit-1">Call the dentist</a>`,
		`<a href="/ui/units/unit-2">Recipe for bread</a>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
}
