package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/reading"
	"github.com/rengo/nooma/testdata/llm"
)

// scriptedLLM answers every prompt with reply (or fails once calls reaches
// failAt) and remembers what it was sent. No test here reaches a model.
type scriptedLLM struct {
	reply   string
	failAt  int
	prompts []string
	reqs    []ports.LLMRequest
}

func (s *scriptedLLM) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	s.prompts = append(s.prompts, req.Prompt)
	s.reqs = append(s.reqs, req)
	if s.failAt > 0 && len(s.prompts) >= s.failAt {
		return ports.LLMResponse{}, errors.New("provider down")
	}
	return ports.LLMResponse{Text: s.reply, Model: "fake-model"}, nil
}

const (
	recordReply = `{"type":"task","normalized_content":"Comprar café","weight":0.6,"decay_rate":0.1,"remind_request":true,"title":"Comprar café"}`
	awaiting    = `{
  "id": "%ID%",
  "task": "classify",
  "message": "%MSG%",
  "record": true,
  "candidates_list": [
    {
      "label": "Dentista",
      "when": "jue 15 oct"
    }
  ],
  "expect": {
    "remind_request": true,
    "title": true
  }
}
`
)

var recordNow = time.Date(2026, 10, 15, 9, 30, 0, 0, time.FixedZone("", -3*3600))

func writePhrasing(t *testing.T, dir, id, msg string) string {
	t.Helper()
	body := strings.NewReplacer("%ID%", id, "%MSG%", msg).Replace(awaiting)
	path := filepath.Join(dir, id+".json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func recordOpts(dir string, p ports.LLMProvider) recordOptions {
	return recordOptions{dir: dir, provider: p, providerName: "fake", model: "configured-model", now: recordNow}
}

func readFields(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s is not JSON: %v", path, err)
	}
	return m
}

func TestDoctorRecordWritesResponsesWithAFakeProvider(t *testing.T) {
	dir := t.TempDir()
	a := writePhrasing(t, dir, "a", "recordame comprar café mañana a las 10")
	before := readFields(t, a)

	// A recorded case is left alone: it is not asking to be recorded.
	done := filepath.Join(dir, "done.json")
	doneBody := `{"id":"done","provider":"p","model":"m","task":"classify","message":"hola","response":"{}"}`
	if err := os.WriteFile(done, []byte(doneBody), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &scriptedLLM{reply: recordReply}
	var out bytes.Buffer
	if err := recordPhrasings(context.Background(), recordOpts(dir, fake), &out); err != nil {
		t.Fatalf("recordPhrasings = %v", err)
	}

	if len(fake.prompts) != 1 {
		t.Fatalf("the provider was called %d times, want 1 (only the record:true case)", len(fake.prompts))
	}
	if r := fake.reqs[0]; r.Task != "capture_processing" || !r.JSONOnly {
		t.Fatalf("request = %+v, want the production capture_processing JSON request", r)
	}
	got := readFields(t, a)
	str := func(k string) string {
		var s string
		if err := json.Unmarshal(got[k], &s); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		return s
	}
	if str("response") != recordReply || str("provider") != "fake" || str("model") != "fake-model" {
		t.Errorf("response, provider or model not written: %s", got)
	}
	if str("now") != "2026-10-15T09:30:00-03:00" {
		t.Errorf("now = %q, want the instant the model was told", str("now"))
	}
	if want := reading.Hash(fake.prompts[0]); str("prompt_sha256") != want {
		t.Errorf("prompt_sha256 = %q, want the hash of the prompt sent, %q", str("prompt_sha256"), want)
	}
	if _, still := got["record"]; still {
		t.Error("record was not cleared")
	}
	if string(got["expect"]) != string(before["expect"]) || string(got["candidates_list"]) != string(before["candidates_list"]) {
		t.Errorf("expect or the context changed:\nbefore %s\nafter  %s", before["expect"], got["expect"])
	}
	if data, _ := os.ReadFile(done); string(data) != doneBody {
		t.Errorf("a case with record unset was rewritten: %s", data)
	}
	if !strings.Contains(out.String(), "recorded 1") {
		t.Errorf("report %q does not say what was recorded", out.String())
	}

	// What it wrote replays: the same file is now a recording reading.Replay accepts.
	data, _ := os.ReadFile(a)
	var c llm.Case
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if err := reading.Replay(c, recordReply); err != nil {
		t.Errorf("the recorded file does not replay: %v", err)
	}
}

func TestRecordSkipsRecordedWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "again.json")
	body := `{"id":"again","provider":"p","model":"m","task":"classify","message":"hola","record":true,"response":"OLD","expect":{}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &scriptedLLM{reply: recordReply}
	var out bytes.Buffer
	if err := recordPhrasings(context.Background(), recordOpts(dir, fake), &out); err != nil {
		t.Fatal(err)
	}
	if len(fake.prompts) != 0 {
		t.Fatalf("a case that already has a response was re-recorded without --force")
	}
	if data, _ := os.ReadFile(path); string(data) != body {
		t.Fatalf("the file changed without --force: %s", data)
	}
	if !strings.Contains(out.String(), "skipped 1") {
		t.Errorf("report %q does not say a case was skipped", out.String())
	}

	// --force re-records only what asks for it: a case with record unset
	// stays as it is even then.
	done := filepath.Join(dir, "done.json")
	doneBody := `{"id":"done","provider":"p","model":"m","task":"classify","message":"hola","response":"{}"}`
	if err := os.WriteFile(done, []byte(doneBody), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := recordOpts(dir, fake)
	opts.force = true
	if err := recordPhrasings(context.Background(), opts, &out); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(done); string(data) != doneBody {
		t.Fatalf("--force rewrote a case that did not ask to be recorded: %s", data)
	}
	var resp string
	_ = json.Unmarshal(readFields(t, path)["response"], &resp)
	if len(fake.prompts) != 1 || resp != recordReply {
		t.Fatalf("--force did not re-record: calls %d, response %q", len(fake.prompts), resp)
	}
}

func TestRecordRefusesUnderCI(t *testing.T) {
	dir := t.TempDir()
	path := writePhrasing(t, dir, "a", "hola")
	before, _ := os.ReadFile(path)

	t.Setenv("CI", "true")
	err := runDoctor([]string{"--record", dir, t.TempDir()}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "refuses to run under CI") {
		t.Fatalf("runDoctor --record under CI = %v, want a refusal that names CI", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("a refused recording changed a file")
	}
}

func TestRecordSendsNilBeliefs(t *testing.T) {
	dir := t.TempDir()
	writePhrasing(t, dir, "a", "recordame comprar café mañana a las 10")
	fake := &scriptedLLM{reply: recordReply}
	if err := recordPhrasings(context.Background(), recordOpts(dir, fake), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// Built here, from the pieces, not through internal/reading: the prompt a
	// recording is made against has no beliefs section and the case's list.
	want := classify.BuildPrompt("recordame comprar café mañana a las 10", nil, recordNow,
		consolidation.DefaultWeightThreshold,
		classify.ReminderContext{Candidates: []classify.ReminderEntry{{Label: "Dentista", When: "jue 15 oct"}}})
	if fake.prompts[0] != want {
		t.Fatalf("the recorded prompt is not the production prompt with beliefs nil:\n%s", fake.prompts[0])
	}
}

func TestRecordKeepsTheNowACaseAlreadyHas(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	body := `{"id":"a","task":"classify","message":"hola","record":true,"now":"2026-01-02T03:04:05Z","expect":{}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &scriptedLLM{reply: recordReply}
	if err := recordPhrasings(context.Background(), recordOpts(dir, fake), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var now string
	_ = json.Unmarshal(readFields(t, path)["now"], &now)
	if now != "2026-01-02T03:04:05Z" || !strings.Contains(fake.prompts[0], "2026-01-02") {
		t.Fatalf("now = %q, prompt told %q: an authored now must be the one the model was told", now, fake.prompts[0][:200])
	}
}

func TestRecordStopsOnAProviderErrorKeepingWhatWasRecorded(t *testing.T) {
	dir := t.TempDir()
	first := writePhrasing(t, dir, "a", "uno")
	second := writePhrasing(t, dir, "b", "dos")
	secondBefore, _ := os.ReadFile(second)

	fake := &scriptedLLM{reply: recordReply, failAt: 2}
	err := recordPhrasings(context.Background(), recordOpts(dir, fake), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "b") {
		t.Fatalf("recordPhrasings = %v, want an error naming the case that failed", err)
	}
	if _, recorded := readFields(t, first)["response"]; !recorded {
		t.Error("the case recorded before the failure was lost")
	}
	if after, _ := os.ReadFile(second); !bytes.Equal(after, secondBefore) {
		t.Error("the case that failed was changed")
	}
}

func TestRecordRejectsAFieldItDoesNotKnow(t *testing.T) {
	dir := t.TempDir()
	body := `{"id":"a","task":"classify","message":"hola","record":true,"surprise":1}`
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &scriptedLLM{reply: recordReply}
	err := recordPhrasings(context.Background(), recordOpts(dir, fake), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "surprise") || len(fake.prompts) != 0 {
		t.Fatalf("recordPhrasings = %v after %d calls, want a refusal naming the field before any call", err, len(fake.prompts))
	}
}

// TestRecordWritesTheFormatPhrasingsAreAuthoredIn: editing a file by key and
// writing it back with nothing set returns the same bytes, so a recording's
// diff is only what the recording added.
func TestRecordWritesTheFormatPhrasingsAreAuthoredIn(t *testing.T) {
	path := filepath.Join(testdataLLMDir(t, "reminders"), "classify-task-remind-me-coffee.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := splitFields(data)
	if err != nil {
		t.Fatal(err)
	}
	back, err := joinFields(fields)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, data) {
		t.Fatalf("round trip changed the file:\n%s\nwas:\n%s", back, data)
	}
}

func TestRecordValidatesACaseBeforeSpendingACall(t *testing.T) {
	dir := t.TempDir()
	writePhrasing(t, dir, "a", "uno")
	// A typo in the expectation: found now, not when the answer comes back.
	bad := `{"id":"b","task":"classify","message":"dos","record":true,"expect":{"reminder_intent":"stopp"}}`
	if err := os.WriteFile(filepath.Join(dir, "b.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &scriptedLLM{reply: recordReply}
	err := recordPhrasings(context.Background(), recordOpts(dir, fake), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "stopp") {
		t.Fatalf("recordPhrasings = %v, want the invalid expectation named", err)
	}
	if len(fake.prompts) != 1 {
		t.Fatalf("%d calls made, want exactly the one for the valid case before it", len(fake.prompts))
	}
	if _, recorded := readFields(t, filepath.Join(dir, "b.json"))["response"]; recorded {
		t.Fatal("an invalid case was recorded")
	}
}
