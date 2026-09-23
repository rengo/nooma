package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ui"
)

// wantUICSP mirrors design m4a §3.5's exact CSP string — asserted as a
// literal here, not against headers.go's own uiCSP constant, so a change to
// the policy has to be a deliberate edit to both places, not a tautology
// that always agrees with itself.
const wantUICSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'"

// assertUISecurityHeaders is TestUISubtreeSetsSecurityHeaders' own shared
// check: every response under /ui carries the five headers design m4a §3.5
// fixes, with Cache-Control the one value that varies by route class
// (no-store on a view, no-cache on a static asset).
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

// stubTodayReader answers Today unconditionally with a fixed, empty
// brain.Today. This package's own tests exist to check cookie, header and
// mux-wiring behavior over a real view, not Today's own markup — that
// detailed rendering is internal/ui's job (tasks 7.1, 7.2) — so every
// fixture below that needs GET /ui to reach a 200 wires this instead of
// leaving Deps.Today nil, which now answers 503 unconditionally
// (TestTodayView_NilTodayReaderAnswers503, internal/ui).
type stubTodayReader struct{}

func (stubTodayReader) Today(context.Context) (brain.Today, error) {
	return brain.Today{}, nil
}

// TestHandlerServesAPIRootAndUIShell is design m4a §3.1's PR 2 shell state,
// renamed from TestHandlerServesBothSurfaces (§7.2). Its own scope is PR 2
// through PR 6 only: the shell paragraph it used to assert on GET /ui no
// longer exists at this tip — ServeHTTP renders Today unconditionally once
// a TodayReader is wired, and answers 503 for the same no-TodayReader
// fixture (ui.New(ui.Deps{})) this test used to build. That fixture's PR 7+
// behavior is taken over by TestTodayView_NilTodayReaderAnswers503
// (internal/ui, §7.2, §8) — this test keeps only the assertion still true
// at this tip: the API root (task 7.3).
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

// TestNoUIUnmountsTheSubtree is design m4a §3.9's --no-ui state (PR 3):
// with Deps.UI == nil, /ui and /ui/login answer exactly as /does-not-exist
// does under the same token state — no stub page, no distinct status.
//
// This is a pinning test for behavior PR 2's own mux gate (server.go's
// `if d.UI != nil`) already produces — verified against this tree before
// PR 3 added any code: both token states already answer identically to an
// unmatched path. The gap PR 3 actually closes is entirely in
// cmd/nooma/serve.go, where Deps.UI was wired unconditionally until this
// PR; that gap is exercised by cmd/nooma's TestResolveUIEnabled_* and
// test/e2e's TestServeNoUI, not by this test. Kept here as a completeness
// check against the class of regression the mutation below names.
func TestNoUIUnmountsTheSubtree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		token      string
		wantStatus int
	}{
		{name: "no token configured", token: "", wantStatus: http.StatusNotFound},
		{name: "a token is configured", token: "the-real-token", wantStatus: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := Handler(Deps{Version: "test", Token: tc.token}) // UI left nil

			unknown := doGet(h, "/does-not-exist")
			if unknown.Code != tc.wantStatus {
				t.Fatalf("GET /does-not-exist = %d, want %d — fixture assumption broken", unknown.Code, tc.wantStatus)
			}

			for _, path := range []string{"/ui", "/ui/login"} {
				got := doGet(h, path)
				if got.Code != unknown.Code || got.Body.String() != unknown.Body.String() {
					t.Errorf("GET %s = %d %q, want the same answer as /does-not-exist (%d %q) — a stub page or a different status would leak that the UI exists but is off",
						path, got.Code, got.Body.String(), unknown.Code, unknown.Body.String())
				}
			}
		})
	}
}

// TestHandlerServesDistinctSurfaces is a small honesty check: the API
// reports what it is, the UI carries its own layout, and neither pretends
// to be the other. It exists so that when a later PR changes the UI, the
// test that breaks names the thing that changed.
func TestHandlerServesDistinctSurfaces(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(Handler(Deps{Version: "test-version", UI: ui.New(ui.Deps{Today: stubTodayReader{}})}))
	t.Cleanup(srv.Close)

	api := get(t, srv.URL+"/")
	uiResp := doGet(Handler(Deps{Version: "test-version", UI: ui.New(ui.Deps{Today: stubTodayReader{}})}), "/ui")
	uiBody := uiResp.Body.String()

	if uiResp.Code != http.StatusOK {
		t.Fatalf("GET /ui = %d, want 200 — without a TodayReader this answers 503, and then the\ncomparison below passes for a reason that has nothing to do with the two\nsurfaces being distinct", uiResp.Code)
	}
	if !strings.Contains(api, "test-version") {
		t.Errorf("the API response does not report the version:\n%s", api)
	}
	if !strings.Contains(uiBody, "<main>") {
		t.Errorf("the UI response does not carry its own layout:\n%s", uiBody)
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

// TestOpenRoutesAndUIRoutesUnderAToken is R2.12's own review checkpoint,
// made executable, and R1's own MUST for the UI. Renamed from
// TestOpenRoutesStayOpenRegardlessOfToken, and its /ui leg inverted (design
// m4a §3.2, PR 4a): GET / stays reachable without a token even when one is
// configured, and never sets a cookie — ADR-0017's scope is the API's
// bearer-token header only, never the UI's cookie handshake (ADR-0007). GET
// /ui with a token configured and no cookie no longer answers 200 with the
// shell — it answers 303 to the handshake screen, carrying no vault data
// and no Set-Cookie.
//
// Its third leg is PR 4b's own RED commit (design m4a §3.2): GET /ui/login
// is now 200 with no Set-Cookie — this leg could not be asserted in PR 4a
// because loginPage is PR 4b's own GREEN, and PR 4a's tip must stay green.
func TestOpenRoutesAndUIRoutesUnderAToken(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test", Token: "the-real-token", UI: ui.New(ui.Deps{})})

	t.Run("/", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET / with a token configured and no Authorization header: status = %d, want %d", rec.Code, http.StatusOK)
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Error("GET / set a cookie — ADR-0017's scope is the API header only, not a UI session")
		}
	})

	t.Run("/ui", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/ui", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET /ui with a token configured and no cookie: status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/ui/login" {
			t.Errorf("GET /ui Location = %q, want %q", loc, "/ui/login")
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Error("GET /ui set a cookie before the handshake ran")
		}
		for _, section := range []string{"FOCUS", "PENDING DIGEST", "SYSTEM"} {
			if strings.Contains(rec.Body.String(), section) {
				t.Errorf("GET /ui with no cookie leaked %q — it must carry no vault-shaped content", section)
			}
		}
	})

	t.Run("/ui/login", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/ui/login", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET /ui/login with a token configured and no cookie: status = %d, want %d", rec.Code, http.StatusOK)
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Error("GET /ui/login set a cookie before any credentials were submitted")
		}
	})
}

// guardedUILeafRequestPaths is every request path newUIMux's guarded
// leaves actually answer — "/ui" for the "GET /ui" pattern, "/ui/" for the
// "GET /ui/{$}" pattern ({$} is net/http.ServeMux's exact-end wildcard, so
// that pattern's real request path is "/ui/", probed and confirmed against
// this tree, not assumed). TestUIViewsRequireCookie iterates this list, not
// only "/ui": round 5's review wired a second guard function into GET
// /ui/{$} in place of requireCookie, leaving requireCookie itself
// untouched, and this test stayed green because it only ever drove GET
// /ui. Iterating closes the half of that gap this test can see — a guard
// that answers a leaf differently, or lets a request through. It does NOT
// close the other half: a swapped guard whose refusals are byte-identical
// is invisible here by construction, and was re-probed to confirm it
// (the swap leaves this test green and fails only
// test/conformance/httpapi_ui_wiring_test.go). Which function a route
// actually uses is that gate's job, not this one's.
// A leaf added to newUIMux (internal/httpapi/server.go) must be
// added here too, in the same commit — the structural sibling of this
// requirement is test/conformance/httpapi_ui_wiring_test.go's
// wantUIMuxWiring table, which pins newUIMux's registrations themselves
// against an AST, so a leaf silently added to one without the other is
// still two separate, deliberate edits away from being missed by both.
var guardedUILeafRequestPaths = []string{"/ui", "/ui/"}

// TestUIViewsRequireCookie is design m4a §3.3's own MUST NOT: a missing
// cookie, a wrong cookie and a cookie whose value fails
// base64.RawURLEncoding decoding all answer every guarded UI leaf
// identically — 303 to /ui/login, no Set-Cookie — and the right cookie
// reaches the view, on each leaf in guardedUILeafRequestPaths. POST /ui,
// which no route under a guarded leaf declares, answers 405 from the mux
// itself, never from requireCookie (design m4a §3.2's "method posture"
// correction: m4a registers no non-GET pattern under a guarded view, so
// requireCookie carries no such arm to intercept it with).
func TestUIViewsRequireCookie(t *testing.T) {
	t.Parallel()

	const token = "the-real-token"
	h := Handler(Deps{Version: "test", Token: token, UI: ui.New(ui.Deps{Today: stubTodayReader{}})})

	for _, leaf := range guardedUILeafRequestPaths {
		leaf := leaf

		t.Run(leaf+": missing, wrong and malformed cookies answer byte-identically", func(t *testing.T) {
			t.Parallel()

			wrongValue := base64.RawURLEncoding.EncodeToString([]byte("not-the-token"))
			// sameLengthWrongValue is 14 bytes decoded, exactly like token itself
			// (spec R2's own "Verified by": "exercised with a same-length wrong
			// value and a different-length value, both rejected"). wrongValue
			// above is 13 bytes, so subtle.ConstantTimeCompare short-circuits on
			// the length mismatch alone and never walks the full comparison —
			// this case is what actually exercises that path.
			sameLengthWrongValue := base64.RawURLEncoding.EncodeToString([]byte("the-fake-token"))
			cases := []struct {
				name   string
				cookie *http.Cookie
			}{
				{name: "missing cookie", cookie: nil},
				{name: "wrong cookie", cookie: &http.Cookie{Name: uiCookieName, Value: wrongValue}},
				{name: "wrong cookie, same length as the real token", cookie: &http.Cookie{Name: uiCookieName, Value: sameLengthWrongValue}},
				{name: "malformed cookie", cookie: &http.Cookie{Name: uiCookieName, Value: "not-valid-base64!!!"}},
			}

			var first *httptest.ResponseRecorder
			for _, tc := range cases {
				req := httptest.NewRequest(http.MethodGet, leaf, nil)
				if tc.cookie != nil {
					req.AddCookie(tc.cookie)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)

				if rec.Code != http.StatusSeeOther {
					t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, http.StatusSeeOther)
				}
				if loc := rec.Header().Get("Location"); loc != "/ui/login" {
					t.Errorf("%s: Location = %q, want %q", tc.name, loc, "/ui/login")
				}
				if rec.Header().Get("Set-Cookie") != "" {
					t.Errorf("%s: response set a cookie", tc.name)
				}
				if first == nil {
					first = rec
				} else if rec.Code != first.Code || rec.Body.String() != first.Body.String() {
					t.Errorf("%s: response is not byte-identical to %q's — %d %q vs %d %q",
						tc.name, cases[0].name, rec.Code, rec.Body.String(), first.Code, first.Body.String())
				}
			}
		})

		t.Run(leaf+": the right cookie reaches the view", func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, leaf, nil)
			req.AddCookie(&http.Cookie{Name: uiCookieName, Value: base64.RawURLEncoding.EncodeToString([]byte(token))})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s with the right cookie = %d, want 200", leaf, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "SYSTEM") {
				t.Errorf("GET %s with the right cookie does not carry the view:\n%s", leaf, rec.Body.String())
			}
		})
	}

	t.Run("POST /ui answers 405 from the mux, not from requireCookie", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodPost, "/ui", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST /ui = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
			t.Errorf("POST /ui Allow = %q, want %q", allow, "GET, HEAD")
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Error("POST /ui set a cookie")
		}
		// Probed, not assumed (design m4a §3.2, §5 claim "no body" for this
		// arm; net/http.ServeMux's own default 405 handler is http.Error,
		// which does write one — "Method Not Allowed\n", confirmed against
		// this tree). What actually matters for R1 is that nothing
		// vault-shaped leaks through an unauthenticated method mismatch;
		// asserted against the generic stdlib message, not against silence.
		if body := rec.Body.String(); body != "Method Not Allowed\n" {
			t.Errorf("POST /ui body = %q, want the generic stdlib 405 message (no vault-shaped content)", body)
		}
	})
}

// TestRequireCookieNoOpOnlyOnLoopback sweeps binding_test.go's own
// bindTokenTruthTable — TestRequireTokenNoOpOnlyOnLoopback's exact shape
// (auth_test.go) — asserting that for every row where DecideBinding
// actually succeeds, requireCookie is a no-op if and only if the effective
// bind is loopback (design m4a §3.2: "When Token == \"\"").
func TestRequireCookieNoOpOnlyOnLoopback(t *testing.T) {
	t.Parallel()

	for _, tc := range bindTokenTruthTable {
		if tc.wantErr {
			// Not a state a live request could ever reach: DecideBinding
			// itself refuses to start the server, so nothing here is
			// reachable through the middleware at all.
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := cfg(t, tc.document)
			lookup := func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok }

			if _, err := DecideBinding(c, lookup); err != nil {
				t.Fatalf("DecideBinding: %v (this row must be one where it succeeds)", err)
			}

			token, _ := ResolveToken(c, lookup)
			wantLoopback := isLoopback(*c.Server.Bind)

			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})

			guarded := requireCookie(token)(next)

			// A request carrying no cookie at all reaches the handler
			// exactly when the middleware is a no-op — which must be
			// exactly when the bind is loopback.
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)

			if called != wantLoopback {
				t.Errorf("bind %q: a cookie-less request reached the handler = %v, want %v",
					*c.Server.Bind, called, wantLoopback)
			}
		})
	}
}

// TestUISubtreeSetsSecurityHeaders is design m4a §3.5, §8: every response
// under /ui — a rendered view, a static asset, an unmatched path, a
// cross-origin refusal — carries the five fixed headers, with the right
// Cache-Control for its route class. No token is configured, so
// requireCookie is a no-op here (303/401 need a token, covered by
// TestUIViewsRequireCookie): 200 on the view and on a static asset, 404 on
// an unmatched /ui path, 403 on a refused cross-origin POST.
func TestUISubtreeSetsSecurityHeaders(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test", UI: ui.New(ui.Deps{Today: stubTodayReader{}})})

	cases := []struct {
		name             string
		method           string
		path             string
		crossSite        bool
		wantStatus       int
		wantCacheControl string
		wantAllow        string
	}{
		{name: "the view", method: http.MethodGet, path: "/ui", wantStatus: http.StatusOK, wantCacheControl: "no-store"},
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

		h := Handler(Deps{Version: "test", UI: ui.New(ui.Deps{Today: stubTodayReader{}})})

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

// TestLoginIssuesTheCookieOnlyOnTheRightToken is design m4a §3.3's own
// contract for POST /ui/login on success: exactly one Set-Cookie, every flag
// ADR-0028 fixes, a 303 back to /ui — and Secure follows the request's own
// TLS state rather than a hard-coded value (spec R2's "Verified by": "cookie
// flags asserted on the Set-Cookie header").
func TestLoginIssuesTheCookieOnlyOnTheRightToken(t *testing.T) {
	t.Parallel()

	const token = "the-real-token"

	for _, tc := range []struct {
		name       string
		newServer  func(http.Handler) *httptest.Server
		wantSecure bool
	}{
		{name: "plain HTTP", newServer: httptest.NewServer, wantSecure: false},
		{name: "TLS", newServer: httptest.NewTLSServer, wantSecure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := Handler(Deps{Version: "test", Token: token, UI: ui.New(ui.Deps{})})
			srv := tc.newServer(h)
			defer srv.Close()

			client := srv.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}

			resp, err := client.PostForm(srv.URL+"/ui/login", url.Values{"token": {token}})
			if err != nil {
				t.Fatalf("POST /ui/login: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusSeeOther {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
			}
			if loc := resp.Header.Get("Location"); loc != "/ui" {
				t.Errorf("Location = %q, want %q", loc, "/ui")
			}

			cookies := resp.Cookies()
			if len(cookies) != 1 {
				t.Fatalf("Set-Cookie count = %d, want 1 (%v)", len(cookies), cookies)
			}
			c := cookies[0]

			if c.Name != uiCookieName {
				t.Errorf("cookie name = %q, want %q", c.Name, uiCookieName)
			}
			decoded, err := base64.RawURLEncoding.DecodeString(c.Value)
			if err != nil {
				t.Fatalf("cookie value does not decode as base64url: %v", err)
			}
			if string(decoded) != token {
				t.Errorf("decoded cookie value = %q, want %q", decoded, token)
			}
			if c.Path != "/ui" {
				t.Errorf("cookie Path = %q, want %q", c.Path, "/ui")
			}
			if !c.HttpOnly {
				t.Error("cookie is not HttpOnly")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Errorf("cookie SameSite = %v, want Strict", c.SameSite)
			}
			if c.Secure != tc.wantSecure {
				t.Errorf("cookie Secure = %v, want %v", c.Secure, tc.wantSecure)
			}
			if c.MaxAge != 0 || !c.Expires.IsZero() {
				t.Errorf("cookie has a lifetime (MaxAge=%d, Expires=%v), want a session cookie", c.MaxAge, c.Expires)
			}
		})
	}
}

// TestLoginRejectionIsByteIdentical is design m4a §3.3's own MUST: an empty
// token field and a wrong one produce the same 401 body and headers, because
// both reach the same comparison — telling them apart would be an oracle for
// whether a submission was even attempted.
func TestLoginRejectionIsByteIdentical(t *testing.T) {
	t.Parallel()

	const token = "the-real-token"
	h := Handler(Deps{Version: "test", Token: token, UI: ui.New(ui.Deps{})})

	cases := []struct {
		name      string
		formToken string
	}{
		{name: "empty field", formToken: ""},
		{name: "wrong token", formToken: "not-the-token"},
	}

	var first *httptest.ResponseRecorder
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/ui/login", strings.NewReader(url.Values{"token": {tc.formToken}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, http.StatusUnauthorized)
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Errorf("%s: response set a cookie on a rejected submission", tc.name)
		}
		if first == nil {
			first = rec
		} else if rec.Code != first.Code || rec.Body.String() != first.Body.String() {
			t.Errorf("%s: response is not byte-identical to %q's", tc.name, cases[0].name)
		}
	}
}

// TestLoginSubmitBodyIsBounded is design m4a §3.3's stated denial-of-service
// mitigation (§9's threat matrix "Denial of service" row): loginSubmit bounds
// the request body with http.MaxBytesReader before ParseForm ever runs. The
// submitted token here is wrong on top of being oversized, so a pass proves
// the bound fires before the comparison is ever reached — not merely that an
// oversized body happens to fail for some unrelated reason. Catches: the
// MaxBytesReader line removed, or its limit widened past the body sent here.
//
// The two leak assertions below are structural guards, not part of that
// proof: ui.LoginView carries no token field and http.Error writes a fixed
// string, so no mutation to the bound can make either fail. They are kept
// for the day one of those two facts changes — do not read a passing run of
// them as evidence the bound itself holds.
//
// The 400 is the status loginSubmit's generic ParseForm-error branch already
// answers, not a bound-specific contract: giving an oversized body its own
// 413 would be a legitimate change that fails this test without regressing
// the bound. Update the expectation in that commit, deliberately.
func TestLoginSubmitBodyIsBounded(t *testing.T) {
	t.Parallel()

	const token = "the-real-token"
	h := Handler(Deps{Version: "test", Token: token, UI: ui.New(ui.Deps{})})

	oversized := strings.Repeat("x", 5000) // a wrong token, > 4096 once form-encoded
	body := url.Values{"token": {oversized}}.Encode()
	if len(body) <= 4096 {
		t.Fatalf("test body is %d bytes, want > 4096 to exceed the bound", len(body))
	}

	req := httptest.NewRequest(http.MethodPost, "/ui/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Error("response set a cookie on a body the bound should have rejected")
	}
	if strings.Contains(rec.Body.String(), oversized) {
		t.Error("response body leaks the submitted value")
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Error("response body leaks the real token")
	}
}

// TestLoginRoutesAbsentWithoutAToken is design m4a §3.2's "no screen, no
// cookie" state: with Token == "" (loopback, no token), GET /ui/login is a
// 404 — a property of the mux, not a branch inside a handler, mirroring how
// d.UI == nil leaves /ui itself unregistered (PR 3).
//
// Committed here, in the GREEN commit, rather than in PR 4b's RED commit:
// probed against that commit's own tree before writing any PR 4b code and
// already true there — no route named /ui/login exists yet at all, with or
// without a token — so writing it as RED would have claimed a failure that
// did not exist (PR 3's task 3.1 precedent for the same situation). It is a
// pinning test now that newUIMux's conditional registration is what makes
// it true.
func TestLoginRoutesAbsentWithoutAToken(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test", UI: ui.New(ui.Deps{})})

	req := httptest.NewRequest(http.MethodGet, "/ui/login", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /ui/login with no token configured: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
