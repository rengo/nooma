package schema

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// fenceStartPattern recognizes a fenced code block's opening line whose
// info string is exactly "sql" (design §6.4 step 1) — a fence with any
// other info string (docs/03-data-model.md's own ```go``` fts5.Register
// snippet) is not SQL and must never be scanned for CREATE statements.
var fenceStartPattern = regexp.MustCompile("^```sql\\s*$")

// fenceEndPattern recognizes a closing fence line: three backticks and
// nothing else but trailing whitespace.
var fenceEndPattern = regexp.MustCompile("^```\\s*$")

// createStatementPattern recognizes that a statement begins with the
// CREATE keyword at all (design §6.4 step 4) — the gate for whether a
// statement is even a candidate for classification. Anything that does not
// match this is ignored (a sample query added to a ```sql``` fence later),
// per docs/03-data-model.md's own documented convention. Anything that DOES
// match this but fails classifyStatement below is a loud error, never a
// silent skip (this task's own trap-avoidance requirement).
var createStatementPattern = regexp.MustCompile(`(?is)^\s*CREATE\b`)

// createNamePattern is design §6.4 step 5's exact regex: case- and
// newline-insensitive, because docs/03-data-model.md writes
// "CREATE UNIQUE INDEX idx_units_unique_active_insight" with its ON clause
// on the next line.
var createNamePattern = regexp.MustCompile(
	`(?is)^\s*CREATE\s+(TABLE|VIRTUAL\s+TABLE|UNIQUE\s+INDEX|INDEX|TRIGGER|VIEW)\s+(?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][A-Za-z0-9_]*)`,
)

// beginEndWordPattern finds bare BEGIN/END word tokens (design §6.4 step
// 3's trigger-aware statement splitting), scanned over the string-masked
// text so a "BEGIN" or "END" appearing inside a string literal (unlikely,
// but not this parser's job to assume) is invisible to it.
var beginEndWordPattern = regexp.MustCompile(`(?i)\bBEGIN\b|\bEND\b`)

// constraintKeywords are the table-level constraint keywords design §6.4
// step 6 names: an item whose first token is one of these is a constraint
// (e.g. relations' own "UNIQUE (from_unit_id, to_unit_id, type)",
// docs/03-data-model.md), not a column.
var constraintKeywords = map[string]bool{
	"PRIMARY":    true,
	"UNIQUE":     true,
	"CHECK":      true,
	"FOREIGN":    true,
	"CONSTRAINT": true,
}

// identPattern extracts a leading SQL identifier — the column or object
// name at the start of an already-trimmed fragment.
var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)

// ParseMarkdown parses every CREATE TABLE, CREATE VIRTUAL TABLE, CREATE
// INDEX, CREATE UNIQUE INDEX, CREATE TRIGGER and CREATE VIEW statement out
// of docs/03-data-model.md's fenced ```sql``` blocks (design §6.4), the
// doc-03 side of the comparison R4.3 requires. The returned Objects are
// sorted the same way Marshal/ParseGolden sort theirs (Sort), so a direct
// comparison against ParseGolden's output needs no separate normalization
// step.
//
// ParseMarkdown never silently drops a statement it does not understand:
// a statement inside a ```sql``` fence that does not even start with
// CREATE (a sample query) is ignored, per docs/03-data-model.md's own
// documented convention (step 4) — it can never be a schema object R4.3
// compares. But a statement that DOES start with CREATE and still fails
// to match any recognized object shape (step 5) is a loud, named error,
// never a quiet omission: silently skipping it would let doc 03 and the
// schema disagree while this gate stays green, exactly the false-assurance
// failure mode a comparison gate must not produce.
func ParseMarkdown(md []byte) ([]Object, error) {
	var objs []Object

	for _, fence := range extractSQLFences(md) {
		stripped := stripLineComments(fence)
		mask := maskStrings(stripped)

		for _, r := range statementRanges(mask) {
			stmt := stripped[r[0]:r[1]]
			stmtMask := string(mask[r[0]:r[1]])
			trimmed := strings.TrimSpace(stmt)
			if trimmed == "" {
				continue
			}
			if !createStatementPattern.MatchString(trimmed) {
				// Not a CREATE statement at all — a sample query added
				// later (design §6.4 step 4). Ignored, not mis-parsed.
				continue
			}

			kind, name, ok := classifyStatement(trimmed)
			if !ok {
				return nil, fmt.Errorf(
					"schema.ParseMarkdown: unrecognized CREATE statement shape, cannot classify: %q",
					oneLine(trimmed),
				)
			}

			obj := Object{Kind: kind, Name: name}
			if kind == KindTable || kind == KindVirtualTable {
				body, bodyMask, ok := extractBody(stmt, stmtMask)
				if !ok {
					return nil, fmt.Errorf(
						"schema.ParseMarkdown: %s %s has no parenthesized body: %q",
						kind, name, oneLine(trimmed),
					)
				}
				if kind == KindTable {
					obj.Columns = tableColumns(body, bodyMask)
				} else {
					obj.Columns = virtualTableColumns(body, bodyMask)
				}
			}
			objs = append(objs, obj)
		}
	}

	Sort(objs)
	return objs, nil
}

// extractSQLFences returns the text of every fenced code block in md whose
// info string is exactly "sql" (design §6.4 step 1).
func extractSQLFences(md []byte) []string {
	var fences []string
	var current []string
	inFence := false

	for _, line := range strings.Split(string(md), "\n") {
		line = strings.TrimRight(line, "\r")
		if !inFence {
			if fenceStartPattern.MatchString(line) {
				inFence = true
				current = nil
			}
			continue
		}
		if fenceEndPattern.MatchString(line) {
			fences = append(fences, strings.Join(current, "\n"))
			inFence = false
			continue
		}
		current = append(current, line)
	}
	// An unterminated fence at end-of-file contributes nothing — doc 03
	// always closes its fences; there is no real-world shape here worth
	// guessing about.
	return fences
}

// stripLineComments removes a "--" and everything after it to end of line,
// string-aware (design §6.4 step 2): a "--" inside a single-quoted string
// literal is data, never a comment start. Two consecutive single quotes are
// the escape for a literal quote character inside a string.
func stripLineComments(sql string) string {
	var b strings.Builder
	runes := []rune(sql)
	inString := false

	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if inString {
			b.WriteRune(c)
			if c == '\'' {
				if i+1 < len(runes) && runes[i+1] == '\'' {
					b.WriteRune(runes[i+1])
					i++
					continue
				}
				inString = false
			}
			continue
		}
		if c == '\'' {
			inString = true
			b.WriteRune(c)
			continue
		}
		if c == '-' && i+1 < len(runes) && runes[i+1] == '-' {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			if i < len(runes) {
				b.WriteRune('\n')
			}
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// maskStrings returns a byte slice the same length as sql where every
// character INSIDE a single-quoted string literal (but not the quotes
// themselves) is replaced with 'x'. Used by every downstream structural
// scan (statement splitting, paren-depth counting, comma splitting,
// BEGIN/END detection) so a semicolon, parenthesis, comma or bare keyword
// that happens to appear inside string data is invisible to them — the
// same string-awareness stripLineComments already applies to "--".
func maskStrings(sql string) []byte {
	mask := []byte(sql)
	inString := false
	for i := 0; i < len(mask); i++ {
		c := mask[i]
		if inString {
			if c == '\'' {
				if i+1 < len(mask) && mask[i+1] == '\'' {
					mask[i] = 'x'
					mask[i+1] = 'x'
					i++
					continue
				}
				inString = false
				continue
			}
			if c != '\n' {
				mask[i] = 'x'
			}
			continue
		}
		if c == '\'' {
			inString = true
		}
	}
	return mask
}

// statementRanges splits mask into [start, end) byte ranges, one per
// statement, at every top-level ";" (design §6.4 step 3): a ";" inside a
// CREATE TRIGGER's BEGIN...END body does not end the statement — only the
// ";" following the balancing END does. A non-empty, non-whitespace
// remainder after the last ";" is still emitted as one final range (rather
// than silently dropped) so an unterminated statement is still handed to
// the classifier, which will name it in an error instead of this scanner
// quietly discarding it.
func statementRanges(mask []byte) [][2]int {
	matches := beginEndWordPattern.FindAllIndex(mask, -1)
	matchIdx := 0
	depth := 0
	start := 0
	var ranges [][2]int

	for i := 0; i < len(mask); i++ {
		for matchIdx < len(matches) && matches[matchIdx][0] == i {
			word := strings.ToUpper(string(mask[matches[matchIdx][0]:matches[matchIdx][1]]))
			switch word {
			case "BEGIN":
				depth++
			case "END":
				if depth > 0 {
					depth--
				}
			}
			matchIdx++
		}
		if mask[i] == ';' && depth == 0 {
			ranges = append(ranges, [2]int{start, i})
			start = i + 1
		}
	}
	if strings.TrimSpace(string(mask[start:])) != "" {
		ranges = append(ranges, [2]int{start, len(mask)})
	}
	return ranges
}

// classifyStatement matches design §6.4 step 5's regex against an already
// CREATE-prefixed statement, returning its Kind and declared name. ok is
// false when the statement starts with CREATE but does not match any
// recognized object shape (e.g. "CREATE FOREIGN TABLE") — the caller
// treats that as a loud error, never a silent skip.
func classifyStatement(stmt string) (Kind, string, bool) {
	m := createNamePattern.FindStringSubmatch(stmt)
	if m == nil {
		return "", "", false
	}
	name := m[2]
	switch strings.ToUpper(strings.Join(strings.Fields(m[1]), " ")) {
	case "TABLE":
		return KindTable, name, true
	case "VIRTUAL TABLE":
		return KindVirtualTable, name, true
	case "INDEX":
		return KindIndex, name, true
	case "UNIQUE INDEX":
		return KindUniqueIndex, name, true
	case "TRIGGER":
		return KindTrigger, name, true
	case "VIEW":
		return KindView, name, true
	default:
		return "", "", false
	}
}

// extractBody returns the text between a statement's outermost matching
// parentheses (and the corresponding slice of its mask), used for CREATE
// TABLE's column list and CREATE VIRTUAL TABLE's fts5(...) module argument
// list. ok is false when stmt has no opening parenthesis at all.
func extractBody(stmt, stmtMask string) (body, bodyMask string, ok bool) {
	open := strings.IndexByte(stmtMask, '(')
	if open == -1 {
		return "", "", false
	}
	depth := 0
	for i := open; i < len(stmtMask); i++ {
		switch stmtMask[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return stmt[open+1 : i], stmtMask[open+1 : i], true
			}
		}
	}
	return "", "", false
}

// splitDepthZeroCommaRanges splits mask into [start, end) ranges at every
// comma that sits at paren-depth 0 relative to mask's own start — the
// shared range computation tableColumns and virtualTableColumns both use,
// so a caller can slice the real body text and its mask with identical
// boundaries.
func splitDepthZeroCommaRanges(mask string) [][2]int {
	var ranges [][2]int
	depth := 0
	start := 0
	for i := 0; i < len(mask); i++ {
		switch mask[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				ranges = append(ranges, [2]int{start, i})
				start = i + 1
			}
		}
	}
	ranges = append(ranges, [2]int{start, len(mask)})
	return ranges
}

// tableColumns implements design §6.4 step 6: split body on depth-0
// commas, drop any item whose first token is a table-level constraint
// keyword (PRIMARY, UNIQUE, CHECK, FOREIGN, CONSTRAINT — relations' own
// "UNIQUE (from_unit_id, to_unit_id, type)", docs/03-data-model.md), and
// take the first token of every remaining item as the column name.
func tableColumns(body, bodyMask string) []string {
	var cols []string
	for _, r := range splitDepthZeroCommaRanges(bodyMask) {
		item := strings.TrimSpace(body[r[0]:r[1]])
		if item == "" {
			continue
		}
		first := firstWord(item)
		if first == "" || constraintKeywords[strings.ToUpper(first)] {
			continue
		}
		cols = append(cols, first)
	}
	return cols
}

// virtualTableColumns implements design §6.4 step 7: split body on
// depth-0 commas; an item is a column iff its own mask slice contains no
// depth-0 "=" (an fts5 module option like content='units' does; a bare
// column name like "content" does not).
func virtualTableColumns(body, bodyMask string) []string {
	var cols []string
	for _, r := range splitDepthZeroCommaRanges(bodyMask) {
		item := strings.TrimSpace(body[r[0]:r[1]])
		if item == "" {
			continue
		}
		if strings.Contains(bodyMask[r[0]:r[1]], "=") {
			continue // an fts5 option (content='units'), not a column
		}
		first := firstWord(item)
		if first == "" {
			continue
		}
		cols = append(cols, first)
	}
	return cols
}

// firstWord returns the leading SQL identifier of an already-trimmed
// fragment, or "" if it does not start with one.
func firstWord(s string) string {
	return identPattern.FindString(strings.TrimSpace(s))
}

// oneLine collapses whitespace (including newlines) so an error message
// naming an unparsed statement stays on one readable line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// DifferenceKind names which of design §6.5's three disagreement shapes a
// Difference reports.
type DifferenceKind string

const (
	// DiffMissingFromSchema: declared in doc 03 but absent from the golden.
	DiffMissingFromSchema DifferenceKind = "missing_from_schema"
	// DiffUndeclaredInDoc: present in the golden but not declared in doc 03
	// — this is the assertion that forces the FTS trigger DDL into doc 03
	// (proposal §4.2, R9.2).
	DiffUndeclaredInDoc DifferenceKind = "undeclared_in_doc"
	// DiffColumnMismatch: a table or virtual_table present on both sides
	// whose column sets differ.
	DiffColumnMismatch DifferenceKind = "column_mismatch"
)

// Difference is one disagreement Diff found between docs/03-data-model.md's
// declared objects and the schema golden (design §6.5). OnlyInDoc and
// OnlyInSchema are populated only for DiffColumnMismatch.
type Difference struct {
	DiffKind     DifferenceKind
	Kind         Kind
	Name         string
	OnlyInDoc    []string
	OnlyInSchema []string
}

// objectKey identifies an Object by (Kind, Name) — the pair design §6.5
// says must match ("every object declared in doc 03 exists in the golden,
// with the same kind").
type objectKey struct {
	Kind Kind
	Name string
}

// Diff compares doc (ParseMarkdown's output) against golden (ParseGolden's
// output), asserting exactly what design §6.5 says this L2 gate asserts —
// no more:
//
//   - every object declared in doc 03 exists in the golden, with the same
//     kind (DiffMissingFromSchema otherwise);
//   - every object in the golden is declared in doc 03 (DiffUndeclaredInDoc
//     otherwise);
//   - for table and virtual_table objects present on both sides, the
//     column SETS are equal (DiffColumnMismatch otherwise, both directions).
//
// It deliberately never compares types, NOT NULL, defaults, FK clauses,
// index columns, index predicates or trigger bodies (design §6.5's
// explicit non-assertion list) — all of that is ddl.golden's job, reviewed
// as a diff. Diff never mutates doc or golden. The returned slice is
// sorted deterministically (DiffKind, then kind rank, then name) so a
// caller's rendered report never depends on Go's randomized map iteration
// order.
func Diff(doc, golden []Object) []Difference {
	docByKey := make(map[objectKey]Object, len(doc))
	for _, o := range doc {
		docByKey[objectKey{o.Kind, o.Name}] = o
	}
	goldenByKey := make(map[objectKey]Object, len(golden))
	for _, o := range golden {
		goldenByKey[objectKey{o.Kind, o.Name}] = o
	}

	var diffs []Difference

	for _, o := range doc {
		if _, ok := goldenByKey[objectKey{o.Kind, o.Name}]; !ok {
			diffs = append(diffs, Difference{DiffKind: DiffMissingFromSchema, Kind: o.Kind, Name: o.Name})
		}
	}
	for _, o := range golden {
		if _, ok := docByKey[objectKey{o.Kind, o.Name}]; !ok {
			diffs = append(diffs, Difference{DiffKind: DiffUndeclaredInDoc, Kind: o.Kind, Name: o.Name})
		}
	}
	for key, docObj := range docByKey {
		if docObj.Kind != KindTable && docObj.Kind != KindVirtualTable {
			continue
		}
		goldenObj, ok := goldenByKey[key]
		if !ok {
			continue // already reported as DiffMissingFromSchema above
		}
		onlyInDoc, onlyInSchema := columnSetDiff(docObj.Columns, goldenObj.Columns)
		if len(onlyInDoc) > 0 || len(onlyInSchema) > 0 {
			diffs = append(diffs, Difference{
				DiffKind:     DiffColumnMismatch,
				Kind:         docObj.Kind,
				Name:         docObj.Name,
				OnlyInDoc:    onlyInDoc,
				OnlyInSchema: onlyInSchema,
			})
		}
	}

	sortDifferences(diffs)
	return diffs
}

// columnSetDiff returns the set difference in both directions, each sorted
// for a deterministic report.
func columnSetDiff(docCols, goldenCols []string) (onlyInDoc, onlyInSchema []string) {
	inGolden := make(map[string]bool, len(goldenCols))
	for _, c := range goldenCols {
		inGolden[c] = true
	}
	inDoc := make(map[string]bool, len(docCols))
	for _, c := range docCols {
		inDoc[c] = true
	}
	for _, c := range docCols {
		if !inGolden[c] {
			onlyInDoc = append(onlyInDoc, c)
		}
	}
	for _, c := range goldenCols {
		if !inDoc[c] {
			onlyInSchema = append(onlyInSchema, c)
		}
	}
	sort.Strings(onlyInDoc)
	sort.Strings(onlyInSchema)
	return onlyInDoc, onlyInSchema
}

// sortDifferences orders diffs by (DiffKind precedence, kind rank, name) so
// Diff's output never depends on map iteration order.
func sortDifferences(diffs []Difference) {
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].DiffKind != diffs[j].DiffKind {
			return diffKindOrder(diffs[i].DiffKind) < diffKindOrder(diffs[j].DiffKind)
		}
		if diffs[i].Kind != diffs[j].Kind {
			return Rank(diffs[i].Kind) < Rank(diffs[j].Kind)
		}
		return diffs[i].Name < diffs[j].Name
	})
}

func diffKindOrder(k DifferenceKind) int {
	switch k {
	case DiffMissingFromSchema:
		return 0
	case DiffUndeclaredInDoc:
		return 1
	case DiffColumnMismatch:
		return 2
	default:
		return 3
	}
}
