package httpapi

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

// uiCookieName is the one cookie this binary sets. It says what it holds
// (design m4a §3.3; ADR-0028).
const uiCookieName = "nooma_token"

// requireCookie is requireToken's sibling for the UI (auth.go): same no-op
// on an empty token, same constant-time comparison, same byte-identical
// refusal for "missing" and "wrong". It differs in what it reads (the
// cookie, never the Authorization header — one presentation per surface,
// ADR-0007) and in how it refuses a GET/HEAD navigation with no valid
// cookie: a 303 to the handshake screen, /ui/login — a browser navigation
// cannot carry an Authorization header, so it is sent where the key is,
// rather than turned away with a bare status. m4a's guarded routes accept
// no other method: a non-GET request never reaches this middleware, the
// mux answers 405 first (design m4a §3.2's "method posture" correction) —
// so there is no second arm here to write.
//
// A cookie value that fails to decode is a wrong cookie, not a third,
// faster-answering outcome (design m4a §3.3): on a decode error, the
// comparison runs anyway against a fixed-length zero-value slice instead
// of returning early, so "missing", "wrong" and "malformed" stay
// byte-identical in both timing and response — an early return on the
// decode error would be a timing and code-path oracle telling a caller
// their cookie failed to decode rather than failed to match, the same
// MUST NOT requireToken already holds for a missing vs. a wrong header.
func requireCookie(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented := make([]byte, len(token))
			if c, err := r.Cookie(uiCookieName); err == nil {
				if decoded, decErr := base64.RawURLEncoding.DecodeString(c.Value); decErr == nil {
					presented = decoded
				}
			}

			// subtle.ConstantTimeCompare requires equal-length inputs to
			// compare in constant time; a length mismatch already returns 0
			// immediately, which leaks only the decoded value's length,
			// never its content — requireToken's own accepted trade-off
			// (auth.go).
			if subtle.ConstantTimeCompare(presented, []byte(token)) != 1 {
				http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
