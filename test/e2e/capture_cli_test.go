//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestCaptureCLIPersistsThroughARealServer is 14a.5, R3.1's own L4 half: the
// compiled `nooma capture` subcommand, run as a subprocess against a real
// `nooma serve` instance over a real socket, persists a unit — proven by a
// subsequent POST /recall (the same route
// TestServeCaptureAndRecallRoundTripThroughRealWiring already proves for the
// HTTP entrance) finding it. Design D11's "one mechanism, two entrances":
// the CLI is a second way in to the identical pipeline, never a second
// writer, so this test drives the CLI and verifies through the API, rather
// than only through the CLI's own printed summary.
func TestCaptureCLIPersistsThroughARealServer(t *testing.T) {
	llm := mockOllama(t)

	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf(`server:
  bind: 127.0.0.1
  http_port: %d
providers:
  local:
    type: ollama
    model: test-model
    endpoint: %s
tasks:
  capture_processing:
    provider: local
  relation_evaluation:
    provider: local
  chat:
    provider: local
  embedding:
    provider: local
`, port, llm.URL))

	startServe(t, home, vault, port)

	stdout, stderr, err := nooma(t, home, work, "capture", "Pick up the dry cleaning on Friday", vault)
	if err != nil {
		t.Fatalf("capture: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "stored") {
		t.Errorf("capture stdout does not say the unit was stored:\n%s", stdout)
	}

	recallBody, err := json.Marshal(map[string]string{"query": "dry cleaning"})
	if err != nil {
		t.Fatal(err)
	}
	recallResp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/recall", port), "application/json", bytes.NewReader(recallBody))
	if err != nil {
		t.Fatalf("POST /recall: %v", err)
	}
	defer func() { _ = recallResp.Body.Close() }()
	if recallResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /recall = %d, want 200", recallResp.StatusCode)
	}
	var recalled struct {
		Units []struct {
			Content string `json:"content"`
		} `json:"units"`
	}
	if err := json.NewDecoder(recallResp.Body).Decode(&recalled); err != nil {
		t.Fatalf("decoding POST /recall response: %v", err)
	}
	if len(recalled.Units) != 1 || recalled.Units[0].Content != "Pick up the dry cleaning" {
		t.Fatalf("POST /recall units = %+v, want exactly the unit nooma capture just persisted", recalled.Units)
	}
}
