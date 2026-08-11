//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// mockConsolidateLLM stands in for a real Ollama listener, the same
// loopback in-process posture mockOllama (capture_recall_test.go) already
// takes for L4 — never docs/06-harness.md §3's "the network", never a real
// LLM (CLAUDE.md non-negotiable #5). Unlike mockOllama, this one answers
// two DIFFERENT judge calls distinguishably: ollama.Client's own wire
// shape carries only {model, prompt}, no task name, so the only way to
// tell derive's belief_derivation call from an ordinary classify call is
// the prompt text itself — "You derive self-beliefs" is
// consolidation.BuildDerivePrompt's own first line (prompt.go), unique to
// that one call site.
func mockConsolidateLLM(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/generate":
			var req struct {
				Prompt string `json:"prompt"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if strings.Contains(req.Prompt, "You derive self-beliefs") {
				// One derived belief — spec R5.8's CREATE half, decoded by
				// internal/brain's decodeDerivedBeliefs (consolidate.go).
				_, _ = fmt.Fprint(w, `{"model":"test-model","response":"{\"beliefs\":[{\"facet\":\"preference\",\"topic_key\":\"dry_cleaning\",\"content\":\"The user wants to remember to pick up the dry cleaning.\",\"confidence\":0.72}]}","done":true}`)
				return
			}
			// classify's own ordinary task fixture, reused verbatim from
			// mockOllama (capture_recall_test.go).
			_, _ = fmt.Fprint(w, `{"model":"test-model","response":"{\"type\":\"task\",\"normalized_content\":\"Pick up the dry cleaning\",\"weight\":0.6,\"decay_rate\":0.1}","done":true}`)
		case "/api/embed":
			_, _ = fmt.Fprint(w, `{"model":"test-model","embeddings":[[0.1,0.2,0.3]]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// consolidateConfig renders a nooma.yml binding every task
// tasksConsolidateConsumes (cmd/nooma/consolidate.go) needs to llmURL —
// the fully-configured fixture every consolidate test but the
// unbound-task one (TestConsolidate_RefusesUnboundTask) starts from.
func consolidateConfig(llmURL string) string {
	return fmt.Sprintf(`providers:
  local:
    type: ollama
    model: test-model
    endpoint: %s
tasks:
  capture_processing:
    provider: local
  relation_evaluation:
    provider: local
  belief_derivation:
    provider: local
  embedding:
    provider: local
`, llmURL)
}

// readVaultConfig opens vault read-only through the real store package —
// the same import test/integration's own L3 suite already makes from
// outside internal/store/** (.golangci.yml's sqlite-containment rule denies
// the literal go-sqlite3/database/sql imports, not this one) — and returns
// the config singleton row, so a test can assert on
// ConsolidationLastRunAt without the CLI printing internal state to stdout
// just to make it observable.
func readVaultConfig(t *testing.T, vault string) ports.VaultConfig {
	t.Helper()

	dbPath := vaultDBPath(t, vault)
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("opening %s read-only: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	cfg, err := sqlite.NewConfigRepo(db).Load(context.Background())
	if err != nil {
		t.Fatalf("loading the config row: %v", err)
	}
	return cfg
}

// vaultDBPath decodes vault's own nooma.yml the same way
// cmd/nooma/status.go's loadVaultConfig does, so readVaultConfig and
// readDecisionLog below open the identical database file the compiled
// binary just wrote to — no .env/ApplyEnv step, since nothing under test
// here reads a provider secret from the environment.
func vaultDBPath(t *testing.T, vault string) string {
	t.Helper()

	f, err := os.Open(filepath.Join(vault, config.ConfigFileName))
	if err != nil {
		t.Fatalf("opening %s: %v", filepath.Join(vault, config.ConfigFileName), err)
	}
	defer func() { _ = f.Close() }()

	cfg, err := config.Decode(f)
	if err != nil {
		t.Fatalf("decoding nooma.yml: %v", err)
	}
	cfg.ApplyDefaults()

	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		t.Fatalf("resolving database path: %v", err)
	}
	return dbPath
}

// TestConsolidate_Lock is spec R6.1 end to end: the compiled `nooma
// consolidate` subcommand, run against a vault a `serve` process already
// holds the write lock on, returns a clean, non-zero-exit error naming the
// holder — never a silent hang or a corrupted concurrent write. Against an
// unlocked vault, it succeeds.
func TestConsolidate_Lock(t *testing.T) {
	t.Run("a held vault refuses and names the holder", func(t *testing.T) {
		home, work := t.TempDir(), t.TempDir()
		vault := initVault(t, home, work, "pablo.nooma")

		holder := holdVaultLock(t, vault)

		_, stderr, err := nooma(t, home, work, "consolidate", vault)
		if err == nil {
			t.Fatal("consolidate succeeded against a vault a lock holder already holds")
		}
		if !strings.Contains(stderr, fmt.Sprint(holder)) {
			t.Errorf("the refusal does not name the holding PID %d:\n%s", holder, stderr)
		}
	})

	t.Run("an unlocked vault succeeds", func(t *testing.T) {
		llm := mockConsolidateLLM(t)

		home, work := t.TempDir(), t.TempDir()
		vault := initVault(t, home, work, "pablo.nooma")
		writeConfig(t, vault, consolidateConfig(llm.URL))

		_, stderr, err := nooma(t, home, work, "consolidate", vault)
		if err != nil {
			t.Fatalf("consolidate: %v\nstderr: %s", err, stderr)
		}

		if _, held, err := vaultlock.ReadHolder(vault); err != nil {
			t.Fatalf("ReadHolder after consolidate: %v", err)
		} else if held {
			t.Error("the vault is still reported as held after consolidate exited")
		}
	})
}

// TestConsolidate_WholePass is spec R6.2: the default invocation (no
// --phase) runs brain.ConsolidateService's full eight-phase pass and
// writes consolidation_last_run_at on completion (R5.4) — read back
// directly through ConfigRepo, since `nooma consolidate` prints no
// internal state to stdout that would make this observable any other way.
func TestConsolidate_WholePass(t *testing.T) {
	llm := mockConsolidateLLM(t)

	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	writeConfig(t, vault, consolidateConfig(llm.URL))

	if before := readVaultConfig(t, vault); before.ConsolidationLastRunAt != nil {
		t.Fatalf("a freshly initialized vault already has ConsolidationLastRunAt = %v", before.ConsolidationLastRunAt)
	}

	stdout, stderr, err := nooma(t, home, work, "consolidate", vault)
	if err != nil {
		t.Fatalf("consolidate: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	after := readVaultConfig(t, vault)
	if after.ConsolidationLastRunAt == nil {
		t.Error("consolidate did not write consolidation_last_run_at after a whole pass")
	}
}

// TestConsolidate_Phase is spec R6.3: a per-phase invocation validates its
// argument through consolidation.ParsePhase — never a second, CLI-local
// phase-name vocabulary — runs exactly that phase, and leaves
// consolidation_last_run_at untouched (R5.4's own MUST NOT, restated at
// the CLI boundary). An unknown name errors cleanly through
// consolidation.ErrUnknownPhase, naming the rejected value.
func TestConsolidate_Phase(t *testing.T) {
	t.Run("a known phase runs and leaves the timestamp untouched", func(t *testing.T) {
		llm := mockConsolidateLLM(t)

		home, work := t.TempDir(), t.TempDir()
		vault := initVault(t, home, work, "pablo.nooma")
		writeConfig(t, vault, consolidateConfig(llm.URL))

		stdout, stderr, err := nooma(t, home, work, "consolidate", "--phase=archive", vault)
		if err != nil {
			t.Fatalf("consolidate --phase=archive: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		after := readVaultConfig(t, vault)
		if after.ConsolidationLastRunAt != nil {
			t.Errorf("a per-phase run wrote consolidation_last_run_at = %v, want untouched", after.ConsolidationLastRunAt)
		}
	})

	t.Run("an unknown phase errors cleanly and names the rejected value", func(t *testing.T) {
		home, work := t.TempDir(), t.TempDir()
		vault := initVault(t, home, work, "pablo.nooma")

		_, stderr, err := nooma(t, home, work, "consolidate", "--phase=not-a-real-phase", vault)
		if err == nil {
			t.Fatal("consolidate --phase=not-a-real-phase succeeded")
		}
		if !strings.Contains(stderr, "not-a-real-phase") {
			t.Errorf("the refusal does not name the rejected phase:\n%s", stderr)
		}
	})
}

// TestConsolidate_NewFileHasNoSecondPhaseVocabulary hand-verifies, for this
// PR's own new file, the property
// TestI11_NoCallerOutsideConsolidationListsThePhaseNames
// (test/conformance/i11_consolidation_phase_order_test.go) already proves
// automatically across the whole tree: consolidate.go never lists two or
// more of the eight phase-name string literals — every name it needs comes
// from consolidation.Phase.String() or consolidation.ParsePhase, never a
// restated copy.
func TestConsolidate_NewFileHasNoSecondPhaseVocabulary(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "cmd", "nooma", "consolidate.go"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"expire_incomplete", "archive", "strengthen", "connect", "derive", "reweight", "pattern_eval", "learn"}
	found := 0
	for _, n := range names {
		if strings.Contains(string(src), `"`+n+`"`) {
			found++
		}
	}
	if found >= 2 {
		t.Errorf("cmd/nooma/consolidate.go contains %d of the eight phase-name string literals, want at most 1 (I11, R6.3)", found)
	}
}
