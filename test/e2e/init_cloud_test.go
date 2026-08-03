//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockOpenAI stands in for OpenAI's real API over a loopback httptest.Server
// — the same posture mockOllama (capture_recall_test.go) already takes for
// its own vendor, an in-process listener rather than "the network" in
// docs/06-harness.md §3's sense. It answers both endpoints the openai
// package speaks: client.go's /v1/chat/completions (classify) and
// embed.go's /v1/embeddings.
func mockOpenAI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			// The same "ordinary task" fixture text mockOllama replays,
			// wrapped in OpenAI's own chat-completions response shape.
			_, _ = fmt.Fprint(w, `{"model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"{\"type\":\"task\",\"normalized_content\":\"Pick up the dry cleaning\",\"weight\":0.6,\"decay_rate\":0.1}"}}]}`)
		case "/v1/embeddings":
			_, _ = fmt.Fprint(w, `{"model":"text-embedding-3-small","data":[{"embedding":[0.1,0.2,0.3]}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// patchNoomaYML replaces literal substrings in a vault's nooma.yml — used
// here to point the wizard's own generated openai entries at a loopback
// server and to set the port serve binds to, without hand-writing a config
// that only imitates what the wizard produces. writeConfig (serve_test.go)
// overwrites the whole file for other tests; this one exists specifically
// to prove the WIZARD's own output works end to end, so the file must stay
// the wizard's own bytes apart from these substitutions — the same reason
// design D15 states the endpoint field exists at all: "so a test can point
// a wizard-written vault at a loopback httptest server."
func patchNoomaYML(t *testing.T, vault string, replacements map[string]string) {
	t.Helper()

	path := filepath.Join(vault, "nooma.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for old, replacement := range replacements {
		text = strings.ReplaceAll(text, old, replacement)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestInitCloudPathWizardVaultEmbedsACaptureThroughTheRealBinary is spec
// R6.3's L4 half (R8.1's own level assignment — not a duplicate of
// test/conformance's L2 half, which proves the distinction exists in the
// pipeline; this proves it survives being wired together for real) and
// R6.2's own verification: a freshly `init`ed Cloud vault's tasks: block
// includes an embedding entry, readable without running a capture at all,
// AND a capture against it actually stores a vector.
func TestInitCloudPathWizardVaultEmbedsACaptureThroughTheRealBinary(t *testing.T) {
	llm := mockOpenAI(t)

	home, work := t.TempDir(), t.TempDir()
	target := filepath.Join(work, "cloud.nooma")

	// The exact Cloud-path prompt sequence: "1" chooses Cloud, the blank
	// line accepts the default OPENAI_API_KEY env var name.
	stdout, stderr, err := noomaWithStdin(t, home, work, "1\n\n", "init", target)
	if err != nil {
		t.Fatalf("init: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	assertVault(t, target)

	raw, err := os.ReadFile(filepath.Join(target, "nooma.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "embedding:") {
		t.Fatalf("the wizard-written nooma.yml has no embedding: task binding, readable without running a capture at all:\n%s", raw)
	}

	port := freePort(t)
	patchNoomaYML(t, target, map[string]string{
		"http_port: 7777": fmt.Sprintf("http_port: %d", port),
		"type: openai\n":  fmt.Sprintf("type: openai\n    endpoint: %s\n", llm.URL),
	})

	startServe(t, home, target, port)

	captureBody, err := json.Marshal(map[string]string{"text": "Pick up the dry cleaning on Friday"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/capture", port), "application/json", bytes.NewReader(captureBody))
	if err != nil {
		t.Fatalf("POST /capture: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /capture = %d, want 201", resp.StatusCode)
	}
	var captured struct {
		Outcome  string `json:"outcome"`
		Embedded bool   `json:"embedded"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&captured); err != nil {
		t.Fatalf("decoding POST /capture response: %v", err)
	}
	if captured.Outcome != "stored" || !captured.Embedded {
		t.Fatalf("POST /capture body = %+v, want outcome=stored embedded=true — a wizard-written Cloud vault must actually embed", captured)
	}
}

// TestInitCloudPathNeverWritesTheScriptedKeyValue is spec R4.3's own L4
// scenario: a wizard run with a scripted key-shaped value asserts the
// literal key string appears nowhere in the written nooma.yml, while .env
// carries the guidance to set it (this PR's own structural choice — see
// cmd/nooma/init.go's promptProviderSetup doc comment: the wizard never
// asks for a credential VALUE at all, so the one place a user could type
// one is the env-var-name prompt, and NewEnvVarName's rejection is what
// this test proves holds even there).
func TestInitCloudPathNeverWritesTheScriptedKeyValue(t *testing.T) {
	const scriptedKey = "sk-proj-not-a-real-key-0123456789"

	home, work := t.TempDir(), t.TempDir()
	target := filepath.Join(work, "cloud.nooma")

	// "1" chooses Cloud; the scripted key is pasted where the env var NAME
	// is asked — the one remaining wizard input a confused user could
	// reach for a credential with.
	stdout, _, err := noomaWithStdin(t, home, work, "1\n"+scriptedKey+"\n", "init", target)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	assertVault(t, target)

	yml, err := os.ReadFile(filepath.Join(target, "nooma.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(yml), scriptedKey) {
		t.Fatalf("nooma.yml contains the scripted key value:\n%s", yml)
	}
	if !strings.Contains(string(yml), "api_key_env: OPENAI_API_KEY") {
		t.Errorf("nooma.yml does not fall back to the documented default env var name:\n%s", yml)
	}

	envPath := filepath.Join(target, ".env")
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf(".env permissions = %o, want 0600", perm)
	}
	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envBytes), scriptedKey) {
		t.Fatalf(".env contains the scripted key value, which this wizard never collects:\n%s", envBytes)
	}

	if !strings.Contains(stdout, "OPENAI_API_KEY") {
		t.Errorf("the wizard's own output does not instruct the user to set the key themselves:\n%s", stdout)
	}
}
