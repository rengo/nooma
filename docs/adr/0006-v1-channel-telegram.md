# ADR-0006 — v1 channel: Telegram

- **Status**: Accepted
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3

## Context

Nooma needs a *lean-in* surface: a place where the user dumps an idea in ten seconds from their
phone, and where the brain speaks to them when something warrants it. The web UI is *lean-back*
(explore, curate, audit) and does not cover that case.

The decision was already made de facto in the original design; this ADR formalizes it and adds
the security constraint that was missing.

## Options evaluated

| Channel | Pro | Con |
|---|---|---|
| Telegram | A bot in two minutes via BotFather, stable and documented API, zero cost, zero legal friction | Less ubiquitous than WhatsApp in some markets |
| WhatsApp | Where people already are | Both paths close badly: the unofficial protocol (ban risk) or the Cloud API with Business Verification (legal friction and cost) — neither compatible with "install the binary and go" |
| Mail | Universal, no account needed | High latency, poor UX for fast capture, unpredictable spam filtering |

## Decision

**Telegram is the first-class channel for v1.** A BotFather bot, the token in `.env` via
`bot_token_env`, and **`allowed_chat_ids` mandatory**.

The constraint being added, which was not explicit before: **without a populated
`allowed_chat_ids`, the channel does not start.** Not "starts with a warning", not "starts in
open mode": it fails clearly and the process continues without that channel. A Telegram bot is
discoverable; a personal brain listening to anyone who messages it is a design failure, not a
careless configuration. The safe default has to be structural, like everything else in Nooma.

WhatsApp stays **in the code as a disabled adapter**, with the warning explaining why. Mail,
Matrix, Discord, and Slack: the community can contribute them against the channel interface.

## Consequences

### What it enables

- The first channel is implemented against a stable API with no paperwork, which lets M3
  validate the **channel interface** itself — the thing that matters long term. Adding a
  channel must be one new adapter and zero core changes.
- The present-but-disabled WhatsApp adapter documents the problem in the place someone will
  look for it.

### What it costs

- In markets where Telegram has low penetration, onboarding requires installing one more app.
- Mandatory `allowed_chat_ids` adds a step to the wizard: the user has to message the bot once
  so `nooma init` can show them their chat ID.

### Reversal criteria

None foreseeable for v1. An additional channel does not supersede this ADR: it adds to it.
