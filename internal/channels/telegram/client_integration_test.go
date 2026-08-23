//go:build integration

package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sentinelToken is a token no real deployment could have, so a test can
// assert it appears nowhere without matching something legitimate.
const sentinelToken = "SENTINEL-TOKEN-DO-NOT-LEAK"

// TestClient_APIRefusalIsDistinguishableFromATransportFailure is R2.2.
//
// A caller deciding whether to retry has to tell "the connection broke"
// from "Telegram will not do this", and a 401 from every other refusal.
// **Without string-matching an error message** — which is the part worth
// asserting, because string-matching is what a caller writes when the
// error taxonomy does not give it anything better.
func TestClient_APIRefusalIsDistinguishableFromATransportFailure(t *testing.T) {
	t.Run("an API refusal carries its code and description", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"chat not found"}`))
		}))
		t.Cleanup(srv.Close)

		_, err := NewClient(srv.URL, sentinelToken, nil).getUpdates(context.Background(), 0)

		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("getUpdates returned %v, want an *APIError", err)
		}
		if apiErr.Code != 400 {
			t.Errorf("Code = %d, want 400", apiErr.Code)
		}
		if apiErr.Description != "chat not found" {
			t.Errorf("Description = %q, want Telegram's own words", apiErr.Description)
		}
		if apiErr.Unauthorized() {
			t.Error("a 400 reports itself Unauthorized — the permanent and transient paths are not distinguishable")
		}
	})

	t.Run("a 401 is the permanent one, told apart by a field and not a string", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
		}))
		t.Cleanup(srv.Close)

		_, err := NewClient(srv.URL, sentinelToken, nil).getUpdates(context.Background(), 0)

		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("getUpdates returned %v, want an *APIError", err)
		}
		if !apiErr.Unauthorized() {
			t.Fatal("a 401 does not report itself Unauthorized — a wrong token would be retried forever, and the channel would look alive while being permanently deaf")
		}
	})

	t.Run("a bare status with no envelope is still classified", func(t *testing.T) {
		// A proxy or gateway in front of Telegram can answer with a status
		// and no envelope at all. A 401 arriving that way is the same
		// permanent failure and must not fall into the transient path.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{}`))
		}))
		t.Cleanup(srv.Close)

		_, err := NewClient(srv.URL, sentinelToken, nil).getUpdates(context.Background(), 0)

		var apiErr *APIError
		if !errors.As(err, &apiErr) || !apiErr.Unauthorized() {
			t.Fatalf("getUpdates returned %v, want an *APIError reporting Unauthorized", err)
		}
	})

	t.Run("a transport failure is not an APIError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		srv.Close() // nothing is listening

		_, err := NewClient(srv.URL, sentinelToken, nil).getUpdates(context.Background(), 0)
		if err == nil {
			t.Fatal("getUpdates against a closed server returned nil")
		}

		var apiErr *APIError
		if errors.As(err, &apiErr) {
			t.Fatalf("a transport failure decoded as an *APIError (%v) — retrying logic cannot tell a broken connection from a refusal", apiErr)
		}
	})

	t.Run("a malformed body is an error, not an empty result", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true,"result":"not an array"}`))
		}))
		t.Cleanup(srv.Close)

		updates, err := NewClient(srv.URL, sentinelToken, nil).getUpdates(context.Background(), 0)
		if err == nil {
			t.Fatalf("getUpdates returned %v and no error for a malformed result — a silent empty batch reads as a quiet channel", updates)
		}
	})
}

// TestClient_TokenNeverAppearsInAnError is R3.3, and it is here rather
// than at L2 because the leak it guards only exists once a real
// *url.Error is involved.
//
// Telegram puts the token in the URL PATH and net/http renders the full
// URL into *url.Error's message. So the obvious wrapper —
// fmt.Errorf("...: %w", err) — writes the bot token into every transport
// error, and from there into the operator's log. This asserts it does not,
// across the three shapes a failure takes.
func TestClient_TokenNeverAppearsInAnError(t *testing.T) {
	t.Run("transport failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		srv.Close()

		_, err := NewClient(srv.URL, sentinelToken, nil).getUpdates(context.Background(), 0)
		assertNoToken(t, err)
	})

	t.Run("API refusal", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"nope"}`))
		}))
		t.Cleanup(srv.Close)

		_, err := NewClient(srv.URL, sentinelToken, nil).getUpdates(context.Background(), 0)
		assertNoToken(t, err)
	})

	t.Run("malformed body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{`))
		}))
		t.Cleanup(srv.Close)

		_, err := NewClient(srv.URL, sentinelToken, nil).getUpdates(context.Background(), 0)
		assertNoToken(t, err)
	})
}

func assertNoToken(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected an error, got nil — this case checks nothing without one")
	}
	if strings.Contains(err.Error(), sentinelToken) {
		t.Fatalf("the bot token appears in the error: %v", err)
	}
	if !strings.Contains(err.Error(), redaction) && strings.Contains(err.Error(), "/bot") {
		t.Errorf("the error names the token's own URL path without redacting it: %v", err)
	}
}

// TestClient_RoundTripsUpdatesAndSends is the happy path, and the only
// place this package's own request shape is asserted.
func TestClient_RoundTripsUpdatesAndSends(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":42,"message":{"message_id":7,"text":"hola","chat":{"id":99}}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, sentinelToken, nil)

	updates, err := c.getUpdates(context.Background(), 41)
	if err != nil {
		t.Fatalf("getUpdates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("getUpdates returned %d updates, want 1", len(updates))
	}
	if updates[0].UpdateID != 42 || updates[0].Message == nil ||
		updates[0].Message.Text != "hola" || updates[0].Message.Chat.ID != 99 {
		t.Fatalf("decoded %+v, want update 42 from chat 99 saying hola", updates[0])
	}
	if !strings.Contains(gotQuery, "offset=41") {
		t.Errorf("query %q carries no offset — every poll would re-read the same updates", gotQuery)
	}
	if !strings.Contains(gotPath, "/bot"+sentinelToken+"/getUpdates") {
		t.Errorf("path %q is not the Bot API's own shape", gotPath)
	}

	if err := c.sendMessage(context.Background(), 99, "noted"); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}
	if !strings.Contains(gotQuery, "chat_id=99") || !strings.Contains(gotQuery, "text=noted") {
		t.Errorf("sendMessage query %q does not carry the chat and the text", gotQuery)
	}
}
