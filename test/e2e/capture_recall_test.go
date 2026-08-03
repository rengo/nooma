//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
// This is deliberately NOT yet R2.8's full demo shape ("posts a capture,
// and issues a recall that finds it") — that needs real wiring this link
// does not carry. 13d's own task text names this test as what it extends:
// "extend 13c.5's L4 test so it exercises serve.go's real wiring rather
// than a handler test's manually-built Deps."
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
