// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/ui"
)

// uiCrossOriginCookieName mirrors internal/httpapi/cookie.go's own unexported
// uiCookieName — this package cannot import it, and ADR-0028 fixes the name,
// so restating it here is restating a fixed fact, not duplicating logic.
const uiCrossOriginCookieName = "nooma_token"

// countingCapturer is this file's own Capturer stub: it counts every call
// and answers a fixed, harmless brain.CaptureResult — its own content is
// irrelevant here, only whether the target handler was reached at all.
type countingCapturer struct {
	calls *int
}

func (c countingCapturer) Capture(context.Context, brain.CaptureInput) (brain.CaptureResult, error) {
	*c.calls++
	return brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "unit-1"}, nil
}

// TestUINonGETLeavesRefuseCrossOrigin is design §3.6 and spec R6's own
// structural proof against wantUIMuxWiring's non-GET rows themselves
// (httpapi_ui_wiring_test.go) — the same table TestUIMuxWiringMatchesDeclaredGuardTable
// pins the wiring for: a foreign-origin POST is refused before the target
// handler ever runs (zero Capturer calls), and a same-origin POST with the
// right cookie reaches it exactly once. POST /ui/login is covered by the
// same loop for its own cross-origin refusal (it is also a non-GET leaf
// inside the same /ui subtree, so the same http.CrossOriginProtection wrap
// applies to it too) but carries no Capturer counter of its own — it never
// reaches Capture — so its same-origin, right-cookie case is left to
// internal/httpapi/server_test.go's own login tests, not re-driven here.
//
// A future non-GET row added to wantUIMuxWiring that this file has no
// bespoke handling for still gets the baseline cross-origin-refusal check
// below, by construction — the loop iterates the table itself, not a
// hand-picked subset of it.
func TestUINonGETLeavesRefuseCrossOrigin(t *testing.T) {
	t.Parallel()

	const token = "cross-origin-probe-token"
	cookie := &http.Cookie{Name: uiCrossOriginCookieName, Value: base64.RawURLEncoding.EncodeToString([]byte(token))}

	found := 0
	for _, row := range wantUIMuxWiring {
		row := row
		if !strings.HasPrefix(row.pattern, "POST ") {
			continue
		}
		found++

		t.Run(row.pattern, func(t *testing.T) {
			t.Parallel()

			path := strings.Replace(strings.TrimPrefix(row.pattern, "POST "), "{id}", "unit-1", 1)

			var calls int
			build := func() http.Handler {
				return httpapi.Handler(httpapi.Deps{
					Version: "test",
					Token:   token,
					UI:      ui.New(ui.Deps{Capture: countingCapturer{calls: &calls}}),
				})
			}

			t.Run("foreign Sec-Fetch-Site refuses before the target runs", func(t *testing.T) {
				calls = 0
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("text=hello"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Sec-Fetch-Site", "cross-site")
				req.AddCookie(cookie)
				rec := httptest.NewRecorder()
				build().ServeHTTP(rec, req)

				if rec.Code != http.StatusForbidden {
					t.Errorf("%s (Sec-Fetch-Site: cross-site) = %d, want 403", row.pattern, rec.Code)
				}
				if calls != 0 {
					t.Errorf("%s (Sec-Fetch-Site: cross-site) reached the target %d time(s), want 0", row.pattern, calls)
				}
			})

			t.Run("foreign Origin with no Sec-Fetch-Site refuses before the target runs", func(t *testing.T) {
				calls = 0
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("text=hello"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", "http://evil.example")
				req.AddCookie(cookie)
				rec := httptest.NewRecorder()
				build().ServeHTTP(rec, req)

				if rec.Code != http.StatusForbidden {
					t.Errorf("%s (foreign Origin) = %d, want 403", row.pattern, rec.Code)
				}
				if calls != 0 {
					t.Errorf("%s (foreign Origin) reached the target %d time(s), want 0", row.pattern, calls)
				}
			})

			if row.pattern == "POST /ui/login" {
				return
			}

			t.Run("same-origin with the right cookie reaches the target exactly once", func(t *testing.T) {
				calls = 0
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("text=hello"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(cookie)
				rec := httptest.NewRecorder()
				build().ServeHTTP(rec, req)

				if calls != 1 {
					t.Errorf("%s (same-origin, right cookie) reached the target %d time(s), want exactly 1 (status %d, body %q)", row.pattern, calls, rec.Code, rec.Body.String())
				}
			})
		})
	}

	if found == 0 {
		t.Fatal("wantUIMuxWiring has no non-GET row — this gate's own guard: nothing to check")
	}
}
