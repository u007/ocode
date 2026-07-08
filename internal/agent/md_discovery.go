package agent

import (
	"context"
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
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/knowledge"
	"github.com/u007/ocode/internal/paths"
)

// Markdown discovery: every project *.md file (except the always-on briefing
// set, which LoadContext injects in full) becomes a discovery Doc whose Text is
// a small-model-generated summary. The index lists "path — summary"; the full
// file content is attached to the volatile tail only when the query matches.
//
// Files owned by an active OKF knowledge bundle (the docs/ directory) are
// EXCLUDED: docs/index.md — a curated TOC of every concept doc — is injected
// into the system prompt when /docs is on, and knowledge_lookup retrieves any
// concept doc on demand. Re-discovering them would produce a redundant
// "path — summary" TOC (duplicate mention of the same docs) and waste
// small-model summarization calls on files the knowledge system already owns.
// Non-bundle *.md (README, CHANGELOG, project docs) remain discoverable.
// Summaries are expensive (one small-model call each), so generation runs in a
// background goroutine and is cached on disk keyed by file content. discoveryDocs()
// — called every Step under a 500ms warm deadline — only ever reads the in-memory
// snapshot of READY summaries; a file with no cached summary yet is simply absent
// from the corpus until the background pass fills it (progressive availability,
// not a placeholder fallback).

const (
	// mdSummaryModel system prompt — one short paragraph describing the doc.
	mdSummarySystemPrompt = `Summarize this project documentation file in ONE sentence (max 25 words) describing what it documents and when a developer would need it. Output ONLY the sentence — no preamble, no quotes, no markdown.`

	mdSummaryTimeoutSeconds = 20
	mdSummaryMaxInputChars  = 6000
	mdSummaryMaxOutputChars = 240
	// mdScanThrottle bounds how often we re-walk the repo for changed/new md
	// files. Keeps discoveryDocs() cheap on large repos while still reflecting
	// edits within a few turns.
	mdScanThrottle = 10 * time.Second
)

// mdEntry is one cached summary, invalidated when the file content changes.
// MTime+Size gate the (more expensive) content hash on the hot scan path.
type mdEntry struct {
	Hash    string `json:"hash"`              // sha256 of file bytes
	Summary string `json:"summary"`           // small-model summary (the embedded Text)
	MTime   int64  `json:"mtime"`             // unix nanos, fast-path change gate
	Size    int64  `json:"size"`              // bytes, fast-path change gate
	FailAt  int64  `json:"fail_at,omitempty"` // unix nanos of last failed summarize (negative cache)
}

type mdDiscoveryState struct {
	mu        sync.Mutex
	cache     map[string]mdEntry // rel path → entry
	snapshot  []discovery.Doc    // ready md docs for the corpus (sorted by ID)
	cachePath string
	root      string
	client    LLMClient // model used for summaries (small model, else main client)
	total     int       // md files discovered on the last scan (ready + pending)
	lastScan  time.Time // last completed scan; gates re-walk throttle
}

// mdFailBackoff is how long a file whose summarization failed is left alone
// before retrying — bounds the cost of a doc that always times out or errors.
const mdFailBackoff = 30 * time.Minute

// mdSummaryWorkers bounds concurrent summary model calls during a blocking pass,
// so a large repo summarizes in parallel without flooding the provider.
const mdSummaryWorkers = 6

// mdSummaryClient resolves the model used for markdown summaries: the highest-
// precedence title client (title agent model > recap model > small model, else
// main client). Returns nil only when there is no client at all.
func (a *Agent) mdSummaryClient() LLMClient {
	clients := a.titleClients()
	if len(clients) == 0 {
		return nil
	}
	return clients[0]
}

// mdRef is a markdown file discovered on disk during a scan.
type mdRef struct {
	rel   string
	abs   string
	mtime int64
	size  int64
}

// effectiveWorkDir resolves the agent's working directory (workDir override or
// the process cwd).
func (a *Agent) effectiveWorkDir() string {
	if strings.TrimSpace(a.workDir) != "" {
		return a.workDir
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// mdIsAlwaysOn reports whether a repo-relative md path is part of the always-on
// briefing set that LoadContext already injects in full. Those must NOT be
// discovery-gated — they carry the core instructions and must be present every
// turn regardless of query relevance.
func mdIsAlwaysOn(rel string) bool {
	rel = filepath.ToSlash(rel)
	switch filepath.Base(rel) {
	case "AGENTS.md", "CLAUDE.md", "OCODE.md", ".cursorrules":
		return true
	}
	// Everything under .opencode/rules/ is always-on.
	return strings.HasPrefix(rel, ".opencode/rules/")
}

// mdSummaryCachePath returns the global, project-scoped path for the markdown
// discovery summary cache: GlobalDataDir()/project/{slug}/md-summaries.json.
// It is shared with tests so they track the relocated cache. If the global data
// dir is unavailable it falls back to the legacy repo-relative location.
func mdSummaryCachePath(root string) string {
	base, err := paths.GlobalDataDir()
	if err != nil {
		return filepath.Join(root, ".ocode", "md-summaries.json")
	}
	return filepath.Join(base, "project", paths.ProjectSlug(root), "md-summaries.json")
}

// ensureMDState lazily initializes the markdown corpus: loads the on-disk summary
// cache, then runs the (blocking) summarize pass to generate any missing/stale
// summaries before the corpus is first used. Called once, when discovery becomes
// active. Inactive only when there is no LLM client at all.
func (a *Agent) ensureMDState() {
	if a.mdState != nil {
		return
	}
	client := a.mdSummaryClient()
	if client == nil {
		emitDebug("MD-DISCOVERY", "no LLM client available — markdown discovery inactive")
		return
	}
	root := a.effectiveWorkDir()
	cachePath := mdSummaryCachePath(root)
	st := &mdDiscoveryState{
		cache:     loadMDCache(cachePath),
		cachePath: cachePath,
		root:      root,
		client:    client,
	}
	a.mdState = st
	a.refreshMDSummaries()
}

// refreshMDSummaries re-walks the repo and synchronously (blocking) generates
// summaries for any new/changed/never-summarized files. Throttled so repeated
// Step() calls don't re-walk the tree every turn. Cheap when nothing changed
// (walk + stat only); blocks the caller while uncached files are summarized.
func (a *Agent) refreshMDSummaries() {
	st := a.mdState
	if st == nil {
		return
	}
	st.mu.Lock()
	if !st.lastScan.IsZero() && time.Since(st.lastScan) < mdScanThrottle {
		st.mu.Unlock()
		return
	}
	root := st.root
	st.mu.Unlock()

	a.mdSummarizePass(root)

	st.mu.Lock()
	st.lastScan = time.Now()
	st.mu.Unlock()
}

// mdSummarizePass walks the repo, reuses cached summaries for unchanged files,
// and generates summaries for new/stale ones with bounded concurrency. Blocks
// until every file is resolved, then publishes the snapshot and persists the
// cache. Deleted files drop out (next is rebuilt from the current walk).
func (a *Agent) mdSummarizePass(root string) {
	var ignorePaths []string
	if a.config != nil {
		ignorePaths = a.config.Ocode.Discovery.IgnorePaths
	}
	refs := walkMarkdownFiles(root, ignorePaths...)

	// Snapshot the current cache under lock so we can decide what needs work
	// without holding the lock across model calls.
	st := a.mdState
	st.mu.Lock()
	cur := make(map[string]mdEntry, len(st.cache))
	for k, v := range st.cache {
		cur[k] = v
	}
	st.mu.Unlock()

	next := make(map[string]mdEntry, len(refs))
	var jobs []mdRef // files that need a fresh summary
	dirty := false

	for _, r := range refs {
		// Fast path: unchanged mtime+size → reuse cached entry without hashing.
		if e, ok := cur[r.rel]; ok && e.MTime == r.mtime && e.Size == r.size && e.Summary != "" {
			next[r.rel] = e
			continue
		}
		content, err := os.ReadFile(r.abs)
		if err != nil {
			emitDebug("MD-DISCOVERY", fmt.Sprintf("read %s failed: %v", r.rel, err))
			if e, ok := cur[r.rel]; ok { // keep prior entry on a transient read error
				next[r.rel] = e
			}
			continue
		}
		hash := hashBytes(content)
		// Content unchanged (mtime touched but bytes identical) → refresh gate fields.
		if e, ok := cur[r.rel]; ok && e.Hash == hash && e.Summary != "" {
			e.MTime, e.Size = r.mtime, r.size
			next[r.rel] = e
			dirty = true
			continue
		}
		// Negative cache: a file whose summarization recently failed (same
		// content) is left alone until the backoff elapses.
		if e, ok := cur[r.rel]; ok && e.Hash == hash && e.FailAt != 0 && time.Since(time.Unix(0, e.FailAt)) < mdFailBackoff {
			next[r.rel] = e
			continue
		}
		jobs = append(jobs, r)
	}

	// Generate the missing summaries concurrently (bounded), blocking until done.
	if len(jobs) > 0 {
		results := make([]mdEntry, len(jobs))
		sem := make(chan struct{}, mdSummaryWorkers)
		var wg sync.WaitGroup
		for i, r := range jobs {
			if a.OnMDIndexing != nil {
				a.OnMDIndexing(r.rel)
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, r mdRef) {
				defer wg.Done()
				defer func() { <-sem }()
				content, err := os.ReadFile(r.abs)
				if err != nil {
					emitDebug("MD-DISCOVERY", fmt.Sprintf("read %s failed: %v", r.rel, err))
					return
				}
				hash := hashBytes(content)
				summary := a.summarizeMarkdown(r.rel, content)
				if summary == "" {
					// Record the failure (negative cache) so the next scan backs
					// off. Do NOT inject a placeholder summary.
					results[i] = mdEntry{Hash: hash, MTime: r.mtime, Size: r.size, FailAt: time.Now().UnixNano()}
					return
				}
				results[i] = mdEntry{Hash: hash, Summary: summary, MTime: r.mtime, Size: r.size}
			}(i, r)
		}
		wg.Wait()
		for i, r := range jobs {
			if results[i].Hash != "" { // empty Hash → read failed, skip
				next[r.rel] = results[i]
				dirty = true
			}
		}
	}

	a.publishMDSnapshot(next)
	if dirty {
		if err := saveMDCache(st.cachePath, next); err != nil {
			emitDebug("MD-DISCOVERY", fmt.Sprintf("save cache failed: %v", err))
		}
	}
	st.mu.Lock()
	st.cache = next
	st.total = len(refs)
	st.mu.Unlock()
}

// mdPending returns how many discovered md files do not yet have a ready summary
// (in-flight or failed-and-backing-off). Used by /discover to signal warmup.
func (a *Agent) mdPending() int {
	st := a.mdState
	if st == nil {
		return 0
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	p := st.total - len(st.snapshot)
	if p < 0 {
		return 0
	}
	return p
}

// publishMDSnapshot rebuilds the ready-doc snapshot from a cache map and stores
// it under lock. Only entries with a non-empty summary become docs.
func (a *Agent) publishMDSnapshot(cache map[string]mdEntry) {
	st := a.mdState
	docs := make([]discovery.Doc, 0, len(cache))
	for rel, e := range cache {
		if e.Summary == "" {
			continue
		}
		docs = append(docs, discovery.Doc{
			ID:     "md:" + rel,
			Kind:   "md",
			Name:   rel,
			Text:   rel + ": " + e.Summary,
			Source: filepath.Join(st.root, rel),
		})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	st.mu.Lock()
	st.snapshot = docs
	st.mu.Unlock()
}

// mdDocs returns the current ready snapshot of markdown corpus docs (a copy).
func (a *Agent) mdDocs() []discovery.Doc {
	st := a.mdState
	if st == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.snapshot) == 0 {
		return nil
	}
	out := make([]discovery.Doc, len(st.snapshot))
	copy(out, st.snapshot)
	return out
}

// summarizeMarkdown generates a one-sentence summary of an md file via the small
// model (same client-resolution as title generation). Returns "" on any failure
// — the caller leaves the file uncached so a later pass retries.
func (a *Agent) summarizeMarkdown(rel string, content []byte) string {
	client := a.mdState.client
	if client == nil {
		return ""
	}
	body := string(content)
	if len(body) > mdSummaryMaxInputChars {
		body = body[:mdSummaryMaxInputChars]
	}
	prompt := "File: " + rel + "\n\n" + body

	ctx, cancel := context.WithTimeout(context.Background(), mdSummaryTimeoutSeconds*time.Second)
	defer cancel()
	done := make(chan struct {
		content string
		err     error
	}, 1)
	go func() {
		resp, err := client.Chat([]Message{
			{Role: "system", Content: mdSummarySystemPrompt},
			{Role: "user", Content: prompt},
		}, nil)
		if err != nil {
			done <- struct {
				content string
				err     error
			}{"", err}
			return
		}
		a.RecordSideUsageFromMessage(resp)
		done <- struct {
			content string
			err     error
		}{resp.Content, nil}
	}()

	select {
	case <-ctx.Done():
		emitDebug("MD-DISCOVERY", fmt.Sprintf("summarize %s timeout: %v", rel, ctx.Err()))
		return ""
	case r := <-done:
		if r.err != nil {
			emitDebug("MD-DISCOVERY", fmt.Sprintf("summarize %s error: %v", rel, r.err))
			return ""
		}
		return sanitizeMDSummary(r.content)
	}
}

// sanitizeMDSummary collapses a model summary to a single trimmed line, bounded
// in length.
func sanitizeMDSummary(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.Trim(s, "\"'`")
	if len(s) > mdSummaryMaxOutputChars {
		s = strings.TrimSpace(s[:mdSummaryMaxOutputChars]) + "…"
	}
	return s
}

// walkMarkdownFiles returns every *.md file under root, excluding the always-on
// briefing set, built-in discovery ignore dirs (.agent, .claude, .git, .opencode,
// .pnpm, .qwen, build, dist, node_modules, target, vendor, and the full skills/
// tree), hidden files, .gitignore matches, and any paths matched by ignorePaths
// (prefix or glob against the repo-relative slash path).
func walkMarkdownFiles(root string, ignorePaths ...string) []mdRef {
	ignorePaths = append(config.DefaultDiscoveryIgnorePaths(), ignorePaths...)
	matcher := loadGitignore(root)
	// When an OKF knowledge bundle is active, its docs/ directory is owned by
	// the knowledge system: docs/index.md (a curated TOC) is injected into the
	// system prompt and knowledge_lookup retrieves any concept doc on demand.
	// Skip those files in markdown discovery to avoid a redundant "path —
	// summary" TOC and duplicate small-model summarization.
	var bundleRoot string
	if b, ok := knowledge.DetectBundle(root); ok {
		bundleRoot = b.Root
	}
	var refs []mdRef
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == ".ocode" || name == ".opencode" || name == ".claude" || name == ".qwen" || name == ".agent" || name == ".pnpm" || name == "node_modules" || name == "vendor" || name == "dist" || name == "build" || name == "target" {
				return filepath.SkipDir
			}
			// Skip other hidden dirs.
			if strings.HasPrefix(name, ".") && name != "." && name != ".opencode" {
				return filepath.SkipDir
			}
			// Skip dirs that match an ignore prefix/pattern.
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil {
				rel = filepath.ToSlash(rel) + "/"
				if mdMatchesIgnorePaths(rel, ignorePaths) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if mdIsAlwaysOn(rel) {
			return nil
		}
		// Skip files owned by an active knowledge bundle (see header note).
		if bundleRoot != "" && (path == bundleRoot || strings.HasPrefix(path, bundleRoot+string(os.PathSeparator))) {
			return nil
		}
		if matcher != nil && matcher.Match(strings.Split(rel, "/"), false) {
			return nil
		}
		if mdMatchesIgnorePaths(rel, ignorePaths) {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		refs = append(refs, mdRef{rel: rel, abs: path, mtime: info.ModTime().UnixNano(), size: info.Size()})
		return nil
	})
	return refs
}

// mdMatchesIgnorePaths reports whether rel (repo-relative slash path) matches
// any entry in ignorePaths. Each entry is tried as a prefix first; if it
// contains a glob character it is also tried via filepath.Match.
func mdMatchesIgnorePaths(rel string, ignorePaths []string) bool {
	for _, p := range ignorePaths {
		if strings.HasPrefix(rel, p) {
			return true
		}
		if strings.ContainsAny(p, "*?[") {
			if matched, _ := filepath.Match(p, rel); matched {
				return true
			}
		}
	}
	return false
}

// loadGitignore builds a matcher from the repo's .gitignore / .ignore, or nil.
func loadGitignore(root string) gitignore.Matcher {
	var patterns []gitignore.Pattern
	for _, f := range []string{".gitignore", ".ignore"} {
		data, err := os.ReadFile(filepath.Join(root, f))
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
	return gitignore.NewMatcher(patterns)
}

// loadMDCache reads the summary cache, returning an empty map on any error
// (missing/corrupt cache rebuilds from scratch — logged, not silently dropped).
func loadMDCache(path string) map[string]mdEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			emitDebug("MD-DISCOVERY", fmt.Sprintf("read cache %s: %v", path, err))
		}
		return map[string]mdEntry{}
	}
	var m map[string]mdEntry
	if err := json.Unmarshal(data, &m); err != nil {
		emitDebug("MD-DISCOVERY", fmt.Sprintf("cache %s unreadable, rebuilding: %v", path, err))
		return map[string]mdEntry{}
	}
	if m == nil {
		return map[string]mdEntry{}
	}
	return m
}

// saveMDCache writes the cache atomically (temp + rename) so concurrent sessions
// never observe a half-written file. Before writing, it merges the current
// on-disk contents so that entries produced by a concurrent ocode instance on
// the same project are not lost — only keys absent from `cache` are carried
// over, so our freshly-generated summaries always win for the files we walked.
func saveMDCache(path string, cache map[string]mdEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir md cache dir: %w", err)
	}
	// Merge: pull in any entries written by a concurrent instance that we
	// did not process ourselves. Our entries take precedence.
	if existing := loadMDCache(path); len(existing) > 0 {
		for k, v := range existing {
			if _, have := cache[k]; !have {
				cache[k] = v
			}
		}
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("marshal md cache: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write md cache tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename md cache: %w", err)
	}
	return nil
}

// renderAttachedMarkdown builds the volatile-tail block of filename+summary lines
// for every attached md doc. Returns "" when none are attached.
func (a *Agent) renderAttachedMarkdown(docs []discovery.Doc, isAttached func(id string) bool) string {
	var b strings.Builder
	for _, d := range docs {
		if d.Kind != "md" || !isAttached(d.ID) {
			continue
		}
		if b.Len() == 0 {
			b.WriteString(promptDiscoveryMarker)
			b.WriteString(" relevant project docs for this task:\n")
		}
		writeIndexLine(&b, d)
	}
	return b.String()
}
