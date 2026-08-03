package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rengo/nooma/internal/core/unit"
)

func getRoute(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestUnitByIDHandler_ReturnsLiveUnit is R2.6's own ordinary-case L2 test:
// GET /units/{id} resolves a `pool` unit through RecallService.LiveByIDs —
// never ports.UnitRepo.ByID, the any-status escape hatch corrections and
// audit use — and renders it with the shared unitResponse shape.
func TestUnitByIDHandler_ReturnsLiveUnit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	svc, units, _, _ := newTestRecallService(t)
	if err := units.Create(ctx, poolUnitFixture("u-live", "descaling the coffee machine")); err != nil {
		t.Fatalf("seeding u-live: %v", err)
	}

	h := Handler(Deps{Version: "test", Recall: svc})
	rec := getRoute(t, h, "/units/u-live")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got unitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v; body = %s", err, rec.Body.String())
	}
	if got.ID != "u-live" {
		t.Errorf("id = %q, want %q", got.ID, "u-live")
	}
}

// TestUnitByIDHandler_NonLiveUnitReturns404SameAsUnknownID is R2.6's own
// I02-shaped negative test (spec's own Scenario): a unit that exists but
// whose status is not `pool` (here, `archived`) must answer the identical
// 404 shape an unknown id would — the surface must not leak the existence
// of a non-live unit through its error shape. Both paths reach
// unitByIDHandler's own single `len(units) == 0` branch, which is the
// strongest form of "the same shape": there is only one shape to produce.
func TestUnitByIDHandler_NonLiveUnitReturns404SameAsUnknownID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	svc, units, _, _ := newTestRecallService(t)
	archived := poolUnitFixture("u-archived", "an old note")
	archived.Status = unit.StatusArchived
	if err := units.Create(ctx, archived); err != nil {
		t.Fatalf("seeding u-archived: %v", err)
	}
	if err := units.Create(ctx, poolUnitFixture("u-live", "a live note")); err != nil {
		t.Fatalf("seeding u-live: %v", err)
	}

	h := Handler(Deps{Version: "test", Recall: svc})

	live := getRoute(t, h, "/units/u-live")
	if live.Code != http.StatusOK {
		t.Fatalf("live unit: status = %d, want %d; body = %s", live.Code, http.StatusOK, live.Body.String())
	}

	archivedResp := getRoute(t, h, "/units/u-archived")
	unknownResp := getRoute(t, h, "/units/does-not-exist")

	if archivedResp.Code != http.StatusNotFound {
		t.Fatalf("archived unit: status = %d, want %d; body = %s", archivedResp.Code, http.StatusNotFound, archivedResp.Body.String())
	}
	if unknownResp.Code != http.StatusNotFound {
		t.Fatalf("unknown id: status = %d, want %d; body = %s", unknownResp.Code, http.StatusNotFound, unknownResp.Body.String())
	}
	if archivedResp.Body.String() != unknownResp.Body.String() {
		t.Errorf("archived unit's 404 body (%q) differs from unknown id's 404 body (%q) — the surface leaks existence through its error shape",
			archivedResp.Body.String(), unknownResp.Body.String())
	}
}

// TestUnitByIDHandler_UnwiredRecallServiceReturns503 applies 13b's own
// nil-dependency pattern to the unit-by-id route.
func TestUnitByIDHandler_UnwiredRecallServiceReturns503(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test"}) // Recall is nil, deliberately.
	rec := getRoute(t, h, "/units/whatever")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

// TestUnitsListHandler_ReturnsRequestedLiveUnits is R2.6's own collection
// test: GET /units?ids=a,b,c maps 1:1 onto LiveByIDs (design D10), dropping
// any id that does not resolve rather than erroring per-id.
func TestUnitsListHandler_ReturnsRequestedLiveUnits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	svc, units, _, _ := newTestRecallService(t)
	archived := poolUnitFixture("u-b", "archived")
	archived.Status = unit.StatusArchived
	if err := units.Create(ctx, poolUnitFixture("u-a", "a")); err != nil {
		t.Fatalf("seeding u-a: %v", err)
	}
	if err := units.Create(ctx, archived); err != nil {
		t.Fatalf("seeding u-b: %v", err)
	}

	h := Handler(Deps{Version: "test", Recall: svc})
	rec := getRoute(t, h, "/units?ids=u-a,u-b,does-not-exist")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got unitsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v; body = %s", err, rec.Body.String())
	}
	if len(got.Units) != 1 || got.Units[0].ID != "u-a" {
		t.Errorf("units = %+v, want exactly [u-a] — u-b is archived and does-not-exist is unknown, both dropped silently", got.Units)
	}
}

// TestUnitsListHandler_NoIdsIs400 mirrors captureHandler's own required-field
// guard: a caller supplying no `ids` gets a plain 400, not an empty list.
func TestUnitsListHandler_NoIdsIs400(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := newTestRecallService(t)
	h := Handler(Deps{Version: "test", Recall: svc})

	rec := getRoute(t, h, "/units")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestUnitsListHandler_UnwiredRecallServiceReturns503 applies 13b's own
// nil-dependency pattern to the units-list route.
func TestUnitsListHandler_UnwiredRecallServiceReturns503(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test"}) // Recall is nil, deliberately.
	rec := getRoute(t, h, "/units?ids=whatever")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}
