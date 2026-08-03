package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/rengo/nooma/internal/config"
)

// bearerPrefix is the one HTTP convention this ADR uses (RFC 6750 §2.1),
// restated as a literal because internal/httpapi owns transport, not
// internal/config or internal/core.
const bearerPrefix = "Bearer "

// ResolveToken reads the same fact DecideBinding already reads — ADR-0007's
// server.auth_token_env — and is the one function ADR-0017 names as the
// shared source: Deps.Token (the middleware), the CLI (design D11) and
// DecideBinding (binding.go) all read "is a token configured" from this one
// place, so none of the three can disagree with another.
//
// It returns (token, true) only when auth_token_env names a variable the
// environment actually holds — the same "set" test DecideBinding performs
// (binding.go:46), never merely that auth_token_env is non-empty. An unset
// auth_token_env, or one naming a variable the environment does not hold,
// both report (\"\", false) — indistinguishable to a caller, because both
// mean the same thing: there is no token to present or check.
func ResolveToken(cfg *config.Config, lookup func(string) (string, bool)) (string, bool) {
	if cfg.Server.AuthTokenEnv == "" {
		return "", false
	}
	v, set := lookup(cfg.Server.AuthTokenEnv)
	if !set {
		return "", false
	}
	return v, true
}

// requireToken is ADR-0017's per-request check. token is Deps.Token exactly
// as ResolveToken produced it (or "" for the transitional, not-yet-wired
// callers) — an empty token means "none configured," and the returned
// middleware becomes a no-op: every request passes through unmodified. That
// state is reachable only on a loopback bind (TestRequireTokenNoOpOnlyOnLoopback
// pins this against DecideBinding's own truth table), so this function does
// not itself need to know anything about binding to stay safe.
//
// When token is non-empty, every request must carry
// "Authorization: Bearer <token>", compared with crypto/subtle.ConstantTimeCompare
// — a plain == on a secret leaks timing information about a partial match.
// A missing header and a wrong token produce the exact same response: status
// 401, empty body, no header naming what was wrong. Spec R2.11's own MUST
// NOT: telling the two apart would be an oracle for the token's existence,
// independent of its value.
func requireToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented := ""
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, bearerPrefix) {
				presented = strings.TrimPrefix(h, bearerPrefix)
			}

			// subtle.ConstantTimeCompare requires equal-length inputs to
			// compare in constant time; a length mismatch already returns 0
			// immediately, which leaks only the token's length, never its
			// content or which byte first differed — the same trade-off
			// every constant-time-compare caller in Go accepts.
			if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
