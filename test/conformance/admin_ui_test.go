// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
	"github.com/rengo/nooma/test/support/memrepo"
)

// postAdmin submits one setting through the real handler stack.
func postAdmin(t *testing.T, h http.Handler, field, value string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/ui/admin", strings.NewReader(url.Values{"field": {field}, "value": {value}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestAdmin_MarginWriteReachesNextToday is R10 end to end (C13), a regression
// pin with no production branch of its own: margin 0.5 holds A against a
// challenger 10% above it; posting margin 0 to /ui/admin lets the next Today
// request show the challenger, through the same keeper, so the incumbent was
// not reset by the write.
func TestAdmin_MarginWriteReachesNextToday(t *testing.T) {
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	v := newHysteresisVault(t, now)
	svc := brain.NewAdminService(fixedClock{now: now}, &counterIDs{}, v.cfg, v.rels, memrepo.NewDecisionLog())
	h := httpapi.Handler(httpapi.Deps{Version: "test", UI: ui.New(ui.Deps{Admin: svc})})

	if rec := postAdmin(t, h, "hysteresis_margin", "0.5"); rec.Code != http.StatusOK {
		t.Fatalf("POST margin 0.5 = %d: %s", rec.Code, rec.Body.String())
	}
	keeper := v.keeper()
	assertTaskFocus(t, "seeding request", v.taskFocus(t, keeper), "A")

	v.setWeight(t, "B", 1.1)
	assertTaskFocus(t, "challenger 10% above, margin 0.5", v.taskFocus(t, keeper), "A")

	if rec := postAdmin(t, h, "hysteresis_margin", "0"); rec.Code != http.StatusOK {
		t.Fatalf("POST margin 0 = %d: %s", rec.Code, rec.Body.String())
	}
	assertTaskFocus(t, "the next Today after margin 0", v.taskFocus(t, keeper), "B")
}

// TestAdminPost_RangeErrorIs400AndStoresNothing is C20: the real service over
// a vault whose every column holds a distinct non-default value refuses an
// out-of-range value with a 400, the form comes back with the message and the
// submitted text, and neither the config nor the log moved.
func TestAdminPost_RangeErrorIs400AndStoresNothing(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	cfg := memrepo.NewConfig()
	log := memrepo.NewDecisionLog()
	if err := cfg.SetWeightThreshold(ctx, 0.7, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := cfg.SetHysteresisMargin(ctx, 0.3, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := cfg.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	svc := brain.NewAdminService(fixedClock{now: now}, &counterIDs{}, cfg, memrepo.NewRelations(), log)
	h := httpapi.Handler(httpapi.Deps{Version: "test", UI: ui.New(ui.Deps{Admin: svc})})

	rec := postAdmin(t, h, "weight_threshold", "2.5")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	for _, want := range []string{"enter a number from 0 to 2", `name="value" value="2.5"`, `data-outcome="invalid"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body lacks %q:\n%s", want, rec.Body.String())
		}
	}
	after, err := cfg.Load(ctx)
	if err != nil || *after.WeightThreshold != *before.WeightThreshold || *after.HysteresisMargin != *before.HysteresisMargin {
		t.Errorf("config changed on a refused value: before %+v, after %+v, %v", before, after, err)
	}
	if rows, err := log.Before(ctx, nil, "config.", 10); err != nil || len(rows) != 0 {
		t.Errorf("config.updated rows = %d, %v; want none", len(rows), err)
	}
}

// TestAdmin_ASavedChangeAppearsInActivity: the row Update writes is the row
// /ui/activity lists, with the setting changed and both values.
func TestAdmin_ASavedChangeAppearsInActivity(t *testing.T) {
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	log := memrepo.NewDecisionLog()
	svc := brain.NewAdminService(fixedClock{now: now}, &counterIDs{}, memrepo.NewConfig(), memrepo.NewRelations(), log)
	h := httpapi.Handler(httpapi.Deps{Version: "test", UI: ui.New(ui.Deps{Admin: svc, Activity: brain.NewActivityService(log)})})

	if rec := postAdmin(t, h, "goal_stagnation_days", "28"); rec.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body.String())
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/activity", nil))
	for _, want := range []string{string(ports.ActionConfigUpdated), `data-field="goal_stagnation_days"`, `data-previous="21"`, `data-next="28"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("activity lacks %q:\n%s", want, rec.Body.String())
		}
	}
}
