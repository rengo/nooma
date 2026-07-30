package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// doctorCheck is one named diagnosis.
//
// Checks are values rather than a sequence of early returns, and that is the
// whole design (D10). Spec R13.2 requires every problem to be reported, not the
// first — written as `if err != nil { return err }` that requirement is
// discipline, and discipline decays the third time somebody adds a check in a
// hurry. As a slice of values the loop cannot short-circuit and a new check
// cannot forget to participate.
//
// docs/01-architecture.md calls doctor "what makes the binary feel cared for". A
// doctor that reports one problem per run makes the user iterate.
type doctorCheck struct {
	name string
	run  func(vault string, cfg *config.Config) error
}

var doctorChecks = []doctorCheck{
	{"configuration", checkConfiguration},
	{"permissions", checkPermissions},
	{"database integrity", checkIntegrity},
	{"schema version", checkSchema},
	{"bind", checkBindExposure},
}

// runDoctor diagnoses a vault without starting anything.
//
// What it deliberately does NOT check, per spec R13.4: provider connectivity and
// hardware. Providers arrive in M1, and "minimum hardware" is an open dated
// decision due before M6. A check that cannot be implemented honestly is worse
// than an absent one, because its passing means nothing — and doctor's whole
// value is that a green line can be believed.
func runDoctor(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { _, _ = fmt.Fprint(errOut, "usage: nooma doctor [vault]\n") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("doctor takes at most one vault path, got %d", fs.NArg())
	}

	vault, err := config.ResolveVault(fs.Arg(0))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "vault: %s\n\n", vault)

	// A configuration that will not load is reported as a failed check rather
	// than aborting the run, so the checks that do not depend on it still report.
	cfg, cfgErr := loadVaultConfig(vault)

	failed := 0
	for _, check := range doctorChecks {
		// A configuration that will not load fails its own check and makes the
		// rest unrunnable; one that loads still has to be validated, which is the
		// check's actual job. An earlier version of this switch returned cfgErr
		// for the configuration check and never called it — so a config that
		// loaded but was invalid passed a check named "configuration". The e2e
		// test caught it, which is the only reason it is not in this commit.
		var err error
		switch {
		case cfgErr != nil:
			err = cfgErr
		default:
			err = check.run(vault, cfg)
		}

		if err != nil {
			failed++
			_, _ = fmt.Fprintf(out, "  FAIL  %-18s %v\n", check.name, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "  ok    %s\n", check.name)
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(doctorChecks))
	}
	_, err = fmt.Fprintf(out, "\n%s looks healthy\n", vault)
	return err
}

func checkConfiguration(vault string, cfg *config.Config) error {
	return cfg.Validate(vault, os.LookupEnv)
}

// checkPermissions verifies the vault is writable, which is what `serve` will
// need and what a read-only mount or a wrong owner would break.
func checkPermissions(vault string, _ *config.Config) error {
	probe, err := os.CreateTemp(vault, ".nooma-doctor-*")
	if err != nil {
		return fmt.Errorf("the vault is not writable: %w", err)
	}
	name := probe.Name()
	_ = probe.Close()
	return os.Remove(name)
}

func checkIntegrity(vault string, cfg *config.Config) error {
	return withVault(vault, cfg, func(v *sqlite.Vault) error {
		return v.IntegrityCheck(context.Background())
	})
}

func checkSchema(vault string, cfg *config.Config) error {
	return withVault(vault, cfg, func(v *sqlite.Vault) error {
		version, err := v.SchemaVersion(context.Background())
		if err != nil {
			return err
		}
		if version <= 0 {
			return fmt.Errorf("the vault reports schema version %d, which no migration produces", version)
		}
		return nil
	})
}

// checkBindExposure reports ADR-0007's answer without starting a server, which is
// the point of having it here: the refusal to listen lives in `serve`, but a user
// should be able to ask "is this exposed?" before starting anything.
func checkBindExposure(_ string, cfg *config.Config) error {
	summary := cfg.Summary()
	for _, line := range strings.Split(summary, "\n") {
		if strings.HasPrefix(line, "bind:") {
			if strings.Contains(line, "exposed") {
				return fmt.Errorf("%s — reachable beyond this machine", strings.TrimSpace(line))
			}
			return nil
		}
	}
	return fmt.Errorf("the effective bind could not be determined")
}

func withVault(vault string, cfg *config.Config, fn func(*sqlite.Vault) error) error {
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("the database %s is missing", filepath.Base(dbPath))
	}

	v, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	return fn(v)
}
