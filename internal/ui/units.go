package ui

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// errHalfCursor and errMalformedCursor are parseBrowseCursor's own two 400
// reasons (design §3.2) — named so the handler's error text and the tests
// that assert on it read the same sentence.
var (
	errHalfCursor      = errors.New("after_created and after_id must both be present, or neither")
	errMalformedCursor = errors.New("after_created must be an RFC3339 timestamp, and after_id must not be empty")
)

// serveUnits answers GET /ui/units: a search when the request carries q —
// Searcher.ForText and nothing else (I22, spec R2) — or a type-filtered,
// keyset-paginated browse page otherwise (spec R1).
func (h *Handler) serveUnits(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	if q := query.Get("q"); q != "" {
		h.serveUnitsSearch(w, r, q)
		return
	}

	if h.deps.Units == nil {
		http.Error(w, "units: not wired in this build", http.StatusServiceUnavailable)
		return
	}

	types, err := parseTypeFilter(query["type"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cursor, err := parseBrowseCursor(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	page, err := h.deps.Units.Browse(r.Context(), types, cursor)
	if err != nil {
		slog.Error("units: browse failed", "err", err)
		http.Error(w, "units: internal error", http.StatusInternalServerError)
		return
	}

	renderUnits(w, r, page.Units, page.Next, query["type"])
}

// serveUnitsSearch answers /ui/units?q= — the browse view's own search
// entrance, reaching Searcher.ForText and rendering its order unchanged: no
// pagination, no type filter (design §3.4, OR6).
func (h *Handler) serveUnitsSearch(w http.ResponseWriter, r *http.Request, q string) {
	if h.deps.Search == nil {
		http.Error(w, "units: search is not wired in this build", http.StatusServiceUnavailable)
		return
	}
	units, _, err := h.deps.Search.ForText(r.Context(), q)
	if err != nil {
		slog.Error("units: search failed", "err", err)
		http.Error(w, "units: internal error", http.StatusInternalServerError)
		return
	}
	renderUnits(w, r, units, nil, nil)
}

// renderUnits writes the rows fragment alone on HX-Request, or the full
// page otherwise (design §3.2) — both branches internal/ui's own views
// (units.templ), never a hand-built HTML string here.
func renderUnits(w http.ResponseWriter, r *http.Request, units []unit.Unit, next *ports.BrowseCursor, types []string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Header.Get("HX-Request") == "true" {
		_ = unitsRows(units, next, types).Render(r.Context(), w)
		return
	}
	_ = UnitsPage(units, next, types).Render(r.Context(), w)
}

// parseTypeFilter parses every "type" query value through unit.ParseType —
// an empty set means all types (spec R1; OQ1's own ruling: no separate
// status axis). The first unknown value is a 400 naming it.
func parseTypeFilter(raw []string) ([]unit.Type, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	types := make([]unit.Type, 0, len(raw))
	for _, s := range raw {
		t, err := unit.ParseType(s)
		if err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, nil
}

// parseBrowseCursor reads the (after_created, after_id) keyset pair
// (design §3.2): both present or neither, after_created RFC3339-parseable,
// and neither key present with an empty value.
func parseBrowseCursor(q url.Values) (*ports.BrowseCursor, error) {
	hasCreated, hasID := q.Has("after_created"), q.Has("after_id")
	if !hasCreated && !hasID {
		return nil, nil
	}
	if hasCreated != hasID {
		return nil, errHalfCursor
	}

	createdRaw, idRaw := q.Get("after_created"), q.Get("after_id")
	if createdRaw == "" || idRaw == "" {
		return nil, errMalformedCursor
	}
	created, err := time.Parse(time.RFC3339, createdRaw)
	if err != nil {
		return nil, errMalformedCursor
	}
	return &ports.BrowseCursor{CreatedAt: created, ID: idRaw}, nil
}

// serveUnit answers GET /ui/units/{id}: one live unit, its stored weight
// and its live relations, via UnitsService.Detail (spec R3). A
// found=false — archived, superseded, incomplete or an absent id all
// resolve to it alike, I02 enforced once at UnitRepo.LiveByIDs and never
// reimplemented here — answers the same 404 class GET /units/{id} already
// gives.
func (h *Handler) serveUnit(w http.ResponseWriter, r *http.Request) {
	if h.deps.Units == nil {
		http.Error(w, "units: not wired in this build", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	detail, found, err := h.deps.Units.Detail(r.Context(), id)
	if err != nil {
		slog.Error("units: detail failed", "err", err)
		http.Error(w, "units: internal error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = UnitPage(detail).Render(r.Context(), w)
}

// moreURL builds the "more" link's hx-get target from next and the same
// type filter the current page used — so paging forward never silently
// drops a filter the caller already applied.
func moreURL(types []string, next *ports.BrowseCursor) string {
	if next == nil {
		return ""
	}
	v := url.Values{}
	for _, t := range types {
		v.Add("type", t)
	}
	v.Set("after_created", next.CreatedAt.UTC().Format(time.RFC3339))
	v.Set("after_id", next.ID)
	return "/ui/units?" + v.Encode()
}
