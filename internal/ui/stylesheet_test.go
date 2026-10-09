package ui_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ui"
)

// ADR-0018 makes the tokens layer the only place a raw value is written:
// every other layer reads a custom property. Review alone was the only check
// on that, so this test is the gate. A media query condition is the one
// exception, since it cannot read a custom property.

var (
	cssComment   = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssRawColour = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\b(?:rgb|rgba|hsl|hsla|oklch)\(`)
	cssRawLength = regexp.MustCompile(`(?:^|[^\w-])(\d*\.?\d+)(px|rem|em|ch|vh|vw|dvh)\b`)
)

const cssLayerOrder = "@layer reset, tokens, base, layout, components, utilities;"

// readStylesheet reads app.css as the binary serves it: the package reads
// embedded assets, never the filesystem (depguard's ui-boundary).
func readStylesheet(t *testing.T) string {
	t.Helper()
	rec := doGet(ui.Assets(), "/ui/static/app.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/static/app.css = %d", rec.Code)
	}
	return cssComment.ReplaceAllString(rec.Body.String(), "")
}

// blockAt returns the text of the brace block opening at the first "{" at or
// after i, braces included, and the index just past it.
func blockAt(t *testing.T, css string, i int) (string, int) {
	t.Helper()
	open := strings.Index(css[i:], "{")
	if open < 0 {
		t.Fatalf("no block after offset %d", i)
	}
	depth := 0
	for j := i + open; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[i+open : j+1], j + 1
			}
		}
	}
	t.Fatal("unbalanced braces in app.css")
	return "", 0
}

func TestStylesheet_LayerOrderIsDeclaredFirst(t *testing.T) {
	css := strings.TrimSpace(readStylesheet(t))
	if !strings.HasPrefix(css, cssLayerOrder) {
		t.Errorf("app.css does not open with %q", cssLayerOrder)
	}
}

func TestStylesheet_RawValuesLiveOnlyInTokens(t *testing.T) {
	css := readStylesheet(t)
	start := strings.Index(css, "@layer tokens")
	if start < 0 {
		t.Fatal("app.css has no tokens layer")
	}
	tokens, end := blockAt(t, css, start)
	if !strings.Contains(tokens, "--color-") || !strings.Contains(tokens, "--space-") {
		t.Fatal("the tokens layer holds no colour or spacing tokens")
	}
	rest := css[:start] + css[end:]

	for n, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@media") {
			continue
		}
		if m := cssRawColour.FindString(trimmed); m != "" {
			t.Errorf("raw colour %q outside the tokens layer: %q (line %d after removing tokens)", m, trimmed, n+1)
		}
		if m := cssRawLength.FindStringSubmatch(trimmed); m != nil {
			t.Errorf("raw length %q outside the tokens layer: %q (line %d after removing tokens)", m[1]+m[2], trimmed, n+1)
		}
	}
}
