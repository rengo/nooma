package mdfence

import (
	"strings"
	"testing"
)

const document = `# Title

Prose that mentions ` + "```yaml" + ` inline, which is not a fence.

## Configuration — ` + "`nooma.yml`" + `

Some prose.

` + "```yaml" + `
server:
  bind: 127.0.0.1
` + "```" + `

More prose.

## Another section

` + "```yaml" + `
this: belongs-to-another-section
` + "```" + `

### A subsection of Another section

` + "```yaml" + `
also: not-ours
` + "```" + `
`

// TestExtractScopesToItsSection is the property neither existing extractor in
// this repository has, and the one that keeps this gate from passing by luck.
// docs/01-architecture.md happens to contain exactly one ```yaml fence today, so
// a whole-document scan would find it and pass — until the day a second yaml
// example appears anywhere in the file, at which point a whole-document scan
// silently compares against whichever came first.
func TestExtractScopesToItsSection(t *testing.T) {
	t.Parallel()

	got, err := Extract([]byte(document), "Configuration", "yaml")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(string(got), "bind: 127.0.0.1") {
		t.Errorf("got the wrong section's fence:\n%s", got)
	}
	if strings.Contains(string(got), "belongs-to-another-section") {
		t.Error("Extract crossed into the following section")
	}
}

// TestExtractStopsAtTheNextHeadingOfEqualOrHigherLevel pins where a section ends.
// A subsection belongs to its parent, so a fence inside one is in scope; the next
// sibling heading is where the section stops.
func TestExtractStopsAtTheNextHeadingOfEqualOrHigherLevel(t *testing.T) {
	t.Parallel()

	const nested = `## Ours

### A subsection

` + "```yaml" + `
in: scope
` + "```" + `

## Not ours

` + "```yaml" + `
out: of-scope
` + "```" + `
`

	got, err := Extract([]byte(nested), "Ours", "yaml")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(string(got), "in: scope") {
		t.Errorf("a fence inside a subsection should be in scope:\n%s", got)
	}
}

// TestExtractIgnoresHeadingsInsideAFence is the case that synthetic fixtures miss
// and the real document has twice. A YAML comment is `# text` — byte for byte a
// Markdown H1. A heading scanner that does not track fence state reads
// `# Reusable providers, shared across multiple tasks` as a level-1 heading,
// decides the section ended there, and cuts the fence in half.
//
// The failure is loud (an unterminated fence) rather than silent, but only by
// luck: had the comment sat after the closing ``` instead of inside, the section
// would simply have ended early and the gate would have compared against nothing
// while looking healthy.
func TestExtractIgnoresHeadingsInsideAFence(t *testing.T) {
	t.Parallel()

	const withComments = "## Ours\n\n```yaml\nserver:\n  bind: 127.0.0.1\n\n# Reusable providers, shared across multiple tasks\nproviders:\n  p:\n    type: anthropic\n\n### not a heading either\ntasks:\n  chat: { provider: p }\n```\n\n## Next\n"

	got, err := Extract([]byte(withComments), "Ours", "yaml")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, want := range []string{"bind: 127.0.0.1", "# Reusable providers", "tasks:"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("extracted block lost %q — the fence was cut at a YAML comment:\n%s", want, got)
		}
	}
}

// TestExtractRequiresExactlyOne is the arity half. Zero and two are both errors,
// and both name the section, because "which block documents this" is not a
// question a test may answer by picking.
func TestExtractRequiresExactlyOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		wantErr  string
	}{
		{
			name:     "zero fences of the requested language",
			document: "## Ours\n\nProse only.\n\n## Next\n",
			wantErr:  "found 0",
		},
		{
			name:     "two fences in the same section",
			document: "## Ours\n\n```yaml\na: 1\n```\n\n```yaml\nb: 2\n```\n",
			wantErr:  "found 2",
		},
		{
			name:     "the section does not exist",
			document: "## Something else\n\n```yaml\na: 1\n```\n",
			wantErr:  "no section",
		},
		{
			name:     "an unterminated fence",
			document: "## Ours\n\n```yaml\na: 1\n",
			wantErr:  "unterminated",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Extract([]byte(tc.document), "Ours", "yaml")
			if err == nil {
				t.Fatalf("Extract accepted %s and returned:\n%s", tc.name, got)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error does not say %q:\n%v", tc.wantErr, err)
			}
			if got != nil {
				t.Error("Extract returned content alongside an error")
			}
		})
	}
}

// TestExtractIgnoresOtherLanguages keeps a section that documents more than one
// format honest: asking for yaml must not return the sql block next to it.
func TestExtractIgnoresOtherLanguages(t *testing.T) {
	t.Parallel()

	const mixed = "## Ours\n\n```sql\nCREATE TABLE t (id TEXT);\n```\n\n```yaml\nkey: value\n```\n"

	got, err := Extract([]byte(mixed), "Ours", "yaml")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(string(got), "key: value") || strings.Contains(string(got), "CREATE TABLE") {
		t.Errorf("Extract did not filter by language:\n%s", got)
	}
}

// TestExtractIgnoresCommentedOutFences follows the precedent set by
// goldenset.ExtractJSONFence: a fence inside an HTML comment is invisible in the
// rendered document, so the honest reading is that it does not exist. Counting it
// would make a commented-out example ambiguous with a live one.
func TestExtractIgnoresCommentedOutFences(t *testing.T) {
	t.Parallel()

	const commented = "## Ours\n\n<!--\n```yaml\nold: example\n```\n-->\n\n```yaml\nlive: example\n```\n"

	got, err := Extract([]byte(commented), "Ours", "yaml")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(string(got), "live: example") {
		t.Errorf("Extract returned the commented-out fence:\n%s", got)
	}
}

// TestExtractMatchesTheSectionByPrefix lets a caller name a section without
// reproducing its full heading, which in this repository carries backticks and an
// em dash. Matching on a substring of the heading text keeps the caller readable
// and the document free to be typographically fussy.
func TestExtractMatchesTheSectionByPrefix(t *testing.T) {
	t.Parallel()

	const fussy = "## Configuration — `nooma.yml`\n\n```yaml\nkey: value\n```\n"

	if _, err := Extract([]byte(fussy), "Configuration", "yaml"); err != nil {
		t.Fatalf("Extract could not find a section by a substring of its heading: %v", err)
	}
}
