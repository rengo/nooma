//go:build integration

package channels_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/channels/telegram"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/ports"
)

func newChannel(t *testing.T, baseURL string, log *bytes.Buffer) ports.Channel {
	t.Helper()

	ch, err := telegram.New(
		config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN", AllowedChatIDs: []int64{7}},
		func(string) (string, bool) { return "token", true },
		nil, baseURL, log,
	)
	if err != nil {
		t.Fatalf("telegram.New: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

// TestRunner_ShutdownInterruptsAnInFlightPoll is R4.3, and it is here
// rather than at L2 because L2 cannot make the claim.
//
// The fake channel's Receive blocks on ctx by construction, so cancelling
// it is guaranteed to work — the L2 test proves the loop reacts, not that
// the transport does. **What this proves is that cancellation reaches a
// real HTTP request in flight**, which is a property of building it with
// http.NewRequestWithContext and of nothing else. Swap that for
// http.NewRequest and this test waits out the poll timeout while the L2
// one still passes.
//
// The server never answers, which is exactly what a quiet long poll looks
// like from the client's side.
func TestRunner_ShutdownInterruptsAnInFlightPoll(t *testing.T) {
	held := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(held)
		<-r.Context().Done() // hold the connection like a real long poll
	}))
	t.Cleanup(srv.Close)

	var log bytes.Buffer
	r := channels.NewRunner(newChannel(t, srv.URL, &log), func(context.Context, ports.ChannelMessage) error {
		return nil
	}, &log)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case <-held:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("the poll never reached the server")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v on cancellation, want nil — shutdown is not a failure", err)
		}
	case <-time.After(5 * time.Second):
		// Well under the 30s poll timeout: if cancellation did not reach
		// the request, this is where that shows.
		t.Fatal("Run did not return within 5s of cancellation, far inside the poll timeout — cancellation is not reaching the in-flight request")
	}
}

// TestRunner_APoisonedCaptureIsRedeliveredAndThenSticks is R4.1 end to
// end: a handler that fails leaves the update unconfirmed, the transport
// sends it again, and the second attempt — which succeeds — is the one
// that confirms.
//
// The offsets the server actually received are the assertion. A cursor
// that advanced past the failed capture would have lost it.
func TestRunner_APoisonedCaptureIsRedeliveredAndThenSticks(t *testing.T) {
	var mu sync.Mutex
	var offsets []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
			return
		}
		mu.Lock()
		offsets = append(offsets, r.URL.Query().Get("offset"))
		n := len(offsets)
		mu.Unlock()

		if n <= 2 {
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":10,"message":{"message_id":1,"text":"hola","chat":{"id":7}}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	t.Cleanup(srv.Close)

	var log bytes.Buffer
	var attempts int
	r := channels.NewRunner(newChannel(t, srv.URL, &log), func(_ context.Context, m ports.ChannelMessage) error {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			return errors.New("the vault is closed")
		}
		return nil
	}, &log)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = r.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if attempts < 2 {
		t.Fatalf("the handler ran %d time(s), want at least 2 — a failed capture must be redelivered", attempts)
	}
	if len(offsets) < 2 {
		t.Fatalf("the server saw %d poll(s), want at least 2", len(offsets))
	}
	if offsets[1] != "10" {
		t.Fatalf("the second poll asked for offset %q, want 10 — a cursor that advanced past a failed capture would have lost it", offsets[1])
	}
	if !strings.Contains(log.String(), "not confirmed") {
		t.Errorf("the log does not record that the message was left unconfirmed:\n%s", log.String())
	}
}
