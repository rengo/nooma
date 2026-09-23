package ui_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
)

// fixedToday is one instant's worth of brain.Today, built by hand rather
// than through TodayService — this package's tests own the rendering, not
// the read model's assembly (design m4a §3.1's "ui imports brain only for
// its output types" split; internal/brain's own tests cover assembly).
// KindLoad carries zero members on purpose: an empty focus is the nil-deref
// mutation TestTodayView_RendersThreeSectionsFromTheModel's own doc comment
// names.
func fixedToday() brain.Today {
	due := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	event := time.Date(2026, 9, 23, 14, 30, 0, 0, time.UTC)
	consolidation := time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	energyAt := time.Date(2026, 9, 22, 7, 0, 0, 0, time.UTC)

	return brain.Today{
		Now: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
		Focuses: []brain.Focus{
			{
				Kind: focus.KindTask,
				Members: []brain.FocusMember{
					{ID: "task-1", Type: unit.TypeTask, Content: "Call the dentist", DueAt: &due, Score: 0.91},
					{ID: "task-2", Type: unit.TypeTask, Content: "Renew the passport", EventAt: &event, Score: math.NaN()},
				},
			},
			{Kind: focus.KindLoad, Members: nil},
		},
		Digest: brain.PendingDigest{
			LowEnergy: false,
			Items: []brain.DigestLine{
				{TriggerID: "trg-1", Text: "Water the plants", FireAt: due, Deferrals: 0},
			},
			Held:     2,
			Question: &ports.RelationQuestion{FromContent: "Buy stamps", ToContent: "Mail the package"},
		},
		Status: brain.VaultStatus{
			LastConsolidationAt: &consolidation,
			Energy:              &prospection.EnergyReading{Level: 0.7, Source: "consolidation", RecordedAt: energyAt},
			Undelivered:         3,
			OpenQuestions:       1,
		},
	}
}

// TestTodayView_RendersThreeSectionsFromTheModel is design m4a §7's PR 7
// task 7.1: a fixed brain.Today renders each focus member's id, content and
// Score (two decimals) in rank order; each digest line's text; Held as a
// count, never a list; the question's two endpoints; the six status lines;
// a NaN Score renders the literal "NaN". Asserted on structure
// (strings.Contains, index order), never the whole document (design §8).
func TestTodayView_RendersThreeSectionsFromTheModel(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	if err := ui.Today(fixedToday(), ui.Serving{Bind: "127.0.0.1:8080", CookieAuth: true}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Today.Render: %v", err)
	}
	page := buf.String()

	if !strings.Contains(page, "<nav") {
		t.Error("page carries no <nav> — deferred from PR 2, this is the first navigable view (task 7.0)")
	}

	// FOCUS: id, content and Score, in rank order — never re-sorted,
	// filtered or dropped (mutation this test catches).
	for _, want := range []string{"task-1", "Call the dentist", "0.91", "task-2", "Renew the passport", "NaN"} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "0.00") {
		t.Error("a NaN Score rendered as 0.00 instead of the literal \"NaN\" — Q7's ruling")
	}
	if i1, i2 := strings.Index(page, "task-1"), strings.Index(page, "task-2"); i1 < 0 || i2 < 0 || i1 > i2 {
		t.Errorf("task-1 (%d) does not precede task-2 (%d) — rank order lost", i1, i2)
	}

	// PENDING DIGEST: raw items in Carry order, Held as a count.
	for _, want := range []string{"PENDING DIGEST", "Water the plants", "Held", "2", "Buy stamps", "Mail the package"} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}

	// SYSTEM: the six lines.
	for _, want := range []string{"SYSTEM", "2026-09-21", "0.70", "consolidation", "2026-09-22", "Undelivered", "3", "Open questions", "1", "127.0.0.1:8080", "enabled"} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
}

// TestTodayView_I18DatesLabelled is design m4a §7's PR 7 task 7.2: a member
// with DueAt and one with EventAt render under different labels, never
// swapped — I18's own UI failure mode.
func TestTodayView_I18DatesLabelled(t *testing.T) {
	t.Parallel()

	due := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	event := time.Date(2026, 9, 23, 14, 30, 0, 0, time.UTC)
	today := brain.Today{
		Focuses: []brain.Focus{
			{
				Kind: focus.KindTask,
				Members: []brain.FocusMember{
					{ID: "with-due", Content: "Pay the rent", DueAt: &due},
					{ID: "with-event", Content: "Dentist appointment", EventAt: &event},
				},
			},
			{Kind: focus.KindLoad},
		},
	}

	var buf strings.Builder
	if err := ui.Today(today, ui.Serving{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Today.Render: %v", err)
	}
	page := buf.String()

	dueLine := page[strings.Index(page, "with-due"):strings.Index(page, "with-event")]
	if !strings.Contains(dueLine, "Due:") || strings.Contains(dueLine, "Event:") {
		t.Errorf("the DueAt member's own line does not carry the Due: label, or wrongly carries Event::\n%s", dueLine)
	}
	eventLine := page[strings.Index(page, "with-event"):]
	if !strings.Contains(eventLine, "Event:") || strings.Contains(eventLine, "Due:") {
		t.Errorf("the EventAt member's own line does not carry the Event: label, or wrongly carries Due::\n%s", eventLine)
	}
}

// TestTodayView_NilTodayReaderAnswers503 is design m4a §7.2's PR 7 tip row:
// from this PR, ui.New(ui.Deps{})'s own no-TodayReader fixture — PR 2's own
// shell-era construction (internal/httpapi's task 2.3) — answers 503, not
// the retired PR 2-6 shell. It takes over that fixture's PR 7+ behavior;
// internal/httpapi's TestHandlerServesAPIRootAndUIShell narrows its own
// scope to PR 2 through PR 6 accordingly (task 7.3).
func TestTodayView_NilTodayReaderAnswers503(t *testing.T) {
	t.Parallel()

	h := ui.New(ui.Deps{})

	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /ui with no TodayReader = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
