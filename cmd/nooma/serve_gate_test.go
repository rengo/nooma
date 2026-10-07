package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/fakechannel"
)

// serveGateViolations is the structural check behind
// TestServe_OneFocusKeeperSharedByTodayAndDigest (design m4c §3.8). It reads
// the non-test sources of cmd/nooma, given as name -> source, and reports each
// way the previous focus could stop being ONE object shared by every writer.
//
// Today and the digest only share an incumbent because serve hands them the
// same *brain.FocusKeeper. Nothing in the type system says so: two keepers
// compile, pass every unit test, and leave the digest reading an incumbent
// Today never writes. So the gate reads the wiring:
//
//	(a) exactly one wireFocus call, in serve.go, assigned to an identifier X;
//	(b) wireToday and wireScheduler are called with X as their last argument;
//	(c) NewFocusKeeper is called only inside wireFocus, and wireFocus only
//	    from serve.go;
//	(d) the keeper then flows serve.go -> wireScheduler -> wireProactive ->
//	    NewCheckService, and parsing serve.go alone cannot see the last two
//	    links: every NewCheckService call passes the enclosing function's own
//	    last parameter, never nil or a call, except wireCheck (`nooma check`
//	    has no channel, so its digest returns before it reads a focus), which
//	    passes nil; and every wireProactive call does the same.
func serveGateViolations(t *testing.T, srcs map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for name, src := range srcs {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files[name] = f
	}

	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	// X is the identifier serve.go assigns wireFocus's result to.
	keeperVar := ""
	focusCallsInServe := 0
	for name, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch calleeName(call) {
				case "wireFocus":
					if name != "serve.go" {
						add("%s: wireFocus called outside serve.go (in %s) — a second keeper", name, fn.Name.Name)
					} else {
						focusCallsInServe++
					}
				case "NewFocusKeeper":
					if fn.Name.Name != "wireFocus" {
						add("%s: NewFocusKeeper called in %s — only wireFocus may build the keeper", name, fn.Name.Name)
					}
				}
				return true
			})
		}
	}

	serve, ok := files["serve.go"]
	if !ok {
		return append(out, "serve.go is missing from the scanned sources")
	}
	if focusCallsInServe != 1 {
		add("serve.go: %d wireFocus calls, want exactly 1", focusCallsInServe)
	}
	ast.Inspect(serve, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || calleeName(call) != "wireFocus" {
			return true
		}
		if id, ok := assign.Lhs[0].(*ast.Ident); ok && len(assign.Lhs) == 1 {
			keeperVar = id.Name
		}
		return true
	})
	if keeperVar == "" {
		add("serve.go: wireFocus's result is not assigned to one identifier")
	}

	todayCalls, schedulerCalls := 0, 0
	ast.Inspect(serve, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || calleeName(call) != "wireToday" {
			return true
		}
		todayCalls++
		if !lastArgIs(call, keeperVar) {
			add("serve.go: wireToday's last argument is not %q, the one shared keeper", keeperVar)
		}
		return true
	})
	ast.Inspect(serve, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || calleeName(call) != "wireScheduler" {
			return true
		}
		schedulerCalls++
		if !lastArgIs(call, keeperVar) {
			add("serve.go: wireScheduler's last argument is not %q, the one shared keeper", keeperVar)
		}
		return true
	})
	if todayCalls < 1 {
		add("serve.go: no wireToday call — Today would not be wired to the shared keeper")
	}
	if schedulerCalls < 1 {
		add("serve.go: no wireScheduler call — the digest would not be wired to the shared keeper")
	}

	for name, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			param := lastParamName(fn)
			keeper := keeperParamName(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch calleeName(call) {
				case "NewCheckService":
					want := keeper
					if fn.Name.Name == "wireCheck" {
						want = "nil"
					}
					if !lastArgIs(call, want) {
						add("%s: %s hands NewCheckService something other than %q as its keeper (its *brain.FocusKeeper parameter named keeper, or nil in wireCheck)", name, fn.Name.Name, want)
					}
				case "NewTodayService":
					if !lastArgIs(call, keeper) {
						add("%s: %s hands NewTodayService something other than its *brain.FocusKeeper parameter named keeper", name, fn.Name.Name)
					}
				case "wireProactive":
					if !lastArgIs(call, param) {
						add("%s: %s hands wireProactive something other than its own %q parameter", name, fn.Name.Name, param)
					}
				}
				return true
			})
		}
	}
	return out
}

// lastParamName is the name of fn's last parameter, "" when it has none.
func lastParamName(fn *ast.FuncDecl) string {
	params := fn.Type.Params
	if params == nil || len(params.List) == 0 {
		return ""
	}
	last := params.List[len(params.List)-1]
	if len(last.Names) == 0 {
		return ""
	}
	return last.Names[len(last.Names)-1].Name
}

// keeperParamName is the name of fn's parameter of type *brain.FocusKeeper,
// "" when it has none.
func keeperParamName(fn *ast.FuncDecl) string {
	if fn.Type.Params == nil {
		return ""
	}
	for _, field := range fn.Type.Params.List {
		star, ok := field.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "FocusKeeper" {
			continue
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "brain" {
			continue
		}
		for _, n := range field.Names {
			if n.Name == "keeper" {
				return n.Name
			}
		}
	}
	return ""
}

// calleeName is the name a call expression invokes: f(...) and pkg.f(...)
// both answer "f".
func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// lastArgIs reports whether call's last argument is the bare identifier name.
func lastArgIs(call *ast.CallExpr, name string) bool {
	if name == "" || len(call.Args) == 0 {
		return false
	}
	id, ok := call.Args[len(call.Args)-1].(*ast.Ident)
	return ok && id.Name == name
}

// goodServeSources is the wiring the gate must accept: the shape of the real
// cmd/nooma, cut down to what the gate reads.
func goodServeSources() map[string]string {
	return map[string]string{
		"serve.go": `package main

func runServe() {
	focusKeeper := wireFocus(db)
	sched, err := wireScheduler(ctx, db, cfg, lookup, errOut, channel, focusKeeper)
	today := wireToday(db, focusKeeper)
	_, _, _ = sched, err, today
}
`,
		"wiring.go": `package main

func wireFocus(db *sqlite.Vault) *brain.FocusKeeper {
	return brain.NewFocusKeeper(units, cfg, rels)
}

func wireToday(db *sqlite.Vault, keeper *brain.FocusKeeper) *brain.TodayService {
	return brain.NewTodayService(clock, units, cfg, state, triggers, questions, log, keeper)
}

func wireCheck(db *sqlite.Vault) *brain.CheckService {
	return brain.NewCheckService(clock, triggers, timers, ids, log, nil, units, state, nil, "", questions, nil)
}

func wireProactive(clock ports.Clock, db *sqlite.Vault, cfg *config.Config, lookup func(string) (string, bool), channel ports.Channel, keeper *brain.FocusKeeper) (*brain.CheckService, error) {
	return brain.NewCheckService(clock, triggers, timers, ids, log, channel, units, state, llm, conversation, questions, keeper), nil
}

func wireScheduler(ctx context.Context, db *sqlite.Vault, cfg *config.Config, lookup func(string) (string, bool), log io.Writer, channel ports.Channel, keeper *brain.FocusKeeper) (*scheduler.Scheduler, error) {
	check, err := wireProactive(systemClock{}, db, cfg, lookup, channel, keeper)
	_ = check
	return nil, err
}
`,
	}
}

// TestServe_OneFocusKeeperSharedByTodayAndDigest fails when cmd/nooma stops
// wiring one shared FocusKeeper. It reads the real sources, and then proves
// the checker itself against probes: a gate that never fires on the broken
// shapes is not a gate.
func TestServe_OneFocusKeeperSharedByTodayAndDigest(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read cmd/nooma: %v", err)
	}
	sources := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		sources[e.Name()] = string(src)
	}
	if got := serveGateViolations(t, sources); len(got) != 0 {
		t.Errorf("cmd/nooma does not wire one shared FocusKeeper:\n  %s", strings.Join(got, "\n  "))
	}

	if got := serveGateViolations(t, goodServeSources()); len(got) != 0 {
		t.Fatalf("the gate fires on the correct wiring: %v", got)
	}

	probes := map[string]func(m map[string]string){
		"a second wireFocus call in serve.go": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "today := ", "other := wireFocus(db)\n\t_ = other\n\ttoday := ", 1)
		},
		"wireToday with the keeper built inline": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "wireToday(db, focusKeeper)", "wireToday(db, wireFocus(db))", 1)
		},
		"wireToday passed a different identifier": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "wireToday(db, focusKeeper)", "wireToday(db, otherKeeper)", 1)
		},
		"wireScheduler passed a different identifier": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "channel, focusKeeper)", "channel, otherKeeper)", 1)
		},
		"wireScheduler passed nil": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "channel, focusKeeper)", "channel, nil)", 1)
		},
		"wireScheduler passed a fresh keeper": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "channel, focusKeeper)", "channel, wireFocus(db))", 1)
		},
		"wireScheduler hands wireProactive nil": func(m map[string]string) {
			m["wiring.go"] = strings.Replace(m["wiring.go"], "channel, keeper)\n	_ = check", "channel, nil)\n	_ = check", 1)
		},
		"wireScheduler hands wireProactive a call": func(m map[string]string) {
			m["wiring.go"] = strings.Replace(m["wiring.go"], "channel, keeper)\n	_ = check", "channel, newKeeper())\n	_ = check", 1)
		},
		"wireProactive hands NewCheckService nil": func(m map[string]string) {
			m["wiring.go"] = strings.Replace(m["wiring.go"], "conversation, questions, keeper), nil", "conversation, questions, nil), nil", 1)
		},
		"wireProactive hands NewCheckService a call": func(m map[string]string) {
			m["wiring.go"] = strings.Replace(m["wiring.go"], "conversation, questions, keeper), nil", "conversation, questions, brainKeeper()), nil", 1)
		},
		"wireCheck hands NewCheckService a keeper": func(m map[string]string) {
			m["wiring.go"] = strings.Replace(m["wiring.go"], `"", questions, nil)`, `"", questions, wireFocusKeeper)`, 1)
		},
		"wireToday hands NewTodayService nil": func(m map[string]string) {
			m["wiring.go"] = strings.Replace(m["wiring.go"], "log, keeper)\n}\n\nfunc wireCheck", "log, nil)\n}\n\nfunc wireCheck", 1)
		},
		"wireToday hands NewTodayService a fresh keeper": func(m map[string]string) {
			m["wiring.go"] = strings.Replace(m["wiring.go"], "log, keeper)\n}\n\nfunc wireCheck", "log, brain.NewFocusKeeper(units, cfg, rels))\n}\n\nfunc wireCheck", 1)
		},
		"no wireToday call in serve.go": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "today := wireToday(db, focusKeeper)", "today := 0", 1)
		},
		"no wireScheduler call in serve.go": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "sched, err := wireScheduler(ctx, db, cfg, lookup, errOut, channel, focusKeeper)", "var sched, err = 0, 0", 1)
		},
		"wireX passed db instead of the keeper": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "wireToday(db, focusKeeper)", "wireToday(db)", 1)
		},
		"NewCheckService handed the last parameter that is not the keeper": func(m map[string]string) {
			m["wiring.go"] = strings.Replace(m["wiring.go"], "channel ports.Channel, keeper *brain.FocusKeeper) (*brain.CheckService", "keeper *brain.FocusKeeper, channel ports.Channel) (*brain.CheckService", 1)
			m["wiring.go"] = strings.Replace(m["wiring.go"], "questions, keeper), nil", "questions, channel), nil", 1)
		},
		"NewCheckService parameter named keeper of the wrong type": func(m map[string]string) {
			m["wiring.go"] = strings.Replace(m["wiring.go"], "channel ports.Channel, keeper *brain.FocusKeeper) (*brain.CheckService", "channel ports.Channel, keeper *brain.Other) (*brain.CheckService", 1)
		},
		"wireFocus called outside serve.go": func(m map[string]string) {
			m["wiring.go"] += "\nfunc wireElsewhere(db *sqlite.Vault) { _ = wireFocus(db) }\n"
		},
		"NewFocusKeeper called outside wireFocus": func(m map[string]string) {
			m["wiring.go"] += "\nfunc wireProactive(db *sqlite.Vault) { _ = brain.NewFocusKeeper(units, cfg, rels) }\n"
		},
	}
	for name, mutate := range probes {
		t.Run("fires on "+name, func(t *testing.T) {
			m := goodServeSources()
			mutate(m)
			if got := serveGateViolations(t, m); len(got) == 0 {
				t.Fatalf("the gate is silent on %q", name)
			}
		})
	}
}

// gateClock is a ports.Clock fixed at one instant: wireProactive's clock seam
// is what lets this test land inside DigestDue's window.
type gateClock struct{ now time.Time }

func (c gateClock) Now() time.Time { return c.now }

// slotSevenAfterDigest is the FX-H fixture (design m4c §6) over a real,
// migrated vault. Six task fillers, A (1.0) and B (0.99) share one set of
// timestamps, so `wireToday`'s real clock and the digest's fixed one scale
// every score together and cannot reorder them: A holds slot 7. One digest is
// sent through wireProactive with the keeper digestKeeper derives from the
// shared one, B then rises to 1.03 (inside A's 5% margin), and the first
// Today request through wireToday reports who holds the task focus's last
// slot: A when the digest and Today share one incumbent, B when they do not.
func slotSevenAfterDigest(t *testing.T, digestKeeper func(shared *brain.FocusKeeper, db *sqlite.Vault) *brain.FocusKeeper) string {
	t.Helper()
	ctx := context.Background()
	vault := writeVault(t, "")
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		t.Fatalf("loadVaultConfig: %v", err)
	}
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	seededAt := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	units := sqlite.NewUnitRepo(db)
	seed := func(id string, typ unit.Type, w float64) {
		if err := units.Create(ctx, unit.Unit{
			ID: id, Type: typ, Status: unit.StatusPool, Content: id, Source: "chat",
			Weight: w, WeightDecayRate: 0.01,
			LastTouchedAt: seededAt, CreatedAt: seededAt, UpdatedAt: seededAt,
		}); err != nil {
			t.Fatalf("seed unit %s: %v", id, err)
		}
	}
	for i := 1; i <= 6; i++ {
		seed(fmt.Sprintf("F%d", i), unit.TypeTask, 10)
	}
	seed("A", unit.TypeTask, 1.0)
	seed("B", unit.TypeTask, 0.99)

	// One pending trigger on a knowledge unit (in neither focus), so a digest
	// is due and has content.
	seed("k-1", unit.TypeKnowledge, 1)
	digestAt := time.Date(2026, 8, 5, prospection.DigestHour, 5, 0, 0, time.UTC)
	triggers := sqlite.NewTriggerRepo(db)
	uid, fireAt := "k-1", digestAt.Add(-time.Hour)
	if err := triggers.Create(ctx, ports.Trigger{
		ID: "trg-1", UnitID: &uid, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "act on k-1"}, FireAt: &fireAt, CreatedAt: seededAt,
	}); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	if err := triggers.Fire(ctx, "trg-1", digestAt); err != nil {
		t.Fatalf("fire trigger: %v", err)
	}

	shared := wireFocus(db)
	cfg.Channels.Telegram.AllowedChatIDs = []int64{12449194}
	ch := fakechannel.New()
	check, err := wireProactive(gateClock{now: digestAt}, db, cfg, func(string) (string, bool) { return "", false }, ch, digestKeeper(shared, db))
	if err != nil {
		t.Fatalf("wireProactive: %v", err)
	}
	if _, err := check.Check(ctx, brain.CheckRequest{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if sent := ch.Sent(t); len(sent) != 1 {
		t.Fatalf("the digest sent %d message(s), want 1", len(sent))
	}
	// wireProactive's clock seam: the pass must run at the instant it was
	// given, not at the real one.
	rows, err := sqlite.NewDecisionLog(db).Since(ctx, digestAt.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("reading the decision log: %v", err)
	}
	sentRows := 0
	for _, row := range rows {
		if row.Action != ports.ActionCheckDigestSent {
			continue
		}
		sentRows++
		if !row.OccurredAt.Equal(digestAt) {
			t.Fatalf("check.digest.sent was recorded at %s, want the clock wireProactive was given (%s)", row.OccurredAt, digestAt)
		}
	}
	if sentRows != 1 {
		t.Fatalf("%d check.digest.sent rows, want 1", sentRows)
	}

	if err := units.ApplyBoosts(ctx, []weight.Boost{{UnitID: "B", Weight: 1.03, LastTouchedAt: seededAt}}, seededAt); err != nil {
		t.Fatalf("raise B: %v", err)
	}
	today, err := wireToday(db, shared).Today(ctx)
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	members := today.Focuses[0].Members
	if len(members) != focus.DefaultSize {
		t.Fatalf("task focus has %d members, want %d", len(members), focus.DefaultSize)
	}
	return members[len(members)-1].ID
}

// TestWireProactive_DigestSharesTodaysKeeper proves the keeper really reaches
// the digest, which the AST gate can only say is wired: a digest sent through
// wireProactive leaves A held for Today's first request, and the two controls
// show the fixture discriminates: a nil keeper, or a second one, leaves
// nothing for Today to find.
func TestWireProactive_DigestSharesTodaysKeeper(t *testing.T) {
	t.Run("the shared keeper", func(t *testing.T) {
		got := slotSevenAfterDigest(t, func(shared *brain.FocusKeeper, _ *sqlite.Vault) *brain.FocusKeeper { return shared })
		if got != "A" {
			t.Fatalf("slot 7 = %s, want A — Today must find the incumbent the digest published", got)
		}
	})
	t.Run("a nil keeper", func(t *testing.T) {
		got := slotSevenAfterDigest(t, func(*brain.FocusKeeper, *sqlite.Vault) *brain.FocusKeeper { return nil })
		if got != "B" {
			t.Fatalf("slot 7 = %s, want B — with no keeper the digest publishes nothing", got)
		}
	})
	t.Run("a second keeper", func(t *testing.T) {
		got := slotSevenAfterDigest(t, func(_ *brain.FocusKeeper, db *sqlite.Vault) *brain.FocusKeeper { return wireFocus(db) })
		if got != "B" {
			t.Fatalf("slot 7 = %s, want B — a second keeper is a second incumbent", got)
		}
	})
}
