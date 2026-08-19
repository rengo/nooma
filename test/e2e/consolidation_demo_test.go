//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/goldenset"
)

// demoConsolidationCaseID names the one corpus this whole file drives —
// design §8.1's owner ruling (#6 in this change's tasks.md preamble): one
// corpus proves archive, connect AND derive together, never three separate
// ones. It is PR 8's own seed case, extended here (two more capture_script
// entries) rather than replaced — Judgment Day on link 8 (JD-737) already
// verified captures 0/1's own archival math against this exact id.
const demoConsolidationCaseID = "dry-cleaning-and-ambiguous-contract-request"

// demoEmbedModel is the fixed model name both embeddings.LoadIndex and
// fakeprovider.NewEmbeddingFake are constructed with — test/integration's
// own consolidateITEmbedModel precedent (consolidate_expire_incomplete_test.go),
// reimplemented locally since that identifier is unexported in another
// package.
const demoEmbedModel = "demo-embed-fake-v1"

// demoT0 anchors demoConsolidationCaseID's own capture_script offsets — a
// value the case's own JSON deliberately does NOT carry (format.md has no
// such field; Judgment Day on link 8, JD-737, found and disclosed exactly
// this gap, leaving it to this PR to pin).
//
// MECHANICAL, not merely a comment — but read the CORRECTED block below
// for exactly which failure lands where. TestDemo_ArchiveFires runs the
// real capture-then-consolidate pipeline over (demoT0, the case's own
// "now") every time this package is tested, so a future edit that moves
// demoT0 (or the case's own offsets) too close together fails LOUDLY,
// never silently — though a timing edit specifically surfaces first as
// connect-script exhaustion inside Consolidate(), not at the archive
// assertion itself.
//
// CORRECTED (Judgment Day, JD-9a-01): an earlier version of this comment
// claimed the mechanism above was already enforced by TestDemo_ArchiveFires,
// citing capture 0's own ~6.73-day threshold as the margin being protected.
// That was wrong about WHICH assertion did the protecting. The test's
// original assertion counted decision_log rows (archived == 0) rather than
// checking the case's own declared expected.archived indices ([0, 1]), so it
// only required "at least one" capture to archive — the weaker of the two
// thresholds below, not the stronger one this comment described. Both
// judges and the orchestrator verified: at demoT0 = now - 3 days, capture 0
// sits at effective weight ~0.6025 (does NOT archive) while capture 1 sits
// at ~0.4912 (archives), so the old "archived >= 1" assertion PASSED even
// though the case's own expected.archived says both must archive — the
// comment's claimed ~3.27-day margin was actually ~7.18 days on the
// assertion as shipped.
//
// TestDemo_ArchiveFires now reads expected.archived from the loaded case
// and requires a matching ActionArchiveArchived row for each declared
// index, so both captures must archive for the test to pass. With that
// fix, the BINDING constraint (the harder of the two to satisfy) really is
// capture 0's own math, restoring the mechanism this comment always
// intended: capture 0 is classify-person-ref-ambiguous-ana (weight 0.7,
// decay_rate 0.05); solving 0.7*exp(-0.05*d) =
// consolidation.DefaultWeightThreshold (0.5) gives d ~= 6.73 days, so demoT0
// must sit at least that far before the case's own "now"
// (2026-02-11T09:00:00Z) or capture 0 never archives. Capture 1
// (classify-pick-up-dry-cleaning, weight 0.6, decay_rate 0.1, offset +24h)
// crosses the same threshold sooner, at ~2.823 days after demoT0 — not the
// binding constraint once both indices are checked, but the one the old,
// weaker assertion actually depended on. Fixed here at 10 days before "now",
// comfortably past capture 0's threshold (JD-737's own table: 7 days already
// archives at effective weight ~0.493; 10 days leaves more margin at
// ~0.4246).
//
// One more disclosed wrinkle, found while proving the strengthened guard is
// genuine (see TestDemo_ArchiveFires's own probe record in
// openspec/changes/m2d-scheduler-demo/tasks.md, PR 9a's Judgment Day
// correction round): a close-demoT0 perturbation does not always fail
// cleanly on this file's own archive assertion. SelectConnectSources' own
// `since` filter gates only which units are considered connect SOURCES,
// never the candidate search itself (connectPairsForSource ->
// RecallService.ScoredFor, whose only liveness filter is LiveByIDs). A
// capture that stays live because it missed the archive threshold becomes
// an extra connect candidate, which can drive more judge calls than
// runDemoPass's fixed two-entry passJudge script allows —
// fakeprovider.Complete's own t.Fatalf then fires INSIDE Consolidate()
// itself, and runtime.Goexit() aborts the test goroutine before this file's
// own archive assertion ever runs. A future regression here may therefore
// surface first as "unscripted Complete call" rather than a missing
// ActionArchiveArchived row — both are real signals of the same underlying
// problem (demoT0 sitting too close to "now"), but only one of them is this
// comment's own archive-clause failure.
var demoT0 = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)

// steppingClock is a mutable ports.Clock — design D6's own "a fake clock
// whose reading advances per capture" (design §8.1, overturning spec R4.2's
// literal "authored directly" reading): the corpus-driving test sets now
// before every CaptureService.Capture call and again before the single
// Consolidate call, rather than reading a real clock. CaptureService and
// ConsolidateService each still read it exactly once per call, unchanged
// (spec R0.2) — this fake only changes what that one read returns between
// calls, never within one.
type steppingClock struct{ now time.Time }

func (c *steppingClock) Now() time.Time { return c.now }

// demoIDs is a deterministic ports.IDGen — test/integration's own
// counterIDs precedent (consolidate_expire_incomplete_test.go), widened to
// four digits since this file's own corpus generates more ids across four
// captures plus one consolidation pass than a single rune digit covers.
type demoIDs struct{ n int }

func (g *demoIDs) New() string {
	g.n++
	return fmt.Sprintf("demo-id-%04d", g.n)
}

// llmCasesDir and consolidationCasesDir resolve their respective testdata
// directories from repoRoot (version_test.go, this package) —
// test/conformance's own testdataLLMCasesDir precedent, reimplemented
// locally since that helper is unexported in a different package.
func llmCasesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "llm", "cases")
}

func consolidationCasesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "consolidation", "cases")
}

// loadDemoCase loads demoConsolidationCaseID through goldenset.Load — the
// same strict, DisallowUnknownFields decode every other golden-set consumer
// in this repo uses, proving format.md's own contract on the one real file
// this test drives (design §8.2).
func loadDemoCase(t *testing.T) goldenset.ConsolidationExample {
	t.Helper()
	var ex goldenset.ConsolidationExample
	path := filepath.Join(consolidationCasesDir(t), demoConsolidationCaseID+".json")
	if err := goldenset.Load(path, &ex); err != nil {
		t.Fatalf("loading consolidation case %q: %v", demoConsolidationCaseID, err)
	}
	return ex
}

// demoVault is what driveDemoCorpus returns: a real, migrated *sqlite.Vault
// (design §8.1's own note — test/e2e/** is not in sqlite-containment's
// exception list, so this file constructs repos through their real
// sqlite.NewXxx constructors and never imports database/sql itself) plus
// the repo handles TestDemo_ArchiveFires and TestDemo_DeriveBeliefExists
// read back from, and unitIDs — the real ids CaptureService.Capture
// returned for each capture_script entry, in order (format.md's own "Why
// indices, not unit IDs" section: a case file authored before any test runs
// cannot know these in advance).
// ids is shared with runDemoPass (never a second, independently-restarted
// demoIDs) — decision_log.id is a PRIMARY KEY across the whole vault, not
// scoped per service, so two counters that each restart at "demo-id-0001"
// collide the moment the pass tries to record its own first decision over
// rows capture already wrote (ports.ErrDecisionExists).
type demoVault struct {
	vault     *sqlite.Vault
	units     *sqlite.UnitRepo
	relations *sqlite.RelationRepo
	decisions *sqlite.DecisionLog
	config    *sqlite.ConfigRepo
	selfModel *sqlite.SelfModelRepo
	state     *sqlite.StateRepo
	lexical   *sqlite.Search
	index     *brain.Index
	embed     *fakeprovider.Fake
	ids       *demoIDs
	unitIDs   []string
}

// driveDemoCorpus builds a fresh vault and drives ex's whole capture_script
// through the real brain.CaptureService.Capture, one call per entry, under
// clock set to demoT0 plus each entry's own offset (design §8.1/D6) — never
// a hand-authored unit row, which is precisely why this exists: only the
// real capture path populates the vector index and the FTS table connect's
// RecallService.ScoredFor needs (design §8.1 item 2).
//
// captureLLM scripts every capture-time provider call this drives (design
// D7, spec R4.3): the entry's own llm_case_id, then, for every entry but
// the first, one relation_evaluation "new"-outcome response —
// capture.go's own judgeRelation fires whenever recall finds a candidate,
// and RecallService's vector leg is an unconditional top-K
// (internal/core/recall.Search has no similarity gate), so once the corpus
// holds more than one live unit, every later capture finds every earlier
// one as a "candidate" regardless of actual content. Scripting "new"
// (relation-no-match-for-dry-cleaning, already committed) keeps every one
// of those calls a safe no-persist, no-real-id-needed response — this
// file's whole point is the CONSOLIDATE-time judge calls (connect, derive),
// not capture-time dedup, which spec R4.4 never asks this corpus to prove.
func driveDemoCorpus(t *testing.T, ex goldenset.ConsolidationExample) demoVault {
	t.Helper()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { _ = v.Close() })

	units := sqlite.NewUnitRepo(v)
	embeddings := sqlite.NewEmbeddingRepo(v)
	lexical := sqlite.NewSearch(v)
	relations := sqlite.NewRelationRepo(v)
	decisions := sqlite.NewDecisionLog(v)
	signals := sqlite.NewSignalRepo(v)

	loaded, err := embeddings.LoadIndex(ctx, demoEmbedModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", demoEmbedModel, err)
	}
	index := brain.NewIndex(loaded)
	embed := fakeprovider.NewEmbeddingFake(demoEmbedModel)

	captureScript := make([]string, 0, len(ex.CaptureScript)*2)
	for i, c := range ex.CaptureScript {
		captureScript = append(captureScript, c.LLMCaseID)
		if i > 0 {
			captureScript = append(captureScript, "relation-no-match-for-dry-cleaning")
		}
	}
	captureLLM := fakeprovider.New(t, llmCasesDir(t), captureScript...)
	clock := &steppingClock{}
	ids := &demoIDs{}
	captureSvc := brain.NewCaptureService(clock, ids, units, embeddings, lexical, relations, decisions, captureLLM, captureLLM, embed, index, signals)

	unitIDs := make([]string, len(ex.CaptureScript))
	for i, c := range ex.CaptureScript {
		offset, err := time.ParseDuration(c.Offset)
		if err != nil {
			t.Fatalf("capture_script[%d].offset %q: %v", i, c.Offset, err)
		}
		clock.now = demoT0.Add(offset)

		result, err := captureSvc.Capture(ctx, brain.CaptureInput{Text: c.Text, Channel: "chat"})
		if err != nil {
			t.Fatalf("capture_script[%d] (%q): Capture: %v", i, c.Text, err)
		}
		if result.UnitID == "" {
			t.Fatalf("capture_script[%d] (%q): Capture returned no UnitID for outcome %q", i, c.Text, result.Outcome)
		}
		unitIDs[i] = result.UnitID
	}

	return demoVault{
		vault: v, units: units, relations: relations, decisions: decisions,
		config: sqlite.NewConfigRepo(v), selfModel: sqlite.NewSelfModelRepo(v), state: sqlite.NewStateRepo(v),
		lexical: lexical, index: index, embed: embed, ids: ids, unitIDs: unitIDs,
	}
}

// connectJudgeCaseID is the case id connectJudgeCase writes fresh into a
// temp dir on every call — never checked in (see connectJudgeCase's own
// doc comment for why a checked-in file structurally cannot carry this
// response's one dynamic field).
const connectJudgeCaseID = "consolidation-demo-connect-related"

// connectJudgeCase solves 9b's own "id problem" (this document's PR 9b
// preamble; 9a's own disclosed scope boundary): a persisted
// ActionConnectRelationPersisted row needs connect's judge to answer with
// a real target_unit_id (relation.Judgment.TargetUnitID, consolidate.go's
// own judgeAndPersistPair — ProposeRelation trusts the judge's own
// TargetUnitID verbatim, it does not cross-check it against the candidate
// RecallService actually found), and that id is only assigned once
// CaptureService.Capture runs inside this same test — a checked-in,
// pre-authored testdata/llm/cases/*.json fixture cannot hold a value it
// predates.
//
// The id IS deterministic, though (demoIDs increments one shared counter,
// no wall clock, no map-iteration-order dependency anywhere on this
// path — the same property 9a's own dv.unitIDs already relies on), so
// this function does not invent or guess a value: it reads the real id
// straight out of dv.unitIDs, using the exact (source, target) capture
// indices the corpus's own expected.relations_created pair declares, and
// writes ONE fresh case file per test run into a t.TempDir() — never
// committed, never stale, self-correcting if a future corpus edit shifts
// which indices are involved. fakeprovider.New still does every bit of
// the actual replaying (CLAUDE.md non-negotiable #5, no network, no real
// LLM) — this only supplies the one value a static file structurally
// cannot carry. derive's own case is copied in unchanged alongside it so
// both calls still replay from one fakeprovider.Fake, matching Order()'s
// own connect-before-derive sequence.
//
// Returns the temp dir fakeprovider.New should load from.
func connectJudgeCase(t *testing.T, dv demoVault, ex goldenset.ConsolidationExample) string {
	t.Helper()
	if len(ex.Expected.RelationsCreated) == 0 {
		t.Fatalf("case %q declares no expected.relations_created — connect's judge response has no pair to target", ex.ID)
	}
	pair := ex.Expected.RelationsCreated[0]
	sourceUnitID, targetUnitID := dv.unitIDs[pair[0]], dv.unitIDs[pair[1]]

	dir := t.TempDir()

	derive, err := os.ReadFile(filepath.Join(llmCasesDir(t), "derive-team-meeting-preference.json"))
	if err != nil {
		t.Fatalf("reading derive-team-meeting-preference.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "derive-team-meeting-preference.json"), derive, 0o600); err != nil {
		t.Fatalf("copying derive-team-meeting-preference.json into judge dir: %v", err)
	}

	response := fmt.Sprintf(`{"outcome":"related","target_unit_id":%q,"type":%q,"strength":0.8,"confidence":0.85}`, targetUnitID, demoRelationType)
	connectCase := struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Task     string `json:"task"`
		Message  string `json:"message"`
		Response string `json:"response"`
	}{
		ID:       connectJudgeCaseID,
		Provider: "anthropic",
		Model:    "claude-sonnet",
		Task:     "relation_evaluation",
		Message:  fmt.Sprintf("unit %s relates to unit %s — 9b's own dynamically-generated judge case, see connectJudgeCase's own doc comment", sourceUnitID, targetUnitID),
		Response: response,
	}
	data, err := json.Marshal(connectCase)
	if err != nil {
		t.Fatalf("marshal dynamic connect judge case: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, connectJudgeCaseID+".json"), data, 0o600); err != nil {
		t.Fatalf("write dynamic connect judge case: %v", err)
	}
	return dir
}

// runDemoPass seeds config.consolidation_last_run_at from ex's own
// last_run_at (R4.4's MAY, so strengthen/connect's `since` is non-nil and
// excludes capture 2 from SelectConnectSources while still including
// capture 3 — see the corpus's own comment in
// testdata/consolidation/cases/dry-cleaning-and-ambiguous-contract-request.json),
// then runs exactly one whole pass at ex's own "now".
//
// passJudge scripts the two provider calls this specific corpus's pass
// makes, in Order()'s own phase sequence (connect before derive):
// connectJudgeCase's own freshly-written case answers connect's one judged
// pair with "outcome":"related" and the real target unit id (9b's own
// discharge of 9a's disclosed scope boundary — see connectJudgeCase's own
// doc comment), and "derive-team-meeting-preference" answers derive's one
// source with a real belief proposal, which DOES persist (createDerived
// Belief carries no unit reference to get wrong).
func runDemoPass(t *testing.T, dv demoVault, ex goldenset.ConsolidationExample) (brain.ConsolidateReport, error) {
	t.Helper()
	ctx := context.Background()

	if ex.LastRunAt != nil {
		lastRunAt, err := time.Parse(time.RFC3339, *ex.LastRunAt)
		if err != nil {
			t.Fatalf("last_run_at %q: %v", *ex.LastRunAt, err)
		}
		if err := dv.config.RecordConsolidationRun(ctx, lastRunAt); err != nil {
			t.Fatalf("seeding consolidation_last_run_at: %v", err)
		}
	}

	now, err := time.Parse(time.RFC3339, ex.Now)
	if err != nil {
		t.Fatalf("now %q: %v", ex.Now, err)
	}

	recallSvc := brain.NewRecallService(dv.index, dv.lexical, dv.units, dv.embed)
	judgeDir := connectJudgeCase(t, dv, ex)
	passJudge := fakeprovider.New(t, judgeDir, connectJudgeCaseID, "derive-team-meeting-preference")
	clock := &steppingClock{now: now}
	consolidateSvc := brain.NewConsolidateService(clock, dv.config, dv.units, dv.relations, dv.ids, dv.decisions, recallSvc, passJudge, dv.selfModel, dv.state)

	return consolidateSvc.Consolidate(ctx, brain.ConsolidateRequest{})
}

// TestDemo_SimulatedWeeks_PassCompletes is spec R4.2 (as corrected by design
// D6) and R4.3: the corpus is built by driving the real capture path under
// a stepping fake clock, every provider call goes through
// test/support/fakeprovider, and one whole consolidation pass over that
// corpus completes without error.
func TestDemo_SimulatedWeeks_PassCompletes(t *testing.T) {
	ex := loadDemoCase(t)
	dv := driveDemoCorpus(t, ex)

	if _, err := runDemoPass(t, dv, ex); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
}

// TestDemo_ArchiveFires is spec R4.4's archive clause: over this corpus's
// chosen "now", every capture_script index the case's own expected.archived
// declares has archived, read back directly through DecisionLog.Since
// (never by re-deriving from units directly) rather than via re-running the
// pass a second time. This is also demoT0's own mechanical guard — see
// demoT0's doc comment. Checking each declared index specifically (JD-9a-01
// correction), rather than counting rows and comparing to zero, is what
// makes demoT0's own margin claim true: see demoT0's own doc comment for
// which capture actually binds and why an earlier version of this
// assertion did not enforce it.
func TestDemo_ArchiveFires(t *testing.T) {
	ex := loadDemoCase(t)
	dv := driveDemoCorpus(t, ex)

	if _, err := runDemoPass(t, dv, ex); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	decisions, err := dv.decisions.Since(context.Background(), time.Time{}, 1000)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	archivedUnitIDs := make(map[string]bool, len(decisions))
	for _, d := range decisions {
		if d.Action != ports.ActionArchiveArchived {
			continue
		}
		var detail struct{ UnitID string }
		if err := json.Unmarshal(d.Context, &detail); err != nil {
			t.Fatalf("unmarshal %s decision context %s: %v", ports.ActionArchiveArchived, d.Context, err)
		}
		archivedUnitIDs[detail.UnitID] = true
	}
	for _, idx := range ex.Expected.Archived {
		if idx < 0 || idx >= len(dv.unitIDs) {
			t.Fatalf("expected.archived index %d is out of range for %d capture_script entries", idx, len(dv.unitIDs))
		}
		unitID := dv.unitIDs[idx]
		if !archivedUnitIDs[unitID] {
			t.Fatalf("capture_script[%d] (unit %s) has no %s row, want one for every case's own expected.archived index %v (spec R4.4's archive clause) — demoT0 %s and the case's own now %s no longer clear the archival threshold; see demoT0's own doc comment", idx, unitID, ports.ActionArchiveArchived, ex.Expected.Archived, demoT0, ex.Now)
		}
	}
}

// TestDemo_ConnectCandidatePairExists is spec R4.4's connect clause,
// narrowed to task 9a.5's own scope: at least one candidate pair is close
// enough by connect's fused ranking to reach the judge. The proof is
// structural, not a second assertion here — passJudge (runDemoPass) scripts
// EXACTLY one relation_evaluation call for connect's own pair; a candidate
// search that finds zero pairs never consumes that scripted entry, and
// fakeprovider.Fake's own t.Cleanup (fakeprovider.go) fails this test for
// "scripted case(s) never called" the moment that happens — the same "a
// broken candidate search... fails the scripted guard" property this
// file's task list names.
func TestDemo_ConnectCandidatePairExists(t *testing.T) {
	ex := loadDemoCase(t)
	dv := driveDemoCorpus(t, ex)

	if _, err := runDemoPass(t, dv, ex); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
}

// TestDemo_DeriveBeliefExists is spec R4.4's derive clause: at least one
// derivable belief exists in this corpus, proven by scripting the dedup
// judge (derive-team-meeting-preference) to answer derive's one source with
// a real proposal — read back through DecisionLog.Since for
// ActionDeriveBeliefCreated (a brand-new topic key, so MergeProposals'
// own CREATE half, never a merge into an empty active-beliefs list).
func TestDemo_DeriveBeliefExists(t *testing.T) {
	ex := loadDemoCase(t)
	dv := driveDemoCorpus(t, ex)

	if _, err := runDemoPass(t, dv, ex); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	decisions, err := dv.decisions.Since(context.Background(), time.Time{}, 1000)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	created := 0
	for _, d := range decisions {
		if d.Action == ports.ActionDeriveBeliefCreated || d.Action == ports.ActionDeriveBeliefReinforced {
			created++
		}
	}
	if created == 0 {
		t.Fatalf("decision_log gained 0 %s/%s rows over %d total, want >= 1 (spec R4.4's derive clause)", ports.ActionDeriveBeliefCreated, ports.ActionDeriveBeliefReinforced, len(decisions))
	}
}

// demoDeriveBeliefTopicKey is testdata/llm/cases/derive-team-meeting-
// preference.json's own scripted topic_key — the fixture 9a already wires
// unconditionally for derive's one source (the corpus's own
// expected.beliefs: [3]) — named here so TestDemo_DecisionLogTellsTheStory
// can assert the derived belief's own Rationale names it (spec R4.5's
// derive clause), without re-deriving it from self_beliefs directly.
const demoDeriveBeliefTopicKey = "team_meeting"

// demoRelationType is the relation type connectJudgeCase's own
// dynamically-written judge case scripts (runDemoPass, below) — kept as
// one named constant rather than a literal repeated in two places.
const demoRelationType = "related_to"

// decisionsWithAction filters decisions (already read once via
// DecisionLog.Since, never re-queried per action) down to one Action —
// a small local helper so TestDemo_DecisionLogTellsTheStory reads once
// and checks three times, matching spec R4.5's own "decision_log alone
// tells the story" framing: one read, multiple assertions over it.
func decisionsWithAction(decisions []ports.Decision, action ports.DecisionAction) []ports.Decision {
	var out []ports.Decision
	for _, d := range decisions {
		if d.Action == action {
			out = append(out, d)
		}
	}
	return out
}

// anyRationaleNames reports whether at least one row's Rationale contains
// every one of substrings — spec R4.5's own bar ("each Rationale string
// is present and names the specific unit/relation/belief the fixture
// expects, not merely that a row of the right Action exists somewhere").
// A substring check, not an exact-string match: it proves the row names
// the specific item without coupling this test to internal/brain's own
// incidental wording (fmt.Sprintf's exact phrasing in consolidate.go).
func anyRationaleNames(rows []ports.Decision, substrings ...string) bool {
	for _, d := range rows {
		if d.Rationale == "" {
			continue
		}
		named := true
		for _, s := range substrings {
			if !strings.Contains(d.Rationale, s) {
				named = false
				break
			}
		}
		if named {
			return true
		}
	}
	return false
}

// TestDemo_DecisionLogTellsTheStory is spec R4.5's own bar — decision_log
// alone tells the story — asserted only through DecisionLog.Since (never
// re-deriving from units/relations/self_beliefs directly): at least one
// legible row for each of ActionArchiveArchived, ActionConnectRelation
// Persisted, and (ActionDeriveBeliefCreated or ActionDeriveBeliefReinforced),
// with each Rationale string present and naming the SPECIFIC unit/
// relation/belief the case's own expected block declares — not merely
// that a row of the right Action exists somewhere, the exact bar 9a's own
// three narrower tests (TestDemo_ArchiveFires excepted, strengthened by
// JD-9a-01) stopped short of. R4.6's own exit criterion rides on this
// assertion (this file's own package, `m2d`'s last link).
//
// The connect clause is the one this corpus could not satisfy until this
// PR: 9a's own passJudge scripted connect's one judge call with
// "outcome":"new" (no persist) precisely because a real persisted
// relation needs the real, run-time unit id RecallService's own candidate
// search finds — an id only CaptureService.Capture assigns once this test
// runs, which a checked-in, pre-authored testdata/llm fixture cannot
// carry (design §8.1; 9a's own disclosed scope boundary). See
// runDemoPass's own connectJudgeCase call for how this PR solves it: a
// fresh per-run judge case, written to a temp dir, whose target_unit_id is
// read from dv.unitIDs itself — never a hardcoded guess.
func TestDemo_DecisionLogTellsTheStory(t *testing.T) {
	ex := loadDemoCase(t)
	dv := driveDemoCorpus(t, ex)

	if _, err := runDemoPass(t, dv, ex); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	decisions, err := dv.decisions.Since(context.Background(), time.Time{}, 1000)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}

	archiveRows := decisionsWithAction(decisions, ports.ActionArchiveArchived)
	for _, idx := range ex.Expected.Archived {
		unitID := dv.unitIDs[idx]
		if !anyRationaleNames(archiveRows, unitID) {
			t.Fatalf("decision_log has no %s row whose Rationale names unit %s (capture_script[%d]) — spec R4.5's archive clause", ports.ActionArchiveArchived, unitID, idx)
		}
	}

	if len(ex.Expected.RelationsCreated) == 0 {
		t.Fatalf("case %q declares no expected.relations_created — spec R4.5's connect clause has nothing to check", ex.ID)
	}
	pair := ex.Expected.RelationsCreated[0]
	sourceID, targetID := dv.unitIDs[pair[0]], dv.unitIDs[pair[1]]
	connectRows := decisionsWithAction(decisions, ports.ActionConnectRelationPersisted)
	if !anyRationaleNames(connectRows, sourceID, targetID) {
		t.Fatalf("decision_log has no %s row whose Rationale names both unit %s (capture_script[%d]) and unit %s (capture_script[%d]) — spec R4.5's connect clause", ports.ActionConnectRelationPersisted, sourceID, pair[0], targetID, pair[1])
	}

	if len(ex.Expected.Beliefs) == 0 {
		t.Fatalf("case %q declares no expected.beliefs — spec R4.5's derive clause has nothing to check", ex.ID)
	}
	deriveRows := append(decisionsWithAction(decisions, ports.ActionDeriveBeliefCreated), decisionsWithAction(decisions, ports.ActionDeriveBeliefReinforced)...)
	if !anyRationaleNames(deriveRows, demoDeriveBeliefTopicKey) {
		t.Fatalf("decision_log has no %s/%s row whose Rationale names belief topic key %q — spec R4.5's derive clause", ports.ActionDeriveBeliefCreated, ports.ActionDeriveBeliefReinforced, demoDeriveBeliefTopicKey)
	}
}
