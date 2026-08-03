# ADR-0017 — Per-request bearer-token auth for the HTTP API

- **Status**: Accepted
- **Date**: 2026-08-03
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M1 Phase C

## Context

[ADR-0007](0007-http-auth.md) decided the bind-time half of this problem: bind to `127.0.0.1`
by default, and require `server.auth_token_env` — refusing to listen without it — the moment
the user configures a non-loopback bind. That decision is `Accepted` and is not reopened here.

What ADR-0007 left unresolved, by its own header ("Enables: M4, tolerable unresolved through
M0–M3"), is the other half: **nothing in the tree validates that token per request.**
`internal/httpapi/server.go`'s `Handler` mounts its routes with no middleware at all — a request
that never presents a token is treated exactly like one that does, because nothing ever asks.

That gap was tolerable while the only routes were `GET /` and a UI placeholder: a request to
either read nothing sensitive and changed nothing. It stops being tolerable the moment this PR
mounts `POST /capture`, which writes to the user's memory. A non-loopback bind with a token
configured (ADR-0007's own LAN case, named as the common one) would still let any request on
that network in, because the token is checked once, at startup, and never again on the wire.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| No per-request check; rely on ADR-0007's bind-time refusal alone | Nothing to build | The token is proven to exist at startup and never checked again — a non-loopback bind is not the same guarantee as a non-loopback bind whose requests are authenticated |
| The token as a query parameter | Trivial to implement, works from a browser address bar | Leaks into access logs, shell history and `Referer` headers — the same objection D10 raises against a `GET`-based query surface for `/recall`, which also carries user content |
| `Authorization: Bearer <token>` header, compared constant-time (chosen) | Not logged by default, no timing oracle, standard and client-library-friendly | Every route must remember to send/check it — solved below by making the check structural, not a convention any single route could forget |
| ADR-0007's own cookie handshake, brought forward from M4 | Solves the browser-navigation case too | The UI does not exist yet (M4, ADR-0008) — there is no browser navigation to protect, and a session/cookie mechanism with no UI to log into is complexity with no owner today |

## Decision

**Every route mounted under the API surface requires `Authorization: Bearer <token>` whenever
`server.auth_token_env` names a set environment variable — the same fact ADR-0007's
`DecideBinding` already reads.** `httpapi.ResolveToken(cfg, lookup)` reads that one variable,
and is the single source both `DecideBinding` and the new per-request middleware read from, so
bind-time and request-time can never disagree about whether a token exists.

The guarantee is structural, not a convention a future route could forget: `Handler(Deps)`
builds an open mux (`GET /{$}`, `GET /ui`) and a guarded mux for the entire API surface, from
**one route-table slice used both to register the routes and to drive this decision's own
completeness test** — a new API route added in a later PR is guarded by construction, because
there is no exported way to reach the inner, unguarded mux at all.

When no token is configured, the middleware is a no-op — every request passes through
unmodified, the ordinary loopback-development experience. That state is reachable only when the
effective bind is loopback, because `DecideBinding` already refuses to return a listen address
for a non-loopback bind with no token configured or an unset variable; an L2 test sweeps the
same bind/token truth table `binding_test.go` exercises and pins that the two decisions read the
same fact.

When a token **is** configured, the comparison uses `crypto/subtle.ConstantTimeCompare`, never a
plain `==` — a string-equality compare on a secret is a timing oracle. A missing token and a
wrong token produce a byte-identical `401` response, with no distinguishing detail: an error
message that told the two apart would itself be an oracle an attacker could use to probe for the
token's existence independently of its value.

### The detail that was not obvious

**This ADR's scope is the API's bearer-token header only.** It does not implement ADR-0007's
cookie handshake for the UI. The UI is server-side rendered, tied to M4 (ADR-0008); a browser
navigating with a plain `GET` cannot attach an `Authorization` header to that navigation, and a
bearer header does not, on its own, solve it — building a session/cookie mechanism now, with no
UI screen to present it from, would be complexity with no owner. `GET /` and `GET /ui` stay
reachable without a token in M1, exactly as M0 left them; this ADR's middleware guards the API
surface only, and ADR-0007's own cookie handshake remains M4's to build, unchanged and
unsuperseded by this decision.

## Consequences

### What it enables

- `POST /capture` — the first write route, and every write route mounted after it onto the
  guarded surface — cannot ship unprotected: the guard is structural, not a checklist item a
  future PR could forget to apply.
- The loopback development case stays exactly as frictionless as it is today: no token
  configured, no header required, nothing to remember.
- A caller probing the surface cannot tell "no token sent" from "wrong token sent" apart from
  the response alone.

### What it costs

- Every new API route must be added to the one guarded route-table slice rather than mounted
  directly on a mux — a discipline the code enforces by construction (nothing exported reaches
  the inner mux), not one reviewers must remember to check for.
- The UI's own authentication (ADR-0007's cookie handshake) is still M4's to build; this ADR
  does nothing to bring that forward, and a browser navigating to `/ui` after M4 ships will still
  need that separate mechanism.

### Reversal criteria

A future milestone introducing more than one credential-holder per vault — at which point this
ADR's one-secret, one-owner model stops being sufficient, and the users/roles/sessions design
ADR-0007 already evaluated and rejected for v1 would need its own ADR.
