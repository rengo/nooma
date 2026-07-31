// Package fakeprovider is the scripted-replay ports.LLMProvider and the
// deterministic ports.EmbeddingProvider used everywhere this repository
// needs a provider without ever calling one (CLAUDE.md non-negotiable #5;
// spec R5.2; design D7).
package fakeprovider

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/goldenset"
)

// TestingT is the subset of *testing.T Fake needs. It exists so a test can
// inject its own spy in place of *testing.T — Fake's own test file does
// exactly that to observe the unscripted-call and under-run failure
// behaviors below without ending its own test run.
type TestingT interface {
	Helper()
	Cleanup(func())
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Fake implements both ports.LLMProvider and ports.EmbeddingProvider —
// Complete replays a scripted, ordered list of case ids (design D7);
// Embed derives a deterministic vector from its input text (design D7,
// spec R6.2's fake-side precondition). See New and NewEmbeddingFake.
type Fake struct {
	t      TestingT
	dir    string
	script []string
	idx    int

	seenPrompts []string

	embedModel string
}

var _ ports.LLMProvider = (*Fake)(nil)

// New returns a Fake that replays, in order and selected by case id —
// never by matching the live prompt text (spec R5.2) — the recordings
// named by ids, loaded from dir via goldenset.Load. A Complete call beyond
// the scripted ids fails t immediately; a scripted id Complete never
// reaches fails t at cleanup — the two properties design D7 names as the
// payoff of scripting rather than looking up.
func New(t TestingT, dir string, ids ...string) *Fake {
	t.Helper()
	f := &Fake{t: t, dir: dir, script: ids}
	t.Cleanup(func() {
		if f.idx < len(f.script) {
			t.Errorf("fakeprovider: %d scripted case(s) never called: %v", len(f.script)-f.idx, f.script[f.idx:])
		}
	})
	return f
}

// Complete implements ports.LLMProvider.
func (f *Fake) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	f.t.Helper()
	f.seenPrompts = append(f.seenPrompts, req.Prompt)

	if f.idx >= len(f.script) {
		f.t.Fatalf("fakeprovider: unscripted Complete call (task %q, prompt %q); no scripted case ids remain", req.Task, req.Prompt)
		return ports.LLMResponse{}, nil
	}

	id := f.script[f.idx]
	f.idx++

	var ex goldenset.LLMExample
	if err := goldenset.Load(filepath.Join(f.dir, id+".json"), &ex); err != nil {
		f.t.Fatalf("fakeprovider: loading case %q: %v", id, err)
		return ports.LLMResponse{}, nil
	}

	if ex.Error != "" {
		return ports.LLMResponse{}, errors.New(ex.Error)
	}
	return ports.LLMResponse{Text: ex.Response, Model: ex.Model}, nil
}

// SeenPrompts returns every prompt Complete received, in call order. A
// case's own recorded prompt field stays documentation, never the replay
// key (design D7) — this is what lets a test assert on it anyway.
func (f *Fake) SeenPrompts() []string {
	return append([]string(nil), f.seenPrompts...)
}
