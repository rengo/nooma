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

	srv := httptest.NewServer(Handler("test-version"))
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

	srv := httptest.NewServer(Handler("test-version"))
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

	srv := httptest.NewServer(Handler("test-version"))
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
