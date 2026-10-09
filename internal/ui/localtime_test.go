package ui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the fixture zone must exist on every CI image, Windows included

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
)

// fixedClock is the injected ports.Clock: the UI reads its zone, never the
// machine's.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// buenosAires is the zone the client runs in: -03:00 all year, no DST.
func buenosAires(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

func clockIn(t *testing.T) func() time.Time {
	t.Helper()
	return fixedClock{now: time.Date(2026, 10, 9, 13, 0, 0, 0, buenosAires(t))}.Now
}

type todayStub struct{ today brain.Today }

func (s todayStub) Today(context.Context) (brain.Today, error) { return s.today, nil }

func get(t *testing.T, deps ui.Deps, pattern, target string, pathID string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Pattern = pattern
	if pathID != "" {
		req.SetPathValue("id", pathID)
	}
	rec := httptest.NewRecorder()
	ui.New(deps).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

const zoneNote = "Times in America/Argentina/Buenos_Aires (UTC-03:00)"

// TestLocalTime_UnitPageShowsEveryDateInTheClocksZone is the client's defect:
// event 11:00 local was stored 14:00Z and the page said 14:00.
func TestLocalTime_UnitPageShowsEveryDateInTheClocksZone(t *testing.T) {
	t.Parallel()
	event := time.Date(2026, 10, 15, 14, 0, 0, 0, time.UTC)
	due := time.Date(2026, 10, 16, 2, 30, 0, 0, time.UTC) // crosses local midnight backwards
	detail := brain.UnitDetail{Unit: unit.Unit{
		ID: "u1", Type: unit.TypeEvent, Content: "Dentist", Weight: 0.5,
		CreatedAt: time.Date(2026, 10, 9, 16, 27, 0, 0, time.UTC), EventAt: &event, DueAt: &due,
	}}
	page := get(t, ui.Deps{Now: clockIn(t), Units: stubUnitsReader{detail: detail, detailFound: true}},
		"GET /ui/units/{id}", "/ui/units/u1", "u1")

	for _, want := range []string{"Created: 2026-10-09 13:27", "Event: 2026-10-15 11:00", "Due: 2026-10-15 23:30", zoneNote} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
	for _, bad := range []string{"16:27", "14:00"} {
		if strings.Contains(page, bad) {
			t.Errorf("page still shows UTC %q:\n%s", bad, page)
		}
	}
}

func TestLocalTime_TodayShowsEveryDateInTheClocksZone(t *testing.T) {
	t.Parallel()
	today := fixedToday() // due 09-22 09:00Z, event 09-23 14:30Z, consolidation 09-21 03:00Z, energy 09-22 07:00Z
	page := get(t, ui.Deps{Now: clockIn(t), Today: todayStub{today}}, "GET /ui", "/ui", "")

	for _, want := range []string{"Due: 2026-09-22 06:00", "Event: 2026-09-23 11:30", "Last consolidation: 2026-09-21 00:00", "consolidation, 2026-09-22 04:00", zoneNote} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
}

func TestLocalTime_BeliefsShowLastReinforcementInTheClocksZone(t *testing.T) {
	t.Parallel()
	page := get(t, ui.Deps{Now: clockIn(t), Beliefs: &fakeBeliefs{groups: beliefFixture()}}, "GET /ui/beliefs", "/ui/beliefs", "")
	// v-1: 2026-09-02T08:15Z -> 05:15 local; g-1: 2026-07-15T12:00Z -> 09:00.
	for _, want := range []string{"2026-09-02 05:15", "2026-07-15 09:00", zoneNote} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
}

func TestLocalTime_ActivityRowsAndTimestampChangesUseOneFormatAndZone(t *testing.T) {
	t.Parallel()
	row := activityRow("c-1", ports.ActionCorrectionApplied, 3,
		brain.ChangedField{Name: "event_at", Previous: "2026-10-16T13:00:00Z", Next: "2026-10-15T11:00:00-03:00"},
		brain.ChangedField{Name: "note", Previous: "2026-10-16", Next: "not a time"},
		brain.ChangedField{Name: "weight", Previous: "0.5", Next: "0.6"})
	row.OccurredAt = time.Date(2026, 10, 9, 16, 10, 57, 0, time.UTC)
	fake := &fakeActivity{page: brain.ActivityPage{Rows: []brain.ActivityRow{row}}}
	page := get(t, ui.Deps{Now: clockIn(t), Activity: fake}, activityGetPattern, "/ui/activity", "")

	for _, want := range []string{
		">2026-10-09 13:10<",
		"event_at: 2026-10-16 10:00 → 2026-10-15 11:00",
		"note: 2026-10-16 → not a time",
		"weight: 0.5 → 0.6",
		`datetime="2026-10-09T16:10:57Z"`,
		zoneNote,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
}

// The older-rows cursor is an API-shaped query value, not displayed text: it
// stays UTC RFC3339 so the pagination contract does not move.
func TestLocalTime_PaginationCursorStaysUTC(t *testing.T) {
	t.Parallel()
	next := &ports.DecisionCursor{OccurredAt: time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC), Seq: 7}
	fake := &fakeActivity{page: brain.ActivityPage{Next: next}}
	page := get(t, ui.Deps{Now: clockIn(t), Activity: fake}, activityGetPattern, "/ui/activity", "")
	if !strings.Contains(page, "before_at=2026-10-09T16%3A00%3A00Z") {
		t.Errorf("cursor moved off UTC RFC3339:\n%s", page)
	}
}

// With no zone injected (a bare template render) the page does not guess one:
// UTC, and no zone note claiming otherwise.
func TestLocalTime_NoClockFallsBackToUTCWithoutANote(t *testing.T) {
	t.Parallel()
	event := time.Date(2026, 10, 15, 14, 0, 0, 0, time.UTC)
	detail := brain.UnitDetail{Unit: unit.Unit{ID: "u1", Type: unit.TypeEvent, EventAt: &event}}
	page := get(t, ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}}, "GET /ui/units/{id}", "/ui/units/u1", "u1")
	if !strings.Contains(page, "Event: 2026-10-15 14:00") || strings.Contains(page, "Times in") {
		t.Errorf("fallback wrong:\n%s", page)
	}
}

// time.Local's name is the literal "Local" on every machine (doc 02): the note
// must not print it, only the offset.
func TestLocalTime_NoteDescribesAnUnnamedZoneByItsOffset(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("", -3*3600)
	clock := fixedClock{now: time.Date(2026, 10, 9, 13, 0, 0, 0, loc)}
	page := get(t, ui.Deps{Now: clock.Now, Today: todayStub{brain.Today{}}}, "GET /ui", "/ui", "")
	if !strings.Contains(page, "Times in UTC-03:00") {
		t.Errorf("note wrong for an unnamed zone:\n%s", page)
	}
}
