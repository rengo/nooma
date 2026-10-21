package classify

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// RecallScope is the model's reading of what a recall asks about and when
// (ADR-0048): the model understands the question, and the code acts on these
// typed fields alone — no Go word list for "ayer" or "esta semana" (ADR-0033).
// Either part may be nil; a scope with neither is no scope at all.
type RecallScope struct {
	// About is the topic asked about, in the message's own language and
	// without question or time words: "lo del oculista" is "oculista".
	About *string
	// Window is the span of time the question names.
	Window *RecallWindow
}

// RecallWindow is a span of time a recall asks about: From inclusive, To
// exclusive, From strictly before To, and On saying which instant of a unit
// falls in it.
type RecallWindow struct {
	On       RecallWindowOn
	From, To time.Time
}

// RecallWindowOn names which instant of a unit a window is read against.
type RecallWindowOn string

const (
	// RecallOnTold is when the unit was told: its created_at. "¿Qué te conté
	// ayer?"
	RecallOnTold RecallWindowOn = "told"
	// RecallOnWhen is when the unit is: its event_at, else its due_at. "¿Qué
	// tengo esta semana?"
	RecallOnWhen RecallWindowOn = "when"
)

// AllRecallWindowOns is the closed vocabulary of a window's on.
func AllRecallWindowOns() []RecallWindowOn { return []RecallWindowOn{RecallOnTold, RecallOnWhen} }

// assignRecallScope reads recall_scope. It is one field and degrades whole: a
// window the decoder cannot read leaves no half-scope behind, so a recall with
// a malformed part answers as one that named none, as before ADR-0048. An
// empty object, or a blank topic with no window, is no scope and no loss.
func assignRecallScope(raw json.RawMessage, c *Classification, now time.Time) Reason {
	var wire struct {
		About  *string         `json:"about"`
		Window json.RawMessage `json:"window"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return ReasonWrongType
	}
	var scope RecallScope
	if wire.About != nil {
		if about := strings.TrimSpace(*wire.About); about != "" {
			scope.About = &about
		}
	}
	if w := bytes.TrimSpace(wire.Window); len(w) > 0 && string(w) != "null" {
		window, r := readRecallWindow(w, now.Location())
		if r != "" {
			return r
		}
		scope.Window = &window
	}
	if scope.About == nil && scope.Window == nil {
		return ""
	}
	c.RecallScope = &scope
	return ""
}

// readRecallWindow reads a window: on from its vocabulary, and from and to as
// event_at is written, a timestamp with its offset or a bare day at its
// midnight in loc.
func readRecallWindow(raw json.RawMessage, loc *time.Location) (RecallWindow, Reason) {
	var wire struct {
		On       json.RawMessage `json:"on"`
		From, To *string
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return RecallWindow{}, ReasonWrongType
	}
	on, r := decodeEnum(wire.On, AllRecallWindowOns())
	if r != "" {
		// A missing on is outside the vocabulary rather than the wrong type:
		// the window says nothing about which instant it bounds.
		return RecallWindow{}, ReasonUnknownEnum
	}
	from, okFrom := readInstant(wire.From, loc)
	to, okTo := readInstant(wire.To, loc)
	if !okFrom || !okTo || !from.Before(to) {
		return RecallWindow{}, ReasonBadFormat
	}
	return RecallWindow{On: *on, From: from, To: to}, ""
}

// readInstant is assignTime's reading of one value, nil being unreadable.
func readInstant(s *string, loc *time.Location) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, *s); err == nil {
		return t, true
	}
	return parseDateOnly(*s, loc)
}
