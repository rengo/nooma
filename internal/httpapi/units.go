package httpapi

import (
	"net/http"
	"strings"
)

// unitsListResponse is what GET /units?ids=… answers with — a flat wrapper
// around the shared unit renderer, matching recallResponse's own
// "units" field so both routes' consumers read the same shape.
type unitsListResponse struct {
	Units []unitResponse `json:"units"`
}

// notFoundBody is the "not found" shape GET /units/{id} answers with for
// both an unknown id and a non-live one (status != pool) — design D10, spec
// R2.6: "the same 404 body... so the surface does not leak the existence of
// a non-live unit through its error shape either." Both cases reach it
// through unitByIDHandler's own single `len(units) == 0` branch below, which
// is the strongest form of "the same shape": there is only one code path
// that can produce it.
var notFoundBody = map[string]string{"error": "unit not found"}

// unitByIDHandler wires GET /units/{id} to RecallService.LiveByIDs — never
// ports.UnitRepo.ByID, which internal/ports/unitrepo.go's own doc comment
// reserves as the any-status escape hatch corrections and audit need (R1.5),
// not a public read surface. A non-pool unit and an unknown id both resolve
// to zero live units and therefore the identical 404 (I02).
func unitByIDHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The same nil-dependency treatment recallHandler and captureHandler
		// both apply to their own dependency — see recall.go's doc comment.
		if d.Recall == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "recall is not wired in this build"})
			return
		}

		id := r.PathValue("id")
		units, err := d.Recall.LiveByIDs(r.Context(), []string{id})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unit lookup failed"})
			return
		}
		if len(units) == 0 {
			writeJSON(w, http.StatusNotFound, notFoundBody)
			return
		}
		writeJSON(w, http.StatusOK, renderUnit(units[0]))
	}
}

// unitsListHandler wires GET /units?ids=a,b,c to the same RecallService.LiveByIDs
// call — design D10: "GET /units?ids=…, and that is the whole of it... maps
// 1:1 onto the single live read method". An id that does not resolve
// (unknown or not live) is simply absent from the response; the collection
// route has no per-id error shape to leak, unlike the single-unit route.
func unitsListHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Recall == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "recall is not wired in this build"})
			return
		}

		raw := r.URL.Query().Get("ids")
		var ids []string
		for _, id := range strings.Split(raw, ",") {
			if id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids is required"})
			return
		}

		units, err := d.Recall.LiveByIDs(r.Context(), ids)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unit lookup failed"})
			return
		}
		rendered := make([]unitResponse, len(units))
		for i, u := range units {
			rendered[i] = renderUnit(u)
		}
		writeJSON(w, http.StatusOK, unitsListResponse{Units: rendered})
	}
}
