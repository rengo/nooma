//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestServeUIPreferencesShowAndSave drives /ui/preferences on the compiled
// binary: the authenticated GET shows the defaults, a POST saves a later
// quiet end, and the next GET shows it. The vault has no provider bound, so
// this also proves serve hands wirePreferences' service to the UI: a nil
// there would answer 503.
func TestServeUIPreferencesShowAndSave(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n  auth_token_env: NOOMA_SERVE_UI_PREFERENCES_TEST_TOKEN\n", port))
	const token = "ui-preferences-e2e-token"
	t.Setenv("NOOMA_SERVE_UI_PREFERENCES_TEST_TOKEN", token)

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

	do := func(method, form string) (int, string) {
		t.Helper()
		var body io.Reader
		if form != "" {
			body = strings.NewReader(form)
		}
		req, err := http.NewRequest(method, base+"/ui/preferences", body)
		if err != nil {
			t.Fatal(err)
		}
		if form != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.AddCookie(cookies[0])
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s /ui/preferences: %v", method, err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(b)
	}

	if code, page := do(http.MethodGet, ""); code != http.StatusOK || !strings.Contains(page, `name="quiet_hours_end" value="07:00"`) {
		t.Fatalf("GET /ui/preferences = %d, want 200 showing the default quiet end:\n%s", code, page)
	}
	form := "lead_amount=24&lead_unit=hours&lead_amount=2&lead_unit=hours&date_only_reminder_at=09:00&quiet_hours_end=08:00&evening_reminder_at=22:00"
	if code, page := do(http.MethodPost, form); code != http.StatusOK || !strings.Contains(page, "Preferences saved.") {
		t.Fatalf("POST /ui/preferences = %d, want 200 and the saved notice:\n%s", code, page)
	}
	if code, page := do(http.MethodGet, ""); code != http.StatusOK || !strings.Contains(page, `name="quiet_hours_end" value="08:00"`) {
		t.Errorf("GET /ui/preferences after the save = %d, want the 08:00 quiet end:\n%s", code, page)
	}
}
