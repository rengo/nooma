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

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/core/unit"
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
	{"task coverage", checkTaskCoverage},
	{"vault coverage", checkVaultCoverage},
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

		// doctorOKDetail marks success that still has something to say
		// (spec R5.6's qualityGateDetail; design D18b row 1's
		// taskCoverageDetail) — recognized here, before the general FAIL
		// branch, so it never counts toward failed and never prints as a
		// problem. This is the one narrow exception to "every non-nil err
		// is a FAIL"; every other check's err is untouched.
		if detail, ok := err.(doctorOKDetail); ok {
			_, _ = fmt.Fprintf(out, "  ok    %-18s %s\n", check.name, detail)
			continue
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

// qualityGateTimeout bounds every live call the quality gate sends (spec
// R5.7's second MUST): a provider that never answers must not make nooma
// doctor hang. Named here, rather than left as a bare literal, so the
// failure text below can state it — a user who sees "unreachable" should
// also see how long doctor was willing to wait.
const qualityGateTimeout = 10 * time.Second

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
	results := runLLMQualityCheck(context.Background(), providers, cases, time.Now(), qualityGateTimeout)
	if err := qualityGateError(results); err != nil {
		return err
	}
	// spec R5.6: the report states the configured-task count on success,
	// zero included, so a reader can tell "passed" from "did not run" —
	// scripts/core-coverage.sh's own "armed but vacuous" framing applied
	// to this check. len(providers) is already zero whenever
	// runLLMQualityCheck's own loop over jsonTasks iterated nothing above
	// — there is no branch here deciding pass vs fail on that count, only
	// what the success line goes on to say.
	return &qualityGateDetail{tasksConfigured: len(providers)}
}

// doctorOKDetail marks a check's success that still has something to say
// (spec R5.6's qualityGateDetail below; design D18b row 1's
// taskCoverageDetail). Both implement error only so they can travel through
// doctorCheck.run's existing single-return-value contract without widening
// it for every other check (spec R5.1's own constraint) — runDoctor's own
// loop recognizes the interface, not each concrete type, so a third such
// check does not need a third type assertion in that loop.
type doctorOKDetail interface {
	error
	doctorOK()
}

// qualityGateDetail marks a successful quality-gate run that still has
// something to say (spec R5.6).
type qualityGateDetail struct{ tasksConfigured int }

func (d *qualityGateDetail) Error() string {
	return fmt.Sprintf("(%d tasks configured)", d.tasksConfigured)
}

func (d *qualityGateDetail) doctorOK() {}

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

	// unreachable is non-empty when a transport-level failure stopped this
	// task's own run early (spec R5.7) — set instead of, never alongside,
	// total/clean/failures above, so a transport error is never folded
	// into or reported alongside a JSON-fitness verdict.
	unreachable string
}

func (r taskQualityResult) failed() bool {
	return r.unreachable != "" || r.clean < r.total
}

// String is one task's own report. When unreachable is set (spec R5.7) it
// is the whole story — never a "k of n" JSON-fitness line, because a
// transport error says nothing about whether this provider's JSON is any
// good. Otherwise it is spec R5.4's "k of n prompts produced clean JSON"
// line, plus one line per failing case naming the field, the reason, and
// the case id (R5.4 Refinement 2) — never a single collapsed "bad JSON"
// verdict, and never folded into another task's own result (spec R5.2).
func (r taskQualityResult) String() string {
	if r.unreachable != "" {
		return fmt.Sprintf("%s: %s", r.task, r.unreachable)
	}
	report := fmt.Sprintf("%s: %d of %d prompts produced clean JSON", r.task, r.clean, r.total)
	for _, f := range r.failures {
		report += "\n    " + f.String()
	}
	return report
}

// llmQualityFailure is one case's own failed decode: the field that
// degraded (or "(response)" when nothing decoded at all), and why. It
// never carries a transport failure — spec R5.7 reports that on
// taskQualityResult.unreachable instead, a different category with a
// different wording.
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
	}
	return fmt.Sprintf("%s: %s failure — field %q (%s)", f.caseID, kind, f.field, f.reason)
}

// runLLMQualityCheck is the gate's own decision logic, proven at L1/L2
// against a scripted ports.LLMProvider (doctor_test.go) — no test in this
// codebase calls a real provider (spec R5.5). Each bound task runs its own
// independent evaluateTask call (spec R5.2: one task's failure never skips
// or folds into a different task's own result).
func runLLMQualityCheck(ctx context.Context, providers map[string]ports.LLMProvider, cases []llm.Case, now time.Time, timeout time.Duration) []taskQualityResult {
	var results []taskQualityResult
	for _, task := range jsonTasks {
		provider, bound := providers[task]
		if !bound {
			continue
		}
		results = append(results, evaluateTask(ctx, provider, task, cases, now, timeout))
	}
	return results
}

// qualityGatePrompt builds the exact live prompt production sends for c,
// through the same builders production calls — classify.BuildPrompt for
// capture_processing, brain.JudgePrompt for relation_evaluation — never a
// corpus case's own separately recorded field.
//
// This closes the defect a live OpenAI key found before any test in this
// codebase did (project/quality-gate-sends-stub-prompts): the gate used to
// send c's own `prompt` field verbatim — a 60-84 byte fake-replay
// identifier, nothing like classify's real ~1550-byte prompt — and a real
// provider answered in prose every one of 21 times, because prose has no
// `{` for classify.Salvage to find. Building through the real function
// here, rather than through a second copy of its logic, means a future
// change to BuildPrompt or JudgePrompt reaches this gate automatically:
// there is no second prompt-construction path left to drift out of sync.
//
// now is cmd/nooma's own clock read (checkLLMQuality's caller), never a
// second time.Now() inside internal/core — classify.BuildPrompt only ever
// sees an instant passed to it as a plain argument (nooma-core: internal/core
// reads no OS state).
func qualityGatePrompt(task string, c llm.Case, now time.Time) string {
	switch task {
	case "capture_processing":
		// beliefs is always nil here, the same way captureRunner.at passes
		// nil (internal/brain/capture.go): nothing reads self_beliefs yet
		// (design D4 — derive is M2, seeding is M4).
		// The quality gate builds the prompt production builds, so it
		// needs §6's threshold too. The calibrated default rather than a
		// vault's configured one: doctor judges whether a provider can
		// answer this prompt's SHAPE, and a per-vault number would make
		// two vaults score the same provider differently on a question
		// that is not about them (ADR-0023).
		return classify.BuildPrompt(c.Message, nil, now, consolidation.DefaultWeightThreshold)
	case "relation_evaluation":
		candidates := make([]unit.Unit, len(c.Candidates))
		for i, cand := range c.Candidates {
			candidates[i] = unit.Unit{ID: cand.ID, Content: cand.Content}
		}
		return brain.JudgePrompt(unit.Unit{Content: c.Message}, candidates)
	default:
		// jsonTasks names only the two cases above; a third would need its
		// own real builder wired in here before it could join jsonTasks at
		// all, not a silent fallback to raw message text.
		return c.Message
	}
}

// evaluateTask sends one jsonTasks member's own corpus slice to provider,
// one prompt at a time, each call bounded by timeout (spec R5.7's second
// MUST: a provider that never answers must not make nooma doctor hang).
// The first transport-level failure stops this task's own run and reports
// it as unreachable — rather than continuing to send prompts to a
// provider that is not answering, and rather than counting that case
// toward the "k of n prompts produced clean JSON" line (spec R5.7's own
// MUST: unreachable is never folded into, or reported alongside, a
// JSON-fitness verdict).
func evaluateTask(ctx context.Context, provider ports.LLMProvider, task string, cases []llm.Case, now time.Time, timeout time.Duration) taskQualityResult {
	result := taskQualityResult{task: task}
	label := corpusTaskLabel(task)
	for _, c := range cases {
		if c.Task != label {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		resp, err := provider.Complete(callCtx, ports.LLMRequest{Prompt: qualityGatePrompt(task, c, now), Task: task, JSONOnly: true})
		cancel()
		if err != nil {
			result.unreachable = fmt.Sprintf("unreachable — %v (doctor waits up to %s per prompt)", err, timeout)
			return result
		}
		result.total++
		if failure := decodeCase(task, c, resp, now); failure == nil {
			result.clean++
		} else {
			result.failures = append(result.failures, *failure)
		}
	}
	return result
}

// decodeCase judges one corpus case's already-received response against
// the same production decoder I14's own conformance suite already proves
// correct — classify.Decode for capture_processing, relation.DecodeJudgment
// for relation_evaluation — reporting clean only when zero Degradation
// entries come back (spec R5.4). It never sees a transport failure:
// evaluateTask only calls this once provider.Complete has already
// returned a response — spec R5.7's own boundary between "unreachable"
// and "unsuitable".
func decodeCase(task string, c llm.Case, resp ports.LLMResponse, now time.Time) *llmQualityFailure {
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

// taskCoverageConsequence is checkTaskCoverage's own FAIL wording for an
// unbound task — one shared sentence, not one per task, because
// resolveTaskProviders (wiring.go, 13d's own conflict-log decision) resolves
// every member of tasksM1Consumes or none: RecallService.ScoredFor has no
// nil guard on its own embed field, so wireBrain returns nil, nil the
// moment ANY one of the three is unbound, and 13b's/13c's existing
// nil-Deps guards then answer every /capture and /recall request with 503
// — regardless of which task is the one left unbound.
//
// This corrects design D18b row 1's own quoted "embedding" wording
// ("capture will store units with no vector and recall will run on its
// lexical leg alone") — that text describes m1b's D8 degradation for a
// provider OUTAGE after wiring already succeeded, a different question
// (D18's own "fit") from the one this check answers ("configured"). Under
// 13d's fail-closed wireBrain, an unbound task never reaches D8's soft
// degradation at all: nothing is captured, not "captured without a
// vector." Tracked in tasks.md's own C21.1 — spec.md and design.md both
// still carry the stale wording as of this fix; this doctor row is the
// corrected one.
const taskCoverageConsequence = "POST /capture and POST /recall will answer 503 — nothing is captured, not a degraded capture — until every task capture and recall need is bound"

// taskCoverageConsolidateConsequence is what an unbound consolidation-only
// task actually costs, and it is a different thing entirely.
//
// Capture and recall keep working; what stops is the nightly pass, on one
// line printed to the serve console and nowhere else. Telling a user their
// captures will 503 when they will not sends them to debug the half of the
// binary that is fine — R5.4's Refinement 2, one layer out: two failures
// sharing a check still owe the reader their own advice.
const taskCoverageConsolidateConsequence = "consolidation will not be scheduled — capture and recall keep working, and the nightly pass that derives beliefs and merges duplicates does not run"

// taskCoverageConsequenceFor picks the consequence a given unbound task
// actually has. A task capture and recall need answers 503; one only a
// consolidation pass reads stops the scheduler.
func taskCoverageConsequenceFor(task string) string {
	for _, m1 := range tasksM1Consumes {
		if m1 == task {
			return taskCoverageConsequence
		}
	}
	return taskCoverageConsolidateConsequence
}

// taskCoverageDetail marks checkTaskCoverage's own success-with-something-
// to-say state (design D18b row 1's "no providers configured at all" row) —
// see doctorOKDetail above for why this implements error.
type taskCoverageDetail struct{}

func (taskCoverageDetail) Error() string { return "(no providers configured)" }
func (taskCoverageDetail) doctorOK()     {}

// checkTaskCoverage is design D18b row 1 (spec R6.3) — a pure configuration
// read, no provider call: it asks whether a provider is BOUND to every
// task tasksM1Consumes names, never whether the bound provider can do the
// job (checkLLMQuality's own question, D16 — "fit") or whether the vault's
// units are actually embedded (checkVaultCoverage's, row 2 — "effective").
// This is the row design D18 states "would have caught C9 before a single
// capture ran": a fresh vault with no providers configured at all is not
// broken (ok, with a note); a vault with SOME providers configured and one
// of tasksM1Consumes left unbound is.
//
// It reads tasksM1Consumes itself, not a restated copy —
// TestCheckTaskCoverageReadsTheSharedListNotACopy (tasks_test.go) proves it
// the same way TestResolveTaskProvidersReadsTheSharedListNotACopy and
// TestBindTasksReadsTheSharedListNotACopy already do for this list's other
// two readers.
//
// Its honest limit, stated here per golden_sets_test.go:164-176's
// proxy-announcement precedent: it would pass on a tasks.embedding entry
// naming a provider type that has no embedder at all — it checks that a
// task has *a* provider, never that the provider can embed. That is D16's
// question (fit), not this one (configured).
func checkTaskCoverage(_ string, cfg *config.Config) error {
	if len(cfg.Providers) == 0 {
		return taskCoverageDetail{}
	}

	var problems []string
	// The same union the wizard binds. Scoped to tasksM1Consumes this check
	// reported "ok task coverage" over the very vault whose scheduler
	// refused to start — a health check that passes a vault the binary
	// cannot fully run says the opposite of the truth.
	for _, task := range tasksTheBinaryRuns() {
		if _, bound := cfg.Tasks[task]; !bound {
			problems = append(problems, fmt.Sprintf("%s is unbound — %s", task, taskCoverageConsequenceFor(task)))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "\n"))
}

// checkVaultCoverage is design D18b row 2 (spec R6.3) — the runtime half of
// docs/03-data-model.md's own units<->embeddings<->fts consistency promise
// (the fts half stays M6's, doc 03's own delta). Unlike row 1, this makes
// one SQL query rather than reading configuration: it answers "did this
// vault actually end up with vectors" (effective), not "is something
// bound" (configured, row 1) or "is the bound provider any good"
// (fit, D16).
func checkVaultCoverage(vault string, cfg *config.Config) error {
	return withVault(vault, cfg, func(v *sqlite.Vault) error {
		count, err := sqlite.NewEmbeddingRepo(v).CountLiveWithoutEmbedding(context.Background())
		if err != nil {
			return fmt.Errorf("counting live units without an embedding: %w", err)
		}
		return vaultCoverageError(count)
	})
}

// vaultCoverageError is checkVaultCoverage's own decision logic, split out
// so the report text is testable without a real vault (doctor_test.go) —
// the same shape checkLLMQuality's qualityGateError already takes. It never
// sees an archived unit: CountLiveWithoutEmbedding already excludes them
// (I02's own read-side filter).
func vaultCoverageError(count int) error {
	if count == 0 {
		return nil
	}
	return fmt.Errorf("%d live units have no embedding; semantic recall cannot reach them", count)
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
