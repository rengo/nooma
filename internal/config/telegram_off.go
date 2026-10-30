package config

import (
	"bytes"
	"fmt"
	"strings"

	yaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// TelegramEdit is what DisableTelegram did to a file.
type TelegramEdit int

const (
	// TelegramAlreadyOff: the channel was off or absent; the file is unchanged.
	TelegramAlreadyOff TelegramEdit = iota
	// TelegramTurnedOff: the value was replaced in the text, every other byte
	// kept.
	TelegramTurnedOff
	// TelegramRewritten: the value could not be located in the text, so the
	// file was re-serialized with the channel off; comments and layout may have
	// moved.
	TelegramRewritten
)

// byteOrderMark is UTF-8's, which Decode ignores at the start of a file.
const byteOrderMark = "\xef\xbb\xbf"

// telegramEnabledPath is channels.telegram.enabled.
const telegramEnabledPath = "$.channels.telegram.enabled"

// DisableTelegram returns doc, a nooma.yml, with channels.telegram.enabled
// false (ADR-0057: an imported copy must not poll the bot its original polls,
// or the two steal each other's messages). A plain value is replaced where it
// stands in the text, so comments and formatting survive. A value carrying an
// anchor, or that is an alias, is never edited in place, since another
// setting may share it; the file is re-serialized instead, as it is when the
// plain value's position cannot be confirmed, and the result says so. The
// result is decoded strictly, as nooma loads it, before it is returned.
func DisableTelegram(doc []byte) ([]byte, TelegramEdit, error) {
	return disableTelegramWith(doc, replaceToken, keepRewrite)
}

// keepRewrite is the identity on a re-serialized file.
func keepRewrite(b []byte) []byte { return b }

// disableTelegram is DisableTelegram with the in-place edit given, so its
// fallback and its strict re-decode can be driven by a test.
func disableTelegram(doc []byte, replace func(doc []byte, line, column int, value, with string) ([]byte, bool)) ([]byte, TelegramEdit, error) {
	return disableTelegramWith(doc, replace, keepRewrite)
}

// disableTelegramWith is disableTelegram with what follows a re-serialization
// given too, so a test can make the rewrite produce a file that does not load.
func disableTelegramWith(doc []byte, replace func(doc []byte, line, column int, value, with string) ([]byte, bool), afterRewrite func([]byte) []byte) ([]byte, TelegramEdit, error) {
	cfg, err := Decode(bytes.NewReader(doc))
	if err != nil {
		return nil, 0, err
	}
	if !cfg.Channels.Telegram.Enabled {
		return doc, TelegramAlreadyOff, nil
	}

	clean := bytes.TrimPrefix(doc, []byte(byteOrderMark))
	file, err := parser.ParseBytes(clean, parser.ParseComments)
	if err != nil {
		return nil, 0, fmt.Errorf("nooma.yml: %w", err)
	}
	path, err := yaml.PathString(telegramEnabledPath)
	if err != nil {
		return nil, 0, err
	}
	node, err := path.FilterFile(file)
	if err != nil {
		return nil, 0, fmt.Errorf("nooma.yml: finding %s: %w", telegramEnabledPath, err)
	}

	scalar, anchor, alias := valueOf(node)
	var out []byte
	how := TelegramTurnedOff
	if anchor == "" && alias == "" {
		tk := scalar.GetToken()
		if edited, ok := replace(clean, tk.Position.Line, tk.Position.Column, tk.Value, "false"); ok {
			// A byte order mark, if any, stays in front.
			out = append(bytes.Clone(doc[:len(doc)-len(clean)]), edited...)
		}
	}
	if out == nil {
		if err := path.ReplaceWithReader(file, strings.NewReader("false")); err != nil {
			return nil, 0, fmt.Errorf("nooma.yml: turning Telegram off: %w", err)
		}
		out, how = afterRewrite([]byte(file.String())), TelegramRewritten
	}

	check, err := Decode(bytes.NewReader(out))
	switch {
	case err != nil && anchor != "":
		return nil, 0, fmt.Errorf("nooma.yml: channels.telegram.enabled carries the anchor &%s, which another setting reuses, so turning Telegram off would take that setting's value away too; give enabled a plain value in the source's nooma.yml and export again (%w)", anchor, err)
	case err != nil && alias != "":
		return nil, 0, fmt.Errorf("nooma.yml: channels.telegram.enabled is the alias *%s, and turning Telegram off did not leave a file that loads; give enabled a plain value in the source's nooma.yml and export again (%w)", alias, err)
	case err != nil:
		return nil, 0, fmt.Errorf("nooma.yml after turning Telegram off: %w", err)
	case check.Channels.Telegram.Enabled:
		return nil, 0, fmt.Errorf("nooma.yml: Telegram is still on after the edit")
	}
	return out, how, nil
}

// valueOf is the scalar behind any tags on node, with the anchor it carries
// or the alias it is, if any: in `enabled: !!bool true` the token is `true`,
// not `!!bool`.
func valueOf(node ast.Node) (scalar ast.Node, anchor, alias string) {
	for {
		switch n := node.(type) {
		case *ast.TagNode:
			node = n.Value
		case *ast.AnchorNode:
			anchor = strings.TrimPrefix(n.Name.String(), "&")
			node = n.Value
		case *ast.AliasNode:
			return node, anchor, strings.TrimPrefix(n.Value.String(), "*")
		default:
			return node, anchor, alias
		}
	}
}

// replaceToken replaces the text value at line and column (both 1-based, the
// column counted in characters) with with, only when the bytes there are
// exactly value.
func replaceToken(doc []byte, line, column int, value, with string) ([]byte, bool) {
	lines := bytes.SplitAfter(doc, []byte("\n"))
	if line < 1 || line > len(lines) || column < 1 {
		return nil, false
	}
	start := 0
	for _, l := range lines[:line-1] {
		start += len(l)
	}
	at := start
	for i, r := 0, []rune(string(lines[line-1])); i < column-1; i++ {
		if i >= len(r) {
			return nil, false
		}
		at += len(string(r[i]))
	}
	if !bytes.HasPrefix(doc[at:], []byte(value)) {
		return nil, false
	}
	out := make([]byte, 0, len(doc)-len(value)+len(with))
	out = append(out, doc[:at]...)
	out = append(out, with...)
	return append(out, doc[at+len(value):]...), true
}
