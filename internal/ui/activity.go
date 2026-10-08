package ui

import "net/http"

// serveActivity answers GET /ui/activity: what Nooma did, newest first.
// It writes nothing.
func (h *Handler) serveActivity(w http.ResponseWriter, r *http.Request) {
	if h.deps.Activity == nil {
		http.Error(w, "activity: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = ActivityPage().Render(r.Context(), w)
}
