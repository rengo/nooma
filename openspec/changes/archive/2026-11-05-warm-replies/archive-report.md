# Archive report — `warm-replies`

**Closed**: 2026-11-05. **`main` at archive**: `ed89743b`, the merge of PR #442. **Verify**: not
re-run for this archive; every PR below is merged and `tasks.md` has every link ticked. One
optional item stays open on purpose (below).

## What shipped

ADR-0064 (Accepted 2026-11-04): a reply that carries a fact is built by code, in warm fixed
sentences, in the user's language. No model words a recall answer, a reminder push, a ringing
timer, the digest, a capture confirmation or a correction's answer, and none of them waits for
one (I59). Days read as a person says them ("mañana a las 10:00"), and the `voice` package of
ADR-0061 is retired.

| PR | Branch | What |
|---|---|---|
| #437 | `plan/warm-replies` | link 0: ADR-0064, design, tasks |
| #438 | `feat/replies-retire-voice` | link 1: `internal/core/voice` deleted |
| #439 | `feat/replies-days-and-recall` | link 2: `Day`/`Clock`/`When`/`Recall`, the recall answer |
| #440 | `feat/replies-pushes` | link 3: pushes and the timer ring, `timer_rephrase` removed |
| #441 | `feat/replies-digest` | link 4: the digest line and header |
| #442 | `feat/replies-confirmations` | link 5: capture confirmations and correction answers |

#435 was closed unmerged (design D5). This closing PR adds the guard of spec R1 for a capture
and a correction (`TestI59_CaptureAndCorrectionCallNoWordingTask`), pins `Dated`'s zone
conversion and the midnight branch of the unarmed-event reply, and corrects two stale comments.

## Left open

- Optional, maintainer: re-record a few `testdata/llm/reminders/` follow-up cases with the new
  turn wording (measured, not required).
