package ui_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
)

// fakeLearning is ui.Learning over a fixed view: Undo and Release record what
// they were given and answer err.
type fakeLearning struct {
	view  brain.LearningView
	err   error
	calls []string
}

func (f *fakeLearning) View(context.Context) (brain.LearningView, error) { return f.view, nil }

func (f *fakeLearning) Undo(_ context.Context, kind brain.LearnedKind, key string) error {
	f.calls = append(f.calls, "undo "+string(kind)+" "+key)
	return f.err
}

func (f *fakeLearning) Release(_ context.Context, kind brain.LearnedKind, key string) error {
	f.calls = append(f.calls, "release "+string(kind)+" "+key)
	return f.err
}

var learnedAt = time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)

// learnedView is a raised bar, undoable, and a goal cadence the user pinned.
func learnedView() brain.LearningView {
	return brain.LearningView{Values: []brain.LearnedValue{
		{Kind: brain.LearnedRelationBar, Key: "same_topic", Value: "0.35", At: &learnedAt, CanUndo: true,
			Sentence: "Nooma raised the same_topic link bar from 0.30 to 0.35 because you rejected 6 of the last 8 same_topic links it made."},
		{Kind: brain.LearnedGoalCadence, Key: brain.GoalCadenceKey, Value: "35 days", Pinned: true, At: &learnedAt, CanRelease: true,
			Sentence: "You set goal check-ins to wait 35 days in Admin."},
	}}
}

func learningDo(t *testing.T, l ui.Learning, method, path, pattern string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Pattern = pattern
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Learning: l}).ServeHTTP(rec, req)
	return rec
}

func learningGet(t *testing.T, l ui.Learning) *httptest.ResponseRecorder {
	t.Helper()
	return learningDo(t, l, http.MethodGet, "/ui/learning", "GET /ui/learning", nil)
}

func TestLearningPage_ShowsEachSentenceAndOnlyTheActsOffered(t *testing.T) {
	t.Parallel()
	rec := learningGet(t, &fakeLearning{view: learnedView()})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body,
		`<a href="/ui/learning" aria-current="page">Learning</a>`,
		`<h2>LEARNING</h2>`,
		"rejected 6 of the last 8 same_topic links", "0.35",
		"You set goal check-ins to wait 35 days in Admin.", "data-pinned",
		`action="/ui/learning/undo"`, `name="kind" value="relation_bar"`, `name="key" value="same_topic"`,
		`action="/ui/learning/release"`, `name="key" value="goal_stagnation_days"`, "Let Nooma learn again",
	)
	if n := strings.Count(body, `action="/ui/learning/undo"`); n != 1 {
		t.Errorf("%d undo forms, want only the bar's", n)
	}
	if n := strings.Count(body, `action="/ui/learning/release"`); n != 1 {
		t.Errorf("%d release forms, want only the pinned cadence's", n)
	}
	if n := strings.Count(body, "data-pinned"); n != 1 {
		t.Errorf("%d pinned markers, want 1", n)
	}
}

func TestLearningPage_NothingLearnedSaysSoInOneLine(t *testing.T) {
	t.Parallel()
	rec := learningGet(t, &fakeLearning{view: brain.LearningView{Values: []brain.LearnedValue{
		{Kind: brain.LearnedGoalCadence, Key: brain.GoalCadenceKey, Value: "21 days", Sentence: "Goal check-ins wait 21 days."},
	}}})
	body := rec.Body.String()
	assertContains(t, body, `<p data-empty>Nooma has not learned anything yet`, "Goal check-ins wait 21 days.")
	if strings.Contains(body, "/ui/learning/undo") || strings.Contains(body, "/ui/learning/release") {
		t.Error("a summary with nothing learned offers an act")
	}
	learnedOnly := brain.LearningView{Values: learnedView().Values[:1]}
	if strings.Contains(learningGet(t, &fakeLearning{view: learnedOnly}).Body.String(), "data-empty") {
		t.Error("a summary with a learned change says nothing was learned")
	}
}

func TestLearningPage_UndoAndReleasePostTheirValue(t *testing.T) {
	t.Parallel()
	for _, act := range []string{"undo", "release"} {
		f := &fakeLearning{view: learnedView()}
		rec := learningDo(t, f, http.MethodPost, "/ui/learning/"+act, "POST /ui/learning/"+act,
			url.Values{"kind": {"relation_bar"}, "key": {"same_topic"}})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200: %s", act, rec.Code, rec.Body)
		}
		if len(f.calls) != 1 || f.calls[0] != act+" relation_bar same_topic" {
			t.Errorf("%s: calls = %v", act, f.calls)
		}
		assertContains(t, rec.Body.String(), `data-outcome="saved"`, "<h2>LEARNING</h2>")
	}
}

func TestLearningPage_ARefusalIsANoticeAndAnUnknownValueIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err    error
		status int
		says   string
	}{
		{brain.ErrNothingToUndo, http.StatusConflict, "Nothing was changed"},
		{ports.ErrNotPinned, http.StatusConflict, "Nothing was changed"},
		{brain.ErrUnknownLearnedValue, http.StatusBadRequest, "Nothing was changed"},
		{&brain.WriteLandedError{Record: true, Err: errors.New("log down")}, http.StatusOK, "activity record"},
	} {
		rec := learningDo(t, &fakeLearning{view: learnedView(), err: tc.err}, http.MethodPost, "/ui/learning/undo", "POST /ui/learning/undo",
			url.Values{"kind": {"relation_bar"}, "key": {"same_topic"}})
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.says) {
			t.Errorf("%v: status %d, want %d with %q: %s", tc.err, rec.Code, tc.status, tc.says, rec.Body)
		}
	}
}

func TestLearningPage_NotWiredIs503(t *testing.T) {
	t.Parallel()
	if rec := learningGet(t, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET status = %d, want 503", rec.Code)
	}
	rec := learningDo(t, nil, http.MethodPost, "/ui/learning/undo", "POST /ui/learning/undo", url.Values{"kind": {"relation_bar"}, "key": {"x"}})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("POST status = %d, want 503", rec.Code)
	}
}
