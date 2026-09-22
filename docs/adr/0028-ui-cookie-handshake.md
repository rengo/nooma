# ADR-0028 — The UI cookie carries the token itself; cross-origin UI requests are refused structurally

- **Status**: Accepted
- **Date**: 2026-09-22
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M4a

## Context

[ADR-0007](0007-http-auth.md) already decided that the UI needs a session cookie — a browser
navigation cannot carry an `Authorization` header — and fixed the cookie's flags: `HttpOnly`,
`SameSite=Strict`, `Secure` when TLS is present. It never decided what the cookie's *value* is,
and it is `Accepted`, so it cannot be edited to say.

`m4a` (`openspec/changes/m4a-ui-foundation`) is the first change that actually builds the
handshake and mounts a guarded `/ui` route. Two forks need a decision before `requireCookie` can
be written: what the cookie holds (Q1), and whether the browser's own cross-origin protections
are enough on their own (Q2). Both were owner-ruled on 2026-09-16
(`openspec/changes/m4-mirror-ui/proposal.md` §8).

## Options evaluated

**Q1 — what the cookie's value is.**

| Option | Verdict |
|---|---|
| The token itself, base64url-encoded | **Chosen.** One secret, two presentations (header, cookie) — the shape ADR-0007 already described. No new state to keep in sync with the token's own rotation |
| An opaque session id (Q1-B) | **Rejected.** Needs a session table — "users + sessions" ADR-0007 already rejected for this single-owner vault. A table with no owner: nothing ever prunes it, nothing ever names who a session belongs to beyond "whoever holds the id" |

**Q2 — what protects the UI's non-GET routes from a cross-site request.**

| Option | Verdict |
|---|---|
| `net/http.CrossOriginProtection`, no trusted origins, wrapping the whole `/ui` subtree | **Chosen.** One line, applies to `POST /ui/login` today and to every mutating route a later slice adds, with no second mechanism to remember |
| A synchronizer token per form (Q2-B) | **Rejected.** State per session, a hidden field in every template, and — the sharper problem — no protection on loopback with no cookie yet to bind the token to; the handshake itself is the route most exposed |
| `SameSite=Strict` alone (Q2-C) | **Rejected.** Zero protection on the default, cookie-less, loopback configuration — non-negotiable #7 (safe defaults are structural) forbids a posture that only protects the configuration an operator opted into |

## Decision

**The UI's session cookie carries the configured token itself, base64url-encoded, compared in
constant time exactly as the header is. Every non-safe request under `/ui` passes
`net/http.CrossOriginProtection` with no trusted origins and no bypass pattern.**

The cookie (`nooma_token`) is a session cookie — no `Max-Age`, no `Expires` — scoped to
`Path=/ui`, `HttpOnly`, `SameSite=Strict`, `Secure` from the request's own `r.TLS` state
(ADR-0007's flags, restated here as what this cookie actually is, not amended). Verifying a
request's cookie against the configured token uses `crypto/subtle.ConstantTimeCompare`, mirroring
`requireToken`'s own comparison (`internal/httpapi/auth.go`); a cookie value that fails to decode
is compared anyway, against a fixed-length zero value, rather than answered with an early return —
an early return would be a timing and code-path oracle for "this cookie is malformed" that
`requireToken`'s own MUST NOT already forbids for "this header is missing".

There is no session table, no session id and, in this first slice, no logout: closing the browser
ends a session cookie, and rotating `server.auth_token_env`'s value invalidates every existing
cookie with no other code path involved — the same rotation story ADR-0007 already told for the
header.

## Consequences

### What it enables

- One secret, two presentations. `requireToken` (header) and `requireCookie` (cookie) compare the
  same bytes the same way; nothing about the UI's auth model needs its own mental model beyond
  ADR-0007's.
- Every mutating route under `/ui` — today's handshake `POST`, and every later slice's forms —
  inherits cross-origin protection from one wrap around the subtree, not a per-route decision a
  future contributor could forget.

### What it costs

- Rotating `server.token`/`auth_token_env` invalidates every open UI session at once, with no
  partial migration — accepted, since a rotation is already meant to be a hard cutover
  (ADR-0007).
- The secret travels in every UI request once the cookie is set, exactly as it already travels in
  every API request's header — the same LAN exposure ADR-0007 accepts and documents when TLS is
  not terminated (`serve.go` uses `ListenAndServe` today, so `Secure` is `false` on every current
  deployment).
- A pre-2023 browser that sends `Origin` cross-site is refused by `CrossOriginProtection`; a
  client that sends neither `Sec-Fetch-Site` nor `Origin` is admitted, on the stdlib's own
  assumption that it is not a browser (`$GOROOT/src/net/http/csrf.go`) — corrected here from an
  earlier, inverted reading of that cost in the umbrella proposal, and recorded so the next reader
  does not re-derive it.

### Reversal criteria

A genuine multi-user vault — ADR-0007's own reversal criterion — would also force this ADR open:
a session id decoupled from the token, revocable per session rather than only by rotating the one
shared secret, is what that mode needs and what this design deliberately does not build.
