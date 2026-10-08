//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestServeUIBeliefsListEditAndRetire is m4e's exit criterion for the
// beliefs mirror, end to end on the compiled binary: a belief seeded in the
// vault is listed on the authenticated GET /ui/beliefs, an edit through
// POST /ui/beliefs/{id}/edit changes its text on the next read, and a retire
// through POST /ui/beliefs/{id}/retire takes it off the list. The vault has
// no provider bound, so this also proves wireBeliefs and uiDeps hand the
// service to the handler without one: a nil there would answer 503.
func TestServeUIBeliefsListEditAndRetire(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n  auth_token_env: NOOMA_SERVE_UI_BELIEFS_TEST_TOKEN\n", port))
	const token = "ui-beliefs-e2e-token"
	t.Setenv("NOOMA_SERVE_UI_BELIEFS_TEST_TOKEN", token)

	seeded := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	db, err := sqlite.Open(context.Background(), vaultDBPath(t, vault))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	if err := sqlite.NewSelfModelRepo(db).UpsertByTopicKey(context.Background(), ports.Belief{
		ID: "belief-e2e", Facet: selfmodel.FacetGoal, TopicKey: "goal/marathon", Content: "run a marathon",
		Confidence: 0.7, Origin: selfmodel.OriginDerived, Status: selfmodel.StatusActive,
		LastReinforcedAt: seeded, CreatedAt: seeded, UpdatedAt: seeded,
	}); err != nil {
		t.Fatalf("seeding the belief: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing the vault before serve: %v", err)
	}

	startServe(t, home, vault, &port)

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := noRedirectClient()

	loginResp, err := client.PostForm(base+"/ui/login", url.Values{"token": {token}})
	if err != nil {
		t.Fatalf("POST /ui/login: %v", err)
	}
	_ = loginResp.Body.Close()
	cookies := loginResp.Cookies()
	if loginResp.StatusCode != http.StatusSeeOther || len(cookies) != 1 {
		t.Fatalf("POST /ui/login = %d with %d cookie(s), want 303 and 1", loginResp.StatusCode, len(cookies))
	}

	do := func(method, path, form string) (int, string) {
		t.Helper()
		var body io.Reader
		if form != "" {
			body = strings.NewReader(form)
		}
		req, err := http.NewRequest(method, base+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if form != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.AddCookie(cookies[0])
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(b)
	}

	code, page := do(http.MethodGet, "/ui/beliefs", "")
	if code != http.StatusOK || !strings.Contains(page, `data-belief-id="belief-e2e"`) || !strings.Contains(page, "run a marathon") {
		t.Fatalf("GET /ui/beliefs = %d, want 200 listing the seeded belief:\n%s", code, page)
	}
	if n := strings.Count(page, "<section data-facet="); n != len(selfmodel.AllFacets()) {
		t.Errorf("GET /ui/beliefs renders %d facet sections, want %d", n, len(selfmodel.AllFacets()))
	}

	code, page = do(http.MethodPost, "/ui/beliefs/belief-e2e/edit", "content=run+a+half+marathon")
	if code != http.StatusOK || !strings.Contains(page, `data-outcome="saved"`) {
		t.Fatalf("POST edit = %d, want 200 and the saved outcome:\n%s", code, page)
	}
	_, page = do(http.MethodGet, "/ui/beliefs", "")
	if !strings.Contains(page, "run a half marathon") || !strings.Contains(page, `data-field="origin">user_stated<`) {
		t.Errorf("GET /ui/beliefs after the edit does not show the new text as user_stated:\n%s", page)
	}

	if code, _ = do(http.MethodPost, "/ui/beliefs/no-such-belief/retire", ""); code != http.StatusNotFound {
		t.Errorf("POST retire of an unknown id = %d, want 404", code)
	}
	code, page = do(http.MethodPost, "/ui/beliefs/belief-e2e/retire", "")
	if code != http.StatusOK || !strings.Contains(page, `data-outcome="retired"`) {
		t.Fatalf("POST retire = %d, want 200 and the retired outcome:\n%s", code, page)
	}
	if code, _ = do(http.MethodPost, "/ui/beliefs/belief-e2e/retire", ""); code != http.StatusConflict {
		t.Errorf("POST retire twice = %d, want 409", code)
	}
	_, page = do(http.MethodGet, "/ui/beliefs", "")
	if strings.Contains(page, "belief-e2e") {
		t.Errorf("GET /ui/beliefs after the retire still lists the belief:\n%s", page)
	}
}
