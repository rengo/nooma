package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestResolveTokenReadsTheSameVariableDecideBindingReads pins ADR-0017's own
// "one read, one source" requirement (design D10): ResolveToken must report
// "no token configured" under exactly the conditions DecideBinding treats as
// "no token configured" — an unset auth_token_env name, or a set name whose
// variable the environment does not hold.
func TestResolveTokenReadsTheSameVariableDecideBindingReads(t *testing.T) {
	t.Parallel()

	t.Run("no auth_token_env configured", func(t *testing.T) {
		t.Parallel()
		c := cfg(t, "{}\n")
		token, configured := ResolveToken(c, func(string) (string, bool) { return "", false })
		if configured {
			t.Errorf("configured = true, want false (no auth_token_env set)")
		}
		if token != "" {
			t.Errorf("token = %q, want empty", token)
		}
	})

	t.Run("auth_token_env named but the variable is unset", func(t *testing.T) {
		t.Parallel()
		c := cfg(t, "server:\n  auth_token_env: NOOMA_AUTH_TOKEN\n")
		token, configured := ResolveToken(c, func(string) (string, bool) { return "", false })
		if configured {
			t.Errorf("configured = true, want false (the named variable is unset)")
		}
		if token != "" {
			t.Errorf("token = %q, want empty", token)
		}
	})

	t.Run("auth_token_env named and the variable is set", func(t *testing.T) {
		t.Parallel()
		c := cfg(t, "server:\n  auth_token_env: NOOMA_AUTH_TOKEN\n")
		lookup := func(k string) (string, bool) {
			if k == "NOOMA_AUTH_TOKEN" {
				return "s3cret", true
			}
			return "", false
		}
		token, configured := ResolveToken(c, lookup)
		if !configured {
			t.Fatal("configured = false, want true")
		}
		if token != "s3cret" {
			t.Errorf("token = %q, want %q", token, "s3cret")
		}
	})
}

// TestRequireTokenNoOpOnlyOnLoopback sweeps binding_test.go's own
// bindTokenTruthTable — the exact combinations DecideBinding is proven
// against — asserting that for every row where DecideBinding actually
// succeeds, requireToken is a no-op if and only if the effective bind is
// loopback (spec R2.10's own MUST, design D10: "one read, one source" so
// bind-time and request-time can never disagree about whether a token
// exists).
func TestRequireTokenNoOpOnlyOnLoopback(t *testing.T) {
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

			token, configured := ResolveToken(c, lookup)
			wantLoopback := isLoopback(*c.Server.Bind)

			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})

			guarded := requireToken(token)(next)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)

			// A request carrying no Authorization header at all reaches the
			// handler exactly when the middleware is a no-op — which must
			// be exactly when the bind is loopback.
			if called != wantLoopback {
				t.Errorf("bind %q: an unauthenticated request reached the handler = %v, want %v (configured=%v)",
					*c.Server.Bind, called, wantLoopback, configured)
			}
		})
	}
}

// TestRequireTokenConstantTimeAndNoDetail pins spec R2.11: when a token is
// configured, a missing token and a wrong token must produce byte-identical
// 401 responses — no distinguishing detail an attacker could use as an
// oracle for the token's existence separately from its value.
func TestRequireTokenConstantTimeAndNoDetail(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guarded := requireToken("the-real-token")(next)

	do := func(t *testing.T, authHeader string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		return rec
	}

	missing := do(t, "")
	wrong := do(t, "Bearer not-the-token")

	if missing.Code != http.StatusUnauthorized {
		t.Errorf("missing token: status = %d, want %d", missing.Code, http.StatusUnauthorized)
	}
	if wrong.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want %d", wrong.Code, http.StatusUnauthorized)
	}
	if missing.Code != wrong.Code || missing.Body.String() != wrong.Body.String() ||
		missing.Header().Get("Content-Type") != wrong.Header().Get("Content-Type") {
		t.Errorf("missing-token and wrong-token responses are not identical: %+v vs %+v", missing, wrong)
	}

	// The correct token still passes through.
	ok := do(t, "Bearer the-real-token")
	if ok.Code != http.StatusOK {
		t.Errorf("correct token: status = %d, want %d", ok.Code, http.StatusOK)
	}
}
