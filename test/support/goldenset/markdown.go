package goldenset

import (
	"fmt"
	"regexp"
	"strings"
)

// jsonFenceStartPattern recognizes a fenced code block's opening line whose
// info string is exactly "json" — every testdata/{recall,classify,llm}/
// format.md's "Shape" section convention. Case-sensitive, mirroring
// test/support/schema/markdown.go's own fenceStartPattern: a ```JSON``` fence
// is invisible to this parser on purpose, since the convention this package
// enforces is lowercase.
var jsonFenceStartPattern = regexp.MustCompile("^```json\\s*$")

// fenceEndPattern recognizes a closing fence line: three backticks and
// nothing else but trailing whitespace.
var fenceEndPattern = regexp.MustCompile("^```\\s*$")

// ExtractJSONFence returns the body of the single fenced ```json``` block in
// md — a format.md's documented example shape (spec R10.2). It is a loud,
// named error, never a silent skip or a silent pick of the first candidate,
// when md contains zero such fences, more than one, or an unterminated one:
// a format.md with no example, or with two competing ones, must never let a
// caller quietly validate against whichever one happens to come first. This
// is the same trap test/support/schema/markdown.go's topLevelCreateCount
// guard was written to close for the SQL side one slice ago — reusing that
// lesson here, not reinventing a parser that could reintroduce it.
func ExtractJSONFence(md []byte) ([]byte, error) {
	var fences [][]byte
	var current []string
	inFence := false

	for _, line := range strings.Split(string(md), "\n") {
		line = strings.TrimRight(line, "\r")
		if !inFence {
			if jsonFenceStartPattern.MatchString(line) {
				inFence = true
				current = nil
			}
			continue
		}
		if fenceEndPattern.MatchString(line) {
			fences = append(fences, []byte(strings.Join(current, "\n")))
			inFence = false
			continue
		}
		current = append(current, line)
	}
	if inFence {
		return nil, fmt.Errorf("goldenset.ExtractJSONFence: found an unterminated ```json fence (opened but never closed with a matching ``` line)")
	}
	if len(fences) == 0 {
		return nil, fmt.Errorf("goldenset.ExtractJSONFence: found 0 fenced ```json blocks, want exactly 1")
	}
	if len(fences) > 1 {
		return nil, fmt.Errorf("goldenset.ExtractJSONFence: found %d fenced ```json blocks, want exactly 1 — ambiguous which one documents the format", len(fences))
	}
	return fences[0], nil
}
