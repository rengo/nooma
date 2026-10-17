package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/reading"
	"github.com/rengo/nooma/testdata/llm"
)

const readingNow = "2026-10-15T09:30:00-03:00"

func bp(b bool) *bool { return &b }

// readCase is a phrasing with its context; answers is what the scripted model
// says to it. No test here reaches a model.
type readCase struct {
	c      llm.Case
	answer string
}

func phrasing(id, msg string, e llm.Expect, answer string, list ...llm.Entry) readCase {
	return readCase{
		c: llm.Case{
			ID: id, Task: "classify", Message: msg, Now: readingNow,
			CandidatesList: list, Expect: &e,
		},
		answer: answer,
	}
}

func reply(extra string) string {
	return `{"type":"task","normalized_content":"x","weight":0.6,"decay_rate":0.1` + extra + `}`
}

var cafe = llm.Entry{Label: "Comprar café", When: "jue 15 oct 10:00"}

// run reads the cases through a model that answers each prompt as scripted.
func readAll(t *testing.T, rcs ...readCase) readingReport {
	t.Helper()
	byPrompt := map[string]string{}
	var cases []llm.Case
	for _, rc := range rcs {
		p, err := reading.Prompt(rc.c)
		if err != nil {
			t.Fatal(err)
		}
		byPrompt[p] = rc.answer
		cases = append(cases, rc.c)
	}
	return runReading(context.Background(), scriptedByPrompt(t, byPrompt), cases, time.Now(), time.Second)
}

type scriptedByPromptLLM struct {
	t       *testing.T
	answers map[string]string
}

func scriptedByPrompt(t *testing.T, answers map[string]string) ports.LLMProvider {
	return &scriptedByPromptLLM{t: t, answers: answers}
}

func (s *scriptedByPromptLLM) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	answer, ok := s.answers[req.Prompt]
	if !ok {
		s.t.Fatalf("a prompt nobody scripted was sent:\n%s", req.Prompt)
	}
	return ports.LLMResponse{Text: answer, Model: "fake"}, nil
}

func TestDoctorReportsReminderReadingRate(t *testing.T) {
	r := readAll(t,
		phrasing("a", "recordame X", llm.Expect{RemindRequest: bp(true)}, reply(`,"remind_request":true`)),
		phrasing("b", "recordame Y", llm.Expect{RemindRequest: bp(true)}, reply(`,"remind_request":true`)),
		phrasing("c", "comprar Z", llm.Expect{}, reply(``)),
		phrasing("d", "recordame qué hablamos", llm.Expect{}, reply(`,"remind_request":true`)), // misread
	)
	got := r.String()
	if !strings.Contains(got, "reminder reading: 3 of 4 (75%)") {
		t.Errorf("report does not state the rate:\n%s", got)
	}
	if !strings.Contains(got, "below 90%") || !strings.Contains(got, "d (remind_request)") {
		t.Errorf("report does not name the threshold or the missed case:\n%s", got)
	}
	if r.failed() {
		t.Error("a reading rate below the threshold failed doctor: only the hard line and an unreachable provider do")
	}

	clean := readAll(t, phrasing("a", "comprar Z", llm.Expect{}, reply(``))).String()
	if !strings.Contains(clean, "reminder reading: 1 of 1 (100%)") || strings.Contains(clean, "below") {
		t.Errorf("a perfect reading says:\n%s", clean)
	}
}

func TestDoctorFalsePositiveLineFailsOnOne(t *testing.T) {
	stop := reply(`,"reminder_intent":"stop","reminder_target":1`)
	none := reply(``)
	cases := func(listoAnswer string) []readCase {
		return []readCase{
			phrasing("no", "no", llm.Expect{}, none, cafe),
			phrasing("si", "sí", llm.Expect{}, none, cafe),
			phrasing("listo", "listo", llm.Expect{}, listoAnswer, cafe),
			phrasing("ya", "ya", llm.Expect{}, none, cafe),
			// A real stop read as a stop is the point, not a false positive.
			phrasing("real-stop", "no me lo recuerdes más", llm.Expect{ReminderIntent: "stop", ReminderTarget: "1"}, stop, cafe),
		}
	}

	r := readAll(t, cases(stop)...)
	got := r.String()
	if !strings.Contains(got, "stop false positives: 1 of 4 negatives (listo)") {
		t.Errorf("the hard line is wrong:\n%s", got)
	}
	if !r.failed() || readingError(r) == nil {
		t.Error("one stop false positive did not fail doctor")
	}

	ok := readAll(t, cases(none)...)
	if !strings.Contains(ok.String(), "stop false positives: 0 of 4 negatives") || ok.failed() || readingError(ok) != nil {
		t.Errorf("no false positive should pass:\n%s", ok)
	}
}

func TestDoctorReportsTitleRate(t *testing.T) {
	want := llm.Expect{RemindRequest: bp(true), Title: bp(true)}
	r := readAll(t,
		phrasing("t1", "recordame la reunión", want, reply(`,"remind_request":true,"title":"Reunión"`)),
		phrasing("t2", "recordame llamar", want, reply(`,"remind_request":true,"title":"Llamar a Ana"`)),
		// A title carrying a clock time is dropped by the decoder: no title.
		phrasing("t3", "recordame la cena", want, reply(`,"remind_request":true,"title":"Cena a las 22:30"`)),
	)
	if got := r.String(); !strings.Contains(got, "titles: 2 of 3 date-free and short") {
		t.Errorf("report:\n%s", got)
	}
}

func TestDoctorReportsDayOnlyRate(t *testing.T) {
	bare := llm.Expect{RemindRequest: bp(true), DayOnly: bp(true)}
	timed := llm.Expect{RemindRequest: bp(true), DayOnly: bp(false)}
	r := readAll(t,
		phrasing("d1", "recordame el jueves", bare, reply(`,"remind_request":true,"due_at":"2026-10-15"`)),
		phrasing("d2", "recordame el viernes", bare, reply(`,"remind_request":true,"due_at":"2026-10-16T00:00:00-03:00"`)),
		phrasing("t1", "recordame a las 10", timed, reply(`,"remind_request":true,"due_at":"2026-10-15T10:00:00-03:00"`)),
		phrasing("t2", "recordame a las 11", timed, reply(`,"remind_request":true,"due_at":"2026-10-15"`)),
	)
	got := r.String()
	// Two lines, each over the cases that expect it: a bare date is not a
	// named time, and a rate over both would say neither.
	for _, line := range []string{"day only: 1 of 2 bare dates read as bare dates", "named times: 1 of 2 kept their time"} {
		if !strings.Contains(got, line) {
			t.Errorf("report lacks %q:\n%s", line, got)
		}
	}
}

func TestDoctorReadingRateCountsOnlyTheReminderFields(t *testing.T) {
	// The intent is read right and the title is wrong (it carries a time, so
	// the decoder drops it): the phrasing counts as read, and the title line
	// counts the miss. ADR-0033's 90% is about the intent fields.
	r := readAll(t, phrasing("a", "recordame la cena", llm.Expect{RemindRequest: bp(true), Title: bp(true)},
		reply(`,"remind_request":true,"title":"Cena a las 22:30"`)))
	got := r.String()
	if !strings.Contains(got, "reminder reading: 1 of 1 (100%)") || !strings.Contains(got, "titles: 0 of 1") {
		t.Errorf("a bad title changed the reading rate, or the title line lost the miss:\n%s", got)
	}
	if strings.Contains(got, "missed:") {
		t.Errorf("a title miss is listed as a reading miss:\n%s", got)
	}
	// And the other way: a wrong intent with a good title is a reading miss.
	r = readAll(t, phrasing("b", "comprar café", llm.Expect{Title: bp(true)}, reply(`,"remind_request":true,"title":"Café"`)))
	if got := r.String(); !strings.Contains(got, "reminder reading: 0 of 1") || !strings.Contains(got, "titles: 1 of 1") {
		t.Errorf("report:\n%s", got)
	}
}

func TestDoctorReadingWithNoProviderBoundFails(t *testing.T) {
	err := checkReading(t.TempDir(), &config.Config{})
	if err == nil || !strings.Contains(err.Error(), "capture_processing") {
		t.Fatalf("checkReading with no provider = %v, want a failure naming capture_processing: a run that measured nothing must not pass", err)
	}
	// doctorOKDetail travels as an error but is printed as a pass.
	if _, passes := err.(doctorOKDetail); passes {
		t.Fatalf("checkReading with no provider = %T, which doctor prints as ok", err)
	}
}

func TestDoctorReportsCheckinOutcomeRates(t *testing.T) {
	r := readAll(t,
		phrasing("c1", "ya lo hice", llm.Expect{TaskCheckinOutcome: "done"}, reply(`,"task_checkin_outcome":"done"`)),
		phrasing("c2", "después lo veo", llm.Expect{TaskCheckinOutcome: "snooze"}, reply(`,"task_checkin_outcome":"drop"`)),
		phrasing("n1", "dale, contame", llm.Expect{NudgeOutcome: "engaged"}, reply(`,"nudge_outcome":"engaged"`)),
		phrasing("r1", "yes, they're related", llm.Expect{RelationOutcome: "confirmed"}, reply(`,"relation_outcome":"confirmed"`)),
		phrasing("r2", "no, nada que ver", llm.Expect{RelationOutcome: "rejected"}, reply(`,"relation_outcome":"rejected"`)),
	)
	got := r.String()
	for _, line := range []string{
		"task_checkin_outcome: 1 of 2", "nudge_outcome: 1 of 1", "relation_outcome: 2 of 2",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("report lacks %q:\n%s", line, got)
		}
	}
}

func TestDoctorReadingStopsWhenTheProviderIsUnreachable(t *testing.T) {
	c := phrasing("a", "x", llm.Expect{}, "").c
	r := runReading(context.Background(), failingLLM{}, []llm.Case{c}, time.Now(), time.Second)
	if !r.failed() || !strings.Contains(r.String(), "unreachable") || strings.Contains(r.String(), "reminder reading:") {
		t.Errorf("an unreachable provider must fail alone, with no rate:\n%s", r)
	}
}

type failingLLM struct{}

func (failingLLM) Complete(context.Context, ports.LLMRequest) (ports.LLMResponse, error) {
	return ports.LLMResponse{}, fmt.Errorf("connection refused")
}

// TestDoctorReadingIsOptIn: a plain doctor run makes no reading calls. The
// reading is its own check, added by --reading only, and the quality gate's
// corpus does not hold the phrasing set.
func TestDoctorReadingIsOptIn(t *testing.T) {
	for _, check := range doctorChecksFor(false) {
		if check.name == "reading" {
			t.Fatal("plain doctor has the reading check")
		}
	}
	withReading := doctorChecksFor(true)
	if last := withReading[len(withReading)-1]; last.name != "reading" || len(withReading) != len(doctorChecksFor(false))+1 {
		t.Fatalf("--reading must add exactly one check, last; got %v", withReading)
	}

	gate, err := llm.Cases()
	if err != nil {
		t.Fatal(err)
	}
	phrasings, err := llm.ReminderCases()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range gate {
		seen[c.ID] = true
	}
	for _, c := range phrasings {
		if seen[c.ID] {
			t.Errorf("phrasing %s is also in the quality gate's corpus, so plain doctor would send it", c.ID)
		}
	}
}

// TestDoctorReadingRunsOnlyWhenAsked goes through runDoctor itself: a plain
// run lists no reading check, so it can make no reading call, and --reading
// lists exactly that one more.
func TestDoctorReadingRunsOnlyWhenAsked(t *testing.T) {
	doctorLines := func(args ...string) string {
		var out bytes.Buffer
		_ = runDoctor(append(args, writeVault(t, "")), &out, &bytes.Buffer{})
		return out.String()
	}
	if plain := doctorLines(); strings.Contains(plain, "reading") {
		t.Errorf("a plain doctor run reached the reading check:\n%s", plain)
	}
	if asked := doctorLines("--reading"); !strings.Contains(asked, "reading ") {
		t.Errorf("doctor --reading did not run the reading check:\n%s", asked)
	}
}

func TestDoctorReadingFlagWorksWhereverItIsWritten(t *testing.T) {
	got, err := parseDoctorArgs([]string{"vault", "--reading"}, &bytes.Buffer{})
	if err != nil || !got.reading || got.vault != "vault" {
		t.Fatalf("parsed %+v, %v: want reading on, vault vault", got, err)
	}
	if _, err := parseDoctorArgs([]string{"--reading", "--record", "ph"}, &bytes.Buffer{}); err == nil {
		t.Error("--record with --reading was accepted: they are separate runs")
	}
}

// A "no, seguí" that undoes a stop expects keep. Read as a stop it would stop
// the reminder again, so it is a negative of the hard line like any message
// that does not expect a stop; counting only the empty expectations missed it.
func TestDoctorFalsePositiveLineCountsAKeepReadAsAStop(t *testing.T) {
	stop := reply(`,"reminder_intent":"stop","reminder_target":1`)
	keep := reply(`,"reminder_intent":"keep"`)
	undo := func(answer string) readCase {
		rc := phrasing("keep", "no, seguí", llm.Expect{ReminderIntent: "keep"}, answer)
		rc.c.OpenQuestion = &llm.Question{Kind: "undo", Text: "Listo, no te lo recuerdo más. (Si fue un error, decime «no, seguí».)"}
		return rc
	}

	r := readAll(t, undo(stop))
	if r.negatives != 1 || len(r.falsePositives) != 1 || !r.failed() {
		t.Fatalf("a keep read as a stop: negatives %d, false positives %v, failed %v; want 1, [keep], true", r.negatives, r.falsePositives, r.failed())
	}
	if ok := readAll(t, undo(keep)); len(ok.falsePositives) != 0 || ok.failed() {
		t.Fatalf("a keep read as a keep failed doctor: %v", ok.falsePositives)
	}
}

// TestDoctorReportsFollowUpReading: the follow-ups (ADR-0036) have their own
// line, judged on every follow-up field, and a referent read on a phrasing
// that changes nothing is named: it would correct a unit nobody changed.
func TestDoctorReportsFollowUpReading(t *testing.T) {
	conv := &llm.Conversation{
		Turns: []llm.ConversationTurn{{Speaker: "user", Text: "reunión con Juan a las 22:30", About: []string{"A"}}},
		Items: []llm.ConversationEntry{{Content: "Reunión con Juan el 2026-10-15 a las 22:30.", EventAt: "2026-10-15T22:30:00-03:00"}},
	}
	followUp := func(id, msg string, e llm.Expect, answer string) readCase {
		rc := phrasing(id, msg, e, answer)
		rc.c.Conversation = conv
		return rc
	}
	correctA := `{"type":"correction","normalized_content":"x","weight":0.6,"decay_rate":0.1,"referent":"A","event_at":"2026-10-15T08:00:00-03:00"}`
	moved := llm.Expect{Type: "correction", Referent: "A", EventAt: "2026-10-15T08:00:00-03:00"}
	r := readAll(t,
		followUp("moves", "Es a las 08:00.", moved, correctA),
		followUp("moves-wrong", "es a las 8", moved, reply(``)),
		followUp("new", "Comprar pan", llm.Expect{Type: "task"}, reply(`,"referent":"A"`)),
		followUp("new-ok", "Comprar leche", llm.Expect{Type: "task"}, reply(``)),
	)
	got := r.String()
	for _, want := range []string{
		"follow-ups: 2 of 4 read as expected",
		"items corrected that nobody changed: 1 of 2 (new)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
}
