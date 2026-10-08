package selfmodel

import "errors"

// MaxBeliefContentRunes bounds the content a form may submit for a belief
// edit, counted in runes so the same sentence is legal in English and in
// Japanese.
//
// It is an input bound of the ports.BrowsePageSize class
// (internal/ports/unitrepo.go): it validates what a form may carry and
// decides nothing doc 02 governs, so it is deliberately NOT a doc 02 §13
// calibration row. The value is chosen, not calibrated: a belief is a
// sentence or a short paragraph, and the bound keeps one form field from
// making the nightly embed cost unbounded. It does not bound derived
// beliefs, whose text the judge produces.
const MaxBeliefContentRunes = 1000

// ErrEmptyContent is returned by NormalizeContent when the normalised
// content is empty.
var ErrEmptyContent = errors.New("selfmodel: belief content is empty")

// ErrContentTooLong is returned by NormalizeContent when the normalised
// content is longer than MaxBeliefContentRunes runes.
var ErrContentTooLong = errors.New("selfmodel: belief content is too long")

// NormalizeText is the total, non-validating half of the content rules:
// every "\r\n" becomes "\n", then surrounding whitespace is trimmed. It
// never errors, so it is safe to apply to stored text that must not be
// validated (derived content is unbounded and may look blank).
func NormalizeText(raw string) string {
	return raw
}

// NormalizeContent is NormalizeText followed by rejecting an empty result
// (ErrEmptyContent) and a result of more than MaxBeliefContentRunes runes
// (ErrContentTooLong). It runs once, on submitted text.
func NormalizeContent(raw string) (string, error) {
	return raw, nil
}
