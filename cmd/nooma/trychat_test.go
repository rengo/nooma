package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/store/vaultlock"
)

// tryChatVault builds a vault whose every task is bound to a loopback
// ollama that reads every message as one event on 10 January 2099 at 15:00
// on the host's wall clock. It records each completion request's body, so a
// test can see what the model was shown.
func tryChatVault(t *testing.T) (vault string, prompts func() []string) {
	t.Helper()
	eventAt := time.Date(2099, 1, 10, 15, 0, 0, 0, time.Local)
	var mu sync.Mutex
	var seen []string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/generate":
			mu.Lock()
			seen = append(seen, string(body))
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"model":"m","response":"{\"type\":\"event\",\"normalized_content\":\"Dentist\",\"weight\":0.7,\"decay_rate\":0.05,\"event_at\":\"%s\"}","done":true}`, eventAt.Format(time.RFC3339))
		case "/api/embed":
			_, _ = fmt.Fprint(w, `{"model":"m","embeddings":[[0.1,0.2,0.3]]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(llm.Close)

	vault = writeVault(t, fmt.Sprintf(`providers:
  local:
    type: ollama
    model: m
    endpoint: %s
tasks:
  capture_processing: {provider: local}
  relation_evaluation: {provider: local}
  chat: {provider: local}
  embedding: {provider: local}
channels:
  telegram:
    enabled: true
    bot_token_env: TRY_CHAT_TEST_TOKEN_NEVER_SET
    allowed_chat_ids: [4242]
`, llm.URL))
	return vault, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// TestTryChat_ShowsEachReplyAndReadsLaterMessagesInTheConversation: the
// tool's reason to exist over `nooma capture` is that a follow-up is read in
// the chat it follows, through the channel's own handler, and that the reply
// printed is the one the chat would show. So the second message must reach
// the model with the first one in view, every message gets one reply line,
// and the reminders the run armed are listed with their local fire time.
func TestTryChat_ShowsEachReplyAndReadsLaterMessagesInTheConversation(t *testing.T) {
	vault, prompts := tryChatVault(t)

	var out, errOut bytes.Buffer
	first, second := "dentist on 10 January 2099 at 15:00", "it is at four, actually"
	if err := run([]string{"try-chat", vault, first, second}, &out, &errOut); err != nil {
		t.Fatalf("try-chat: %v\nstderr: %s", err, errOut.String())
	}
	got := out.String()

	for _, msg := range []string{first, second} {
		if !strings.Contains(got, "← "+msg+"\n") {
			t.Errorf("transcript does not show the message %q as sent:\n%s", msg, got)
		}
	}
	if n := strings.Count(got, "\n→ "); n != 2 {
		t.Errorf("transcript has %d reply lines, want one per message:\n%s", n, got)
	}
	if strings.Contains(got, "→ Done.") {
		t.Errorf("a reply fell back to the handler's unrendered placeholder:\n%s", got)
	}
	if want := time.Date(2099, 1, 10, 0, 0, 0, 0, time.Local).Format("2006-01-02"); !strings.Contains(got, want) {
		t.Errorf("transcript lists no reminder on %s in local time:\n%s", want, got)
	}

	inContext := false
	for _, p := range prompts() {
		if strings.Contains(p, second) && strings.Contains(p, first) {
			inContext = true
		}
	}
	if !inContext {
		t.Error("no request for the second message showed the model the first one — the conversation is not wired")
	}
}

// TestTryChat_RefusesAHeldVault: the tool writes captures, so it takes the
// vault lock and refuses a vault a running serve holds, naming the holder.
func TestTryChat_RefusesAHeldVault(t *testing.T) {
	vault, _ := tryChatVault(t)
	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		t.Fatalf("acquiring the vault lock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	var out, errOut bytes.Buffer
	err = run([]string{"try-chat", vault, "hello"}, &out, &errOut)
	var inUse *vaultlock.InUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("try-chat on a held vault = %v, want a vaultlock.InUseError", err)
	}
	if out.Len() != 0 {
		t.Errorf("a refused run wrote a transcript:\n%s", out.String())
	}
}

// TestTryChat_NeedsAnExplicitVaultAndAMessage: unlike serve, the tool never
// falls back to the default vault, so it cannot be pointed at the live one by
// omission; and a run with no message has nothing to try.
func TestTryChat_NeedsAnExplicitVaultAndAMessage(t *testing.T) {
	vault, _ := tryChatVault(t)
	for _, args := range [][]string{{}, {vault}} {
		var out, errOut bytes.Buffer
		if err := run(append([]string{"try-chat"}, args...), &out, &errOut); err == nil {
			t.Errorf("try-chat %v succeeded, want a refusal", args)
		}
	}
}

// TestTryChat_NeverBuildsTheTelegramChannel: the replies go to an in-memory
// channel, never to a transport. trychat.go must not reach the code that
// builds the real one, so nothing it does can be sent to Telegram.
func TestTryChat_NeverBuildsTheTelegramChannel(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "trychat.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing trychat.go: %v", err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if n.Name == "wireChannel" {
				t.Error("trychat.go references wireChannel, which builds the real Telegram channel")
			}
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok && x.Name == "telegram" && n.Sel.Name != "Name" {
				t.Errorf("trychat.go uses telegram.%s; only the channel's name may be borrowed", n.Sel.Name)
			}
		}
		return true
	})
}
