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

// htmlCommentStart and htmlCommentEnd bound an HTML comment block. Matched
// as plain substrings (not anchored to line start/end) since a comment
// marker may share a line with other Markdown text.
const (
	htmlCommentStart = "<!--"
	htmlCommentEnd   = "-->"
)

// ExtractJSONFence returns the body of the single fenced ```json``` block in
// md — a format.md's documented example shape (spec R10.2). It is a loud,
// named error, never a silent skip or a silent pick of the first candidate,
// when md contains zero such fences, more than one, or an unterminated one:
// a format.md with no example, or with two competing ones, must never let a
// caller quietly validate against whichever one happens to come first. This
// is the same trap test/support/schema/markdown.go's topLevelCreateCount
// guard was written to close for the SQL side one slice ago — reusing that
// lesson here, not reinventing a parser that could reintroduce it.
//
// A fence entirely inside an HTML comment (`<!-- ... -->`) is invisible to
// this parser — it is never counted as a candidate at all (four-lens pre-PR
// review, CRITICAL finding 3). A human reading the rendered Markdown sees no
// example there either, so the honest reading is "this fence does not
// exist," which naturally surfaces through the existing zero/two-fence
// error taxonomy above (e.g. a format.md whose ONLY fence is commented out
// reports "found 0 fenced ```json blocks", the same loud error an author
// would get for never having written an example at all) instead of adding a
// third, redundant error variant for what is, from the parser's own
// perspective, simply "no live fence found."
func ExtractJSONFence(md []byte) ([]byte, error) {
	var fences [][]byte
	var current []string
	inFence := false
	inComment := false

	for _, line := range strings.Split(string(md), "\n") {
		line = strings.TrimRight(line, "\r")

		if inComment {
			if strings.Contains(line, htmlCommentEnd) {
				inComment = false
			}
			continue
		}
		if strings.Contains(line, htmlCommentStart) {
			inComment = true
			continue
		}

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
