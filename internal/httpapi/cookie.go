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
// faster-answering outcome (design m4a §3.3): the comparison runs anyway
// against a fixed-length zero-value slice instead of returning early — an
// early return on the decode error would be a timing and code-path oracle
// telling a caller their cookie failed to decode rather than failed to
// match, the same MUST NOT requireToken already holds for a missing vs. a
// wrong header. presentedSecret (below) is what makes that early return
// unexpressible INSIDE presentedSecret ITSELF: it has no error result and
// no http.ResponseWriter parameter, so presentedSecret has no decode-error
// branch of its own left to write one in — "missing", "wrong" and
// "malformed" all reach the comparison by the one path presentedSecret
// returns through. That signature says nothing about THIS function's own
// body, though: requireCookie could still re-derive the same fact itself
// (call r.Cookie or decode the value a second time) and branch on it before
// ever calling presentedSecret. What forbids that is a separate,
// control-flow rule
// (test/conformance/httpapi_secret_compare_test.go): the only `return`
// permitted inside this handler's http.HandlerFunc literal is the one
// guarded by the subtle.ConstantTimeCompare comparison below — any other
// early exit fails that gate, whatever fact it branches on.
//
// Three different artifacts prove three different halves of the timing
// claim. TestUIViewsRequireCookie (server_test.go) proves "missing",
// "wrong" and "malformed" answer byte-identically over HTTP — same status,
// same Location, same absence of Set-Cookie — because that is what a
// response recorder can observe; it cannot observe timing.
// test/conformance/httpapi_secret_compare_test.go proves the structural
// half instead, in two parts: presentedSecret's signature makes the
// decode-error branch unexpressible inside presentedSecret, and a
// transitive walk from requireCookie and requireToken over same-package
// calls proves subtle.ConstantTimeCompare is still reached, even through an
// in-package helper like this one; separately, its control-flow rule proves
// this handler itself carries no OTHER early return, guarding against a
// caller-level early exit that the signature check alone cannot see. None
// of the three measures real elapsed time — they prove the structure that
// timing-safety depends on, never the timing itself.
func requireCookie(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented := presentedSecret(r, uiCookieName, len(token))

			// subtle.ConstantTimeCompare requires equal-length inputs to
			// compare in constant time; presentedSecret always returns
			// len(token) bytes, so this comparison never short-circuits on
			// a length mismatch — requireToken's own comparison (auth.go)
			// accepts that short-circuit for its own presented value, but
			// presentedSecret's fixed-length contract removes even that
			// leak here.
			if subtle.ConstantTimeCompare(presented, []byte(token)) != 1 {
				http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// presentedSecret returns exactly want bytes, always — the decoded cookie
// value when it decodes to that length, a zero value otherwise. It returns
// no error and takes no http.ResponseWriter, so presentedSecret itself has
// no decode-error branch to answer from: "missing", "wrong" and "malformed"
// reach its return by the same path because there is no other path inside
// this function to take. This signature is silent on what a CALLER does
// with the result — requireCookie's own doc comment names the separate
// control-flow rule that closes that gap.
func presentedSecret(r *http.Request, name string, want int) []byte {
	c, err := r.Cookie(name)
	if err != nil {
		return make([]byte, want)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(decoded) != want {
		return make([]byte, want)
	}
	return decoded
}
