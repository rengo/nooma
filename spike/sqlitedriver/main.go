// Spike for ADR-0001: can ncruces/go-sqlite3 (cgo-free) carry sqlite-vec and FTS5
// well enough to be Nooma's storage engine?
//
// THROWAWAY CODE. This branch is never merged. Its output is a decision recorded
// in docs/adr/0001-sqlite-driver.md, not code that ships. No error handling
// discipline, no tests, no dependency rule — it exists to produce numbers.
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	"github.com/ncruces/go-sqlite3"
)

const (
	dim      = 768 // nomic-embed-text, the Ollama default from ADR-0003
	units    = 10_000
	queries  = 200
	rrfK     = 60
	topK     = 20
	ftsOps   = 1_000
	writeOps = 500
)

var results []result

type result struct {
	n      int
	name   string
	passed bool
	detail string
}

func check(n int, name string, passed bool, format string, a ...any) {
	results = append(results, result{n, name, passed, fmt.Sprintf(format, a...)})
}

func main() {
	dir, err := os.MkdirTemp("", "nooma-spike-*")
	must(err)
	defer os.RemoveAll(dir)
	vault := filepath.Join(dir, "nooma.db")

	db, err := sqlite3.Open("file:" + vault)
	must(err)
	defer db.Close()

	criterion3(db)
	schema(db)
	corpus := seed(db)
	criterion1(db)
	criterion2(db)
	criterion4(db, dir)
	criterion67(db, corpus)

	report()
}

// ---------------------------------------------------------------- criterion 3

func criterion3(db *sqlite3.Conn) {
	must(db.Exec(`PRAGMA journal_mode = WAL`))
	must(db.Exec(`PRAGMA foreign_keys = ON`))
	must(db.BusyTimeout(5 * time.Second))

	journal := scalar(db, `PRAGMA journal_mode`)
	fk := scalar(db, `PRAGMA foreign_keys`)

	ok := journal == "wal" && fk == "1"
	check(3, "operational PRAGMAs", ok, "journal_mode=%s foreign_keys=%s busy_timeout=5s", journal, fk)
}

// ------------------------------------------------------------------- schema

func schema(db *sqlite3.Conn) {
	must(db.Exec(fmt.Sprintf(`
		CREATE TABLE units (
		  id              TEXT PRIMARY KEY,
		  type            TEXT NOT NULL,
		  content         TEXT NOT NULL,
		  status          TEXT NOT NULL DEFAULT 'pool',
		  weight          REAL NOT NULL DEFAULT 1.0,
		  last_touched_at TEXT NOT NULL,
		  created_at      TEXT NOT NULL
		);
		CREATE INDEX idx_units_status_touched ON units(status, last_touched_at);

		CREATE VIRTUAL TABLE units_fts USING fts5(content, content='units', content_rowid='rowid');

		CREATE TRIGGER units_ai AFTER INSERT ON units BEGIN
		  INSERT INTO units_fts(rowid, content) VALUES (new.rowid, new.content);
		END;
		CREATE TRIGGER units_ad AFTER DELETE ON units BEGIN
		  INSERT INTO units_fts(units_fts, rowid, content) VALUES('delete', old.rowid, old.content);
		END;
		CREATE TRIGGER units_au AFTER UPDATE ON units BEGIN
		  INSERT INTO units_fts(units_fts, rowid, content) VALUES('delete', old.rowid, old.content);
		  INSERT INTO units_fts(rowid, content) VALUES (new.rowid, new.content);
		END;

		CREATE VIRTUAL TABLE unit_embeddings USING vec0(unit_id TEXT PRIMARY KEY, embedding FLOAT[%d]);
	`, dim)))
}

// -------------------------------------------------------------------- corpus

var vocab = strings.Fields(`brain memory vault decay weight relation trigger consolidation
	nudge belief energy focus capture recall insight loop pattern signal threshold digest
	telegram schedule archive resurface graph confidence timer prospection glass box`)

func seed(db *sqlite3.Conn) [][]float32 {
	rng := rand.New(rand.NewSource(42)) // deterministic: the spike must be reproducible
	corpus := make([][]float32, 0, units)

	must(db.Exec(`BEGIN`))
	insUnit, _, err := db.Prepare(`INSERT INTO units(id,type,content,last_touched_at,created_at) VALUES (?,?,?,?,?)`)
	must(err)
	insVec, _, err := db.Prepare(`INSERT INTO unit_embeddings(unit_id, embedding) VALUES (?,?)`)
	must(err)

	now := time.Now().UTC().Format(time.RFC3339)
	for i := range units {
		id := fmt.Sprintf("unit-%05d", i)
		text := sentence(rng)
		vec := randomVector(rng)
		corpus = append(corpus, vec)

		bindAll(insUnit, id, "knowledge", text, now, now)
		mustStep(insUnit)
		bindAll(insVec, id)
		must(insVec.BindBlob(2, serialize(vec)))
		mustStep(insVec)
	}
	insUnit.Close()
	insVec.Close()
	must(db.Exec(`COMMIT`))
	return corpus
}

func sentence(rng *rand.Rand) string {
	w := make([]string, 8)
	for i := range w {
		w[i] = vocab[rng.Intn(len(vocab))]
	}
	return strings.Join(w, " ")
}

func randomVector(rng *rand.Rand) []float32 {
	v := make([]float32, dim)
	var norm float64
	for i := range v {
		v[i] = float32(rng.NormFloat64())
		norm += float64(v[i]) * float64(v[i])
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] /= float32(norm)
	}
	return v
}

func serialize(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// ---------------------------------------------------------------- criterion 1

func criterion1(db *sqlite3.Conn) {
	count := scalar(db, `SELECT count(*) FROM unit_embeddings`)
	rng := rand.New(rand.NewSource(7))
	hits := knn(db, randomVector(rng), topK)
	ok := count == fmt.Sprint(units) && len(hits) == topK
	check(1, "sqlite-vec loads, vec0 answers KNN", ok,
		"%s vectors of dim %d stored, KNN returned %d neighbours", count, dim, len(hits))
}

func knn(db *sqlite3.Conn, q []float32, k int) []string {
	stmt, _, err := db.Prepare(`SELECT unit_id FROM unit_embeddings WHERE embedding MATCH ? AND k = ?`)
	must(err)
	defer stmt.Close()
	must(stmt.BindBlob(1, serialize(q)))
	must(stmt.BindInt(2, k))

	var out []string
	for stmt.Step() {
		out = append(out, stmt.ColumnText(0))
	}
	must(stmt.Err())
	return out
}

// ---------------------------------------------------------------- criterion 2

func criterion2(db *sqlite3.Conn) {
	rng := rand.New(rand.NewSource(99))
	must(db.Exec(`BEGIN`))
	for i := range ftsOps {
		switch i % 3 {
		case 0:
			id := fmt.Sprintf("extra-%05d", i)
			must(db.Exec(fmt.Sprintf(
				`INSERT INTO units(id,type,content,last_touched_at,created_at) VALUES ('%s','task','%s','t','t')`,
				id, sentence(rng))))
		case 1:
			must(db.Exec(fmt.Sprintf(
				`UPDATE units SET content='%s' WHERE id='unit-%05d'`, sentence(rng), rng.Intn(units))))
		case 2:
			must(db.Exec(fmt.Sprintf(`DELETE FROM units WHERE id='extra-%05d'`, (i/3)*3)))
		}
	}
	must(db.Exec(`COMMIT`))

	// 'integrity-check' rebuilds nothing; it reports whether the index matches the table.
	err := db.Exec(`INSERT INTO units_fts(units_fts) VALUES('integrity-check')`)
	inSync := err == nil

	nUnits := scalar(db, `SELECT count(*) FROM units`)
	nFts := scalar(db, `SELECT count(*) FROM units_fts`)
	ok := inSync && nUnits == nFts
	detail := fmt.Sprintf("%d mixed ops; units=%s fts=%s; integrity-check", ftsOps, nUnits, nFts)
	if inSync {
		detail += " clean"
	} else {
		detail += fmt.Sprintf(" FAILED: %v", err)
	}
	check(2, "FTS5 stays in sync through triggers", ok, "%s", detail)
}

// ---------------------------------------------------------------- criterion 4

func criterion4(db *sqlite3.Conn, dir string) {
	integrity := scalar(db, `PRAGMA integrity_check`)

	backup := filepath.Join(dir, "backup.db")
	errVacuum := db.Exec(fmt.Sprintf(`VACUUM INTO '%s'`, backup))

	var size int64
	if fi, err := os.Stat(backup); err == nil {
		size = fi.Size()
	}
	// The backup must be a usable vault, not just a file that exists.
	readable := false
	if bdb, err := sqlite3.Open("file:" + backup); err == nil {
		readable = scalar(bdb, `SELECT count(*) FROM units`) != ""
		bdb.Close()
	}

	ok := integrity == "ok" && errVacuum == nil && readable
	check(4, "VACUUM INTO and integrity_check with WAL open", ok,
		"integrity_check=%s; backup %d KB, reopened and readable=%v", integrity, size/1024, readable)
}

// ------------------------------------------------------------- criteria 6 & 7

func criterion67(db *sqlite3.Conn, corpus [][]float32) {
	rng := rand.New(rand.NewSource(1234))

	// --- 6: hybrid recall latency
	lat := make([]time.Duration, 0, queries)
	for range queries {
		q := corpus[rng.Intn(len(corpus))]
		term := vocab[rng.Intn(len(vocab))]
		start := time.Now()
		vecHits := knn(db, q, topK)
		ftsHits := fts(db, term, topK)
		_ = rrf(vecHits, ftsHits)
		lat = append(lat, time.Since(start))
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p50 := lat[len(lat)*50/100]
	p95 := lat[len(lat)*95/100]
	p99 := lat[len(lat)*99/100]

	check(6, "hybrid recall p95 < 100ms over 10k units", p95 < 100*time.Millisecond,
		"p50=%v p95=%v p99=%v over %d queries", round(p50), round(p95), round(p99), queries)

	// --- 7: write throughput (DB path only; embedding generation is provider latency)
	insUnit, _, err := db.Prepare(`INSERT INTO units(id,type,content,last_touched_at,created_at) VALUES (?,?,?,?,?)`)
	must(err)
	defer insUnit.Close()
	insVec, _, err := db.Prepare(`INSERT INTO unit_embeddings(unit_id, embedding) VALUES (?,?)`)
	must(err)
	defer insVec.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	start := time.Now()
	for i := range writeOps {
		// One transaction per capture: a capture is a user-facing event, not a batch.
		must(db.Exec(`BEGIN`))
		id := fmt.Sprintf("w-%05d", i)
		bindAll(insUnit, id, "task", sentence(rng), now, now)
		mustStep(insUnit)
		bindAll(insVec, id)
		must(insVec.BindBlob(2, serialize(randomVector(rng))))
		mustStep(insVec)
		must(db.Exec(`COMMIT`))
	}
	elapsed := time.Since(start)
	rate := float64(writeOps) / elapsed.Seconds()

	check(7, "write throughput >= 50 units/s", rate >= 50,
		"%.0f units/s (%d captures in %v, one transaction each, unit+vector+FTS)",
		rate, writeOps, round(elapsed))
}

func fts(db *sqlite3.Conn, term string, k int) []string {
	stmt, _, err := db.Prepare(`SELECT u.id FROM units_fts f JOIN units u ON u.rowid = f.rowid
	                            WHERE units_fts MATCH ? ORDER BY rank LIMIT ?`)
	must(err)
	defer stmt.Close()
	must(stmt.BindText(1, term))
	must(stmt.BindInt(2, k))

	var out []string
	for stmt.Step() {
		out = append(out, stmt.ColumnText(0))
	}
	must(stmt.Err())
	return out
}

// rrf is the fusion from ADR-0010, here only to make the latency measurement honest:
// the real recall path pays for fusion too.
func rrf(lists ...[]string) []string {
	score := map[string]float64{}
	for _, l := range lists {
		for i, id := range l {
			score[id] += 1.0 / float64(rrfK+i+1)
		}
	}
	out := make([]string, 0, len(score))
	for id := range score {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return score[out[i]] > score[out[j]] })
	return out
}

// -------------------------------------------------------------------- report

func report() {
	fmt.Printf("\nADR-0001 spike — ncruces/go-sqlite3 + sqlite-vec\n")
	fmt.Printf("%s\n", strings.Repeat("=", 78))
	failed := 0
	for _, r := range results {
		mark := "PASS"
		if !r.passed {
			mark = "FAIL"
			failed++
		}
		fmt.Printf("[%s] criterion %d — %s\n         %s\n", mark, r.n, r.name, r.detail)
	}
	fmt.Printf("%s\n", strings.Repeat("=", 78))
	fmt.Printf("%d/%d measured criteria passed (5 is a separate cross-compilation check)\n",
		len(results)-failed, len(results))
}

// --------------------------------------------------------------------- utils

func scalar(db *sqlite3.Conn, q string) string {
	stmt, _, err := db.Prepare(q)
	must(err)
	defer stmt.Close()
	if stmt.Step() {
		return stmt.ColumnText(0)
	}
	return ""
}

func bindAll(stmt *sqlite3.Stmt, vals ...string) {
	must(stmt.Reset())
	for i, v := range vals {
		must(stmt.BindText(i+1, v))
	}
}

func mustStep(stmt *sqlite3.Stmt) {
	stmt.Step()
	must(stmt.Err())
}

func round(d time.Duration) time.Duration { return d.Round(time.Microsecond * 10) }

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "spike failed:", err)
		os.Exit(1)
	}
}
