package ui_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
)

// beliefEditCall is one recorded Edit: the id and the content exactly as the
// handler passed them.
type beliefEditCall struct {
	id      string
	content string
}

// fakeBeliefs is this file's Beliefs stub: it answers a fixed ByFacet result
// and a configurable error per operation, and records every Edit and Retire
// call. BeliefsService's own behaviour is internal/brain's tests' job.
type fakeBeliefs struct {
	groups     []brain.FacetBeliefs
	listErr    error
	editErr    error
	retireErr  error
	listCalls  int
	editCalls  []beliefEditCall
	retireCall []string
	// onEdit, when set, runs after a recorded Edit and may change groups, so the
	// next ByFacet read shows what the write did.
	onEdit func(f *fakeBeliefs, id, content string)
}

func (f *fakeBeliefs) ByFacet(context.Context) ([]brain.FacetBeliefs, error) {
	f.listCalls++
	return f.groups, f.listErr
}

func (f *fakeBeliefs) Edit(_ context.Context, id, content string) error {
	f.editCalls = append(f.editCalls, beliefEditCall{id: id, content: content})
	if f.onEdit != nil {
		f.onEdit(f, id, content)
	}
	return f.editErr
}

func (f *fakeBeliefs) Retire(_ context.Context, id string) error {
	f.retireCall = append(f.retireCall, id)
	return f.retireErr
}

// beliefFixture is the page the view tests render: three facets populated,
// two empty. The value facet is handed over in an order a UI-side sort by
// confidence or by id would flip (v-2 before v-1, 0.35 before 0.80), because
// the order is brain's and the UI must render it as given.
func beliefFixture() []brain.FacetBeliefs {
	at := func(s string) time.Time {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			panic(err)
		}
		return ts
	}
	return []brain.FacetBeliefs{
		{Facet: selfmodel.FacetIdentity, Beliefs: []ports.Belief{}},
		{Facet: selfmodel.FacetValue, Beliefs: []ports.Belief{
			{ID: "v-2", Facet: selfmodel.FacetValue, Content: "Honesty first", Confidence: 0.35, Origin: selfmodel.OriginUserStated, LastReinforcedAt: at("2026-08-01T10:30:00Z")},
			{ID: "v-1", Facet: selfmodel.FacetValue, Content: "Keep promises", Confidence: 0.80, Origin: selfmodel.OriginDerived, LastReinforcedAt: at("2026-09-02T08:15:00Z")},
		}},
		{Facet: selfmodel.FacetGoal, Beliefs: []ports.Belief{
			{ID: "g-1", Facet: selfmodel.FacetGoal, Content: "Run a <b>marathon</b>", Confidence: 0.60, Origin: selfmodel.OriginSeed, LastReinforcedAt: at("2026-07-15T12:00:00Z")},
		}},
		{Facet: selfmodel.FacetSocial, Beliefs: []ports.Belief{}},
		{Facet: selfmodel.FacetPreference, Beliefs: []ports.Belief{
			{ID: "p-1", Facet: selfmodel.FacetPreference, Content: "Dark roast", Confidence: 0.50, Origin: selfmodel.OriginDerived, LastReinforcedAt: at("2026-09-10T07:00:00Z")},
		}},
	}
}

const (
	beliefsGetPattern    = "GET /ui/beliefs"
	beliefEditPattern    = "POST /ui/beliefs/{id}/edit"
	beliefRetirePattern  = "POST /ui/beliefs/{id}/retire"
	beliefsResultMarker  = `id="beliefs-result"`
	beliefsFullPageTitle = "<h2>BELIEFS</h2>"
)

// beliefsGet builds GET /ui/beliefs with Pattern set as the real mux would.
func beliefsGet() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/ui/beliefs", nil)
	req.Pattern = beliefsGetPattern
	return req
}

// beliefsPost builds a POST to one of the two belief routes: the path value
// id as the mux would bind it, the form body as given, HX-Request when htmx.
func beliefsPost(pattern, id, body string, htmx bool) *http.Request {
	action := "edit"
	if pattern == beliefRetirePattern {
		action = "retire"
	}
	req := httptest.NewRequest(http.MethodPost, "/ui/beliefs/"+id+"/"+action, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Pattern = pattern
	req.SetPathValue("id", id)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return req
}

func serveBeliefs(fake *fakeBeliefs, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Beliefs: fake}).ServeHTTP(rec, req)
	return rec
}

// between returns the text from the first occurrence of start up to and
// including the next end after it, failing the test when either is missing:
// every assertion below is scoped to one element this way, never to a flat
// strings.Contains over the whole page.
func between(t *testing.T, body, start, end string) string {
	t.Helper()
	i := strings.Index(body, start)
	if i < 0 {
		t.Fatalf("body has no %q:\n%s", start, body)
	}
	j := strings.Index(body[i:], end)
	if j < 0 {
		t.Fatalf("body has no %q after %q:\n%s", end, start, body)
	}
	return body[i : i+j+len(end)]
}

func facetSection(t *testing.T, body string, facet selfmodel.Facet) string {
	t.Helper()
	return between(t, body, `<section data-facet="`+string(facet)+`"`, "</section>")
}

func beliefItem(t *testing.T, body, id string) string {
	t.Helper()
	return between(t, body, `<li id="belief-`+id+`"`, "</li>")
}

func outcomeOf(t *testing.T, body string) string {
	t.Helper()
	return between(t, body, "<p data-outcome=", "</p>")
}

func TestBeliefsView_RendersAllFiveFacetsInOrderWithEmptyOnesPresent(t *testing.T) {
	t.Parallel()

	rec := serveBeliefs(&fakeBeliefs{groups: beliefFixture()}, beliefsGet())
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/beliefs = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, beliefsFullPageTitle) {
		t.Errorf("page has no %q heading:\n%s", beliefsFullPageTitle, body)
	}

	last := -1
	for _, facet := range selfmodel.AllFacets() {
		sec := facetSection(t, body, facet)
		at := strings.Index(body, sec)
		if at <= last {
			t.Errorf("facet %s renders before the previous facet, want selfmodel.AllFacets() order", facet)
		}
		last = at
		if !strings.Contains(sec, "<h3>"+string(facet)+"</h3>") {
			t.Errorf("facet %s section has no heading:\n%s", facet, sec)
		}
	}

	for _, facet := range []selfmodel.Facet{selfmodel.FacetIdentity, selfmodel.FacetSocial} {
		sec := facetSection(t, body, facet)
		if strings.Contains(sec, "data-belief-id") {
			t.Errorf("empty facet %s lists a belief:\n%s", facet, sec)
		}
		if !strings.Contains(sec, "No beliefs yet.") {
			t.Errorf("empty facet %s does not say it is empty:\n%s", facet, sec)
		}
	}
	for _, facet := range []selfmodel.Facet{selfmodel.FacetValue, selfmodel.FacetGoal, selfmodel.FacetPreference} {
		sec := facetSection(t, body, facet)
		if strings.Contains(sec, "No beliefs yet.") {
			t.Errorf("populated facet %s says it is empty:\n%s", facet, sec)
		}
	}
}

func TestBeliefsView_KeepsBrainsOrderWithinAFacet(t *testing.T) {
	t.Parallel()

	rec := serveBeliefs(&fakeBeliefs{groups: beliefFixture()}, beliefsGet())
	sec := facetSection(t, rec.Body.String(), selfmodel.FacetValue)

	v2 := strings.Index(sec, `data-belief-id="v-2"`)
	v1 := strings.Index(sec, `data-belief-id="v-1"`)
	if v2 < 0 || v1 < 0 {
		t.Fatalf("value facet lists v-2 at %d and v-1 at %d, want both:\n%s", v2, v1, sec)
	}
	if v2 > v1 {
		t.Error("value facet lists v-1 before v-2: the UI re-sorted what brain.ByFacet ordered")
	}
}

func TestBeliefsView_EachBeliefShowsItsOwnFields(t *testing.T) {
	t.Parallel()

	body := serveBeliefs(&fakeBeliefs{groups: beliefFixture()}, beliefsGet()).Body.String()

	v1 := beliefItem(t, body, "v-1")
	for _, want := range []string{"Keep promises", "0.80", "derived", "2026-09-02 08:15"} {
		if !strings.Contains(v1, want) {
			t.Errorf("v-1 does not show %q:\n%s", want, v1)
		}
	}
	for _, other := range []string{"Honesty first", "0.35", "user_stated", "2026-08-01"} {
		if strings.Contains(v1, other) {
			t.Errorf("v-1 shows %q, which belongs to v-2:\n%s", other, v1)
		}
	}

	v2 := beliefItem(t, body, "v-2")
	for _, want := range []string{"Honesty first", "0.35", "user_stated", "2026-08-01 10:30"} {
		if !strings.Contains(v2, want) {
			t.Errorf("v-2 does not show %q:\n%s", want, v2)
		}
	}
	if strings.Contains(v2, "0.80") {
		t.Errorf("v-2 shows v-1's confidence:\n%s", v2)
	}

	g1 := beliefItem(t, body, "g-1")
	if !strings.Contains(g1, "Run a &lt;b&gt;marathon&lt;/b&gt;") || strings.Contains(g1, "<b>marathon") {
		t.Errorf("g-1 content is not escaped:\n%s", g1)
	}
}

func TestBeliefsView_FormsPostContentToTheirOwnBeliefOnly(t *testing.T) {
	t.Parallel()

	body := serveBeliefs(&fakeBeliefs{groups: beliefFixture()}, beliefsGet()).Body.String()

	if !strings.Contains(body, beliefsResultMarker) {
		t.Errorf("page has no %s region for the forms to swap into", beliefsResultMarker)
	}

	for _, id := range []string{"v-1", "v-2"} {
		item := beliefItem(t, body, id)
		edit := between(t, item, `<details data-action="edit">`, "</details>")
		if !strings.Contains(edit, `action="/ui/beliefs/`+id+`/edit"`) {
			t.Errorf("%s edit form does not post to its own edit route:\n%s", id, edit)
		}
		if !strings.Contains(edit, `hx-target="#beliefs-result"`) {
			t.Errorf("%s edit form does not swap into the result region:\n%s", id, edit)
		}
		if !strings.Contains(edit, `<textarea id="content-`+id+`" name="content"`) {
			t.Errorf("%s edit form has no content textarea:\n%s", id, edit)
		}
		for _, banned := range []string{`name="facet"`, `name="confidence"`, `name="id"`, `name="origin"`, `name="topic_key"`} {
			if strings.Contains(item, banned) {
				t.Errorf("%s carries a %s input, but content is the only editable field:\n%s", id, banned, item)
			}
		}

		retire := between(t, item, `<details data-action="retire">`, "</details>")
		if !strings.Contains(retire, `action="/ui/beliefs/`+id+`/retire"`) {
			t.Errorf("%s retire form does not post to its own retire route:\n%s", id, retire)
		}
		if !strings.Contains(retire, "Confirm retire") {
			t.Errorf("%s retire is not behind a confirm step:\n%s", id, retire)
		}
	}

	v1edit := between(t, beliefItem(t, body, "v-1"), `<details data-action="edit">`, "</details>")
	if !strings.Contains(v1edit, ">Keep promises</textarea>") {
		t.Errorf("v-1 edit textarea is not prefilled with the stored content:\n%s", v1edit)
	}
	v2edit := between(t, beliefItem(t, body, "v-2"), `<details data-action="edit">`, "</details>")
	if !strings.Contains(v2edit, ">Honesty first</textarea>") || strings.Contains(v2edit, "Keep promises") {
		t.Errorf("v-2 edit textarea is not prefilled with its own content:\n%s", v2edit)
	}
}

func TestBeliefsView_ClaimHintOnlyOnBeliefsTheUserDoesNotOwn(t *testing.T) {
	t.Parallel()

	body := serveBeliefs(&fakeBeliefs{groups: beliefFixture()}, beliefsGet()).Body.String()

	const hint = "Save keeps this belief as yours"
	for _, id := range []string{"v-1", "g-1", "p-1"} {
		edit := between(t, beliefItem(t, body, id), `<details data-action="edit">`, "</details>")
		if !strings.Contains(edit, `data-hint="claim"`) || !strings.Contains(edit, hint) {
			t.Errorf("%s (derived or seed) edit form does not say that saving claims it:\n%s", id, edit)
		}
	}
	userStated := between(t, beliefItem(t, body, "v-2"), `<details data-action="edit">`, "</details>")
	if strings.Contains(userStated, `data-hint="claim"`) || strings.Contains(userStated, hint) {
		t.Errorf("v-2 is already the user's, but its edit form carries the claim hint:\n%s", userStated)
	}
}

func TestBeliefsView_NilDependencyAnswers503(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	ui.New(ui.Deps{}).ServeHTTP(rec, beliefsGet())

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /ui/beliefs with no Beliefs = %d, want 503", rec.Code)
	}
}

func TestBeliefsView_ListFailureAnswers500WithoutTheCause(t *testing.T) {
	t.Parallel()

	fake := &fakeBeliefs{listErr: errors.New("open /vault/nooma.db: disk is on fire")}
	rec := serveBeliefs(fake, beliefsGet())

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("GET /ui/beliefs with a failing read = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "disk is on fire") {
		t.Errorf("the 500 body leaks the cause:\n%s", rec.Body.String())
	}
}

func TestBeliefEdit_PassesThePathIDAndTheRawContentOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		id   string
		body string
		want beliefEditCall
	}{
		{
			name: "browser CRLF and padding reach brain untouched, a posted id, facet and confidence are ignored",
			id:   "v-1",
			body: "content=%20%20a%0D%0Ab%20%20&id=v-2&facet=goal&confidence=0.1&origin=seed",
			want: beliefEditCall{id: "v-1", content: "  a\r\nb  "},
		},
		{
			name: "a second belief",
			id:   "g-1",
			body: "content=other+text",
			want: beliefEditCall{id: "g-1", content: "other text"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeBeliefs{groups: beliefFixture()}
			rec := serveBeliefs(fake, beliefsPost(beliefEditPattern, tc.id, tc.body, false))

			if rec.Code != http.StatusOK {
				t.Fatalf("POST edit = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			if len(fake.editCalls) != 1 || fake.editCalls[0] != tc.want {
				t.Errorf("Edit calls = %+v, want exactly [%+v]", fake.editCalls, tc.want)
			}
			if len(fake.retireCall) != 0 {
				t.Errorf("an edit also called Retire: %v", fake.retireCall)
			}
			if got := outcomeOf(t, rec.Body.String()); !strings.Contains(got, `data-outcome="saved"`) || !strings.Contains(got, "Saved.") {
				t.Errorf("outcome = %s, want the saved outcome", got)
			}
		})
	}
}

// beliefErrorCases is the error to status and copy table both POST routes
// share. Every row asserts its own status AND its own marker, so a swap of
// two rows fails twice.
var beliefErrorCases = []struct {
	name    string
	err     error
	status  int
	outcome string
	copy    string
}{
	{"empty content", fmt.Errorf("belief edit: %w", selfmodel.ErrEmptyContent), http.StatusBadRequest, "invalid", "Content cannot be empty."},
	{"content too long", fmt.Errorf("belief edit: %w", selfmodel.ErrContentTooLong), http.StatusBadRequest, "invalid", "Content is too long: at most 1000 characters."},
	{"unknown belief", fmt.Errorf("belief edit: read: %w", ports.ErrBeliefNotFound), http.StatusNotFound, "", ""},
	{"changed or retired", fmt.Errorf("belief edit: %w", ports.ErrBeliefStatusConflict), http.StatusConflict, "conflict", "changed or was retired"},
	{"unexpected failure", errors.New("open /vault/nooma.db: disk is on fire"), http.StatusInternalServerError, "", ""},
}

func TestBeliefEdit_MapsEachErrorToItsOwnStatusAndCopy(t *testing.T) {
	t.Parallel()

	for _, tc := range beliefErrorCases {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s htmx=%v", tc.name, htmx), func(t *testing.T) {
				t.Parallel()

				fake := &fakeBeliefs{groups: beliefFixture(), editErr: tc.err}
				rec := serveBeliefs(fake, beliefsPost(beliefEditPattern, "v-1", "content=x", htmx))

				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
				}
				if tc.outcome != "" {
					got := outcomeOf(t, rec.Body.String())
					if !strings.Contains(got, `data-outcome="`+tc.outcome+`"`) || !strings.Contains(got, tc.copy) {
						t.Errorf("outcome = %s, want data-outcome=%q carrying %q", got, tc.outcome, tc.copy)
					}
				}
				if strings.Contains(rec.Body.String(), "disk is on fire") {
					t.Errorf("the response leaks the cause:\n%s", rec.Body.String())
				}
			})
		}
	}
}

func TestBeliefEdit_ConflictCopySaysChangedOrRetired(t *testing.T) {
	t.Parallel()

	fake := &fakeBeliefs{groups: beliefFixture(), editErr: ports.ErrBeliefStatusConflict}
	rec := serveBeliefs(fake, beliefsPost(beliefEditPattern, "v-1", "content=x", true))

	want := "This belief changed or was retired since the page was loaded. Reload to see its current state."
	if got := outcomeOf(t, rec.Body.String()); !strings.Contains(got, want) {
		t.Errorf("conflict outcome = %s, want the copy %q — a lost compare-and-swap can come from derive rewriting the text, not only from a retire", got, want)
	}
}

func TestBeliefEdit_InvalidContentRerendersTheFormWithTheSubmittedText(t *testing.T) {
	t.Parallel()

	fake := &fakeBeliefs{groups: beliefFixture(), editErr: selfmodel.ErrContentTooLong}
	rec := serveBeliefs(fake, beliefsPost(beliefEditPattern, "v-1", "content=half-typed+%3Ci%3Eedit%3C%2Fi%3E", false))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, beliefsFullPageTitle) {
		t.Errorf("a 400 on the full-page path must re-render the page:\n%s", body)
	}
	v1 := between(t, beliefItem(t, body, "v-1"), `<details data-action="edit"`, "</details>")
	if !strings.Contains(v1, ">half-typed &lt;i&gt;edit&lt;/i&gt;</textarea>") {
		t.Errorf("v-1's form lost the submitted text:\n%s", v1)
	}
	if !strings.Contains(v1, "<details data-action=\"edit\" open>") {
		t.Errorf("v-1's form is not left open on a rejected submit:\n%s", v1)
	}
	v2 := between(t, beliefItem(t, body, "v-2"), `<details data-action="edit"`, "</details>")
	if strings.Contains(v2, "half-typed") || !strings.Contains(v2, ">Honesty first</textarea>") {
		t.Errorf("v-2's form was touched by v-1's rejected submit:\n%s", v2)
	}
}

func TestBeliefEdit_ContentComesFromTheBodyNotTheQuery(t *testing.T) {
	t.Parallel()

	fake := &fakeBeliefs{groups: beliefFixture()}
	req := beliefsPost(beliefEditPattern, "v-1", "", false)
	req.URL.RawQuery = "content=smuggled"
	serveBeliefs(fake, req)

	if len(fake.editCalls) != 1 || fake.editCalls[0].content != "" {
		t.Errorf("Edit calls = %+v, want one call with the (empty) body content, not the query's", fake.editCalls)
	}
}

func TestBeliefEdit_OnlyARejectedSubmitReopensThatBeliefsForm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		code int
	}{
		{"saved", nil, http.StatusOK},
		{"conflict", ports.ErrBeliefStatusConflict, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeBeliefs{groups: beliefFixture(), editErr: tc.err}
			rec := serveBeliefs(fake, beliefsPost(beliefEditPattern, "v-1", "content=typed+text", false))

			if rec.Code != tc.code {
				t.Fatalf("status = %d, want %d", rec.Code, tc.code)
			}
			form := between(t, beliefItem(t, rec.Body.String(), "v-1"), `<details data-action="edit"`, "</details>")
			if strings.Contains(form, "typed text") || !strings.Contains(form, ">Keep promises</textarea>") {
				t.Errorf("the form shows the submitted text instead of the stored one:\n%s", form)
			}
			if strings.Contains(form, " open") {
				t.Errorf("the form is left open after a submit that was not rejected for its content:\n%s", form)
			}
		})
	}
}

func TestBeliefEdit_RejectsAnOversizedBodyBeforeCallingBrain(t *testing.T) {
	t.Parallel()

	fake := &fakeBeliefs{groups: beliefFixture()}
	rec := serveBeliefs(fake, beliefsPost(beliefEditPattern, "v-1", "content="+strings.Repeat("a", 70*1024), false))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if len(fake.editCalls) != 0 {
		t.Errorf("Edit was called %d time(s) for a body over the 64 KiB bound", len(fake.editCalls))
	}
}

func TestBeliefRetire_PassesThePathIDOnly(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"v-1", "g-1"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			fake := &fakeBeliefs{groups: beliefFixture()}
			rec := serveBeliefs(fake, beliefsPost(beliefRetirePattern, id, "id=v-2&content=nope", false))

			if rec.Code != http.StatusOK {
				t.Fatalf("POST retire = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			if len(fake.retireCall) != 1 || fake.retireCall[0] != id {
				t.Errorf("Retire calls = %v, want exactly [%s]", fake.retireCall, id)
			}
			if len(fake.editCalls) != 0 {
				t.Errorf("a retire also called Edit: %+v", fake.editCalls)
			}
			if got := outcomeOf(t, rec.Body.String()); !strings.Contains(got, `data-outcome="retired"`) || !strings.Contains(got, "Retired.") {
				t.Errorf("outcome = %s, want the retired outcome", got)
			}
		})
	}
}

func TestBeliefRetire_MapsEachErrorToItsOwnStatusAndCopy(t *testing.T) {
	t.Parallel()

	for _, tc := range beliefErrorCases {
		if tc.status == http.StatusBadRequest {
			continue // content validation belongs to the edit route
		}
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s htmx=%v", tc.name, htmx), func(t *testing.T) {
				t.Parallel()

				fake := &fakeBeliefs{groups: beliefFixture(), retireErr: tc.err}
				rec := serveBeliefs(fake, beliefsPost(beliefRetirePattern, "v-1", "", htmx))

				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
				}
				if tc.outcome != "" {
					got := outcomeOf(t, rec.Body.String())
					if !strings.Contains(got, `data-outcome="`+tc.outcome+`"`) || !strings.Contains(got, tc.copy) {
						t.Errorf("outcome = %s, want data-outcome=%q carrying %q", got, tc.outcome, tc.copy)
					}
				}
				if strings.Contains(rec.Body.String(), "disk is on fire") {
					t.Errorf("the response leaks the cause:\n%s", rec.Body.String())
				}
			})
		}
	}
}

func TestBeliefPosts_NilDependencyAnswers503(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{beliefEditPattern, beliefRetirePattern} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			ui.New(ui.Deps{}).ServeHTTP(rec, beliefsPost(pattern, "v-1", "content=x", false))

			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("%s with no Beliefs = %d, want 503", pattern, rec.Code)
			}
		})
	}
}

func TestBeliefPosts_AnswerAFragmentOnHTMXAndTheFullPageOtherwise(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{beliefEditPattern, beliefRetirePattern} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			frag := &fakeBeliefs{groups: beliefFixture()}
			fragBody := serveBeliefs(frag, beliefsPost(pattern, "v-1", "content=x", true)).Body.String()
			if strings.Contains(fragBody, "<html") || strings.Contains(fragBody, beliefsFullPageTitle) {
				t.Errorf("an HX-Request answer carries the full page:\n%s", fragBody)
			}
			if !strings.HasPrefix(strings.TrimSpace(fragBody), "<p data-outcome=") {
				t.Errorf("an HX-Request answer does not lead with the outcome fragment:\n%s", fragBody)
			}
			// An edit re-reads the belief it changed, once; a retire has nothing to
			// read, the item is only removed. Neither lists for the full page.
			wantReads := 1
			if pattern == beliefRetirePattern {
				wantReads = 0
			}
			if frag.listCalls != wantReads {
				t.Errorf("an HX-Request answer read the list %d time(s), want %d", frag.listCalls, wantReads)
			}

			page := &fakeBeliefs{groups: beliefFixture()}
			pageBody := serveBeliefs(page, beliefsPost(pattern, "v-1", "content=x", false)).Body.String()
			if !strings.Contains(pageBody, "<html") || !strings.Contains(pageBody, beliefsFullPageTitle) {
				t.Errorf("a plain POST does not answer the full page:\n%s", pageBody)
			}
			if page.listCalls != 1 {
				t.Errorf("a plain POST read the list %d time(s), want 1", page.listCalls)
			}
			region := between(t, pageBody, beliefsResultMarker, "</p>")
			if !strings.Contains(region, "data-outcome=") {
				t.Errorf("the outcome is not inside the result region:\n%s", region)
			}
		})
	}
}

func TestBeliefPosts_ALandedWriteIsStillReportedWhenTheListFails(t *testing.T) {
	t.Parallel()

	fake := &fakeBeliefs{listErr: errors.New("disk is on fire")}
	rec := serveBeliefs(fake, beliefsPost(beliefEditPattern, "v-1", "content=x", false))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the write landed", rec.Code)
	}
	if got := outcomeOf(t, rec.Body.String()); !strings.Contains(got, "Saved.") {
		t.Errorf("outcome = %s, want the saved outcome", got)
	}
	if strings.Contains(rec.Body.String(), "disk is on fire") {
		t.Errorf("the response leaks the cause:\n%s", rec.Body.String())
	}
}

// recordingHandler is a slog.Handler that keeps every record, so the test can
// assert that a landed write is logged as a warning with its cause.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// TestBeliefWriteLanded_ShowsSuccessWithTheNoticeOfTheMissingPartOnly swaps
// slog's default logger, so it is deliberately not parallel: the parallel
// tests of this package are paused until every sequential test has finished.
func TestBeliefWriteLanded_ShowsSuccessWithTheNoticeOfTheMissingPartOnly(t *testing.T) {
	variants := []struct {
		name   string
		landed *brain.WriteLandedError
		notice string
	}{
		{"record only", &brain.WriteLandedError{Record: true, Err: errors.New("record: disk is on fire")}, "%s, but the activity record of it could not be written."},
		{"signal only", &brain.WriteLandedError{Signal: true, Err: errors.New("signal: disk is on fire")}, "%s and recorded, but the learning signal could not be written."},
		{"both", &brain.WriteLandedError{Record: true, Signal: true, Err: errors.New("both: disk is on fire")}, "%s, but neither the activity record nor the learning signal could be written."},
		{"neither part named", &brain.WriteLandedError{Err: errors.New("unnamed: disk is on fire")}, "%s, but a follow-up write failed."},
	}

	for _, pattern := range []string{beliefEditPattern, beliefRetirePattern} {
		// A retire says "Retired", an edit or claim "Saved": the verb is the action's.
		verb := "Saved"
		if pattern == beliefRetirePattern {
			verb = "Retired"
		}
		for _, v := range variants {
			t.Run(pattern+" "+v.name, func(t *testing.T) {
				v.notice = fmt.Sprintf(v.notice, verb)
				rec := &recordingHandler{}
				prev := slog.Default()
				slog.SetDefault(slog.New(rec))
				t.Cleanup(func() { slog.SetDefault(prev) })

				fake := &fakeBeliefs{groups: beliefFixture()}
				wrapped := fmt.Errorf("belief write: %w", v.landed)
				fake.editErr, fake.retireErr = wrapped, wrapped
				resp := serveBeliefs(fake, beliefsPost(pattern, "v-1", "content=x", true))

				if resp.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200: the write landed, a retry would only conflict", resp.Code)
				}
				got := outcomeOf(t, resp.Body.String())
				if !strings.Contains(got, `data-outcome="saved-with-notice"`) || !strings.Contains(got, v.notice) {
					t.Errorf("outcome = %s, want a saved-with-notice outcome carrying %q", got, v.notice)
				}
				for _, other := range variants {
					if other.name != v.name && strings.Contains(got, fmt.Sprintf(other.notice, verb)) {
						t.Errorf("outcome also carries the %q notice: %s", other.name, got)
					}
				}
				if strings.Contains(got, "disk is on fire") {
					t.Errorf("the notice leaks the cause: %s", got)
				}

				if len(rec.records) != 1 {
					t.Fatalf("slog records = %d, want exactly 1", len(rec.records))
				}
				r := rec.records[0]
				if r.Level != slog.LevelWarn {
					t.Errorf("log level = %s, want WARN", r.Level)
				}
				attrs := map[string]string{}
				r.Attrs(func(a slog.Attr) bool {
					attrs[a.Key] = a.Value.String()
					return true
				})
				if !strings.Contains(attrs["err"], "disk is on fire") {
					t.Errorf("the warning's err attribute = %q, want the cause", attrs["err"])
				}
				if attrs["belief"] != "v-1" {
					t.Errorf("the warning's belief attribute = %q, want the belief id v-1", attrs["belief"])
				}
			})
		}
	}
}

// beliefSwapFixture is a fake whose Edit applies the change the way brain
// would for a claim: the text becomes the submitted one and the origin becomes
// user_stated, so the fresh read shows the belief as the user's.
func claimingFake() *fakeBeliefs {
	return &fakeBeliefs{groups: beliefFixture(), onEdit: func(f *fakeBeliefs, id, content string) {
		for gi := range f.groups {
			for bi := range f.groups[gi].Beliefs {
				if b := &f.groups[gi].Beliefs[bi]; b.ID == id {
					b.Content, b.Origin = content, selfmodel.OriginUserStated
				}
			}
		}
	}}
}

func TestBeliefRetire_HTMXAnswerRemovesThatItemAndNothingElse(t *testing.T) {
	t.Parallel()

	fake := &fakeBeliefs{groups: beliefFixture()}
	rec := serveBeliefs(fake, beliefsPost(beliefRetirePattern, "v-1", "", true))
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := outcomeOf(t, body); !strings.Contains(got, `data-outcome="retired"`) {
		t.Errorf("outcome = %s, want the retired outcome", got)
	}
	removal := between(t, body, `<li id="belief-v-1"`, "</li>")
	if !strings.Contains(removal, `hx-swap-oob="delete"`) {
		t.Errorf("the removal of v-1 is not an out-of-band delete: %s", removal)
	}
	for _, other := range []string{"v-2", "g-1", "p-1"} {
		if strings.Contains(body, "belief-"+other) {
			t.Errorf("the answer touches %s, which was not retired:\n%s", other, body)
		}
	}
	if strings.Contains(body, "data-belief-id=") || strings.Contains(body, "data-field=") {
		t.Errorf("a retire answer re-renders an item instead of removing it:\n%s", body)
	}
}

func TestBeliefEdit_HTMXClaimReplacesTheItemAsUserStatedWithoutTheHint(t *testing.T) {
	t.Parallel()

	rec := serveBeliefs(claimingFake(), beliefsPost(beliefEditPattern, "v-1", "content=Keep+promises", true))
	body := rec.Body.String()

	if got := outcomeOf(t, body); !strings.Contains(got, `data-outcome="saved"`) {
		t.Errorf("outcome = %s, want the saved outcome", got)
	}
	item := beliefItem(t, body, "v-1")
	if !strings.Contains(item, `hx-swap-oob="true"`) {
		t.Errorf("the item is not swapped out of band: %s", item)
	}
	if got := between(t, item, `<span data-field="origin">`, "</span>"); !strings.Contains(got, "user_stated") {
		t.Errorf("origin = %s, want user_stated after a claim", got)
	}
	if strings.Contains(item, `data-hint="claim"`) {
		t.Errorf("a claimed belief still shows the claim hint: %s", item)
	}
	for _, other := range []string{"v-2", "g-1", "p-1"} {
		if strings.Contains(body, "belief-"+other) {
			t.Errorf("the answer touches %s, which was not edited:\n%s", other, body)
		}
	}
}

func TestBeliefEdit_HTMXAnswerCarriesTheNewContent(t *testing.T) {
	t.Parallel()

	rec := serveBeliefs(claimingFake(), beliefsPost(beliefEditPattern, "g-1", "content=Run+a+half+marathon", true))
	item := beliefItem(t, rec.Body.String(), "g-1")

	if got := between(t, item, `<p data-field="content">`, "</p>"); !strings.Contains(got, "Run a half marathon") {
		t.Errorf("content = %s, want the new text", got)
	}
	if got := between(t, item, "<textarea", "</textarea>"); !strings.Contains(got, "Run a half marathon") {
		t.Errorf("the edit form = %s, want the new text", got)
	}
}

func TestBeliefEdit_HTMXFailuresSwapNoItem(t *testing.T) {
	t.Parallel()

	for name, err := range map[string]error{
		"invalid":  selfmodel.ErrEmptyContent,
		"conflict": ports.ErrBeliefStatusConflict,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeBeliefs{groups: beliefFixture(), editErr: err}
			body := serveBeliefs(fake, beliefsPost(beliefEditPattern, "v-1", "content=x", true)).Body.String()
			if strings.Contains(body, "hx-swap-oob") {
				t.Errorf("a refused edit swaps an item:\n%s", body)
			}
		})
	}
}

func TestLayout_NavLinksToBeliefsAndActivity(t *testing.T) {
	t.Parallel()

	pages := map[string]string{}
	var buf strings.Builder
	if err := ui.BeliefsPage(nil, nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("BeliefsPage.Render: %v", err)
	}
	pages["beliefs"] = buf.String()
	buf.Reset()
	if err := ui.Today(brain.Today{}, ui.Serving{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("Today.Render: %v", err)
	}
	pages["today"] = buf.String()

	for name, page := range pages {
		nav := between(t, page, "<nav", "</nav>")
		if !strings.Contains(nav, `<a href="/ui/beliefs"`) {
			t.Errorf("%s: nav has no beliefs link:\n%s", name, nav)
		}
		if !strings.Contains(nav, `<a href="/ui/activity"`) {
			t.Errorf("%s: nav has no activity link:\n%s", name, nav)
		}
	}
}
