# ADR-0007 — HTTP API and UI auth

- **Status**: Accepted
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M4 (tolerable unresolved through M0–M3)

## Context

The binary exposes HTTP on `:7777`: API + complete UI. The vault holds one person's entire
memory — probably the most sensitive file on their machine.

One owner per vault. No users, no roles, no organization. Any auth system carrying those
notions would be invented complexity.

But the "I run it on the Raspberry in the living room and open it from my phone" case will be
**common**, not exotic. And there, binding to localhost stops being enough.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| Bind to `127.0.0.1` only | Zero config, zero friction | Does not cover the LAN/Raspberry case, which is common. Users end up binding `0.0.0.0` with no protection |
| Token always required | Uniform | Absurd friction for `localhost` on your own machine |
| Localhost bind by default + mandatory token if exposed | Covers both cases with the safe default | Two paths to test |
| Users + sessions + roles | "Complete" | Complexity with no owner: there is never more than one user per vault |

## Decision

**Bind to `127.0.0.1` by default. If the user configures a non-loopback bind,
`server.auth_token_env` becomes mandatory and the server does not start without it.**

Not "starts with a warning": it does not start. Same criterion as `allowed_chat_ids` in
[ADR-0006](0006-v1-channel-telegram.md) — the safety of the default is structural, not a
warning that gets ignored.

`nooma doctor` reports the effective bind and whether it is exposed. No users, no roles, no
session management: one owner, one secret.

### The detail that was not obvious

A bearer token in a header solves the API, but **it does not solve the UI**. The UI is
server-side rendered: the browser navigates with plain `GET`s and cannot attach an
authorization header to a navigation. A cookie is required.

Therefore: when the token is active, the UI exposes a minimal handshake (one screen asking for
the token once) that sets a **session cookie with `HttpOnly`, `SameSite=Strict`, and `Secure`
when TLS is present**. The API keeps accepting the header. Two ways to present the same secret,
one secret.

Without this, "optional token" is a decision that breaks itself in M4 the moment the first
browser navigation appears.

## Consequences

### What it enables

- The local case (the majority) pays no friction at all.
- The LAN case is secure by construction, not by user discipline.

### What it costs

- Two auth paths to maintain and test (header and cookie).
- `SameSite=Strict` breaks any future flow where something external links into the UI.
  Accepted: no such flow exists in v1.
- Without TLS on a LAN, the token travels in the clear inside the local network. Documented;
  solving it properly (certificates) is disproportionate for v1.

### Reversal criteria

A real multi-user case appearing — the binary's multi-tenant mode. That mode needs real
identity and will demand its own ADR; this one remains valid for single-vault mode.
