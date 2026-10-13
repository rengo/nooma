package ui_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ui"
)

// The forms that wait on the model show a pending state: htmx puts
// .htmx-request on the form while its request is in flight, hx-disabled-elt
// disables the submit button, and hx-sync drops a second submit. The
// stylesheet turns the idle label into the pending one. These tests pin the
// markup contract; the timing itself is the browser's.

func captureFormRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/ui/capture", nil)
	req.Pattern = "GET /ui/capture"
	return req
}

func assertPendingForm(t *testing.T, form, idle, pending string) {
	t.Helper()
	for _, want := range []string{
		`hx-disabled-elt="find button[type=submit]"`,
		`hx-sync="this:drop"`,
		`<span data-idle>` + idle + `</span>`,
		`<span data-pending aria-hidden="true">` + pending + `</span>`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("form lacks %q:\n%s", want, form)
		}
	}
}

func TestCapturePage_FormShowsAPendingState(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Capture: &fakeCapturer{}}).ServeHTTP(rec, captureFormRequest())
	assertPendingForm(t, between(t, rec.Body.String(), `<form method="post" action="/ui/capture"`, "</form>"), "Capture", "Capturing…")
}

func TestUnitPage_CorrectionFormShowsAPendingState(t *testing.T) {
	t.Parallel()
	detail := brain.UnitDetail{Unit: unit.Unit{ID: "unit-1", Type: unit.TypeTask, Content: "Pay rent", CreatedAt: time.Now()}}
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Units: stubUnitsReader{detail: detail, detailFound: true}}).ServeHTTP(rec, unitRequest("unit-1"))
	form := between(t, rec.Body.String(), `action="/ui/units/unit-1/correct"`, "</form>")
	assertPendingForm(t, form, "Submit correction", "Saving…")
}

// A boosted submit swaps the whole answer into the page, so the answer to a
// correction that cannot read its unit back must be a page, not the bare
// capture fragment an HX-Request normally gets.
func TestCorrectView_BoostedFallbackIsAFullPage(t *testing.T) {
	t.Parallel()
	fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeCorrected, Correction: &brain.Correction{UnitID: "unit-9"}}}
	req := correctRequest("unit-9", "text=x")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Capture: fake, Units: stubUnitsReader{detailFound: false}}).ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "<nav") || !strings.Contains(body, `data-outcome="corrected"`) {
		t.Errorf("boosted fallback is not a full page carrying the outcome:\n%s", body)
	}
}

// Swapped content must not re-run the page's own htmx <script>.
func TestLayout_BoostedSwapDoesNotRerunScripts(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Capture: &fakeCapturer{}}).ServeHTTP(rec, captureFormRequest())
	if !strings.Contains(rec.Body.String(), `&#34;allowScriptTags&#34;:false`) {
		t.Errorf("htmx-config does not disable allowScriptTags:\n%s", rec.Body.String())
	}
}

// cssRuleBody returns the declarations of the rule whose selector list
// contains exactly selector, so a renamed selector or a deleted declaration
// is found missing rather than matched by a stray substring.
func cssRuleBody(t *testing.T, css, selector string) string {
	t.Helper()
	rule := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	for _, m := range rule.FindAllStringSubmatch(css, -1) {
		for _, sel := range strings.Split(m[1], ",") {
			if strings.TrimSpace(sel) == selector {
				return strings.Join(strings.Fields(m[2]), "")
			}
		}
	}
	t.Fatalf("app.css has no rule for %q", selector)
	return ""
}

func TestStylesheet_PendingStateSwapsIdleLabelForPendingOne(t *testing.T) {
	t.Parallel()
	css := readStylesheet(t)
	for selector, want := range map[string]string{
		"[data-pending]":                       "display:none",
		".htmx-request [data-idle]":            "display:none",
		".htmx-request [data-pending]":         "display:inline-flex",
		".htmx-request [data-pending]::before": "animation:spinvar(--spinner-duration)linearinfinite",
		"button:disabled":                      "cursor:progress",
	} {
		if body := cssRuleBody(t, css, selector); !strings.Contains(body, want) {
			t.Errorf("rule %q lacks %q: %s", selector, want, body)
		}
	}
}

// The browser must not stay on the boosted POST's URL: reloading it is a 405.
func TestCorrectView_BoostedAnswerReplacesTheURL(t *testing.T) {
	t.Parallel()
	detail := brain.UnitDetail{Unit: unit.Unit{ID: "unit-1", Type: unit.TypeTask, Content: "Pay rent", CreatedAt: time.Now()}}
	fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeCorrected, Correction: &brain.Correction{UnitID: "unit-1"}}}
	req := correctRequest("unit-1", "text=x")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Capture: fake, Units: stubUnitsReader{detail: detail, detailFound: true}}).ServeHTTP(rec, req)
	if got := rec.Header().Get("HX-Replace-Url"); got != "/ui/units/unit-1" {
		t.Errorf("HX-Replace-Url = %q, want /ui/units/unit-1", got)
	}

	req = correctRequest("unit-9", "text=x")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")
	rec = httptest.NewRecorder()
	ui.New(ui.Deps{Capture: fake, Units: stubUnitsReader{detailFound: false}}).ServeHTTP(rec, req)
	if got := rec.Header().Get("HX-Replace-Url"); got != "/ui/capture" {
		t.Errorf("fallback HX-Replace-Url = %q, want /ui/capture", got)
	}
}
