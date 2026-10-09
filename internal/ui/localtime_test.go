package ui_test

import (
	"context"
	"html"
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

// Brain-written prose embeds UTC instants (armRationale, internal/brain/capture.go);
// under a footer saying times are local they must read local too. Stored text is
// untouched: only the rendering changes.
func TestLocalTime_ActivityRationaleInstantsAreLocal(t *testing.T) {
	t.Parallel()
	row := activityRow("a-1", ports.ActionCaptureUnitCreated, 1)
	row.Rationale = "armed a trigger for 2026-10-16T13:00:00Z, firing at 2026-10-09T16:53:30Z (lead 2h); kept 2026-10-16 and 12:00Z as written"
	fake := &fakeActivity{page: brain.ActivityPage{Rows: []brain.ActivityRow{row}}}
	page := get(t, ui.Deps{Now: clockIn(t), Activity: fake}, activityGetPattern, "/ui/activity", "")

	want := "armed a trigger for 2026-10-16 10:00, firing at 2026-10-09 13:53 (lead 2h); kept 2026-10-16 and 12:00Z as written"
	if !strings.Contains(page, want) {
		t.Errorf("rationale not localized, want %q in:\n%s", want, page)
	}
	if strings.Contains(page, "T13:00:00Z") || strings.Contains(page, "T16:53:30Z") {
		t.Errorf("a UTC instant survived in the rationale:\n%s", page)
	}
}

func TestLocalTime_CaptureProseInstantsAreLocal(t *testing.T) {
	t.Parallel()
	for name, result := range map[string]brain.CaptureResult{
		"arm_refused": {Outcome: brain.OutcomeArmRefused, ArmRefused: &brain.ArmRefused{Message: "too soon: 2026-10-15T14:00:00Z"}},
		"conversed":   {Outcome: brain.OutcomeConversed, Reply: "see you 2026-10-15T14:00:00Z"},
	} {
		req := captureRequest("text=hi")
		rec := httptest.NewRecorder()
		ui.New(ui.Deps{Now: clockIn(t), Capture: &fakeCapturer{result: result}}).ServeHTTP(rec, req)
		body := rec.Body.String()
		if !strings.Contains(body, "2026-10-15 11:00") || strings.Contains(body, "14:00:00Z") {
			t.Errorf("%s: capture prose not localized:\n%s", name, body)
		}
	}
}

// The user's own captured text is shown as written, even when it contains an
// instant: only fields and the brain's own prose are converted.
func TestLocalTime_UserTextIsShownAsWritten(t *testing.T) {
	t.Parallel()
	const raw = "call at 2026-10-15T14:00:00Z"
	today := brain.Today{Digest: brain.PendingDigest{Items: []brain.DigestLine{{TriggerID: "t", Text: raw}}}}
	page := get(t, ui.Deps{Now: clockIn(t), Today: todayStub{today}}, "GET /ui", "/ui", "")
	if !strings.Contains(page, raw) {
		t.Errorf("digest line (user text) was rewritten:\n%s", page)
	}

	row := activityRow("u-1", ports.ActionCorrectionApplied, 1,
		brain.ChangedField{Name: "content", Previous: raw, Next: "moved to 2026-10-16T09:00:00Z please"},
		brain.ChangedField{Name: "event_at", Previous: "2026-10-16T13:00:00Z", Next: "2026-10-15T11:00:00-03:00"})
	page = get(t, ui.Deps{Now: clockIn(t), Activity: &fakeActivity{page: brain.ActivityPage{Rows: []brain.ActivityRow{row}}}},
		activityGetPattern, "/ui/activity", "")
	for _, want := range []string{"content: " + raw + " → moved to 2026-10-16T09:00:00Z please", "event_at: 2026-10-16 10:00 → 2026-10-15 11:00"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
}

// Token boundaries: an instant glued to a letter or digit, or inside a URL or
// query, is not an instant the brain wrote.
func TestLocalTime_ProseBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"at 2026-10-16T13:00:00Z.", "at 2026-10-16 10:00."},
		{"(2026-10-16T13:00:00Z)", "(2026-10-16 10:00)"},
		{"12026-10-16T13:00:00Z", "12026-10-16T13:00:00Z"},
		{"2026-10-16T13:00:00Z0", "2026-10-16T13:00:00Z0"},
		{"x2026-10-16T13:00:00Z", "x2026-10-16T13:00:00Z"},
		{"2026-10-16T13:00:00Zx", "2026-10-16T13:00:00Zx"},
		{"?at=2026-10-16T13:00:00Z", "?at=2026-10-16T13:00:00Z"},
		{"a=2026-10-16T13:00:00Z", "a=2026-10-16T13:00:00Z"},
		{"/2026-10-16T13:00:00Z", "/2026-10-16T13:00:00Z"},
		{"#2026-10-16T13:00:00Z", "#2026-10-16T13:00:00Z"},
		{"&2026-10-16T13:00:00Z", "&2026-10-16T13:00:00Z"},
		{"see https://x.example/a b2026 then 2026-10-16T13:00:00Z", "see https://x.example/a b2026 then 2026-10-16 10:00"},
		{"see https://x.example/p:2026-10-16T13:00:00Z end", "see https://x.example/p:2026-10-16T13:00:00Z end"},
		{"armed for 2026-10-16T13:00:00Z, firing at 2026-10-09T16:53:30Z", "armed for 2026-10-16 10:00, firing at 2026-10-09 13:53"},
	} {
		row := activityRow("p-1", ports.ActionCaptureUnitCreated, 1)
		row.Rationale = tc.in
		page := get(t, ui.Deps{Now: clockIn(t), Activity: &fakeActivity{page: brain.ActivityPage{Rows: []brain.ActivityRow{row}}}},
			activityGetPattern, "/ui/activity", "")
		if !strings.Contains(page, "<span>"+html.EscapeString(tc.want)+"</span>") {
			t.Errorf("rationale %q: want %q in:\n%s", tc.in, tc.want, page)
		}
	}
}

// Each instant converts with the offset in force at that instant, not the
// clock's: Madrid leaves summer time on 2026-10-25, the clock sits before it.
func TestLocalTime_DSTOffsetsArePerInstantNotTheClocks(t *testing.T) {
	t.Parallel()
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	clock := fixedClock{now: time.Date(2026, 10, 9, 13, 0, 0, 0, madrid)}.Now // CEST, +02:00
	row := activityRow("d-1", ports.ActionCorrectionApplied, 1,
		brain.ChangedField{Name: "event_at", Previous: "2026-10-24T12:00:00Z", Next: "2026-10-26T12:00:00Z"})
	page := get(t, ui.Deps{Now: clock, Activity: &fakeActivity{page: brain.ActivityPage{Rows: []brain.ActivityRow{row}}}},
		activityGetPattern, "/ui/activity", "")
	if !strings.Contains(page, "event_at: 2026-10-24 14:00 → 2026-10-26 13:00") {
		t.Errorf("offsets not per instant (want 14:00 CEST then 13:00 CET):\n%s", page)
	}
}
