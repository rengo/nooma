// Side question raised by the ADR-0001 spike: is sqlite-vec needed at all?
//
// The whole staleness risk of the ncruces path comes from one dependency. Nooma is
// a personal brain — one human, one vault. If a plain linear scan in Go is fast
// enough at realistic vault sizes, the dependency disappears and with it the pin.
//
// THROWAWAY CODE. Never merged.
package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"
)

const (
	dim     = 768
	topK    = 20
	queries = 200
)

func main() {
	fmt.Printf("Brute-force cosine over normalized vectors, dim=%d, top-%d\n", dim, topK)
	fmt.Printf("%-10s %-12s %-10s %-10s %-10s\n", "units", "memory", "p50", "p95", "p99")
	fmt.Println("---------------------------------------------------------------")

	for _, n := range []int{1_000, 10_000, 50_000, 100_000, 500_000} {
		corpus := build(n)
		lat := measure(corpus)
		fmt.Printf("%-10d %-12s %-10v %-10v %-10v\n",
			n, human(n*dim*4), round(lat[50]), round(lat[95]), round(lat[99]))
	}
}

func build(n int) [][]float32 {
	rng := rand.New(rand.NewSource(42))
	out := make([][]float32, n)
	for i := range out {
		v := make([]float32, dim)
		var norm float64
		for j := range v {
			v[j] = float32(rng.NormFloat64())
			norm += float64(v[j]) * float64(v[j])
		}
		norm = math.Sqrt(norm)
		for j := range v {
			v[j] /= float32(norm)
		}
		out[i] = v
	}
	return out
}

// search is the whole "index": vectors are unit-normalized, so cosine similarity
// is a dot product, and top-K is a partial sort.
func search(corpus [][]float32, q []float32, k int) []int {
	type hit struct {
		idx int
		sim float32
	}
	hits := make([]hit, len(corpus))
	for i, v := range corpus {
		var s float32
		for j := range q {
			s += q[j] * v[j]
		}
		hits[i] = hit{i, s}
	}
	sort.Slice(hits, func(a, b int) bool { return hits[a].sim > hits[b].sim })

	out := make([]int, k)
	for i := range out {
		out[i] = hits[i].idx
	}
	return out
}

func measure(corpus [][]float32) map[int]time.Duration {
	rng := rand.New(rand.NewSource(7))
	lat := make([]time.Duration, 0, queries)
	for range queries {
		q := corpus[rng.Intn(len(corpus))]
		start := time.Now()
		_ = search(corpus, q, topK)
		lat = append(lat, time.Since(start))
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	return map[int]time.Duration{
		50: lat[len(lat)*50/100],
		95: lat[len(lat)*95/100],
		99: lat[len(lat)*99/100],
	}
}

func human(bytes int) string {
	switch {
	case bytes > 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30))
	case bytes > 1<<20:
		return fmt.Sprintf("%.0f MB", float64(bytes)/(1<<20))
	default:
		return fmt.Sprintf("%.0f KB", float64(bytes)/(1<<10))
	}
}

func round(d time.Duration) time.Duration { return d.Round(10 * time.Microsecond) }
