// Package mdfence extracts exactly one fenced code block from one section of a
// Markdown document.
//
// It exists because this repository already had two fence extractors and neither
// could do this. `test/support/schema` is hardcoded to ```sql and deliberately
// collects *every* match across the whole document, because doc 03 legitimately
// has many CREATE blocks. `test/support/goldenset` has the exactly-one-or-error
// arity this gate needs, but is JSON-specific. Neither scopes to a section.
//
// The section scope is the property that keeps a gate built on this from passing
// by luck. docs/01-architecture.md happens to contain exactly one ```yaml fence
// today, so a whole-document scan would find it and pass — right up until a
// second yaml example appears anywhere in the file, at which point the scan
// silently starts comparing against whichever one comes first. Silently picking
// the first match is this project's most-recorded defect shape; a gate built on
// it would be the twelfth instance living inside a mechanism designed to prevent
// drift.
//
// The two older extractors are deliberately not refactored onto this one. That
// is a change with its own risk and its own review, not a side effect of adding a
// gate.
package mdfence

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	htmlCommentStart = "<!--"
	htmlCommentEnd   = "-->"
)

var (
	headingPattern  = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	fenceEndPattern = regexp.MustCompile("^\\s*```\\s*$")

	// Any fence delimiter, opening or closing, of any language. Used only to
	// track whether a line is inside a code block while looking for headings.
	anyFenceDelimiter = regexp.MustCompile("^\\s*```")
)

// Extract returns the single fenced block of the given language inside the first
// section whose heading contains sectionTitle.
//
// It fails, rather than choosing, when the section is missing, when the section
// holds no such fence, and when it holds more than one. "Which block documents
// this?" is not a question a test may answer by picking.
//
// A section runs from its heading to the next heading of equal or higher level,
// so fences inside subsections are in scope and the next sibling ends it.
//
// A fence inside an HTML comment is not a candidate, following
// goldenset.ExtractJSONFence: a commented-out example is invisible in the
// rendered document, so the honest reading is that it does not exist. Counting it
// would make a stale example ambiguous with a live one.
func Extract(md []byte, sectionTitle, language string) ([]byte, error) {
	lines := strings.Split(string(md), "\n")

	// Headings are only headings outside a fenced block. A YAML comment is
	// `# text`, byte for byte a Markdown H1, and docs/01-architecture.md's
	// configuration example contains two of them. Scanning without tracking fence
	// state reads the first as a level-1 heading, ends the section there, and cuts
	// the fence in half.
	heads := headingLevels(lines)

	start, level := -1, 0
	for i, line := range lines {
		if heads[i] == 0 {
			continue
		}
		if m := headingPattern.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil && strings.Contains(m[2], sectionTitle) {
			start, level = i+1, heads[i]
			break
		}
	}
	if start == -1 {
		return nil, fmt.Errorf("mdfence: no section whose heading contains %q", sectionTitle)
	}

	end := len(lines)
	for i := start; i < len(lines); i++ {
		if heads[i] > 0 && heads[i] <= level {
			end = i
			break
		}
	}

	fences, err := fencesIn(lines[start:end], language)
	if err != nil {
		return nil, fmt.Errorf("mdfence: section %q: %w", sectionTitle, err)
	}
	switch len(fences) {
	case 1:
		return []byte(fences[0]), nil
	case 0:
		return nil, fmt.Errorf("mdfence: section %q: found 0 fenced ```%s blocks, want exactly 1", sectionTitle, language)
	default:
		return nil, fmt.Errorf("mdfence: section %q: found %d fenced ```%s blocks, want exactly 1 — ambiguous which one is authoritative", sectionTitle, len(fences), language)
	}
}

// headingLevels reports, for each line, its Markdown heading level, or 0 when the
// line is not a heading. Lines inside a fenced block are never headings, however
// much they look like one.
func headingLevels(lines []string) []int {
	levels := make([]int, len(lines))
	inFence := false

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		if anyFenceDelimiter.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingPattern.FindStringSubmatch(line); m != nil {
			levels[i] = len(m[1])
		}
	}
	return levels
}

func fencesIn(lines []string, language string) ([]string, error) {
	startPattern := regexp.MustCompile("^\\s*```" + regexp.QuoteMeta(language) + `\s*$`)

	var (
		fences  []string
		current []string
		inFence bool
		inHTML  bool
	)

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		if inHTML {
			if strings.Contains(line, htmlCommentEnd) {
				inHTML = false
			}
			continue
		}
		if !inFence && strings.Contains(line, htmlCommentStart) {
			// A single-line comment opens and closes on the same line.
			if !strings.Contains(line, htmlCommentEnd) {
				inHTML = true
			}
			continue
		}

		if !inFence {
			if startPattern.MatchString(line) {
				inFence = true
				current = nil
			}
			continue
		}
		if fenceEndPattern.MatchString(line) {
			fences = append(fences, strings.Join(current, "\n"))
			inFence = false
			continue
		}
		current = append(current, line)
	}

	if inFence {
		return nil, fmt.Errorf("found an unterminated ```%s fence (opened but never closed)", language)
	}
	return fences, nil
}
