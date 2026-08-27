// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

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
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestCaptureArmRefusal_WritesARowExactlyWhenTheCaptureIsOtherwiseTraceless
// is R4.2, and it asserts the rule rather than the table.
//
// The rule: a refused arming writes a capture.arm.refused row exactly when
// the capture would otherwise leave no trace at all. That is derived, not
// chosen — doc 02 §11 records every decision WITH AN EFFECT, and a row per
// capture saying "this was not a timer" is noise that defeats the glass
// box. A timer or recurring_reminder never becomes a unit (I04) and never
// reaches recordClassifyDecision, so its refusal is silent otherwise; a
// dated event that refuses still persists its unit, so it already has a
// record and a second row would double-count one fact.
//
// **The assertion never enumerates which refusals qualify, and never asks
// prospection.Arm what it decided.** It reads two things the sweep itself
// produces: whether a cell wrote anything at all, and whether that cell's
// Kind armed successfully in ANY cell of the sweep. A Kind that armed
// somewhere is a Kind that can arm, so a refusal from it is a request the
// user made and did not get; a Kind that never armed anywhere arms nothing
// by design, and a row per capture saying "this was not a timer" is the
// noise doc 02 §11 exists to keep out.
//
// A fourteenth Kind, or a refusal reachable from an input combination
// nobody thought of, is covered without a test edit — which a four-row
// table would not be. That table shape is the m3a defect this test is
// written to avoid.
//
// Exhaustive over inputs, not over the table: classify.AllKinds() (thirteen
// members, pinned complete by classify/kind_test.go) × {dated future, dated
// past, undated} × {recurrence rule present, absent}.
func TestCaptureArmRefusal_WritesARowExactlyWhenTheCaptureIsOtherwiseTraceless(t *testing.T) {
	kinds := classify.AllKinds()
	if len(kinds) != 13 {
		t.Fatalf("classify.AllKinds() has %d members, want 13 — this test's own claim to be exhaustive over kinds is what changed", len(kinds))
	}

	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)

	dateShapes := []struct {
		name  string
		at    *time.Time
		dated bool
	}{
		{name: "undated"},
		{name: "dated future", at: ptr(now.Add(30 * 24 * time.Hour)), dated: true},
		{name: "dated past", at: ptr(now.Add(-30 * 24 * time.Hour)), dated: true},
	}
	ruleShapes := []struct {
		name string
		rule string
	}{
		{name: "no recurrence rule"},
		{name: "yearly recurrence rule", rule: "yearly"},
	}

	// cell is what one sweep coordinate produced, collected first and
	// judged afterwards: the rule needs to know whether a Kind armed
	// ANYWHERE before it can say whether that Kind's refusals should have
	// been recorded.
	type cell struct {
		name      string
		kind      classify.Kind
		armed     int
		refused   []ports.Decision
		otherRows int
	}
	var cells []cell

	for _, kind := range kinds {
		for _, date := range dateShapes {
			for _, rule := range ruleShapes {
				name := fmt.Sprintf("%s/%s/%s", kind, date.name, rule.name)
				c := cell{name: name, kind: kind}

				t.Run(name, func(t *testing.T) {
					decisions := memrepo.NewDecisionLog()
					runCaptureForRefusalSweep(t, now, decisions, string(kind), date.at, rule.rule)

					rows, err := decisions.Since(context.Background(), now.Add(-time.Hour), -1)
					if err != nil {
						t.Fatalf("decisions.Since: %v", err)
					}

					for _, row := range rows {
						switch {
						case row.Action == ports.ActionCaptureArmRefused:
							c.refused = append(c.refused, row)
						case strings.HasPrefix(string(row.Action), "capture.armed."):
							c.armed++
						default:
							c.otherRows++
						}
					}

					if c.armed > 1 {
						t.Errorf("%d capture.armed.* rows, want at most 1 — one Plan is one row", c.armed)
					}
					if len(c.refused) > 1 {
						t.Errorf("%d capture.arm.refused rows, want at most 1", len(c.refused))
					}
					if c.armed > 0 && len(c.refused) > 0 {
						t.Error("the capture both armed and refused — a Plan is one or the other")
					}
				})

				cells = append(cells, c)
			}
		}
	}

	// Which Kinds can arm at all, read off the sweep rather than declared.
	canArm := map[classify.Kind]bool{}
	for _, c := range cells {
		if c.armed > 0 {
			canArm[c.kind] = true
		}
	}
	if len(canArm) == 0 {
		t.Fatal("no cell in the sweep armed anything — the sweep proves nothing about refusals")
	}

	// The rule, read both ways over every cell.
	for _, c := range cells {
		traceless := c.armed == 0 && c.otherRows == 0
		want := 0
		if canArm[c.kind] && traceless {
			want = 1
		}
		if len(c.refused) != want {
			t.Errorf("%s: %d capture.arm.refused rows, want %d (kind can arm: %v; capture otherwise wrote %d rows)",
				c.name, len(c.refused), want, canArm[c.kind], c.armed+c.otherRows)
		}
	}

	// Every refusal row is readable on its own terms, and no two refusal
	// reasons share a sentence — a reader must be able to tell "you gave me
	// no date" from "you gave me a date that has passed", which are two
	// different things to fix.
	rationaleOf := map[string]string{}
	for _, c := range cells {
		for _, row := range c.refused {
			if row.Rationale == "" {
				t.Errorf("%s: Rationale is empty — doc 02 §11 requires a human-readable sentence", c.name)
			}
			if !row.OccurredAt.Equal(now) {
				t.Errorf("%s: OccurredAt = %v, want the capture's single clock read %v", c.name, row.OccurredAt, now)
			}

			var refusedContext struct {
				Why string `json:"why"`
			}
			if err := json.Unmarshal(row.Context, &refusedContext); err != nil {
				t.Errorf("%s: Context is not valid JSON: %v (%s)", c.name, err, row.Context)
				continue
			}
			if refusedContext.Why == "" {
				t.Errorf("%s: Context.why is empty — the row must name which refusal it records", c.name)
				continue
			}

			if other, taken := rationaleOf[row.Rationale]; taken && other != refusedContext.Why {
				t.Errorf("refusals %q and %q share the rationale %q — a reader cannot tell them apart",
					refusedContext.Why, other, row.Rationale)
			}
			rationaleOf[row.Rationale] = refusedContext.Why
		}
	}
	if len(rationaleOf) < 2 {
		t.Errorf("the sweep produced %d distinct refusal rationale(s), want at least 2 — no-date and already-past are different failures", len(rationaleOf))
	}
}

// runCaptureForRefusalSweep runs one capture whose classification is built
// from the sweep's own coordinates.
//
// The provider case is generated into t.TempDir() rather than checked in:
// the sweep is 78 cells, and 78 near-identical fixture files would be a
// corpus nobody reads and every future Kind would have to be added to by
// hand. What is under test here is the logging rule, not the fixtures.
func runCaptureForRefusalSweep(t *testing.T, now time.Time, decisions ports.DecisionLog, kind string, at *time.Time, rule string) {
	t.Helper()

	ctx := context.Background()
	response := map[string]any{
		"type":               kind,
		"normalized_content": "swept classification for " + kind,
		"weight":             0.5,
		"decay_rate":         0.05,
	}
	if at != nil {
		// Both date fields carry the same instant, so one shape serves
		// every kind: Arm reads due_at for a timer and event_at for the
		// two trigger kinds, and the sweep does not have to know which.
		response["due_at"] = at.Format(time.RFC3339)
		response["event_at"] = at.Format(time.RFC3339)
	}
	if rule != "" {
		response["recurrence_rule"] = rule
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal generated classification: %v", err)
	}
	caseJSON, err := json.Marshal(map[string]any{
		"id":       "swept",
		"provider": "anthropic",
		"model":    "claude-sonnet",
		"task":     "classify",
		"message":  "swept",
		"response": string(responseJSON),
	})
	if err != nil {
		t.Fatalf("marshal generated case: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "swept.json"), caseJSON, 0o600); err != nil {
		t.Fatalf("write generated case: %v", err)
	}

	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	// Only a chitchat reaches the chat task (ADR-0021), so only a chitchat
	// sweep scripts it. Scripting it unconditionally would fail every
	// other kind at cleanup with an uncalled case, which is the same
	// unscripted-call guard working in the other direction.
	chatScript := []string(nil)
	if kind == "chitchat" {
		chatCaseJSON, err := json.Marshal(map[string]any{
			"id":       "swept-chat",
			"provider": "anthropic",
			"model":    "claude-sonnet",
			"task":     "chat",
			"message":  "swept",
			"response": "swept chat reply",
		})
		if err != nil {
			t.Fatalf("marshal generated chat case: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "swept-chat.json"), chatCaseJSON, 0o600); err != nil {
			t.Fatalf("write generated chat case: %v", err)
		}
		chatScript = []string{"swept-chat"}
	}

	llm := fakeprovider.New(t, dir, "swept")
	chat := fakeprovider.New(t, dir, chatScript...)
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(), embeddings,
		memrepo.NewLexical(), memrepo.NewRelations(), decisions, llm, llm, chat,
		fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx), memrepo.NewSignals(),
		memrepo.NewTriggers(), memrepo.NewTimers())

	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: "swept", Channel: "chat"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
}

// ptr is the sweep's one-line address-of helper.
func ptr[T any](v T) *T { return &v }
