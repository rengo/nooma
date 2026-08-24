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

	pending, err := r.triggers.Undelivered(ctx)
	if err != nil {
		return 0, fmt.Errorf("check: undelivered triggers: %w", err)
	}
	if len(pending) == 0 {
		// **An empty digest is not sent**, and m3a left this to m3d
		// explicitly ("Carry takes no position on whether an empty result
		// is delivered"). A message every morning saying nothing happened
		// is a message people learn to ignore, and the one that matters
		// arrives in the same shape they learned to ignore.
		return 0, nil
	}

	items, err := r.digestItems(ctx, pending, history)
	if err != nil {
		return 0, err
	}

	energy, err := r.state.LatestEnergy(ctx)
	if err != nil {
		return 0, fmt.Errorf("check: reading energy: %w", err)
	}

	// Adjacency is M4's — focus.Rank accepts an empty map and scores
	// every candidate on its own terms, which is the honest input until
	// something computes it. Passing a made-up one would be worse than
	// passing none.
	carry, held := prospection.Carry(items, map[string]float64{}, prospection.LowEnergy(energy, now), now)

	if len(carry) == 0 {
		return 0, nil
	}
	if !commit {
		return len(carry), nil
	}

	if err := r.channel.Send(ctx, "", renderDigest(carry, pending)); err != nil {
		return 0, r.record(ctx, now, ports.ActionCheckDeliveryFailed,
			fmt.Sprintf("the digest could not be delivered; its %d item(s) stay undelivered and tomorrow's digest carries them: %v", len(carry), err),
			checkDetail{})
	}

	for _, item := range carry {
		if err := r.triggers.Surface(ctx, item.ID, now); err != nil {
			return 0, fmt.Errorf("check: digest was sent but trigger %q was not marked delivered: %w", item.ID, err)
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
func renderDigest(carry []prospection.DigestItem, pending []ports.DueTrigger) string {
	text := make(map[string]string, len(pending))
	for _, t := range pending {
		text[t.ID] = t.Payload.ActionText
	}

	var b strings.Builder
	b.WriteString("Here " + plural(len(carry)) + ":")
	for _, item := range carry {
		line := text[item.ID]
		if line == "" {
			line = "something you asked me to remind you about"
		}
		b.WriteString("\n• " + line)
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return "is 1 thing for today"
	}
	return "are " + strconv.Itoa(n) + " things for today"
}
