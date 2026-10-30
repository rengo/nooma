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
// or the two steal each other's messages). The value is replaced where it
// stands in the text, so comments and formatting survive; only if its
// position cannot be confirmed is the file re-serialized, and the result says
// so. The result is decoded strictly, as nooma loads it, before it is
// returned.
func DisableTelegram(doc []byte) ([]byte, TelegramEdit, error) {
	return disableTelegram(doc, replaceToken)
}

// disableTelegram is DisableTelegram with the in-place edit given, so its
// fallback and its strict re-decode can be driven by a test.
func disableTelegram(doc []byte, replace func(doc []byte, line, column int, value, with string) ([]byte, bool)) ([]byte, TelegramEdit, error) {
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

	var out []byte
	how := TelegramTurnedOff
	tk := scalarOf(node).GetToken()
	if edited, ok := replace(clean, tk.Position.Line, tk.Position.Column, tk.Value, "false"); ok {
		// A byte order mark, if any, stays in front.
		out = append(bytes.Clone(doc[:len(doc)-len(clean)]), edited...)
	} else {
		if err := path.ReplaceWithReader(file, strings.NewReader("false")); err != nil {
			return nil, 0, fmt.Errorf("nooma.yml: turning Telegram off: %w", err)
		}
		out, how = []byte(file.String()), TelegramRewritten
	}

	check, err := Decode(bytes.NewReader(out))
	if err != nil {
		return nil, 0, fmt.Errorf("nooma.yml after turning Telegram off: %w", err)
	}
	if check.Channels.Telegram.Enabled {
		return nil, 0, fmt.Errorf("nooma.yml: Telegram is still on after the edit")
	}
	return out, how, nil
}

// scalarOf is the value behind any tags and anchors on node: in
// `enabled: !!bool true` the token to replace is `true`, not `!!bool`.
func scalarOf(node ast.Node) ast.Node {
	for {
		switch n := node.(type) {
		case *ast.TagNode:
			node = n.Value
		case *ast.AnchorNode:
			node = n.Value
		default:
			return node
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
