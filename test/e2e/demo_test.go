//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDemoCaptureAskCorrectionEndToEnd is 14b.1, R3.2's own L4 proof and
// M1's exit criterion (docs/05-build-plan.md §M1: "capture via API/CLI, ask
// 'what do you know about X?' and get a real recall") walked as one
// sequence over one vault, in the order a user would do it, rather than
// pieced together from each link's own narrower test: capture through the
// API, capture through the CLI, ask a real question and get the right
// content back, then correct one of the two captures and see the edit
// land. No step here is new production code — every mechanism (POST
// /capture, `nooma capture`, POST /recall, an explicit-referent
// correction) already has its own proof earlier in this chain (13d, 14a,
// 13c, 12g); what no test before this one walks is the whole sequence in
// order, which is the thing eighteen separate proofs cannot by themselves
// show.
//
// No case here is a timer or a recurring_reminder (R3.3, Q3a's own "the
// demo must not be shown a timer") — both captures below classify as
// `task`, and the correction never changes that.
func TestDemoCaptureAskCorrectionEndToEnd(t *testing.T) {
	llm := demoProvider(t)

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
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Step 1 — capture via the API: R2.1's own entrance.
	captureBody, err := json.Marshal(map[string]string{"text": "Pick up the dry cleaning on Friday"})
	if err != nil {
		t.Fatal(err)
	}
	apiResp, err := http.Post(base+"/capture", "application/json", bytes.NewReader(captureBody))
	if err != nil {
		t.Fatalf("step 1 (API capture): POST /capture: %v", err)
	}
	defer func() { _ = apiResp.Body.Close() }()
	if apiResp.StatusCode != http.StatusCreated {
		t.Fatalf("step 1 (API capture): POST /capture = %d, want 201", apiResp.StatusCode)
	}

	// Step 2 — capture via the CLI: R3.1/R3.2's own entrance, `nooma
	// capture` as an HTTP client of the same running server.
	stdout, stderr, err := nooma(t, home, work, "capture", "Call the dentist about my appointment", vault)
	if err != nil {
		t.Fatalf("step 2 (CLI capture): nooma capture: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "stored") {
		t.Fatalf("step 2 (CLI capture): stdout does not say the unit was stored:\n%s", stdout)
	}

	// Step 3 — ask: the umbrella proposal's own demo question, "what do you
	// know about X?", over the standalone /recall route (R2.4, Q3b — no
	// classify call on this path), and it must find the CLI capture, not
	// the API one, proving the query's own content decides the answer.
	askBody, err := json.Marshal(map[string]string{"query": "what do you know about dentist appointments?"})
	if err != nil {
		t.Fatal(err)
	}
	askResp, err := http.Post(base+"/recall", "application/json", bytes.NewReader(askBody))
	if err != nil {
		t.Fatalf("step 3 (ask): POST /recall: %v", err)
	}
	defer func() { _ = askResp.Body.Close() }()
	if askResp.StatusCode != http.StatusOK {
		t.Fatalf("step 3 (ask): POST /recall = %d, want 200", askResp.StatusCode)
	}
	var recalled struct {
		Units []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"units"`
	}
	if err := json.NewDecoder(askResp.Body).Decode(&recalled); err != nil {
		t.Fatalf("step 3 (ask): decoding POST /recall response: %v", err)
	}
	if len(recalled.Units) == 0 || recalled.Units[0].Content != "Dentist appointment" {
		t.Fatalf("step 3 (ask): POST /recall units = %+v, want the dentist capture first", recalled.Units)
	}
	dentistID := recalled.Units[0].ID

	// Step 4 — correct the unit the ask just found: R1.5's explicit
	// referent, the same unit_id field 13b's captureRequest carries — no
	// recall-based referent ambiguity, the property this demo's own
	// correction is chosen to prove is the edit landing, not resolution.
	correctionBody, err := json.Marshal(map[string]string{
		"text":    "no, it's actually the vet, not the dentist",
		"unit_id": dentistID,
	})
	if err != nil {
		t.Fatal(err)
	}
	correctionResp, err := http.Post(base+"/capture", "application/json", bytes.NewReader(correctionBody))
	if err != nil {
		t.Fatalf("step 4 (correction): POST /capture: %v", err)
	}
	defer func() { _ = correctionResp.Body.Close() }()
	if correctionResp.StatusCode != http.StatusOK {
		t.Fatalf("step 4 (correction): POST /capture = %d, want 200", correctionResp.StatusCode)
	}
	var corrected struct {
		Outcome    string `json:"outcome"`
		Correction struct {
			UnitID string   `json:"unit_id"`
			Fields []string `json:"fields"`
		} `json:"correction"`
	}
	if err := json.NewDecoder(correctionResp.Body).Decode(&corrected); err != nil {
		t.Fatalf("step 4 (correction): decoding POST /capture response: %v", err)
	}
	if corrected.Outcome != "corrected" || corrected.Correction.UnitID != dentistID {
		t.Fatalf("step 4 (correction): body = %+v, want outcome=corrected unit_id=%s", corrected, dentistID)
	}

	unitResp, err := http.Get(base + "/units/" + dentistID)
	if err != nil {
		t.Fatalf("step 4 (correction): GET /units/%s: %v", dentistID, err)
	}
	defer func() { _ = unitResp.Body.Close() }()
	var afterEdit struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(unitResp.Body).Decode(&afterEdit); err != nil {
		t.Fatalf("step 4 (correction): decoding GET /units response: %v", err)
	}
	if afterEdit.Content != "Vet appointment" {
		t.Fatalf("step 4 (correction): unit content after edit = %q, want %q", afterEdit.Content, "Vet appointment")
	}
}

// demoProvider stands in for Ollama the way mockOllama (capture_recall_test.go)
// does, over one more request/response pair than that fixed-reply fake needs:
// this demo drives two different captures and one correction through the
// same running server, so the classify response has to depend on which text
// was actually sent, not repeat one canned reply for all three. Routing is
// by substring on the request's own prompt field — classify.BuildPrompt
// embeds the caller's raw text verbatim, so each capture's own words are
// what select its response, the same way a real model would key off content
// rather than off which call number this is.
func demoProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/generate":
			var req struct {
				Prompt string `json:"prompt"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			switch {
			case strings.Contains(req.Prompt, "dry cleaning"):
				_, _ = fmt.Fprint(w, `{"model":"test-model","response":"{\"type\":\"task\",\"normalized_content\":\"Pick up the dry cleaning\",\"weight\":0.6,\"decay_rate\":0.1}","done":true}`)
			case strings.Contains(req.Prompt, "vet"):
				_, _ = fmt.Fprint(w, `{"model":"test-model","response":"{\"type\":\"correction\",\"normalized_content\":\"Vet appointment\",\"weight\":0.7,\"decay_rate\":0.04}","done":true}`)
			case strings.Contains(req.Prompt, "dentist"):
				_, _ = fmt.Fprint(w, `{"model":"test-model","response":"{\"type\":\"task\",\"normalized_content\":\"Dentist appointment\",\"weight\":0.6,\"decay_rate\":0.1}","done":true}`)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		case "/api/embed":
			// Every text embeds to the same fixed vector — the same posture
			// mockOllama already takes. The "ask" step's own answer is
			// decided by the lexical (FTS) leg matching "dentist" in the
			// unit's real content, not by vector distance, so a shared
			// vector across all three texts does not make the two captures
			// indistinguishable to recall.
			_, _ = fmt.Fprint(w, `{"model":"test-model","embeddings":[[0.1,0.2,0.3]]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
