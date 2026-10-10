package ui_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/correction"
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
	// instead of by "\n", and this test does the same: each <dt>/<dd> pair is
	// sliced from its own label up to the next one, the last up to </dl>.
	iCreated, iEvent, iDue := strings.Index(page, "<dt>Created</dt>"), strings.Index(page, "<dt>Event</dt>"), strings.Index(page, "<dt>Due</dt>")
	if iCreated < 0 || iEvent < 0 || iDue < 0 {
		t.Fatalf("page is missing one of the three date labels:\n%s", page)
	}
	createdLine := page[iCreated:iEvent]
	eventLine := page[iEvent:iDue]
	dueLine := page[iDue : iDue+strings.Index(page[iDue:], "</dl>")]

	if !strings.Contains(createdLine, "2026-09-01") || strings.Contains(createdLine, "<dt>Due</dt>") || strings.Contains(createdLine, "<dt>Event</dt>") {
		t.Errorf("Created: line wrong or carries another date's label:\n%s", createdLine)
	}
	if !strings.Contains(dueLine, "2026-09-22") || strings.Contains(dueLine, "<dt>Created</dt>") || strings.Contains(dueLine, "<dt>Event</dt>") {
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
	if !strings.Contains(page, "<dt>Stored weight</dt><dd>0.50</dd>") {
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

// correctRequest builds a POST /ui/units/{id}/correct request with
// req.Pattern and the {id} path value set as net/http's own ServeMux would
// set them — unitRequest's own precedent, extended for the correction
// route's own pattern.
func correctRequest(id, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/ui/units/"+id+"/correct", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Pattern = "POST /ui/units/{id}/correct"
	req.SetPathValue("id", id)
	return req
}

// TestCorrectView_SetsReferentFromPath is spec R5's own MUST: a correction
// submitted from the unit detail page sets CaptureInput.ReferentID to that
// unit's own id, taken from the request path — never from a submitted form
// field — so it always wins resolveReferent's explicit branch (doc 02 §5
// step 4) instead of falling into chat's own hybrid-recall/ambiguity-gate
// path. This is this route's own behavioural proof; ui_entrances_test.go's
// part (c) is its structural sibling, pinning the composite literal itself.
func TestCorrectView_SetsReferentFromPath(t *testing.T) {
	t.Parallel()

	// A deliberately distinctive id — never "unit-1", this file's own
	// default fixture id used elsewhere — so a handler that hardcodes
	// "unit-1" instead of actually reading the path cannot pass this test
	// by coincidence.
	const id = "unit-77-correction-target"
	fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeCorrected, Correction: &brain.Correction{UnitID: id}}}
	h := ui.New(ui.Deps{Capture: fake})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, correctRequest(id, "text=It's+due+Friday,+not+Thursday"))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ui/units/%s/correct = %d, want 200: %s", id, rec.Code, rec.Body.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("Capture was called %d time(s), want exactly 1", len(fake.calls))
	}
	got := fake.calls[0]
	if got.ReferentID != id {
		t.Errorf("ReferentID = %q, want %q (the path's own id)", got.ReferentID, id)
	}
	if got.Channel != "ui" {
		t.Errorf("Channel = %q, want \"ui\"", got.Channel)
	}
}

// TestCorrectView_NilCapturerIs503 is TestCaptureView_NilCapturerIs503's own
// sibling for the correction route: a nil Deps.Capture answers 503, never a
// panic on a nil interface call — serveCorrect shares its nil-check with
// serveCapture, but the check is untested on this route on its own.
func TestCorrectView_NilCapturerIs503(t *testing.T) {
	t.Parallel()

	// A nil-Capturer call is exactly the kind of mistake this test exists to
	// catch: if the guard is removed, h.deps.Capture.Capture panics on a nil
	// interface. Recovering here turns that into a clean, reported test
	// failure instead of crashing the whole test binary.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("POST /ui/units/unit-1/correct with no Capturer panicked instead of answering 503: %v", r)
		}
	}()

	h := ui.New(ui.Deps{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, correctRequest("unit-1", "text=It's+due+Friday,+not+Thursday"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("POST /ui/units/unit-1/correct with no Capturer = %d, want 503", rec.Code)
	}
}

// TestCorrectView_UnknownUnitIs404 is fix-unit-correction-form R4: a
// correction of a unit that does not exist answers 404 with a plain
// sentence, not the 500 every other Capture error gets.
func TestCorrectView_UnknownUnitIs404(t *testing.T) {
	t.Parallel()

	fake := &fakeCapturer{err: fmt.Errorf("capture: correction: %w", brain.ErrUnknownReferent)}
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Capture: fake}).ServeHTTP(rec, correctRequest("no-such-unit", "text=on+the+9th"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /ui/units/no-such-unit/correct = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No unit has that id.") {
		t.Errorf("body = %q, want the plain sentence", rec.Body.String())
	}
}

// TestCorrectView_CaptureErrorIs500 is TestUnitView_DetailErrorIs500's own
// pattern applied to serveCorrect: a Capturer.Capture error answers 500 and
// never reflects the raw error into the response body — serveCorrect's own
// error branch; TestCaptureView_CaptureErrorIs500 is serveCapture's.
func TestCorrectView_CaptureErrorIs500(t *testing.T) {
	t.Parallel()

	fake := &fakeCapturer{err: errors.New("boom")}
	h := ui.New(ui.Deps{Capture: fake})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, correctRequest("unit-1", "text=It's+due+Friday,+not+Thursday"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("POST /ui/units/unit-1/correct (Capture error) = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("response reflects the raw error:\n%s", rec.Body.String())
	}
}

// TestCorrectView_BodyIsBounded is TestCaptureView_BodyIsBounded's own
// sibling for the correction route: parseCaptureForm's MaxBytesReader bound
// is shared by serveCapture and serveCorrect, but the correction route's own
// call is untested on its own.
func TestCorrectView_BodyIsBounded(t *testing.T) {
	t.Parallel()

	fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "unit-1"}}
	h := ui.New(ui.Deps{Capture: fake})

	oversized := "text=" + strings.Repeat("a", 70*1024)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, correctRequest("unit-1", oversized))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /ui/units/unit-1/correct with an oversized submission = %d, want 400", rec.Code)
	}
	if len(fake.calls) != 0 {
		t.Errorf("Capture was called %d time(s) for an oversized submission, want 0", len(fake.calls))
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

// A correction sent from a unit page answers with that unit page — its
// fields read again after the edit, and the outcome naming what changed — so
// the user stays on the unit instead of landing on /ui/capture. With or
// without an HX-Request header the answer is the same full page.
func TestCorrectView_StaysOnTheUnitPage(t *testing.T) {
	t.Parallel()
	event := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	for name, hx := range map[string]bool{"plain post": false, "htmx": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var readID string
			detail := brain.UnitDetail{Unit: unit.Unit{ID: "unit-9", Type: unit.TypeEvent, Content: "Dentist", CreatedAt: event, EventAt: &event}}
			fake := &fakeCapturer{result: brain.CaptureResult{
				Outcome:    brain.OutcomeCorrected,
				Correction: &brain.Correction{UnitID: "unit-9", Fields: []correction.Field{correction.FieldEventAt}},
			}}
			h := ui.New(ui.Deps{Capture: fake, Units: stubUnitsReader{detail: detail, detailFound: true, calledID: &readID}})
			req := correctRequest("unit-9", "text=it+is+on+the+15th")
			if hx {
				req.Header.Set("HX-Request", "true")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("POST correct = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			for _, want := range []string{`data-unit-id="unit-9"`, "<main>", `data-outcome="corrected"`, "Changed: Event."} {
				if !strings.Contains(body, want) {
					t.Errorf("answer lacks %q:\n%s", want, body)
				}
			}
			if strings.Contains(body, "<h2>CAPTURE</h2>") {
				t.Errorf("answer is the capture page, not the unit page:\n%s", body)
			}
			if readID != "unit-9" {
				t.Errorf("Detail read %q after the correction, want the path's unit", readID)
			}
		})
	}
}

// A correction that finds the unit already holding what it says is not a
// change, and the page does not report one.
func TestCorrectView_AnUnchangedCorrectionSaysNothingChanged(t *testing.T) {
	t.Parallel()
	event := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	detail := brain.UnitDetail{Unit: unit.Unit{ID: "unit-9", Type: unit.TypeEvent, Content: "Dentist", CreatedAt: event, EventAt: &event}}
	fake := &fakeCapturer{result: brain.CaptureResult{
		Outcome:    brain.OutcomeCorrected,
		Correction: &brain.Correction{UnitID: "unit-9", Unchanged: true},
	}}
	h := ui.New(ui.Deps{Capture: fake, Units: stubUnitsReader{detail: detail, detailFound: true}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, correctRequest("unit-9", "text=it+is+on+the+15th"))

	body := rec.Body.String()
	if !strings.Contains(body, "Nothing was changed: the entry already says that.") {
		t.Errorf("answer does not say nothing changed:\n%s", body)
	}
	if strings.Contains(body, "Corrected") || strings.Contains(body, "Changed:") {
		t.Errorf("answer claims a change:\n%s", body)
	}
}

// When nothing changed, the unit page carries the same ask the capture page
// would, so the user knows what to write instead.
func TestCorrectView_AskStaysOnTheUnitPage(t *testing.T) {
	t.Parallel()
	detail := brain.UnitDetail{Unit: unit.Unit{ID: "unit-9", Type: unit.TypeTask, Content: "Pay rent", CreatedAt: time.Now()}}
	fake := &fakeCapturer{result: brain.CaptureResult{
		Outcome:    brain.OutcomeAsked,
		Correction: &brain.Correction{UnitID: "unit-9", Ambiguous: true, Why: brain.AskNotAnEdit},
	}}
	h := ui.New(ui.Deps{Capture: fake, Units: stubUnitsReader{detail: detail, detailFound: true}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, correctRequest("unit-9", "text=when+is+it"))

	body := rec.Body.String()
	for _, want := range []string{`data-unit-id="unit-9"`, `data-outcome="asked"`, "Nothing was changed"} {
		if !strings.Contains(body, want) {
			t.Errorf("answer lacks %q:\n%s", want, body)
		}
	}
}

// The correction form is a plain POST: the answer is a full page, so an
// error answer (404, 500) replaces the page with its message instead of
// htmx swapping an empty selection over the unit.
func TestUnitView_CorrectionFormIsAPlainPost(t *testing.T) {
	t.Parallel()
	detail := brain.UnitDetail{Unit: unit.Unit{ID: "unit-1", Type: unit.TypeTask, Content: "Pay rent", CreatedAt: time.Now()}}
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}}).ServeHTTP(rec, unitRequest("unit-1"))
	form := between(t, rec.Body.String(), `action="/ui/units/unit-1/correct"`, "</form>")
	if strings.Contains(form, "hx-") {
		t.Errorf("correction form carries htmx attributes:\n%s", form)
	}
}

// When the corrected unit cannot be read back (no longer live, or the read
// fails), the answer falls back to the capture outcome rather than a unit
// page with empty fields.
func TestCorrectView_UnreadableUnitFallsBackToTheOutcome(t *testing.T) {
	t.Parallel()
	for name, reader := range map[string]stubUnitsReader{
		"not found":   {detailFound: false},
		"read failed": {detailErr: errors.New("boom"), detailFound: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeCorrected, Correction: &brain.Correction{UnitID: "unit-9"}}}
			rec := httptest.NewRecorder()
			ui.New(ui.Deps{Capture: fake, Units: reader}).ServeHTTP(rec, correctRequest("unit-9", "text=x"))
			body := rec.Body.String()
			if strings.Contains(body, "<h2>UNIT</h2>") || !strings.Contains(body, `data-outcome="corrected"`) {
				t.Errorf("answer is not the capture outcome:\n%s", body)
			}
		})
	}
}

// The relations section says the unit is not connected only when it has no
// relation.
func TestUnitView_RelationsEmptyStateOnlyWhenEmpty(t *testing.T) {
	t.Parallel()
	const line = "Not connected to anything yet."
	render := func(detail brain.UnitDetail) string {
		rec := httptest.NewRecorder()
		ui.New(ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}}).ServeHTTP(rec, unitRequest("unit-1"))
		return rec.Body.String()
	}
	bare := brain.UnitDetail{Unit: unit.Unit{ID: "unit-1", Type: unit.TypeTask, Content: "Pay rent", CreatedAt: time.Now()}}
	if body := render(bare); !strings.Contains(body, line) {
		t.Errorf("a unit with no relations has no empty state:\n%s", body)
	}
	linked := bare
	linked.Relations = []brain.RelatedUnit{{RelationID: "r-1", Type: "relates_to", Confidence: 0.8, Outgoing: true, Other: unit.Unit{ID: "unit-2", Content: "Bank"}}}
	if body := render(linked); strings.Contains(body, line) {
		t.Errorf("a unit with a relation shows the empty state:\n%s", body)
	}
}
