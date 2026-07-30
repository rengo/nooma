package config

import (
	"errors"
	"fmt"
)

// check is one named validation. Making checks values rather than a sequence of
// early returns is what makes spec R5.4 — report every problem, not the first —
// a property of the code's shape instead of a rule somebody has to remember when
// adding the next one (design D10).
//
// `nooma doctor` exists to make the binary feel cared for. A doctor that reports
// one problem per run makes the user iterate.
type check struct {
	name string
	run  func(*Config, string, func(string) (string, bool)) error
}

var checks = []check{
	{"binding", checkBinding},
	{"telegram", checkTelegram},
}

// Validate runs every check and joins their failures.
//
// vaultDir is where a relative database path resolves. lookup reads the
// environment, injected rather than taken from os so that precedence and
// absence are testable in parallel (design D7's precedent).
//
// Call ApplyDefaults first: Validate judges the configuration the binary will
// actually run with.
func (c *Config) Validate(vaultDir string, lookup func(string) (string, bool)) error {
	var problems []error
	for _, ck := range checks {
		if err := ck.run(c, vaultDir, lookup); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// checkBinding is ADR-0007's half that config can decide: a non-loopback bind
// makes server.auth_token_env mandatory, and the variable it names must actually
// be set.
//
// Catching it here means `nooma doctor` reports it without starting a server. The
// refusal to open a socket is a separate mechanism, because a server that binds
// and then complains has already exposed the port.
func checkBinding(c *Config, _ string, lookup func(string) (string, bool)) error {
	bind := *c.Server.Bind
	if isLoopbackHost(bind) {
		return nil
	}
	if c.Server.AuthTokenEnv == "" {
		return fmt.Errorf("server.bind is %q, which is not loopback, so server.auth_token_env is mandatory (ADR-0007)", bind)
	}
	if _, set := lookup(c.Server.AuthTokenEnv); !set {
		return fmt.Errorf("server.bind is %q and server.auth_token_env names $%s, which is not set (ADR-0007)", bind, c.Server.AuthTokenEnv)
	}
	return nil
}

// checkTelegram enforces ADR-0006 and non-negotiable #7 in M0, two milestones
// before the adapter that consumes it exists.
//
// That timing is the decision. Deferring the check to M3 would leave
// docs/01-architecture.md's "without that list, the channel does not start" as
// future discipline rather than a gate, and a safe default nobody enforces is a
// warning wearing a promise's clothes.
func checkTelegram(c *Config, _ string, lookup func(string) (string, bool)) error {
	t := c.Channels.Telegram
	if !t.Enabled {
		return nil
	}

	var problems []error
	if len(t.AllowedChatIDs) == 0 {
		problems = append(problems, errors.New("channels.telegram is enabled with no allowed_chat_ids; anyone who finds the bot could talk to this brain (ADR-0006)"))
	}
	if t.BotTokenEnv == "" {
		problems = append(problems, errors.New("channels.telegram is enabled without bot_token_env"))
	} else if _, set := lookup(t.BotTokenEnv); !set {
		problems = append(problems, fmt.Errorf("channels.telegram is enabled and bot_token_env names $%s, which is not set", t.BotTokenEnv))
	}
	return errors.Join(problems...)
}
