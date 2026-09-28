package ui_test

import (
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

	lines := strings.Split(page, "\n")
	var createdLine, eventLine, dueLine string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "Created:"):
			createdLine = l
		case strings.Contains(l, "Event:"):
			eventLine = l
		case strings.Contains(l, "Due:"):
			dueLine = l
		}
	}

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
// not a panic on a zero-value UnitDetail.
func TestUnitView_NotFoundIs404(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{Units: stubUnitsReader{detailFound: false}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("archived-unit"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /ui/units/archived-unit (found=false) = %d, want 404", rec.Code)
	}
}

// TestUnitView_RendersLiveRelations is task 4.1: every relation
// UnitsService.Detail returns (already filtered to live neighbours by the
// brain layer, PR 2's own TestUnitsService_DetailDropsNonLiveNeighbours)
// appears on the page, by its own relation id and its other endpoint's
// content — this package's job is rendering completeness, not re-deriving
// the live filter UnitsService.Detail already applied.
func TestUnitView_RendersLiveRelations(t *testing.T) {
	t.Parallel()

	detail := brain.UnitDetail{
		Unit: unit.Unit{ID: "unit-1", Type: unit.TypeTask, Content: "Pay the rent", CreatedAt: time.Now()},
		Relations: []brain.RelatedUnit{
			{
				RelationID: "rel-1",
				Type:       "relates_to",
				Outgoing:   true,
				Confidence: 0.8,
				Other:      unit.Unit{ID: "unit-2", Content: "Renew the passport"},
			},
			{
				RelationID: "rel-2",
				Type:       "blocks",
				Outgoing:   false,
				Confidence: 0.6,
				Other:      unit.Unit{ID: "unit-3", Content: "Call the dentist"},
			},
		},
	}
	h := ui.New(ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("unit-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/units/unit-1 = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		`data-relation-id="rel-1"`, "relates_to", "Renew the passport",
		`data-relation-id="rel-2"`, "blocks", "Call the dentist",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
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
