package ui_test

import (
	"context"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ui"
)

// htmxConfigMetaRe extracts the htmx-config meta tag's own content
// attribute — the CSP-compatible JSON layout.templ writes in Page's <head>
// (design m4a §3.5).
var htmxConfigMetaRe = regexp.MustCompile(`<meta name="htmx-config" content="([^"]*)"`)

// responseHandlingEntry mirrors one element of htmx's own responseHandling
// array: code is a regular expression tested, unanchored, against the
// status code's decimal string.
type responseHandlingEntry struct {
	Code string `json:"code"`
	Swap bool   `json:"swap"`
}

// TestLayout_HTMXConfigSwapsErrorResponses is design §3.5/OR7, made
// executable: without responseHandling, htmx's own default only swaps
// 2xx/3xx, so a capture form's 400/503/500 fragment (serveCapture,
// serveCorrect) would never reach its target at all. This renders a real
// page (Today, GET /ui's own view), extracts the htmx-config meta's JSON,
// and asserts some responseHandling entry swaps a representative 4xx and a
// representative 5xx status.
func TestLayout_HTMXConfigSwapsErrorResponses(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	if err := ui.Today(brain.Today{}, ui.Serving{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Today.Render: %v", err)
	}
	page := buf.String()

	m := htmxConfigMetaRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("page carries no htmx-config meta tag:\n%s", page)
	}

	var cfg struct {
		ResponseHandling []responseHandlingEntry `json:"responseHandling"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &cfg); err != nil {
		t.Fatalf("htmx-config content is not valid JSON: %v\ncontent: %s", err, m[1])
	}
	if len(cfg.ResponseHandling) == 0 {
		t.Fatal("htmx-config carries no responseHandling entries — a 4xx/5xx fragment would be dropped silently (OR7)")
	}

	for _, code := range []string{"400", "500", "503"} {
		if !htmxSwaps(cfg.ResponseHandling, code) {
			t.Errorf("htmx would not swap status %s — the first matching responseHandling entry (or none) drops that fragment silently (OR7)", code)
		}
	}
}

// htmxSwaps reports whether htmx would swap a response with this status,
// using the vendored htmx.min.js's own rule: the first entry whose code
// matches (new RegExp(code).test(status), unanchored) decides, and no match
// means no swap.
func htmxSwaps(entries []responseHandlingEntry, code string) bool {
	for _, e := range entries {
		re, err := regexp.Compile(e.Code)
		if err != nil {
			continue
		}
		if re.MatchString(code) {
			return e.Swap
		}
	}
	return false
}

// The nav marks the page being shown, and only that one; the unit detail
// page belongs to Units.
func TestLayout_NavMarksTheCurrentPage(t *testing.T) {
	t.Parallel()
	for title, want := range map[string]string{
		"nooma":            "/ui",
		"nooma — units":    "/ui/units",
		"nooma — unit":     "/ui/units",
		"nooma — activity": "/ui/activity",
	} {
		var buf strings.Builder
		if err := ui.Page(title, templ.NopComponent).Render(context.Background(), &buf); err != nil {
			t.Fatalf("Render: %v", err)
		}
		page := buf.String()
		if n := strings.Count(page, `aria-current="page"`); n != 1 {
			t.Errorf("%q: %d links marked current, want 1", title, n)
		}
		if !strings.Contains(page, `<a href="`+want+`" aria-current="page">`) {
			t.Errorf("%q: %s is not the current link:\n%s", title, want, page)
		}
	}
}
