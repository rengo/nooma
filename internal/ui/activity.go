package ui

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
)

// The 400 reasons of parseActivityCursor, named like parseBrowseCursor's so
// the handler's text and the tests that assert on it read the same sentence.
var (
	errActivityHalfCursor      = errors.New("before_at and before_seq must both be present, or neither")
	errActivityMalformedCursor = errors.New("before_at must be an RFC3339 timestamp, and before_seq a positive integer")
)

// serveActivity answers GET /ui/activity: what Nooma did, newest first,
// optionally narrowed to one action family (m4e-activity design §3.5, §3.6).
// It writes nothing, and the page it renders offers no way to: previous values
// are shown, never offered back.
func (h *Handler) serveActivity(w http.ResponseWriter, r *http.Request) {
	if h.deps.Activity == nil {
		http.Error(w, "activity: not wired in this build", http.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query()
	cursor, err := parseActivityCursor(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	kind := query.Get("kind")

	page, err := h.deps.Activity.Page(r.Context(), kind, cursor)
	if errors.Is(err, brain.ErrUnknownActivityKind) {
		http.Error(w, "activity: unknown kind", http.StatusBadRequest)
		return
	}
	if err != nil {
		slog.Error("activity: page failed", "err", err)
		http.Error(w, "activity: internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = ActivityPage(page, kind, brain.ActivityFamilies(ports.AllDecisionActions())).Render(r.Context(), w)
}

// parseActivityCursor reads the (before_at, before_seq) keyset pair, the
// shape of parseBrowseCursor: both present or neither, before_at RFC3339 and
// before_seq a positive integer (rowids start at 1).
func parseActivityCursor(q url.Values) (*ports.DecisionCursor, error) {
	hasAt, hasSeq := q.Has("before_at"), q.Has("before_seq")
	if !hasAt && !hasSeq {
		return nil, nil
	}
	if hasAt != hasSeq {
		return nil, errActivityHalfCursor
	}
	at, err := time.Parse(time.RFC3339, q.Get("before_at"))
	if err != nil {
		return nil, errActivityMalformedCursor
	}
	seq, err := strconv.ParseInt(q.Get("before_seq"), 10, 64)
	if err != nil || seq < 1 {
		return nil, errActivityMalformedCursor
	}
	return &ports.DecisionCursor{OccurredAt: at, Seq: seq}, nil
}

// olderURL builds the "older" link: the cursor of the last row shown and the
// kind filter the current page used, so paging never drops a filter.
func olderURL(kind string, next *ports.DecisionCursor) string {
	v := url.Values{}
	if kind != "" {
		v.Set("kind", kind)
	}
	v.Set("before_at", next.OccurredAt.UTC().Format(time.RFC3339))
	v.Set("before_seq", strconv.FormatInt(next.Seq, 10))
	return "/ui/activity?" + v.Encode()
}
