package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/redact"
)

// importedModuleScriptsForJudge returns the source of the modules an inline or
// heredoc interpreter source IMPORTS from the project, in the same
// executedScriptContext shape executedScriptsForJudge uses.
//
// Why it exists: a module import runs that module's top-level code, so it is the
// imported FILE's calls — not the import statement — that a permission judge
// must weigh. Without the source, `python3 -c "import helper"` was an opaque
// import: the TypeSafe judge has no read_file tool, so it could only name
// truncated_or_unknown and defer, and the chat judge was told to treat an
// "unresolved import" the same way. Shipping the source instead lets both
// judges decide the question that actually matters — where the call GOES — and
// lets the rubric say, without exceptions, that an import is never itself a
// fact. measured 2026-10-28: a bare `import playwright` scored allow at
// 0.34-0.40 against the 0.80 floor purely because the name looked network-y.
//
// Scope and limits, all deliberately fail-closed in the cheap direction:
//   - Only the language whose module syntax we can resolve reliably today
//     (python). Other languages return nil and keep the pre-existing behaviour.
//   - Only modules that resolve to a regular file inside the allowed scope
//     (resolveCustomScript applies the same containment check as a command's
//     script operand). An installed package resolves to nothing and therefore
//     stays inert — which is the point: importing it opens no connection.
//   - A relative import (from . import x) is skipped: it needs the enclosing
//     package context to resolve, and a WRONG resolution would mis-attribute a
//     file to the judge.
//   - Credential-bearing files are never shipped (isSecretMaterialPath /
//     redact.IsSensitiveFile), same as every other judge-facing source.
//   - readFileSnippet + maxInterpreterSourceBytes bound the bytes exactly like
//     executedScriptsForJudge, so a truncated entry can never produce a grant
//     (verifyAutoGrant refuses a partial view).
//
// Additive: nothing here allows or denies. Omitting a module leaves the
// pre-existing deferral in place.
func (a *Agent) importedModuleScriptsForJudge(language, source, workDir string, budget int) []executedScriptContext {
	if budget <= 0 || source == "" || workDir == "" {
		return nil
	}
	if language != "python" {
		return nil
	}
	maxLines := a.executedScriptLineCap()
	var out []executedScriptContext
	seen := make(map[string]bool)
	for _, module := range pythonImportedModules(source) {
		if len(out) >= budget {
			break
		}
		for _, path := range a.resolveImportedModule(module, workDir) {
			if len(out) >= budget {
				break
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			// The judge is an LLM: never ship a credential-bearing file into its prompt.
			if redact.IsSensitiveFile(path) || isSecretMaterialPath(path) {
				continue
			}
			content, totalLines, err := readFileSnippet(path, maxLines)
			if err != nil || !isValidTextContent(content) {
				continue
			}
			truncated := totalLines > maxLines
			if len(content) > maxInterpreterSourceBytes {
				content = content[:maxInterpreterSourceBytes]
				truncated = true
			}
			clean, valid := sanitizeSource(content)
			if !valid || strings.TrimSpace(clean) == "" {
				continue
			}
			out = append(out, executedScriptContext{
				Path:       path,
				Text:       clean,
				SHA256:     hashBytes([]byte(clean)),
				TotalLines: totalLines,
				Truncated:  truncated,
			})
		}
	}
	return out
}

// resolveImportedModule maps a dotted module name to the files python would load
// for it from workDir: <name>.py, then <name>/__init__.py (a package). Only
// paths that pass the same in-scope and regularity checks as a command's script
// operand are returned, so a name pointing outside the allowed roots — or at a
// directory, a symlink target or a special file — yields nothing.
func (a *Agent) resolveImportedModule(module, workDir string) []string {
	parts := strings.Split(module, ".")
	for _, p := range parts {
		if !isPythonIdentifier(p) {
			return nil
		}
	}
	rel := filepath.Join(parts...)
	candidates := []string{
		filepath.Join(workDir, rel+".py"),
		filepath.Join(workDir, rel, "__init__.py"),
	}
	var out []string
	seen := make(map[string]bool)
	for _, cand := range candidates {
		path := a.resolveCustomScript(cand)
		if path == "" || seen[path] {
			continue
		}
		// Refuse a symlinked module. resolveCustomScript's containment check runs
		// on the PATH, which would let `<project>/x.py -> ~/.ssh/id_rsa` ship a
		// credential to the judge (and, for a link out of the roots, ship a file
		// the scope check never saw). An unshipped module simply defers.
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// pythonImportedModules extracts the dotted module names a python source imports.
//
// Heuristic by design, and it fails harmless: a false POSITIVE only offers a
// candidate path that must still resolve to a real in-scope file to be shipped,
// and a false NEGATIVE leaves the module unsent (the pre-existing deferral).
// Triple-quoted bodies and line comments are stripped first so prose that
// mentions "import os" is not read as code.
func pythonImportedModules(source string) []string {
	if source == "" {
		return nil
	}
	// ';' separates statements on a -c one-liner ("import playwright, sys; print(…)");
	// splitting first keeps the second statement from being read as an imported name.
	src := strings.ReplaceAll(stripPythonTripleQuotedBodies(source), ";", "\n")
	seen := make(map[string]bool)
	var mods []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		name = strings.TrimSuffix(name, "\\")
		if name == "" || strings.HasPrefix(name, ".") || seen[name] {
			return
		}
		seen[name] = true
		mods = append(mods, name)
	}
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(stripPythonLineComment(raw))
		if line == "" {
			continue
		}
		if rest, ok := cutPythonKeyword(line, "import"); ok {
			for _, item := range strings.Split(rest, ",") {
				fields := strings.Fields(item)
				if len(fields) == 0 {
					continue
				}
				add(fields[0])
			}
			continue
		}
		rest, ok := cutPythonKeyword(line, "from")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		module := fields[0]
		add(module)
		// "from a.b import c" may import the SUBMODULE a.b.c; resolveImportedModule
		// only returns something when that file actually exists.
		if len(fields) >= 3 && fields[1] == "import" && !strings.HasPrefix(module, ".") {
			names := strings.Trim(strings.Join(fields[2:], " "), "()")
			for _, item := range strings.Split(names, ",") {
				name := strings.Fields(strings.TrimSpace(item))
				if len(name) == 0 {
					continue
				}
				add(module + "." + name[0])
			}
		}
	}
	sort.Strings(mods)
	return mods
}

// cutPythonKeyword returns the text after a leading keyword that is a standalone
// word (so "imported_flag = 1" is not an import statement).
func cutPythonKeyword(line, keyword string) (string, bool) {
	if !strings.HasPrefix(line, keyword) {
		return "", false
	}
	rest := line[len(keyword):]
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t' && rest[0] != '(') {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// stripPythonLineComment removes a trailing "# ..." comment that is not inside a
// single- or double-quoted string. Escape sequences are not tracked: a false
// positive here only shortens a line, and the parser is advisory.
func stripPythonLineComment(line string) string {
	var quote rune
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#':
			return line[:i]
		}
	}
	return line
}

// isPythonIdentifier reports whether s is a plausible python identifier segment,
// which also keeps a junk token out of a filesystem path.
func isPythonIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
