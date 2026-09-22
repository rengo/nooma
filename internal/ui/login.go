package ui

import (
	"context"
	"net/http"
)

// LoginView is the handshake screen's own read model. httpapi builds it and
// hands it to RenderLogin (design m4a §3.3's "the seam between httpapi and
// ui is exactly two calls"). Rejected distinguishes only whether the last
// submission failed; it never carries the submitted token or any part of
// it — the screen after a failed attempt looks identical no matter what was
// submitted, the same byte-identical posture requireCookie already holds
// for a missing versus a wrong cookie.
type LoginView struct {
	Rejected bool
}

// RenderLogin renders the handshake screen at status. httpapi owns the
// route, the comparison and the cookie; this package owns only the markup
// (design m4a §3.3) — status travels through this one call so the response
// carries a single, correct Content-Type and status line, rather than
// httpapi calling w.WriteHeader itself ahead of a header this function
// still needs to set.
func RenderLogin(ctx context.Context, w http.ResponseWriter, status int, view LoginView) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	return Login(view).Render(ctx, w)
}
