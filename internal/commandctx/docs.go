package commandctx

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/knowledge"
)

// DocsInit bootstraps the OKF knowledge bundle for workDir. It returns the
// human-readable result text plus the context-agent pass the caller should
// dispatch on every call, new bundle or re-run (the TUI sends it through
// dispatchContextAgent; the server returns it as `annotate_prompt` so
// the web client can send it as a normal turn). Moved from internal/tui
// (docs_knowledge.go) so both surfaces behave identically.
func DocsInit(docPromptEnabled bool, workDir string) (text string, annotatePrompt string) {
	if !docPromptEnabled {
		return "Error: knowledge system is not enabled. Run /docs on first.", ""
	}

	if wd := strings.TrimSpace(workDir); wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return fmt.Sprintf("Error resolving working directory: %v", err), ""
		}
		workDir = wd
	}

	// An existing bundle re-audits its index, then runs the same incremental
	// pass as a new one so code features added since the last init get docs.
	if bundle, ok := knowledge.DetectBundle(workDir); ok {
		return docsReAudit(bundle) + "\n" + docsInitDispatchNote, docsInitPrompt
	}

	// Init the bundle: create index.md + log.md with marker.
	if err := knowledge.InitBundle(workDir); err != nil {
		return fmt.Sprintf("Error initializing bundle: %v", err), ""
	}

	// The annotation pass runs as a context-agent turn on both surfaces.
	return "Bundle initialized. " + docsInitDispatchNote, docsInitPrompt
}

const docsInitDispatchNote = "Dispatching context agent to classify existing docs and document code features..."

// docsInitPrompt is the context-agent pass behind /docs init. Part 1 classifies
// hand-written docs; part 2 documents the code's existing features. It is
// incremental: classified docs and already-covered features are skipped, so a
// re-run fills gaps without rewriting docs.
// shortcut: the 25-feature cap bounds one context turn's step budget, raise it once large repos stop converging across re-runs.
const docsInitPrompt = `You are maintaining the OKF knowledge bundle for this project (docs/). If you are the main agent, dispatch this whole task to the "context" sub-agent (task agent=context) and report its result.

Part 1: classify existing docs
1. Scan docs/ for markdown files. Exclude index.md, log.md, and files matching PLAN-*.md or *.OCODE.md.
2. If docs/.okfignore exists, skip every file whose name or path matches its patterns.
3. Skip any file whose frontmatter already has a type. It is already classified.
4. For each remaining file that fits a category, use doc_write to add frontmatter (type, title, description, tags where appropriate), preserving all existing body content.
5. Do NOT delete any content or files.

Categories:
- decisions/ — architecture decisions, technical choices
- playbooks/ — how-to guides, runbooks
- schemas/ — data models, API structures
- gotchas/ — known pitfalls, workarounds
- concepts/ — general documentation, explanations

If a file doesn't fit any category, leave it as-is (non-conforming).

Part 2: document existing code features
Goal: each user-facing feature and each significant subsystem in the code has one bundle doc. Never rewrite a doc that already covers its feature.
1. Read docs/index.md to see what is already documented.
2. Survey these entry points in order: CLI commands, HTTP routes, slash or chat commands, agent tools, public config keys, then the main packages or modules. Read the source to confirm each feature. Do not describe behavior you did not read.
3. For each feature, call doc_search with its name. If a bundle doc covers it, skip it.
4. For each uncovered feature, call doc_write once. Use type Playbook for a workflow a user runs, Schema for a data model or API shape, and Concept for everything else. The body states what it does, its entry points, and its key files and symbols. Cite repo-relative paths and symbol names. Never cite line numbers.
5. Document at most 25 features per pass. Leave the rest for a later /docs init.
6. Do NOT edit index.md or log.md. doc_write maintains both.

Report: docs classified (paths), docs created (paths), features already covered, and features not yet documented, so the next /docs init can continue.`

// docsReAudit regenerates the bundle index under lock and reports counts.
func docsReAudit(bundle *knowledge.Bundle) string {
	// Generate a fresh index under the bundle lock (M1).
	if err := knowledge.WithBundleLock(bundle.Root, func() error {
		return knowledge.GenerateIndex(bundle)
	}); err != nil {
		return fmt.Sprintf("Error regenerating index: %v", err)
	}

	// Count docs.
	allDocs, err := bundle.Docs()
	if err != nil {
		return fmt.Sprintf("Error reading bundle: %v", err)
	}

	conforming := 0
	deprecated := 0
	unclassified := 0
	for _, d := range allDocs {
		if d.Conforming {
			conforming++
		} else {
			unclassified++
		}
		if d.Status == "deprecated" {
			deprecated++
		}
	}

	return fmt.Sprintf("Knowledge bundle re-audited at `%s`.\n- Conforming docs: %d\n- Non-conforming: %d\n- Deprecated: %d\n\nIndex has been regenerated.", bundle.Root, conforming, unclassified, deprecated)
}

// docsCleanupPrompt is the context-agent audit prompt for /docs cleanup. The
// agent decides what is stale, duplicated, superseded or orphaned; it only
// deprecates, because deleting stays a human-confirmed step.
const docsCleanupPrompt = `Audit this project's OKF knowledge bundle (docs/) for cleanup. If you are the main agent, dispatch this audit to the "context" sub-agent (task agent=context) and report its result.

1. Read docs/index.md, then use doc_search and doc_get to review the bundle.
2. Find documents that are:
   - duplicated: two or more docs cover the same topic or decision
   - stale: describes code, files or behavior that no longer exists — verify against the source first
   - superseded: replaced by a newer doc or decision
   - orphaned: no longer about anything in the current codebase
3. Do not deprecate a document unless you are confident it is redundant or obsolete. When in doubt, keep it.
4. For each document to remove, call doc_deprecate with a one-line reason: what supersedes it or why it is stale. If duplicates should be merged, write the surviving doc with doc_write first, then deprecate the others.
5. Never delete a file and never use a shell rm. Deletion happens afterwards via "/docs cleanup --yes".
6. Report the deprecated docs (path + reason), any candidates you kept, and remind the user to run "/docs cleanup --yes" to delete them.`

// DocsCleanup returns the human-readable result plus, when confirm is false,
// the context-agent cleanup-audit prompt the caller should dispatch (the TUI
// sends it through dispatchContextAgent; the server returns it as
// `cleanup_prompt` so the web client can send it as a normal turn).
//
// The audit itself is an LLM pass, never a preset list: the context agent
// reviews the bundle and deprecates what is stale, duplicated, superseded or
// orphaned. With confirm=true it deletes every doc already marked deprecated
// under the bundle lock, appends deletion log entries, and regenerates the index.
func DocsCleanup(workDir string, confirm bool) (text string, cleanupPrompt string) {
	bundle, ok := knowledge.DetectBundle(workDir)
	if !ok {
		return "No OKF knowledge bundle found. Run /docs init first.", ""
	}

	allDocs, err := bundle.Docs()
	if err != nil {
		return fmt.Sprintf("Error reading bundle: %v", err), ""
	}

	var deprecated []*knowledge.Doc
	for _, d := range allDocs {
		if d.Status == "deprecated" {
			deprecated = append(deprecated, d)
		}
	}
	sort.Slice(deprecated, func(i, j int) bool {
		return deprecated[i].Path < deprecated[j].Path
	})

	if !confirm {
		// The LLM decides what to deprecate; deletion stays human-gated.
		return "Auditing the knowledge bundle for cleanup candidates...", docsCleanupPrompt
	}

	if len(deprecated) == 0 {
		return "No deprecated documents to clean up.", ""
	}

	// Perform deletion under lock.
	deleted := 0
	err = knowledge.WithBundleLock(bundle.Root, func() error {
		for _, d := range deprecated {
			fullPath := filepath.Join(bundle.Root, d.Path)
			if err := os.Remove(fullPath); err != nil {
				// Log but continue — don't let one failure abort the batch.
				agent.DebugAppendf("DOCS", "cleanup: failed to remove %s: %v", fullPath, err)
				continue
			}
			if err := knowledge.AppendLog(bundle, "Deletion", d.Path, fmt.Sprintf("Deleted deprecated doc: %s", d.Title)); err != nil {
				agent.DebugAppendf("DOCS", "cleanup: failed to append log for %s: %v", d.Path, err)
			}
			deleted++
		}
		// Regenerate index.
		if err := knowledge.GenerateIndex(bundle); err != nil {
			return fmt.Errorf("regenerate index after cleanup: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Sprintf("Error during cleanup: %v", err), ""
	}

	return fmt.Sprintf("Deleted %d deprecated document(s). Index and log updated.", deleted), ""
}

// DocsStatus returns rich status about the knowledge system, mirroring the
// TUI's /docs status output.
func DocsStatus(docPromptEnabled bool, workDir string) string {
	var b strings.Builder
	b.WriteString("## Knowledge System Status\n\n")
	b.WriteString(fmt.Sprintf("**Doc prompt enabled:** %v\n", docPromptEnabled))

	if !docPromptEnabled {
		b.WriteString("\n**Active:** No (DocPromptEnabled is off)\n")
		b.WriteString("Run `/docs on` to enable the doc-first prompt, then `/docs init` to set up the OKF bundle.\n")
		return b.String()
	}

	bundle, bundleOK := knowledge.DetectBundle(workDir)

	if !bundleOK {
		b.WriteString("\n**Bundle present:** No\n")
		b.WriteString("Run `/docs init` to create an OKF knowledge bundle.\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("**Bundle root:** `%s`\n", bundle.Root))
	b.WriteString(fmt.Sprintf("**OKF version:** %s\n", bundle.OKFVersion))

	allDocs, err := bundle.Docs()
	conforming := 0
	deprecated := 0
	unclassified := 0
	lastLogDate := ""
	if err == nil {
		for _, d := range allDocs {
			if d.Conforming {
				conforming++
			} else {
				unclassified++
			}
			if d.Status == "deprecated" {
				deprecated++
			}
		}
	}

	// Read last log entry date.
	logPath := filepath.Join(bundle.Root, "log.md")
	if data, err := os.ReadFile(logPath); err == nil {
		content := string(data)
		// Find latest "## YYYY-MM-DD" heading.
		for _, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(line, "## ") && len(line) > 3 {
				lastLogDate = strings.TrimPrefix(line, "## ")
			}
		}
	}

	b.WriteString(fmt.Sprintf("**Conforming docs:** %d\n", conforming))
	b.WriteString(fmt.Sprintf("**Non-conforming:** %d\n", unclassified))
	b.WriteString(fmt.Sprintf("**Deprecated:** %d\n", deprecated))
	if lastLogDate != "" {
		b.WriteString(fmt.Sprintf("**Last log entry:** %s\n", lastLogDate))
	}
	b.WriteString("\n**Active:** Yes\n")

	return b.String()
}
