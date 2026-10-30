package config

import (
	"strings"
	"testing"
)

// TestDisableTelegram_TurnsItOffChangingNothingElse: an imported copy must not
// poll the same bot as the original. The value `true` becomes `false` in the
// text itself, so every comment, blank line and spacing stays where it was,
// and the result decodes strictly with the channel off.
func TestDisableTelegram_TurnsItOffChangingNothingElse(t *testing.T) {
	cases := map[string]struct{ in, out string }{
		"block style, comments around": {
			"# mi bóveda ñ\nserver:\n  bind: 127.0.0.1   # loopback\n\nchannels:\n  telegram:\n    enabled: true    # the real bot\n    bot_token_env: TELEGRAM_BOT_TOKEN\n    allowed_chat_ids: [123, 456]\n# end\n",
			"# mi bóveda ñ\nserver:\n  bind: 127.0.0.1   # loopback\n\nchannels:\n  telegram:\n    enabled: false    # the real bot\n    bot_token_env: TELEGRAM_BOT_TOKEN\n    allowed_chat_ids: [123, 456]\n# end\n",
		},
		"flow style, a multi-byte value before it": {
			"channels: {telegram: {bot_token_env: \"TOKEN_Ñ\", enabled: true}}\n",
			"channels: {telegram: {bot_token_env: \"TOKEN_Ñ\", enabled: false}}\n",
		},
		"Windows line endings, capitalised": {
			"channels:\r\n  telegram:\r\n    enabled: True\r\n",
			"channels:\r\n  telegram:\r\n    enabled: false\r\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, how, err := DisableTelegram([]byte(c.in))
			if err != nil {
				t.Fatalf("DisableTelegram: %v", err)
			}
			if how != TelegramTurnedOff {
				t.Errorf("how = %v, want TelegramTurnedOff", how)
			}
			if string(got) != c.out {
				t.Errorf("DisableTelegram =\n%q\nwant\n%q", got, c.out)
			}
			cfg, err := Decode(strings.NewReader(string(got)))
			if err != nil || cfg.Channels.Telegram.Enabled {
				t.Errorf("the result decodes to %+v, %v; want the channel off, strictly decoded", cfg, err)
			}
		})
	}
}

// TestDisableTelegram_LeavesAFileWithTheChannelOffAlone: off already, or
// never configured, the file is returned byte for byte.
func TestDisableTelegram_LeavesAFileWithTheChannelOffAlone(t *testing.T) {
	for name, doc := range map[string]string{
		"off":        "channels:\n  telegram:\n    enabled: false # spare\n",
		"no enabled": "channels:\n  telegram:\n    bot_token_env: TELEGRAM_BOT_TOKEN\n",
		"no channel": "server:\n  bind: 127.0.0.1\n",
		"empty":      "",
	} {
		got, how, err := DisableTelegram([]byte(doc))
		if err != nil || how != TelegramAlreadyOff || string(got) != doc {
			t.Errorf("%s: DisableTelegram = %q, %v, %v; want the file untouched", name, got, how, err)
		}
	}
}

// TestDisableTelegram_RefusesAFileThatDoesNotLoad: an imported nooma.yml is
// checked as strictly as one nooma loads.
func TestDisableTelegram_RefusesAFileThatDoesNotLoad(t *testing.T) {
	if _, _, err := DisableTelegram([]byte("api_key: sk-secret\n")); err == nil {
		t.Error("DisableTelegram accepted a file with an unknown key")
	}
}

// TestReplaceToken_ChangesNothingWhereTheValueIsNot: the text is edited only
// where the bytes are exactly the value, so a wrong position falls back to
// a rewrite instead of corrupting the file.
func TestReplaceToken_ChangesNothingWhereTheValueIsNot(t *testing.T) {
	doc := []byte("channels:\n  telegram:\n    enabled: true\n")
	if out, ok := replaceToken(doc, 3, 14, "true", "false"); !ok || string(out) != "channels:\n  telegram:\n    enabled: false\n" {
		t.Errorf("replaceToken at the value = %q, %v", out, ok)
	}
	for _, at := range [][2]int{{3, 5}, {2, 14}, {9, 1}, {3, 99}} {
		if _, ok := replaceToken(doc, at[0], at[1], "true", "false"); ok {
			t.Errorf("replaceToken at line %d column %d replaced text that is not the value", at[0], at[1])
		}
	}
}
