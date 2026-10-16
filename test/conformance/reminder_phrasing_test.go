package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/reading"
	"github.com/rengo/nooma/test/support/goldenset"
	"github.com/rengo/nooma/testdata/llm"
)

// TestReminderPhrasingSetReadsAsExpected replays every recorded phrasing of
// testdata/llm/reminders/ against its hand-written expectation (ADR-0033
// point 5): the recording is rebuilt into its prompt and must hash to the
// prompt_sha256 it was made against, so a prompt change fails here and names
// the cases to record again. A case still awaiting its recording is skipped
// and counted: the merge gate for those is scripts/recordings-complete.sh,
// not this test.
func TestReminderPhrasingSetReadsAsExpected(t *testing.T) {
	dir := filepath.Join(repoRootFromCaller(t), "testdata", "llm", "reminders")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	awaiting, total := 0, 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		total++
		var ex goldenset.LLMExample
		if err := goldenset.Load(filepath.Join(dir, entry.Name()), &ex); err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
			continue
		}
		c := ex.Case()
		if want := strings.TrimSuffix(entry.Name(), ".json"); c.ID != want {
			t.Errorf("%s holds id %q: a case's file is named by its id", entry.Name(), c.ID)
		}
		if err := reading.ValidateCase(c); err != nil {
			t.Errorf("%s: %v", c.ID, err)
		}
		t.Run(c.ID, func(t *testing.T) {
			if ex.Record {
				awaiting++
				t.Skip("awaiting recording (nooma doctor --record)")
			}
			if err := reading.Replay(c, ex.Response); err != nil {
				t.Error(err)
			}
		})
	}
	t.Logf("reminder phrasing set: %d case(s), %d awaiting recording", total, awaiting)
}

// TestReminderPhrasingSetCoversWhatTheDesignListsAndSpecR1 pins the shape of
// the set (design D1c, spec R1): it is not a count to reach but the cases the
// judges raised, so a deletion that leaves the number up still fails here.
func TestReminderPhrasingSetCoversWhatTheDesignListsAndSpecR1(t *testing.T) {
	cases, err := llm.ReminderCases()
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) < 40 {
		t.Errorf("the set holds %d phrasings, want about 40 or more", len(cases))
	}
	has := func(what string, pred func(llm.Case) bool) int {
		t.Helper()
		n := 0
		for _, c := range cases {
			if pred(c) {
				n++
			}
		}
		if n == 0 {
			t.Errorf("the set has no phrasing for %s", what)
		}
		return n
	}
	bare := func(m string) func(llm.Case) bool {
		return func(c llm.Case) bool { return c.Message == m && len(c.CandidatesList) > 0 }
	}
	kind := func(k string) func(llm.Case) bool {
		return func(c llm.Case) bool { return c.OpenQuestion != nil && c.OpenQuestion.Kind == k }
	}
	expect := func(f func(*llm.Expect) bool) func(llm.Case) bool {
		return func(c llm.Case) bool { return c.Expect != nil && f(c.Expect) }
	}

	// Spec R1's table, one phrasing each.
	for _, m := range []string{
		"recordame comprar café mañana a las 10", "Remind me to call Ana at 5pm", "si no, avisame",
		"si no puedo, avisame", "recordámelo", "recordame qué hablamos con Juan",
		"my dentist sent a reminder to confirm", "recordé que tenía que comprar café",
		"Ana me dijo 'avisame cuando llegues'", "no me lo recuerdes más", "listo, no me lo recuerdes más",
		"dont remind me anymore", "listo, nos vemos mañana", "ya lo hice",
	} {
		has("R1's "+m, func(c llm.Case) bool { return strings.HasPrefix(c.Message, m) || c.Message == m })
	}
	for _, prefix := range []string{"no te olvides de recordarme", "haceme acordar", "acordate de avisarme", "poneme un recordatorio", "que no se me olvide"} {
		has("R1's "+prefix, func(c llm.Case) bool { return strings.HasPrefix(c.Message, prefix) })
	}
	// The typographic apostrophe is what a phone keyboard types.
	has("don’t remind me anymore, with a curly apostrophe", func(c llm.Case) bool { return strings.HasPrefix(c.Message, "don’t remind me anymore") })
	has("don't remind me anymore", func(c llm.Case) bool { return strings.HasPrefix(c.Message, "don't remind me anymore") })

	// The negatives: a bare answer while the candidate list is not empty.
	for _, m := range []string{"no", "sí", "listo", "ya"} {
		has("a bare "+m+" with candidates", bare(m))
	}
	for _, c := range cases {
		if c.Message == "no" && len(c.CandidatesList) > 0 && c.Expect != nil && c.Expect.ReminderIntent == "stop" {
			t.Errorf("%s: a bare no with candidates is expected to be a stop", c.ID)
		}
	}
	has("an undo answered by a bare sí", func(c llm.Case) bool {
		return c.Message == "sí" && c.OpenQuestion != nil && c.OpenQuestion.Kind == "undo"
	})

	// Every open question, and the answers to them.
	for _, k := range []string{"remind_when", "which_reminder", "undo"} {
		has("an open "+k, kind(k))
	}
	has("a choice answer", expect(func(e *llm.Expect) bool { return e.QuestionAnswer == "choice" && e.ChoiceNumber != nil }))
	has("a keep", expect(func(e *llm.Expect) bool { return e.ReminderIntent == "keep" }))
	has("one candidate and a message about something else, unclear", func(c llm.Case) bool {
		return len(c.CandidatesList) == 1 && c.Expect != nil && c.Expect.ReminderIntent == "stop" && c.Expect.ReminderTarget == "unclear"
	})

	// Day only, titles, and the check-in answers.
	has("a bare date", expect(func(e *llm.Expect) bool { return e.DayOnly != nil && *e.DayOnly }))
	has("a named time", expect(func(e *llm.Expect) bool { return e.DayOnly != nil && !*e.DayOnly }))
	has("a title", expect(func(e *llm.Expect) bool { return e.Title != nil && *e.Title }))
	has("a task check-in answer", expect(func(e *llm.Expect) bool { return e.TaskCheckinOutcome != "" }))
	// Every outcome the check-in can be answered with, and a done that is not
	// next to a list of reminders that may be stopped (link 1b).
	for _, o := range []string{"done", "snooze", "drop"} {
		has("a task check-in answered "+o, expect(func(e *llm.Expect) bool { return e.TaskCheckinOutcome == o }))
	}
	for _, o := range []string{"engaged", "declined"} {
		has("a nudge answered "+o, expect(func(e *llm.Expect) bool { return e.NudgeOutcome == o }))
	}
	has("a done with no list", func(c llm.Case) bool {
		return len(c.CandidatesList) == 0 && c.Expect != nil && c.Expect.TaskCheckinOutcome == "done"
	})
	has("a nudge answer", expect(func(e *llm.Expect) bool { return e.NudgeOutcome != "" }))
	has("a relation answer", expect(func(e *llm.Expect) bool { return e.RelationOutcome != "" }))

	// Links 2 and 7 replay these two.
	for _, id := range []string{"classify-task-remind-me-coffee", "classify-task-remind-me-daily-pills"} {
		has(id, func(c llm.Case) bool { return c.ID == id })
	}
}

// listsOf is every numbered entry a case shows the model: the candidate list,
// and the which_reminder question's own list.
func listsOf(c llm.Case) []llm.Entry {
	entries := append([]llm.Entry(nil), c.CandidatesList...)
	if c.OpenQuestion != nil {
		entries = append(entries, c.OpenQuestion.List...)
	}
	return entries
}

// TestReminderPhrasingSetListsAreWhatThePhraseRendererWrites: production
// writes a candidate's date through internal/core/phrase (Time for an instant,
// Date for a whole day) and the list through classify.RenderReminderList. The
// set is recorded against exactly those bytes, so a different rendering later
// would be a different prompt, and this fails first. The set's lists are in
// Spanish, the fallback of a user who has written in it.
func TestReminderPhrasingSetListsAreWhatThePhraseRendererWrites(t *testing.T) {
	cases, err := llm.ReminderCases()
	if err != nil {
		t.Fatal(err)
	}
	es := phrase.For(classify.LanguageES)
	seen := 0
	for _, c := range cases {
		for _, e := range listsOf(c) {
			seen++
			at, err := time.Parse(time.RFC3339, e.At)
			if err != nil {
				t.Errorf("%s: entry %q has no instant (at): %v", c.ID, e.Label, err)
				continue
			}
			want := es.Time(at)
			if e.DayOnly {
				want = es.Date(at)
			}
			if e.When != want {
				t.Errorf("%s: entry %q says %q, the renderer writes %q", c.ID, e.Label, e.When, want)
			}
		}
		q := c.OpenQuestion
		if q == nil || q.Kind != string(classify.QuestionWhichReminder) {
			continue
		}
		lead, _, _ := strings.Cut(q.Text, "\n")
		var entries []classify.ReminderEntry
		for _, e := range q.List {
			entries = append(entries, classify.ReminderEntry{Label: e.Label, When: e.When})
		}
		if want := classify.WhichReminderText(lead, entries); q.Text != want {
			t.Errorf("%s: the which_reminder text is not lead + RenderReminderList:\n%q\nwant\n%q", c.ID, q.Text, want)
		}
	}
	if seen == 0 {
		t.Error("the set lists no reminder at all")
	}
}

// TestReminderPhrasingSetContextIsInTheFutureOfItsNow: a case that shows the
// model a list or a question carries the now it was written at, and every
// listed reminder is after it. Without an authored now the recording would use
// the clock of the day it is made, and a list of reminders already in the past
// is a prompt production never sends.
func TestReminderPhrasingSetContextIsInTheFutureOfItsNow(t *testing.T) {
	cases, err := llm.ReminderCases()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if len(c.CandidatesList) == 0 && c.OpenQuestion == nil {
			continue
		}
		now, err := time.Parse(time.RFC3339, c.Now)
		if err != nil {
			t.Errorf("%s: a case with a list or an open question needs an authored now: %v", c.ID, err)
			continue
		}
		for _, e := range listsOf(c) {
			if at, err := time.Parse(time.RFC3339, e.At); err == nil && !at.After(now) {
				t.Errorf("%s: entry %q is at %s, not after now %s", c.ID, e.Label, e.At, c.Now)
			}
		}
	}
}
