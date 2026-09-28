// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// countingLexical wraps a ports.LexicalSearch and counts how many times
// SearchLexical is called — this test's own proof that /ui/units?q= runs
// the lexical leg exactly once per request, never twice for a browse
// reimplementation on top of the shared mechanism.
type countingLexical struct {
	ports.LexicalSearch
	calls int
}

func (c *countingLexical) SearchLexical(ctx context.Context, tokens []string, k int) ([]string, error) {
	c.calls++
	return c.LexicalSearch.SearchLexical(ctx, tokens, k)
}

// TestI22_BrowseSearchIsTheSameMechanism is spec R2's own "Verified by": a
// browse search reaches no mechanism RecallService.ForText does not
// already own. One RecallService, wired once; /ui/units?q= (driven
// directly through ui.Handler — a browse search's own entrance) and POST
// /recall (through httpapi.Handler, the production route) both search the
// identical raw text and return the identical ordered ids. Below the
// admission floor both answer empty.
func TestI22_BrowseSearchIsTheSameMechanism(t *testing.T) {
	ctx := context.Background()
	const query = "water the plants"

	units := memrepo.NewUnits()
	lexical := &countingLexical{LexicalSearch: memrepo.NewLexical()}
	embeddings := memrepo.NewEmbeddings()
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	vec, err := embed.Embed(ctx, ports.EmbedRequest{Text: query})
	if err != nil {
		t.Fatalf("deriving query's vector: %v", err)
	}
	if err := units.Create(ctx, poolUnit("match", "remember to water the plants")); err != nil {
		t.Fatalf("seeding match: %v", err)
	}
	if err := embeddings.Put(ctx, ports.Embedding{UnitID: "match", Model: embedFakeModel, Vector: vec.Vector}); err != nil {
		t.Fatalf("seeding match's embedding: %v", err)
	}

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewRecallService(brain.NewIndex(idx), lexical, units, embed)

	browseSearch := func(t *testing.T, q string) ([]string, int) {
		t.Helper()
		lexical.calls = 0
		h := ui.New(ui.Deps{Search: svc})
		req := httptest.NewRequest(http.MethodGet, "/ui/units?q="+url.QueryEscape(q), nil)
		req.Pattern = "GET /ui/units"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ui/units?q=%s = %d, want 200: %s", q, rec.Code, rec.Body.String())
		}
		return extractDataUnitIDs(rec.Body.String()), lexical.calls
	}

	recallRoute := func(t *testing.T, q string) ([]string, int) {
		t.Helper()
		lexical.calls = 0
		h := httpapi.Handler(httpapi.Deps{Recall: svc})
		body, err := json.Marshal(map[string]string{"query": q})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/recall", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /recall = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Units []struct {
				ID string `json:"id"`
			} `json:"units"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(resp.Units))
		for i, u := range resp.Units {
			ids[i] = u.ID
		}
		return ids, lexical.calls
	}

	browseIDs, browseCalls := browseSearch(t, query)
	recallIDs, recallCalls := recallRoute(t, query)

	if len(browseIDs) == 0 {
		t.Fatal("/ui/units?q= found nothing — this test would pass vacuously")
	}
	if len(browseIDs) != len(recallIDs) {
		t.Fatalf("/ui/units?q= returned %v, /recall returned %v — different result sets for the same query", browseIDs, recallIDs)
	}
	for i := range browseIDs {
		if browseIDs[i] != recallIDs[i] {
			t.Errorf("/ui/units?q= order %v does not match /recall order %v at index %d", browseIDs, recallIDs, i)
		}
	}
	if browseCalls != 1 {
		t.Errorf("/ui/units?q= called SearchLexical %d times, want exactly 1", browseCalls)
	}
	if recallCalls != 1 {
		t.Errorf("POST /recall called SearchLexical %d times, want exactly 1", recallCalls)
	}

	t.Run("below the admission floor both answer empty", func(t *testing.T) {
		belowFloor := "a query with no vector or lexical match at all"
		belowIDs, _ := browseSearch(t, belowFloor)
		belowRecallIDs, _ := recallRoute(t, belowFloor)
		if len(belowIDs) != 0 {
			t.Errorf("/ui/units?q= below the floor returned %v, want empty", belowIDs)
		}
		if len(belowRecallIDs) != 0 {
			t.Errorf("/recall below the floor returned %v, want empty", belowRecallIDs)
		}
	})
}

// extractDataUnitIDs pulls every `data-unit-id="…"` value out of an
// internal/ui rendered fragment, in document order — this test's own
// window into what units.templ actually rendered, without depending on
// its surrounding markup.
func extractDataUnitIDs(page string) []string {
	const marker = `data-unit-id="`
	var ids []string
	for {
		i := strings.Index(page, marker)
		if i < 0 {
			return ids
		}
		page = page[i+len(marker):]
		end := strings.Index(page, `"`)
		if end < 0 {
			return ids
		}
		ids = append(ids, page[:end])
		page = page[end:]
	}
}
