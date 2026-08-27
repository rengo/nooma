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
	"time"

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
// unbound-task one (TestConsolidate_RefusesUnboundTaskBeforeTheLock) starts from.
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
  chat:
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

// readDecisionLog opens vault read-only the same way readVaultConfig does
// and returns every row decision_log holds, oldest first — the direct read
// spec R6.4's own exit criterion needs: "run the pass by hand on a vault
// and read the decision_log."
func readDecisionLog(t *testing.T, vault string) []ports.Decision {
	t.Helper()

	dbPath := vaultDBPath(t, vault)
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("opening %s read-only: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	decisions, err := sqlite.NewDecisionLog(db).Since(context.Background(), time.Time{}, 1000)
	if err != nil {
		t.Fatalf("reading decision_log: %v", err)
	}
	return decisions
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
		llm := mockConsolidateLLM(t)

		home, work := t.TempDir(), t.TempDir()
		vault := initVault(t, home, work, "pablo.nooma")
		writeConfig(t, vault, consolidateConfig(llm.URL))

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

// TestConsolidate_RefusesUnboundTaskBeforeTheLock is design §7.2: a vault
// with an unbound task (here, belief_derivation — no
// relation_evaluation/embedding gap needed to prove the same point)
// refuses before taking the lock, naming the unbound task. `consolidate`'s
// posture deliberately diverges from `serve`'s degrade-and-503 here — a
// pass that silently skipped derive because no provider was bound would
// still write consolidation_last_run_at as though a full pass had run,
// corrupting the next pass's own `since` (design §7.2's own reasoning).
//
// Proven "before the lock" rather than merely "the vault is refused"
// (which a post-lock check would also produce): the vault's write lock is
// already held by THIS test process when consolidate runs. If the refusal
// happened after attempting the lock, the error would be about a held
// vault, not about the unbound task — so the assertion below on the error
// text is the ordering proof, not a restatement of it.
func TestConsolidate_RefusesUnboundTaskBeforeTheLock(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	writeConfig(t, vault, `providers:
  local:
    type: ollama
    model: test-model
    endpoint: http://127.0.0.1:1
tasks:
  relation_evaluation:
    provider: local
  embedding:
    provider: local
`)

	holdVaultLock(t, vault)

	_, stderr, err := nooma(t, home, work, "consolidate", vault)
	if err == nil {
		t.Fatal("consolidate succeeded against a vault missing belief_derivation's binding")
	}
	if !strings.Contains(stderr, "belief_derivation") {
		t.Errorf("the refusal does not name the unbound task:\n%s", stderr)
	}
	if strings.Contains(stderr, "already in use") {
		t.Errorf("the refusal is about the held lock, not the unbound task — the check ran after attempting the lock, not before:\n%s", stderr)
	}
}

// TestConsolidate_ExitCriterion is spec R6.4 — m2c's own exit criterion,
// quoting the proposal directly: "run the pass by hand on a vault and read
// the decision_log." A minimal fixture vault is seeded through the REAL
// capture path (`nooma serve` + `nooma capture`, never a repo-constructed
// row — the m2d demo golden set is explicitly out of m2c's scope), then
// run through `nooma consolidate`. The pass must exit 0, and decision_log
// must gain at least one row whose rationale is a legible sentence, not a
// code — read back directly through DecisionLog.Since, since
// `renderConsolidateReport` prints only which phase(s) ran, never a
// decision's own text.
//
// The one captured unit is fresh (`LastTouchedAt` is `now`), StatusPool
// (a fresh capture is always live — classify/tounit_test.go's own
// comment), and consolidation_last_run_at is unset on a freshly
// initialized vault, so `since` is nil and every live unit is eligible:
// SelectConnectSources (internal/core/consolidation/connect.go) filters
// only on status and `since`, never on age. That makes derive's own
// source selection (design §7.3) the phase most likely to produce an
// effect from exactly one capture, with no elapsed-time or paired-unit
// precondition the way strengthen (co-use window) or connect (a related
// pair) would need — the fixture design.md §6.3's task list names as one
// example among several ("e.g. a unit old enough for strengthen's co-use
// window, or a relation pair connect can find"), not the only one.
func TestConsolidate_ExitCriterion(t *testing.T) {
	llm := mockConsolidateLLM(t)

	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	port := freePort(t)
	writeConfig(t, vault, fmt.Sprintf(`server:
  bind: 127.0.0.1
  http_port: %d
providers:
  local:
    type: ollama
    model: test-model
    endpoint: %s
tasks:
  capture_processing:
    provider: local
  relation_evaluation:
    provider: local
  chat:
    provider: local
  belief_derivation:
    provider: local
  embedding:
    provider: local
`, port, llm.URL))

	// Seed through the real capture path: nooma capture proxies over HTTP
	// to a running nooma serve (design D11) — never a second direct-vault
	// writer, so this is the identical write path a real user's capture
	// would take.
	serve := startServe(t, home, vault, port)
	stdout, stderr, err := nooma(t, home, work, "capture", "Pick up the dry cleaning on Friday", vault)
	if err != nil {
		t.Fatalf("capture: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "stored") {
		t.Fatalf("capture did not store a unit:\n%s", stdout)
	}

	// Release the lock before consolidate needs it — killing the process
	// releases the flock the same way a normal exit would (this test does
	// not need to prove a CLEAN shutdown, only that the lock is free
	// afterward; TestServeReleasesTheLockOnSignal already proves the clean
	// path).
	_ = serve.Process.Kill()
	_ = serve.Wait()

	stdout, stderr, err = nooma(t, home, work, "consolidate", vault)
	if err != nil {
		t.Fatalf("consolidate: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	decisions := readDecisionLog(t, vault)

	// The criterion is what the PASS produced, not what the vault holds.
	// Seeding goes through the real capture path, so capture.* rows already
	// exist before consolidate runs; a check over the whole table is
	// satisfied by those alone and would keep passing if consolidation
	// regressed to producing nothing at all. spec R6.4's own words are
	// "decision_log GAINS at least one row" — so only rows this pass wrote
	// count, and a consolidate.* action is what makes a row this pass's.
	var gained []ports.Decision
	for _, d := range decisions {
		if strings.HasPrefix(string(d.Action), "consolidate.") {
			gained = append(gained, d)
		}
	}
	if len(gained) == 0 {
		t.Fatalf("no consolidate.* row in decision_log — the exit criterion requires the PASS to produce at least one real effect on the fixture, and %d pre-existing capture.* row(s) do not count (spec R6.4): %+v", len(decisions), decisions)
	}

	legible := false
	for _, d := range gained {
		// "Legible sentence, not a code": a bare code (an Action value like
		// "consolidate.derive.belief_created") has no space; every
		// rationale this file's own record() call sites build (design §7.5)
		// is a full sentence built with fmt.Sprintf.
		if strings.Contains(d.Rationale, " ") {
			legible = true
			t.Logf("legible decision_log row: action=%s rationale=%q", d.Action, d.Rationale)
		}
	}
	if !legible {
		t.Fatalf("the pass wrote %d consolidate.* row(s), none with a legible (space-containing) rationale: %+v", len(gained), gained)
	}
}
