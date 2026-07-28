// Spike for ADR-0001, second run: ncruces/go-sqlite3 at its CURRENT version, with no
// sqlite-vec, and vector proximity done by brute force in Go.
//
// The first run measured v0.21.3 + sqlite-vec and found the combination only compiles
// pinned to late 2024. This run measures the configuration actually being accepted.
//
// THROWAWAY CODE. This branch is never merged.
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

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/ext/fts5"
)

const (
	dim      = 768
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

	db, err := sqlite3.Open("file:" + filepath.Join(dir, "nooma.db"))
	must(err)
	defer db.Close()

	// FTS5 is opt-in in this driver: registered per connection, not compiled in.
	must(fts5.Register(db))

	fmt.Printf("driver: ncruces/go-sqlite3, no sqlite-vec\n")
	fmt.Printf("sqlite: %s\n\n", scalar(db, `select sqlite_version()`))

	criterion3(db)
	schema(db)
	seed(db)
	idx := criterion1(db)
	criterion2(db)
	criterion4(db, dir)
	criterion67(db, idx)

	report()
}

// ---------------------------------------------------------------- criterion 3

func criterion3(db *sqlite3.Conn) {
	must(db.Exec(`PRAGMA journal_mode = WAL`))
	must(db.Exec(`PRAGMA foreign_keys = ON`))
	must(db.BusyTimeout(5 * time.Second))

	journal, fk := scalar(db, `PRAGMA journal_mode`), scalar(db, `PRAGMA foreign_keys`)
	check(3, "operational PRAGMAs", journal == "wal" && fk == "1",
		"journal_mode=%s foreign_keys=%s busy_timeout=5s", journal, fk)
}

// ------------------------------------------------------------------- schema

// Note the difference from the first run: embeddings are a plain table with a BLOB
// column. No virtual table, no extension. Everything needed to store them is present
// in every SQLite build ever compiled.
func schema(db *sqlite3.Conn) {
	must(db.Exec(`
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

		CREATE TABLE unit_embeddings (
		  unit_id   TEXT PRIMARY KEY REFERENCES units(id) ON DELETE CASCADE,
		  model     TEXT NOT NULL,
		  dim       INTEGER NOT NULL,
		  embedding BLOB NOT NULL
		);

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
	`))
}

// -------------------------------------------------------------------- corpus

var vocab = strings.Fields(`brain memory vault decay weight relation trigger consolidation
	nudge belief energy focus capture recall insight loop pattern signal threshold digest
	telegram schedule archive resurface graph confidence timer prospection glass box`)

func seed(db *sqlite3.Conn) {
	rng := rand.New(rand.NewSource(42))
	must(db.Exec(`BEGIN`))
	insUnit, _, err := db.Prepare(`INSERT INTO units(id,type,content,last_touched_at,created_at) VALUES (?,?,?,?,?)`)
	must(err)
	insVec, _, err := db.Prepare(`INSERT INTO unit_embeddings(unit_id,model,dim,embedding) VALUES (?,?,?,?)`)
	must(err)

	now := time.Now().UTC().Format(time.RFC3339)
	for i := range units {
		id := fmt.Sprintf("unit-%05d", i)
		bindText(insUnit, id, "knowledge", sentence(rng), now, now)
		mustStep(insUnit)

		bindText(insVec, id, "nomic-embed-text")
		must(insVec.BindInt(3, dim))
		must(insVec.BindBlob(4, serialize(randomVector(rng))))
		mustStep(insVec)
	}
	insUnit.Close()
	insVec.Close()
	must(db.Exec(`COMMIT`))
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

func deserialize(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// ---------------------------------------------------------------- criterion 1

// index is the whole vector store: ids and their normalized vectors, resident in RAM.
type index struct {
	ids  []string
	vecs [][]float32
}

// load is a cost the sqlite-vec path does not pay: every vector has to be read out of
// SQLite and deserialized at startup. On a large vault this is the number that hurts,
// so it gets measured rather than assumed.
func load(db *sqlite3.Conn) (index, time.Duration) {
	start := time.Now()
	stmt, _, err := db.Prepare(`SELECT unit_id, embedding FROM unit_embeddings`)
	must(err)
	defer stmt.Close()

	var idx index
	for stmt.Step() {
		idx.ids = append(idx.ids, stmt.ColumnText(0))
		idx.vecs = append(idx.vecs, deserialize(stmt.ColumnRawBlob(1)))
	}
	must(stmt.Err())
	return idx, time.Since(start)
}

func criterion1(db *sqlite3.Conn) index {
	idx, elapsed := load(db)

	// Correctness, not just speed: a vector must be its own nearest neighbour.
	probe := 4242
	hits := idx.knn(idx.vecs[probe], topK)
	selfFound := len(hits) > 0 && hits[0] == idx.ids[probe]

	ok := len(idx.ids) == units && len(hits) == topK && selfFound
	check(1, "vectors stored in SQLite, brute-force KNN in Go", ok,
		"%d vectors of dim %d; index loaded in %v (%s RAM); KNN returned %d, self-match=%v",
		len(idx.ids), dim, round(elapsed), human(len(idx.ids)*dim*4), len(hits), selfFound)
	return idx
}

// knn is the entire vector search. Vectors are unit-normalized, so cosine similarity is
// a dot product and top-K is a selection over the scored slice.
func (idx index) knn(q []float32, k int) []string {
	type hit struct {
		i   int
		sim float32
	}
	hits := make([]hit, len(idx.vecs))
	for i, v := range idx.vecs {
		var s float32
		for j := range q {
			s += q[j] * v[j]
		}
		hits[i] = hit{i, s}
	}
	sort.Slice(hits, func(a, b int) bool { return hits[a].sim > hits[b].sim })

	if k > len(hits) {
		k = len(hits)
	}
	out := make([]string, k)
	for i := range out {
		out[i] = idx.ids[hits[i].i]
	}
	return out
}

// ---------------------------------------------------------------- criterion 2

func criterion2(db *sqlite3.Conn) {
	rng := rand.New(rand.NewSource(99))
	must(db.Exec(`BEGIN`))
	for i := range ftsOps {
		switch i % 3 {
		case 0:
			must(db.Exec(fmt.Sprintf(
				`INSERT INTO units(id,type,content,last_touched_at,created_at) VALUES ('extra-%05d','task','%s','t','t')`,
				i, sentence(rng))))
		case 1:
			must(db.Exec(fmt.Sprintf(
				`UPDATE units SET content='%s' WHERE id='unit-%05d'`, sentence(rng), rng.Intn(units))))
		case 2:
			must(db.Exec(fmt.Sprintf(`DELETE FROM units WHERE id='extra-%05d'`, (i/3)*3)))
		}
	}
	must(db.Exec(`COMMIT`))

	err := db.Exec(`INSERT INTO units_fts(units_fts) VALUES('integrity-check')`)
	nUnits, nFts := scalar(db, `SELECT count(*) FROM units`), scalar(db, `SELECT count(*) FROM units_fts`)

	detail := fmt.Sprintf("%d mixed ops; units=%s fts=%s; integrity-check", ftsOps, nUnits, nFts)
	if err == nil {
		detail += " clean"
	} else {
		detail += fmt.Sprintf(" FAILED: %v", err)
	}
	check(2, "FTS5 stays in sync through triggers", err == nil && nUnits == nFts, "%s", detail)
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
	readable := false
	if bdb, err := sqlite3.Open("file:" + backup); err == nil {
		readable = scalar(bdb, `SELECT count(*) FROM unit_embeddings`) != ""
		bdb.Close()
	}

	check(4, "VACUUM INTO and integrity_check with WAL open",
		integrity == "ok" && errVacuum == nil && readable,
		"integrity_check=%s; backup %d KB, reopened and readable=%v", integrity, size/1024, readable)
}

// ------------------------------------------------------------- criteria 6 & 7

func criterion67(db *sqlite3.Conn, idx index) {
	rng := rand.New(rand.NewSource(1234))

	lat := make([]time.Duration, 0, queries)
	for range queries {
		q := idx.vecs[rng.Intn(len(idx.vecs))]
		term := vocab[rng.Intn(len(vocab))]
		start := time.Now()
		_ = rrf(idx.knn(q, topK), fts(db, term, topK))
		lat = append(lat, time.Since(start))
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p50, p95, p99 := lat[len(lat)*50/100], lat[len(lat)*95/100], lat[len(lat)*99/100]

	check(6, "hybrid recall p95 < 100ms over 10k units", p95 < 100*time.Millisecond,
		"p50=%v p95=%v p99=%v over %d queries", round(p50), round(p95), round(p99), queries)

	insUnit, _, err := db.Prepare(`INSERT INTO units(id,type,content,last_touched_at,created_at) VALUES (?,?,?,?,?)`)
	must(err)
	defer insUnit.Close()
	insVec, _, err := db.Prepare(`INSERT INTO unit_embeddings(unit_id,model,dim,embedding) VALUES (?,?,?,?)`)
	must(err)
	defer insVec.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	start := time.Now()
	for i := range writeOps {
		must(db.Exec(`BEGIN`))
		id := fmt.Sprintf("w-%05d", i)
		bindText(insUnit, id, "task", sentence(rng), now, now)
		mustStep(insUnit)
		bindText(insVec, id, "nomic-embed-text")
		must(insVec.BindInt(3, dim))
		must(insVec.BindBlob(4, serialize(randomVector(rng))))
		mustStep(insVec)
		must(db.Exec(`COMMIT`))
	}
	rate := float64(writeOps) / time.Since(start).Seconds()

	check(7, "write throughput >= 50 units/s", rate >= 50,
		"%.0f units/s (%d captures, one transaction each, unit+vector+FTS)", rate, writeOps)
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
	fmt.Printf("\nADR-0001 spike (run 2) — ncruces current, brute-force vectors\n")
	fmt.Println(strings.Repeat("=", 78))
	failed := 0
	for _, r := range results {
		mark := "PASS"
		if !r.passed {
			mark = "FAIL"
			failed++
		}
		fmt.Printf("[%s] criterion %d — %s\n         %s\n", mark, r.n, r.name, r.detail)
	}
	fmt.Println(strings.Repeat("=", 78))
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

func bindText(stmt *sqlite3.Stmt, vals ...string) {
	must(stmt.Reset())
	for i, v := range vals {
		must(stmt.BindText(i+1, v))
	}
}

func mustStep(stmt *sqlite3.Stmt) {
	stmt.Step()
	must(stmt.Err())
}

func human(b int) string {
	if b > 1<<20 {
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	}
	return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
}

func round(d time.Duration) time.Duration { return d.Round(10 * time.Microsecond) }

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "spike failed:", err)
		os.Exit(1)
	}
}
