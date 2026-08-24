# Archive Report — m3c-telegram

**Change**: m3c-telegram (third of M3's chained changes: m3a → m3b → **m3c** → m3d)
**Date Archived**: 2026-08-23
**Mode**: hybrid (openspec files + Engram observations)
**Status**: Complete — all 5 implementation PRs merged, every task checked, no open owner question
**Main Branch HEAD at Archive**: 1aba591 (merge of PR #221)

Archived the same day it finished, which `m3a` did not manage and its own report says so.

---

## Artifacts Archived

Moved to `openspec/changes/archive/2026-08-23-m3c-telegram/`:

- `spec.md` — Requirements R0–R6.3
- `design.md` — §1–§11, including the 2026-08-23 owner ruling on redelivery
- `tasks.md` — the five-PR task list, all tasks checked, findings H1–H13, and the verification pass
- `archive-report.md` — this report

## What shipped

The binary has a mouth and an ear. A message from an allowed chat becomes an ordinary capture — the
same `CaptureService` the HTTP route calls — and its result is answered back into the conversation. A
message from anywhere else becomes nothing, with one log line naming the chat id and not its text.

| # | PR | Branch | Merge |
|---|---|---|---|
| — | [#216](https://github.com/rengo/nooma/pull/216) | `plan/m3c-telegram-artifacts` | `a63db8f` |
| 1 | [#217](https://github.com/rengo/nooma/pull/217) | `feat/ports-channel` | `6fc5bf0` |
| 2 | [#218](https://github.com/rengo/nooma/pull/218) | `feat/channels-telegram-client` | `ae2f850` |
| 3 | [#219](https://github.com/rengo/nooma/pull/219) | `feat/channels-telegram-allowlist` | `820608c` |
| 4 | [#220](https://github.com/rengo/nooma/pull/220) | `feat/channels-telegram-resilience` | `4c1fb3e` |
| 5 | [#221](https://github.com/rengo/nooma/pull/221) | `feat/channels-telegram-inbound` | `1aba591` |

**Five PRs, as designed.** PR 2's pre-drawn cut was measured (237 against a 400 budget) and not
needed. No migration, no schema change, no `docs/02-cognitive-core.md` change.

**Merging all five changed the binary's observable behaviour not at all.** The channel is constructed
and not started; `runServe` does not reference it and a test asserts that. `m3d` turns it on in the
same change that gives it something to say.

## R0 was the experiment, and it passed

`docs/02-cognitive-core.md:653` claims: *"Provenance is the caller's fact, never the brain's … Nothing
in the decision layer names a channel, so nothing has to be revisited when one is added."*

This was the first real channel since that was written. **Verified directly**: `git diff
--name-only` across all five PRs returns **zero** files under `internal/core/**`. The boundary was in
the right place.

Two scans keep it that way rather than leaving it as a claim nobody re-checks: one walks
`internal/ports` for a vendor-named identifier, the other `internal/brain` and `internal/core`. They
**parse rather than grep**, because `ports.Channel`'s doc comment legitimately names Telegram as the
first implementation — a byte scan cannot tell a doc comment from a type name and would either fail
on correct code or be weakened until it proved nothing.

## Verification Gate: PASS

`make check-all` green end to end. `internal/core` coverage 100% (896/896).

**Five mutations verified rather than reasoned about.** Each was made, run, and reverted:

| Mutation | What failed |
|---|---|
| A `telegramChatID` type in `internal/ports` | The vendor scan, naming the file, the identifier and the claim at stake |
| The host literal in a test file | The "zero in tests" leg |
| `fmt.Errorf("%w", err)` instead of `sanitize` | The sentinel test, printing the bot token inside a `*url.Error` |
| `base << n` for the backoff | The ceiling leg, at the ninth failure |
| An unbounded map for the dedup ring | Only the growth and eviction legs — every behavioural assertion still passed |
| `http.NewRequest` instead of `NewRequestWithContext` | Only the **L3** shutdown test, after 45 seconds; L2 still passed |

That last row is the one worth keeping: **two tests with similar names were not the same assertion at
two sizes**, and the mutation is what proved it.

---

## Findings — H1 through H13

### The two hazards this slice was the first to reach

**H-class hazard 1 — the token leak the obvious code has.** Telegram puts the bot token in the URL
**path**, and `net/http` renders the full URL into `*url.Error`'s message. So
`fmt.Errorf("telegram: %s: %w", method, err)` — the wrapper anyone would write — puts the token into
every transport error and from there into the operator's log. Closed by `sanitize` on every error
path, whose doc comment states plainly that it is a **denylist**: it redacts the token it was given,
and a future path formatting the URL some other way would still have to come through it.

**H-class hazard 2 — nothing stopped a test dialling the real host.** M2's discharge #4 recorded that
the network half of non-negotiable #5 has no structural guard; until this chain there was nothing to
dial. `test/conformance/telegram_host_literal_test.go` now asserts the literal appears exactly once,
at `telegram.defaultBaseURL`, and **zero times in any `_test.go`**.

Its own construction is the finding worth carrying: **a scan for a literal cannot contain the
literal.** The needle is assembled from three pieces at runtime, with a comment saying the seam is
correct there and nowhere else.

### The findings that changed the shape of the code

| # | Finding | Resolution |
|---|---|---|
| **H1** | The spec's message type omits an id its own R4.1 needs | Under-specification, not disagreement — `ChannelMessage.ID`, opaque above the adapter |
| **H3 / H6** | `m3b`'s scan asserts zero host-literal occurrences under `internal/**`, which this change makes false — **and it contained the literal itself** | Narrowed to `internal/brain`, `internal/scheduler`, `internal/core`, with its markers assembled from parts. H3 predicted the assertion would need narrowing; the red step showed the conflict was direct |
| **H8** | The cursor rule needed a case neither artifact named | R4.1 alone (never advance past anything unconfirmed) would let **one message from a stranger stall the channel forever**, since a refused message is never confirmed by anyone. `nextOffset` tracks admitted-but-unconfirmed ids specifically |
| **H9** | `New` refuses a `bot_token_env` that is **set but empty**, which `internal/config`'s validator does not | A deliberate divergence, recorded because the two are meant to agree |
| **H10** | Design §3.6 sketches the loop calling `CaptureService` inline | A `Handler` func instead — the loop is consumer-independent as well as transport-independent, and `internal/channels` never imports the brain in PR 4 |
| **H11** | Two shutdown tests, L2 and L3 | **Not the same assertion at two sizes.** The fake's `Receive` blocks on `ctx` by construction; reaching a real in-flight request depends on `http.NewRequestWithContext` and nothing else. Recorded because two similarly-named tests is the shape someone deletes one of as redundant |
| **H13** | `RenderReply` needed a case the spec did not name | A recall with no results. *"I could not find anything"* is an answer; an empty list is a message a person reads as a bug |

**H4, H5, H7, H12** are smaller: a deprecated `parser.ParseDir`, where `SentMessage` lives, PR 2's
unused pre-drawn cut, and the scan PR 5 added in place of one PR 1 had already shipped.

---

## Decisions worth carrying to `m3d`

**`Confirm` is its own port method.** Folding it into `Receive` reads fine and hides that the confirm
**is** the durability boundary — a caller that never confirmed would re-read the same message forever
with no method call missing from the trace, and the bug would present as a transport fault.

**Capture, reply, confirm — and a failed reply does not undo the capture.** The capture is durable
and the reply is not. Returning an error there would redeliver the message and duplicate the unit to
retry a sentence.

**A handler error breaks the batch rather than skipping the message.** Whatever failed is likely to
fail for the next one, and confirming past a failure loses a capture one message at a time.

**A message is marked seen before the confirm**, because the confirm is what can fail after the work
is already durable.

**Permanent failure is asked, not known.** The runner asks the error whether retrying can help; a
second channel's permanent failure is its own to name.

---

## Handoffs to `m3d`

- **Starting the channel** is `m3d`'s single wiring PR, alongside the `proactive_check` tick.
  `wireChannel` exists and nothing calls it, asserted by a test.
- **`Send` is what `m3d` delivers through.** Nothing in this change calls it except the reply path.
- **A durable offset**, if wanted, slots into `Confirm` without changing the port. The dedup ring
  does not survive a restart — the owner's 2026-08-23 ruling, stated in its doc comment — so a
  restart between a capture and its confirm duplicates one message.
- **Rate limiting is unmitigated**, and named in design §8: an allowed chat can drive unlimited LLM
  calls. The allow-list is the rate limit, and it is a list of the operator's own chat ids.

## Final State

`internal/ports/channel.go` (`Channel`, `ChannelMessage`, `ConversationID`),
`internal/channels/{runner,dedup,reply}.go`, `internal/channels/telegram/{client,errors,channel,backoff}.go`,
`test/support/fakechannel`, and `wireChannel` in `cmd/nooma`. `docs/01-architecture.md` gained the
channel's paragraph.

**Exit criterion discharged**: a message posted to a fake Telegram server becomes a capture and gets
a reply; a message from a chat id outside the allow-list becomes nothing, audibly.

**Next**: `m3d-delivery-demo` — the `proactive_check` tick, push with quiet-hours deferral, digest
assembly, fire-time rephrasing, the four check-in resolutions, `serve` wiring, and the L4 demo. It is
M3's largest slice, and proposal §5's own threshold applies: if `sdd-tasks`'s forecast exceeds nine
PRs or 3,200 lines, the check-in pair splits off as `m3e-checkins`.
