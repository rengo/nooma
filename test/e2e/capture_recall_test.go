//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestServeCaptureAndRecallAreReachableButUnwired is R2.8's own L4 test for
// this link (13c): the compiled `nooma serve` binary, over a real migrated
// vault and a real socket, answers POST /capture, POST /recall and GET
// /units/{id} honestly in the transitional state cmd/nooma/serve.go is
// still in — Capture and Recall are both nil until 13d's full wiring
// (providers/repos/Index/services) lands. This pins that reaching any of
// them over a real socket answers 503 "not wired in this build" — the same
// guard TestCaptureHandler_UnwiredCaptureServiceReturns503 (13b) and this
// PR's own TestRecallHandler_UnwiredRecallServiceReturns503 /
// TestUnitByIDHandler_UnwiredRecallServiceReturns503 already pin at the
// handler-test level — rather than hanging, crashing, or silently
// succeeding, proven once more at the compiled-binary level.
//
// This is deliberately NOT R2.8's full demo shape ("posts a capture, and
// issues a recall that finds it") — that needs real wiring this link does
// not carry. See TestServeCaptureAndRecallRoundTripThroughRealWiring below
// (13d) for the extension this test's own doc comment used to name as
// future work: it drives the identical two routes over a vault whose
// nooma.yml actually configures the three tasksM1Consumes providers, and
// this test keeps proving the other half — that an UNCONFIGURED vault
// still answers honestly rather than crashing, the same nil-Deps guard
// this link's own routes always carry regardless of what 13d wires.
func TestServeCaptureAndRecallAreReachableButUnwired(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf("server:\n  bind: 127.0.0.1\n  http_port: %d\n", port))

	startServe(t, home, vault, port)

	captureBody, err := json.Marshal(map[string]string{"text": "pick up the dry cleaning"})
	if err != nil {
		t.Fatal(err)
	}
	captureResp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/capture", port), "application/json", bytes.NewReader(captureBody))
	if err != nil {
		t.Fatalf("POST /capture: %v", err)
	}
	defer func() { _ = captureResp.Body.Close() }()
	if captureResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("POST /capture = %d, want %d (Capture is nil until 13d)", captureResp.StatusCode, http.StatusServiceUnavailable)
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
	if recallResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("POST /recall = %d, want %d (Recall is nil until 13d)", recallResp.StatusCode, http.StatusServiceUnavailable)
	}

	unitResp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/units/whatever", port))
	if err != nil {
		t.Fatalf("GET /units/whatever: %v", err)
	}
	defer func() { _ = unitResp.Body.Close() }()
	if unitResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("GET /units/whatever = %d, want %d (Recall is nil until 13d)", unitResp.StatusCode, http.StatusServiceUnavailable)
	}
}

// mockOllama stands in for a real Ollama listener over a loopback
// httptest.Server — an in-process listener, not "the network" in
// docs/06-harness.md §3's sense (the same posture design §6 already
// states for provider client tests). It answers both endpoints
// ollama.Client speaks (client.go's /api/generate, embed.go's /api/embed)
// with a single fixed reply each, regardless of the request body: this
// test needs no scenario variety, only a deterministic vendor to wire
// against.
func mockOllama(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/generate":
			// testdata/llm/cases/classify-pick-up-dry-cleaning.json's own
			// recorded response — the "ordinary task" fixture reused
			// across this repository's fakeprovider-driven tests, replayed
			// here verbatim over a real socket instead of a scripted fake.
			_, _ = fmt.Fprint(w, `{"model":"test-model","response":"{\"type\":\"task\",\"normalized_content\":\"Pick up the dry cleaning\",\"weight\":0.6,\"decay_rate\":0.1}","done":true}`)
		case "/api/embed":
			// A fixed vector regardless of input text: capture-time and
			// recall-time embedding calls both hit this same handler, so
			// both return the identical vector, which is all
			// recall.Search's cosine comparison needs to find a perfect
			// match without this test asserting anything about real
			// semantic similarity.
			_, _ = fmt.Fprint(w, `{"model":"test-model","embeddings":[[0.1,0.2,0.3]]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestServeCaptureAndRecallRoundTripThroughRealWiring is R2.8's own full
// demo shape (spec: "posts a capture, and issues a recall that finds it"),
// finally reachable now that this link (13d) wires
// config->providers->repos->Index->services->token into serve.go's real
// Handler(Deps) instead of the transitional nils
// TestServeCaptureAndRecallAreReachableButUnwired above still pins for an
// unconfigured vault. Unlike that test, this one's nooma.yml binds all
// three tasksM1Consumes tasks to one ollama-typed provider pointed at
// mockOllama — proving serve's real wiring, not a handler test's
// manually-built Deps, resolves a genuinely configured vault into a
// working CaptureService/RecallService pair reachable over a real socket.
func TestServeCaptureAndRecallRoundTripThroughRealWiring(t *testing.T) {
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
  embedding:
    provider: local
`, port, llm.URL))

	startServe(t, home, vault, port)

	captureBody, err := json.Marshal(map[string]string{"text": "Pick up the dry cleaning on Friday"})
	if err != nil {
		t.Fatal(err)
	}
	captureResp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/capture", port), "application/json", bytes.NewReader(captureBody))
	if err != nil {
		t.Fatalf("POST /capture: %v", err)
	}
	defer func() { _ = captureResp.Body.Close() }()
	if captureResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /capture = %d, want 201 (a fully-wired vault must store this capture)", captureResp.StatusCode)
	}
	var captured struct {
		Outcome  string `json:"outcome"`
		Embedded bool   `json:"embedded"`
	}
	if err := json.NewDecoder(captureResp.Body).Decode(&captured); err != nil {
		t.Fatalf("decoding POST /capture response: %v", err)
	}
	if captured.Outcome != "stored" || !captured.Embedded {
		t.Fatalf("POST /capture body = %+v, want outcome=stored embedded=true", captured)
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
		t.Fatalf("POST /recall units = %+v, want exactly the unit just captured — a real recall finding a real capture", recalled.Units)
	}
}
