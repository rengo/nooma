package ui_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ui"
)

// fakeCapturer is capture_test.go's and unit_test.go's own Capturer stub:
// it records every call's input and answers a fixed, configurable
// brain.CaptureResult — CaptureService's own assembly is brain's own test's
// job (internal/brain/capture_test.go), not this package's.
type fakeCapturer struct {
	result brain.CaptureResult
	err    error
	calls  []brain.CaptureInput
}

func (f *fakeCapturer) Capture(_ context.Context, in brain.CaptureInput) (brain.CaptureResult, error) {
	f.calls = append(f.calls, in)
	return f.result, f.err
}

// captureRequest builds a POST /ui/capture request with req.Pattern set as
// net/http's own ServeMux would set it when this handler is reached through
// the real mux — unitsRequest's own precedent (units_test.go).
func captureRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/ui/capture", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Pattern = "POST /ui/capture"
	return req
}

// TestCaptureView_CallsCaptureOnceWithUIChannel is spec R4: the capture
// form's POST builds a brain.CaptureInput from the submitted text and calls
// Capturer.Capture exactly once, with Channel "ui" — the UI's own fact,
// never "api" (httpapi/capture.go's own default for POST /capture) and
// never empty — and no ReferentID on this route.
func TestCaptureView_CallsCaptureOnceWithUIChannel(t *testing.T) {
	t.Parallel()

	fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "unit-1"}}
	h := ui.New(ui.Deps{Capture: fake})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, captureRequest("text=Pick+up+the+dry+cleaning"))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ui/capture = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("Capture was called %d time(s), want exactly 1", len(fake.calls))
	}
	got := fake.calls[0]
	if got.Text != "Pick up the dry cleaning" {
		t.Errorf("Text = %q, want %q", got.Text, "Pick up the dry cleaning")
	}
	if got.Channel != "ui" {
		t.Errorf("Channel = %q, want \"ui\"", got.Channel)
	}
	if got.ReferentID != "" {
		t.Errorf("ReferentID = %q, want empty on the plain capture route", got.ReferentID)
	}
}

// TestCaptureView_IgnoresSubmittedUnitID is spec R4's own MUST: a submitted
// unit_id never reaches CaptureInput.ReferentID on this route — that field
// is set only by the correction route, from the path (spec R5, design
// §3.7's gate c).
func TestCaptureView_IgnoresSubmittedUnitID(t *testing.T) {
	t.Parallel()

	fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "unit-1"}}
	h := ui.New(ui.Deps{Capture: fake})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, captureRequest("text=hello&unit_id=some-other-unit"))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ui/capture = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("Capture was called %d time(s), want exactly 1", len(fake.calls))
	}
	if got := fake.calls[0].ReferentID; got != "" {
		t.Errorf("ReferentID = %q, want empty — a submitted unit_id must be ignored", got)
	}
}

// TestCaptureView_RendersEveryOutcome is R4's own total-switch requirement,
// made behavioural: every brain.CaptureOutcome renders its own
// distinguishable marker, and no two outcomes render identical bodies for
// distinct fixtures — the same "never invents a field CaptureResult
// doesn't carry" property httpapi's own renderCaptureResult holds, proven
// here for the UI's own rendering instead.
func TestCaptureView_RendersEveryOutcome(t *testing.T) {
	t.Parallel()

	fireAt := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		result brain.CaptureResult
	}{
		{"stored", brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "unit-1"}},
		{"armed", brain.CaptureResult{Outcome: brain.OutcomeArmed, Armed: &brain.Armed{What: prospection.ArmTimer, ID: "timer-1", FireAt: fireAt}}},
		{"arm_refused", brain.CaptureResult{Outcome: brain.OutcomeArmRefused, ArmRefused: &brain.ArmRefused{Why: prospection.RefusalNoDate, Message: "no date to arm against"}}},
		{"conversed", brain.CaptureResult{Outcome: brain.OutcomeConversed, Reply: "Sure thing."}},
		{"out_of_scope", brain.CaptureResult{Outcome: brain.OutcomeOutOfScope}},
		{"recalled", brain.CaptureResult{Outcome: brain.OutcomeRecalled, Recalled: []unit.Unit{{ID: "unit-2", Content: "Renew the passport"}}}},
		{"corrected", brain.CaptureResult{Outcome: brain.OutcomeCorrected, Correction: &brain.Correction{UnitID: "unit-3", Fields: []correction.Field{correction.FieldContent}}}},
		{"asked", brain.CaptureResult{Outcome: brain.OutcomeAsked, Correction: &brain.Correction{UnitID: "unit-4", Ambiguous: true}}},
	}

	if len(cases) != len(brain.AllCaptureOutcomes()) {
		t.Fatalf("this test covers %d outcomes, brain.AllCaptureOutcomes() names %d — every outcome must have its own case", len(cases), len(brain.AllCaptureOutcomes()))
	}

	bodies := make(map[string]string, len(cases))
	for _, tc := range cases {
		fake := &fakeCapturer{result: tc.result}
		h := ui.New(ui.Deps{Capture: fake})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, captureRequest("text=hello"))

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: POST /ui/capture = %d, want 200: %s", tc.name, rec.Code, rec.Body.String())
		}
		marker := `data-outcome="` + tc.name + `"`
		if !strings.Contains(rec.Body.String(), marker) {
			t.Errorf("%s: response does not carry %q:\n%s", tc.name, marker, rec.Body.String())
		}
		bodies[tc.name] = rec.Body.String()
	}

	for name1, body1 := range bodies {
		for name2, body2 := range bodies {
			if name1 != name2 && body1 == body2 {
				t.Errorf("%s and %s render identical bodies — not distinguishable", name1, name2)
			}
		}
	}
}

// TestCaptureView_AskSaysNothingChanged is fix-unit-correction-form R2: an
// ask tells the user in plain words that nothing was changed and what to do
// instead, and the two asks say different things — an edit plan that was
// ambiguous names what to write, a referent that was ambiguous points at
// the unit page's own form.
func TestCaptureView_AskSaysNothingChanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		corr brain.Correction
		want string
	}{
		{"plan ambiguous", brain.Correction{UnitID: "unit-4", Ambiguous: true}, "Write the one new value"},
		{"referent ambiguous", brain.Correction{Ambiguous: true}, "use its correction form"},
	}
	for _, tc := range cases {
		fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeAsked, Correction: &tc.corr}}
		rec := httptest.NewRecorder()
		ui.New(ui.Deps{Capture: fake}).ServeHTTP(rec, captureRequest("text=hello"))
		body := rec.Body.String()
		if !strings.Contains(body, "Nothing was changed") || !strings.Contains(body, tc.want) {
			t.Errorf("%s: body does not say %q and %q:\n%s", tc.name, "Nothing was changed", tc.want, body)
		}
	}
}

// TestCaptureView_EmptyTextIs400 is parseCaptureForm's own bad-body case:
// an empty (or absent) text field is a 400, and Capture is never reached —
// the same "no call on a bad body" posture TestCaptureView_BodyIsBounded
// pins for an oversized one.
func TestCaptureView_EmptyTextIs400(t *testing.T) {
	t.Parallel()

	fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "unit-1"}}
	h := ui.New(ui.Deps{Capture: fake})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, captureRequest("text="))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /ui/capture with empty text = %d, want 400", rec.Code)
	}
	if len(fake.calls) != 0 {
		t.Errorf("Capture was called %d time(s) for an empty text field, want 0", len(fake.calls))
	}
}

// TestCaptureView_BodyIsBounded is design §3.5: a submission larger than
// captureFormMaxBytes is refused via http.MaxBytesReader, never buffered in
// full — loginSubmit's own precedent (internal/httpapi/cookie.go), sized
// for a capture's longer text rather than a login form's short token.
func TestCaptureView_BodyIsBounded(t *testing.T) {
	t.Parallel()

	fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "unit-1"}}
	h := ui.New(ui.Deps{Capture: fake})

	oversized := "text=" + strings.Repeat("a", 70*1024)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, captureRequest(oversized))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /ui/capture with an oversized submission = %d, want 400", rec.Code)
	}
	if len(fake.calls) != 0 {
		t.Errorf("Capture was called %d time(s) for an oversized submission, want 0", len(fake.calls))
	}
}

// TestCaptureView_EscapesReply is TestUnitsView_EscapesVaultContent's own
// sibling for the capture result (design §9's threat-matrix row on content
// injection): OutcomeConversed's Reply is model-generated text, escaped by
// templ's default { expr } handling — never templ.Raw — the same property
// unit.templ and units.templ already pin for vault content.
func TestCaptureView_EscapesReply(t *testing.T) {
	t.Parallel()

	const payload = `<script>alert(1)</script>`
	const escaped = `&lt;script&gt;alert(1)&lt;/script&gt;`

	fake := &fakeCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeConversed, Reply: payload}}
	h := ui.New(ui.Deps{Capture: fake})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, captureRequest("text=hello"))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ui/capture = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, payload) {
		t.Errorf("the model's reply reached the response unescaped:\n%s", body)
	}
	if !strings.Contains(body, escaped) {
		t.Errorf("the model's reply is not escaped as expected:\n%s", body)
	}
}

// TestCaptureView_CaptureErrorIs500 is serveCapture's own error branch: a
// Capturer.Capture error answers 500 and never reflects the raw error into
// the response body.
func TestCaptureView_CaptureErrorIs500(t *testing.T) {
	t.Parallel()

	fake := &fakeCapturer{err: errors.New("boom")}
	h := ui.New(ui.Deps{Capture: fake})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, captureRequest("text=hello"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("POST /ui/capture (Capture error) = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("response reflects the raw error:\n%s", rec.Body.String())
	}
}

// TestCaptureView_NilCapturerIs503 is captureHandler's own nil-dependency
// posture (internal/httpapi/capture.go), applied to the UI route: a nil
// Deps.Capture answers 503, never a panic on a nil interface call —
// TestUnitsView_NilUnitsIs503's own precedent.
func TestCaptureView_NilCapturerIs503(t *testing.T) {
	t.Parallel()

	// Without the guard, h.deps.Capture.Capture panics on a nil interface;
	// recovering reports that as this test's failure instead of crashing
	// the whole test binary (TestCorrectView_NilCapturerIs503's shape).
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("POST /ui/capture with no Capturer panicked instead of answering 503: %v", r)
		}
	}()

	h := ui.New(ui.Deps{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, captureRequest("text=hello"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("POST /ui/capture with no Capturer = %d, want 503", rec.Code)
	}
}
