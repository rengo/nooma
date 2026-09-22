package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ui"
)

// wantUICSP mirrors design m4a §3.5's exact CSP string — asserted as a
// literal here, not against headers.go's own uiCSP constant, so a change to
// the policy has to be a deliberate edit to both places, not a tautology
// that always agrees with itself.
const wantUICSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'"

// assertUISecurityHeaders is TestUISubtreeSetsSecurityHeaders' and
// TestHandlerServesAPIRootAndUIShell's shared check: every response under
// /ui carries the five headers design m4a §3.5 fixes, with Cache-Control
// the one value that varies by route class (no-store on a view, no-cache
// on a static asset).
func assertUISecurityHeaders(t *testing.T, h http.Header, wantCacheControl string) {
	t.Helper()

	want := map[string]string{
		"Content-Security-Policy": wantUICSP,
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "same-origin",
		"X-Frame-Options":         "DENY",
		"Cache-Control":           wantCacheControl,
	}
	for header, w := range want {
		if got := h.Get(header); got != w {
			t.Errorf("header %s = %q, want %q", header, got, w)
		}
	}
}

// doGet runs one GET against h without opening a real socket.
func doGet(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestHandlerServesAPIRootAndUIShell is design m4a §3.1's PR 2 shell state,
// renamed from TestHandlerServesBothSurfaces (§7.2): GET / and GET /ui both
// 200; the /ui body carries the layout and PR 2's own shell paragraph —
// "Today arrives in a later PR" — never a FOCUS/PENDING DIGEST/SYSTEM
// section or any other vault-shaped content. This test's own scope is PR 2
// through PR 6 only: from PR 7, ui.New(ui.Deps{})'s own no-TodayReader
// fixture falls into a 503 arm instead, which
// TestTodayView_NilTodayReaderAnswers503 takes over asserting (§7.2, §8).
func TestHandlerServesAPIRootAndUIShell(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(Handler(Deps{Version: "test-version", UI: ui.New(ui.Deps{})}))
	t.Cleanup(srv.Close)

	rootResp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = rootResp.Body.Close() }()
	if rootResp.StatusCode != http.StatusOK {
		t.Errorf("GET / = %d, want 200", rootResp.StatusCode)
	}

	uiResp, err := http.Get(srv.URL + "/ui")
	if err != nil {
		t.Fatalf("GET /ui: %v", err)
	}
	defer func() { _ = uiResp.Body.Close() }()
	if uiResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui = %d, want 200", uiResp.StatusCode)
	}
	assertUISecurityHeaders(t, uiResp.Header, "no-store")

	body, err := io.ReadAll(uiResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)

	if !strings.Contains(page, "Today arrives in a later PR") {
		t.Errorf("GET /ui does not carry PR 2's shell paragraph:\n%s", page)
	}
	for _, section := range []string{"FOCUS", "PENDING DIGEST", "SYSTEM"} {
		if strings.Contains(page, section) {
			t.Errorf("GET /ui already carries %q — Today is PR 7's, not PR 2's:\n%s", section, page)
		}
	}
}

// TestUnknownPathIs404 keeps the UI subtree from becoming a catch-all. A mux
// that answered everything would make the first real route impossible to
// notice as missing.
func TestUnknownPathIs404(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(Handler(Deps{Version: "test-version"}))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /does-not-exist = %d, want 404", resp.StatusCode)
	}
}

// TestHandlerServesDistinctSurfaces is a small honesty check: the API
// reports what it is, the UI carries its own layout, and neither pretends
// to be the other. It exists so that when a later PR changes the UI, the
// test that breaks names the thing that changed.
func TestHandlerServesDistinctSurfaces(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(Handler(Deps{Version: "test-version", UI: ui.New(ui.Deps{})}))
	t.Cleanup(srv.Close)

	api := get(t, srv.URL+"/")
	uiBody := get(t, srv.URL+"/ui")

	if !strings.Contains(api, "test-version") {
		t.Errorf("the API response does not report the version:\n%s", api)
	}
	if api == uiBody {
		t.Error("the API and the UI return the same body; one of them is not doing its job")
	}
}

func get(t *testing.T, url string) string {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var b strings.Builder
	buf := make([]byte, 512)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

// TestGuardedRoutesRequireToken is R2.11's own completeness test: it
// iterates apiRoutes — the ONE declared route-table slice Handler also
// registers from (design D10's "one slice, two consumers" shape) — and
// asserts every entry returns 401 with no token and with a wrong token when
// a token is configured, and that both responses are byte-identical (R2.11's
// own MUST NOT against an oracle). A future PR adding a route to apiRoutes
// is guarded by construction and is covered here without this test changing.
func TestGuardedRoutesRequireToken(t *testing.T) {
	t.Parallel()

	d := Deps{Version: "test", Token: "the-real-token"}
	h := Handler(d)

	routes := apiRoutes(d)
	if len(routes) == 0 {
		t.Fatal("apiRoutes returned no routes — nothing for this completeness test to check")
	}

	for _, rt := range routes {
		t.Run(rt.pattern, func(t *testing.T) {
			t.Parallel()

			method, path, ok := strings.Cut(rt.pattern, " ")
			if !ok {
				t.Fatalf("route pattern %q has no method prefix", rt.pattern)
			}

			doRequest := func(authHeader string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, nil)
				if authHeader != "" {
					req.Header.Set("Authorization", authHeader)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				return rec
			}

			noToken := doRequest("")
			wrongToken := doRequest("Bearer not-the-token")

			if noToken.Code != http.StatusUnauthorized {
				t.Errorf("no token: status = %d, want %d", noToken.Code, http.StatusUnauthorized)
			}
			if wrongToken.Code != http.StatusUnauthorized {
				t.Errorf("wrong token: status = %d, want %d", wrongToken.Code, http.StatusUnauthorized)
			}
			if noToken.Code != wrongToken.Code || noToken.Body.String() != wrongToken.Body.String() {
				t.Errorf("route %q: missing-token and wrong-token responses differ — %d %q vs %d %q",
					rt.pattern, noToken.Code, noToken.Body.String(), wrongToken.Code, wrongToken.Body.String())
			}
		})
	}
}

// TestOpenRoutesStayOpenRegardlessOfToken is R2.12's own review checkpoint,
// made executable: GET / and GET /ui stay reachable without a token even
// when one is configured, and neither sets a cookie — ADR-0017's scope is
// the API's bearer-token header only, never the UI's cookie handshake
// (ADR-0007, PR 4a's own requireCookie inverts the /ui leg of this test).
func TestOpenRoutesStayOpenRegardlessOfToken(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test", Token: "the-real-token", UI: ui.New(ui.Deps{})})

	for _, path := range []string{"/", "/ui"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("GET %s with a token configured and no Authorization header: status = %d, want %d", path, rec.Code, http.StatusOK)
			}
			if rec.Header().Get("Set-Cookie") != "" {
				t.Errorf("GET %s set a cookie — ADR-0017's scope is the API header only, not a UI session", path)
			}
		})
	}
}

// TestUISubtreeSetsSecurityHeaders is design m4a §3.5, §8: every response
// under /ui — a rendered view, a static asset, an unmatched path, a
// cross-origin refusal — carries the five fixed headers, with the right
// Cache-Control for its route class. PR 2's tip has no cookie state yet
// (303/401 arrive with PR 4a), so this test exercises every arm PR 2's own
// mux can produce: 200 on the shell and on a static asset, 404 on an
// unmatched /ui path, 403 on a refused cross-origin POST.
func TestUISubtreeSetsSecurityHeaders(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test", UI: ui.New(ui.Deps{})})

	cases := []struct {
		name             string
		method           string
		path             string
		crossSite        bool
		wantStatus       int
		wantCacheControl string
		wantAllow        string
	}{
		{name: "the shell", method: http.MethodGet, path: "/ui", wantStatus: http.StatusOK, wantCacheControl: "no-store"},
		{name: "a static asset", method: http.MethodGet, path: "/ui/static/app.css", wantStatus: http.StatusOK, wantCacheControl: "no-cache"},
		{name: "an unmatched /ui path", method: http.MethodGet, path: "/ui/does-not-exist", wantStatus: http.StatusNotFound, wantCacheControl: "no-store"},
		{name: "a refused cross-origin POST", method: http.MethodPost, path: "/ui", crossSite: true, wantStatus: http.StatusForbidden, wantCacheControl: "no-store"},
		{name: "a same-origin POST to a GET-only leaf", method: http.MethodPost, path: "/ui", wantStatus: http.StatusMethodNotAllowed, wantCacheControl: "no-store", wantAllow: "GET, HEAD"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.crossSite {
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.wantStatus)
			}
			assertUISecurityHeaders(t, rec.Header(), tc.wantCacheControl)
			if tc.wantAllow != "" {
				if allow := rec.Header().Get("Allow"); allow != tc.wantAllow {
					t.Errorf("Allow = %q, want %q", allow, tc.wantAllow)
				}
			}
			if rec.Header().Get("Set-Cookie") != "" {
				t.Errorf("%s %s set a cookie", tc.method, tc.path)
			}
		})
	}
}

// TestUICrossOriginPostIsRefused is design m4a §3.4, §8: the whole /ui
// subtree is wrapped in http.CrossOriginProtection, constructed once with
// no trusted origins and no bypass patterns. A cross-site POST is refused
// before any handler this package writes ever runs; a same-origin POST, or
// one carrying neither Sec-Fetch-Site nor Origin, is not refused by this
// middleware (it may still answer 404/405 downstream, which this test does
// not assert); GET — a safe method — always passes regardless of the
// headers.
func TestUICrossOriginPostIsRefused(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test", UI: ui.New(ui.Deps{})})

	cases := []struct {
		name          string
		secFetchSite  string
		origin        string
		wantForbidden bool
	}{
		{name: "cross-site Sec-Fetch-Site", secFetchSite: "cross-site", wantForbidden: true},
		{name: "same-origin Sec-Fetch-Site", secFetchSite: "same-origin", wantForbidden: false},
		{name: "no Sec-Fetch-Site, foreign Origin", origin: "http://evil.example", wantForbidden: true},
		{name: "neither header", wantForbidden: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/ui", nil)
			if tc.secFetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.secFetchSite)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			forbidden := rec.Code == http.StatusForbidden
			if forbidden != tc.wantForbidden {
				t.Errorf("POST /ui (%s) = %d, want forbidden=%v", tc.name, rec.Code, tc.wantForbidden)
			}
		})
	}

	t.Run("GET is a safe method and always passes", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/ui", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Error("GET /ui with Sec-Fetch-Site: cross-site was refused; safe methods always pass")
		}
	})
}

// TestUIStaticServesStylesheetAndHtmx is design m4a §3.2, §8: each of the
// three embedded static files answers 200 with its bytes intact, and
// neither /ui/static nor /ui/static/ resolves to anything — no redirect,
// no directory listing, the {file...}+http.FileServerFS defect ui.Assets
// corrects (§3.2).
func TestUIStaticServesStylesheetAndHtmx(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test", UI: ui.New(ui.Deps{})})

	t.Run("app.css", func(t *testing.T) {
		t.Parallel()

		rec := doGet(h, "/ui/static/app.css")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/static/app.css = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
			t.Errorf("Content-Type = %q, want text/css", ct)
		}
		if !strings.Contains(rec.Body.String(), "@layer") {
			t.Error("app.css body does not declare cascade layers")
		}
	})

	t.Run("htmx.min.js", func(t *testing.T) {
		t.Parallel()

		rec := doGet(h, "/ui/static/htmx.min.js")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/static/htmx.min.js = %d, want 200", rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Error("htmx.min.js body is empty")
		}
	})

	t.Run("htmx.LICENSE", func(t *testing.T) {
		t.Parallel()

		rec := doGet(h, "/ui/static/htmx.LICENSE")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/static/htmx.LICENSE = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Zero-Clause BSD") {
			t.Error("htmx.LICENSE body does not name the license")
		}
	})

	for _, path := range []string{"/ui/static", "/ui/static/"} {
		t.Run("no listing at "+path, func(t *testing.T) {
			t.Parallel()

			rec := doGet(h, path)
			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s = %d, want 404 (no directory listing, no redirect)", path, rec.Code)
			}
		})
	}
}

// TestUIRootIsNeverRedirectedByTheMux is design m4a §3.2, §8: it inspects
// the pattern newUIMux resolves a request to, rather than the response a
// handler produces, because a legitimate 303 requireCookie answers under a
// token (PR 4a) is not the mux's own redirect. Registering "/ui/" as a
// subtree instead of the exact leaves "GET /ui" and "GET /ui/{$}" is the
// class that let GET /ui 307 to /ui/ from the mux itself, before
// requireCookie or any handler ever ran.
func TestUIRootIsNeverRedirectedByTheMux(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"", "the-real-token"} {
		t.Run("token="+token, func(t *testing.T) {
			t.Parallel()

			mux := newUIMux(Deps{Token: token, UI: ui.New(ui.Deps{})})

			for path, want := range map[string]string{
				"/ui":  "GET /ui",
				"/ui/": "GET /ui/{$}",
			} {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				_, pattern := mux.Handler(req)
				if pattern != want {
					t.Errorf("newUIMux resolves %s to pattern %q, want the exact leaf %q", path, pattern, want)
				}
			}
		})
	}

	// The outer mux (Handler's own mux, not newUIMux) registers both "/ui"
	// and "/ui/" as siblings (server.go's mux.Handle("/ui", uiSubtree) and
	// mux.Handle("/ui/", uiSubtree)) precisely so that neither form triggers
	// net/http.ServeMux's own subtree-root redirect. This drives Handler(d)
	// end to end with httptest.ResponseRecorder, which never follows a
	// redirect on its own, so a 301/307 shows up here as exactly that status
	// with a Location header — not as a followed 200.
	t.Run("the outer mux mounts both /ui and /ui/ with no redirect", func(t *testing.T) {
		t.Parallel()

		h := Handler(Deps{Version: "test", UI: ui.New(ui.Deps{})})

		for _, path := range []string{"/ui", "/ui/"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("GET %s = %d, want 200 (no redirect)", path, rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("GET %s set Location: %q — want no redirect", path, loc)
			}
		}
	})
}
