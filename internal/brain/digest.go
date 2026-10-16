package brain

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/phrase"
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
// items it carried and how many past reminders it lapsed (or, on a dry
// run, would).
//
// Everything it decides is prospection's: DigestDue, in the scan's night,
// says whether a digest is owed, LowEnergy reads the care gate, and Carry
// splits what goes out from what waits. This supplies their inputs and
// persists their outcome.
func (r checkRunner) assembleDigest(ctx context.Context, now time.Time, commit bool) (int, int, error) {
	if r.channel == nil {
		// No channel, no digest. The push path already declines to mark
		// anything delivered without one; the digest must too, and the
		// asymmetry would have been worse here — a digest with no channel
		// would surface every carried item at once, recording a delivery
		// nobody received and removing them from tomorrow's digest
		// forever. `nooma check` on a Telegram-less vault is exactly this
		// case, and it is what caught it.
		return 0, 0, nil
	}

	history, err := r.log.Since(ctx, now.AddDate(0, 0, -digestHistoryDays), -1)
	if err != nil {
		return 0, 0, fmt.Errorf("check: reading digest history: %w", err)
	}

	if !r.night.DigestDue(lastDigestAt(history), now) {
		return 0, 0, nil
	}

	energy, err := r.state.LatestEnergy(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("check: reading energy: %w", err)
	}
	// Read once, used twice: Carry's own truncation, and the gate on
	// asking. Recomputing it at the second site would be the same rule in
	// two places, which is the drift this file already refuses elsewhere.
	low := prospection.LowEnergy(energy, now)

	// BEFORE anything is sent, and before the queue is read: a question
	// this pass expires must not also be a question this pass asks, and
	// the digest going out now must not count against the question it may
	// be carrying.
	if _, err := r.expireStaleQuestions(ctx, history, now, commit); err != nil {
		return 0, 0, err
	}

	// Read before this digest surfaces anything, so what it delivers now is
	// not counted as open through it.
	reasks, err := r.openCheckins(ctx, history, now, commit)
	if err != nil {
		return 0, 0, err
	}

	pending, err := r.triggers.Undelivered(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("check: undelivered triggers: %w", err)
	}

	// A reminder whose event is past is lapsed before anything reads the list
	// (ADR-0032). A late or superseded one-shot row is skipped, held for the
	// re-plan step; a recurring one is not judged late (a push's window).
	pending, past, err := splitPast(ctx, r.gate(), pending, now)
	if err != nil {
		return 0, 0, err
	}
	// What is left of the one-shot reminders belongs to the push path, which
	// retries it. The digest carries only the recurring ones waiting for it.
	pending = slices.DeleteFunc(pending, func(t ports.DueTrigger) bool { return t.RecurrenceRule == nil })
	lapsed := 0
	for _, t := range past {
		done, err := r.retire(ctx, staleRow{t, false, prospection.LapsePassed}, now, commit)
		if err != nil {
			return 0, 0, err
		}
		if done {
			lapsed++
		}
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
			return 0, lapsed, err
		}
	}

	if len(pending) == 0 && len(reasks) == 0 && question == nil {
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
		return 0, lapsed, nil
	}

	items, err := digestItems(ctx, r.units, pending, history)
	if err != nil {
		return 0, lapsed, err
	}

	// This first split is the VERDICT, not what is sent: its length decides the
	// two returns below. Adjacency changes which items carry and in what order,
	// and how many only on a low-energy morning, when Carry truncates; a dry
	// run's count under the message bound can then differ from the real pass's
	// (doc 02 §7). The split that is sent is re-ranked with adjacency once the
	// focus is computed, after every return that does not send.
	carry, held := prospection.Carry(items, map[string]float64{}, low, now)

	if len(carry) == 0 && len(reasks) == 0 && question == nil {
		return 0, lapsed, nil
	}
	if !commit {
		// The count of the lines the message bound keeps. Adjacency can reorder
		// the real pass's carry, which a dry run does not compute (it reads no
		// focus), so under a bound that cuts the two may keep different lines.
		_, sent, _ := r.digestText(ctx, now, carry, pending, reasks, question)
		return len(sent), lapsed, nil
	}

	if r.conversation == "" {
		// Same rule the pushed path takes: no destination, no send. The
		// items stay undelivered and tomorrow's digest carries them,
		// which is exactly what a failed send would have left behind —
		// without asking the transport to parse an empty chat id first.
		return 0, lapsed, r.record(ctx, now, ports.ActionCheckDeliveryFailed,
			fmt.Sprintf("the digest could not be delivered; this vault has no conversation to "+
				"push to, so its %d item(s) stay undelivered and tomorrow's digest carries them", len(carry)),
			checkDetail{})
	}

	// Only here, past every path that does not send: an empty digest, a dry
	// run and a missing conversation have all returned, so none of them reads
	// the keeper, and none can leave a check.focus.unavailable row.
	round, haveRound, err := r.digestFocus(ctx, now)
	if err != nil {
		return 0, lapsed, err
	}

	if haveRound {
		// Adjacency to the incumbent this digest loaded, P: its own Select has
		// not run yet. A fresh keeper holds none, so R10's empty adjacency.
		carry, held = prospection.Carry(items, carryAdjacency(round.adjacent, pending), low, now)
	}

	// What fits one message is sent; the rest stays undelivered for tomorrow.
	text, sent, asked := r.digestText(ctx, now, carry, pending, reasks, question)
	if err := r.channel.Send(ctx, r.conversation, text); err != nil {
		return 0, lapsed, r.record(ctx, now, ports.ActionCheckDeliveryFailed,
			fmt.Sprintf("the digest could not be delivered; its %d item(s) stay undelivered and tomorrow's digest carries them: %v", len(sent), err),
			checkDetail{})
	}
	// The user has now seen this digest, so its selection becomes the
	// incumbent: published only after Send succeeds, because an unsent digest
	// must not move a focus the user never saw. A question-only digest
	// publishes too; the carry count is not the criterion, the send is.
	//
	// This precedes the Surface loop on purpose: the user has seen the digest
	// whether or not marking its triggers delivered succeeds, so a Surface
	// failure must not take the incumbent back.
	if haveRound {
		r.focus.publish(round)
	}

	// Stamped in the order shown, a second apart (surfaced_at's precision), so
	// the newest open check-in, which a bare answer reaches, is the digest's
	// last line (doc 02 §7).
	shown := 0
	stamp := func() time.Time {
		shown++
		return now.Add(time.Duration(shown-1) * time.Second)
	}
	for _, item := range sent {
		if err := r.triggers.Surface(ctx, item.ID, stamp()); err != nil {
			return 0, lapsed, fmt.Errorf("check: digest was sent but trigger %q was not marked delivered: %w", item.ID, err)
		}
	}
	reasked := make(map[string]ports.DueTrigger, len(reasks))
	for _, t := range reasks {
		reasked[t.ID] = t
	}
	for _, id := range asked {
		if err := r.markReasked(ctx, reasked[id], stamp(), now); err != nil {
			return 0, lapsed, err
		}
	}
	if question != nil {
		// After the send, exactly as Surface is: MarkAsked is one-way
		// (asked_at IS NULL is its precondition), so a question marked
		// before a failed send would be a question nobody was ever asked.
		if err := r.questions.MarkAsked(ctx, question.ID, now); err != nil {
			return 0, lapsed, fmt.Errorf("check: digest was sent but question %q was not marked asked: %w", question.ID, err)
		}
		if err := r.recordQuestion(ctx, now, ports.ActionCheckDigestQuestionAsked,
			fmt.Sprintf("the daily digest asked about the relation between %q and %q", question.FromContent, question.ToContent),
			questionDetail{QuestionID: question.ID, RelationID: question.RelationID}); err != nil {
			return 0, lapsed, err
		}
	}
	if err := r.record(ctx, now, ports.ActionCheckDigestSent,
		digestSentRationale(len(sent), len(held), len(carry)-len(sent), len(asked), len(reasks)-len(asked)),
		checkDetail{}); err != nil {
		return 0, lapsed, err
	}

	// One row per held item, and this is not only for the glass box: the
	// deferral count is derived from these rows (design §3.4), so a held
	// item that wrote none would reset its own patience every morning and
	// never reach MaxDigestDeferrals. The audit trail IS the counter.
	for _, item := range held {
		if err := r.record(ctx, now, ports.ActionCheckDigestHeld,
			fmt.Sprintf("trigger %q was held back by the low-energy gate; it has now been held %d time(s)", item.ID, item.Deferrals+1),
			checkDetail{ID: item.ID}); err != nil {
			return 0, lapsed, err
		}
	}

	return len(sent), lapsed, nil
}

// digestSentRationale is the check.digest.sent row's text: the day's items,
// then the check-ins asked again, counted apart; it mentions what the message
// bound left for later only when there was any.
func digestSentRationale(sent, held, dropped, asked, waiting int) string {
	text := fmt.Sprintf("the daily digest carried %d item(s) and held %d", sent, held)
	if dropped > 0 {
		text += fmt.Sprintf(" and left %d for tomorrow because the message would not fit", dropped)
	}
	if asked > 0 {
		text += fmt.Sprintf("; it asked %d check-in(s) again", asked)
	}
	if waiting > 0 {
		text += fmt.Sprintf(" and left %d to ask on a later morning because the message would not fit", waiting)
	}
	return text
}

// digestText is the digest's message and the items it holds: the carried ones,
// then the check-ins it asks again (doc 02 §7). The digest speaks the newest
// capture's language (ADR-0032 point 5).
func (r checkRunner) digestText(ctx context.Context, now time.Time, carry []prospection.DigestItem, pending, reasks []ports.DueTrigger, question *ports.RelationQuestion) (string, []prospection.DigestItem, []string) {
	set := r.setFor(ctx, "")
	return digestMessage(set, carry, r.saidNow(ctx, pending, now, set), reaskSaid(ctx, r.units, reasks, now, set), question)
}

// digestFocus computes the focus the digest is about to be sent against. A
// nil keeper means no incumbent and no round (haveRound is false).
//
// A compute failure is not fatal: a focus that cannot be computed must not
// stop a digest the user is owed, and the digest worked without one before
// m4c. It is recorded as one check.focus.unavailable row, the digest is sent
// as it stands, and nothing is published. The row repeats on each retry scan
// while the digest stays unsent, because DigestDue holds until one is sent;
// nothing de-duplicates it. Only a failure to RECORD the row is returned.
func (r checkRunner) digestFocus(ctx context.Context, now time.Time) (round focusRound, haveRound bool, err error) {
	if r.focus == nil {
		return focusRound{}, false, nil
	}
	round, err = r.focus.compute(ctx, now)
	if err != nil {
		return focusRound{}, false, r.record(ctx, now, ports.ActionCheckFocusUnavailable,
			fmt.Sprintf("the focus could not be computed, so the digest is sent without it and the incumbent is left where it was: %v", err),
			checkDetail{})
	}
	return round, true, nil
}

// carryAdjacency maps unit-keyed adjacency onto the trigger ids Carry ranks by
// (prospection/digest.go): Carry sets each candidate's ID to its trigger's id,
// so a map keyed by unit id would never match. A trigger with no unit, or whose
// unit has no adjacency entry, gets none.
func carryAdjacency(byUnit map[string]float64, pending []ports.DueTrigger) map[string]float64 {
	byTrigger := make(map[string]float64, len(pending))
	for _, t := range pending {
		if t.UnitID == nil {
			continue
		}
		if strength, ok := byUnit[*t.UnitID]; ok {
			byTrigger[t.ID] = strength
		}
	}
	return byTrigger
}

// digestItems turns undelivered triggers into what Carry consumes: a
// candidate for ranking, and how many digests each has already waited
// through.
//
// A package function, not a checkRunner method: Today (today.go) needs
// the identical read for the identical reason — its own PENDING DIGEST
// mirrors what assembleDigest would carry — and a second copy on
// todayRunner would be the same rule in two places, which is how the
// digest's Carry order and Today's Carry order would drift apart (design
// §3.6).
func digestItems(ctx context.Context, units ports.UnitRepo, pending []ports.DueTrigger, history []ports.Decision) ([]prospection.DigestItem, error) {
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

	candidates, err := units.LiveFocusCandidates(ctx, ids)
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

// saidNow is pending as the digest says it: each text is its unit's now, with
// when, or the one it was armed with when the unit cannot be read (ADR-0032).
func (r checkRunner) saidNow(ctx context.Context, pending []ports.DueTrigger, now time.Time, set phrase.Set) []ports.DueTrigger {
	return saidNow(ctx, r.units, pending, now, set)
}

// saidNow is the method over any unit repo; Today says its lines the same way.
func saidNow(ctx context.Context, units ports.UnitRepo, pending []ports.DueTrigger, now time.Time, set phrase.Set) []ports.DueTrigger {
	out := slices.Clone(pending)
	for i, t := range out {
		if u, ok := readUnit(ctx, units, t); ok {
			text, when := unitWords(u, t, now, set)
			if when != "" {
				text = set.Line(text, when)
			}
			out[i].Payload.ActionText = text
		} else {
			out[i].Payload.ActionText = clip(t.Payload.ActionText)
		}
	}
	return out
}

// digestMessageUnits is Telegram's bound on one message, in the UTF-16 units it
// counts (render.go's reminderTextRunes bounds a line; nothing bounded the sum).
// A rendering bound nothing else branches on, like questionSnippetRunes, so it
// carries no docs/02 §13 row.
const digestMessageUnits = 4096

// digestLine is one line of the digest and the carried items it stands for: a
// unit's reminders are one line.
type digestLine struct {
	text  string
	items []prospection.DigestItem
}

// renderDigest is what the digest says: a header naming the count, then one
// line per item, and the carried items it holds. A digest over digestMessageUnits
// drops the lines past the bound (the last first) and ends saying how many: the
// items of a dropped line are not in the second result, so the caller leaves
// them undelivered and tomorrow's digest carries them. The question, when there
// is one, is never the thing dropped.
//
// Held items are not mentioned. A digest that listed what it withheld
// would defeat the low-energy gate it came from — the point of holding
// something back is that the person does not have to think about it today.
func renderDigest(set phrase.Set, carry []prospection.DigestItem, pending []ports.DueTrigger, question *ports.RelationQuestion) (string, []prospection.DigestItem) {
	text, sent, _ := digestMessage(set, carry, pending, nil, question)
	return text, sent
}

// digestMessage is renderDigest with the check-ins asked again (doc 02 §7): a
// block after the day's items, each re-ask clipped like a day line. When the
// message would exceed digestMessageUnits, the re-asks give way first, the last
// first, and only then the day's lines: a re-ask left out waits for a later
// morning, unasked and uncounted, so re-asks never wedge a digest. It returns
// the day's items kept and the ids of the check-ins asked, in the order shown.
func digestMessage(set phrase.Set, carry []prospection.DigestItem, pending, reasks []ports.DueTrigger, question *ports.RelationQuestion) (string, []prospection.DigestItem, []string) {
	byID := make(map[string]ports.DueTrigger, len(pending))
	for _, t := range pending {
		byID[t.ID] = t
	}

	// One line per unit, however many of its reminders are carried (ADR-0032
	// point 4); a reminder of no unit stands alone.
	var lines []digestLine
	lineOf := map[string]int{}
	for _, item := range carry {
		t := byID[item.ID]
		if t.UnitID != nil {
			if i, seen := lineOf[*t.UnitID]; seen {
				lines[i].items = append(lines[i].items, item)
				continue
			}
			lineOf[*t.UnitID] = len(lines)
		}
		line := t.Payload.ActionText
		if line == "" {
			line = set.SomethingYouAsked
		}
		lines = append(lines, digestLine{text: line, items: []prospection.DigestItem{item}})
	}

	asks := make([]string, 0, len(reasks))
	for _, t := range reasks {
		asks = append(asks, set.DigestItem(set.Done(clip(t.Payload.ActionText))))
	}

	keep, ask := len(lines), len(asks)
	text := composeDigest(set, lines, keep, asks[:ask], question)
	for utf16Len(text) > digestMessageUnits && (ask > 0 || keep > 1) {
		if ask > 0 {
			ask--
		} else {
			keep--
		}
		text = composeDigest(set, lines, keep, asks[:ask], question)
	}
	var sent []prospection.DigestItem
	for _, l := range lines[:keep] {
		sent = append(sent, l.items...)
	}
	asked := make([]string, 0, ask)
	for _, t := range reasks[:ask] {
		asked = append(asked, t.ID)
	}
	return text, sent, asked
}

// composeDigest is the message with the first keep lines, and the note of the
// lines left out when there are any.
func composeDigest(set phrase.Set, lines []digestLine, keep int, asks []string, question *ports.RelationQuestion) string {
	var b strings.Builder
	if keep > 0 {
		b.WriteString(set.DigestHeader(keep))
		for _, l := range lines[:keep] {
			b.WriteString("\n" + set.DigestItem(l.text))
		}
		if keep < len(lines) {
			b.WriteString("\n" + set.More(len(lines)-keep))
		}
	}
	if len(asks) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(set.AskAgain + "\n" + strings.Join(asks, "\n"))
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
			b.WriteString("\n\n" + set.OneMore)
		}
		b.WriteString(questionLine(set, *question))
	}
	return b.String()
}

// utf16Len is how long Telegram counts s: in UTF-16 code units.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// questionLine is the digest's relation question, in doc 02 §4's own
// wording ("I linked X with Y, are they related?").
func questionLine(set phrase.Set, q ports.RelationQuestion) string {
	return set.Question(snippet(q.FromContent), snippet(q.ToContent))
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
// digests have gone out on without an answer, as a state transition
// (resolution = expired). Nothing is deleted — non-negotiable #6.
//
// It runs HERE and not in nightly consolidation, because the bound is
// counted in DIGESTS and the digest path is the only one that reads
// decision_log's ActionCheckDigestSent rows. Consolidation would need a
// second reader of the same audit rows to answer a question it does not
// otherwise ask — heldCounts' own argument, applied to a second counter.
//
// digestHistoryDays is already MaxDigestDeferrals + 2, so the window this
// pass has ALREADY read is exactly wide enough to count them, and this
// costs no additional read.
//
// Two properties fall out of the port rather than out of a second rule. A
// question is asked exactly once (MarkAsked's asked_at IS NULL
// precondition, and Unasked never returns an asked row), so the open pool
// is bounded at MaxDigestDeferrals by arithmetic. And a vault whose
// digests stop going out expires nothing, which is correct: it also asked
// nothing.
func (r checkRunner) expireStaleQuestions(ctx context.Context, history []ports.Decision, now time.Time, commit bool) (int, error) {
	if r.questions == nil {
		return 0, nil
	}

	open, err := r.questions.Open(ctx)
	if err != nil {
		return 0, fmt.Errorf("check: open questions: %w", err)
	}

	expired := 0
	for _, q := range open {
		if q.AskedAt == nil {
			// Open returns none, by contract. Skipped rather than fatal:
			// a row that cannot be counted must not stop the digest, and
			// undercounting only leaves a question open one pass longer.
			continue
		}
		if digestsSince(history, *q.AskedAt) < prospection.MaxDigestDeferrals {
			continue
		}
		expired++
		if !commit {
			continue
		}
		if err := r.questions.Expire(ctx, q.ID, now); err != nil {
			return 0, fmt.Errorf("check: expire question %q: %w", q.ID, err)
		}
		if err := r.recordQuestion(ctx, now, ports.ActionCheckQuestionExpired,
			fmt.Sprintf("the question about %q and %q went unanswered through %d digest(s) and expired",
				q.FromContent, q.ToContent, prospection.MaxDigestDeferrals),
			questionDetail{QuestionID: q.ID, RelationID: q.RelationID, Resolution: string(ports.QuestionExpired)}); err != nil {
			return 0, err
		}
	}
	return expired, nil
}

// digestsSince is how many digests went out strictly after at, counted
// from the audit trail — heldCounts' own form, for the second counter
// design §3.5 needed.
//
// Strictly after, and that is the whole of two rules at once: the digest
// that asked a question writes its ActionCheckDigestSent row at the same
// instant MarkAsked stamps asked_at, so it does not count against the
// question it asked; and the sweep runs before this pass sends, so neither
// does the digest going out now.
func digestsSince(history []ports.Decision, at time.Time) int {
	n := 0
	for i := range history {
		if history[i].Action == ports.ActionCheckDigestSent && history[i].OccurredAt.After(at) {
			n++
		}
	}
	return n
}
