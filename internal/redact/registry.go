package redact

import (
	"sort"
	"strings"
	"sync"
)

// Entry represents a registered secret value with metadata.
type Entry struct {
	Value       string
	Kind        string
	Source      string
	FirstSeenAt int64 // Unix timestamp
}

// Registry manages the mapping between secret values and their token indexes.
type Registry struct {
	mu       sync.RWMutex
	nonce    string
	valToIdx map[string]int
	entries  map[int]Entry
	nextIdx  int
}

// NewRegistry creates a new registry with the given nonce.
func NewRegistry(nonce string) *Registry {
	return &Registry{
		nonce:    nonce,
		valToIdx: make(map[string]int),
		entries:  make(map[int]Entry),
		nextIdx:  1,
	}
}

// Nonce returns the session nonce.
func (r *Registry) Nonce() string {
	return r.nonce
}

// normalizeValue trims whitespace and symmetric quotes from a value.
func normalizeValue(v string) string {
	v = strings.TrimSpace(v)
	// Remove symmetric quotes
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') ||
			(v[0] == '\'' && v[len(v)-1] == '\'') ||
			(v[0] == '`' && v[len(v)-1] == '`') {
			v = v[1 : len(v)-1]
		}
	}
	return strings.TrimSpace(v)
}

// GetOrAssign returns the index for a value, registering it if new.
// Same value always gets the same index (accumulative reuse).
func (r *Registry) GetOrAssign(value, kind, source string) int {
	norm := normalizeValue(value)
	if norm == "" {
		return 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if idx, ok := r.valToIdx[norm]; ok {
		return idx
	}

	idx := r.nextIdx
	r.nextIdx++
	r.valToIdx[norm] = idx
	r.entries[idx] = Entry{
		Value:       norm,
		Kind:        kind,
		Source:      source,
		FirstSeenAt: 0, // Caller should set if needed
	}
	return idx
}

// Lookup returns the entry for a given index.
func (r *Registry) Lookup(idx int) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[idx]
	return e, ok
}

// All returns all entries sorted by index ascending.
func (r *Registry) All() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool {
		// We need to find the index for each entry to sort properly
		// Since we can't easily get index from entry, we sort by value as a proxy
		// Actually, let's iterate in index order
		return false // placeholder
	})

	// Better: iterate by index
	indices := make([]int, 0, len(r.entries))
	for idx := range r.entries {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	result = make([]Entry, 0, len(indices))
	for _, idx := range indices {
		result = append(result, r.entries[idx])
	}
	return result
}

// Substitute replaces all registered values in text with their tokens.
// Replaces longest values first to avoid partial matches.
func (r *Registry) Substitute(text string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.entries) == 0 {
		return text
	}

	// Collect values and sort by length descending
	type valIdx struct {
		val string
		idx int
	}
	var vals []valIdx
	for idx, e := range r.entries {
		vals = append(vals, valIdx{val: e.Value, idx: idx})
	}
	sort.Slice(vals, func(i, j int) bool {
		return len(vals[i].val) > len(vals[j].val)
	})

	result := text
	for _, vi := range vals {
		token := FormatToken(r.nonce, vi.idx)
		result = strings.ReplaceAll(result, vi.val, token)
	}
	return result
}

// Resolve replaces tokens in text with their registered values.
// Ignores tokens with foreign nonces.
func (r *Registry) Resolve(text string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.entries) == 0 {
		return text
	}

	result := text
	for idx, e := range r.entries {
		token := FormatToken(r.nonce, idx)
		result = strings.ReplaceAll(result, token, e.Value)
	}
	return result
}