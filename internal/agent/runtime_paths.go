package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// runtimePathsSection returns formatted lines to append inside the <env> block.
// It detects JS/Bun/React and Python projects and enumerates relevant tool paths.
// Returns nil when no relevant project is detected.
// Output is capped to 80 lines to bound prompt size.
func runtimePathsSection(root, cwd string) []string {
	detectRoot := root
	if detectRoot == "" {
		detectRoot = cwd
	}
	if detectRoot == "" {
		return nil
	}
	jsRoots := jsProjectRoots(detectRoot)
	pyRoots := pythonProjectRoots(detectRoot)
	if len(jsRoots) == 0 && len(pyRoots) == 0 {
		return nil
	}
	var lines []string
	if len(jsRoots) > 0 {
		if jsLines := jsRuntimePaths(jsRoots); len(jsLines) > 0 {
			lines = append(lines, jsLines...)
		}
	}
	if len(pyRoots) > 0 {
		if pyLines := pythonRuntimePaths(pyRoots); len(pyLines) > 0 {
			lines = append(lines, pyLines...)
		}
	}
	const maxRuntimeLines = 80
	if len(lines) > maxRuntimeLines {
		lines = append(lines[:maxRuntimeLines], fmt.Sprintf("  ... truncated %d additional runtime lines", len(lines)-maxRuntimeLines))
	}
	return lines
}

func jsProjectRoots(root string) []string {
	roots := candidateRoots(root)
	jsMarkers := []string{
		"package.json",
		"bun.lockb",
		"bun.lock",
		"pnpm-lock.yaml",
		"yarn.lock",
		"package-lock.json",
		"npm-shrinkwrap.json",
		".nvmrc",
		".node-version",
	}
	var out []string
	for _, r := range roots {
		for _, m := range jsMarkers {
			if fileExists(filepath.Join(r, m)) {
				out = append(out, r)
				break
			}
		}
	}
	return dedupSorted(out)
}

func pythonProjectRoots(root string) []string {
	roots := candidateRoots(root)
	pyMarkers := []string{
		"pyproject.toml",
		"setup.py",
		"setup.cfg",
		"Pipfile",
		"poetry.lock",
		"uv.lock",
		"Pipfile.lock",
		"manage.py",
	}
	var out []string
	for _, r := range roots {
		matched := false
		for _, m := range pyMarkers {
			if fileExists(filepath.Join(r, m)) {
				matched = true
				break
			}
		}
		if !matched {
			// requirements*.txt glob
			if matches, _ := filepath.Glob(filepath.Join(r, "requirements*.txt")); len(matches) > 0 {
				matched = true
			}
		}
		if matched {
			out = append(out, r)
		}
	}
	return dedupSorted(out)
}

func candidateRoots(root string) []string {
	roots := []string{root}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				continue
			}
			sub := filepath.Join(root, name)
			if fileExists(filepath.Join(sub, "package.json")) ||
				fileExists(filepath.Join(sub, "pyproject.toml")) ||
				fileExists(filepath.Join(sub, "requirements.txt")) ||
				fileExists(filepath.Join(sub, "bun.lockb")) ||
				fileExists(filepath.Join(sub, "bun.lock")) ||
				fileExists(filepath.Join(sub, "pnpm-lock.yaml")) ||
				fileExists(filepath.Join(sub, "Pipfile")) ||
				fileExists(filepath.Join(sub, "poetry.lock")) ||
				fileExists(filepath.Join(sub, "uv.lock")) {
				roots = append(roots, sub)
			}
			// Also check glob for requirements*.txt
			if matches, _ := filepath.Glob(filepath.Join(sub, "requirements*.txt")); len(matches) > 0 {
				already := false
				for _, r := range roots {
					if r == sub {
						already = true
						break
					}
				}
				if !already {
					roots = append(roots, sub)
				}
			}
		}
	}
	return roots
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func dedupSorted(in []string) []string {
	m := make(map[string]struct{}, len(in))
	for _, s := range in {
		m[s] = struct{}{}
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func jsRuntimePaths(roots []string) []string {
	var lines []string
	lines = append(lines, "  Runtime paths (JS/Bun):")

	tools := []string{"node", "bun", "npm", "pnpm", "npx"}
	for _, t := range tools {
		if p, err := exec.LookPath(t); err == nil {
			line := fmt.Sprintf("    - %s: %s", t, p)
			if v := toolVersion(t); v != "" {
				line += fmt.Sprintf(" (%s)", v)
			}
			lines = append(lines, line)
		} else {
			// Only show not-found for core tools; skip npx if missing
			if t != "npx" {
				lines = append(lines, fmt.Sprintf("    - %s: not found in PATH", t))
			}
		}
	}

	// NVM: enumerate each version's bin for node/npm/pnpm/npx
	if nvmLines := nvmPathsDetailed(); len(nvmLines) > 0 {
		lines = append(lines, nvmLines...)
	}

	// .nvmrc / .node-version for each detected root
	for _, r := range roots {
		for _, name := range []string{".nvmrc", ".node-version"} {
			p := filepath.Join(r, name)
			if data, err := os.ReadFile(p); err == nil {
				v := strings.TrimSpace(string(data))
				if v != "" {
					// Avoid embedding overly long or suspicious content; cap at 40 chars, single line
					if len(v) > 40 {
						v = v[:40]
					}
					v = strings.ReplaceAll(v, "\n", "")
					rel := r
					// Show relative path if not root
					lines = append(lines, fmt.Sprintf("    - %s (%s): %s", name, rel, v))
				}
			}
		}
	}

	return lines
}

func nvmPathsDetailed() []string {
	nvmDir := os.Getenv("NVM_DIR")
	if nvmDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".nvm")
			if fileExists(candidate) {
				nvmDir = candidate
			}
		}
	}
	if nvmDir == "" || !fileExists(nvmDir) {
		return nil
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("    - NVM_DIR: %s", nvmDir))

	aliasPath := filepath.Join(nvmDir, "alias", "default")
	if data, err := os.ReadFile(aliasPath); err == nil {
		v := strings.TrimSpace(string(data))
		if v != "" {
			if len(v) > 40 {
				v = v[:40]
			}
			lines = append(lines, fmt.Sprintf("    - nvm default alias: %s", v))
		}
	}

	versionsDir := filepath.Join(nvmDir, "versions", "node")
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return lines
	}
	var vers []string
	for _, e := range entries {
		if e.IsDir() {
			vers = append(vers, e.Name())
		}
	}
	sort.Strings(vers)
	capped := vers
	suffix := ""
	if len(vers) > 10 {
		capped = vers[len(vers)-10:]
		suffix = fmt.Sprintf(" (+%d older)", len(vers)-10)
	}
	// Tools managed by nvm
	nvmTools := []string{"node", "npm", "pnpm", "npx"}
	for _, v := range capped {
		bin := filepath.Join(versionsDir, v, "bin")
		if !fileExists(bin) {
			continue
		}
		lines = append(lines, fmt.Sprintf("    - nvm %s: %s", v, bin))
		for _, t := range nvmTools {
			tp := filepath.Join(bin, t)
			if fileExists(tp) {
				lines = append(lines, fmt.Sprintf("      - %s: %s", t, tp))
			}
		}
	}
	if suffix != "" {
		lines = append(lines, fmt.Sprintf("    - nvm versions: %d total%s", len(vers), suffix))
	}
	return lines
}

func pythonRuntimePaths(roots []string) []string {
	var lines []string
	lines = append(lines, "  Runtime paths (Python):")

	tools := []string{"python", "python3", "pip", "pip3", "uv", "poetry"}
	for _, t := range tools {
		if p, err := exec.LookPath(t); err == nil {
			line := fmt.Sprintf("    - %s: %s", t, p)
			if v := toolVersion(t); v != "" {
				line += fmt.Sprintf(" (%s)", v)
			}
			lines = append(lines, line)
		}
	}

	// Local venvs for each detected root
	for _, r := range roots {
		for _, name := range []string{".venv", "venv", ".env", "env"} {
			bin := filepath.Join(r, "bin", "python")
			if fileExists(filepath.Join(r, name, "bin", "python")) {
				bin = filepath.Join(r, name, "bin", "python")
				lines = append(lines, fmt.Sprintf("    - venv %s (%s): %s", name, r, bin))
			}
			bin3 := filepath.Join(r, name, "bin", "python3")
			if fileExists(bin3) && bin3 != bin {
				lines = append(lines, fmt.Sprintf("    - venv %s/python3 (%s): %s", name, r, bin3))
			}
		}
	}

	// pyenv
	pyenvRoot := os.Getenv("PYENV_ROOT")
	if pyenvRoot == "" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".pyenv")
			if fileExists(candidate) {
				pyenvRoot = candidate
			}
		}
	}
	hasPyenv := pyenvRoot != "" && fileExists(pyenvRoot)
	if hasPyenv {
		lines = append(lines, fmt.Sprintf("    - PYENV_ROOT: %s", pyenvRoot))
		shims := filepath.Join(pyenvRoot, "shims")
		if fileExists(shims) {
			lines = append(lines, fmt.Sprintf("    - pyenv shims: %s", shims))
		}
		if data, err := os.ReadFile(filepath.Join(pyenvRoot, "version")); err == nil {
			v := strings.TrimSpace(string(data))
			if v != "" && len(v) < 40 {
				lines = append(lines, fmt.Sprintf("    - pyenv global: %s", v))
			}
		}
	}
	for _, r := range roots {
		verFile := filepath.Join(r, ".python-version")
		if data, err := os.ReadFile(verFile); err == nil {
			v := strings.TrimSpace(string(data))
			if v != "" {
				if len(v) > 40 {
					v = v[:40]
				}
				v = strings.ReplaceAll(v, "\n", "")
				lines = append(lines, fmt.Sprintf("    - .python-version (%s): %s", r, v))
				if hasPyenv {
					pyBin := filepath.Join(pyenvRoot, "versions", v, "bin", "python")
					if fileExists(pyBin) {
						lines = append(lines, fmt.Sprintf("    - pyenv %s: %s", v, pyBin))
					}
				}
			}
		}
	}
	if !hasPyenv {
		// Check for .python-version even without pyenv already handled above
	}

	if len(lines) == 1 {
		lines = append(lines, "    - (no python tools found in PATH)")
	}
	return lines
}

func toolVersion(name string) string {
	cmd := exec.Command(name, "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if idx := strings.Index(v, "\n"); idx != -1 {
		v = v[:idx]
	}
	if len(v) > 80 {
		v = v[:80]
	}
	return v
}
