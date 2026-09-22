package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPresentedSecret pins presentedSecret's own contract, the one
// requireCookie's timing-safety now depends on structurally rather than by
// convention: it always returns exactly want bytes, has no error result to
// short-circuit on, and takes no http.ResponseWriter to answer a decode
// error with — the shape that makes an early return on a decode error
// unexpressible (design m4a §3.3, ADR-0028). This is a pinning test for a
// behaviour-preserving refactor, not a new behavioural RED: requireCookie's
// response-level behaviour (TestUIViewsRequireCookie,
// server_test.go) was already correct before presentedSecret existed; what
// changed here is the shape, watched red only because presentedSecret
// itself did not exist yet, not because any response changed.
func TestPresentedSecret(t *testing.T) {
	const want = len("the-real-token")

	t.Run("missing cookie returns a want-length zero value", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/ui", nil)
		got := presentedSecret(r, uiCookieName, want)
		assertWantLengthZeroValue(t, got, want)
	})

	t.Run("malformed base64 returns a want-length zero value", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/ui", nil)
		r.AddCookie(&http.Cookie{Name: uiCookieName, Value: "not-valid-base64!!!"})
		got := presentedSecret(r, uiCookieName, want)
		assertWantLengthZeroValue(t, got, want)
	})

	t.Run("a value that decodes to the wrong length returns a want-length zero value", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/ui", nil)
		r.AddCookie(&http.Cookie{Name: uiCookieName, Value: base64.RawURLEncoding.EncodeToString([]byte("short"))})
		got := presentedSecret(r, uiCookieName, want)
		assertWantLengthZeroValue(t, got, want)
	})

	t.Run("a value that decodes to want bytes is returned decoded", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/ui", nil)
		r.AddCookie(&http.Cookie{Name: uiCookieName, Value: base64.RawURLEncoding.EncodeToString([]byte("the-real-token"))})
		got := presentedSecret(r, uiCookieName, want)
		if string(got) != "the-real-token" {
			t.Errorf("presentedSecret(...) = %q, want %q", got, "the-real-token")
		}
	})
}

// assertWantLengthZeroValue asserts got is exactly want bytes, every byte
// zero — presentedSecret's own contract on every failure path (missing,
// malformed, or wrong-length), never a shorter or nil slice a caller could
// tell apart from a genuine zero-value cookie by length alone.
func assertWantLengthZeroValue(t *testing.T, got []byte, want int) {
	t.Helper()

	if len(got) != want {
		t.Fatalf("presentedSecret(...) has length %d, want %d", len(got), want)
	}
	for i, b := range got {
		if b != 0 {
			t.Errorf("presentedSecret(...)[%d] = %d, want 0", i, b)
		}
	}
}
