package ui_test

import (
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ui"
)

// ADR-0018 makes the tokens layer the only place a raw value is written:
// every other layer reads a custom property. Review alone was the only check
// on that, so this test is the gate. The one exception is a media query
// condition, which cannot read a custom property, and it may use only the
// breakpoints app.css documents.

var (
	cssComment     = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssMediaQuery  = regexp.MustCompile(`@media([^{]*)\{`)
	cssDeclaration = regexp.MustCompile(`([\w-]+)\s*:\s*([^;{}]+)[;}]`)
	cssHexColour   = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	cssColourFunc  = regexp.MustCompile(`(?i)(?:^|[^\w-])(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix|light-dark)\(`)
	cssDimension   = regexp.MustCompile(`(?:^|[^\w-])(\d*\.?\d+)([a-zA-Z]+)\b`)
	cssWord        = regexp.MustCompile(`[A-Za-z-][\w-]*`)
)

// cssBreakpoints are the media query lengths app.css documents.
var cssBreakpoints = map[string]bool{"30rem": true, "40rem": true}

// cssNamedColours is CSS Color 4's named colours. currentColor, transparent
// and the keywords inherit, initial and unset are not raw values.
var cssNamedColours = map[string]bool{
	"aliceblue": true, "antiquewhite": true, "aqua": true, "aquamarine": true, "azure": true,
	"beige": true, "bisque": true, "black": true, "blanchedalmond": true, "blue": true,
	"blueviolet": true, "brown": true, "burlywood": true, "cadetblue": true, "chartreuse": true,
	"chocolate": true, "coral": true, "cornflowerblue": true, "cornsilk": true, "crimson": true,
	"cyan": true, "darkblue": true, "darkcyan": true, "darkgoldenrod": true, "darkgray": true,
	"darkgreen": true, "darkgrey": true, "darkkhaki": true, "darkmagenta": true,
	"darkolivegreen": true, "darkorange": true, "darkorchid": true, "darkred": true,
	"darksalmon": true, "darkseagreen": true, "darkslateblue": true, "darkslategray": true,
	"darkslategrey": true, "darkturquoise": true, "darkviolet": true, "deeppink": true,
	"deepskyblue": true, "dimgray": true, "dimgrey": true, "dodgerblue": true, "firebrick": true,
	"floralwhite": true, "forestgreen": true, "fuchsia": true, "gainsboro": true,
	"ghostwhite": true, "gold": true, "goldenrod": true, "gray": true, "green": true,
	"greenyellow": true, "grey": true, "honeydew": true, "hotpink": true, "indianred": true,
	"indigo": true, "ivory": true, "khaki": true, "lavender": true, "lavenderblush": true,
	"lawngreen": true, "lemonchiffon": true, "lightblue": true, "lightcoral": true,
	"lightcyan": true, "lightgoldenrodyellow": true, "lightgray": true, "lightgreen": true,
	"lightgrey": true, "lightpink": true, "lightsalmon": true, "lightseagreen": true,
	"lightskyblue": true, "lightslategray": true, "lightslategrey": true, "lightsteelblue": true,
	"lightyellow": true, "lime": true, "limegreen": true, "linen": true, "magenta": true,
	"maroon": true, "mediumaquamarine": true, "mediumblue": true, "mediumorchid": true,
	"mediumpurple": true, "mediumseagreen": true, "mediumslateblue": true,
	"mediumspringgreen": true, "mediumturquoise": true, "mediumvioletred": true,
	"midnightblue": true, "mintcream": true, "mistyrose": true, "moccasin": true,
	"navajowhite": true, "navy": true, "oldlace": true, "olive": true, "olivedrab": true,
	"orange": true, "orangered": true, "orchid": true, "palegoldenrod": true, "palegreen": true,
	"paleturquoise": true, "palevioletred": true, "papayawhip": true, "peachpuff": true,
	"peru": true, "pink": true, "plum": true, "powderblue": true, "purple": true,
	"rebeccapurple": true, "red": true, "rosybrown": true, "royalblue": true, "saddlebrown": true,
	"salmon": true, "sandybrown": true, "seagreen": true, "seashell": true, "sienna": true,
	"silver": true, "skyblue": true, "slateblue": true, "slategray": true, "slategrey": true,
	"snow": true, "springgreen": true, "steelblue": true, "tan": true, "teal": true,
	"thistle": true, "tomato": true, "turquoise": true, "violet": true, "wheat": true,
	"white": true, "whitesmoke": true, "yellow": true, "yellowgreen": true,
}

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

	// A media condition may hold a documented breakpoint and nothing else
	// raw; it is then dropped, so a one-line rule after it is still read.
	for _, m := range cssMediaQuery.FindAllStringSubmatch(rest, -1) {
		for _, d := range cssDimension.FindAllStringSubmatch(m[1], -1) {
			if !cssBreakpoints[d[1]+d[2]] {
				t.Errorf("media query %q uses %s, not a documented breakpoint", strings.TrimSpace(m[1]), d[1]+d[2])
			}
		}
	}
	rest = cssMediaQuery.ReplaceAllString(rest, "@media {")

	for _, d := range cssDeclaration.FindAllStringSubmatch(rest, -1) {
		prop, value := d[1], strings.TrimSpace(d[2])
		if strings.HasPrefix(prop, "--") {
			t.Errorf("custom property %s is defined outside the tokens layer", prop)
		}
		if m := cssHexColour.FindString(value); m != "" {
			t.Errorf("%s: raw colour %s outside the tokens layer", prop, m)
		}
		if m := cssColourFunc.FindStringSubmatch(value); m != nil {
			t.Errorf("%s: colour function %s() outside the tokens layer", prop, m[1])
		}
		for _, w := range cssWord.FindAllString(value, -1) {
			if cssNamedColours[strings.ToLower(w)] {
				t.Errorf("%s: named colour %q outside the tokens layer", prop, w)
			}
		}
		for _, m := range cssDimension.FindAllStringSubmatch(value, -1) {
			if m[2] == "fr" {
				continue // a grid track's share of free space, not a length
			}
			t.Errorf("%s: raw length or duration %s outside the tokens layer", prop, m[1]+m[2])
		}
	}
}

// A result region the views announce through (aria-live) must stay in the
// accessibility tree while empty: display:none or visibility:hidden on it
// could keep the outcome from being read when it arrives.
func TestStylesheet_ResultRegionsAreNeverHidden(t *testing.T) {
	css := readStylesheet(t)
	rule := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	for _, m := range rule.FindAllStringSubmatch(css, -1) {
		selector, body := m[1], strings.ReplaceAll(m[2], " ", "")
		if !strings.Contains(selector, "-result") {
			continue
		}
		if strings.Contains(body, "display:none") || strings.Contains(body, "visibility:hidden") {
			t.Errorf("result region hidden by %q { %s }", strings.TrimSpace(selector), strings.TrimSpace(m[2]))
		}
	}
}

// The tokens' contrast claims, checked in both themes: text roles hold 4.5:1
// and control borders 3:1 against the page and card backgrounds.
func TestStylesheet_ColourRolesHoldTheirContrast(t *testing.T) {
	css := readStylesheet(t)
	role := regexp.MustCompile(`(--color-[\w-]+):\s*light-dark\((#[0-9a-fA-F]{6}),\s*(#[0-9a-fA-F]{6})\)`)
	roles := map[string][2]string{}
	for _, m := range role.FindAllStringSubmatch(css, -1) {
		roles[m[1]] = [2]string{m[2], m[3]}
	}
	for _, c := range []struct {
		fg  string
		min float64
	}{
		{"--color-fg", 4.5}, {"--color-muted", 4.5}, {"--color-accent", 4.5},
		{"--color-border-strong", 3},
	} {
		for _, bg := range []string{"--color-surface", "--color-bg"} {
			f, okF := roles[c.fg]
			b, okB := roles[bg]
			if !okF || !okB {
				t.Fatalf("%s or %s is not a light-dark() hex pair", c.fg, bg)
			}
			for theme, i := range map[string]int{"light": 0, "dark": 1} {
				if got := contrast(f[i], b[i]); got < c.min {
					t.Errorf("%s on %s (%s) is %.2f:1, want at least %.1f:1", c.fg, bg, theme, got, c.min)
				}
			}
		}
	}
}

// contrast is WCAG 2's contrast ratio between two #rrggbb colours.
func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(hex string) float64 {
	var rgb [3]float64
	for i := range rgb {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c := float64(v) / 255
		if c <= 0.03928 {
			rgb[i] = c / 12.92
		} else {
			rgb[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*rgb[0] + 0.7152*rgb[1] + 0.0722*rgb[2]
}
