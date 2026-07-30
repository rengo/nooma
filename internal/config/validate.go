package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
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
	{"database path", checkDatabasePath},
	{"providers", checkProviders},
	{"tasks", checkTasks},
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

func checkDatabasePath(c *Config, vaultDir string, _ func(string) (string, bool)) error {
	_, err := c.DatabasePath(vaultDir)
	return err
}

// DatabasePath resolves database.path against the vault and refuses to leave it.
//
// The vault is one object that is copied, moved and backed up as a unit
// (docs/01-architecture.md). A database outside it breaks that guarantee
// silently: everything works until a backup is restored somewhere else and the
// brain comes back empty. Absolute paths are allowed only when they land inside
// the vault, so the rule is about the destination rather than about the notation.
//
// The returned path is always absolute, because internal/store/sqlite.Open
// rejects a relative one.
func (c *Config) DatabasePath(vaultDir string) (string, error) {
	raw := *c.Database.Path

	resolved := raw
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(vaultDir, resolved)
	}
	resolved = filepath.Clean(resolved)

	vault := filepath.Clean(vaultDir)
	rel, err := filepath.Rel(vault, resolved)
	if err != nil {
		return "", fmt.Errorf("database.path %q cannot be resolved against the vault %q: %w", raw, vault, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("database.path %q resolves to %q, which is outside the vault %q — the vault must stay one self-contained object", raw, resolved, vault)
	}

	return resolved, nil
}

// checkProviders police what strict decoding structurally cannot. `providers` is
// a map, so its keys are user data and the decoder has no schema to compare them
// against; the `type` values inside are the part that has a documented list.
//
// M0 interprets no provider (spec R3.1), so an unset api_key_env is deliberately
// not an error here: a configuration that is correct for M1 must stay loadable
// today (spec R5.2).
func checkProviders(c *Config, _ string, _ func(string) (string, bool)) error {
	var problems []error
	for _, name := range sortedKeys(c.Providers) {
		typ := c.Providers[name].Type
		switch {
		case typ == "":
			problems = append(problems, fmt.Errorf("providers.%s has no type; one of %s is required", name, strings.Join(DocumentedProviderTypes, ", ")))
		case !slices.Contains(DocumentedProviderTypes, typ):
			problems = append(problems, fmt.Errorf("providers.%s.type is %q, which is not one of the documented types: %s", name, typ, strings.Join(DocumentedProviderTypes, ", ")))
		}
	}
	return errors.Join(problems...)
}

// checkTasks rejects a task name outside the documented seven. A typo here would
// otherwise silently leave a brain task unbound — the decoder cannot catch it,
// because the name is a map key.
func checkTasks(c *Config, _ string, _ func(string) (string, bool)) error {
	var problems []error
	for _, name := range sortedKeys(c.Tasks) {
		if !slices.Contains(DocumentedTaskNames, name) {
			problems = append(problems, fmt.Errorf("tasks.%s is not a documented task; the seven are %s", name, strings.Join(DocumentedTaskNames, ", ")))
		}
	}
	return errors.Join(problems...)
}

// DocumentedProviderTypes are the provider `type` values
// docs/01-architecture.md defines. Three, not four: the document has four
// provider *entries* and `anthropic` appears twice.
var DocumentedProviderTypes = []string{"anthropic", "ollama", "whisper_cpp"}

// DocumentedTaskNames are the seven brain tasks docs/01-architecture.md binds to
// providers.
var DocumentedTaskNames = []string{
	"chat",
	"capture_processing",
	"relation_evaluation",
	"belief_derivation",
	"embedding",
	"audio_transcription",
	"image_description",
}
