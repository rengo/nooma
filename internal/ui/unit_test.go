package ui_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ui"
)

// unitRequest builds a GET /ui/units/{id} request with req.Pattern and the
// {id} path value set as net/http's own ServeMux would set them when this
// handler is reached through the real mux — unitsRequest's own precedent
// (units_test.go) for driving ui.Handler directly, extended with
// SetPathValue since this pattern carries a wildcard the browse route does
// not.
func unitRequest(id string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/ui/units/"+id, nil)
	req.Pattern = "GET /ui/units/{id}"
	req.SetPathValue("id", id)
	return req
}

// TestUnitView_I18ThreeDatesNeverSwap is task 4.1: Created:, Event: and
// Due: each keep their own label, and a nil EventAt/DueAt renders the
// literal "none" — never the zero time, and never substituted with
// CreatedAt or the other date field's own value (I18's own UI failure
// mode, TestTodayView_I18DatesLabelled's precedent applied to one unit's
// three distinct date fields instead of two focus members).
func TestUnitView_I18ThreeDatesNeverSwap(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	due := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)

	detail := brain.UnitDetail{
		Unit: unit.Unit{
			ID:        "unit-1",
			Type:      unit.TypeTask,
			Content:   "Pay the rent",
			Weight:    0.5,
			CreatedAt: created,
			DueAt:     &due,
			EventAt:   nil,
		},
	}
	h := ui.New(ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("unit-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units/unit-1 = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()

	// templ's generated output is one unbroken line — today_test.go's own
	// TestTodayView_I18DatesLabelled precedent slices by label position
	// instead of by "\n", and this test does the same: each <li> is sliced
	// from its own label up to the next one.
	iCreated, iEvent, iDue := strings.Index(page, "Created:"), strings.Index(page, "Event:"), strings.Index(page, "Due:")
	if iCreated < 0 || iEvent < 0 || iDue < 0 {
		t.Fatalf("page is missing one of the three date labels:\n%s", page)
	}
	createdLine := page[iCreated:iEvent]
	eventLine := page[iEvent:iDue]
	dueLine := page[iDue:]

	if !strings.Contains(createdLine, "2026-09-01") || strings.Contains(createdLine, "Due:") || strings.Contains(createdLine, "Event:") {
		t.Errorf("Created: line wrong or carries another date's label:\n%s", createdLine)
	}
	if !strings.Contains(dueLine, "2026-09-22") || strings.Contains(dueLine, "Created:") || strings.Contains(dueLine, "Event:") {
		t.Errorf("Due: line wrong or carries another date's label:\n%s", dueLine)
	}
	if !strings.Contains(eventLine, "none") {
		t.Errorf("nil EventAt did not render the literal \"none\":\n%s", eventLine)
	}
	if strings.Contains(eventLine, "2026-09-01") || strings.Contains(eventLine, "2026-09-22") {
		t.Errorf("Event: line carries a date value from another field — a swap, not an explicit absence:\n%s", eventLine)
	}
	if strings.Contains(eventLine, "0001-01-01") {
		t.Errorf("nil EventAt rendered the zero time instead of the literal \"none\":\n%s", eventLine)
	}
}

// TestUnitView_RendersUnitIdentityAndType is spec R3: the page carries the
// unit's own id as a stable hook — units.templ's own row anchor and
// TestUnitsView_OnePageWithNextLink's precedent for data-unit-id, applied
// here to the detail page's single unit — and its Type, rendered plainly
// in a <span>, the same shape a browse row's own type span takes.
func TestUnitView_RendersUnitIdentityAndType(t *testing.T) {
	t.Parallel()

	detail := brain.UnitDetail{
		Unit: unit.Unit{ID: "unit-7", Type: unit.TypeKnowledge, Content: "Recipe for bread", CreatedAt: time.Now()},
	}
	h := ui.New(ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("unit-7"))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units/unit-7 = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{`data-unit-id="unit-7"`, "<span>knowledge</span>"} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
}

// TestUnitView_StoredWeightLabelled is spec R3: the unit's Weight renders
// labelled as "stored weight" specifically — never "effective weight" or a
// bare "Weight:", either of which would misrepresent a value this package
// never decays or recomputes (design §3.3 OR3).
func TestUnitView_StoredWeightLabelled(t *testing.T) {
	t.Parallel()

	detail := brain.UnitDetail{
		Unit: unit.Unit{ID: "unit-1", Type: unit.TypeTask, Content: "Pay the rent", Weight: 0.5, CreatedAt: time.Now()},
	}
	h := ui.New(ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("unit-1"))

	page := rec.Body.String()
	if !strings.Contains(page, "Stored weight: 0.50") {
		t.Errorf("page does not label the weight as \"Stored weight\":\n%s", page)
	}
	if strings.Contains(page, "Effective weight") {
		t.Errorf("page claims an effective weight this package never computes:\n%s", page)
	}
}

// TestUnitView_NotFoundIs404 is task 4.1: UnitsService.Detail's found=false
// — archived, superseded, incomplete or an absent id all resolve to it
// alike (I02) — answers the same 404 class GET /units/{id} already gives,
// not a panic on a zero-value UnitDetail. The status code alone cannot
// distinguish serveUnit's own !found branch from ServeHTTP's unrelated
// default-404 arm (a routing bug that never reaches serveUnit at all would
// answer 404 too), so this also asserts Detail was actually called with
// the request's own id — proof the request reached serveUnit and took the
// !found branch, not the default arm.
func TestUnitView_NotFoundIs404(t *testing.T) {
	t.Parallel()

	var calledID string
	h := ui.New(ui.Deps{Units: stubUnitsReader{detailFound: false, calledID: &calledID}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("archived-unit"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /ui/units/archived-unit (found=false) = %d, want 404", rec.Code)
	}
	if calledID != "archived-unit" {
		t.Errorf("Detail was not called with the request's own id (got %q) — the 404 came from somewhere other than serveUnit's own !found branch", calledID)
	}
}

// TestUnitView_DetailErrorIs500 is serveUnit's own error posture — a
// UnitsService.Detail error (a repo failure, not a not-found) answers 500,
// never the same 404 found=false answers, and never reflects the raw
// error into the response body (serveToday's own precedent).
func TestUnitView_DetailErrorIs500(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{Units: stubUnitsReader{detailErr: errors.New("boom")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("unit-1"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("GET /ui/units/unit-1 (Detail error) = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("response reflects the raw error:\n%s", rec.Body.String())
	}
}

// TestUnitView_RendersLiveRelations is task 4.1: every relation
// UnitsService.Detail returns (already filtered to live neighbours by the
// brain layer, PR 2's own TestUnitsService_DetailDropsNonLiveNeighbours)
// appears on the page, by its own relation id and its other endpoint's
// content — this package's job is rendering completeness, not re-deriving
// the live filter UnitsService.Detail already applied. It also asserts
// each relation's own direction (Outgoing renders "outgoing"/"→", the
// reverse renders "incoming"/"←" — an owner decision beyond tasks.md's own
// letter: a relation's direction is part of what it means, not decoration)
// and Confidence formatted to two decimals, the same precision
// formatWeight/formatScore already use elsewhere on this page.
func TestUnitView_RendersLiveRelations(t *testing.T) {
	t.Parallel()

	rel1 := brain.RelatedUnit{
		RelationID: "rel-1",
		Type:       "relates_to",
		Outgoing:   true,
		Confidence: 0.8,
		Other:      unit.Unit{ID: "unit-2", Content: "Renew the passport"},
	}
	rel2 := brain.RelatedUnit{
		RelationID: "rel-2",
		Type:       "blocks",
		Outgoing:   false,
		Confidence: 0.6,
		Other:      unit.Unit{ID: "unit-3", Content: "Call the dentist"},
	}

	// Both orders are rendered: with one order only, direction and
	// confidence correlate with the loop index, so a template that took
	// them from the position (i%2) instead of the relation would pass.
	for _, order := range [][]brain.RelatedUnit{{rel1, rel2}, {rel2, rel1}} {
		detail := brain.UnitDetail{
			Unit:      unit.Unit{ID: "unit-1", Type: unit.TypeTask, Content: "Pay the rent", CreatedAt: time.Now()},
			Relations: order,
		}
		h := ui.New(ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, unitRequest("unit-1"))

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/units/unit-1 = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		page := rec.Body.String()

		// Direction and confidence are per-relation, so a flat
		// strings.Contains over the whole page cannot tell "rel-1 rendered
		// outgoing" from "rel-2 rendered outgoing, rel-1 rendered incoming" —
		// both produce the same set of substrings somewhere on the page.
		// Slicing by each relation's own data-relation-id, today_test.go's
		// and this file's own I18ThreeDatesNeverSwap precedent for
		// position-based slicing over templ's unbroken-line output, scopes
		// each assertion to the relation it must belong to.
		iFirst := strings.Index(page, `data-relation-id="`+order[0].RelationID+`"`)
		iSecond := strings.Index(page, `data-relation-id="`+order[1].RelationID+`"`)
		if iFirst < 0 || iSecond < 0 || iSecond < iFirst {
			t.Fatalf("page is missing %s and/or %s in the expected order:\n%s", order[0].RelationID, order[1].RelationID, page)
		}
		blocks := map[string]string{
			order[0].RelationID: page[iFirst:iSecond],
			order[1].RelationID: page[iSecond:],
		}

		for id, c := range map[string]struct{ want, unwanted []string }{
			"rel-1": {
				want:     []string{"relates_to", `<a href="/ui/units/unit-2">Renew the passport</a>`, `aria-label="outgoing"`, "→", "0.80"},
				unwanted: []string{`aria-label="incoming"`, "←", "0.60"},
			},
			"rel-2": {
				want:     []string{"blocks", `<a href="/ui/units/unit-3">Call the dentist</a>`, `aria-label="incoming"`, "←", "0.60"},
				unwanted: []string{`aria-label="outgoing"`, "→", "0.80"},
			},
		} {
			for _, w := range c.want {
				if !strings.Contains(blocks[id], w) {
					t.Errorf("order %s,%s: %s's own block does not contain %q:\n%s", order[0].RelationID, order[1].RelationID, id, w, blocks[id])
				}
			}
			for _, u := range c.unwanted {
				if strings.Contains(blocks[id], u) {
					t.Errorf("order %s,%s: %s's own block wrongly contains %q — the other relation's direction or confidence:\n%s", order[0].RelationID, order[1].RelationID, id, u, blocks[id])
				}
			}
		}
	}
}

// TestUnitView_NilUnitsIs503 is serveUnit's own nil-dependency posture,
// serveUnits' own precedent (TestUnitsView_NilUnitsIs503) applied to the
// detail route: wireUnits is wired unconditionally in production, but a
// nil Deps.Units must not panic on a nil interface call.
func TestUnitView_NilUnitsIs503(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("unit-1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /ui/units/unit-1 with no UnitsReader = %d, want 503", rec.Code)
	}
}

// TestUnitView_EscapesVaultContent is TestUnitsView_EscapesVaultContent's
// own sibling for the detail page (design §9's threat-matrix row on
// content injection): both the unit's own content and a related unit's
// content are vault data, escaped by templ's default { expr } handling —
// two separate call sites in unit.templ, both pinned here rather than
// assuming one covers the other.
func TestUnitView_EscapesVaultContent(t *testing.T) {
	t.Parallel()

	const payload = `<script>alert(1)</script>`
	const escaped = `&lt;script&gt;alert(1)&lt;/script&gt;`

	detail := brain.UnitDetail{
		Unit: unit.Unit{ID: "unit-1", Type: unit.TypeTask, Content: payload, CreatedAt: time.Now()},
		Relations: []brain.RelatedUnit{
			{RelationID: "rel-1", Type: "relates_to", Other: unit.Unit{ID: "unit-2", Content: payload}},
		},
	}
	h := ui.New(ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("unit-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units/unit-1 = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, payload) {
		t.Errorf("vault content reached the detail page unescaped:\n%s", body)
	}
	if strings.Count(body, escaped) != 2 {
		t.Errorf("expected both the unit's own content and its relation's content escaped exactly once each (2 total), got %d:\n%s", strings.Count(body, escaped), body)
	}
}
