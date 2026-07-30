package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vaultTree builds a real directory tree in a temp dir. Paths ending in "/" are
// plain directories; anything else is an empty file. A vault is any directory
// containing a nooma.yml, so "pablo.nooma/nooma.yml" makes one.
//
// Real directories rather than an in-memory filesystem, because resolution's job
// is to be right about a filesystem, and the cheapest honest fake of a filesystem
// is a filesystem. It stays L1: no database, no network, no subprocess.
func vaultTree(t *testing.T, entries ...string) string {
	t.Helper()

	root := t.TempDir()
	for _, e := range entries {
		p := filepath.Join(root, filepath.FromSlash(e))
		if strings.HasSuffix(e, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// env builds the injected environment D7 describes. There is deliberately no
// executable member: R6.6 forbids resolution from ever consulting the binary's
// own directory, and the cleanest way to guarantee that is to give the resolver
// no way to ask.
func env(cwd, home string, vars map[string]string) environment {
	return environment{
		getenv:  func(k string) string { return vars[k] },
		getwd:   func() (string, error) { return cwd, nil },
		homeDir: func() (string, error) { return home, nil },
		readDir: os.ReadDir,
	}
}

func TestResolveExplicitArgument(t *testing.T) {
	t.Parallel()

	root := vaultTree(t, "pablo.nooma/nooma.yml", "work.nooma/nooma.yml")
	want := filepath.Join(root, "pablo.nooma")

	t.Run("an absolute path", func(t *testing.T) {
		t.Parallel()

		got, err := resolveVault(want, env(root, root, nil))
		if err != nil {
			t.Fatalf("resolveVault: %v", err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// R6.4: `nooma serve pablo.nooma` from the directory holding it is the form
	// every user already knows. Without this clause it would work by accident of
	// implementation rather than by contract.
	t.Run("a relative path resolves against the working directory", func(t *testing.T) {
		t.Parallel()

		got, err := resolveVault("pablo.nooma", env(root, root, nil))
		if err != nil {
			t.Fatalf("resolveVault: %v", err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("the result is always absolute", func(t *testing.T) {
		t.Parallel()

		got, err := resolveVault("pablo.nooma", env(root, root, nil))
		if err != nil {
			t.Fatalf("resolveVault: %v", err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("got %q, which is relative; sqlite.Open rejects a relative path", got)
		}
	})

	// R6.4's MUST NOT. A typo in an explicit path must fail loudly, not fall
	// through and open a different brain — which is R6.2's failure mode arriving
	// by another route.
	t.Run("a path that is not a vault fails without falling through", func(t *testing.T) {
		t.Parallel()

		_, err := resolveVault(filepath.Join(root, "typo.nooma"), env(root, root, nil))
		if err == nil {
			t.Fatal("resolveVault accepted a path that is not a vault")
		}
		if !strings.Contains(err.Error(), "typo.nooma") {
			t.Errorf("error does not name the path the user gave:\n%v", err)
		}
		// If it had fallen through, it would have found pablo.nooma or work.nooma.
		if strings.Contains(err.Error(), "pablo.nooma") {
			t.Errorf("resolution fell through to another location:\n%v", err)
		}
	})
}

func TestResolveEnvironmentVariable(t *testing.T) {
	t.Parallel()

	root := vaultTree(t, "pablo.nooma/nooma.yml", "beside.nooma/nooma.yml")
	want := filepath.Join(root, "pablo.nooma")

	got, err := resolveVault("", env(root, root, map[string]string{"NOOMA_VAULT": want}))
	if err != nil {
		t.Fatalf("resolveVault: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	t.Run("an explicit argument wins over the variable", func(t *testing.T) {
		t.Parallel()

		arg := filepath.Join(root, "beside.nooma")
		got, err := resolveVault(arg, env(root, root, map[string]string{"NOOMA_VAULT": want}))
		if err != nil {
			t.Fatalf("resolveVault: %v", err)
		}
		if got != arg {
			t.Errorf("got %q, want the explicit argument %q", got, arg)
		}
	})
}

// TestResolveAscendsFromTheWorkingDirectory is the step the design went through
// three revisions to get right, and this is the case that killed the previous
// two. `cd pablo.nooma/attachments && nooma status` is an ordinary action; without
// an ascent it falls through to ~/.nooma and can open a DIFFERENT vault in
// silence — the worst failure this component can produce.
func TestResolveAscendsFromTheWorkingDirectory(t *testing.T) {
	t.Parallel()

	root := vaultTree(t,
		"pablo.nooma/nooma.yml",
		"pablo.nooma/attachments/deep/",
		".nooma/other.nooma/nooma.yml", // the home fallback, deliberately different
	)
	want := filepath.Join(root, "pablo.nooma")

	cases := []struct {
		name string
		cwd  string
	}{
		{"the cwd is the vault", filepath.Join(root, "pablo.nooma")},
		{"one level inside it", filepath.Join(root, "pablo.nooma", "attachments")},
		{"two levels inside it", filepath.Join(root, "pablo.nooma", "attachments", "deep")},
		{"the cwd holds the vault", root},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveVault("", env(tc.cwd, root, nil))
			if err != nil {
				t.Fatalf("resolveVault: %v", err)
			}
			if got != want {
				t.Errorf("got %q, want the nearest vault %q — a different one was opened", got, want)
			}
		})
	}
}

// TestResolveNearestWins pins the ordering the ascent implies. A vault inside a
// vault is unusual and must not be surprising: standing in the inner one means
// the inner one.
func TestResolveNearestWins(t *testing.T) {
	t.Parallel()

	root := vaultTree(t, "outer.nooma/nooma.yml", "outer.nooma/inner.nooma/nooma.yml")

	got, err := resolveVault("", env(filepath.Join(root, "outer.nooma", "inner.nooma"), root, nil))
	if err != nil {
		t.Fatalf("resolveVault: %v", err)
	}
	if want := filepath.Join(root, "outer.nooma", "inner.nooma"); got != want {
		t.Errorf("got %q, want the nearest %q", got, want)
	}
}

// TestResolveCandidateCount is R6.2. The binary never chooses: opening the wrong
// brain is the worst failure this step can produce, and picking a candidate would
// make it silent.
func TestResolveCandidateCount(t *testing.T) {
	t.Parallel()

	t.Run("two candidates in one directory fail, naming both", func(t *testing.T) {
		t.Parallel()

		root := vaultTree(t, "pablo.nooma/nooma.yml", "work.nooma/nooma.yml", "here/")

		_, err := resolveVault("", env(filepath.Join(root, "here"), root, nil))
		if err == nil {
			t.Fatal("resolveVault chose between two candidates")
		}
		for _, want := range []string{"pablo.nooma", "work.nooma"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error does not list %q, so the user cannot tell what to pass:\n%v", want, err)
			}
		}
	})

	t.Run("no vault anywhere fails, pointing at nooma init", func(t *testing.T) {
		t.Parallel()

		root := vaultTree(t, "empty/")

		_, err := resolveVault("", env(filepath.Join(root, "empty"), root, nil))
		if err == nil {
			t.Fatal("resolveVault found a vault where there is none")
		}
		if !strings.Contains(err.Error(), "nooma init") {
			t.Errorf("error does not tell the user how to create one:\n%v", err)
		}
	})
}

// TestResolveOnlyDirectoriesAreCandidates is R6.3. A file named like a vault is
// not a vault, and counting it would turn a stray download into an ambiguity.
func TestResolveOnlyDirectoriesAreCandidates(t *testing.T) {
	t.Parallel()

	root := vaultTree(t, "decoy.nooma", "real.nooma/nooma.yml", "here/")

	got, err := resolveVault("", env(root, root, nil))
	if err != nil {
		t.Fatalf("resolveVault: %v", err)
	}
	if want := filepath.Join(root, "real.nooma"); got != want {
		t.Errorf("got %q, want %q — a file named decoy.nooma was counted", got, want)
	}
}

// TestResolveNeverTreatsDotNoomaAsACandidate is R6.7, and it only became
// reachable when step 3 started ascending: the walk now passes through $HOME,
// where ~/.nooma lives. The glob *.nooma matches the name .nooma, because * also
// matches the empty string — so the container looks like a candidate by name.
//
// It has no nooma.yml, so the predicate would reject it anyway. The requirement
// is that it must not be REPORTED as a broken vault: ~/.nooma is doing its job,
// and sending the user to repair it would be a bug in the message.
func TestResolveNeverTreatsDotNoomaAsACandidate(t *testing.T) {
	t.Parallel()

	root := vaultTree(t, ".nooma/pablo.nooma/nooma.yml", "work/")
	want := filepath.Join(root, ".nooma", "pablo.nooma")

	got, err := resolveVault("", env(filepath.Join(root, "work"), root, nil))
	if err != nil {
		t.Fatalf("resolveVault: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want the home vault %q", got, want)
	}

	t.Run("and a hidden vault is still a candidate", func(t *testing.T) {
		t.Parallel()

		hidden := vaultTree(t, ".work.nooma/nooma.yml", "here/")
		got, err := resolveVault("", env(filepath.Join(hidden, "here"), hidden, nil))
		if err != nil {
			t.Fatalf("resolveVault: %v", err)
		}
		if want := filepath.Join(hidden, ".work.nooma"); got != want {
			t.Errorf("got %q, want %q — the exclusion must be the literal name, not any dotfile", got, want)
		}
	})
}

// TestResolveHomeFallback is step 4, reached only when the ascent finds nothing.
func TestResolveHomeFallback(t *testing.T) {
	t.Parallel()

	home := vaultTree(t, ".nooma/pablo.nooma/nooma.yml")
	elsewhere := t.TempDir()

	got, err := resolveVault("", env(elsewhere, home, nil))
	if err != nil {
		t.Fatalf("resolveVault: %v", err)
	}
	if want := filepath.Join(home, ".nooma", "pablo.nooma"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolvePrecedence drives the four steps against one tree where every step
// could succeed, and asserts the earlier one wins each time.
func TestResolvePrecedence(t *testing.T) {
	t.Parallel()

	root := vaultTree(t,
		"arg.nooma/nooma.yml",
		"envvar.nooma/nooma.yml",
		"cwd.nooma/nooma.yml",
		".nooma/home.nooma/nooma.yml",
		"here/",
	)
	cwd := filepath.Join(root, "here")

	all := map[string]string{"NOOMA_VAULT": filepath.Join(root, "envvar.nooma")}

	t.Run("argument beats everything", func(t *testing.T) {
		t.Parallel()

		got, _ := resolveVault(filepath.Join(root, "arg.nooma"), env(cwd, root, all))
		if want := filepath.Join(root, "arg.nooma"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("the variable beats the ascent", func(t *testing.T) {
		t.Parallel()

		got, _ := resolveVault("", env(cwd, root, all))
		if want := filepath.Join(root, "envvar.nooma"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// With three candidates beside `here`, the ascent must refuse rather than
	// choose — which is itself the proof that it reached step 3 instead of
	// skipping to the home fallback.
	t.Run("the ascent refuses to choose, and never reaches home", func(t *testing.T) {
		t.Parallel()

		_, err := resolveVault("", env(cwd, root, nil))
		if err == nil {
			t.Fatal("resolveVault chose among three candidates")
		}
		if strings.Contains(err.Error(), "home.nooma") {
			t.Errorf("resolution fell through to the home fallback despite candidates in the ascent:\n%v", err)
		}
	})
}
