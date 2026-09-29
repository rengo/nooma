package ui_test

import (
	"context"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ui"
)

// htmxConfigMetaRe extracts the htmx-config meta tag's own content
// attribute — the CSP-compatible JSON layout.templ writes in Page's <head>
// (design m4a §3.5).
var htmxConfigMetaRe = regexp.MustCompile(`<meta name="htmx-config" content="([^"]*)"`)

// responseHandlingEntry mirrors one element of htmx's own responseHandling
// array: code is a glob-style status pattern where "." matches any single
// digit — which is also valid regexp syntax, so a literal entry can be
// compiled and matched against a status code directly, as this test does.
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
		if !anyEntrySwaps(cfg.ResponseHandling, code) {
			t.Errorf("no responseHandling entry swaps status %s — that fragment would be dropped silently (OR7)", code)
		}
	}
}

// anyEntrySwaps reports whether some responseHandling entry both matches
// code and swaps it.
func anyEntrySwaps(entries []responseHandlingEntry, code string) bool {
	for _, e := range entries {
		re, err := regexp.Compile("^" + e.Code + "$")
		if err != nil {
			continue
		}
		if re.MatchString(code) && e.Swap {
			return true
		}
	}
	return false
}
