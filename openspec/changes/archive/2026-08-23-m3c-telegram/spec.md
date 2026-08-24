# Spec — M3 Phase C: the Telegram channel

Delta spec for `m3c-telegram`, the third of M3's four chained changes (`m3a` → `m3b` → **`m3c`** →
`m3d`). Derived from `openspec/changes/m3-mouth-telegram/proposal.md` §3.2, §4.1 and §5, and from
ADR-0006 and ADR-0014, both `Accepted` and neither reopened here.

`m3a` decided **when** to speak. `m3b` decided **what** comes due and wrote it down. This change
gives the binary a **mouth and an ear**, and nothing else: a message arrives, becomes a capture, and
gets an answer. It delivers no nudge, fires no timer and assembles no digest — every one of those is
`m3d`'s, and this change ships no code that could do them.

---

## Scope boundary (binding)

**In**: `internal/ports/channel.go` (new), `internal/channels/telegram/**` (new),
`test/support/fakechannel/**` (new), `cmd/nooma/wiring.go` (the channel's construction only).

**Out**: `internal/core/**` — see R0. `internal/brain/**` — the inbound path calls the existing
`brain.CaptureService` and adds nothing to it. `internal/scheduler/**` — the `proactive_check` tick
is `m3d`'s. `internal/store/**` — this change persists nothing of its own; every row it causes is
written by `CaptureService`, through code `m1` already shipped and `m3b` already extended.

**No migration. No `docs/03-data-model.md` change. No schema-golden change. No `§13` calibration
row** — every number this change introduces is a transport parameter (a poll timeout, a backoff
ceiling), not a behavioural one, and §13 is for numbers that change what the brain decides. R6.3
states where they live instead.

### R0 — no `internal/core` change

**MUST**: no file under `internal/core/**` is created or modified by this change.

The reason is doc 02's own, at line 653: *"Provenance is the caller's fact, never the brain's.
Which channel a capture arrived through … travels inward as data. Nothing in the decision layer
names a channel, so nothing has to be revisited when one is added."* This change is the first test
of that claim since it was written. If adding the first real channel requires touching core, the
claim was wrong and the boundary is in the wrong place — so **R0 is not bookkeeping here, it is the
experiment.**

**Verified by**: the existing `depguard` `core-purity` rule, plus this change's own PR diffs.

---

## 1. The channel port — PR `feat/ports-channel`

### R1.1 — `internal/ports` declares a channel contract that names no vendor

**MUST**: `internal/ports/channel.go` declares a `Channel` interface carrying the two directions a
conversation needs and nothing else, in vocabulary that mentions no vendor: no `Telegram`, no
`chat_id`, no `update`, no `getUpdates`.

**MUST**: the inbound direction delivers **messages already admitted** — a message a `Channel`
hands upward has passed whatever admission rule that channel has. The port declares no allow-list,
because an allow-list is a property of a specific transport's identity space and `internal/ports`
has no way to express "chat id" without naming Telegram.

**MUST**: the message type carries the text, the conversation identity the channel must reply into,
and the channel's own name — the string that becomes `brain.CaptureInput.Channel` and therefore
`units.source`. It carries no vendor id type: a conversation identity is opaque to everything above
the adapter.

**MUST NOT**: any method whose name begins `Delete`, `Remove`, `Purge`, `Drop` or `Destroy` — I03's
strengthened prefix set, applied to this port as it is to every repository port
(`test/conformance/i03_units_never_deleted_test.go`).

**Verified by**: L2 — a reflection scan asserting the interface's method set carries no forbidden
prefix, and a source scan asserting no identifier under `internal/ports/**` contains "telegram"
case-insensitively.

### R1.2 — a fake `Channel` exists in `test/support`, and it is what proves the port is vendor-free

**MUST**: `test/support/fakechannel` ships an in-memory `ports.Channel` that speaks no HTTP, holds
no token and knows nothing about Telegram, and the shared contract suite in `test/support/repocontract`
runs against it.

**MUST**: the same contract suite runs against `internal/channels/telegram`'s implementation at L3
— design D6's "answered twice" standing rule, which this repository applies to every port with more
than one implementation.

**Rationale, stated because it is the whole argument for a port here**: a channel interface with
exactly one implementation is a layer of indirection, not a boundary. The fake is what makes it a
boundary — if the port can be satisfied by something with no network at all, then nothing above it
depends on Telegram, which is the claim R0 exists to test.

**Verified by**: L2 against the fake, L3 against the real client over `httptest`.

---

## 2. The Telegram client — PR `feat/channels-telegram-client`

### R2.1 — `getUpdates` and `sendMessage` over an injectable base URL

**MUST**: `internal/channels/telegram` implements `ports.Channel` over Telegram's Bot API using
**long polling only** (`getUpdates`). No webhook, no inbound listener, no port opened — ADR-0014,
`Accepted`, and this change does not reopen it.

**MUST**: the client's base URL is a **constructor parameter**, not a package constant. Every test
constructs the client against an `httptest.Server`.

**MUST NOT**: the literal string `api.telegram.org` appears anywhere under `internal/channels/**`
outside a single named default constant, and appears in no test file at all.

**Rationale**: proposal §9's risk R5 records that *"nothing prevents a test dialling
`api.telegram.org`"*, and that M2's discharge #4 already found the network half of non-negotiable
#5 ("no test touches the network or a real LLM") has no structural guard. **This change is the first
slice where a copy-pasted real URL would pass CI**, so it is the slice that owes the guard.

**Verified by**: L2 — a source scan over `internal/channels/**` asserting the host literal appears
exactly once, in the file and at the identifier the scan names, and zero times in any `_test.go`
file. The scan parses Go source rather than grepping raw bytes, so a comment mentioning the host
neither trips it nor satisfies it.

### R2.2 — an API error is not a transport error, and neither is a Go error the caller must guess at

**MUST**: Telegram's own `{"ok": false, "error_code": …, "description": …}` envelope is decoded and
surfaced as a distinct, named error carrying the code and the description. A caller must be able to
tell "Telegram refused this" from "the connection failed" without string-matching.

**MUST**: a `401 Unauthorized` — a wrong or revoked bot token — is distinguishable from every other
API error, because it is the one that will never succeed on retry and must not be backed off
forever (R4.2).

**Verified by**: L3 against `httptest` — a fixture per case: transport failure, `ok: false`, `401`,
and a malformed body.

---

## 3. Admission — PR `feat/channels-telegram-allowlist`

### R3.1 — `allowed_chat_ids` is enforced at receipt, before anything else happens

**MUST**: a message whose chat id is not in `channels.telegram.allowed_chat_ids` is **dropped at the
point of receipt**: it never becomes a `ports.ChannelMessage`, never reaches `CaptureService`, never
produces a unit, and is never replied to.

**MUST NOT**: the check happens anywhere above the adapter. Nothing in `internal/brain` may be the
thing that enforces it, because a second channel added later would then need its own copy of the
same rule.

**MUST**: a refused message is recorded — the channel is not silent about it. ADR-0006 makes the
allow-list mandatory precisely because *"anyone who finds the bot could talk to this brain"*, so a
message from outside it is a security-relevant event, and one that leaves no trace is one nobody can
notice.

**Rationale for where the record goes**: not `decision_log`. That table is doc 02 §11's glass box for
**the brain's own decisions**, and refusing an unknown sender is a transport-level access decision
the brain never saw. It is logged through the channel's own logger. This is stated here rather than
left to the design because putting it in `decision_log` would be the easy, wrong answer.

**Verified by**: L3 — an update from an allowed chat becomes a capture; an update from a
non-allowed chat produces zero captures, zero units and zero replies, and one log line naming the
refused chat id.

### R3.2 — an empty allow-list does not start the channel

**MUST**: constructing the Telegram channel with `Enabled: true` and an empty `AllowedChatIDs`
**fails** rather than starting permissively. `internal/config`'s validator already refuses that
combination (`validate.go:86`), and this requirement makes the adapter refuse it too, so the
property survives a caller that skipped validation.

**Rationale**: CLAUDE.md non-negotiable #7 — *"Safe defaults are structural, not warnings. Without
`allowed_chat_ids` the channel does not start."* Two independent refusals is not redundancy here;
it is the difference between a rule the configuration layer happens to check and a rule the channel
cannot be made to break.

**Verified by**: L2 — the constructor returns an error for the empty case, and the error names the
configuration key.

### R3.3 — the token is read from the environment, once, and never logged

**MUST**: the bot token is read from the environment variable `channels.telegram.bot_token_env`
names, at construction, and held in memory. It is never read from the config file, never written to
one, and never appears in any log line, error message or `String()` method.

**MUST**: the token does not appear in an error even when the request that failed carried it in its
URL — Telegram's API puts the token in the **path** (`/bot<token>/getUpdates`), so a naive
`fmt.Errorf("%w", err)` around an `*url.Error` leaks it into every transport error. **This is not a
hypothetical**: `net/http`'s own error strings include the URL.

**Verified by**: L2/L3 — a fixture whose token is a known sentinel string, asserting the sentinel
appears in no error returned and in no line written to the channel's logger, across a transport
failure, an API error and a successful call.

---

## 4. Resilience — PR `feat/channels-telegram-resilience`

### R4.1 — the update offset is confirmed only after the capture is persisted

**MUST**: `getUpdates`'s `offset` parameter advances past an update **only after** that update's
capture has been persisted successfully. An update whose capture failed is not confirmed, and is
redelivered on the next poll.

**MUST**: the inbound path therefore tolerates **one redelivery of an already-processed message**
without producing a second unit for it, or states plainly why it cannot — see the open question in
§7.

**Rationale**: proposal §9's risk R7, stated before it is discovered rather than after: *"`getUpdates`'s
offset is process state. After a restart, a confirmed-but-unprocessed update is gone and an
unconfirmed one redelivers."* Confirming first loses messages; confirming last duplicates them.
**Losing a capture is unrecoverable and duplicating one is not**, which is the same asymmetry
`m3a`'s F6 used to put the delay-caveat boundary on the cheap side.

**Verified by**: L3 — a capture that fails leaves the offset unadvanced and the update is seen
again; a capture that succeeds advances it and the update is not.

### R4.2 — backoff is bounded, and a permanent failure is not retried as though it were transient

**MUST**: consecutive polling failures back off, bounded by a ceiling, and recover to the base
interval on the first success.

**MUST**: a `401` (R2.2) is **not** retried on the transient path. A wrong token does not become
right by waiting, and a channel that retries it forever looks alive while being permanently deaf.
What it does instead — stop, or surface — is the design's to decide and its decision is binding.

**MUST**: no backoff constant is a magic number inside a function. Each is a named constant with a
doc comment saying what it is derived from.

**Verified by**: L2 with an injected clock — the backoff sequence is a pure function of the failure
count and is asserted as one.

### R4.3 — shutdown is prompt, and an in-flight long poll does not hold it

**MUST**: the channel stops within the server's shutdown grace, and stopping does not wait for an
in-flight `getUpdates` to return on its own — a long poll legitimately holds a connection open for
its full timeout.

**MUST**: shutdown does not lose an update whose capture is already in progress: either it
completes and is confirmed, or it is left unconfirmed and redelivered (R4.1). It is never confirmed
without having been persisted.

**Verified by**: L3 — a `Stop` issued while a poll is in flight returns within the grace, and the
in-flight update's outcome satisfies R4.1's disjunction.

---

## 5. Inbound — PR `feat/channels-telegram-inbound`

### R5.1 — an admitted message becomes a capture and gets a reply

**MUST**: an admitted inbound message is handed to `brain.CaptureService.Capture` with
`CaptureInput.Text` set to the message text verbatim and `CaptureInput.Channel` set to the
channel's own name, and the resulting `brain.CaptureResult` is rendered into a reply posted back to
the originating conversation.

**MUST**: the reply is **total over `brain.AllCaptureOutcomes()`** — every one of the seven outcomes
renders to a distinguishable reply, and a member added later without a rendering fails a test rather
than silently producing an empty message. `internal/httpapi` and `cmd/nooma` already carry that
property for their own surfaces; this is the third.

**MUST NOT**: the channel re-derives anything the capture already decided. It renders
`CaptureResult` and nothing else — no second classification, no second recall, no opinion about
what the outcome means.

**Verified by**: L2 — a total-mapping test over `AllCaptureOutcomes()`, the shape
`TestAllCaptureOutcomesHaveAStatusMapping` already established. L3 — a message posted to a fake
Telegram server becomes a unit and produces a reply.

### R5.2 — the channel adds no path a capture could take that the HTTP surface cannot

**MUST**: the inbound path calls the same `brain.CaptureService.Capture` the HTTP route calls, with
no channel-specific branch inside `internal/brain`.

**Rationale**: I22's own shape, one layer out — *"one mechanism, two entrances"*. A channel that
reached into the pipeline would be a second implementation of capture, and the first divergence
would be invisible until someone compared two surfaces nobody compares.

**Verified by**: L2 — a source scan asserting `internal/brain/**` contains no identifier naming a
channel or a vendor.

---

## 6. Cross-cutting

### R6.1 — every test runs against `httptest`, and that is structural

**MUST**: no test in this change opens a connection to any host outside `httptest`'s own loopback
listener. Enforced by R2.1's scan rather than by discipline.

### R6.2 — the channel is constructed in `cmd/nooma/wiring.go` and started by nothing yet

**MUST**: `wireChannel` (or its named equivalent) exists and is unit-tested, and **`runServe` does
not start the channel in this change**. Wiring the channel into the server's lifecycle, alongside
the `proactive_check` tick, is `m3d`'s single wiring PR.

**Rationale**: a channel that starts polling the moment this change merges would make every
subsequent PR in the chain run against a live poller in `serve`, and would deliver nothing when it
did — there is no delivery path until `m3d`. Shipping the constructor without the start is what
keeps the change reviewable and the binary's behaviour unchanged.

**Verified by**: L2 — the constructor is tested directly; a source scan asserts `runServe` does not
reference it.

### R6.3 — transport parameters are named constants, and they are not §13 rows

**MUST**: the poll timeout, the backoff base and its ceiling are named constants in
`internal/channels/telegram` with doc comments deriving them.

**MUST NOT**: any of them appears in `docs/02-cognitive-core.md` §13. That table calibrates what the
**brain decides**; a poll timeout changes nothing the brain would decide differently. The rule this
follows is the one `internal/ports`' own status vocabularies already follow — outside §13's reach,
pinned instead by a test in their own layer.

---

## 7. What this spec does not require, and one question it leaves open

**Not required**: delivery of anything. No trigger fires through this channel, no timer is
surfaced, no digest is assembled, no check-in is asked. `m3d` owns all of it.

**Not required**: a second channel. The port exists to make the boundary real, not to ship WhatsApp.

**Not required**: editing or deleting a sent message; reactions; media; commands (`/start`).

**Open question, blocking for PR 5's design and named rather than assumed** — **Q1: what does the
inbound path do with a redelivered update it has already captured?** R4.1 makes redelivery possible
by construction. Three answers, and the change cannot pick one silently:

- **(a) Nothing — accept the duplicate.** A redelivered message becomes a second unit. Honest,
  trivial, and wrong in the one case it matters: a restart mid-capture duplicates the user's note.
- **(b) Remember the last confirmed update id in the vault**, so the offset survives a restart. Turns
  process state into vault state, which needs a store surface this change's scope boundary excludes.
- **(c) Deduplicate on the update id in memory**, bounded. Survives a redelivery within one process
  lifetime — which is R4.1's own case — and not a restart, which is (b)'s.

The proposal's own risk R7 says only *"inbound handling must tolerate one redelivery"*, which is
(a) or (c) but not (b). **Recommendation: (c)**, because it discharges the risk as stated, needs no
store surface, and leaves (b) available to `m3d` if a restart-durable offset is ever wanted. The
design decides; this spec records that it must.

---

## Exit criterion (this change's own success condition)

A message posted to a **fake Telegram server** becomes a capture and gets a reply; a message from a
chat id outside the allow-list becomes nothing — no unit, no reply — **audibly**, with one log line
naming the refused chat id.

And, structurally: the literal `api.telegram.org` appears exactly once in the repository outside
documentation, at a named default constant, and in no test.
