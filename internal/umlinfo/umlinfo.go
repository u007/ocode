// Package umlinfo builds the symbol index behind the web UML tab: an
// AST-derived map of a project's packages, symbols (types, interfaces,
// funcs, methods) and package-level import edges.
//
// v1 ships the Go analyzer only; the package is deliberately
// language-agnostic at the edges so TS/JS/Python can be added behind the
// same Index shape later (see mockups/uml-viewer-spec.md in the
// uml-viewer worktree).
package umlinfo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"

	"github.com/u007/ocode/internal/crashguard"
)

// shortcut: 20k-file / 1 MiB / 50k-symbol caps keep the index bounded on huge monorepos, and the cache holds only 4 projects. The UI surfaces `truncated` when these hit; upgrade to sharded parsing + a merge pass (or a persisted SQLite index) if a repo needs more.
const (
	SchemaVersion       = 1
	MaxGoFiles          = 20000
	MaxFileBytes        = 1 << 20 // 1 MiB
	MaxSymbols          = 50000
	MaxMembersPerSymbol = 40
	MaxDocLen           = 240
	cacheEntries        = 4
)

// Member is one field or method of a type.
type Member struct {
	Kind string `json:"kind"` // field | method
	Name string `json:"name"`
	Sig  string `json:"sig"`
}

// Symbol is one navigable declaration. File is relative to the index root;
// Line is 1-based.
type Symbol struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"` // struct | interface | type | func | method
	Lang     string   `json:"lang"`
	Pkg      string   `json:"pkg"` // package id (rel dir)
	File     string   `json:"file"`
	Line     int      `json:"line"`
	EndLine  int      `json:"end_line"`
	Doc      string   `json:"doc,omitempty"`
	Receiver string   `json:"receiver,omitempty"`
	Sig      string   `json:"sig,omitempty"`
	Members  []Member `json:"members,omitempty"`
	IsTest   bool     `json:"is_test,omitempty"`
}

// Package is one clustering unit. ID is the directory relative to root.
type Package struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Lang        string `json:"lang"`
	FileCount   int    `json:"file_count"`
	SymbolCount int    `json:"symbol_count"`
}

// Edge is a package-level dependency.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // import
}

// Index is the JSON payload served by GET /api/uml.
type Index struct {
	Version     int       `json:"version"`
	Root        string    `json:"root"`
	GeneratedAt time.Time `json:"generated_at"`
	Monorepo    string    `json:"monorepo"` // go.work | pnpm-workspace.yaml | package.json | pyproject.toml | none
	Truncated   bool      `json:"truncated,omitempty"`
	Packages    []Package `json:"packages"`
	Symbols     []Symbol  `json:"symbols"`
	Edges       []Edge    `json:"edges"`
}

// BuildIndex parses all supported sources under root.
func BuildIndex(ctx context.Context, root string) (*Index, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	idx := &Index{
		Version:     SchemaVersion,
		Root:        abs,
		GeneratedAt: time.Now().UTC(),
		Monorepo:    detectMonorepo(abs),
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, truncated, err := collectFiles(ctx, abs, ".go")
	if err != nil {
		return nil, err
	}
	idx.Truncated = truncated

	mod := modulePath(abs)
	pkgs := map[string]*Package{}
	edgeSet := map[[2]string]struct{}{}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filepath.Join(abs, f))
		if err != nil {
			continue
		}
		syms, imports, err := analyzeGoFile(abs, f, data)
		if err != nil {
			// A file that doesn't parse is skipped, not fatal: the
			// index is a best-effort map, never a build gate.
			continue
		}
		for i := range syms {
			syms[i].Pkg = pkgID(f)
		}
		if len(idx.Symbols)+len(syms) > MaxSymbols {
			idx.Truncated = true
			break
		}
		idx.Symbols = append(idx.Symbols, syms...)
		p := pkgs[pkgID(f)]
		if p == nil {
			p = &Package{ID: pkgID(f), Label: pkgID(f), Lang: "Go"}
			pkgs[p.ID] = p
		}
		p.FileCount++
		p.SymbolCount += len(syms)
		for _, imp := range imports {
			if rel, ok := importToRel(mod, imp); ok && rel != pkgID(f) {
				edgeSet[[2]string{pkgID(f), rel}] = struct{}{}
			}
		}
	}

	// Deterministic output ordering.
	for _, p := range pkgs {
		idx.Packages = append(idx.Packages, *p)
	}
	sort.Slice(idx.Packages, func(i, j int) bool { return idx.Packages[i].ID < idx.Packages[j].ID })
	sort.Slice(idx.Symbols, func(i, j int) bool {
		if idx.Symbols[i].Pkg == idx.Symbols[j].Pkg {
			if idx.Symbols[i].File == idx.Symbols[j].File {
				return idx.Symbols[i].Line < idx.Symbols[j].Line
			}
			return idx.Symbols[i].File < idx.Symbols[j].File
		}
		return idx.Symbols[i].Pkg < idx.Symbols[j].Pkg
	})
	for e := range edgeSet {
		idx.Edges = append(idx.Edges, Edge{From: e[0], To: e[1], Kind: "import"})
	}
	sort.Slice(idx.Edges, func(i, j int) bool {
		if idx.Edges[i].From == idx.Edges[j].From {
			return idx.Edges[i].To < idx.Edges[j].To
		}
		return idx.Edges[i].From < idx.Edges[j].From
	})
	return idx, nil
}

func pkgID(relFile string) string {
	d := filepath.ToSlash(filepath.Dir(relFile))
	if d == "." {
		return "."
	}
	return d
}

// fingerprint walks root (cheap, stat-only) and hashes the sorted
// (path, size, mtime) list. Two trees with the same hash produce the same
// index, so the cache can be keyed on content without parsing twice.
func fingerprint(ctx context.Context, root string) (string, error) {
	var b strings.Builder
	truncated := false
	n := 0
	err := walkRoot(ctx, root, func(rel string, d fs.DirEntry, info fs.FileInfo) error {
		if strings.HasSuffix(rel, ".go") {
			if n >= MaxGoFiles {
				truncated = true
				return errLimit
			}
			n++
			fmt.Fprintf(&b, "%s|%d|%d\n", rel, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	if err != nil && err != errLimit && err != context.Canceled && err != context.DeadlineExceeded {
		return "", err
	}
	h := sha256.Sum256([]byte(b.String()))
	if truncated {
		// Cannot distinguish "same truncated first N files" cheaply here;
		// callers treat a truncated fingerprint as cache-busting by adding
		// the marker, which forces a rebuild rather than a wrong hit.
		sum := hex.EncodeToString(h[:])
		return sum + ":truncated", nil
	}
	return hex.EncodeToString(h[:]), nil
}

var errLimit = fmt.Errorf("umlinfo: file limit reached")

// collectFiles returns Go source files relative to root, honoring
// .gitignore plus a hard cap.
func collectFiles(ctx context.Context, root, ext string) ([]string, bool, error) {
	var files []string
	truncated := false
	ignore := newRootIgnore(root)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip, don't fail the index
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && skipDir(rel, d.Name(), ignore) {
				return filepath.SkipDir
			}
			return ctx.Err()
		}
		if !strings.HasSuffix(rel, ext) {
			return nil
		}
		// Symlinks are listed but never followed: WalkDir reports the link
		// itself, and os.ReadFile on it would parse a file outside the root.
		if !d.Type().IsRegular() {
			return nil
		}
		if info, ierr := d.Info(); ierr != nil || info.Size() > MaxFileBytes {
			return nil
		}
		if len(files) >= MaxGoFiles {
			truncated = true
			return errLimit
		}
		files = append(files, rel)
		return nil
	})
	if err == errLimit {
		return files, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	sort.Strings(files)
	return files, truncated, nil
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".worktrees": true, "coverage": true, "__pycache__": true,
	".next": true, ".cache": true, "out": true, "target": true,
}

func skipDir(rel, name string, ignore *rootIgnore) bool {
	if skipDirs[name] {
		return true
	}
	return ignore != nil && ignore.ignored(rel+"/", true)
}

// Monorepo detection is marker-presence only, and only at the root — it does
// not glob workspace member dirs. v1 reports the marker for the UI badge;
// per-package clustering comes from the directory structure itself.
// shortcut: root-level markers only, upgrade to member-dir expansion when
// someone needs monorepos whose roots aren't their module roots.
func detectMonorepo(root string) string {
	for _, marker := range []string{"go.work", "pnpm-workspace.yaml", "package.json", "pyproject.toml"} {
		if _, err := os.Stat(filepath.Join(root, marker)); err == nil {
			switch marker {
			case "package.json":
				data, err := os.ReadFile(filepath.Join(root, marker))
				if err != nil || !strings.Contains(string(data), "\"workspaces\"") {
					continue
				}
			}
			return marker
		}
	}
	return "none"
}

// modulePath reads the module directive from the root go.mod. Returns "" if absent.
func modulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// importToRel maps an import path onto a directory relative to the module
// root, e.g. github.com/u007/ocode/internal/agent with module
// github.com/u007/ocode -> internal/agent. Returns false for third-party
// imports.
func importToRel(modPath, importPath string) (string, bool) {
	if modPath == "" {
		return "", false
	}
	if importPath == modPath {
		return ".", true
	}
	if strings.HasPrefix(importPath, modPath+"/") {
		return strings.TrimPrefix(importPath, modPath+"/"), true
	}
	return "", false
}

// walkRoot is shared by collectFiles and fingerprint; fn receives rel paths.
func walkRoot(ctx context.Context, root string, fn func(rel string, d fs.DirEntry, info fs.FileInfo) error) error {
	ignore := newRootIgnore(root)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && skipDir(rel, d.Name(), ignore) {
				return filepath.SkipDir
			}
			return ctx.Err()
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		return fn(rel, d, info)
	})
}

// rootIgnore loads .gitignore + .ignore at root with go-git's matcher.
// Minimal: root-level files only, same scope as the file-search matcher.
type rootIgnore struct {
	matcher gitignore.Matcher
}

func newRootIgnore(root string) *rootIgnore {
	var patterns []gitignore.Pattern
	for _, name := range []string{".gitignore", ".ignore"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, gitignore.ParsePattern(line, nil))
			}
		}
	}
	if len(patterns) == 0 {
		return nil
	}
	return &rootIgnore{matcher: gitignore.NewMatcher(patterns)}
}

func (r *rootIgnore) ignored(relPath string, isDir bool) bool {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return r.matcher.Match(parts, isDir)
}

// ---------- cached indexer ----------

type cacheEntry struct {
	fp     string
	index  *Index
	fail   error
	failAt time.Time
}

// Indexer caches built indexes keyed by fingerprint and serializes builds
// per root so concurrent polls don't start parallel parses.
type Indexer struct {
	mu       sync.Mutex
	entries  map[string]*cacheEntry // key: abs root
	building map[string]struct{}    // roots with a StartBuild in flight
}

func NewIndexer() *Indexer {
	return &Indexer{entries: map[string]*cacheEntry{}, building: map[string]struct{}{}}
}

// Get returns (index, fresh=true) when a cache entry matches fp, else
// (nil, false). Freshness is the caller's fingerprint comparison.
func (i *Indexer) Get(root, fp string) (*Index, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	e, ok := i.entries[root]
	if !ok || e.fp != fp || e.index == nil {
		return nil, false
	}
	return e.index, true
}

// LastError returns a recent build failure for root, if any.
func (i *Indexer) LastError(root string) (error, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	e, ok := i.entries[root]
	if !ok || e.fail == nil || time.Since(e.failAt) > 30*time.Second {
		return nil, false
	}
	return e.fail, true
}

// Build builds (or returns) the index for root. The fingerprint is
// computed synchronously by the caller and passed in so GET handlers can
// decide 200-vs-202 without doing the build inline.
func (i *Indexer) Build(ctx context.Context, root, fp string) (*Index, error) {
	i.mu.Lock()
	if e, ok := i.entries[root]; ok && e.fp == fp && e.index != nil {
		i.mu.Unlock()
		return e.index, nil
	}
	i.evictIfNeededLocked()
	i.mu.Unlock()

	idx, err := BuildIndex(ctx, root)

	i.mu.Lock()
	defer i.mu.Unlock()
	if err != nil {
		i.markFailedLocked(root, err)
		return nil, err
	}
	i.entries[root] = &cacheEntry{fp: fp, index: idx}
	return idx, nil
}

// markFailedLocked records a build failure without discarding a good cached
// index: the fingerprint is left stale so Get misses and polls retry.
func (i *Indexer) markFailedLocked(root string, err error) {
	prev := i.entries[root]
	if prev == nil {
		prev = &cacheEntry{}
	}
	prev.fail, prev.failAt = err, time.Now()
	i.entries[root] = prev
}

// Fingerprint exposes the fingerprint computation for the HTTP handler.
func Fingerprint(ctx context.Context, root string) (string, error) {
	return fingerprint(ctx, root)
}

// Building reports whether a StartBuild is in flight for root.
func (i *Indexer) Building(root string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	_, ok := i.building[root]
	return ok
}

// StartBuild kicks off a background build for root. No-op when one is
// already running. The fingerprint is the caller's, so a poll that
// discovers fresh-enough cache state never launches work. The goroutine
// owns a 30-second deadline; the HTTP request that triggered it returns
// immediately.
func (i *Indexer) StartBuild(root, fp string) {
	i.mu.Lock()
	if _, ok := i.building[root]; ok {
		i.mu.Unlock()
		return
	}
	i.building[root] = struct{}{}
	i.mu.Unlock()

	crashguard.Go(func() {
		defer func() {
			i.mu.Lock()
			delete(i.building, root)
			i.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		idx, err := BuildIndex(ctx, root)
		i.mu.Lock()
		defer i.mu.Unlock()
		i.evictIfNeededLocked()
		if err != nil {
			i.markFailedLocked(root, err)
			return
		}
		i.entries[root] = &cacheEntry{fp: fp, index: idx}
	})
}

func (i *Indexer) evictIfNeededLocked() {
	for len(i.entries) >= cacheEntries {
		for k := range i.entries {
			delete(i.entries, k)
			break
		}
	}
}

// MarshalJSON keeps Index marshalling obvious to callers and tests.
func (idx *Index) MarshalJSON() ([]byte, error) {
	type alias Index
	return json.Marshal((*alias)(idx))
}
