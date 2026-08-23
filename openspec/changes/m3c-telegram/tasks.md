# Tasks — M3 Phase C: the Telegram channel

Implementation task list for `m3c-telegram`, derived from `spec.md` (R0–R6.3) and `design.md`
(§1–§11 plus the 2026-08-23 owner ruling), both read in full before this document. Design §6 fixes
the slicing — **five PRs**, stacked, ~1,650 budgeted impl+docs lines.

Chain strategy **`stacked-to-main`**, delivery **`auto-chain`**. Order is linear: `1 → 2 → 3 → 4 →
5`. Nothing here is independent of what precedes it, unlike `m3b`'s PR 3.

**Strict TDD is active.** Every behavioural task states the two-commit RED/GREEN shape, and inside
every PR the conformance/L1 test commit is strictly ahead of the implementation commit. **For every
RED/L2/L3 task, a `Mutation:` line names the code change that must make the test fail** — a task
without one is not checkable from the tree alone.

---

## Findings — spec/design disagreements found in this session (report, don't paper over)

**H1 — spec R1.1 describes a message type carrying "the conversation identity the channel must reply
into" and does not name a message id; design §3.1 adds `ChannelMessage.ID`.** The spec's own R4.1
requires confirming an offset per update and its §7 Q1 asks about redelivered updates, neither of
which is expressible without an id crossing the port. **Resolved in design's favour**: the spec
under-specified rather than disagreeing — R1.1's list is what a *reply* needs, and R4.1's cursor
needs one more thing. `ID` is opaque above the adapter, so no vendor concept leaks with it.

**H2 — spec R6.2 says `wireChannel` "or its named equivalent" exists and `runServe` does not start
the channel; design §9 states merging all five PRs changes observable behaviour "not at all".**
These are consistent, and the combination is worth stating because it looks like dead code: **PR 5
ships a constructor with no production caller**, exactly as `m3b` PR 3 shipped
`LiveFocusCandidates` with none. The precedent and its reasoning (`ports.StateRepo` declared ahead
of its implementation) apply unchanged, and task 5.6 asserts the absence rather than leaving it to
be noticed.

**H3 — design §3.2 modifies a test `m3b` shipped, which is outside this change's stated scope
boundary.** `spec.md`'s scope box lists `internal/ports`, `internal/channels`, `test/support` and
`cmd/nooma/wiring.go`; `test/e2e/check_demo_test.go` is none of those. **The modification is
necessary and is recorded here rather than smuggled**: that test asserts zero occurrences of
`api.telegram.org` under `internal/**`, which this change makes false by design. Narrowing it (to
`internal/brain`, `internal/scheduler`, `internal/core`) preserves the claim still worth holding.
Leaving it would make PR 2 red; deleting it would drop a real guard.

**H4 — `parser.ParseDir` is deprecated as of Go 1.25, and the first version of task 1.3's scan used
it.** Deprecated for not considering build tags when associating files with packages — which for a
scan over `internal/ports` (no build tags anywhere) changes nothing about the result, but
`staticcheck` is in the lint gate and a deprecation is a deprecation. Rewritten to reuse the
per-file `parser.ParseFile` walker task 5.4's brain scan already needed, so the two scans share one
implementation instead of having two. **Falsifiability was re-verified after the rewrite rather than
carried over from the first version** — a probe passing before a refactor says nothing about after.

**H5 — the harness's `Sent` returns `repocontract.SentMessage`, a type the port does not declare.**
A sent message has a conversation and a text and no id or channel, so `ports.ChannelMessage` is the
wrong shape for it, and adding an outbound type to `internal/ports` would put a type there with no
production reader — `Send` takes its two fields as parameters. `SentMessage` therefore lives in
`repocontract`, beside the harness that is its only consumer, the same way `EmbeddingHarness`'s own
helpers do.

---

## Owner-review items carried forward (design §10 — decided defaults, ship if the owner is silent)

| # | Item | Decided | PR / Task |
|---|---|---|---|
| R1 | `Confirm` is its own port method | As designed | PR 1, task 1.2 |
| R2 | A refused message advances the offset | As designed | PR 3, task 3.4 |
| R3 | A failed `Send` does not block the `Confirm` | As designed | PR 5, task 5.3 |
| R4 | A capture error breaks the batch | As designed | PR 5, task 5.2 |
| R5 | `401` stops the loop rather than backing off | As designed | PR 4, task 4.3 |
| R6 | Poll timeout 30s | As designed | PR 2, task 2.2 |
| **Q1** | Redelivery handling | **Ruled 2026-08-23: bounded in-memory dedup** | PR 4, tasks 4.5–4.6 |

---

## PR 1 — `feat/ports-channel` (~250 impl+docs)

Depends on nothing. Ships the port, the fake, the shared contract, and the I03 widening.

- [x] **1.1** Commit 1 (RED): `test/support/repocontract/channel.go` (new) — `RunChannel(t, newChannel
      func(t *testing.T) ports.Channel)`: `Send` then the fake's own inspection shows the text and
      the conversation; `Receive` on a quiet channel returns an empty slice and a **nil error**
      (the ordinary case, asserted as not-an-error); `Name()` is non-empty; a `Receive` after
      `Confirm` does not return the confirmed message again. Plus a reflection scan over
      `reflect.TypeOf((*ports.Channel)(nil)).Elem()` asserting no method name begins `Delete`,
      `Remove`, `Purge`, `Drop` or `Destroy` — over the interface's own method set, never a
      hand-typed list of the five method names.
      **Red**: `undefined: ports.Channel`, `ports.ChannelMessage`, `ports.ConversationID`,
      `fakechannel.New`.
      Stub: the interface plus a fake whose every method is a no-op/zero-value — compiles; the
      `Send`-then-inspect case fails first.
      Requirement: R1.1, R1.2.
      **Mutation**: empty the reflection scan's prefix set — a hypothetical `DeleteConversation`
      would then compile and pass undetected; also make the fake's `Receive` ignore `Confirm` — the
      confirmed-message-not-returned assertion must fail.
- [x] **1.2** Commit 2 (GREEN): implement `internal/ports/channel.go` — `ConversationID`,
      `ChannelMessage{ID, Conversation, Text, Channel}`, `Channel{Name, Receive, Confirm, Send,
      Close}` with design §3.1's exact doc comments, **including the paragraph explaining why
      `Confirm` is its own method** (owner item R1); and `test/support/fakechannel/fakechannel.go`.
      Verify: `go test ./test/conformance/... -run Channel`.
      Requirement: R1.1, R1.2; design §3.1.
- [x] **1.3** `test/conformance/channel_port_names_no_vendor_test.go` (new) — parses every file
      under `internal/ports/**` with `go/parser` and asserts no **identifier** contains "telegram"
      case-insensitively. Parses rather than greps: a doc comment may legitimately name Telegram as
      the first implementation, and a byte scan cannot tell that from a leaked type name.
      Requirement: R1.1's MUST NOT; R0's experiment.
      **Mutation**: rename `ChannelMessage.Conversation` to `TelegramChatID` — the scan fails; a
      byte-grep version would also fail on the doc comment and therefore could not be written this
      way at all.
- [x] **1.4** `test/conformance/i03_units_never_deleted_test.go` (extend) — add
      `reflect.TypeOf((*ports.Channel)(nil)).Elem()` to `sweptPortsRepoTypes`, and extend that
      variable's doc comment: the list's claim is "every ports repository interface", and a channel
      is not a repository — so either the claim widens or the port stays out. **It widens**: I03's
      subject is "nothing is deleted", and a channel that could delete a conversation would be the
      same failure in a different table.
      Requirement: I03, applied.
- [x] **1.5** Purity/lint: `golangci-lint run`. `internal/ports/channel.go` imports the standard
      library only.
      Requirement: design §4.
- [x] Verify (PR-level): `make check-all`; diff touches only `internal/ports/channel.go`,
      `test/support/{fakechannel,repocontract}/**`, `test/conformance/{channel_port_names_no_vendor,
      i03_units_never_deleted}_test.go`. No `docs/02-cognitive-core.md` delta. Target ≤250.

---

## PR 2 — `feat/channels-telegram-client` (~400 impl+docs, watch the ceiling)

Depends on PR 1. Ships the Bot API client, the error taxonomy, and the host-literal guard.

- [ ] **2.1** Commit 1 (RED): `test/conformance/telegram_host_literal_test.go` (new) — parses the
      whole repository with `go/ast` and asserts the literal `api.telegram.org` appears **exactly
      once** in a non-test `.go` file, at the identifier the test names (`telegram.defaultBaseURL`),
      and **zero times** in any `_test.go` file. Written before the constant exists, so its first
      failure is "zero occurrences, want one".
      **Red**: zero occurrences.
      Requirement: R2.1; design §3.2; proposal §9 risk R5.
      **Mutation**: add the literal to any test file — the zero-in-tests leg fails; add a second
      non-test occurrence — the exactly-once leg fails. A byte-grep version passes on a comment
      mentioning the host, which is `m2d`'s JD-4-01 defect and why this parses.
- [ ] **2.2** Commit 2 (GREEN): `internal/channels/telegram/client.go` — `defaultBaseURL`,
      `pollTimeoutSeconds = 30` (owner item R6, doc comment deriving it), `NewClient(baseURL string,
      token string, httpClient *http.Client)` following `ollama.NewClient`'s shape exactly
      (`""` means the default), `getUpdates(ctx, offset)` and `sendMessage(ctx, chatID, text)`.
      Verify: `go test ./test/conformance/... -run TelegramHostLiteral`.
      Requirement: R2.1; design §1's `ollama` precedent.
- [ ] **2.3** Commit 1 (RED): `internal/channels/telegram/client_integration_test.go` (build tag
      `integration`) — four `httptest` fixtures, each asserting a **distinguishable** error: a
      transport failure (server closed), `{"ok": false, "error_code": 400, …}`, a `401`, and a
      malformed body. `errors.As` reaches an `*APIError` carrying the code and description for the
      middle two; the `401` is distinguishable from the `400` **without string-matching**.
      **Red**: `undefined: telegram.APIError`.
      Requirement: R2.2.
      **Mutation**: collapse `401` into the general `APIError` path with no distinguishing
      predicate — PR 4's backoff test then cannot tell a permanent failure from a transient one,
      and this test's own "distinguishable without string-matching" leg fails first.
- [ ] **2.4** Commit 2 (GREEN): `internal/channels/telegram/errors.go` — `APIError{Code int;
      Description string}` with `Error()` and an `Unauthorized()` predicate (or a sentinel
      `ErrUnauthorized` reached by `errors.Is`; the implementation picks one and says why).
      Verify: `go test -tags=integration ./internal/channels/telegram/...`.
      Requirement: R2.2.
- [ ] **2.5** `test/e2e/check_demo_test.go` (modify) — narrow
      `TestCheckDemo_ShipsNoTelegramTransport`'s scan from `internal/**` to `internal/brain/**`,
      `internal/scheduler/**` and `internal/core/**`, and rewrite its doc comment to say what the
      narrowed claim is and why it narrowed. **Finding H3**: this file is outside the scope box and
      the modification is necessary — leaving it makes PR 2 red, deleting it drops a real guard.
      Requirement: design §3.2; **H3**.
      **Mutation**: leave the scan at `internal/**` — PR 2 cannot merge, which is the point: the
      narrowing is forced by the change and must be visible in the diff rather than discovered by CI.
- [ ] **2.6** Purity/lint: `golangci-lint run`.
- [ ] Verify (PR-level): `make check-all`; diff touches only
      `internal/channels/telegram/{client,errors}{,_integration_test}.go`,
      `test/conformance/telegram_host_literal_test.go`, `test/e2e/check_demo_test.go`. Target ≤400.
      **If measured lines exceed 400**, design §6's pre-drawn cut applies: the client (2.1–2.2) from
      the error taxonomy (2.3–2.5), giving a sixth PR `feat/channels-telegram-errors`. **Report
      before splitting.**

---

## PR 3 — `feat/channels-telegram-allowlist` (~300 impl+docs)

Depends on PR 2. Ships admission, the token, and the refusal record.

- [ ] **3.1** Commit 1 (RED): `internal/channels/telegram/channel_test.go` — `New` with
      `Enabled: true` and an **empty** `AllowedChatIDs` returns an error naming the configuration
      key `allowed_chat_ids`; `New` with an empty `BotTokenEnv`, and with a `BotTokenEnv` naming an
      unset variable, each fail naming what is missing.
      **Red**: `undefined: telegram.New`.
      Stub: a constructor returning `(nil, nil)` unconditionally — compiles; the empty-allow-list
      case fails first.
      Requirement: R3.2, R3.3; CLAUDE.md non-negotiable #7.
      **Mutation**: make the empty-allow-list case a warning that still constructs — the assertion
      fails. The point of a second refusal beside `internal/config`'s is a caller that skipped
      validation, so a test that only exercised the validator would not cover this.
- [ ] **3.2** Commit 2 (GREEN): `internal/channels/telegram/channel.go` — `New(cfg config.Telegram,
      lookup func(string) (string, bool), httpClient *http.Client, baseURL string, log io.Writer)`,
      reading the token via `lookup` (never `os.Getenv` directly, so a test injects without touching
      the process environment) and refusing the three invalid shapes.
      Verify: `go test ./internal/channels/telegram/...`.
      Requirement: R3.2, R3.3; design §3.4.
- [ ] **3.3** Commit 1 (RED): `internal/channels/telegram/token_leak_test.go` (new) — the token is a
      known sentinel (`"SENTINEL-TOKEN-DO-NOT-LEAK"`); across a transport failure, an `APIError` and
      a **successful** call, the sentinel appears in **no returned error** and in **no byte written
      to the channel's log**.
      **Red**: the naive `fmt.Errorf("%w", err)` path leaks it — the transport-failure leg fails
      first, and its failure message shows the token inside a `*url.Error`.
      Requirement: R3.3; design §3.4.
      **Mutation**: this test IS the mutation's detector — reverting `sanitize` to `%w` reproduces
      the original leak. The successful-call leg exists so a future error path added elsewhere is
      also covered by the log half.
- [ ] **3.4** Commit 2 (GREEN): `sanitize(err) error` in `errors.go`, applied on **every** error
      path in `client.go`; admission inside `Receive` — an update whose chat id is not in the
      allow-list is dropped before a `ports.ChannelMessage` is built, **and its offset is still
      advanced** (owner item R2: a refused message has no capture to lose); one log line naming the
      refused chat id and **not the message text** (design §3.3).
      Verify: `go test ./internal/channels/telegram/...`.
      Requirement: R3.1, R3.3.
- [ ] **3.5** Commit 1 (RED, L3): `channel_integration_test.go` — over `httptest`: an update from an
      allowed chat becomes a `ports.ChannelMessage`; an update from a non-allowed chat yields
      **zero** messages, and the log holds one line containing the refused chat id and **not** the
      message body.
      Requirement: R3.1.
      **Mutation**: log the refused message's text alongside its chat id — the "not the body" leg
      fails. That leg is the one this task exists for: the refusal itself is easy, and logging the
      body is the natural thing to write.
- [ ] **3.6** Purity/lint: `golangci-lint run`.
- [ ] Verify (PR-level): `make check-all`; diff touches only `internal/channels/telegram/**`.
      Target ≤300.

---

## PR 4 — `feat/channels-telegram-resilience` (~350 impl+docs)

Depends on PR 3. Ships backoff, the offset rule, the dedup ring, and shutdown.

- [ ] **4.1** Commit 1 (RED): `internal/channels/telegram/backoff_test.go` — `backoffFor(n int)
      time.Duration` is a pure function: `n = 0` is the base, it doubles, it is capped at
      `pollBackoffMax`, and it never returns a negative or zero duration for any `n` in `[0, 64]`
      (the overflow leg — doubling a duration 64 times overflows `int64`, and a negative sleep is a
      busy loop).
      **Red**: `undefined: backoffFor`.
      Requirement: R4.2.
      **Mutation**: implement it as `base << n` with no cap — the overflow leg fails at the `n`
      where the shift wraps negative, which a three-value table would not reach.
- [ ] **4.2** Commit 2 (GREEN): `backoffFor` plus `pollBackoffBase` and `pollBackoffMax` as named
      constants with doc comments deriving them.
      Requirement: R4.2, R6.3.
- [ ] **4.3** Commit 1 (RED, L2): the loop's failure classification — a `401` **stops** the loop and
      returns; every other failure backs off and continues. Asserted over a fake `Channel` whose
      `Receive` returns a scripted error sequence, with an injected sleep function so no test sleeps.
      **Red**: no classification exists; the `401` case loops forever (the test asserts the loop
      returns within a bounded number of iterations rather than hanging).
      Requirement: R4.2; owner item R5.
      **Mutation**: treat `401` as transient — the loop does not return and the bounded-iteration
      assertion fails rather than the test hanging, which is why the bound is written as an
      assertion and not as a timeout.
- [ ] **4.4** Commit 2 (GREEN): `internal/channels/runner.go` — `channelRunner` with the loop of
      design §3.6, the sleep injectable, `ctx` cancellation recognised as shutdown rather than as a
      transport failure.
      Verify: `go test ./internal/channels/...`.
      Requirement: R4.2, R4.3.
- [ ] **4.5** Commit 1 (RED, L1): `internal/channels/runner_test.go` — the dedup ring: it remembers
      the last `dedupWindow` ids, evicts oldest-first, reports a repeat as seen and a fresh id as
      not, and **its memory does not grow** past the window over 10× the window's worth of ids.
      **Red**: `undefined` — no ring exists.
      Requirement: **Q1**'s ruling (2026-08-23); design §3.5.
      **Mutation**: implement it as an unbounded `map[string]bool` — every behavioural leg passes
      and only the growth leg fails, which is why the growth leg is written.
- [ ] **4.6** Commit 2 (GREEN): the ring, `dedupWindow` as a named constant whose doc comment
      derives it from the confirm cadence (one batch plus whatever failed in it, with headroom) and
      **states plainly that it does not survive a restart** (design §3.5, risk A).
      Requirement: **Q1**; R4.1.
- [ ] **4.7** Commit 1 (RED, L3): `runner_integration_test.go` — the offset rule against a fake
      Telegram server: a capture that **fails** leaves the offset unadvanced and the update is
      returned again on the next poll; a capture that **succeeds** advances it and the update is
      not. Both legs in one test, because either alone is satisfied by an implementation that never
      confirms or always confirms.
      Requirement: R4.1.
      **Mutation**: confirm before capturing — the failed-capture leg fails, and it is the leg that
      encodes "losing a capture is unrecoverable and duplicating one is not".
- [ ] **4.8** Commit 1 (RED, L3): shutdown — a `Stop`/context cancel issued **while a poll is in
      flight** returns within a bound well under the poll timeout, and the in-flight update is
      either confirmed-and-captured or unconfirmed, never confirmed-without-capture.
      Requirement: R4.3.
      **Mutation**: build the request with `http.NewRequest` instead of
      `http.NewRequestWithContext` — cancellation no longer reaches the in-flight call and the
      bounded-return assertion fails after the full poll timeout.
- [ ] **4.9** Purity/lint: `golangci-lint run`.
- [ ] Verify (PR-level): `make check-all` **with `-race`** on the runner's tests specifically — the
      loop plus a shutdown path is `m2d`'s JD-5-01 shape, and that finding was a real data race on
      an unguarded `io.Writer`. Diff touches only `internal/channels/**`. Target ≤350.

---

## PR 5 — `feat/channels-telegram-inbound` (~350 impl+docs)

Depends on PR 4. Ships capture, reply, wiring, and the L4 demo.

- [ ] **5.1** Commit 1 (RED, L2): `test/conformance/channel_reply_totality_test.go` (new) — the
      reply rendering is **total over `brain.AllCaptureOutcomes()`**: every one of the seven
      outcomes renders to a non-empty, distinguishable reply, and the test asserts
      `len(AllCaptureOutcomes()) == 7` at its top so a silently narrowed vocabulary is caught too.
      `TestAllCaptureOutcomesHaveAStatusMapping` (`internal/httpapi`) is the shape.
      **Red**: `undefined: renderReply`.
      Requirement: R5.1.
      **Mutation**: drop one `case` from the rendering switch — the outcome's reply becomes empty
      and the totality leg fails. A hand-written seven-case test would pass after an eighth outcome
      is added; iterating `AllCaptureOutcomes()` is what makes it fail.
- [ ] **5.2** Commit 2 (GREEN): `renderReply(brain.CaptureResult) string` plus the runner's capture
      step: `CaptureInput{Text: msg.Text, Channel: ch.Name()}`, a capture error **breaks the batch**
      (owner item R4) and confirms nothing.
      Verify: `go test ./internal/channels/... ./test/conformance/... -run Reply`.
      Requirement: R5.1, R4.1.
- [ ] **5.3** Commit 1 (RED, L2): a failed `Send` does **not** block the `Confirm` (owner item R3) —
      over a fake channel whose `Send` always errors, the message is still confirmed and the log
      holds the failure. The reply is not durable; the capture is.
      Requirement: design §3.6; owner item R3.
      **Mutation**: return early on a `Send` error — the message is never confirmed, is redelivered,
      and captures a second time, which is the duplicate this ordering exists to prevent.
- [ ] **5.4** `test/conformance/brain_names_no_channel_test.go` (new) — parses `internal/brain/**`
      and asserts no identifier names a channel or a vendor. **R0's experiment, made checkable**:
      doc 02:653 claims nothing in the decision layer names a channel, and this is the first change
      that could have falsified it.
      Requirement: R5.2, R0.
      **Mutation**: add a `telegramReply` helper to `internal/brain` — the scan fails. Without this
      test, R0 is a claim nobody re-checks after the change that could break it.
- [ ] **5.5** Commit 2 (GREEN): `cmd/nooma/wiring.go` — `wireChannel(cfg, lookup) (ports.Channel,
      error)`, returning `(nil, nil)` when Telegram is disabled so a caller need not branch on
      configuration.
      Requirement: R6.2.
- [ ] **5.6** `cmd/nooma/wiring_test.go` (extend) — `wireChannel` is unit-tested, **and a source
      scan asserts `runServe` does not reference it** (R6.2, finding **H2**): this PR ships a
      constructor with no production caller, deliberately, and the absence is asserted rather than
      left to be noticed. `m3b` PR 3 shipped `LiveFocusCandidates` the same way.
      Requirement: R6.2; **H2**.
      **Mutation**: call `wireChannel` from `runServe` — the scan fails. Starting a poller that has
      nothing to deliver is what this assertion prevents until `m3d`.
- [ ] **5.7** Commit 1 (RED, L4): `test/e2e/telegram_demo_test.go` (new) — **the change's own exit
      criterion**: a fake Telegram server; a message from an **allowed** chat becomes a unit and
      produces a reply posted back to its conversation; a message from a **non-allowed** chat
      produces zero units, zero replies, and one log line naming the refused chat id. Both fixtures
      are required together — with only the allowed one the refusal half is unfalsifiable, and with
      only the refused one the capture half is.
      Requirement: R5.1, R3.1; the Exit criterion.
      **Mutation**: seed only the allowed message — the refusal half becomes unfalsifiable, which is
      exactly why both fixtures ship together.
- [ ] **5.8** `docs/01-architecture.md` — the channel's row in the command/component table, per
      design §5.
      Requirement: R6.2.
- [ ] **5.9** Purity/lint: `golangci-lint run`.
- [ ] Verify (PR-level): `make check-all`; diff touches only `internal/channels/**`,
      `cmd/nooma/wiring{,_test}.go`, `test/conformance/**`, `test/e2e/telegram_demo_test.go`,
      `docs/01-architecture.md`. **No `docs/02-cognitive-core.md` delta — R0's file-list form.**
      Target ≤350.

---

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | ~1,650 budgeted impl+docs across 5 PRs; test lines tracked separately, historically 1.3×–4.3× on this project |
| 400-line budget risk | **High for PR 2** (400, at the ceiling pre-code, with a pre-drawn cut); Medium for PRs 4 and 5 (350 each); Low for PRs 1 and 3 |
| Chained PRs recommended | Yes — five links, already a chain by design |
| Suggested split | PR 2 only, and it is pre-drawn (design §6): the client from the error taxonomy. Report before splitting |
| Delivery strategy | `auto-chain` |
| Chain strategy | `stacked-to-main` |

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| §0 Scope | R0 | 1.3, 5.4 (the experiment, made checkable) |
| §1 The port | R1.1, R1.2 | 1.1–1.5 |
| §2 The client | R2.1, R2.2 | 2.1–2.6 |
| §3 Admission | R3.1–R3.3 | 3.1–3.6 |
| §4 Resilience | R4.1–R4.3 | 4.1–4.9 |
| §5 Inbound | R5.1, R5.2 | 5.1–5.4, 5.7 |
| §6 Cross-cutting | R6.1, R6.2, R6.3 | 2.1 (R6.1), 5.5–5.6 (R6.2), 2.2/4.2/4.6 (R6.3) |
| Exit criterion | — | 5.7 |

---

## Handoffs to `m3d`

- **Starting the channel** is `m3d`'s single wiring PR, alongside the `proactive_check` tick. This
  change ships the constructor and asserts nothing calls it (task 5.6).
- **A durable offset**, if wanted, slots into `Confirm` without changing the port (design §3.5).
  Risk A — a restart between capture and confirm duplicates one message — is accepted here and is
  `m3d`'s to close if it chooses.
- **Rate limiting** is unmitigated in v1 and named in design §8. The allow-list is the rate limit.
- **`Send` is what `m3d` delivers through.** Nothing in this change calls it except the reply path.
