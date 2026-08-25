package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// vaultDirs are the directories `nooma init` creates, per
// docs/01-architecture.md §"Vault structure".
var vaultDirs = []string{"attachments", "derived", "logs"}

// runInit creates a vault.
//
// With a path, that path is the target — relative to the working directory if
// relative, matching every other command (spec R6.4). With no argument the target
// is ~/.nooma/<username>.nooma (spec R7.1b).
func runInit(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		_, _ = fmt.Fprint(errOut, "usage: nooma init [vault]\n\n"+
			"With no argument, creates ~/.nooma/<username>.nooma.\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("init takes at most one vault path, got %d", fs.NArg())
	}

	target, err := initTarget(fs.Arg(0))
	if err != nil {
		return err
	}

	choices, bindings := promptProviderSetup(os.Stdin, out)

	if err := createVault(target, choices, bindings); err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "created vault %s\n", target)
	return err
}

// initTarget resolves where the vault will be created (spec R7.1b).
//
// With no argument the target is ~/.nooma/<username>.nooma, not the working
// directory, and that choice is the whole of this function's judgement. A bare
// command that writes nooma.db, nooma.yml, .env and three directories into
// wherever the user happens to be standing is a command that will one day be run
// in $HOME, or in a source checkout. Creating a named directory in a known place
// is recoverable and predictable; scattering six entries into an arbitrary cwd is
// neither.
func initTarget(arg string) (string, error) {
	if arg != "" {
		return filepath.Abs(arg)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving the home directory: %w", err)
	}
	container := filepath.Join(home, config.HomeVaultDir)

	// A second bare init would leave two vaults in ~/.nooma, and resolution step 4
	// requires exactly one (spec R6.2). Refusing here keeps the default location
	// usable by refusing to make it undecidable — the alternative is a user who
	// runs init twice and then cannot start the binary without an explicit path.
	if entries, err := os.ReadDir(container); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || name == config.HomeVaultDir || !strings.HasSuffix(name, config.VaultSuffix) {
				continue
			}
			if config.IsVault(filepath.Join(container, name)) {
				return "", fmt.Errorf(
					"%s already holds the vault %s, and a second one there would make the default location ambiguous.\n"+
						"Pass a path explicitly, for example `nooma init ~/work.nooma`",
					container, name)
			}
		}
	}

	return filepath.Join(container, username()+config.VaultSuffix), nil
}

// username is the vault's name in home mode, matching docs/01-architecture.md's
// own `pablo.nooma` example.
func username() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		name := u.Username
		// On Windows this is "DOMAIN\\user", and only the last element is a legal
		// directory name.
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:]
		}
		if name != "" {
			return name
		}
	}
	return "nooma"
}

// createVault builds the vault in a sibling staging directory and moves it into
// place, so a failure anywhere leaves the filesystem as it was (spec R7.4).
//
// The order of the target checks IS the safety:
//
//  1. Lstat first, and refuse anything that is not a plain directory without
//     touching it. os.ReadDir on a plain FILE returns an error, not an empty
//     listing — so an emptiness check written as len(entries) == 0 without also
//     requiring err == nil classifies a stray file as "empty", and os.Remove
//     deletes files happily. `touch pablo.nooma && nooma init pablo.nooma` would
//     delete it: non-negotiable #6 violated at the vault's own root. Lstat rather
//     than Stat, so a symlink is seen as a symlink instead of as whatever it
//     points at.
//  2. An existing empty directory is accepted, and has to be: os.Rename refuses
//     ANY existing directory, empty or not (Go's own Lstat guard fires before
//     rename(2), which POSIX would have allowed), and `mkdir x.nooma && nooma
//     init x.nooma` is a thing people do.
//  3. A non-empty directory is refused (R7.3). An init that overwrites is a
//     delete with better manners.
func createVault(target string, choices []providerChoice, bindings map[string]string) error {
	switch info, err := os.Lstat(target); {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%s is a symlink; refusing to create a vault through it", target)
	case err == nil && !info.IsDir():
		return fmt.Errorf("%s already exists and is not a directory", target)
	case err == nil:
		empty, err := isEmptyDir(target)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("%s already exists and is not empty; refusing to overwrite it", target)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("inspecting %s: %w", target, err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(target), err)
	}

	// MkdirTemp creates the directory exclusively and appends a random suffix, so
	// two racing `init`s cannot build into the same staging directory. That is a
	// guarantee from the standard library rather than a name this code picks and
	// hopes about.
	staging, err := os.MkdirTemp(filepath.Dir(target), filepath.Base(target)+".tmp-")
	if err != nil {
		return fmt.Errorf("creating a staging directory beside %s: %w", target, err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	if err := populateVault(staging, choices, bindings); err != nil {
		return err
	}

	return moveIntoPlace(staging, target)
}

func moveIntoPlace(staging, target string) error {
	if _, err := os.Lstat(target); err == nil {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clearing the empty directory at %s: %w", target, err)
		}
	}

	if err := os.Rename(staging, target); err != nil {
		if errors.Is(err, os.ErrExist) || strings.Contains(err.Error(), "not empty") {
			return fmt.Errorf("%s was created by something else while this vault was being built", target)
		}
		return fmt.Errorf("moving the new vault into %s: %w", target, err)
	}
	return nil
}

func populateVault(dir string, choices []providerChoice, bindings map[string]string) error {
	for _, sub := range vaultDirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", sub, err)
		}
	}

	yml := defaultConfig(renderProviders(choices, bindings))
	if err := os.WriteFile(filepath.Join(dir, config.ConfigFileName), []byte(yml), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", config.ConfigFileName, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envSkeleton(choices)), 0o600); err != nil {
		return fmt.Errorf("writing .env: %w", err)
	}

	// The database is created last, because it is the only step that can fail for
	// reasons outside this process's control, and failing before it means less to
	// unwind.
	dbPath := filepath.Join(dir, filepath.Base(config.DefaultDatabasePath))
	vault, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		return fmt.Errorf("creating the database: %w", err)
	}
	return vault.Close()
}

func isEmptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("listing %s: %w", dir, err)
	}
	return len(entries) == 0, nil
}

// defaultConfig is the nooma.yml a new vault starts with. providersSection is
// renderProviders' own output — M0's commented placeholder when the wizard
// was skipped, or a real providers:/tasks: block otherwise (design D15).
//
// TestFreshVaultIsLoadable runs `nooma init` and then loads the result through
// the real loader, so this is not a template somebody eyeballed once: if it stops
// decoding or stops validating, that test fails. Everything here is either a documented default restated for discoverability or
// a commented example — a fresh vault with the wizard skipped configures no
// provider and enables no channel, because M0 interprets neither.
func defaultConfig(providersSection string) string {
	return `# nooma.yml — see docs/01-architecture.md for the full schema.
#
# Secrets are never written here. A credential is always referenced by the NAME
# of an environment variable, and the value lives in the .env beside this file
# (which is never committable) or in the process environment.

server:
  bind: 127.0.0.1      # a non-loopback bind requires auth_token_env (ADR-0007)
  http_port: 7777
  ui: true

database:
  path: ./nooma.db     # relative to this vault; it may not point outside it

` + providersSection + `
channels:
  telegram:
    enabled: false     # enabling this without allowed_chat_ids is a config error
`
}

// EnvVarName is the NAME of an environment variable — never its value
// (design D15, spec R4.3). It is the only credential-adjacent field
// providerChoice carries, and it cannot hold a credential: NewEnvVarName is
// the sole constructor, and it rejects anything not POSIX-shaped.
type EnvVarName string

// envVarNamePattern is POSIX's own shell-variable-name rule. Every
// documented provider's real key format — "sk-ant-api03-…",
// "sk-proj-…" — carries a lowercase letter or a hyphen, both illegal here,
// so a real key can never survive this constructor.
var envVarNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// NewEnvVarName rejects anything that is not a POSIX-shaped environment
// variable name. This is spec R4.3's structural guarantee at its narrowest:
// a caller that only ever builds an EnvVarName through this function can
// never end up holding a key-shaped string in a field meant to name one.
func NewEnvVarName(s string) (EnvVarName, error) {
	if !envVarNamePattern.MatchString(s) {
		return "", fmt.Errorf("%q is not a POSIX-shaped environment variable name (%s) — a real API key is never shaped like one, which is the point", s, envVarNamePattern.String())
	}
	return EnvVarName(s), nil
}

// providerChoice is one providers: entry the wizard writes. None of its
// fields can hold a raw credential value (design D15) — APIKeyEnv names the
// environment variable the key lives in, never the key itself.
type providerChoice struct {
	Name      string // the providers: map key, e.g. "openai_chat"
	Type      string // anthropic | openai | ollama
	Model     string
	APIKeyEnv EnvVarName // empty for ollama
	BaseURL   string     // ollama only; empty means the client's own default
}

// renderProviders renders the providers: and tasks: yml block. Its declared
// parameters carry no field typed to hold a raw secret — APIKeyEnv is an
// EnvVarName, never a plain string that could be a credential value — so
// this function is structurally incapable of writing one into nooma.yml,
// the guarantee spec R4.3 asks for (design D15). With no choices it
// reproduces M0's own commented placeholder exactly, unchanged.
func renderProviders(choices []providerChoice, bindings map[string]string) string {
	if len(choices) == 0 {
		return "# providers:           # added in M1, when nooma starts calling models\n# tasks:\n"
	}

	var b strings.Builder
	b.WriteString("providers:\n")
	for _, c := range choices {
		fmt.Fprintf(&b, "  %s:\n    type: %s\n", c.Name, c.Type)
		if c.APIKeyEnv != "" {
			fmt.Fprintf(&b, "    api_key_env: %s\n", c.APIKeyEnv)
		}
		if c.BaseURL != "" {
			fmt.Fprintf(&b, "    endpoint: %s\n", c.BaseURL)
		}
		fmt.Fprintf(&b, "    model: %s\n", c.Model)
	}

	maxLen := 0
	for _, task := range tasksM1Consumes {
		if len(task) > maxLen {
			maxLen = len(task)
		}
	}
	b.WriteString("\ntasks:\n")
	for _, task := range tasksM1Consumes {
		fmt.Fprintf(&b, "  %-*s { provider: %s }\n", maxLen+1, task+":", bindings[task])
	}
	return b.String()
}

// bindTasks binds every task tasksM1Consumes names to chatProvider, except
// "embedding", which binds to embedProvider — reading the shared list
// itself (design D18a), never a restated copy. A task added to
// tasksM1Consumes by a future milestone is bound automatically by this same
// loop; a hardcoded three-name map would silently keep writing three.
// TestBindTasksReadsTheSharedListNotACopy (tasks_test.go) proves it the same
// way wiring.go's own resolveTaskProviders is already proven — D18a's
// second reader.
func bindTasks(embedProvider, chatProvider string) map[string]string {
	bindings := make(map[string]string, len(tasksM1Consumes))
	for _, task := range tasksM1Consumes {
		if task == "embedding" {
			bindings[task] = embedProvider
		} else {
			bindings[task] = chatProvider
		}
	}
	return bindings
}

// defaultOpenAIKeyEnv is the Cloud path's own default — the exact name
// docs/01-architecture.md's example config already uses for its `gpt_cloud`
// entry.
const defaultOpenAIKeyEnv = "OPENAI_API_KEY"

// promptProviderSetup runs nooma init's own wizard step (spec R4.1, design
// D15): exactly two first-class paths, Cloud and Ollama, plus the option to
// skip entirely and configure later. It never asks for a credential value —
// the only thing it collects beyond the path choice is an environment
// variable NAME, and NewEnvVarName's own rejection means even a user who
// pastes a real key at that one prompt cannot make it into nooma.yml: the
// wizard's whole call graph never holds a credential, the strongest form of
// R4.3's structural guarantee, not only renderProviders' own signature.
//
// EOF — a non-interactive caller, or an e2e test supplying no stdin — reads
// the same as an empty line: skip, reproducing M0's own commented
// placeholder exactly, so every pre-existing test that never scripted stdin
// keeps passing unchanged.
func promptProviderSetup(in io.Reader, out io.Writer) ([]providerChoice, map[string]string) {
	reader := bufio.NewReader(in)

	_, _ = fmt.Fprintln(out, "Configure a model provider now?")
	_, _ = fmt.Fprintln(out, "  1) Cloud (OpenAI) — recommended, required for embeddings")
	_, _ = fmt.Fprintln(out, "  2) Ollama (local)")
	_, _ = fmt.Fprint(out, "Press Enter to skip and configure this later. Choice [1/2]: ")

	switch readLine(reader) {
	case "1", "cloud", "Cloud":
		return cloudPath(reader, out)
	case "2", "ollama", "Ollama":
		choices := ollamaPath()
		return choices, bindTasks(choices[0].Name, choices[0].Name)
	case "":
		return nil, nil
	default:
		_, _ = fmt.Fprintln(out, "Not a recognized choice; skipping provider setup — edit nooma.yml directly whenever you are ready.")
		return nil, nil
	}
}

// cloudPath asks only for the one thing a Cloud vault might legitimately
// need overridden: the environment variable NAME the key will be read from.
// It never asks for the key's value (spec R4.3's own MUST NOT), and it
// writes two openai-typed providers.model bound to different tasks — a
// chat model is not an embedding model (design D15) — because PR 17 already
// gives openai.Client an Embed method by the time this path is written.
func cloudPath(reader *bufio.Reader, out io.Writer) ([]providerChoice, map[string]string) {
	_, _ = fmt.Fprintf(out, "OpenAI API key environment variable name [%s]: ", defaultOpenAIKeyEnv)
	keyEnv := EnvVarName(defaultOpenAIKeyEnv)
	if line := readLine(reader); line != "" {
		v, err := NewEnvVarName(line)
		if err != nil {
			_, _ = fmt.Fprintf(out, "%q is not a valid environment variable name (%v) — using %s instead.\n", line, err, defaultOpenAIKeyEnv)
		} else {
			keyEnv = v
		}
	}
	_, _ = fmt.Fprintf(out, "Add your OpenAI API key to .env as %s= before using this vault.\n", keyEnv)

	choices := []providerChoice{
		{Name: "openai_chat", Type: "openai", Model: "gpt-4o-mini", APIKeyEnv: keyEnv},
		{Name: "openai_embed", Type: "openai", Model: "text-embedding-3-small", APIKeyEnv: keyEnv},
	}
	return choices, bindTasks("openai_embed", "openai_chat")
}

// ollamaPath needs no further prompt. ADR-0002 discards the embedded
// llama.cpp option; one local model name, bound to every task, is a
// reasonable default a user is expected to edit directly in nooma.yml — the
// same posture defaultConfig() already takes toward every other value it
// writes.
func ollamaPath() []providerChoice {
	return []providerChoice{{Name: "ollama_local", Type: "ollama", Model: "llama3.1"}}
}

// readLine reads one line of scripted or interactive input, trimmed. EOF —
// including an immediately-closed stdin, which is what exec.Cmd hands a
// child process whose Stdin field was never set — returns "" rather than
// blocking or erroring, which is what makes "no input at all" behave like
// "the user pressed Enter" throughout this wizard.
func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

// envSkeleton documents the accepted format where the user edits it, and
// offers a line for every variable the wizard just configured.
//
// The parser accepts a deliberately narrow subset and rejects everything else by
// name, so the rules belong here rather than only in a doc: a user who writes
// `export FOO=bar` should find out from the file they are editing, not from an
// error after a restart.
//
// **The provider lines are derived from choices, never written in advance.**
// This file exists to be the place the value gets pasted into, so a hint naming
// a variable nothing reads sends the user to edit a line that will never be
// looked up — and the cloud path lets them type a name no constant could have
// guessed. Ollama and a skipped setup configure no key at all, and inventing one
// there would invent a requirement the vault does not have.
func envSkeleton(choices []providerChoice) string {
	var b strings.Builder
	b.WriteString(`# Secrets for this vault. Never commit this file.
#
# Accepted, and nothing else:
#
#   KEY=value          KEY is letters, digits and _, not starting with a digit
#   KEY="value"        quotes are stripped
#   KEY='value'        either kind
#   KEY=               a deliberate empty value
#   # a comment        first non-space character is #
#
# Rejected, with the line number: an export prefix, a missing '=', a quote that
# is never closed, the same KEY twice, and a bare # after an unquoted value
# (quote the value if the # is part of it).
#
# A variable already set in the environment always wins over this file.

`)

	if keys := providerKeyEnvs(choices); len(keys) > 0 {
		b.WriteString("# Paste the API key for this vault's provider here.\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "# %s=\n", k)
		}
	} else {
		// No provider key to name — a local Ollama needs none, and a skipped
		// setup has not chosen one yet. The line teaches where the name comes
		// from instead of guessing a vendor.
		b.WriteString("# A provider API key goes here, named by the api_key_env you set in nooma.yml.\n")
	}

	// Telegram has no wizard step, so nothing in choices can carry this name.
	// It stays a fixed suggestion precisely because nothing contradicts it.
	b.WriteString("# TELEGRAM_BOT_TOKEN=\n")
	return b.String()
}

// providerKeyEnvs returns the distinct API-key variable names in choices, in
// first-seen order. Distinct because the cloud path writes two providers — a
// chat model and an embedding model (design D15) — that read one key, and
// offering the same line twice would read as two different values to paste.
func providerKeyEnvs(choices []providerChoice) []string {
	var out []string
	seen := make(map[EnvVarName]bool, len(choices))
	for _, c := range choices {
		if c.APIKeyEnv == "" || seen[c.APIKeyEnv] {
			continue
		}
		seen[c.APIKeyEnv] = true
		out = append(out, string(c.APIKeyEnv))
	}
	return out
}
