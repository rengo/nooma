package ui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ui"
)

// doGet runs one GET against h without opening a real socket.
func doGet(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestAssetsServesTheThreeEmbeddedLeaves pins Assets() itself, not
// newUIMux's routing over it: internal/httpapi's TestUIStaticServesStylesheetAndHtmx
// drives requests through Handler(d) and newUIMux, whose three exact leaf
// patterns (GET /ui/static/app.css, /htmx.min.js, /htmx.LICENSE) never route
// a bare "/ui/static" or "/ui/static/" into Assets() at all — reverting
// Assets() to the http.StripPrefix+http.FileServerFS construction design
// m4a §3.2 corrected (the one that answers a 200 directory listing on an
// unmatched subtree) still leaves that httpapi test green, because the
// mutated handler is simply never reached through the outer mux. This test
// closes that gap by calling Assets() directly, so a regression to the
// listing construction fails here even if newUIMux's own leaves still mask
// it upstream.
func TestAssetsServesTheThreeEmbeddedLeaves(t *testing.T) {
	t.Parallel()

	h := ui.Assets()

	t.Run("app.css", func(t *testing.T) {
		t.Parallel()

		rec := doGet(h, "/ui/static/app.css")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/static/app.css = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
			t.Errorf("Content-Type = %q, want text/css", ct)
		}
	})

	t.Run("htmx.min.js", func(t *testing.T) {
		t.Parallel()

		rec := doGet(h, "/ui/static/htmx.min.js")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/static/htmx.min.js = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
			t.Errorf("Content-Type = %q, want a javascript content-type", ct)
		}
	})

	t.Run("htmx.LICENSE", func(t *testing.T) {
		t.Parallel()

		rec := doGet(h, "/ui/static/htmx.LICENSE")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/static/htmx.LICENSE = %d, want 200", rec.Code)
		}
	})

	for _, path := range []string{"/ui/static", "/ui/static/"} {
		t.Run("no listing at "+path, func(t *testing.T) {
			t.Parallel()

			rec := doGet(h, path)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s = %d, want 404 (no directory listing, no redirect)", path, rec.Code)
			}
			body := rec.Body.String()
			if strings.Contains(body, "<a href=") {
				t.Errorf("GET %s body contains an <a href=...> link — looks like a directory listing:\n%s", path, body)
			}
			if strings.Contains(body, "<pre>") {
				t.Errorf("GET %s body contains a <pre> block — looks like a directory listing:\n%s", path, body)
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("GET %s set Location: %q — want no redirect", path, loc)
			}
		})
	}

	t.Run("an unknown static path", func(t *testing.T) {
		t.Parallel()

		rec := doGet(h, "/ui/static/nope")
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /ui/static/nope = %d, want 404", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("GET /ui/static/nope set Location: %q — want no redirect", loc)
		}
	})
}
