package tui

import (
	"sort"
	"strings"
)

// fuzzyScore ranks how well `item` matches `query`. Higher is better.
// Returns 0 if there is no meaningful match.
//
// Tiers (largest weight first):
//   - exact match on full item                          : 1_000_000
//   - item starts with the full query                   : 500_000
//   - item contains the full query as substring         : 250_000 + position bonus
//   - all whitespace-separated tokens appear in item    : 100_000 + Σ token bonuses
//   - all query characters appear as a subsequence      : 10_000 + density bonus
func fuzzyScore(item, query string) int {
	if query == "" {
		return 1
	}
	li := strings.ToLower(item)
	lq := strings.ToLower(query)

	if li == lq {
		return 1_000_000
	}
	if strings.HasPrefix(li, lq) {
		return 500_000 + max(0, 200-len(li))
	}
	if idx := strings.Index(li, lq); idx >= 0 {
		return 250_000 + max(0, 200-idx) + max(0, 100-len(li))
	}

	tokens := strings.Fields(lq)
	if len(tokens) > 1 {
		score := 100_000
		ok := true
		for _, t := range tokens {
			idx := strings.Index(li, t)
			if idx < 0 {
				ok = false
				break
			}
			score += max(0, 200-idx) + len(t)*2
		}
		if ok {
			return score + max(0, 100-len(li))
		}
	}

	// Subsequence fallback: every char of query must appear in order.
	score, ok := subsequenceScore(li, lq)
	if !ok {
		return 0
	}
	return 10_000 + score + max(0, 100-len(li))
}

func subsequenceScore(item, query string) (int, bool) {
	if len(query) == 0 {
		return 0, true
	}
	score := 0
	prev := -1
	qi := 0
	qb := []byte(query)
	ib := []byte(item)
	for i := 0; i < len(ib) && qi < len(qb); i++ {
		if ib[i] == qb[qi] {
			if prev >= 0 {
				gap := i - prev - 1
				score += max(0, 20-gap)
			} else {
				score += max(0, 30-i) // earlier first match = better
			}
			prev = i
			qi++
		}
	}
	if qi < len(qb) {
		return 0, false
	}
	return score, true
}

// fuzzyFilter returns items that match `query`, sorted by descending
// score. An empty query returns the original list unchanged.
func fuzzyFilter(items []string, query string) []string {
	if strings.TrimSpace(query) == "" {
		return items
	}
	type scored struct {
		s    int
		i    int
		item string
	}
	out := make([]scored, 0, len(items))
	for i, it := range items {
		if s := fuzzyScore(it, query); s > 0 {
			out = append(out, scored{s: s, i: i, item: it})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].s != out[b].s {
			return out[a].s > out[b].s
		}
		return out[a].i < out[b].i
	})
	result := make([]string, len(out))
	for i, s := range out {
		result[i] = s.item
	}
	return result
}

// fuzzyFilterFunc is the generic form for arbitrary item types. The
// caller supplies the searchable text for each item.
func fuzzyFilterFunc[T any](items []T, query string, key func(T) string) []T {
	if strings.TrimSpace(query) == "" {
		return items
	}
	type scored struct {
		s    int
		i    int
		item T
	}
	out := make([]scored, 0, len(items))
	for i, it := range items {
		if s := fuzzyScore(key(it), query); s > 0 {
			out = append(out, scored{s: s, i: i, item: it})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].s != out[b].s {
			return out[a].s > out[b].s
		}
		return out[a].i < out[b].i
	})
	result := make([]T, len(out))
	for i, s := range out {
		result[i] = s.item
	}
	return result
}
