package conformance

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/ui"
)

// TestLearningPage_LearnedUndoneThenReleased drives /ui/learning through the
// real mux over a real summary (doc 02 §9, I53): the learner's sentence and
// its undo, an undo that pins the bar, and a release that hands it back.
func TestLearningPage_LearnedUndoneThenReleased(t *testing.T) {
	v := newI48Vault(t)
	v.judgeSix(t)
	v.learn(t, i48Night)
	svc := brain.NewLearningService(fixedClock{now: i48Night.Add(time.Hour)}, v.ids, v.rels, v.cfg, v.log)
	h := httpapi.Handler(httpapi.Deps{Version: "test", Learning: svc, UI: ui.New(ui.Deps{Learning: svc})})
	do := func(method, path, body string) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d: %s", method, path, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	form := url.Values{"kind": {"relation_bar"}, "key": {"same_topic"}}.Encode()

	page := do(http.MethodGet, "/ui/learning", "")
	for _, want := range []string{"raised the same_topic link bar from 0.30 to 0.35", `action="/ui/learning/undo"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("the learned page lacks %q:\n%s", want, page)
		}
	}

	do(http.MethodPost, "/ui/learning/undo", form)
	page = do(http.MethodGet, "/ui/learning", "")
	if !strings.Contains(page, "data-pinned") || !strings.Contains(page, `action="/ui/learning/release"`) || strings.Contains(page, `action="/ui/learning/undo"`) {
		t.Fatalf("after the undo the bar is not shown pinned and releasable:\n%s", page)
	}
	if bar := v.bar(t, "same_topic"); bar.Persist != 0.30 {
		t.Errorf("after the undo the bar is %+v", bar)
	}
	if api := do(http.MethodGet, "/learning", ""); !strings.Contains(api, `"pinned":true`) {
		t.Errorf("GET /learning does not show the pin: %s", api)
	}

	do(http.MethodPost, "/ui/learning/release", form)
	page = do(http.MethodGet, "/ui/learning", "")
	if strings.Contains(page, "data-pinned") || strings.Contains(page, "/ui/learning/release") || !strings.Contains(page, "learn the same_topic link bar again") {
		t.Errorf("after the release:\n%s", page)
	}
}
