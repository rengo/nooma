# Design — M3 Phase C: the Telegram channel

Technical design for `m3c-telegram`, derived from `spec.md` (R0–R6.3) and
`openspec/changes/m3-mouth-telegram/proposal.md` §3.2, §4.1, §5, §9, both read in full before this
document. ADR-0006 and ADR-0014 are `Accepted` and are not reopened.

---

## 1. Ground truth this design was verified against

Read at authoring time, not assumed:

- **`internal/channels`** holds `doc.go` and nothing else. There is no channel port, no adapter, no
  fake. This change writes the first of each.
- **`internal/config`** already carries the whole configuration surface: `Channels.Telegram{Enabled,
  BotTokenEnv, AllowedChatIDs}` (`config.go:76-81`) and `checkTelegram` (`validate.go:78-95`), which
  refuses `Enabled` with an empty `AllowedChatIDs`, refuses an empty `BotTokenEnv`, and refuses a
  `BotTokenEnv` naming an unset variable. **Nothing in this change adds a configuration key.**
- **`internal/providers/ollama`** already establishes the injectable-base-URL shape this change
  copies: `NewClient(baseURL, model string, httpClient *http.Client)` with `defaultBaseURL` a
  package constant and `""` meaning "use it" (`client.go:16-36`).
- **`internal/scheduler`** already establishes the logger shape: an `io.Writer` field plus a `logf`
  helper guarded by its own mutex (`scheduler.go:50,61-62,101`) — the mutex added by `m2d`'s
  Judgment Day finding JD-5-01, a real data race. A channel with a polling goroutine and a shutdown
  path has the same shape and inherits the same requirement.
- **`brain.CaptureService.Capture(ctx, CaptureInput{Text, Channel, ReferentID}) (CaptureResult,
  error)`** is the whole inbound surface. `CaptureOutcome` has **seven** members after `m3b`, and
  `AllCaptureOutcomes()` exists so a caller's switch can be proven total.
- **`docs/02-cognitive-core.md:653`** — *"Provenance is the caller's fact, never the brain's … Nothing
  in the decision layer names a channel."*
- **`test/conformance/i03_units_never_deleted_test.go:118-137`** sweeps nine ports interfaces for
  removal-prefixed methods. A tenth is added by this change.

**One thing that is NOT true and is worth stating**: there is no existing structural guard against a
test dialling a real host. `test/e2e/check_demo_test.go` scans for `api.telegram.org` under
`internal/**`, added by `m3b` to prove `m3b` shipped no transport — it is not a general network
guard, and it will start passing vacuously the moment this change adds the constant it looks for.
**§3.2 replaces it rather than leaving two scans disagreeing.**

---

## 2. What `m3c` decides, in one paragraph

A message arrives from Telegram, is admitted or refused by chat id at the point of receipt, becomes
a `brain.CaptureInput` with `Channel = "telegram"`, and its `CaptureResult` is rendered back into
the conversation it came from. Nothing else. The channel opens outbound connections only, holds its
token in memory and never writes it anywhere, advances its read cursor only after a capture is
durable, and is constructed but **not started** — starting it is `m3d`'s wiring PR, because there is
nothing to deliver until `m3d` exists.

---

## 3. Decisions

### 3.1 The port — two methods, and why not one

Spec R1.1 asks for a contract that names no vendor. Three shapes were considered.

| Option | Verdict |
|---|---|
| **A. `Channel` with `Start(ctx, handler func(Message) Reply)`** — the adapter owns the loop and calls up | **Rejected.** Inverts control in a way that puts the reply's *shape* inside the port: the handler's return type becomes part of the contract, and `brain.CaptureResult` is not a thing `internal/ports` can name without importing `internal/brain`, which is the wrong direction. |
| **B. `Channel` with `Receive(ctx) ([]Message, error)` and `Send(ctx, ConversationID, string) error`** — chosen | The adapter owns the transport, the caller owns the loop and the rendering. `Receive` blocks up to the transport's own timeout and returns what it got; `Send` posts one message. Both directions are expressible against a fake with no network, which is what R1.2 asks the port to prove. |
| **C. Two ports, `Inbound` and `Outbound`** | **Rejected.** No caller wants one without the other, and a channel that could receive but not answer is not a conversation. Splitting would be symmetry for its own sake. |

```go
// internal/ports/channel.go
type ConversationID string

type ChannelMessage struct {
	// ID is the transport's own identifier for this message, opaque above
	// the adapter. It exists so a caller can tell a redelivery from a new
	// message (design §3.5).
	ID string
	// Conversation is where a reply goes. Opaque: nothing above the
	// adapter constructs one.
	Conversation ConversationID
	// Text is the message body, verbatim.
	Text string
	// Channel is this channel's own name, and becomes
	// brain.CaptureInput.Channel and therefore units.source.
	Channel string
}

type Channel interface {
	// Name is the channel's own name.
	Name() string

	// Receive returns every message admitted since the last call, blocking
	// up to the transport's own timeout. An empty slice and a nil error is
	// the ordinary quiet case, not a failure.
	Receive(ctx context.Context) ([]ChannelMessage, error)

	// Confirm tells the channel every message up to and including id has
	// been handled durably, and need not be delivered again. Separated
	// from Receive on purpose — see §3.5.
	Confirm(ctx context.Context, id string) error

	// Send posts text into conversation.
	Send(ctx context.Context, conversation ConversationID, text string) error

	// Close releases the channel's resources.
	Close() error
}
```

**`Confirm` is a third method rather than a parameter on the next `Receive`**, and that is the
port's one non-obvious shape. Folding it in — `Receive(ctx, confirmedThrough string)` — would make
the contract *"tell me what you handled while asking for more"*, which reads fine and hides the
thing spec R4.1 cares about: **the confirm is the durability boundary**, and a caller that forgot to
pass it would silently keep re-reading the same message with no method call missing from the trace.
As its own method it is a line a reviewer can find.

**No `Delete`-prefixed method**, and the I03 sweep gains `ports.Channel` in PR 1.

### 3.2 The host literal, and replacing `m3b`'s scan rather than adding a second

Spec R2.1 forbids `api.telegram.org` outside one named constant and anywhere in tests. `m3b` already
ships a scan for that literal (`test/e2e/check_demo_test.go`), written to prove `m3b` opened no
channel — and it asserts **zero** occurrences under `internal/**`. This change makes that assertion
false by design.

**Two scans disagreeing is worse than one scan moved.** So:

- `m3b`'s scan is **narrowed, not deleted**: it keeps asserting that `internal/brain/**`,
  `internal/scheduler/**` and `internal/core/**` mention no Telegram marker, which is the claim that
  is still true and still worth holding — a channel adapter existing must not mean the brain learned
  a vendor's name.
- The new scan in `internal/channels/telegram` asserts the literal appears **exactly once**, at the
  named constant, and **zero times** in any `_test.go` file anywhere in the repository.

Both parse Go source with `go/ast` rather than grepping bytes, so a comment naming the host neither
trips nor satisfies them — `m2d`'s JD-4-01 found exactly that defect in a byte-comparing scan, and
the lesson is not re-learned here.

### 3.3 Admission, and where the refusal is recorded

Spec R3.1 puts the allow-list at receipt. Concretely: inside `Receive`, after decoding Telegram's
update envelope and before constructing a `ports.ChannelMessage`. A refused update **is still
confirmed** — it advances the offset — because a message that will never be admitted must not be
redelivered forever.

That is the one place this design lets an update advance the offset without a capture, and it is
deliberate: R4.1's rule exists so a *capture* is never lost, and a refused message has no capture to
lose.

**Where the refusal is recorded**: the channel's own `io.Writer` log, one line naming the refused
chat id and nothing else about the message. Spec R3.1 already rules out `decision_log`; what it does
not say, and this design does, is that **the refused message's text is not logged**. A message from
an unknown sender is untrusted input from an unknown party; writing its body into the operator's log
turns an access refusal into an injection surface for whoever finds the bot.

### 3.4 The token, and the leak the naive wrapper produces

Telegram's API puts the token in the **path**: `https://api.telegram.org/bot<TOKEN>/getUpdates`.
`net/http` returns `*url.Error`, whose `Error()` includes the full URL. So:

```go
resp, err := c.httpClient.Do(req)
if err != nil {
    return nil, fmt.Errorf("telegram: getUpdates: %w", err)   // leaks the token
}
```

Every transport error written that way carries the bot token into the log. **This is the default
behaviour of the obvious code**, which is why spec R3.3 states it as a MUST rather than trusting
care.

**The fix is one helper, used everywhere**: `sanitize(err)` returns an error with the token replaced
by a fixed redaction marker, and the client's every error path goes through it. Not a `String()`
method on a token type — a token type would still be interpolated into the URL, and the leak is in
`url.Error`'s own rendering of a string the type never touched.

The token is read once, at construction, via the `lookup func(string) (string, bool)` shape
`internal/config`'s validator and `cmd/nooma`'s wiring already use — so a test injects it without
touching the process environment.

### 3.5 Redelivery — owner ruling, 2026-08-23

Spec §7's Q1 asked what the inbound path does with a redelivered update. **Owner ruling: bounded
in-memory deduplication.**

The loop remembers the ids it has already captured, in a bounded set, and drops a message whose id
it recognises before calling `CaptureService`. It survives a redelivery **within one process
lifetime**, which is the case R4.1 actually creates, and it does not survive a restart — which is
stated in the doc comment rather than left for someone to discover.

**The bound is a fixed-capacity ring, not a growing map.** A polling loop runs for the process's
whole life, and an unbounded set of every id ever seen is a slow leak with no ceiling. The capacity
is a named constant derived from Telegram's own retention: an update is redelivered only until it is
confirmed, and the loop confirms after every batch, so the window that needs remembering is one
batch plus whatever failed in it — the constant is that, with generous headroom, and its doc comment
says so.

**Not chosen, and why it stays available**: persisting the last confirmed update id in the vault
would survive restarts, and it needs a store surface this change's scope boundary excludes. `m3d`
can add it without changing the port — `Confirm` already names the durability boundary, so a durable
implementation of it is a swap inside the adapter.

### 3.6 The loop, and who owns it

`Channel` has no `Start`. The loop lives in a `channelRunner` in `internal/channels` (not in
`telegram`), because it is transport-independent: receive, admit, dedup, capture, confirm, reply. A
second channel would reuse it whole.

```
for ctx not done:
    msgs, err := ch.Receive(ctx)
    on err  → backoff(err), continue          (§3.7)
    on ok   → backoff reset
    for each msg:
        if seen(msg.ID)          → skip
        result, err := capture.Capture(ctx, CaptureInput{Text: msg.Text, Channel: ch.Name()})
        on err  → log, do NOT confirm, do NOT mark seen, break   (R4.1)
        mark seen(msg.ID)
        ch.Send(ctx, msg.Conversation, render(result))
        ch.Confirm(ctx, msg.ID)
```

**`Send` before `Confirm`, and a failed `Send` does not block the confirm.** The capture is the
durable thing; the reply is not. A reply that fails to post is logged and the message stays
confirmed, because re-running the capture to retry a reply would duplicate the unit — trading an
unrecoverable loss for a recoverable one, backwards.

**A capture error breaks the batch rather than skipping the message.** Whatever failed is likely to
fail for the next message too (a closed vault, a dead provider), and confirming past a failure is
how R4.1's rule gets violated one message at a time.

### 3.7 Backoff

Two failure classes, and only one is transient.

| Class | Behaviour |
|---|---|
| Transport error, `5xx`, malformed body, `ok: false` with any code but 401 | Exponential backoff from `pollBackoffBase`, doubling, capped at `pollBackoffMax`. Reset to base on the first success |
| **`401`** | **Stop the loop and return.** A wrong or revoked token does not become right by waiting; a channel that retries it forever looks alive while being permanently deaf, which is the failure ADR-0006's "the channel does not start" posture exists to avoid — one layer later |

The backoff is a **pure function** `backoffFor(consecutiveFailures int) time.Duration`, so it is
tested at L1 with no clock and no sleeping. The loop's sleep takes the duration and a `ctx`, so
shutdown interrupts it (§3.8).

### 3.8 Shutdown

`Receive` blocks up to Telegram's long-poll timeout — 30 seconds is the value this design picks
(`pollTimeoutSeconds`), well inside `serve`'s shutdown grace but far too long to wait for.

**`ctx` cancellation is the mechanism, and it reaches the HTTP request** because the request is
built with `http.NewRequestWithContext`. Cancelling the loop's context cancels the in-flight
`getUpdates`, which returns a context error the loop recognises as shutdown rather than as a
transport failure to back off from.

The in-flight update's outcome satisfies R4.3 by construction: either its capture completed and it
was confirmed, or it was not confirmed and Telegram redelivers it. There is no third state, because
`Confirm` is only ever called after `Capture` returns nil.

---

## 4. Package layout and dependency map

```
internal/ports/channel.go          ← Channel, ChannelMessage, ConversationID
internal/channels/runner.go        ← channelRunner: the transport-independent loop
internal/channels/telegram/
    client.go                      ← the Bot API client, injectable baseURL
    channel.go                     ← ports.Channel over that client: admission, offset
    errors.go                      ← APIError, sanitize
test/support/fakechannel/          ← in-memory ports.Channel, no network
test/support/repocontract/channel.go ← the shared contract both implementations answer
cmd/nooma/wiring.go                ← wireChannel; NOT started (R6.2)
```

`internal/channels/**` imports `internal/ports`, `internal/brain` (for `CaptureService` and
`CaptureResult`, in the runner only) and the standard library. It imports **no** `internal/core`
package, and nothing imports it except `cmd/nooma`.

`internal/ports/channel.go` imports the standard library only.

---

## 5. File changes

| File | Action | PR |
|---|---|---|
| `internal/ports/channel.go` | Create | 1 |
| `test/support/fakechannel/fakechannel.go` | Create | 1 |
| `test/support/repocontract/channel.go` | Create | 1 |
| `test/conformance/i03_units_never_deleted_test.go` | Modify — `ports.Channel` added to the sweep | 1 |
| `internal/channels/telegram/{client,errors}.go` | Create | 2 |
| `internal/channels/telegram/channel.go` | Create | 3 |
| `test/e2e/check_demo_test.go` | Modify — the host scan narrowed (§3.2) | 2 |
| `internal/channels/runner.go` | Create | 4, extended in 5 |
| `cmd/nooma/wiring.go` | Modify — `wireChannel` | 5 |
| `docs/01-architecture.md` | Modify — the channel's row | 5 |

**No migration. No `docs/02-cognitive-core.md` change** — this change decides nothing the brain
decides, which is R0 restated as a file list.

---

## 6. The five PRs

Chain `stacked-to-main`, delivery `auto-chain`.

| # | Branch | Content | Impl+docs |
|---|---|---|---|
| 1 | `feat/ports-channel` | The port, the fake, the contract suite, the I03 sweep widened | ~250 |
| 2 | `feat/channels-telegram-client` | `getUpdates`/`sendMessage`, `APIError`, `sanitize`, the host scan moved | ~400 |
| 3 | `feat/channels-telegram-allowlist` | Admission at receipt, the token, the refusal log | ~300 |
| 4 | `feat/channels-telegram-resilience` | Backoff, the offset, dedup, shutdown | ~350 |
| 5 | `feat/channels-telegram-inbound` | The runner's capture + reply, `wireChannel`, doc 01 | ~350 |

**~1,650 budgeted impl+docs lines**, matching proposal §5.1. **PR 2 is the one at risk** at 400: its
natural cut is the client (`getUpdates` + `sendMessage`) from the error taxonomy (`APIError` +
`sanitize` + the host scan), giving a sixth PR `feat/channels-telegram-errors`. `sdd-tasks` applies
that cut if its own forecast exceeds 400 — reported before splitting, never split silently.

---

## 7. Testing strategy

| Layer | What | Where |
|---|---|---|
| **L1** | `backoffFor` is a pure function of the failure count | `internal/channels/telegram/backoff_test.go` (PR 4) |
| **L1** | The dedup ring evicts oldest-first and is bounded | `internal/channels/runner_test.go` (PR 4) |
| **L2** | The port contract, against the fake | `test/conformance/` (PR 1) |
| **L2** | `ports.Channel` declares no removal-prefixed method | `i03_…_test.go` (PR 1) |
| **L2** | `internal/ports/**` contains no identifier matching "telegram" | `test/conformance/` (PR 1) |
| **L2** | The host literal appears once, at the named constant, and never in a `_test.go` | `test/conformance/` (PR 2) |
| **L2** | The token appears in no error and no log line, across three failure shapes | `internal/channels/telegram/` (PR 3) |
| **L2** | The reply mapping is total over `brain.AllCaptureOutcomes()` | `test/conformance/` (PR 5) |
| **L3** | The same port contract, against the real client over `httptest` | `internal/channels/telegram/…_integration_test.go` (PR 2) |
| **L3** | Admission: allowed → capture; refused → zero units, zero replies, one log line | PR 3 |
| **L3** | The offset advances only after a durable capture; a failed capture redelivers | PR 4 |
| **L3** | `Close` during an in-flight poll returns within the grace | PR 4 |
| **L4** | A message posted to a fake Telegram server becomes a unit and gets a reply | `test/e2e/` (PR 5) |

**Every test constructs its client against an `httptest.Server`.** The guard is R2.1's scan, not
discipline.

**The fake channel is not a second implementation of Telegram.** It holds a slice of messages and a
slice of sent replies. If a contract case cannot be written against it, the case belongs to the
adapter's own L3 suite, not to the port.

---

## 8. Threat matrix

Unlike `m3b`, this one is real. Every row is applicable and answered.

| Boundary | Assessment |
|---|---|
| **Inbound network** | **None, by construction.** ADR-0014: long polling only. The binary opens no port, needs no DNS name, no certificate, no public address. The channel makes outbound connections and nothing else, and a webhook is out of scope for v1 rather than unimplemented |
| **Untrusted input → the brain** | A message body from an allowed chat reaches `classify` as prompt text. That surface is M1's and unchanged: `classify.BuildPrompt` renders it verbatim into a prompt whose response is decoded by a schema with `DisallowUnknownFields` and a closed vocabulary per field. This change adds no new parsing of message text |
| **Untrusted input → the operator's log** | **Closed here.** A refused message's chat id is logged; its **text is not** (§3.3). Logging an unknown party's message body turns an access refusal into an injection surface for whoever finds the bot |
| **Credential exposure** | **The active risk, and the one the obvious code gets wrong.** The token is in the URL path and `net/http` puts the URL in `*url.Error`, so the naive `fmt.Errorf("%w", err)` leaks it into every transport error (§3.4). Closed by `sanitize` on every error path, asserted by a sentinel-token test across three failure shapes |
| **Credential at rest** | The token is never in the vault, never in `nooma.yml`, and read from the environment variable `bot_token_env` names — ADR-0006's decision, already enforced by `internal/config`'s validator |
| **Authorization** | `allowed_chat_ids` at receipt (§3.3), and the adapter refuses to construct with an empty list even if the validator was skipped (R3.2). Two independent refusals, which is what makes it structural rather than checked |
| **Denial of service** | An allowed chat can send unlimited messages, each costing an LLM call. **Not mitigated in v1, and named rather than omitted**: the allow-list is the rate limit, and it is a list of the operator's own chat ids. A rate limiter is `m3d`'s or later, and there is no sensible default before someone has run it |
| **Reply injection** | A `CaptureResult` renders into a reply. The rendering is a total switch over a closed vocabulary producing fixed sentences plus the user's own content — no message the user sends can make the channel send something structurally different |
| **Shell / subprocess** | N/A — no `os/exec` |
| **VCS/PR automation** | N/A |

---

## 9. Migration / rollout

**No migration.** No schema change, no configuration key added, no `store_api.golden` change.

**The channel is constructed and not started** (R6.2). Merging all five PRs changes the binary's
observable behaviour **not at all**: `serve` runs exactly as it does today. `m3d`'s wiring PR is
what turns it on, in the same change that gives it something to say.

That is deliberate, and it is the rollout plan: five reviewable PRs land a whole subsystem with zero
behavioural risk, and one later PR flips it on once there is a reason to.

---

## 10. Owner-review items

Decided defaults. Ship as designed unless the owner says otherwise.

| # | Item | Decided |
|---|---|---|
| R1 | `Confirm` is its own port method rather than a `Receive` parameter | As designed (§3.1) |
| R2 | A refused message advances the offset | As designed (§3.3) — it has no capture to lose |
| R3 | A failed `Send` does not block the `Confirm` | As designed (§3.6) |
| R4 | A capture error breaks the batch rather than skipping one message | As designed (§3.6) |
| R5 | `401` stops the loop rather than backing off | As designed (§3.7) |
| R6 | Poll timeout 30s | As designed (§3.8) |
| **Q1** | **Redelivery handling** | **Ruled 2026-08-23: bounded in-memory dedup** (§3.5) |

---

## 11. Risks this design adds or sharpens

| # | Risk | Posture |
|---|---|---|
| A | **The dedup ring does not survive a restart.** A restart between capture and confirm duplicates one message | Accepted, and stated in the doc comment. The durable fix needs a store surface this change excludes; `Confirm` is already the seam it would slot into |
| B | **`sanitize` is a denylist.** It redacts the token it knows; a future error path that formats the URL some other way could still leak | Mitigated by routing every error through one constructor, and asserted by a sentinel test rather than by review. Named because a denylist is what it is |
| C | **The loop has no rate limit.** An allowed chat can drive unlimited LLM calls | Accepted for v1 (§8) |
| D | **`Receive` returning a batch means one slow capture delays the rest of that batch** | Accepted — batches are small in a personal vault, and processing them concurrently would break R4.1's ordering guarantee for the offset |
| E | **PR 2 is budgeted at 400 with no margin** | Its cut is pre-drawn (§6), to be applied on measurement rather than on feel |
