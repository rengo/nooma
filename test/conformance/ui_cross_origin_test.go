// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sort"
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

// countingBeliefs is this file's own ui.Beliefs stub, sharing the counter
// countingCapturer uses: Edit and Retire, the two mutating entrances, each
// count one call; ByFacet is a read and counts nothing.
type countingBeliefs struct {
	calls *int
}

func (countingBeliefs) ByFacet(context.Context) ([]brain.FacetBeliefs, error) { return nil, nil }

func (c countingBeliefs) Edit(context.Context, string, string) error {
	*c.calls++
	return nil
}

func (c countingBeliefs) Retire(context.Context, string) error {
	*c.calls++
	return nil
}

// uiCrossOriginBodies maps every non-GET route pattern to a form body that
// passes THAT handler's parsing (m4e design 3.12 G6, spec R9). A fixed body
// shared by every route would make a new route's same-origin case fail on a
// parse error instead of proving the guard: "text=hello" is the capture and
// correction forms' field, "content=new+text" the belief edit's, and the
// belief retire reads no body at all.
var uiCrossOriginBodies = map[string]string{
	"POST /ui/capture":             "text=hello",
	"POST /ui/units/{id}/correct":  "text=hello",
	"POST /ui/beliefs/{id}/edit":   "content=new+text",
	"POST /ui/beliefs/{id}/retire": "",
}

// uiCrossOriginBodyExempt lists the rows that have no valid-body entry, each
// with its reason. POST /ui/login mints the cookie, so the same-origin-with-
// cookie subtest cannot apply to it; its two refusal subtests send
// uiCrossOriginPlaceholderBody, and only the same-origin subtest is skipped.
var uiCrossOriginBodyExempt = map[string]string{
	"POST /ui/login": "unguarded by design: it mints the cookie, so the same-origin-with-cookie subtest cannot apply; the row keeps a fixed placeholder body for the two refusal subtests, and only the same-origin subtest is skipped (wantUIMuxWiring has it as guarded:false)",
}

// uiCrossOriginPlaceholderBody is what a refusal subtest sends for an
// exempt row: the refusal happens before any parsing, so any body will do.
const uiCrossOriginPlaceholderBody = "text=hello"

// uiCrossOriginBodyFor returns the body a subtest posts to pattern.
func uiCrossOriginBodyFor(pattern string) string {
	if body, ok := uiCrossOriginBodies[pattern]; ok {
		return body
	}
	return uiCrossOriginPlaceholderBody
}

// uiNonGETPatterns returns every non-GET pattern wantUIMuxWiring declares.
func uiNonGETPatterns() []string {
	var rows []string
	for _, row := range wantUIMuxWiring {
		if !strings.HasPrefix(row.pattern, "GET ") {
			rows = append(rows, row.pattern)
		}
	}
	return rows
}

// uiCrossOriginBodyViolations is the coverage rule as a pure function, so a
// test can feed it a broken table and watch it object: a row is covered by
// exactly one of the body table or the exemption map; a body entry with no
// row is stale; an exemption needs a reason and a row.
func uiCrossOriginBodyViolations(rows []string, bodies, exempt map[string]string) []string {
	isRow := make(map[string]bool, len(rows))
	var out []string
	for _, pattern := range rows {
		isRow[pattern] = true
		_, inBodies := bodies[pattern]
		reason, inExempt := exempt[pattern]
		switch {
		case inBodies && inExempt:
			out = append(out, pattern+": in both the body table and the exemption map")
		case !inBodies && !inExempt:
			out = append(out, pattern+": has neither a body entry nor an exemption")
		case inExempt && strings.TrimSpace(reason) == "":
			out = append(out, pattern+": exemption with an empty reason")
		}
	}
	for pattern := range bodies {
		if !isRow[pattern] {
			out = append(out, pattern+": stale body entry, no such row in wantUIMuxWiring")
		}
	}
	for pattern := range exempt {
		if !isRow[pattern] {
			out = append(out, pattern+": exemption with no row in wantUIMuxWiring")
		}
	}
	sort.Strings(out)
	return out
}

// TestUICrossOriginBodiesCoverEveryPOSTRow fails when a non-GET row of
// wantUIMuxWiring has neither a valid-body entry nor an exemption, and when a
// table names a row that does not exist (U4, U4b).
func TestUICrossOriginBodiesCoverEveryPOSTRow(t *testing.T) {
	t.Parallel()

	rows := uiNonGETPatterns()
	if len(rows) == 0 {
		t.Fatal("wantUIMuxWiring has no non-GET row — this gate's own guard: nothing to check")
	}
	for _, v := range uiCrossOriginBodyViolations(rows, uiCrossOriginBodies, uiCrossOriginBodyExempt) {
		t.Error(v)
	}
}

// TestUICrossOriginBodyCoverageRuleFiresOnEachViolation feeds the rule a
// broken table per violation class, so the gate above is shown to fire and
// not merely to be quiet.
func TestUICrossOriginBodyCoverageRuleFiresOnEachViolation(t *testing.T) {
	t.Parallel()

	rows := []string{"POST /a", "POST /b"}
	cases := []struct {
		name   string
		bodies map[string]string
		exempt map[string]string
		want   string
	}{
		{"a clean table", map[string]string{"POST /a": "x=1"}, map[string]string{"POST /b": "reason"}, ""},
		{"a row with no entry", map[string]string{"POST /a": "x=1"}, nil, "POST /b: has neither"},
		{"a stale entry", map[string]string{"POST /a": "x=1", "POST /b": "", "POST /gone": ""}, nil, "POST /gone: stale body entry"},
		{"an exemption with an empty reason", map[string]string{"POST /a": "x=1"}, map[string]string{"POST /b": " "}, "POST /b: exemption with an empty reason"},
		{"an exemption with no row", map[string]string{"POST /a": "x=1", "POST /b": ""}, map[string]string{"POST /gone": "reason"}, "POST /gone: exemption with no row"},
		{"a pattern in both", map[string]string{"POST /a": "x=1", "POST /b": ""}, map[string]string{"POST /b": "reason"}, "POST /b: in both"},
	}
	for _, tc := range cases {
		got := strings.Join(uiCrossOriginBodyViolations(rows, tc.bodies, tc.exempt), "\n")
		if tc.want == "" {
			if got != "" {
				t.Errorf("%s: violations = %q, want none", tc.name, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: violations = %q, want one containing %q", tc.name, got, tc.want)
		}
	}
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
		if strings.HasPrefix(row.pattern, "GET ") {
			continue
		}
		found++

		t.Run(row.pattern, func(t *testing.T) {
			t.Parallel()

			path := strings.Replace(strings.TrimPrefix(row.pattern, "POST "), "{id}", "unit-1", 1)

			body := uiCrossOriginBodyFor(row.pattern)

			var calls int
			build := func() http.Handler {
				return httpapi.Handler(httpapi.Deps{
					Version: "test",
					Token:   token,
					UI:      ui.New(ui.Deps{Capture: countingCapturer{calls: &calls}, Beliefs: countingBeliefs{calls: &calls}}),
				})
			}

			t.Run("foreign Sec-Fetch-Site refuses before the target runs", func(t *testing.T) {
				calls = 0
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
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
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
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

			if _, exempt := uiCrossOriginBodyExempt[row.pattern]; exempt {
				return
			}

			t.Run("same-origin with the right cookie reaches the target exactly once", func(t *testing.T) {
				calls = 0
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
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
