package telegram

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/config"
)

// envLookup builds the lookup a test injects instead of the process
// environment, so no test sets or reads a real variable.
func envLookup(pairs map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := pairs[name]
		return v, ok
	}
}

// TestNew_RefusesEveryUnsafeConfiguration is R3.2 and R3.3, and it exists
// because internal/config's validator already refuses all three.
//
// That is not redundancy. The validator is a check a caller performs; this
// is a rule the channel cannot be made to break. CLAUDE.md non-negotiable
// #7 — "safe defaults are structural, not warnings" — is the difference
// between the two, and a caller that skipped validation is exactly the
// case the second refusal exists for.
func TestNew_RefusesEveryUnsafeConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cfg   config.Telegram
		env   map[string]string
		wants string
	}{
		{
			name:  "enabled with no allowed chat ids",
			cfg:   config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN"},
			env:   map[string]string{"TG_TOKEN": "t"},
			wants: "allowed_chat_ids",
		},
		{
			name:  "enabled with no token variable named",
			cfg:   config.Telegram{Enabled: true, AllowedChatIDs: []int64{7}},
			wants: "bot_token_env",
		},
		{
			name:  "enabled with a token variable that is not set",
			cfg:   config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN", AllowedChatIDs: []int64{7}},
			env:   map[string]string{},
			wants: "TG_TOKEN",
		},
		{
			name:  "enabled with a token variable that is set but empty",
			cfg:   config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN", AllowedChatIDs: []int64{7}},
			env:   map[string]string{"TG_TOKEN": ""},
			wants: "TG_TOKEN",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch, err := New(tc.cfg, envLookup(tc.env), nil, "http://example.invalid", &bytes.Buffer{})
			if err == nil {
				_ = ch.Close()
				t.Fatal("New succeeded — a channel that starts without its guard is the failure non-negotiable #7 exists to prevent")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error %q does not name %q, so a reader cannot tell what to fix", err, tc.wants)
			}
		})
	}
}

// TestNew_AcceptsAValidConfiguration keeps the refusals above from passing
// vacuously: a constructor that refused everything would satisfy all four.
func TestNew_AcceptsAValidConfiguration(t *testing.T) {
	ch, err := New(
		config.Telegram{Enabled: true, BotTokenEnv: "TG_TOKEN", AllowedChatIDs: []int64{7}},
		envLookup(map[string]string{"TG_TOKEN": "t"}),
		nil, "http://example.invalid", &bytes.Buffer{},
	)
	if err != nil {
		t.Fatalf("New on a valid configuration: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })

	if ch.Name() == "" {
		t.Error("Name() is empty — it becomes units.source, which is NOT NULL")
	}
}

// TestNew_RefusesWhenDisabled: constructing a channel that configuration
// says is off is a caller error, not a silent no-op channel. A no-op would
// poll nothing forever and look healthy.
func TestNew_RefusesWhenDisabled(t *testing.T) {
	_, err := New(config.Telegram{Enabled: false}, envLookup(nil), nil, "", &bytes.Buffer{})
	if err == nil {
		t.Fatal("New succeeded for a disabled channel — the caller decides whether to construct one, and a no-op channel would look healthy while doing nothing")
	}
}
