//go:build integration

package telegram

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// updatesServer answers getUpdates with the scripted batches, one per call,
// and records every sendMessage.
type updatesServer struct {
	batches [][]string
	call    int
	sent    []repocontract.SentMessage
	offsets []string
}

func newUpdatesServer(t *testing.T, batches ...[]string) (*httptest.Server, *updatesServer) {
	t.Helper()

	state := &updatesServer{batches: batches}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			state.sent = append(state.sent, repocontract.SentMessage{
				Conversation: ports.ConversationID(r.URL.Query().Get("chat_id")),
				Text:         r.URL.Query().Get("text"),
			})
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
			return
		}

		state.offsets = append(state.offsets, r.URL.Query().Get("offset"))
		body := `{"ok":true,"result":[]}`
		if state.call < len(state.batches) {
			body = `{"ok":true,"result":[` + strings.Join(state.batches[state.call], ",") + `]}`
		}
		state.call++
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// updateJSON renders one Telegram update as the API would send it. Named
// for the JSON rather than for the type, because `update` is this
// package's own decoded struct.
func updateJSON(updateID, chatID int64, text string) string {
	return `{"update_id":` + strconv.FormatInt(updateID, 10) +
		`,"message":{"message_id":1,"text":"` + text +
		`","chat":{"id":` + strconv.FormatInt(chatID, 10) + `}}}`
}

func newTestChannel(t *testing.T, baseURL string, allowed []int64, log *bytes.Buffer) *Channel {
	t.Helper()

	ch, err := New(
		config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN", AllowedChatIDs: allowed},
		func(string) (string, bool) { return sentinelToken, true },
		nil, baseURL, log,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

// TestChannel_AdmitsTheAllowListAndRefusesTheRest is R3.1, and both halves
// are asserted in one test because either alone is satisfied by a channel
// that admits everything or nothing.
func TestChannel_AdmitsTheAllowListAndRefusesTheRest(t *testing.T) {
	var log bytes.Buffer
	srv, _ := newUpdatesServer(t, []string{
		updateJSON(10, 999, "from a stranger"),
		updateJSON(11, 7, "from the owner"),
	})
	ch := newTestChannel(t, srv.URL, []int64{7}, &log)

	msgs, err := ch.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}

	if len(msgs) != 1 {
		t.Fatalf("Receive returned %d message(s), want only the allowed one: %+v", len(msgs), msgs)
	}
	if msgs[0].Text != "from the owner" {
		t.Errorf("Text = %q, want the allowed chat's message", msgs[0].Text)
	}
	if string(msgs[0].Conversation) != "7" {
		t.Errorf("Conversation = %q, want %q", msgs[0].Conversation, "7")
	}

	logged := log.String()
	if !strings.Contains(logged, "999") {
		t.Errorf("the log does not name the refused chat id — a refusal nobody can notice is a refusal nobody can act on:\n%s", logged)
	}
	if strings.Contains(logged, "from a stranger") {
		t.Errorf("the log carries the refused message's TEXT:\n%s\n\nthat turns an access refusal into an injection surface for whoever finds the bot", logged)
	}
}

// TestChannel_TokenNeverReachesTheLog is R3.3's log half. The error half
// lives in client_integration_test.go; this one exists because the channel
// is the first thing in this package that writes anywhere.
func TestChannel_TokenNeverReachesTheLog(t *testing.T) {
	var log bytes.Buffer
	srv, _ := newUpdatesServer(t, []string{updateJSON(10, 999, "refused")})
	ch := newTestChannel(t, srv.URL, []int64{7}, &log)

	if _, err := ch.Receive(context.Background()); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	// And a failing poll, whose error is the one that carries a URL.
	srv.Close()
	if _, err := ch.Receive(context.Background()); err == nil {
		t.Fatal("Receive against a closed server returned nil")
	}

	if strings.Contains(log.String(), sentinelToken) {
		t.Fatalf("the bot token reached the log:\n%s", log.String())
	}
}

// TestChannel_CursorDoesNotPassAnUnconfirmedMessage is R4.1's arithmetic,
// asserted here because PR 4 builds its loop on top of it.
//
// The offsets the server actually received are the assertion. A channel
// that advanced past an unconfirmed message would ask for a higher one,
// and the update whose capture had not finished would be gone.
func TestChannel_CursorDoesNotPassAnUnconfirmedMessage(t *testing.T) {
	srv, state := newUpdatesServer(t,
		[]string{updateJSON(10, 7, "first")},
		[]string{},
	)
	ch := newTestChannel(t, srv.URL, []int64{7}, &bytes.Buffer{})
	ctx := context.Background()

	msgs, err := ch.Receive(ctx)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("first Receive: %d message(s), %v", len(msgs), err)
	}

	// No Confirm: the next poll must not acknowledge update 10.
	if _, err := ch.Receive(ctx); err != nil {
		t.Fatalf("second Receive: %v", err)
	}
	if len(state.offsets) < 2 || state.offsets[1] != "10" {
		t.Fatalf("offsets asked for were %v, want the second to be 10 — an unconfirmed message must not be acknowledged", state.offsets)
	}

	// After Confirm, the cursor moves past it.
	if err := ch.Confirm(ctx, msgs[0].ID); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if _, err := ch.Receive(ctx); err != nil {
		t.Fatalf("third Receive: %v", err)
	}
	if state.offsets[2] != "11" {
		t.Fatalf("offsets asked for were %v, want the third to be 11 — a confirmed message must not be delivered again", state.offsets)
	}
}

// TestChannel_RefusedMessageDoesNotHoldTheCursor: a refused update has no
// capture to lose, so it must not pin the cursor the way an admitted one
// does. Otherwise a single message from a stranger would stall the channel
// forever.
func TestChannel_RefusedMessageDoesNotHoldTheCursor(t *testing.T) {
	srv, state := newUpdatesServer(t,
		[]string{updateJSON(10, 999, "stranger")},
		[]string{},
	)
	ch := newTestChannel(t, srv.URL, []int64{7}, &bytes.Buffer{})
	ctx := context.Background()

	if msgs, err := ch.Receive(ctx); err != nil || len(msgs) != 0 {
		t.Fatalf("first Receive: %d message(s), %v", len(msgs), err)
	}
	if _, err := ch.Receive(ctx); err != nil {
		t.Fatalf("second Receive: %v", err)
	}

	if state.offsets[1] != "11" {
		t.Fatalf("offsets asked for were %v, want the second to be 11 — a refused message pinning the cursor would stall the channel on one stranger", state.offsets)
	}
}
