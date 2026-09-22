package brain

import (
	"context"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// TodayService assembles the Today view model — the mirror's first read
// model (design §3.6). CheckService's own shell/worker shape: this struct
// holds the one ports.Clock, todayRunner never sees it, so
// brain_single_clock_read_test.go's one-Now()-per-file rule holds for
// this file the same way it already holds for check.go.
//
// It needs no provider: it reads repositories and calls core, so a vault
// with no LLM configured still renders Today. It is wired unconditionally
// at vault open, from wireBrain's own call site — never from inside
// wireScheduler's LLM-gated path, which skips wiring a service on a vault
// with no bound provider for an unrelated reason.
type TodayService struct {
	clock ports.Clock
	run   todayRunner
}

// NewTodayService wires a TodayService over the ports one Today request
// needs.
func NewTodayService(clock ports.Clock, units ports.UnitRepo, cfg ports.ConfigRepo, state ports.StateRepo, triggers ports.TriggerRepo, questions ports.PendingQuestionRepo, log ports.DecisionLog) *TodayService {
	return &TodayService{
		clock: clock,
		run:   todayRunner{units: units, cfg: cfg, state: state, triggers: triggers, questions: questions, log: log},
	}
}

// Today builds the view model at one instant — this file's one Now() call.
func (s *TodayService) Today(ctx context.Context) (Today, error) {
	return s.run.at(ctx, s.clock.Now())
}

// Today is what GET /ui renders. Every field is a value the brain already
// decided or a row the vault already holds; nothing here is computed by
// the template, and nothing here is written back — R6, I27.
type Today struct {
	Now     time.Time
	Focuses []Focus // focus.AllKinds() order: task, then load
	Digest  PendingDigest
	Status  VaultStatus
}

// Focus is one focus.Kind's own top-N, ranked by focus.Priority alone —
// no focus.Select, no hysteresis, no incumbent (design §3.6's own ruling:
// Select's first production caller is m4c's, with a real margin).
type Focus struct {
	Kind    focus.Kind
	Members []FocusMember
}

// FocusMember is one ranked unit with the text a view needs and the dates
// I18 says must never be confused. Score is the literal value focus.Rank
// produced — NaN included — never coerced; rendering it is PR 7's.
type FocusMember struct {
	ID      string
	Type    unit.Type
	Content string
	DueAt   *time.Time
	EventAt *time.Time
	Score   float64
}

// PendingDigest is what the morning delivery would carry if it went out
// at Now — prospection.Carry's own output over the same inputs
// assembleDigest reads, and nothing here is marked delivered or asked.
type PendingDigest struct {
	LowEnergy bool
	Items     []DigestLine // Carry's carry slice, in its order, joined to pending by ID
	Held      int          // len(held): counted, never listed
	Question  *ports.RelationQuestion
}

// DigestLine is one carried item, joined back to the pending trigger it
// came from for the text a view needs — prospection.DigestItem carries
// none.
type DigestLine struct {
	TriggerID string
	UnitID    *string
	Text      string
	FireAt    time.Time
	Deferrals int
}

// VaultStatus is the four SYSTEM lines the vault can answer. The other
// two — the effective bind and whether /ui sits behind a cookie — are the
// listener's own facts and reach the view through ui.Serving, never
// through this package.
type VaultStatus struct {
	LastConsolidationAt *time.Time
	Energy              *prospection.EnergyReading
	Undelivered         int
	OpenQuestions       int
}

// todayRunner does the read-only work of one Today request over every
// port it needs, except the clock — TodayService's own field.
type todayRunner struct {
	units     ports.UnitRepo
	cfg       ports.ConfigRepo
	state     ports.StateRepo
	triggers  ports.TriggerRepo
	questions ports.PendingQuestionRepo
	log       ports.DecisionLog
}

// at builds Today at the instant Today already read — design §3.6's own
// eight-step order, so the two focuses and the digest all agree with each
// other inside one response. Every read here is a GET; none of it writes
// (I27, proven by test/conformance's own writeGuard).
func (r todayRunner) at(ctx context.Context, now time.Time) (Today, error) {
	out := Today{Now: now}

	cfg, err := r.cfg.Load(ctx)
	if err != nil {
		return Today{}, fmt.Errorf("today: reading config: %w", err)
	}
	out.Status.LastConsolidationAt = cfg.ConsolidationLastRunAt

	energy, err := r.state.LatestEnergy(ctx)
	if err != nil {
		return Today{}, fmt.Errorf("today: reading energy: %w", err)
	}
	out.Status.Energy = energy
	low := prospection.LowEnergy(energy, now)

	pending, err := r.triggers.Undelivered(ctx)
	if err != nil {
		return Today{}, fmt.Errorf("today: undelivered triggers: %w", err)
	}
	out.Status.Undelivered = len(pending)

	history, err := r.log.Since(ctx, now.AddDate(0, 0, -digestHistoryDays), -1)
	if err != nil {
		return Today{}, fmt.Errorf("today: reading digest history: %w", err)
	}

	items, err := digestItems(ctx, r.units, pending, history)
	if err != nil {
		return Today{}, err
	}
	// Adjacency is M4's — the same reading assembleDigest already gives
	// this call (digest.go), applied here so the two agree.
	carry, held := prospection.Carry(items, map[string]float64{}, low, now)
	out.Digest.LowEnergy = low
	out.Digest.Items = joinDigestLines(carry, pending)
	out.Digest.Held = len(held)

	// No question on a low-energy morning — assembleDigest's own rule
	// (digest.go), read here rather than re-derived.
	if !low {
		unasked, err := r.questions.Unasked(ctx)
		if err != nil {
			return Today{}, fmt.Errorf("today: unasked questions: %w", err)
		}
		if len(unasked) > 0 {
			out.Digest.Question = &unasked[0]
		}
	}

	open, err := r.questions.Open(ctx)
	if err != nil {
		return Today{}, fmt.Errorf("today: open questions: %w", err)
	}
	out.Status.OpenQuestions = len(open)

	out.Focuses = make([]Focus, 0, len(focus.AllKinds()))
	for _, k := range focus.AllKinds() {
		f, err := r.rankFocus(ctx, k, now)
		if err != nil {
			return Today{}, err
		}
		out.Focuses = append(out.Focuses, f)
	}

	return out, nil
}

// rankFocus is one Kind's own two reads: the port already filters by
// focus.Types(k), so focus.Rank scores exactly what the type asks for,
// and LiveByIDs fetches the text focus.Candidate deliberately carries
// none of, for the top focus.DefaultSize alone rather than for the
// whole pool (design §3.7's own rejected-alternative table).
func (r todayRunner) rankFocus(ctx context.Context, k focus.Kind, now time.Time) (Focus, error) {
	candidates, err := r.units.LiveFocusCandidatesByType(ctx, focus.Types(k))
	if err != nil {
		return Focus{}, fmt.Errorf("today: focus candidates for %q: %w", k, err)
	}
	ranked := focus.Rank(candidates, map[string]float64{}, now)
	if len(ranked) > focus.DefaultSize {
		ranked = ranked[:focus.DefaultSize]
	}

	ids := make([]string, len(ranked))
	for i, rk := range ranked {
		ids[i] = rk.Candidate.ID
	}
	live, err := r.units.LiveByIDs(ctx, ids)
	if err != nil {
		return Focus{}, fmt.Errorf("today: live units for %q: %w", k, err)
	}
	byID := make(map[string]unit.Unit, len(live))
	for _, u := range live {
		byID[u.ID] = u
	}

	members := make([]FocusMember, 0, len(ranked))
	for _, rk := range ranked {
		u, ok := byID[rk.Candidate.ID]
		if !ok {
			// Archived between the two reads (design §3.7's N7): the view
			// shows one member fewer, and misreports nothing.
			continue
		}
		members = append(members, FocusMember{
			ID: u.ID, Type: u.Type, Content: u.Content,
			DueAt: u.DueAt, EventAt: u.EventAt, Score: rk.Score,
		})
	}
	return Focus{Kind: k, Members: members}, nil
}

// joinDigestLines pairs Carry's own carry slice — []prospection.DigestItem,
// which names an id and a deferral count but carries no text — back to the
// []ports.DueTrigger pending already holds, mirroring renderDigest's own
// text[t.ID] lookup (digest.go).
func joinDigestLines(carry []prospection.DigestItem, pending []ports.DueTrigger) []DigestLine {
	byID := make(map[string]ports.DueTrigger, len(pending))
	for _, t := range pending {
		byID[t.ID] = t
	}

	lines := make([]DigestLine, 0, len(carry))
	for _, item := range carry {
		t, ok := byID[item.ID]
		if !ok {
			// Carry never invents an id (its own doc comment); this is
			// unreachable in practice and skipped rather than trusted.
			continue
		}
		lines = append(lines, DigestLine{
			TriggerID: item.ID, UnitID: t.UnitID, Text: t.Payload.ActionText,
			FireAt: t.FireAt, Deferrals: item.Deferrals,
		})
	}
	return lines
}
