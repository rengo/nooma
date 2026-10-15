package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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
	want := llm.Expect{RemindRequest: bp(true), DayOnly: bp(true)}
	r := readAll(t,
		phrasing("d1", "recordame el jueves", want, reply(`,"remind_request":true,"due_at":"2026-10-15"`)),
		phrasing("d2", "recordame el viernes", want, reply(`,"remind_request":true,"due_at":"2026-10-16T00:00:00-03:00"`)),
	)
	if got := r.String(); !strings.Contains(got, "day only: 1 of 2 bare dates") {
		t.Errorf("report:\n%s", got)
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
