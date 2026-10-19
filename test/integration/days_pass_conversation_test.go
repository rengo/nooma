//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/scheduler"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/goldenset"
)

// The days-pass gate: real phrasings, replayed from the model's recorded
// readings (testdata/llm/reminders), go in through the channel's capture
// handler over a real migrated vault, the scheduler's check pass runs every
// scheduler.ProactiveCheckInterval for weeks, and every message the user
// would see, and every message they sent, is a line of a golden transcript.
// A reminder regression changes a line, whichever function it lives in.
//
// Regenerate with -update, and read every changed line against doc 02 §7
// before committing it: a wrong line is a product bug, not a new golden.

// dpZone is the zone the readings were recorded in.
var dpZone = time.FixedZone("UTC-3", -3*60*60)

func dpAt(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, dpZone)
}

// dpConversation is the one chat the vault pushes to and the user writes in.
const dpConversation = "12449194"

// dpSay is one message the user sends at a local minute, read by the model as
// the recorded case id says. late marks a reading replayed later than it was
// recorded, which the transcript header names.
//
// text and reading, when set, are a hand-written message and reading in
// place of a recording: a phrasing not recorded yet. The transcript header
// says so; a phrasing the set records moves to its recorded id once it is.
type dpSay struct {
	at            time.Time
	id            string
	late          bool
	text, reading string
}

// dpScenario is one life: preferences saved before the first message, the
// messages, how long the server runs, and when it is down.
type dpScenario struct {
	name                string
	about               string
	quietEnd            string
	remindMeLead        int
	say                 []dpSay
	from, until         time.Time
	downFrom, downUntil time.Time
}

// dpLLM replays the recorded reading of the message being captured. Two other
// tasks answer what no reminder decides: the chat task a chitchat, with a
// placeholder for the model's own words, and the relation judge, comparing
// one reminder with another, the recorded "new" of
// testdata/llm/cases/relation-no-match-for-dry-cleaning.json. Any other call
// fails the walk.
type dpLLM struct {
	t    *testing.T
	next *goldenset.LLMExample
}

func (l *dpLLM) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	switch req.Task {
	case "capture_processing":
		if l.next == nil {
			l.t.Fatalf("a second classification for one message: %q", req.Prompt)
		}
		ex := l.next
		l.next = nil
		return ports.LLMResponse{Text: ex.Response, Model: ex.Model}, nil
	case "chat":
		return ports.LLMResponse{Text: "(chat reply)", Model: "replay"}, nil
	case "relation_evaluation":
		return ports.LLMResponse{Text: `{"outcome":"new"}`, Model: "claude-sonnet"}, nil
	}
	l.t.Fatalf("unscripted %q call: %q", req.Task, req.Prompt)
	return ports.LLMResponse{}, nil
}

// dpChannel is the transport: it records every message sent, at the local
// minute the clock says, and receives nothing (the walk hands messages to the
// capture handler itself, as the runner does).
type dpChannel struct {
	clock *daysClock
	lines *[]string
}

func (c *dpChannel) Name() string                                            { return "telegram" }
func (c *dpChannel) Receive(context.Context) ([]ports.ChannelMessage, error) { return nil, nil }
func (c *dpChannel) Confirm(context.Context, string) error                   { return nil }
func (c *dpChannel) Close() error                                            { return nil }
func (c *dpChannel) Send(_ context.Context, _ ports.ConversationID, text string) error {
	*c.lines = append(*c.lines, dpLine(c.clock.now, "→", text))
	return nil
}

// dpLine is one transcript line; a message of several lines continues
// indented under its own text.
func dpLine(at time.Time, arrow, text string) string {
	prefix := at.In(dpZone).Format("Mon 2006-01-02 15:04") + " " + arrow + " "
	return prefix + strings.ReplaceAll(text, "\n", "\n"+strings.Repeat(" ", len(prefix)-len(arrow)+1))
}

// live runs the scenario and returns its transcript.
func (s dpScenario) live(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	recordings := filepath.Join("..", "..", "testdata", "llm", "reminders")

	v, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	units, embeddings, triggers, timers := sqlite.NewUnitRepo(v), sqlite.NewEmbeddingRepo(v), sqlite.NewTriggerRepo(v), sqlite.NewTimerRepo(v)
	decisions, config, relations, questions := sqlite.NewDecisionLog(v), sqlite.NewConfigRepo(v), sqlite.NewRelationRepo(v), sqlite.NewPendingQuestionRepo(v)
	var prefs ports.ReminderPrefsUpdate
	if s.quietEnd != "" {
		prefs.QuietHoursEnd = &s.quietEnd
	}
	if s.remindMeLead != 0 {
		prefs.RemindMeLeadMinutes = &s.remindMeLead
	}
	if prefs != (ports.ReminderPrefsUpdate{}) {
		if err := config.SetReminderPrefs(ctx, prefs, s.from); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := embeddings.LoadIndex(ctx, consolidateITEmbedModel)
	if err != nil {
		t.Fatal(err)
	}

	// The serve wiring (cmd/nooma/wiring.go: wireBrain, wireFocus,
	// wireProactive) over one clock the walk moves.
	clock, ids, llm := &daysClock{now: s.from}, &daysIDs{}, &dpLLM{t: t}
	var lines []string
	ch := &dpChannel{clock: clock, lines: &lines}
	capture := brain.NewCaptureService(clock, ids, units, embeddings, sqlite.NewSearch(v), relations, decisions,
		llm, llm, llm, fakeprovider.NewEmbeddingFake(consolidateITEmbedModel), brain.NewIndex(idx), sqlite.NewSignalRepo(v),
		triggers, timers, 0.5, questions).WithReminderPrefs(config)
	keeper := brain.NewFocusKeeper(units, config, relations)
	// One chat log for the walk, as serve holds one for the process (ADR-0036).
	conv := brain.NewConversation()
	capture.WithConversation(conv)
	check := brain.NewCheckService(clock, triggers, timers, ids, decisions, ch, units, sqlite.NewStateRepo(v),
		llm, dpConversation, questions, keeper).WithQuietHours(config).WithConversation(conv)
	handle := channels.CaptureHandler(capture, ch, conv, nil)

	header := []string{"# " + s.about,
		fmt.Sprintf("# a check pass every %s from %s to %s", scheduler.ProactiveCheckInterval,
			s.from.Format("Mon 2006-01-02 15:04 -07:00"), s.until.Format("Mon 2006-01-02 15:04"))}
	if s.quietEnd != "" {
		header = append(header, "# quiet hours end at "+s.quietEnd)
	}
	if s.remindMeLead != 0 {
		header = append(header, fmt.Sprintf("# an asked reminder rings %d minutes before", s.remindMeLead))
	}
	pending := s.say
	for at := s.from; at.Before(s.until); at = at.Add(scheduler.ProactiveCheckInterval) {
		if at.Equal(s.downFrom) {
			lines = append(lines, dpLine(at, "··", "the server stops"))
		}
		if at.Equal(s.downUntil) {
			lines = append(lines, dpLine(at, "··", "the server starts"))
		}
		down := !at.Before(s.downFrom) && at.Before(s.downUntil)
		for len(pending) > 0 && !pending[0].at.After(at) {
			m := pending[0]
			pending = pending[1:]
			if down {
				t.Fatalf("%s: a message at %s while the server is down", m.id, m.at)
			}
			var ex goldenset.LLMExample
			if m.reading != "" {
				ex = goldenset.LLMExample{ID: m.id, Message: m.text, Response: m.reading, Model: "hand-written", Now: m.at.Format(time.RFC3339)}
				header = append(header, fmt.Sprintf("# %q is read by a hand-written reading, not a recording", m.text))
			} else if err := goldenset.Load(filepath.Join(recordings, m.id+".json"), &ex); err != nil {
				t.Fatalf("recording %s: %v", m.id, err)
			}
			recorded, err := time.Parse(time.RFC3339, ex.Now)
			if err != nil {
				t.Fatalf("recording %s: now %q: %v", m.id, ex.Now, err)
			}
			// A reading that names a date was resolved against the day it was
			// recorded on; said on another day it is a reading the model never gave.
			dated := strings.Contains(ex.Response, `"due_at"`) || strings.Contains(ex.Response, `"event_at"`)
			switch {
			case m.late:
				header = append(header, fmt.Sprintf("# %q is replayed at %s; its reading was recorded at %s",
					ex.Message, m.at.Format("Mon 01-02 15:04"), recorded.In(dpZone).Format("Mon 01-02 15:04")))
			case dated && (m.at.Before(recorded.Truncate(time.Minute)) || m.at.In(dpZone).YearDay() != recorded.In(dpZone).YearDay()):
				t.Fatalf("%s is said at %s but its dated reading was recorded at %s: mark it late", m.id, m.at, recorded)
			}
			clock.now = m.at
			lines = append(lines, dpLine(m.at, "←", ex.Message))
			open, err := triggers.Delivered(ctx)
			if err != nil {
				t.Fatal(err)
			}
			llm.next = &ex
			if err := handle(ctx, ports.ChannelMessage{ID: m.id, Conversation: dpConversation, Text: ex.Message, Channel: ch.Name()}); err != nil {
				t.Fatalf("%s: %v", m.id, err)
			}
			if llm.next != nil {
				t.Fatalf("%s: the capture never classified", m.id)
			}
			lines = append(lines, dpCheckins(t, decisions, open, m.at)...)
		}
		if down {
			continue
		}
		clock.now = at
		if _, err := check.Check(ctx, brain.CheckRequest{}); err != nil {
			t.Fatalf("Check at %v: %v", at, err)
		}
	}
	if len(pending) > 0 {
		t.Fatalf("%d messages after the walk ended", len(pending))
	}
	return strings.Join(header, "\n") + "\n" + strings.Join(lines, "\n") + "\n"
}

// dpCheckins is a line for each check-in a message answered, read from the
// decision_log rows the capture wrote at at: what it was about, among the
// check-ins open before it, and how it ended. A bare "listo" that resolves
// nothing reads unlike one that closes its check-in, which the outbound lines
// alone cannot show: neither is ever asked again.
func dpCheckins(t *testing.T, log ports.DecisionLog, open []ports.OpenCheckIn, at time.Time) []string {
	t.Helper()
	rows, err := log.Since(context.Background(), at.Add(-time.Nanosecond), -1) // strictly after
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range rows {
		outcome := map[ports.DecisionAction]string{ports.ActionCaptureCheckInResolved: "", ports.ActionCaptureCheckInSnoozed: "put off",
			ports.ActionCaptureCheckInUnmatched: "nothing open"}
		said, ok := outcome[d.Action]
		if !ok || !d.OccurredAt.Equal(at) {
			continue
		}
		var c struct {
			TriggerID  string `json:"trigger_id"`
			Resolution string `json:"resolution"`
		}
		if err := json.Unmarshal(d.Context, &c); err != nil {
			t.Fatal(err)
		}
		if said == "" {
			said = c.Resolution
		}
		about := c.TriggerID
		for _, o := range open {
			if o.ID == c.TriggerID {
				about = o.Payload.ActionText
			}
		}
		out = append(out, dpLine(at, "··", fmt.Sprintf("check-in %q: %s", about, said)))
	}
	return out
}

// TestDaysPass_RecordedConversations is the gate. Each scenario's transcript
// is testdata/days-pass/conversation-<name>.txt, compared exactly.
func TestDaysPass_RecordedConversations(t *testing.T) {
	for _, s := range dpScenarios() {
		t.Run(s.name, func(t *testing.T) {
			got := s.live(t)
			path := filepath.Join("..", "..", "testdata", "days-pass", "conversation-"+s.name+".txt")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v (run with -update to write it)", path, err)
			}
			if got != string(want) {
				t.Errorf("%s differs from what the user would have seen.\ngot:\n%s\nwant:\n%s", path, got, want)
			}
		})
	}
}

func dpScenarios() []dpScenario {
	return []dpScenario{
		{
			// The live case of 2026-10-17 (ADR-0036): a follow-up is read in
			// the chat it follows. "Es a las 08:00." moves the meeting it
			// follows (A: the dentist, an hour earlier, is outside the window
			// and not shown), stores no unit of its own, and so leaves nothing
			// for the next morning's digest to ask about. Read alone, recall
			// could not tell the two apart and asked.
			name:  "follow-up-moves-meeting",
			about: "a meeting is captured, and the next message moves it",
			from:  dpAt(10, 13, 19, 55), until: dpAt(10, 15, 0, 0),
			say: []dpSay{
				{at: dpAt(10, 13, 20, 0), id: "hand-capture-dentist", text: "dentista el viernes a las 10",
					reading: `{"type":"event","normalized_content":"Dentista el 2026-10-16 a las 10:00.","weight":0.7,` +
						`"decay_rate":0.05,"event_at":"2026-10-16T10:00:00-03:00","title":"Dentista","language":"es"}`},
				{at: dpAt(10, 13, 21, 5), id: "hand-capture-meeting", text: "Tengo reunión con Juan mañana a las 22:30",
					reading: `{"type":"event","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 22:30.","weight":0.7,` +
						`"decay_rate":0.05,"event_at":"2026-10-14T22:30:00-03:00","title":"Reunión con Juan","language":"es"}`},
				{at: dpAt(10, 13, 21, 10), id: "classify-followup-time-moves-meeting", text: "Es a las 08:00.",
					reading: `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 08:00.","weight":0.7,` +
						`"decay_rate":0.05,"event_at":"2026-10-14T08:00:00-03:00","title":"Reunión con Juan","language":"es","referent":"A"}`},
			},
		},
		{
			// The live case of 2026-10-19 (ADR-0037): told at 08:44 about a
			// dentist tomorrow at 10:00, the 24-hour lead would ring today at
			// 10:00, 75 minutes later, and the reply called it "the day
			// before". It is not armed: the reply names the 2-hour ring only,
			// and that is the one ring.
			name:  "told-day-lead",
			about: "an event tomorrow is captured before its 24-hour lead",
			from:  dpAt(10, 19, 8, 40), until: dpAt(10, 20, 12, 0),
			say: []dpSay{
				{at: dpAt(10, 19, 8, 44), id: "hand-capture-dentist-tomorrow", text: "mañana tengo dentista a las 10",
					reading: `{"type":"event","normalized_content":"Tengo dentista el 2026-10-20 a las 10:00.","weight":0.7,` +
						`"decay_rate":0.05,"event_at":"2026-10-20T10:00:00-03:00","title":"Dentista","language":"es"}`},
			},
		},
		{
			name:  "five-weeks",
			about: "every reminder kind the recordings hold, five weeks, the server down three days",
			from:  dpAt(10, 16, 10, 40), until: dpAt(11, 21, 0, 0),
			downFrom: dpAt(10, 19, 22, 0), downUntil: dpAt(10, 22, 21, 30),
			say: []dpSay{
				{at: dpAt(10, 16, 10, 42), id: "classify-task-remind-me-coffee"},
				{at: dpAt(10, 16, 10, 43), id: "classify-request-day-only-thursday"},
				{at: dpAt(10, 16, 10, 44), id: "classify-request-named-time-friday"},
				{at: dpAt(10, 16, 10, 45), id: "classify-task-remind-me-daily-pills"},
				{at: dpAt(10, 16, 10, 46), id: "classify-request-haceme-acordar"},
				{at: dpAt(10, 16, 10, 47), id: "classify-request-poneme-un-recordatorio"},
				{at: dpAt(10, 16, 10, 48), id: "classify-request-acordate-de-avisarme"},
				{at: dpAt(10, 16, 10, 49), id: "classify-title-has-no-date"},
				{at: dpAt(10, 16, 10, 50), id: "classify-reported-speech-avisame"},
				{at: dpAt(10, 16, 17, 30), id: "classify-request-english-call-ana", late: true},
				{at: dpAt(10, 17, 8, 50), id: "classify-checkin-snooze"},
				{at: dpAt(10, 17, 10, 0), id: "classify-negative-bare-listo-with-candidates"},
				{at: dpAt(10, 22, 21, 40), id: "classify-negative-ya-lo-hice-with-candidates"},
			},
		},
		{
			name:     "late-riser",
			about:    "a user whose night ends at 09:30: a named time rings inside it, a chosen one waits for its end",
			quietEnd: "09:30",
			from:     dpAt(10, 16, 10, 40), until: dpAt(10, 24, 12, 0),
			say: []dpSay{
				{at: dpAt(10, 16, 10, 43), id: "classify-request-day-only-thursday"},
				{at: dpAt(10, 16, 10, 44), id: "classify-request-named-time-friday"},
				{at: dpAt(10, 16, 10, 46), id: "classify-request-haceme-acordar"},
			},
		},
		{
			// The re-plan's catch-up of an asked unit is the asked one
			// (CatchUpAsked): back at 04:30, it waits for quiet end (07:00),
			// a time Nooma chose keeping the night rules (ADR-0031 point 6).
			// Plain CatchUp would read an unasked event's leads, find the
			// 2-hour one still ahead, and owe none.
			name:         "long-lead-night-return",
			about:        "a 12-hour asked lead whose ring the downtime ate, the server back at night",
			remindMeLead: 720,
			from:         dpAt(10, 16, 10, 40), until: dpAt(10, 18, 12, 0),
			downFrom: dpAt(10, 16, 21, 0), downUntil: dpAt(10, 17, 4, 30),
			say: []dpSay{{at: dpAt(10, 16, 10, 42), id: "classify-task-remind-me-coffee"}},
		},
		{
			// A digest is a morning's, or none (the noon cutoff): back at
			// 22:55 on Sunday, the server sends no digest that night, and the
			// re-ask Sunday's would have carried comes Monday morning. The
			// skipped morning burns no re-ask: both are still asked.
			name:  "noon-cutoff",
			about: "a check-in put off, the server down over the next morning and back at 22:55",
			from:  dpAt(10, 16, 10, 40), until: dpAt(10, 21, 12, 0),
			downFrom: dpAt(10, 17, 12, 0), downUntil: dpAt(10, 18, 22, 55),
			say: []dpSay{
				{at: dpAt(10, 16, 10, 46), id: "classify-request-haceme-acordar"},
				{at: dpAt(10, 17, 8, 50), id: "classify-checkin-snooze"},
			},
		},
	}
}
