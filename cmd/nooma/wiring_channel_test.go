package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/rengo/nooma/internal/config"
)

// TestWireChannel_ReturnsNothingWhenTelegramIsDisabled: the zero case is a
// nil Channel, not an error and not a channel that polls nothing. A caller
// should not have to branch on configuration to find out whether there is
// a channel.
func TestWireChannel_ReturnsNothingWhenTelegramIsDisabled(t *testing.T) {
	cfg := &config.Config{}

	ch, err := wireChannel(cfg, func(string) (string, bool) { return "", false }, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("wireChannel with Telegram disabled: %v", err)
	}
	if ch != nil {
		t.Fatal("wireChannel returned a channel for a disabled configuration")
	}
}

// TestWireChannel_BuildsAConfiguredChannel keeps the case above from
// passing vacuously.
func TestWireChannel_BuildsAConfiguredChannel(t *testing.T) {
	cfg := &config.Config{}
	cfg.Channels.Telegram = config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN", AllowedChatIDs: []int64{7}}

	ch, err := wireChannel(cfg, func(string) (string, bool) { return "t", true }, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("wireChannel: %v", err)
	}
	if ch == nil {
		t.Fatal("wireChannel returned nothing for an enabled configuration")
	}
	t.Cleanup(func() { _ = ch.Close() })
}

// TestWireChannel_PropagatesAnUnsafeConfiguration: the channel's own
// refusals must reach the caller rather than becoming a nil channel that
// looks like "Telegram is off".
func TestWireChannel_PropagatesAnUnsafeConfiguration(t *testing.T) {
	cfg := &config.Config{}
	cfg.Channels.Telegram = config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN"} // no allow-list

	if _, err := wireChannel(cfg, func(string) (string, bool) { return "t", true }, &bytes.Buffer{}); err == nil {
		t.Fatal("wireChannel accepted a configuration with no allowed_chat_ids — a silent nil here reads as 'Telegram is off' and the guard disappears")
	}
}

// TestRunServeDoesNotStartTheChannel is R6.2, and finding H2's own
// assertion.
//
// This PR ships a constructor with no production caller, deliberately:
// starting a poller before there is anything to deliver would make every
// later PR in M3 run against a live channel that says nothing. m3d wires
// it into runServe alongside the proactive_check tick.
//
// The absence is asserted rather than remembered, because "we will wire it
// later" is not a property anything checks. It parses rather than greps so
// this test's own doc comment, which names the function, does not satisfy
// or trip it.
func TestRunServeDoesNotStartTheChannel(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "serve.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing serve.go: %v", err)
	}

	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if ok && ident.Name == "wireChannel" {
			found = true
		}
		return true
	})

	if found {
		t.Fatal("serve.go references wireChannel — the channel is constructed but not started until m3d gives it something to deliver")
	}
}
