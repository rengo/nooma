package brain

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// digestHistoryDays is how far back the pass reads decision_log to answer
// two questions: when the last digest went out, and how many digests each
// held item has already been held through.
//
// MaxDigestDeferrals + 2 days. The window has to cover every digest an
// item could still be waiting through — that is MaxDigestDeferrals of them
// — plus a margin for a day with no digest at all (the vault was off, the
// pass never ran). Reading further back costs rows and answers nothing:
// a deferral older than the bound has already forced its item out.
const digestHistoryDays = prospection.MaxDigestDeferrals + 2

// assembleDigest sends one digest if one is due, and reports how many
// items it carried.
//
// Everything it decides is prospection's: DigestDue says whether a digest
// is owed, LowEnergy reads the care gate, and Carry splits what goes out
// from what waits. This supplies their inputs and persists their outcome.
func (r checkRunner) assembleDigest(ctx context.Context, now time.Time, commit bool) (int, error) {
	if r.channel == nil {
		// No channel, no digest. The push path already declines to mark
		// anything delivered without one; the digest must too, and the
		// asymmetry would have been worse here — a digest with no channel
		// would surface every carried item at once, recording a delivery
		// nobody received and removing them from tomorrow's digest
		// forever. `nooma check` on a Telegram-less vault is exactly this
		// case, and it is what caught it.
		return 0, nil
	}

	history, err := r.log.Since(ctx, now.AddDate(0, 0, -digestHistoryDays), -1)
	if err != nil {
		return 0, fmt.Errorf("check: reading digest history: %w", err)
	}

	if !prospection.DigestDue(lastDigestAt(history), now) {
		return 0, nil
	}

	energy, err := r.state.LatestEnergy(ctx)
	if err != nil {
		return 0, fmt.Errorf("check: reading energy: %w", err)
	}
	// Read once, used twice: Carry's own truncation, and the gate on
	// asking. Recomputing it at the second site would be the same rule in
	// two places, which is the drift this file already refuses elsewhere.
	low := prospection.LowEnergy(energy, now)

	pending, err := r.triggers.Undelivered(ctx)
	if err != nil {
		return 0, fmt.Errorf("check: undelivered triggers: %w", err)
	}

	// The digest's SECOND item source, and it does not enter Carry's
	// candidate list: a relation has two endpoints and focus.Priority
	// ranks one unit, so inventing a candidate to make a question
	// rankable would be the invented number this repository refuses.
	// It is appended instead, competing with nothing (owner ruling Q2).
	//
	// **No question on a low-energy morning.** Doc 02 §7's care gate holds
	// back non-urgent items and a graph question is the most non-urgent
	// thing this system produces — expressed as a rule rather than a rank,
	// which is the same trade Q2 already took.
	var question *ports.RelationQuestion
	if !low {
		question, err = r.nextQuestion(ctx)
		if err != nil {
			return 0, err
		}
	}

	if len(pending) == 0 && question == nil {
		// **An empty digest is not sent**, and m3a left this to m3d
		// explicitly ("Carry takes no position on whether an empty result
		// is delivered"). A message every morning saying nothing happened
		// is a message people learn to ignore, and the one that matters
		// arrives in the same shape they learned to ignore.
		//
		// m3e widens the test rather than the rule: a digest that asks "I
		// linked X with Y, are they related?" is not empty. It has content
		// and it wants something, and without this a vault with an
		// uncertain relation and no triggers would stay silent forever.
		return 0, nil
	}

	items, err := r.digestItems(ctx, pending, history)
	if err != nil {
		return 0, err
	}

	// Adjacency is M4's — focus.Rank accepts an empty map and scores
	// every candidate on its own terms, which is the honest input until
	// something computes it. Passing a made-up one would be worse than
	// passing none.
	carry, held := prospection.Carry(items, map[string]float64{}, low, now)

	if len(carry) == 0 && question == nil {
		return 0, nil
	}
	if !commit {
		return len(carry), nil
	}

	if r.conversation == "" {
		// Same rule the pushed path takes: no destination, no send. The
		// items stay undelivered and tomorrow's digest carries them,
		// which is exactly what a failed send would have left behind —
		// without asking the transport to parse an empty chat id first.
		return 0, r.record(ctx, now, ports.ActionCheckDeliveryFailed,
			fmt.Sprintf("the digest could not be delivered; this vault has no conversation to "+
				"push to, so its %d item(s) stay undelivered and tomorrow's digest carries them", len(carry)),
			checkDetail{})
	}

	if err := r.channel.Send(ctx, r.conversation, renderDigest(carry, pending, question)); err != nil {
		return 0, r.record(ctx, now, ports.ActionCheckDeliveryFailed,
			fmt.Sprintf("the digest could not be delivered; its %d item(s) stay undelivered and tomorrow's digest carries them: %v", len(carry), err),
			checkDetail{})
	}

	for _, item := range carry {
		if err := r.triggers.Surface(ctx, item.ID, now); err != nil {
			return 0, fmt.Errorf("check: digest was sent but trigger %q was not marked delivered: %w", item.ID, err)
		}
	}
	if question != nil {
		// After the send, exactly as Surface is: MarkAsked is one-way
		// (asked_at IS NULL is its precondition), so a question marked
		// before a failed send would be a question nobody was ever asked.
		if err := r.questions.MarkAsked(ctx, question.ID, now); err != nil {
			return 0, fmt.Errorf("check: digest was sent but question %q was not marked asked: %w", question.ID, err)
		}
		if err := r.record(ctx, now, ports.ActionCheckDigestQuestionAsked,
			fmt.Sprintf("the daily digest asked about the relation between %q and %q", question.FromContent, question.ToContent),
			checkDetail{ID: question.ID}); err != nil {
			return 0, err
		}
	}
	if err := r.record(ctx, now, ports.ActionCheckDigestSent,
		fmt.Sprintf("the daily digest carried %d item(s) and held %d", len(carry), len(held)),
		checkDetail{}); err != nil {
		return 0, err
	}

	// One row per held item, and this is not only for the glass box: the
	// deferral count is derived from these rows (design §3.4), so a held
	// item that wrote none would reset its own patience every morning and
	// never reach MaxDigestDeferrals. The audit trail IS the counter.
	for _, item := range held {
		if err := r.record(ctx, now, ports.ActionCheckDigestHeld,
			fmt.Sprintf("trigger %q was held back by the low-energy gate; it has now been held %d time(s)", item.ID, item.Deferrals+1),
			checkDetail{ID: item.ID}); err != nil {
			return 0, err
		}
	}

	return len(carry), nil
}

// digestItems turns undelivered triggers into what Carry consumes: a
// candidate for ranking, and how many digests each has already waited
// through.
func (r checkRunner) digestItems(ctx context.Context, pending []ports.DueTrigger, history []ports.Decision) ([]prospection.DigestItem, error) {
	// A trigger with no source unit — a pattern watcher — has no
	// candidate, and m3b's LiveFocusCandidates doc comment names not
	// passing it a NULL unit id as the caller's obligation. This is the
	// caller.
	ids := make([]string, 0, len(pending))
	for _, t := range pending {
		if t.UnitID != nil {
			ids = append(ids, *t.UnitID)
		}
	}

	candidates, err := r.units.LiveFocusCandidates(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("check: focus candidates: %w", err)
	}
	byUnit := make(map[string]focus.Candidate, len(candidates))
	for _, c := range candidates {
		byUnit[c.ID] = c
	}

	deferrals := heldCounts(history)

	items := make([]prospection.DigestItem, 0, len(pending))
	for _, t := range pending {
		item := prospection.DigestItem{ID: t.ID, Deferrals: deferrals[t.ID]}
		if t.UnitID != nil {
			if c, ok := byUnit[*t.UnitID]; ok {
				item.Candidate = &c
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// lastDigestAt is when the most recent digest went out, or nil.
func lastDigestAt(history []ports.Decision) *time.Time {
	var latest *time.Time
	for i := range history {
		if history[i].Action != ports.ActionCheckDigestSent {
			continue
		}
		at := history[i].OccurredAt
		if latest == nil || at.After(*latest) {
			latest = &at
		}
	}
	return latest
}

// heldCounts is how many digests each trigger has been held through,
// counted from the audit trail rather than from a column.
//
// Design §3.4's choice, and its cost is stated there: counting rows is
// O(rows in the window) per digest. The window is digestHistoryDays, a
// personal vault's decision_log is small, and the alternative — a column —
// is a migration for a counter meaningless outside a digest. An in-memory
// counter was rejected outright: a restart would reset every item's
// patience, which is precisely the starvation MaxDigestDeferrals bounds.
func heldCounts(history []ports.Decision) map[string]int {
	counts := map[string]int{}
	for i := range history {
		if history[i].Action != ports.ActionCheckDigestHeld {
			continue
		}
		var detail checkDetail
		if err := unmarshalCheckDetail(history[i].Context, &detail); err != nil || detail.ID == "" {
			// A row whose context cannot be read is a row that cannot be
			// counted. Skipped rather than fatal: a corrupt audit row must
			// not stop the digest, and undercounting only delays an item.
			continue
		}
		counts[detail.ID]++
	}
	return counts
}

// renderDigest is what the digest says: a header naming the count, then
// one line per item.
//
// Held items are not mentioned. A digest that listed what it withheld
// would defeat the low-energy gate it came from — the point of holding
// something back is that the person does not have to think about it today.
func renderDigest(carry []prospection.DigestItem, pending []ports.DueTrigger, question *ports.RelationQuestion) string {
	text := make(map[string]string, len(pending))
	for _, t := range pending {
		text[t.ID] = t.Payload.ActionText
	}

	var b strings.Builder
	if len(carry) > 0 {
		b.WriteString("Here " + plural(len(carry)) + ":")
		for _, item := range carry {
			line := text[item.ID]
			if line == "" {
				line = "something you asked me to remind you about"
			}
			b.WriteString("\n• " + line)
		}
	}

	if question != nil {
		// "One more" only when there IS something before it. A digest sent
		// for a question alone cannot open with a header counting items it
		// does not have — "Here are 0 things for today" is a sentence that
		// is simply false, and the item header counts Carry's output.
		//
		// The question itself is written once, in questionLine, so the two
		// shapes cannot drift into two different questions.
		if b.Len() > 0 {
			b.WriteString("\n\nOne more \u2014 ")
		}
		b.WriteString(questionLine(*question))
	}
	return b.String()
}

// questionLine is the digest's relation question, in doc 02 §4's own
// wording ("I linked X with Y, are they related?").
func questionLine(q ports.RelationQuestion) string {
	return "I linked \"" + snippet(q.FromContent) + "\" with \"" + snippet(q.ToContent) + "\". Are they related?"
}

// snippet is one endpoint's own text, bounded to questionSnippetRunes.
//
// Rune-aware, not byte-aware: units.content is free text and a byte-wise
// cut lands inside a multi-byte rune often enough that the digest would
// render a replacement character for it.
func snippet(s string) string {
	runes := []rune(s)
	if len(runes) <= questionSnippetRunes {
		return s
	}
	return string(runes[:questionSnippetRunes]) + "\u2026"
}

func plural(n int) string {
	if n == 1 {
		return "is 1 thing for today"
	}
	return "are " + strconv.Itoa(n) + " things for today"
}

// questionSnippetRunes bounds how much of a unit's own text a digest
// question quotes. units.content is unbounded and a digest line is not, so
// both endpoints are truncated to this many runes with an ellipsis.
//
// A rendering bound with no decision behind it — nothing branches on it —
// so it lives in internal/brain rather than internal/core and carries no
// docs/02 §13 calibration row, digestHistoryDays being the shipped
// precedent for a brain-side bound.
const questionSnippetRunes = 60

// nextQuestion is the one queued relation question this digest may ask, or
// nil when the queue is empty or this vault has no question store.
//
// The head of Unasked, and nothing more: the port already returns oldest
// created_at first, then id, so FIFO is a contract of the read rather than
// a rule re-derived here. FIFO and not confidence — ranking by confidence
// would ask first about the relation closest to asserting itself anyway,
// which is the question that matters least, and FIFO also drains the queue
// in order, so a question created today cannot wait behind one created
// tomorrow (design §3.5).
func (r checkRunner) nextQuestion(ctx context.Context) (*ports.RelationQuestion, error) {
	if r.questions == nil {
		return nil, nil
	}
	queued, err := r.questions.Unasked(ctx)
	if err != nil {
		return nil, fmt.Errorf("check: unasked questions: %w", err)
	}
	if len(queued) == 0 {
		return nil, nil
	}
	return &queued[0], nil
}

// expireStaleQuestions closes every open question that MaxDigestDeferrals
// digests have gone out on without an answer.
//
// STUB (task 5.5/5.6): expires nothing.
func (r checkRunner) expireStaleQuestions(ctx context.Context, history []ports.Decision, now time.Time, commit bool) (int, error) {
	return 0, nil
}
