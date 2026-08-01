# ADR-0014 — Telegram transport: long polling, never a webhook

- **Status**: Accepted
- **Date**: 2026-07-31
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3

## Context

[ADR-0006](0006-v1-channel-telegram.md) makes Telegram the first-class channel for v1 and
settles the bot, the token's source, and `allowed_chat_ids` being mandatory. It says nothing
about **how messages reach Nooma**.

[ADR-0007](0007-http-auth.md) binds the server to `127.0.0.1` by default and makes
`server.auth_token_env` mandatory the moment a non-loopback bind is configured — "not 'starts
with a warning': it does not start."

Those two decisions are coherent **only under one of the two transports Telegram offers**. The
gap was found by the owner asking an ordinary question — *does Nooma need to be exposed to the
internet at all?* — and a search of `docs/` returning no mention of polling, webhook or
`getUpdates` anywhere. The entire security posture of the project rests on an assumption no
document states.

That is the failure mode this repository keeps meeting: a property every part of the design
relies on, that nothing affirms.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| **Long polling** (`getUpdates`) | Zero inbound ports. No DNS name, no certificate, no reverse proxy. Works behind NAT, on a laptop that sleeps, on a home connection with a dynamic address. Coherent with ADR-0007's loopback default | Nooma must be running to receive anything — messages sent while it is off wait in Telegram's queue rather than arriving late by push. One outbound connection held open |
| **Webhook** | Telegram pushes; no held connection, marginally lower latency and fewer requests at rest | Requires a **publicly reachable HTTPS endpoint** with a valid certificate and a stable name. Inverts ADR-0007's premise: the process that was reachable only from `localhost` now has to accept connections from the internet. Every home deployment needs a tunnel, a dynamic-DNS record, or a port forward |

## Decision

**Nooma reaches Telegram; Telegram never reaches Nooma. The channel uses long polling
(`getUpdates`), and a webhook transport is out of scope for v1.**

The channel opens **outbound** connections only. Nooma requires **no inbound port, no DNS name,
no certificate and no public address** to be fully functional. Combined with ADR-0007's loopback
default, a stock Nooma is not reachable from the internet at all, and that is a property of the
design rather than of the user's firewall.

This is not a performance decision — a webhook is marginally cheaper at rest. It is a decision
about what kind of thing Nooma is. **A personal brain that must be reachable from the internet to
work is no longer something that runs on your machine.** The latency Telegram's own long polling
adds is not perceptible in a conversation; the exposure a webhook adds is permanent.

### What this does not forbid

Reaching the **UI** from another device is a deployment concern, and this ADR deliberately leaves
it open. A user who wants to open Nooma's UI from a phone has three paths, in ascending order of
exposure, and all three sit **outside the binary**:

1. A VPN or a mesh network (Tailscale, WireGuard) — the device joins the machine's network and
   the loopback default never changes.
2. A LAN bind — `server.bind` set to a non-loopback address, at which point ADR-0007 makes the
   auth token structurally mandatory.
3. A reverse proxy or tunnel terminating TLS in front of Nooma.

None of these require Nooma to change, and none is Nooma's to ship in v1. Keeping them outside
the binary is what lets the default stay closed: **the product does not have to choose an
exposure model on the user's behalf.**

### Operational consequence, stated because it is a real tradeoff

With polling, Nooma receives a message only while it is running. Telegram queues updates
server-side and delivers them on the next `getUpdates`, so nothing is lost while the process is
down — but nothing arrives either. A timer that should have fired at 3 a.m. on a sleeping laptop
fires when the laptop wakes, late and honest, rather than never.

That is the correct behaviour for a brain that lives on one machine, and M3's prospection work
must treat a late delivery as a normal case rather than an error.

## Consequences

### What it enables

- **The zero-exposure default becomes a property of the whole system**, not just of the HTTP
  server. ADR-0007 closed one door; this closes the other, and now the sentence "Nooma needs no
  inbound connectivity" is true without qualification.
- `nooma doctor`'s exposure report (ADR-0007) can state the complete picture, because there is no
  second listener it does not know about.
- A home deployment needs no dynamic DNS, no port forward, and no certificate. That removes the
  single largest setup obstacle for a self-hosted personal tool.

### What it costs

- **One held outbound connection** per configured channel, and a process that must be running to
  converse. For a personal brain on a personal machine that is the expected shape, but it does
  make Nooma unsuitable as an always-available assistant without the user keeping it running
  somewhere.
- Marginally more requests at rest than a webhook. Telegram's long polling amortises this well;
  it is named here so the tradeoff is recorded rather than discovered.

### Reversal criteria

A deployment story where Nooma is expected to run on an always-on host that **already** terminates
TLS on a public name — a hosted or multi-tenant mode, which `docs/05-build-plan.md`'s "After v1"
section already contemplates. In that world a webhook stops requiring the user to build an
exposure path, because the path exists for other reasons. That is a different product shape, and
it needs its own ADR superseding this one, not an option flag added quietly.
