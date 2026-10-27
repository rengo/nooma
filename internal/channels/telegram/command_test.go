package telegram

import (
	"testing"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/phrase"
)

// TestCommandReply answers a bot command without the model: /start was read
// by classify as out_of_scope, with no message to take a language from, and
// the first thing the bot ever said was "That is not something I can do.".
// /start greets, any other command says it is not one Nooma knows, both in
// the sender's Telegram language or Spanish when it has none Nooma speaks.
// Text that only starts with a slash, with no command entity at its start,
// is a message like any other.
func TestCommandReply(t *testing.T) {
	es, en := phrase.For(classify.LanguageES), phrase.For(classify.LanguageEN)
	cmd := []entity{{Type: "bot_command", Offset: 0, Length: 6}}
	for _, tc := range []struct {
		name     string
		text     string
		entities []entity
		lang     string
		want     string
		ok       bool
	}{
		{"start in Spanish", "/start", cmd, "es", es.Greeting, true},
		{"start in a regional Spanish", "/start", cmd, "es-AR", es.Greeting, true},
		{"start in English", "/start", cmd, "en", en.Greeting, true},
		{"start in English, regional", "/start", cmd, "en-GB", en.Greeting, true},
		{"start with no language", "/start", cmd, "", es.Greeting, true},
		{"start in a language Nooma does not speak", "/start", cmd, "pt-BR", es.Greeting, true},
		{"start addressed to the bot", "/start@nooma_bot", []entity{{Type: "bot_command", Length: 16}}, "en", en.Greeting, true},
		{"start with a payload", "/start hola", cmd, "es", es.Greeting, true},
		{"an unknown command", "/ayuda", []entity{{Type: "bot_command", Length: 6}}, "es", es.UnknownCommand, true},
		{"an unknown command in English", "/help", []entity{{Type: "bot_command", Length: 5}}, "en", en.UnknownCommand, true},
		{"a slash with no command entity", "/ no es un comando", nil, "es", "", false},
		{"a command later in the text", "mirá /start", []entity{{Type: "bot_command", Offset: 5, Length: 6}}, "es", "", false},
		{"a plain message", "avisame en 20 minutos", nil, "es", "", false},
	} {
		got, ok := commandReply(tc.text, tc.entities, tc.lang)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: commandReply = %q, %v; want %q, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	if es.Greeting == "" || en.Greeting == "" || es.UnknownCommand == "" || en.UnknownCommand == "" {
		t.Error("a command reply is empty in some language")
	}
}
