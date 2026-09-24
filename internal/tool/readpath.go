package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// defaultReadHintNames bounds how many candidate filenames a not-found read
// message lists.
const defaultReadHintNames = 5

// unicodeSpaceReplacer maps the non-ASCII space code points that commonly
// appear in filenames onto a plain ASCII space.
//
// macOS uses U+202F (NARROW NO-BREAK SPACE) between the time and AM/PM in
// screenshot filenames ("Screenshot 2026-09-23 at 11.00.05\u202fPM.png").
// Models routinely re-emit that path with an ASCII space, so a path that is
// visually identical fails to stat. U+00A0 (NO-BREAK SPACE) arrives in files
// pasted from the web; U+2007/U+2009 are the other fixed/thin spaces that
// survive a copy-paste; U+FEFF (ZERO WIDTH NO-BREAK SPACE) is a stray BOM.
var unicodeSpaceReplacer = strings.NewReplacer(
	"\u00a0", " ", // no-break space
	"\u2007", " ", // figure space
	"\u2009", " ", // thin space
	"\u202f", " ", // narrow no-break space
	"\ufeff", " ", // zero-width no-break space
)

// NormalizeUnicodeSpaces replaces non-ASCII space characters with a plain
// ASCII space. It is the single definition of "space" shared by read-target
// recovery and the not-found hint, so the two can never disagree.
func NormalizeUnicodeSpaces(s string) string {
	if !strings.ContainsFunc(s, isUnicodeSpace) {
		return s
	}
	return unicodeSpaceReplacer.Replace(s)
}

func isUnicodeSpace(r rune) bool {
	switch r {
	case '\u00a0', '\u2007', '\u2009', '\u202f', '\ufeff':
		return true
	}
	return false
}

// ReadTargetResolution is the outcome of resolving a read path on disk.
type ReadTargetResolution struct {
	// Path is the path the read should use. It equals the input when the
	// literal path exists or nothing could be recovered.
	Path string
	// Exists reports whether Path exists on disk.
	Exists bool
	// Hint is a short clause explaining a miss and naming nearby candidates,
	// for a not-found message. Empty when Exists is true or there is nothing
	// useful to add.
	Hint string
}

// ResolveReadTarget resolves a read target, tolerating the Unicode space
// characters that macOS puts in screenshot filenames.
//
// When abs does not exist as given, a sibling in the same directory whose name
// is equal after NormalizeUnicodeSpaces is used instead — but only when
// exactly one such sibling exists and the case matches, so a case-only
// mismatch is never silently redirected to a different file on a
// case-sensitive filesystem. hintMax bounds the number of candidate names in
// Hint (<= 0 uses defaultReadHintNames).
//
// Recovery is read-only by design: callers only invoke this after the literal
// path has already failed to stat, so a create-a-new-file flow can never be
// redirected onto a differently-spaced existing file.
func ResolveReadTarget(abs string, hintMax int) ReadTargetResolution {
	res := ReadTargetResolution{Path: abs}
	if _, err := os.Lstat(abs); err == nil {
		res.Exists = true
		return res
	}
	dir, base := filepath.Split(abs)
	if dir == "" {
		dir = "."
	}
	variants := unicodeSpaceSiblings(dir, base)
	if len(variants) == 1 {
		res.Path = variants[0]
		res.Exists = true
		return res
	}
	res.Hint = readTargetHint(dir, base, variants, hintMax)
	return res
}

// unicodeSpaceSiblings returns existing entries in dir whose name equals base
// after Unicode-space normalization (case preserved), excluding base itself.
func unicodeSpaceSiblings(dir, base string) []string {
	norm := NormalizeUnicodeSpaces(base)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if name == base {
			continue
		}
		if NormalizeUnicodeSpaces(name) == norm {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	return out
}

// readTargetHint builds the not-found clause for a read miss. Variants (names
// differing only by a non-ASCII space) are the most likely culprit, so they
// are reported first; otherwise a few similar names are listed.
func readTargetHint(dir, base string, variants []string, max int) string {
	if max <= 0 {
		max = defaultReadHintNames
	}
	if len(variants) > 0 {
		return fmt.Sprintf("multiple files differ only by a non-ASCII space: %s", quoteNames(variants, max))
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return "the parent directory does not exist"
	}
	similar := similarNames(dir, base, max)
	if len(similar) == 0 {
		return ""
	}
	return fmt.Sprintf("similar names in %s: %s", dir, quoteNames(similar, max))
}

// similarNames returns up to max existing entries in dir whose name is close
// to base: equal ignoring case, or sharing an extension-less prefix. Sorted by
// name for deterministic output.
func similarNames(dir, base string, max int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	target := strings.ToLower(NormalizeUnicodeSpaces(base))
	stem := stripExt(target)
	var out []string
	for _, e := range entries {
		name := e.Name()
		if name == base {
			continue
		}
		cand := strings.ToLower(NormalizeUnicodeSpaces(name))
		if cand == target || closeName(stem, stripExt(cand)) {
			out = append(out, filepath.Join(dir, name))
			if len(out) >= max {
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// stripExt removes the final extension from a filename, but only when a
// non-empty stem precedes it (so a dotfile like ".env" is left intact).
func stripExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[:i]
	}
	return name
}

// closeName reports whether two extension-less names are close enough to be
// worth suggesting: one is a prefix of the other (both at least three runes),
// or they share at least four leading characters.
func closeName(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if len(a) >= 3 && len(b) >= 3 && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a)) {
		return true
	}
	return commonPrefixLen(a, b) >= 4
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// quoteNames renders up to max basenames for a message. strconv.Quote escapes
// a non-ASCII space, so an invisible character is visible in the output
// instead of looking like a plain space.
func quoteNames(names []string, max int) string {
	if max > 0 && len(names) > max {
		names = names[:max]
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, strconv.Quote(filepath.Base(n)))
	}
	return strings.Join(parts, ", ")
}
