package ui

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Every time the mirror displays is rendered in the zone the injected clock's
// instant carries — the same zone doc 02 §5 says travels inside the clock, so
// there is no timezone setting. Stored values stay UTC/RFC3339; only the
// presentation moves. ServeHTTP reads the zone once per request and hands it to
// the templates through the request context, so no template reaches for
// time.Local or time.Now.

type zoneKey struct{}

// withZone returns ctx carrying the zone of the clock's current instant. A nil
// clock leaves ctx untouched: a bare template render then falls back to UTC and
// shows no zone note.
func withZone(ctx context.Context, now func() time.Time) context.Context {
	if now == nil {
		return ctx
	}
	return context.WithValue(ctx, zoneKey{}, now())
}

// zoneAt is the instant whose location the page renders in, if one was injected.
func zoneAt(ctx context.Context) (time.Time, bool) {
	now, ok := ctx.Value(zoneKey{}).(time.Time)
	return now, ok
}

// formatTime renders t as "2006-01-02 15:04" in the injected zone, UTC without one.
// Each instant uses the offset in force at that instant, so a date across a DST
// change reads correctly.
func formatTime(ctx context.Context, t time.Time) string {
	loc := time.UTC
	if now, ok := zoneAt(ctx); ok {
		loc = now.Location()
	}
	return t.In(loc).Format(dateLayout)
}

// instantRe finds RFC3339 instants inside text. Dates without a time of day
// ("2026-10-16") and bare clock times ("12:00Z") do not match and stay as written.
var instantRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)

// localizeInstant converts a change value only when the WHOLE value is one
// RFC3339 instant. Any other value (content, action text, belief text) is the
// user's or a number or a flag, and is shown as written.
func localizeInstant(ctx context.Context, v string) string {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return formatTime(ctx, t)
	}
	return v
}

// localizeProse rewrites the RFC3339 instants inside text the brain itself wrote
// (a decision's rationale, a refusal, a chat reply) to formatTime's local form.
// Never use it on the user's captured text. A token counts only on its own
// boundaries: glued to a letter or digit on either side, or sitting after
// "/", "=", "?", "&" or "#", or inside a word containing "://" (a URL or query),
// it is left as written, as is a token that does not parse. Stored text is never
// touched.
func localizeProse(ctx context.Context, s string) string {
	var b strings.Builder
	last := 0
	for _, m := range instantRe.FindAllStringIndex(s, -1) {
		start, end := m[0], m[1]
		if !instantBounded(s, start, end) {
			continue
		}
		t, err := time.Parse(time.RFC3339, s[start:end])
		if err != nil {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(formatTime(ctx, t))
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// instantBounded reports whether s[start:end] stands alone as an instant.
func instantBounded(s string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:start])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("/=?&#", r) {
			return false
		}
	}
	if end < len(s) {
		r, _ := utf8.DecodeRuneInString(s[end:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	wordStart := strings.LastIndexAny(s[:start], " \t\r\n") + 1
	wordEnd := len(s)
	if i := strings.IndexAny(s[end:], " \t\r\n"); i >= 0 {
		wordEnd = end + i
	}
	return !strings.Contains(s[wordStart:wordEnd], "://")
}

// zoneNote is the one-line statement of which zone the page's times are in, or
// "" when no zone was injected. time.Local's name is the literal "Local" on
// every machine (doc 02 §5), so an unnamed or "Local" zone is described by its
// offset alone.
func zoneNote(ctx context.Context) string {
	now, ok := zoneAt(ctx)
	if !ok {
		return ""
	}
	_, secs := now.Zone()
	return describeZone(now.Location().String(), secs)
}

// describeZone words the note from a zone's name and its offset in seconds.
// "Local" and "" carry no information, so they are dropped for the offset.
func describeZone(name string, secs int) string {
	if secs == 0 && (name == "UTC" || name == "Local" || name == "") {
		return "Times in UTC"
	}
	sign, abs := '+', secs
	if secs < 0 {
		sign, abs = '-', -secs
	}
	offset := fmt.Sprintf("UTC%c%02d:%02d", sign, abs/3600, abs%3600/60)
	if name == "" || name == "Local" {
		return "Times in " + offset
	}
	return fmt.Sprintf("Times in %s (%s)", name, offset)
}

// zoned is the request with the clock's zone in its context.
func (h *Handler) zoned(r *http.Request) *http.Request {
	return r.WithContext(withZone(r.Context(), h.deps.Now))
}
