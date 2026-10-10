package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

type failingLLM struct{ err error }

func (f failingLLM) Complete(context.Context, ports.LLMRequest) (ports.LLMResponse, error) {
	return ports.LLMResponse{}, f.err
}

func newFailingCaptureService(t *testing.T, err error) *brain.CaptureService {
	t.Helper()
	embeddings := memrepo.NewEmbeddings()
	idx, loadErr := embeddings.LoadIndex(context.Background(), embedFakeModel)
	if loadErr != nil {
		t.Fatalf("LoadIndex: %v", loadErr)
	}
	llm := failingLLM{err}
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	return brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(), embeddings, memrepo.NewLexical(), memrepo.NewRelations(), memrepo.NewDecisionLog(), llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)
}

// captureLogs routes slog to a buffer for one test. It swaps the process
// default, so a test that calls it must not be parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func decodeErrorBody(t *testing.T, raw []byte) errorBody {
	t.Helper()
	var b errorBody
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return b
}

// TestCaptureHandler_FailuresAreStatusCodeAndMessageNotABare500 is the
// plain-errors contract on POST /capture: a provider or model failure keeps
// a meaningful status, carries a stable code and a sentence a person can act
// on, and leaves exactly one log line — class, provider, path — that holds
// neither a secret nor the captured text.
func TestCaptureHandler_FailuresAreStatusCodeAndMessageNotABare500(t *testing.T) {
	const text = "buy lilies for Marta"
	tests := []struct {
		name       string
		svc        func(*testing.T) *brain.CaptureService
		wantStatus int
		wantCode   string
		wantInMsg  string
		wantLog    string
	}{
		{"missing key", func(t *testing.T) *brain.CaptureService {
			return newFailingCaptureService(t, ports.KeyMissing("openai", "OPENAI_API_KEY"))
		}, http.StatusServiceUnavailable, "provider_key_missing", "OPENAI_API_KEY", "provider=openai"},
		{"rejected key", func(t *testing.T) *brain.CaptureService {
			return newFailingCaptureService(t, ports.StatusFailure("openai", 401))
		}, http.StatusBadGateway, "provider_key_rejected", "rejected", "provider=openai"},
		{"timeout", func(t *testing.T) *brain.CaptureService {
			return newFailingCaptureService(t, ports.TransportFailure("anthropic", context.DeadlineExceeded))
		}, http.StatusGatewayTimeout, "provider_timeout", "too long", "provider=anthropic"},
		{"a caller that hung up", func(t *testing.T) *brain.CaptureService {
			return newFailingCaptureService(t, ports.TransportFailure("openai", context.Canceled))
		}, 499, "canceled", "canceled", "provider=openai"},
		{"undecodable answer", func(t *testing.T) *brain.CaptureService {
			return newTestCaptureService(t, time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC), "classify-empty-response")
		}, http.StatusBadGateway, "model_output_unusable", "nothing was saved", "class=model_output_unusable"},
		{"anything else", func(t *testing.T) *brain.CaptureService {
			return newFailingCaptureService(t, errors.New("disk is on fire"))
		}, http.StatusInternalServerError, "internal", "capture failed", "class=internal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			h := Handler(Deps{Version: "test", Capture: tt.svc(t)})

			rec := postCapture(t, h, `{"text":"`+text+`"}`)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			body := decodeErrorBody(t, rec.Body.Bytes())
			if body.Code != tt.wantCode || !strings.Contains(body.Error, tt.wantInMsg) {
				t.Errorf("body = %+v, want code %q and a message containing %q", body, tt.wantCode, tt.wantInMsg)
			}
			line := logs.String()
			if strings.Count(line, "\n") != 1 || !strings.Contains(line, tt.wantLog) || !strings.Contains(line, "path=/capture") || !strings.Contains(line, "class="+tt.wantCode) {
				t.Errorf("want exactly one log line with class, provider and path, got %q", line)
			}
			if strings.Contains(line, text) || strings.Contains(rec.Body.String(), text) {
				t.Errorf("the captured text leaked (log %q, body %s)", line, rec.Body)
			}
		})
	}
}

// Every error the JSON API answers carries a code, so a client never has to
// match on a sentence.
func TestErrorResponsesAllCarryACode(t *testing.T) {
	h := Handler(Deps{Version: "test"})
	svc := newTestCaptureService(t, time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC))
	wired := Handler(Deps{Version: "test", Capture: svc})
	tests := []struct {
		name string
		rec  func() (int, []byte)
		want string
	}{
		{"not wired", func() (int, []byte) { r := postCapture(t, h, `{"text":"x"}`); return r.Code, r.Body.Bytes() }, "not_wired"},
		{"bad json", func() (int, []byte) { r := postCapture(t, wired, `{`); return r.Code, r.Body.Bytes() }, "invalid_request"},
		{"no text", func() (int, []byte) { r := postCapture(t, wired, `{"text":""}`); return r.Code, r.Body.Bytes() }, "invalid_request"},
		{"unknown unit", func() (int, []byte) {
			r := postCapture(t, wired, `{"text":"x","unit_id":"nope"}`)
			return r.Code, r.Body.Bytes()
		}, "unit_not_found"},
	}
	for _, tt := range tests {
		code, raw := tt.rec()
		if code < 400 {
			t.Fatalf("%s: status %d is not an error", tt.name, code)
		}
		if got := decodeErrorBody(t, raw).Code; got != tt.want {
			t.Errorf("%s: code = %q, want %q", tt.name, got, tt.want)
		}
	}
}
