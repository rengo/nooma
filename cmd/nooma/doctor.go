package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/testdata/llm"
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
	{"llm quality", checkLLMQuality},
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

// jsonTasks is the fixed, ordered set of tasks the quality gate checks —
// design D16: only capture_processing and relation_evaluation answer in
// JSON a production decoder can judge. embedding answers with a vector, a
// different question design D18 asks instead.
var jsonTasks = []string{"capture_processing", "relation_evaluation"}

// corpusTaskLabel maps a jsonTasks member to testdata/llm/format.md's own
// "task" field spelling. The corpus predates internal/config's task names
// and still labels capture-processing prompts "classify" (ADR-0002's own
// wording, "a fixed set of classify and judge prompts"); this is the one
// place the gate bridges the two vocabularies.
func corpusTaskLabel(task string) string {
	if task == "capture_processing" {
		return "classify"
	}
	return task
}

// checkLLMQuality is ADR-0002's structured-JSON quality gate (spec R5.1):
// it sends testdata/llm/'s embedded corpus, once per prompt, to whichever
// provider each of jsonTasks is bound to, and judges a task's prompts
// valid only when the production decoder reports zero degradations (spec
// R5.4) — never merely "did not error".
func checkLLMQuality(_ string, cfg *config.Config) error {
	cases, err := llm.Cases()
	if err != nil {
		return fmt.Errorf("loading the quality gate's prompt corpus: %w", err)
	}
	providers := qualityGateProviders(cfg, os.LookupEnv)
	return qualityGateError(runLLMQualityCheck(context.Background(), providers, cases, time.Now()))
}

// qualityGateProviders resolves, for each of jsonTasks, whether cfg binds a
// provider this binary can build that implements ports.LLMProvider —
// reusing wiring.go's own buildProvider rather than a second constructor. A
// task cfg does not bind, or binds to a provider this binary cannot build
// or that does not speak ports.LLMProvider, is simply absent from the
// returned map. spec R5.6 (link 16a-ii) turns that absence into "the gate
// reports nothing for that task" — runLLMQualityCheck already does that by
// construction below, iterating this map rather than jsonTasks itself.
func qualityGateProviders(cfg *config.Config, lookup func(string) (string, bool)) map[string]ports.LLMProvider {
	result := make(map[string]ports.LLMProvider)
	for _, task := range jsonTasks {
		binding, bound := cfg.Tasks[task]
		if !bound {
			continue
		}
		provider, present := cfg.Providers[binding.Provider]
		if !present {
			continue
		}
		client, err := buildProvider(provider, lookup)
		if err != nil {
			continue
		}
		llmClient, ok := client.(ports.LLMProvider)
		if !ok {
			continue
		}
		result[task] = llmClient
	}
	return result
}

// taskQualityResult is one jsonTasks member's own gate result — kept as a
// typed value distinct from doctorCheck's shared error-only {name, run}
// shape so spec R5.2's "the report names the task" and R5.4's "the report
// states the count" are provable directly against it (doctor_test.go), not
// parsed back out of a printed line runDoctor's own loop owns.
type taskQualityResult struct {
	task     string
	total    int
	clean    int
	failures []llmQualityFailure
}

func (r taskQualityResult) failed() bool { return r.clean < r.total }

// String is one task's own failure report — spec R5.4's "k of n prompts
// produced clean JSON" line, plus one line per failing case naming the
// field, the reason, and the case id (R5.4 Refinement 2) — never a single
// collapsed "bad JSON" verdict, and never folded into another task's own
// result (spec R5.2).
func (r taskQualityResult) String() string {
	report := fmt.Sprintf("%s: %d of %d prompts produced clean JSON", r.task, r.clean, r.total)
	for _, f := range r.failures {
		report += "\n    " + f.String()
	}
	return report
}

// reasonTransportError marks a Complete call that failed outright (a
// scripted or, at runtime, a real transport error) rather than a decode
// degradation. Link 16a-ii names this case "unreachable" distinctly from a
// JSON-fitness verdict (spec R5.7); this half folds it into a generic
// failure, which is as far as R5.1/R5.3/R5.4 alone ask for.
const reasonTransportError = "transport_error"

// llmQualityFailure is one case's own failed decode: the field that
// degraded (or "(response)" when nothing decoded at all), and why.
type llmQualityFailure struct {
	caseID string
	field  string
	reason string
}

// String names the failure kind doc 02 §5.1 treats as a distinct event —
// vocabulary (ReasonUnknownEnum) versus formatting (every other reason,
// including a response that failed to decode at all) — spec R5.4
// Refinement 2: a wrong-shaped value and an out-of-vocabulary value call
// for different user advice, and a gate that merges them throws that
// distinction away.
func (f llmQualityFailure) String() string {
	kind := "formatting"
	if f.reason == string(classify.ReasonUnknownEnum) {
		kind = "vocabulary"
	} else if f.reason == reasonTransportError {
		kind = "transport"
	}
	return fmt.Sprintf("%s: %s failure — field %q (%s)", f.caseID, kind, f.field, f.reason)
}

// runLLMQualityCheck is the gate's own decision logic, proven at L1/L2
// against a scripted ports.LLMProvider (doctor_test.go) — no test in this
// codebase calls a real provider (spec R5.5). It sends each corpus case
// matching a bound task's own corpus label exactly once (spec R5.4's
// "never retried") and decodes the live response with the same production
// decoder I14's own conformance suite already proves correct.
func runLLMQualityCheck(ctx context.Context, providers map[string]ports.LLMProvider, cases []llm.Case, now time.Time) []taskQualityResult {
	var results []taskQualityResult
	for _, task := range jsonTasks {
		provider, bound := providers[task]
		if !bound {
			continue
		}
		result := taskQualityResult{task: task}
		label := corpusTaskLabel(task)
		for _, c := range cases {
			if c.Task != label {
				continue
			}
			result.total++
			if failure := evaluateCase(ctx, provider, task, c, now); failure == nil {
				result.clean++
			} else {
				result.failures = append(result.failures, *failure)
			}
		}
		results = append(results, result)
	}
	return results
}

// evaluateCase sends one corpus case's prompt — never its response or
// error field, which llm.Case carries neither of (spec R5.3's MUST NOT
// made structural) — to provider exactly once, and reports clean only when
// the production decoder finds zero Degradation entries (spec R5.4).
func evaluateCase(ctx context.Context, provider ports.LLMProvider, task string, c llm.Case, now time.Time) *llmQualityFailure {
	resp, err := provider.Complete(ctx, ports.LLMRequest{Prompt: c.Prompt, Task: task})
	if err != nil {
		return &llmQualityFailure{caseID: c.ID, field: "(response)", reason: reasonTransportError}
	}

	var degradations []classify.Degradation
	switch task {
	case "capture_processing":
		classification, err := classify.Decode(resp.Text, now)
		if err != nil {
			return &llmQualityFailure{caseID: c.ID, field: "(response)", reason: string(classify.ReasonWrongType)}
		}
		degradations = classification.Degradations
	case "relation_evaluation":
		judgment, err := relation.DecodeJudgment(resp.Text)
		if err != nil {
			return &llmQualityFailure{caseID: c.ID, field: "(response)", reason: string(classify.ReasonWrongType)}
		}
		degradations = judgment.Degradations
	}
	if len(degradations) == 0 {
		return nil
	}
	d := degradations[0]
	return &llmQualityFailure{caseID: c.ID, field: d.Field, reason: string(d.Reason)}
}

// qualityGateError folds every jsonTasks result into checkLLMQuality's one
// return value — nil when every bound task's prompts all decoded clean
// (including when no task is bound at all, spec R5.6), a joined multi-line
// error otherwise, one task's own String() per line so a failure never
// reads as "the provider" in general (spec R5.2, ADR-0002).
func qualityGateError(results []taskQualityResult) error {
	var failed []string
	for _, r := range results {
		if r.failed() {
			failed = append(failed, r.String())
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return errors.New(strings.Join(failed, "\n"))
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
