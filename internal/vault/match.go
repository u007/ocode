package vault

import (
	"cmp"
	"log"
	"net/url"
	"slices"
	"strings"
)

// MatchForURL returns metadata for every item whose stored URL plausibly
// belongs to rawURL. Matching is deliberately simple (documented limitation:
// no public-suffix list): a host matches when it is exactly equal
// (case-insensitive) or is a parent domain of the requested host, and an item's
// path adds weight when it is a non-root prefix of the requested path.
//
// Ranking: score desc, then site asc, then id asc. A locked vault is
// ErrLocked; a malformed or hostless rawURL yields an empty slice with no
// error.
func (v *Vault) MatchForURL(rawURL string) ([]ItemMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dk == nil {
		return nil, ErrLocked
	}
	want, err := url.Parse(rawURL)
	if err != nil {
		log.Printf("vault: match url: parse: %v", err)
		return []ItemMeta{}, nil
	}
	if want.Host == "" {
		log.Printf("vault: match url: empty host")
		return []ItemMeta{}, nil
	}

	type scored struct {
		meta  ItemMeta
		score int
	}
	matches := make([]scored, 0, len(v.items))
	for _, it := range v.items {
		score, ok := matchScore(want, it)
		if !ok {
			continue
		}
		matches = append(matches, scored{
			meta: ItemMeta{
				ID:       it.ID,
				Site:     it.Site,
				URL:      it.URL,
				Title:    it.Title,
				Username: it.Username,
			},
			score: score,
		})
	}
	slices.SortStableFunc(matches, func(a, b scored) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		if c := strings.Compare(strings.ToLower(a.meta.Site), strings.ToLower(b.meta.Site)); c != 0 {
			return c
		}
		return strings.Compare(a.meta.ID, b.meta.ID)
	})

	out := make([]ItemMeta, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.meta)
	}
	return out, nil
}

// matchScore reports whether item's URL belongs to want, and if so its ranking
// score: +2 for a non-root path prefix, +1 for an exact host.
func matchScore(want *url.URL, it Item) (int, bool) {
	itemURL, err := url.Parse(it.URL)
	if err != nil || itemURL.Host == "" {
		return 0, false
	}
	wantHost := strings.ToLower(want.Host)
	itemHost := strings.ToLower(itemURL.Host)

	score := 0
	switch {
	case wantHost == itemHost:
		score++ // exact host
	case strings.HasSuffix(wantHost, "."+itemHost):
		// Parent-domain match (no exact-host bonus).
	default:
		return 0, false
	}

	if p := itemURL.Path; p != "" && p != "/" && strings.HasPrefix(want.Path, p) {
		score += 2
	}
	return score, true
}
