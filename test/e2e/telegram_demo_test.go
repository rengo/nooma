//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/channels/telegram"
	"github.com/rengo/nooma/internal/config"
)

// fakeTelegram is a Bot API server holding a scripted batch and recording
// every reply.
type fakeTelegram struct {
	mu      sync.Mutex
	batch   string
	served  bool
	replies []string
}

func (f *fakeTelegram) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			f.replies = append(f.replies, r.URL.Query().Get("text"))
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
			return
		}
		if !f.served {
			f.served = true
			_, _ = w.Write([]byte(`{"ok":true,"result":[` + f.batch + `]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}
}

func (f *fakeTelegram) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.replies))
	copy(out, f.replies)
	return out
}

// capturedInput records what reached the brain, without needing one.
type capturedInput struct {
	mu   sync.Mutex
	seen []brain.CaptureInput
}

func (c *capturedInput) Capture(_ context.Context, in brain.CaptureInput) (brain.CaptureResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, in)
	return brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "u-" + strconv.Itoa(len(c.seen))}, nil
}

func (c *capturedInput) captures() []brain.CaptureInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]brain.CaptureInput, len(c.seen))
	copy(out, c.seen)
	return out
}

// TestTelegramDemo_AnAllowedMessageIsCapturedAndAnsweredAndAStrangerIsNot
// is m3c's own exit criterion.
//
// Both fixtures are required together, and they are each other's control:
// with only the allowed message the refusal half is unfalsifiable, with
// only the stranger the capture half is, and a channel that admitted
// everything or nothing would satisfy exactly one of them.
func TestTelegramDemo_AnAllowedMessageIsCapturedAndAnsweredAndAStrangerIsNot(t *testing.T) {
	fake := &fakeTelegram{batch: strings.Join([]string{
		`{"update_id":10,"message":{"message_id":1,"text":"remind me to water the plants","chat":{"id":999}}}`,
		`{"update_id":11,"message":{"message_id":2,"text":"pick up the dry cleaning","chat":{"id":7}}}`,
	}, ",")}

	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	var log bytes.Buffer
	ch, err := telegram.New(
		config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN", AllowedChatIDs: []int64{7}},
		func(string) (string, bool) { return "bot-token", true },
		nil, srv.URL, &log,
	)
	if err != nil {
		t.Fatalf("telegram.New: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })

	capturer := &capturedInput{}
	runner := channels.NewRunner(ch, channels.CaptureHandler(capturer, ch, &log), &log)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run: %v", err)
	}

	// The allowed message became a capture, verbatim, with the channel's
	// own name as its provenance.
	captures := capturer.captures()
	if len(captures) != 1 {
		t.Fatalf("the brain saw %d capture(s) %+v, want exactly the allowed one", len(captures), captures)
	}
	if captures[0].Text != "pick up the dry cleaning" {
		t.Errorf("Text = %q, want the allowed message verbatim", captures[0].Text)
	}
	if captures[0].Channel != telegram.Name {
		t.Errorf("Channel = %q, want %q — this is what becomes units.source", captures[0].Channel, telegram.Name)
	}

	// It got an answer.
	replies := fake.sent()
	if len(replies) != 1 {
		t.Fatalf("the channel sent %d repl(ies) %v, want exactly one", len(replies), replies)
	}
	if strings.TrimSpace(replies[0]) == "" {
		t.Error("the reply is empty — a person cannot tell that from being ignored")
	}

	// The stranger became nothing, audibly.
	logged := log.String()
	if !strings.Contains(logged, "999") {
		t.Errorf("the log does not name the refused chat id:\n%s", logged)
	}
	if strings.Contains(logged, "water the plants") {
		t.Errorf("the log carries the refused message's TEXT:\n%s\n\nthat turns an access refusal into an injection surface for whoever finds the bot", logged)
	}
	if strings.Contains(strings.Join(replies, " "), "water the plants") {
		t.Error("the stranger got a reply")
	}
}

// TestTelegramDemo_TheBotTokenReachesNoLogAndNoReply is the credential
// boundary at the outermost layer this change has.
func TestTelegramDemo_TheBotTokenReachesNoLogAndNoReply(t *testing.T) {
	const token = "E2E-SENTINEL-TOKEN"

	fake := &fakeTelegram{batch: `{"update_id":10,"message":{"message_id":1,"text":"hola","chat":{"id":7}}}`}
	srv := httptest.NewServer(fake.handler())

	var log bytes.Buffer
	ch, err := telegram.New(
		config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN", AllowedChatIDs: []int64{7}},
		func(string) (string, bool) { return token, true },
		nil, srv.URL, &log,
	)
	if err != nil {
		t.Fatalf("telegram.New: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })

	capturer := &capturedInput{}
	runner := channels.NewRunner(ch, channels.CaptureHandler(capturer, ch, &log), &log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(300 * time.Millisecond)
		srv.Close() // force the failure path, where the URL is in the error
	}()
	_ = runner.Run(ctx)

	if strings.Contains(log.String(), token) {
		t.Fatalf("the bot token reached the log:\n%s", log.String())
	}
	for _, reply := range fake.sent() {
		if strings.Contains(reply, token) {
			t.Fatalf("the bot token reached a reply: %q", reply)
		}
	}
}

// TestTelegramDemo_ShipsNoInboundListener is ADR-0014, made structural at
// the layer that could break it: the channel opens outbound connections
// and nothing else.
//
// A webhook transport would need an http.Server, a route or a listener
// inside internal/channels. There is none, and this is what says so — the
// ADR's claim is that Nooma needs no inbound port to be fully functional,
// and a claim about the whole binary deserves a check somewhere.
func TestTelegramDemo_ShipsNoInboundListener(t *testing.T) {
	root := repoRootForCheckDemo(t)
	forbidden := []string{"http.ListenAndServe", "http.Server{", "net.Listen"}

	scanned := 0
	err := filepath.WalkDir(filepath.Join(root, "internal", "channels"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Test files legitimately stand up httptest servers — that is how
		// every test here avoids the network. The claim is about the
		// shipped adapter.
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		rel, _ := filepath.Rel(root, path)
		for _, marker := range forbidden {
			if strings.Contains(string(body), marker) {
				t.Errorf("%s contains %q — ADR-0014 is long polling only, and a listener here is the webhook transport arriving by the back door", rel, marker)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning internal/channels: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned zero files — nothing was checked")
	}
}
