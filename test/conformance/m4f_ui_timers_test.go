// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
	"github.com/rengo/nooma/test/support/memrepo"
)

// m4f's UI half, timers (doc 02 §8): the timers listed and cancelled from the
// UI through the transition the chat uses (I46).

// uiTimersVault is /ui/timers' whole stack over memrepos: the real handler and
// TimersService, with i46Two pending and tmr-gone already cancelled.
type uiTimersVault struct {
	timers  *memrepo.Timers
	log     *memrepo.DecisionLog
	signals *memrepo.Signals
	units   *memrepo.Units
	h       http.Handler
}

func newUITimersVault(t *testing.T) *uiTimersVault {
	t.Helper()
	ctx := context.Background()
	v := &uiTimersVault{timers: memrepo.NewTimers(), log: memrepo.NewDecisionLog(), signals: memrepo.NewSignals(), units: memrepo.NewUnits()}
	for _, s := range append(i46Two, i46Timer{"tmr-gone", i46Mama.Add(time.Hour), "Regar"}) {
		text := s.text
		if err := v.timers.Create(ctx, ports.Timer{ID: s.id, FireAt: s.at, ActionText: &text, CreatedAt: i46Now.Add(-time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.timers.Cancel(ctx, "tmr-gone"); err != nil {
		t.Fatal(err)
	}
	svc := brain.NewTimersService(fixedClock{now: i46Now}, &counterIDs{}, v.timers, v.log)
	v.h = httpapi.Handler(httpapi.Deps{Version: "test", UI: ui.New(ui.Deps{
		Timers: svc, Now: func() time.Time { return i46Now },
	})})
	return v
}

func (v *uiTimersVault) do(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	v.h.ServeHTTP(rec, req)
	return rec
}

func (v *uiTimersVault) cancelRows(t *testing.T) []ports.Decision {
	t.Helper()
	var out []ports.Decision
	for _, d := range i40Actions(t, v.log) {
		if d.Action == ports.ActionCaptureTimerCancelled {
			out = append(out, d)
		}
	}
	return out
}

// TestI46_TheTimersPageListsTheLiveTimers: /ui/timers lists every pending
// timer, soonest first, at its hour in the user's zone, each with its own
// cancel form; a timer no longer pending is not listed, and the page reads
// timers, never units (I04).
func TestI46_TheTimersPageListsTheLiveTimers(t *testing.T) {
	v := newUITimersVault(t)

	rec := v.do(t, http.MethodGet, "/ui/timers")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/timers = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	pizza, mama := strings.Index(body, "Sacar la pizza del horno"), strings.Index(body, "Llamar a mamá")
	if pizza < 0 || mama < 0 || pizza > mama {
		t.Fatalf("want both live timers, the pizza (10:28) before mamá (11:07):\n%s", body)
	}
	for _, want := range []string{"2026-10-26 10:28", "2026-10-26 11:07", `action="/ui/timers/tmr-pizza/cancel"`, `action="/ui/timers/tmr-mama/cancel"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Regar") || strings.Contains(body, "tmr-gone") {
		t.Errorf("a cancelled timer is listed:\n%s", body)
	}
	if rows := i40Actions(t, v.log); len(rows) != 0 {
		t.Errorf("reading the timers wrote %d decision row(s), want none", len(rows))
	}
}

// TestI46_TheTimersPageSaysWhenNoneIsSet: an empty list says so.
func TestI46_TheTimersPageSaysWhenNoneIsSet(t *testing.T) {
	v := newUITimersVault(t)
	for _, id := range []string{"tmr-pizza", "tmr-mama"} {
		if err := v.timers.Cancel(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	body := v.do(t, http.MethodGet, "/ui/timers").Body.String()
	if !strings.Contains(body, "No timer is set") || strings.Contains(body, "/cancel") {
		t.Errorf("an empty page should say no timer is set and offer no cancel:\n%s", body)
	}
}

// TestI46_ACancelFromTheUIIsTheChatsTransition: a cancel from the UI moves the
// timer pending -> cancelled (never a delete) and writes the one
// capture.timer.cancelled row the chat writes, its position counted in the
// live list the page showed and its surface named; it emits no signal
// (ADR-0052) and creates no unit (I04). The page comes back without it.
func TestI46_ACancelFromTheUIIsTheChatsTransition(t *testing.T) {
	v := newUITimersVault(t)

	rec := v.do(t, http.MethodPost, "/ui/timers/tmr-mama/cancel")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Timer cancelled: 2026-10-26 11:07 — Llamar a mamá.") || strings.Contains(body, `action="/ui/timers/tmr-mama/cancel"`) {
		t.Errorf("the answer should confirm the cancel and list mamá no more:\n%s", body)
	}

	live, err := v.timers.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].ID != "tmr-pizza" || v.timers.Count() != 3 {
		t.Errorf("pending after the cancel = %+v of %d timers, want only the pizza, all three kept (a cancel is never a delete)", live, v.timers.Count())
	}

	rows := v.cancelRows(t)
	if len(rows) != 1 {
		t.Fatalf("capture.timer.cancelled rows = %+v, want one", rows)
	}
	var ctxRow struct {
		TimerID  string `json:"timer_id"`
		FireAt   string `json:"fire_at"`
		Position int    `json:"position"`
		Listed   int    `json:"listed"`
		Surface  string `json:"surface"`
	}
	if err := json.Unmarshal(rows[0].Context, &ctxRow); err != nil {
		t.Fatal(err)
	}
	if ctxRow.TimerID != "tmr-mama" || ctxRow.FireAt != i46Mama.UTC().Format(time.RFC3339) || ctxRow.Position != 2 || ctxRow.Listed != 2 || ctxRow.Surface != "ui" {
		t.Errorf("row context = %+v, want tmr-mama at its instant, position 2 of 2, surface ui", ctxRow)
	}
	if !strings.Contains(rows[0].Rationale, "from the UI") {
		t.Errorf("rationale = %q, want it to name the UI", rows[0].Rationale)
	}
	if sigs, _ := v.signals.Since(context.Background(), time.Time{}, 10); len(sigs) != 0 {
		t.Errorf("a timer cancel emitted signals %+v, want none (ADR-0052)", sigs)
	}
	if n := v.units.Count(); n != 0 {
		t.Errorf("a timer cancel created %d unit(s), want none (I04)", n)
	}
}

// TestI46_AUICancelOfATimerNoLongerSetSaysSoAndLogsNothing: a timer that rang
// or was cancelled after the page was drawn, or was never there, is answered
// as no longer set, and nothing is logged as cancelled.
func TestI46_AUICancelOfATimerNoLongerSetSaysSoAndLogsNothing(t *testing.T) {
	for name, id := range map[string]string{"cancelled": "tmr-gone", "fired": "tmr-pizza", "unknown": "tmr-none"} {
		t.Run(name, func(t *testing.T) {
			v := newUITimersVault(t)
			if name == "fired" {
				if err := v.timers.Fire(context.Background(), "tmr-pizza", i46Pizza, nil); err != nil {
					t.Fatal(err)
				}
			}
			rec := v.do(t, http.MethodPost, "/ui/timers/"+id+"/cancel")
			if rec.Code != http.StatusConflict {
				t.Errorf("cancel of %s = %d, want 409", id, rec.Code)
			}
			if body := rec.Body.String(); !strings.Contains(body, "That timer is no longer set") {
				t.Errorf("the answer should say the timer is no longer set:\n%s", body)
			}
			if rows := i40Actions(t, v.log); len(rows) != 0 {
				t.Errorf("decision rows = %+v, want none", rows)
			}
		})
	}
}

// TestI46_AChatCancelNamesItsSurface: the chat's row carries the same shape,
// surface chat, so the two surfaces stay one transition with one record.
func TestI46_AChatCancelNamesItsSurface(t *testing.T) {
	f := newI46(t, i46Reading("correction", "cancel", "1"), i46Two, nil)
	f.capture(t, "cancelá el del horno")

	rows := f.rows(t, ports.ActionCaptureTimerCancelled)
	if len(rows) != 1 || !strings.Contains(string(rows[0].Context), `"surface":"chat"`) {
		t.Fatalf("capture.timer.cancelled rows = %+v, want one naming surface chat", rows)
	}
}
