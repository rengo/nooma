package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/reading"
	"github.com/rengo/nooma/testdata/llm"
)

// readingThreshold is ADR-0033's reversal criterion: below it, a model reads
// reminders too badly for the model-reads design to stand.
const readingThreshold = 90

// doctorChecksFor is the list a run executes: the plain checks, plus the
// reading when --reading asked for it. The reading is about 50 live calls, so
// a plain `nooma doctor` never makes them.
func doctorChecksFor(withReading bool) []doctorCheck {
	if !withReading {
		return doctorChecks
	}
	return append(slices.Clone(doctorChecks), doctorCheck{"reading", checkReading})
}

// checkReading sends the reminder phrasing set to the vault's
// capture_processing provider and reports how it reads (ADR-0033 point 5).
// It fails only on what must never happen: a stop read from a message that
// is not one, a provider that cannot be reached, or no provider to ask. A rate below the
// threshold is reported, and judged by the person who changed the prompt.
func checkReading(_ string, cfg *config.Config) error {
	cases, err := llm.ReminderCases()
	if err != nil {
		return fmt.Errorf("loading the phrasing set: %w", err)
	}
	provider, bound := qualityGateProviders(cfg, os.LookupEnv)["capture_processing"]
	if !bound {
		// A run that measured nothing must not read as a pass: the hard line
		// would be "0 of 0", and a green doctor would say so.
		return errors.New("capture_processing is not bound to a provider that can answer, so nothing was read: configure one first (`nooma init`)")
	}
	report := runReading(context.Background(), provider, cases, time.Now(), recordTimeout)
	if err := readingError(report); err != nil {
		return err
	}
	return readingDetail(report.String())
}

// readingDetail is the reading's success, which still has a report to print.
type readingDetail string

func (d readingDetail) Error() string { return indentLines(string(d)) }
func (readingDetail) doctorOK()       {}

func indentLines(s string) string { return strings.ReplaceAll(s, "\n", "\n      ") }

// readingError is the check's verdict on a report: nil unless it failed.
func readingError(r readingReport) error {
	if !r.failed() {
		return nil
	}
	return errors.New(indentLines(r.String()))
}

type outcomeRate struct {
	name  string
	n, ok int
}

// readingReport is what reading the phrasing set measured.
type readingReport struct {
	n, ok  int      // phrasings read, and read as expected
	missed []string // "id (field, field)" for each one that was not

	negatives      int      // phrasings that are not a stop
	falsePositives []string // ids of those read as a stop

	titleN, titleOK     int
	dayOnlyN, dayOnlyOK int // phrasings expecting a bare date
	timedN, timedOK     int // phrasings expecting a named time
	outcomes            []outcomeRate

	unreachable string // set instead of everything above when the provider stopped answering
}

func (r readingReport) failed() bool { return r.unreachable != "" || len(r.falsePositives) > 0 }

// runReading sends each phrasing through the production prompt (at its own
// `now` when it has one) and judges the answer against what it expects. The
// first transport failure stops the run and is reported alone, as the quality
// gate does: a provider that stopped answering says nothing about reading.
func runReading(ctx context.Context, provider ports.LLMProvider, cases []llm.Case, now time.Time, timeout time.Duration) readingReport {
	r := readingReport{outcomes: []outcomeRate{
		{name: reading.FieldTaskCheckinOutcome}, {name: reading.FieldNudgeOutcome}, {name: reading.FieldRelationOutcome},
	}}
	for _, c := range cases {
		at := now
		if c.Now != "" {
			if parsed, err := time.Parse(time.RFC3339, c.Now); err == nil {
				at = parsed
			}
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		resp, err := provider.Complete(callCtx, ports.LLMRequest{Prompt: reading.PromptAt(c, at), Task: "capture_processing", JSONOnly: true})
		cancel()
		if err != nil {
			return readingReport{unreachable: fmt.Sprintf("unreachable — %v (doctor waits up to %s per prompt)", err, timeout)}
		}

		misses, stop := []string{"(response)"}, false
		if cls, err := classify.Decode(resp.Text, at); err == nil {
			misses = reading.Check(c, cls)
			stop = cls.ReminderIntent != nil && *cls.ReminderIntent == classify.ReminderIntentStop
		}
		r.add(c, misses, stop)
	}
	return r
}

func (r *readingReport) add(c llm.Case, misses []string, readAsStop bool) {
	e := llm.Expect{}
	if c.Expect != nil {
		e = *c.Expect
	}
	missed := func(field string) bool {
		return slices.Contains(misses, field) || slices.Contains(misses, "(response)")
	}
	count := func(n, ok *int, field string) {
		*n++
		if !missed(field) {
			*ok++
		}
	}

	// The rate is ADR-0033's: only the reminder fields decide whether a
	// phrasing was read. A wrong title or day-only flag has its own line.
	r.n++
	if reading.ReminderMissed(misses) {
		r.missed = append(r.missed, fmt.Sprintf("%s (%s)", c.ID, strings.Join(misses, ", ")))
	} else {
		r.ok++
	}
	if e.ReminderIntent != string(classify.ReminderIntentStop) {
		r.negatives++
		if readAsStop {
			r.falsePositives = append(r.falsePositives, c.ID)
		}
	}
	if e.Title != nil {
		count(&r.titleN, &r.titleOK, reading.FieldTitle)
	}
	if e.DayOnly != nil && *e.DayOnly {
		count(&r.dayOnlyN, &r.dayOnlyOK, reading.FieldDayOnly)
	}
	if e.DayOnly != nil && !*e.DayOnly {
		count(&r.timedN, &r.timedOK, reading.FieldDayOnly)
	}
	for i, expected := range []string{e.TaskCheckinOutcome, e.NudgeOutcome, e.RelationOutcome} {
		if expected != "" {
			count(&r.outcomes[i].n, &r.outcomes[i].ok, r.outcomes[i].name)
		}
	}
}

func (r readingReport) String() string {
	if r.unreachable != "" {
		return "reading: " + r.unreachable
	}
	var b strings.Builder
	pct := 0
	if r.n > 0 {
		pct = 100 * r.ok / r.n
	}
	fmt.Fprintf(&b, "reminder reading: %d of %d (%d%%)", r.ok, r.n, pct)
	if pct < readingThreshold {
		fmt.Fprintf(&b, " — below %d%%, ADR-0033's reversal threshold", readingThreshold)
	}
	for _, m := range r.missed {
		b.WriteString("\n  missed: " + m)
	}
	fmt.Fprintf(&b, "\nstop false positives: %d of %d negatives", len(r.falsePositives), r.negatives)
	if len(r.falsePositives) > 0 {
		b.WriteString(" (" + strings.Join(r.falsePositives, ", ") + ") — this must be 0")
	}
	if r.titleN > 0 {
		fmt.Fprintf(&b, "\ntitles: %d of %d date-free and short", r.titleOK, r.titleN)
	}
	if r.dayOnlyN > 0 {
		fmt.Fprintf(&b, "\nday only: %d of %d bare dates read as bare dates", r.dayOnlyOK, r.dayOnlyN)
	}
	if r.timedN > 0 {
		fmt.Fprintf(&b, "\nnamed times: %d of %d kept their time", r.timedOK, r.timedN)
	}
	for _, o := range r.outcomes {
		if o.n > 0 {
			fmt.Fprintf(&b, "\n%s: %d of %d", o.name, o.ok, o.n)
		}
	}
	return b.String()
}
