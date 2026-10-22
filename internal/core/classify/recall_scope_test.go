package classify

import (
	"slices"
	"testing"
	"time"
)

// recallPayload is a recall whose only scope is the one a test adds to it.
func recallPayload(extra string) string {
	return `{"type":"recall","normalized_content":"¿qué tengo?","weight":0.2,"decay_rate":0.3` + extra + `}`
}

// TestAllRecallWindowOnsSwept pins recall_scope's window vocabulary
// (ADR-0048), so a member added later fails here until it is named.
func TestAllRecallWindowOnsSwept(t *testing.T) {
	assertVocabulary(t, AllRecallWindowOns(), map[RecallWindowOn]bool{RecallOnTold: true, RecallOnWhen: true})
}

// TestDecode_RecallScope: the model's reading of what a recall asks about and
// when decodes as typed fields (ADR-0048), and a scope with any malformed part
// degrades whole, alone (I14) — a recall then answers as if it named none.
func TestDecode_RecallScope(t *testing.T) {
	t.Run("terms and a window decode", func(t *testing.T) {
		c, err := Decode(recallPayload(`,"recall_scope":{"terms":[" dentista ","odontólogo","","dentista","Dentistas"],"window":{"on":"when",`+
			`"from":"2026-08-01T12:00:00-05:00","to":"2026-08-03T00:00:00-05:00"}}`), testNow)
		if err != nil || len(c.Degradations) != 0 {
			t.Fatalf("Decode = %v, degraded %v; want clean", err, c.Degradations)
		}
		s := c.RecallScope
		// Trimmed, blanks and repeats dropped, in the model's order: the
		// code keeps what the model wrote and adds no word of its own.
		if want := []string{"dentista", "odontólogo", "Dentistas"}; s == nil || !slices.Equal(s.Terms, want) {
			t.Fatalf("RecallScope = %+v, want terms %q", s, want)
		}
		w := s.Window
		from := time.Date(2026, 8, 1, 12, 0, 0, 0, testZone)
		to := time.Date(2026, 8, 3, 0, 0, 0, 0, testZone)
		if w == nil || w.On != RecallOnWhen || !w.From.Equal(from) || !w.To.Equal(to) {
			t.Errorf("Window = %+v, want when from %v to %v", w, from, to)
		}
	})
	t.Run("a window of bare days is their midnights in the user's zone", func(t *testing.T) {
		c, _ := Decode(recallPayload(`,"recall_scope":{"window":{"on":"told","from":"2026-07-30","to":"2026-07-31"}}`), testNow)
		if len(c.Degradations) != 0 || c.RecallScope == nil || c.RecallScope.Terms != nil || c.RecallScope.Window == nil {
			t.Fatalf("got %+v degraded %v, want a window alone", c.RecallScope, c.Degradations)
		}
		w := c.RecallScope.Window
		if w.On != RecallOnTold || !w.From.Equal(time.Date(2026, 7, 30, 0, 0, 0, 0, testZone)) ||
			!w.To.Equal(time.Date(2026, 7, 31, 0, 0, 0, 0, testZone)) {
			t.Errorf("Window = %+v, want told over 30 Jul", w)
		}
	})
	for _, tc := range []struct{ name, extra string }{
		{"absent", ``},
		{"null", `,"recall_scope":null`},
		{"empty", `,"recall_scope":{}`},
		{"blank terms and no window", `,"recall_scope":{"terms":["  ",""],"window":null}`},
		{"no terms and no window", `,"recall_scope":{"terms":[]}`},
	} {
		t.Run(tc.name+" is no scope and no loss", func(t *testing.T) {
			c, _ := Decode(recallPayload(tc.extra), testNow)
			if len(c.Degradations) != 0 || c.RecallScope != nil {
				t.Errorf("got %+v degraded %v, want no scope, clean", c.RecallScope, c.Degradations)
			}
		})
	}
	for _, tc := range []struct {
		name, extra string
		reason      Reason
	}{
		{"a scope that is not an object", `,"recall_scope":"oculista"`, ReasonWrongType},
		{"terms that are not a list", `,"recall_scope":{"terms":"dentista"}`, ReasonWrongType},
		{"a term that is not text", `,"recall_scope":{"terms":["dentista",3]}`, ReasonWrongType},
		{"a window that is not an object", `,"recall_scope":{"terms":["x"],"window":"ayer"}`, ReasonWrongType},
		{"an unknown on", `,"recall_scope":{"terms":["x"],"window":{"on":"said","from":"2026-07-30","to":"2026-07-31"}}`, ReasonUnknownEnum},
		{"no on", `,"recall_scope":{"window":{"from":"2026-07-30","to":"2026-07-31"}}`, ReasonUnknownEnum},
		{"an unreadable from", `,"recall_scope":{"window":{"on":"told","from":"ayer","to":"2026-07-31"}}`, ReasonBadFormat},
		{"no to", `,"recall_scope":{"window":{"on":"told","from":"2026-07-30"}}`, ReasonBadFormat},
		{"an empty window", `,"recall_scope":{"window":{"on":"told","from":"2026-07-30","to":"2026-07-30"}}`, ReasonBadFormat},
		{"a window that ends before it starts", `,"recall_scope":{"window":{"on":"when","from":"2026-07-31","to":"2026-07-30"}}`, ReasonBadFormat},
	} {
		t.Run(tc.name+" degrades the whole scope", func(t *testing.T) {
			c, err := Decode(recallPayload(tc.extra), testNow)
			if err != nil {
				t.Fatal(err)
			}
			assertOnlyDegraded(t, c, "recall_scope", tc.reason)
			if c.RecallScope != nil {
				t.Errorf("RecallScope = %+v, want nil", c.RecallScope)
			}
			if c.Kind == nil || c.NormalizedContent == nil {
				t.Errorf("the rest of the classification did not survive: %+v", c)
			}
		})
	}
}
