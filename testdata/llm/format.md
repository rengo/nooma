# LLM golden-set case format

Real responses from each provider, recorded once and replayed by a fake
provider (`docs/06-harness.md` §5) — this is what lets every test in this
codebase run without the network and without a real LLM
(CLAUDE.md non-negotiable #5). It backs both `classify` (ADR-0002) and the
dedup/relation judge (`docs/02-cognitive-core.md` §5), and — per
ADR-0002 — the same recordings feed `nooma doctor`'s structured-JSON
quality gate: written once, used in two places.

**A case does not record a `prompt` field.** An earlier version of this
format did, and `nooma doctor`'s live gate sent that field verbatim — a
short, fake-replay identifier, never classify's real ~1550-byte prompt —
which is why a real provider failed 21 of 21 prompts the first time a
human ran `doctor` against one (see
`openspec/changes/m1c-surface/tasks.md`'s Conflicts §C24). `message`
(and, for a relation_evaluation case, `candidates`) below replace it: the
gate now builds the live prompt through `classify.BuildPrompt` or
`brain.JudgePrompt` — the same functions production calls — instead of
replaying a separately recorded string that could drift from what those
functions actually produce.

`cases/` is empty in this change. Populating it with real recorded
responses is M1's responsibility (spec R10.1's MUST NOT); this file exists
so whoever adds the first case does not have to guess the shape.

## File naming convention

One file per case, `cases/<case-id>.json`, where `<case-id>` is the case's
own `id` field verbatim (e.g. `id: "classify-remind-me-tomorrow"` →
`cases/classify-remind-me-tomorrow.json`).

## Shape

```json
{
  "id": "classify-remind-me-tomorrow",
  "provider": "anthropic",
  "model": "claude-sonnet",
  "task": "classify",
  "message": "Remind me to buy descaling solution tomorrow",
  "response": "{\"type\":\"task\",\"normalized_content\":\"Buy descaling solution\"}"
}
```

A relation_evaluation case also carries `candidates`, one `{id, content}` pair per recall
candidate — e.g. `"candidates": [{"id": "cand-duplicate", "content": "Pick up the dry cleaning
this Friday"}]` — see `testdata/llm/cases/relation-duplicate-high-confidence.json` for a full
example. This corpus's own format-checking test
(`TestHarness_GoldenSetFormatMatchesType`) enforces exactly one fenced example, so `candidates`
is documented here in prose rather than as a second block.

## Fields

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | yes | Unique identifier for this case; must match the case's filename (see naming convention above) |
| `provider` | string | yes | The provider this response was recorded from, as configured (e.g. `anthropic`, `ollama`) |
| `model` | string | yes | The model identifier string the provider reports |
| `task` | string | yes | Which pipeline call this recorded response feeds, e.g. `classify` or a judge task (`docs/02-cognitive-core.md` §5's dedup/relation judge). Not a closed enum today — ADR-0002 describes "a fixed set of classify and judge prompts" without naming every judge task, so the loader does not validate `task` against a fixed list |
| `message` | string | yes | The raw text `nooma doctor`'s quality gate feeds to the real prompt builder — the user's message alone for a `classify`-tagged case, the new unit's content for a `relation_evaluation`-tagged case. Never the prompt itself: `cmd/nooma/doctor.go` builds the live prompt from this field through `classify.BuildPrompt` (capture_processing) or `brain.JudgePrompt` (relation_evaluation), the exact functions production calls, so a change to either builder reaches the gate automatically |
| `candidates` | array of `{id, content}` | only for a `relation_evaluation`-tagged case | The recall candidates `brain.JudgePrompt` renders alongside `message` — one entry per candidate, `id`/`content` are the only two fields it reads. A documentation convention by task, not mechanized by the loader: a `classify`-tagged case simply omits it |
| `record` | bool | no | `true` on a reminder phrasing awaiting its recording session (`reminders/` only). It has no `response`, `provider` or `model` yet. `nooma doctor --record` records it and clears the flag; a `record: true` that already has a `response` is a request to re-record, and only `--force` honors it. `scripts/recordings-complete.sh` fails the merge while any remains |
| `now` | string, RFC 3339 | no | The instant the prompt was built at, with the user's UTC offset. `--record` stores the one it told the model, and replay builds the prompt at it, so the hash below can be reproduced |
| `open_question` | `{kind, text, list}` | no | The reminder question Nooma has open when the phrasing is read: `kind` is `remind_when`, `which_reminder` or `undo`, and `text` is the exact words Nooma sent, numbered list included. For `which_reminder`, `list` holds the same entries as data (`{label, when, at, day_only}`, as in `candidates_list`), and a test checks `text` is the lead plus `classify.RenderReminderList` of them. The prompt shows it, and only it, as the list (`classify.RenderReminderList`) |
| `candidates_list` | array of `{label, when, at, day_only}` | no | The stop candidates the code built, in position order; at most five. `when` is what the prompt shows and is written by `phrase.Set` (`Time`, "jue 15 oct, 10:00", or `Date` when `day_only` is true); `at` is the instant it renders, which a test checks against `when` and against the case's `now`. A case with a list or an open question carries an authored `now` before every listed instant. Shown to the model only when no `which_reminder` question is open |
| `conversation` | `{turns, items}` | no | The recent chat a follow-up is read in (ADR-0036), as `classify.Conversation`: `turns`, oldest first and at most `conversation_context_turns`, each `{at, speaker, text, about}` with `at` when it was said (RFC 3339, not after `now`; the prompt shows it, ADR-0042), `speaker` one of `user`, `nooma` or `push` (a reminder or check-in Nooma sent) and `about` the letters of the items it concerns; `items`, lettered from `A` in order, each `{content, title, event_at, due_at}` as the unit stands now, an instant as RFC 3339 or a bare `YYYY-MM-DD` read in the case's zone. A case with a conversation carries an authored `now` |
| `expect` | object | no, but every phrasing in `reminders/` has one | The reading the phrasing should get, written by hand and never touched by `--record`. The reminder fields (`remind_request`, `reminder_intent`, `question_answer`, `choice_number`, `reminder_target`) are strict: unset means the model must read none, false or absent, save `remind_request` beside a follow-up's leads (a `referent` letter and `reminder_leads_minutes`, expected and read), which production ignores (ADR-0043) and so is free. The outcomes are strict too, because each has an effect in production: an unset `task_checkin_outcome`, `nudge_outcome` or `relation_outcome` means the model must read none (the model never sees whether the open check-in is a task or a nudge, so an expected outcome that closes it is judged by how the reading closes it, by `classify.ResolveCheckin`, the mapping production uses: `done` and `engaged` are one reading, `drop` and `declined` another; an expected `snooze` keeps its exact value, and the other field is free only while the effect is the same), and no phrasing may read a `state_outcome`. `day_only`, `title` and `type` (the classify type, so a reply that answers Nooma is caught when it comes back as a new unit) are checked only when set. `reminder_target` is `"1"` to `"5"` or `"unclear"`. `referent` (a letter of `conversation.items`, or `"unclear"`) is strict too, since a referent corrects a unit in production, and a `clarifying_question` must come back exactly when it is `"unclear"`; `voice`, `"voseo"` or `"tuteo"` and only beside an `"unclear"` referent, is the variety that question must be in, checked only when set: it may hold no second-person form only the other variety uses (`internal/reading`'s two word lists), so a question with no second person passes either; `event_at`, the instant a follow-up moves its item to, and `due_at`, the instant a task is due (both RFC 3339, a bare date written as its local midnight), are checked only when set |
| `known_misread` | string | no | Why a phrasing is kept although the model reads it wrong. Replay asserts that it still misreads, so the mark must be removed the day it reads right. It covers only the fields it names, each as a word of its own (`remind_request`, `reminder_leads_add`): a miss of a field it does not name still fails, so a mark never hides a misread of the field the case exists to test. Nobody edits a response by hand |
| `prompt_sha256` | string | with `now`, on a recorded reminder phrasing | The SHA-256 of the prompt the response was recorded against. Replay rebuilds that prompt through `classify.BuildPrompt` and fails on a mismatch, so a prompt change cannot leave stale recordings green |
| `response` | string | exactly one of `response`/`error` | The exact raw response text, recorded once and replayed verbatim by a fake provider — never re-sent to a real provider by a test |
| `error` | string | exactly one of `response`/`error` | A recorded provider-level failure (timeout, HTTP error, rate limit) the fake provider must surface instead of a response. `docs/06-harness.md` §3 says providers are always served from fixtures, so a failure path can only ever be exercised from a recording like this one |

## The reminder phrasing set

`reminders/*.json` holds the phrasings that measure how well a model reads a reminder
request, a stop, a keep and an answer (ADR-0033). It is embedded apart from `cases/`
(`llm.ReminderCases`), so the quality gate does not send it on every `doctor` run;
`nooma doctor --reading` does. Phrasings are synthetic: `scripts/recordings-complete.sh`
rejects a vault path, a ULID-shaped id or an e-mail address in any of them.

A phrasing is written with `record: true` and its `expect`, then recorded once per prompt
change with `nooma doctor --record testdata/llm/reminders [vault]` (docs/06-harness.md §5).
`internal/reading` compares the replayed response to `expect`.

**One rule for a bare word.** With no reminder question open, a bare word answers an open
check-in (spec: a bare "listo" keeps M3's behaviour), and the model is never shown whether one
is open: the store decides which one it answers. So it expects `type: chitchat` (it stores no
unit) and the outcome the word means: "sí" is `nudge_outcome: engaged`, "ya" and "listo" are
`task_checkin_outcome: done`, and "no" is `task_checkin_outcome: snooze` with no `nudge_outcome`
(the client's ruling: a bare no is "not yet, ask again later"; only an explicit refusal, to do
the task or to be asked about it, is `drop` or `declined`). With a question open, the word
answers that question instead.

## Cross-field constraint

Exactly one of `response` or `error` must be set — never both, never
neither, except that a case with `record: true` is awaiting its recording and has
neither (or keeps the old `response` it is to replace). This is mechanized: `LLMExample.Validate`
(`test/support/goldenset/types.go`) rejects a case with both empty or both
set.

`task` should name one of ADR-0002's fixed corpus categories, but this is a
documentation convention, not a mechanized constraint — `Load`
(`test/support/goldenset`) does not validate `task` against a fixed list.

A `testdata/classify/` case may set its own `llm_case_id` to this case's
`id`, to trace a malformed classify output back to the exact recording it
degrades from (see `testdata/classify/format.md`'s cross-field section).
No field here points back to `classify/` — the reference is one-directional
by design, since one recording can back more than one classify case.

## What makes a good case

Real classify prompts vary with self-beliefs and the clock (see `message`
above — `classify.BuildPrompt` still renders both into the live prompt,
this corpus just no longer stores its own snapshot of the result), so a
corpus of only clean, well-formed responses cannot exercise the two things
this corpus actually exists for:

- **A malformed response for each I14 shape** `testdata/classify/format.md`
  lists (truncated JSON, wrong type, unknown enum) — one recording per
  shape, linked from its classify case via `llm_case_id`.
- **At least one recorded `error` case** per plausible provider failure
  (timeout, HTTP error, rate limit) — otherwise a fake provider's failure
  path is never exercised at all, since providers never touch the network
  in a test (CLAUDE.md non-negotiable #5).
- **Coverage across the providers/models this project actually configures**
  (`provider`/`model`), not just one, so a provider-specific quirk in a
  real response's shape has a fixture to catch it.

## What the loader does and does not check

**Checked** (`test/support/goldenset/loader.go`, `DecodeStrict`):

- The file is valid JSON, decodable into `goldenset.LLMExample`.
- An unknown field anywhere in the document is rejected
  (`json.Decoder.DisallowUnknownFields`).
- `id`, `provider`, `model`, `task` and `message` are present and
  non-empty, and exactly one of `response`/`error` is set
  (`LLMExample.Validate`). A case gutted down to `{}`, or missing a single
  required field, is rejected with that field named in the error.

**Not checked**:

- `task` against ADR-0002's fixed corpus categories — a documentation
  convention, not a mechanized one.
- `candidates` being present on a `relation_evaluation`-tagged case, or
  absent on a `classify`-tagged one — a documentation convention by task,
  the same posture `task` itself already takes above.
- `Load` validates one file at a time; "one file per case" is a corpus
  convention this format.md documents, not something `Load` itself checks
  (there is no directory-listing step inside `Load`).
