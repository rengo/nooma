package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandlerServesBothSurfaces is spec R11.1. M0's HTTP surface is deliberately
// thin — the API answers, the UI is a placeholder — but both must exist, because
// docs/01-architecture.md's Layer 2 is that the binary serves the user's frontend
// from the same process, and a placeholder that 404s would make that untrue on
// day one.
func TestHandlerServesBothSurfaces(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(Handler(Deps{Version: "test-version"}))
	t.Cleanup(srv.Close)

	for _, path := range []string{"/", "/ui"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			resp, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
			}
			buf := make([]byte, 1)
			if n, _ := resp.Body.Read(buf); n == 0 {
				t.Errorf("GET %s returned an empty body", path)
			}
		})
	}
}

// TestUnknownPathIs404 keeps the placeholder from becoming a catch-all. A mux
// that answered everything would make the first real route impossible to notice
// as missing.
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

// TestHandlerNeverEchoesTheVersionIntoTheUI is a small honesty check: the API
// reports what it is, the UI placeholder says it is a placeholder, and neither
// pretends to be the other. It exists so that when M4 replaces the UI, the test
// that breaks names the thing that changed.
func TestHandlerServesDistinctSurfaces(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(Handler(Deps{Version: "test-version"}))
	t.Cleanup(srv.Close)

	api := get(t, srv.URL+"/")
	ui := get(t, srv.URL+"/ui")

	if !strings.Contains(api, "test-version") {
		t.Errorf("the API response does not report the version:\n%s", api)
	}
	if api == ui {
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
// (ADR-0007, M4).
func TestOpenRoutesStayOpenRegardlessOfToken(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test", Token: "the-real-token"})

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
