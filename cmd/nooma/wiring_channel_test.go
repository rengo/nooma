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

// TestRunServeStartsTheChannelAndJoinsItFirst replaces m3c's
// TestRunServeDoesNotStartTheChannel, and the replacement is the point.
//
// That test asserted an ABSENCE: m3c shipped wireChannel with no
// production caller on purpose, because starting a poller with nothing to
// deliver would have made every later PR run against a live channel that
// said nothing. It failed the moment this PR wired it in — which is
// exactly what it was written to do, and why it is replaced here rather
// than deleted quietly.
//
// What replaces it asserts the shape m3d actually promises: serve starts
// the channel, and joins the poller BEFORE the server and the scheduler,
// because the poller is the only thing that accepts new work.
func TestRunServeStartsTheChannelAndJoinsItFirst(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "serve.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing serve.go: %v", err)
	}

	var wiresChannel, joinsPoller bool
	ast.Inspect(file, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		switch ident.Name {
		case "wireChannel":
			wiresChannel = true
		case "pollerDone":
			joinsPoller = true
		}
		return true
	})

	if !wiresChannel {
		t.Error("serve.go does not reference wireChannel — m3d is the change that turns the channel on")
	}
	if !joinsPoller {
		t.Error("serve.go does not join the poller at shutdown — a poller left running past the vault's close accepts work nothing can persist")
	}
}

// TestWireTranscriber_HearsOnlyThroughAProviderThatCanTranscribe: unbound,
// voice is off with nothing said; an openai provider with its key hears; an
// ollama binding or a missing key is off, and serve says why (ADR-0062).
func TestWireTranscriber_HearsOnlyThroughAProviderThatCanTranscribe(t *testing.T) {
	providers := map[string]config.Provider{
		"cloud": {Type: "openai", APIKeyEnv: "OPENAI_API_KEY", Model: "gpt-4o-mini"},
		"local": {Type: "ollama", Model: "llama3"},
	}
	cases := []struct {
		name, bound string
		key, hears  bool
	}{
		{"unbound", "", true, false},
		{"openai with its key", "cloud", true, true},
		{"openai without its key", "cloud", false, false},
		{"ollama", "local", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Providers: providers, Tasks: map[string]config.TaskBinding{}}
			if tc.bound != "" {
				cfg.Tasks["audio_transcription"] = config.TaskBinding{Provider: tc.bound}
			}
			var log bytes.Buffer
			lookup := func(string) (string, bool) { return "", false }
			if tc.key {
				lookup = func(string) (string, bool) { return "sk", true }
			}
			got := wireTranscriber(cfg, lookup, &log)
			if (got != nil) != tc.hears {
				t.Errorf("transcriber = %v, want hearing %v", got, tc.hears)
			}
			if said := log.Len() > 0; said != (tc.bound != "" && !tc.hears) {
				t.Errorf("log %q: a binding that cannot hear is named, and only then", log.String())
			}
		})
	}
}
