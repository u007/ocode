package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"regexp"

	"github.com/mattn/go-runewidth"
)

// pathLinkRegion marks a clickable file-path span on one visual (wrapped) line.
// Columns are visual columns (matching mouse.X / selection coordinates), not
// byte offsets.
type pathLinkRegion struct {
	line     int    // index into the surface's visual lines
	startCol int    // visual column, inclusive
	endCol   int    // visual column, exclusive
	path     string // resolved absolute file path
	lineNo   int    // 1-based line number from a :NN suffix, 0 if none
}

// pathCandidateRe matches path-ish tokens. Go's RE2 has no lookaround, so this
// is deliberately permissive — candidates are filtered by looksLikePathToken
// and verified against the filesystem (only existing files become links). A
// trailing :line[-endline] or :line:col suffix is captured as part of the token.
var pathCandidateRe = regexp.MustCompile(`[A-Za-z0-9._/+@~-]+(?::[0-9]+(?:[-:][0-9]+)?)?`)

// looksLikePathToken cheaply rejects ordinary words before paying for a stat.
func looksLikePathToken(tok string) bool {
	if strings.ContainsRune(tok, '/') {
		return true
	}
	// bare filename: needs a dot-extension somewhere past the first char
	if i := strings.LastIndexByte(tok, '.'); i > 0 && i < len(tok)-1 {
		return true
	}
	return false
}

// splitPathLine separates a trailing :line[-endline] or :line:col suffix from
// the path. For ranges like :1213-1218 only the start line is returned.
func splitPathLine(tok string) (string, int) {
	if i := strings.IndexByte(tok, ':'); i >= 0 {
		rest := tok[i+1:]
		numPart := rest
		// stop at ':' (col) or '-' (end of line range)
		if j := strings.IndexAny(rest, ":-"); j >= 0 {
			numPart = rest[:j]
		}
		if n, err := strconv.Atoi(numPart); err == nil {
			return tok[:i], n
		}
	}
	return tok, 0
}

// resolveExistingFile resolves a (possibly relative) path against workDir and
// confirms it points to an existing regular file. A leading "~" or "~/" is
// expanded to the user's home directory first, so shell-style paths such as
// ~/.config/opencode/auth.json are recognized and become clickable links.
// (Go's filepath treats "~" as non-absolute, so without this expansion the
// path would be joined onto workDir and the stat would fail.)
func resolveExistingFile(path, workDir string) (string, bool) {
	expanded := path
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				expanded = home
			} else {
				expanded = filepath.Join(home, path[2:])
			}
		}
	}
	abs := expanded
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workDir, abs)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return "", false
	}
	return abs, true
}

// byteIdxToVisualCol returns the visual column at byteIdx in a plain-text line,
// accounting for wide runes and tabs (inverse of visualColToRuneIdx).
func byteIdxToVisualCol(line string, byteIdx int) int {
	col := 0
	for i := 0; i < byteIdx && i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		w := runewidth.RuneWidth(r)
		if r == '\t' {
			w = tabWidth - (col % tabWidth)
		}
		col += w
		i += size
	}
	return col
}

// pathLinkAtCol returns the file-path link under visualCol on an ANSI-stripped
// line, if the token there resolves to an existing file. Detection is lazy
// (only the token under the cursor is statted) to avoid scanning/stat-ing the
// whole transcript on every render or hover event. On a miss over a candidate
// token, the returned region still carries the token's startCol/endCol so
// callers can cache the negative result for that span.
func pathLinkAtCol(rawLine string, visualCol int, workDir string) (pathLinkRegion, bool) {
	for _, loc := range pathCandidateRe.FindAllStringIndex(rawLine, -1) {
		tok := rawLine[loc[0]:loc[1]]
		// Drop trailing sentence punctuation the permissive regex may have grabbed.
		trimmed := strings.TrimRight(tok, ".,;:)]}\"'")
		if trimmed == "" {
			continue
		}
		startCol := byteIdxToVisualCol(rawLine, loc[0])
		endCol := byteIdxToVisualCol(rawLine, loc[0]+len(trimmed))
		if visualCol < startCol || visualCol >= endCol {
			continue
		}
		pathPart, lineNo := splitPathLine(trimmed)
		if !looksLikePathToken(pathPart) {
			return pathLinkRegion{startCol: startCol, endCol: endCol}, false
		}
		abs, ok := resolveExistingFile(pathPart, workDir)
		if !ok {
			return pathLinkRegion{startCol: startCol, endCol: endCol}, false
		}
		return pathLinkRegion{startCol: startCol, endCol: endCol, path: abs, lineNo: lineNo}, true
	}
	return pathLinkRegion{}, false
}

// pathLinkProbeCache memoizes the last pathLinkAtCol probe per surface.
// MouseModeAllMotion fires on every cursor move (20-60Hz), and each probe over
// a path-like token costs a regex scan plus an os.Stat; cursor motion within
// the same token must not repeat that. Keyed by line content, so the cache
// self-invalidates when the transcript re-renders or the cursor changes line.
type pathLinkProbeCache struct {
	rawLine    string
	startCol   int // probed token span, [startCol, endCol); empty span = no cache
	endCol     int
	probeCol   int // exact column probed; used for miss caching
	r          pathLinkRegion
	ok         bool
	cachedMiss bool // true = the probeCol was a miss and is cached
}

// probe returns the cached result when visualCol is still inside the last
// probed token on the same line, otherwise runs pathLinkAtCol and caches it.
// Both hits and misses are cached to avoid redundant regex+stat work on
// repeated mouse motion over non-path text.
func (c *pathLinkProbeCache) probe(rawLine string, visualCol int, workDir string) (pathLinkRegion, bool) {
	// Cache hit: cursor still inside previously probed token span.
	if c.endCol > c.startCol && visualCol >= c.startCol && visualCol < c.endCol && rawLine == c.rawLine {
		return c.r, c.ok
	}
	// Cache miss: same column on same line — return cached miss.
	if c.cachedMiss && c.probeCol == visualCol && rawLine == c.rawLine {
		return c.r, false
	}
	r, ok := pathLinkAtCol(rawLine, visualCol, workDir)
	if ok {
		// Hit: cache the token span.
		c.rawLine, c.startCol, c.endCol, c.r, c.ok = rawLine, r.startCol, r.endCol, r, ok
		c.cachedMiss = false
	} else if r.endCol > r.startCol {
		// Token found but file doesn't exist: cache the span so motion
		// within the token doesn't re-stat the nonexistent file.
		c.rawLine, c.startCol, c.endCol, c.r, c.ok = rawLine, r.startCol, r.endCol, r, false
		c.cachedMiss = false
	} else {
		// No token found at all: cache the exact column to avoid
		// re-scanning on repeated mouse motion over non-path text.
		c.rawLine, c.probeCol = rawLine, visualCol
		c.cachedMiss = true
		c.r, c.ok = r, false
		// Clear token span so the hit guard doesn't fire.
		c.startCol, c.endCol = 0, 0
	}
	return r, ok
}

// applyPathLinkUnderline returns a copy of lines with the region's span
// underlined. rawLines provides plain text for visual-column → byte mapping.
func applyPathLinkUnderline(lines, rawLines []string, r pathLinkRegion) []string {
	if r.line < 0 || r.line >= len(lines) {
		return lines
	}
	out := make([]string, len(lines))
	copy(out, lines)
	raw := ""
	if r.line < len(rawLines) {
		raw = rawLines[r.line]
	}
	cs := visualColToRuneIdx(raw, r.startCol)
	ce := visualColToRuneIdx(raw, r.endCol)
	out[r.line] = insertSGRSpan(out[r.line], raw, cs, ce, "\x1b[4m", "\x1b[24m")
	return out
}
