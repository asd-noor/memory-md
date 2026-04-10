// Package rrf implements Reciprocal Rank Fusion (RRF) for hybrid search.
//
// RRF merges two ranked lists (FTS5 and vector) into a single ranked list.
// Score for a document d: sum over each list of 1 / (K + rank(d)).
// K=60 is the standard constant from the original RRF paper.
package rrf

import "sort"

const K = 60

// Result is a single fused search result.
type Result struct {
	Rowid int64
	Score float64
}

// Merge takes two slices of rowids (each already ranked best-first) and
// returns a fused slice sorted by descending RRF score.
func Merge(ftsRowids, vecRowids []int64) []Result {
	scores := make(map[int64]float64)

	for rank, rowid := range ftsRowids {
		scores[rowid] += 1.0 / float64(K+rank+1)
	}
	for rank, rowid := range vecRowids {
		scores[rowid] += 1.0 / float64(K+rank+1)
	}

	results := make([]Result, 0, len(scores))
	for rowid, score := range scores {
		results = append(results, Result{Rowid: rowid, Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Rowid < results[j].Rowid
	})

	return results
}
