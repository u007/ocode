package tui

import "strings"

// pathSegmentCount reports how many path segments a relative path contains.
// Separators are normalised so both "/" and "\\" count, and a leading "./"
// plus duplicate/trailing separators are ignored.
//
//	"main.go"                -> 1
//	"internal/tui/model.go"  -> 3
//	"./web/src/App.tsx"      -> 3
func pathSegmentCount(p string) int {
	norm := strings.ReplaceAll(p, "\\", "/")
	norm = strings.TrimPrefix(norm, "./")
	norm = strings.Trim(norm, "/")
	if norm == "" {
		return 0
	}
	count := 0
	for _, seg := range strings.Split(norm, "/") {
		if seg != "" {
			count++
		}
	}
	return count
}

// lessPathShortest is the shared file-search ordering rule: fewest path
// segments first, then lexicographically. It is used to break ties between
// equally relevant search results (so a shallow file surfaces above a deeply
// nested one) and to order an unfiltered file list.
func lessPathShortest(a, b string) bool {
	sa, sb := pathSegmentCount(a), pathSegmentCount(b)
	if sa != sb {
		return sa < sb
	}
	return a < b
}
