// Package httpapi serves nooma's HTTP surface: the API and the UI, from the same
// binary and the same process (docs/01-architecture.md, Layers 1 and 2).
//
// In M0 that surface is deliberately thin — a hello response and a UI
// placeholder. What is not thin is the decision about where it may listen, which
// is ADR-0007's and lands here in full.
package httpapi

import (
	"fmt"
	"net"
	"strconv"

	"github.com/rengo/nooma/internal/config"
)

// DecideBinding returns the address to listen on, or refuses.
//
// ADR-0007: bind to 127.0.0.1 by default, and if the user configures a
// non-loopback bind then server.auth_token_env becomes mandatory and the server
// does not start without it. The safety of that default is structural, not a
// warning.
//
// This is a pure function, and it is called BEFORE net.Listen for a reason that
// is easy to lose: a server that binds and then complains has already exposed the
// port. Deciding first means no code path can create a listener while skipping
// the refusal, and the whole truth table is testable without opening a socket.
//
// The decision parses; it never compares strings. "127.0.0.1.evil" is a hostname,
// not an address, and a prefix or substring test that accepted it would disable
// ADR-0007 silently — the server would bind a public interface believing it was
// local.
func DecideBinding(cfg *config.Config, lookup func(string) (string, bool)) (string, error) {
	host := *cfg.Server.Bind
	port := *cfg.Server.HTTPPort

	if isLoopback(host) {
		return net.JoinHostPort(host, strconv.Itoa(port)), nil
	}

	if cfg.Server.AuthTokenEnv == "" {
		return "", fmt.Errorf(
			"server.bind is %q, which is not loopback, so server.auth_token_env is mandatory (ADR-0007); refusing to listen",
			host)
	}
	if _, set := lookup(cfg.Server.AuthTokenEnv); !set {
		return "", fmt.Errorf(
			"server.bind is %q and server.auth_token_env names $%s, which is not set (ADR-0007); refusing to listen",
			host, cfg.Server.AuthTokenEnv)
	}

	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// isLoopback decides by parsing.
//
// The literal "localhost" is a deliberate special case with no DNS lookup: a
// resolution inside a security-relevant decision is slow at startup and
// untestable offline, which non-negotiable #5 forbids. Anything else that does
// not parse as an IP is treated as exposed — the safe direction, so an
// unresolvable or unfamiliar name demands the token rather than assuming safety.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
