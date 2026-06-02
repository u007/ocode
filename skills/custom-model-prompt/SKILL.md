---
name: custom-model-prompt
description: Create or update a model-specific custom prompt file ({MODEL}.OCODE.md)
when_to_use: When the user asks to add, configure, customize, or update a prompt for a specific model. Also triggered by: "custom prompt for", "model-specific behavior", "tailor this model", "system prompt for {model}", "make this model behave", "model instructions"
---

# Custom Model Prompt

Inject a custom system prompt for a specific model via a file named `{MODEL}.OCODE.md`. Three steps.

## 1. Find the right stem

The filename stem (everything before `.OCODE.md`) must match the value the active client's `GetModel()` returns, lowercased. The agent has no other source for this — you have to look it up in the codebase. Try, in order:

1. The model list / provider registry — e.g. `internal/agent/small_model.go`, `internal/agent/client.go` provider tables, `internal/config/ocodeconfig.go` defaults, or the advisor default in `internal/agent/advisor_tool.go`. In this repo the canonical model id is the bare slug (e.g. `deepseek-v4-flash`, `claude-sonnet-4-6`), **not** the `provider/model` form.
2. Concrete greps if (1) is unclear:
   ```bash
   grep -rn "GetModel" --include='*.go' internal/ | grep -v _test.go
   grep -rn '"[a-z][a-z0-9-]*"' --include='*.go' internal/config/ internal/agent/ | grep -i model
   ```
3. **Do not invent.** If you can't find the exact string, ask the user.

## 2. Write the file

Location (first match wins):

| Priority | Location                            | Example                                            |
|----------|-------------------------------------|----------------------------------------------------|
| 1 (highest) | Project root                        | `./deepseek-v4-flash.OCODE.md`                     |
| 2        | `.opencode/`                        | `./.opencode/deepseek-v4-flash.OCODE.md`           |
| 3 (lowest) | Global config                       | `~/.config/opencode/deepseek-v4-flash.OCODE.md`    |

Use the **highest-priority location that makes sense for the request** — usually project root for repo-wide rules, `.opencode/` for personal overrides, global for cross-project behavior. Filename match is case-insensitive; prefer lowercase.

### Content rules

- **If the user gave concrete instructions, use them.** Do not pre-fill from a template. Their wording is the spec; preserve it (or near-verbatim paraphrase) and add only the surrounding structure (Role / Constraints / Domain Knowledge) needed to make it land well as a system prompt.
- **Only fall back to the template** below when the user says something general like "add a custom prompt for this model."
- Keep the file focused. A model-specific prompt is layered on top of the repo's `AGENTS.md` / `CLAUDE.md` — don't duplicate those, **add** to them.

### Template (fallback only)

```markdown
# Model-Specific Instructions for {MODEL}

## Role
[What this model should act like]

## Coding Style
[Language/framework conventions, formatting, testing]

## Constraints
[Things this model should avoid doing]

## Domain Knowledge
[Project context that only applies when this model is active]
```

### Wildcards (use sparingly)

A filename stem ending in a single `*` is a **prefix wildcard** — it matches every model whose id starts with the stem's prefix. Example: `minimax-m*.OCODE.md` matches `minimax-m2`, `minimax-m2.5`, `minimax-m2.7`, `minimax-m3`, and any future `minimax-m*` release.

Rules (see `internal/agent/context.go::LoadModelContext`):

- Only a **trailing** `*` is a wildcard. `*` anywhere else in the stem is literal — so `minimax-*.5` is a literal stem, not a glob.
- A bare `*` stem (`*.OCODE.md`) is **rejected**. Without this guard, the project-root-wins precedence would silently shadow every real model-specific file.
- In the same directory, an **exact match beats a wildcard match** for the same model. So a specific `minimax-m3.OCODE.md` overrides `minimax-m*.OCODE.md` for that one model, while siblings still get the wildcard.
- Across directories, the existing project-root > .opencode/ > global precedence still applies — a project-root wildcard beats a `.opencode/` exact match for the same model.

When to use a wildcard: you have a family of models (e.g. `minimax-m*`) that share a policy, and you don't want to copy-paste the file per slug. When **not** to use a wildcard: the policy is model-specific and you only have one model — the exact-stem form is clearer.

## 3. Verify (do not skip)

A test exists for the loader at `internal/agent/context_test.go::TestLoadModelContext_*`. Run it first:

```bash
go test ./internal/agent/ -run TestLoadModelContext -v
```

Expected: all 14 subtests pass (8 exact-match + 6 wildcard). If the test doesn't exist in this repo (fork / older checkout, or a checkout that pre-dates wildcard support), fall back to:

```bash
# Quick glob: do you have a file the loader would actually pick up?
# (Handles both exact and trailing-* wildcard stems.)
ls -1 *.OCODE.md .opencode/*.OCODE.md ~/.config/opencode/*.OCODE.md 2>/dev/null \
  | awk -v m="$(echo <stem> | tr A-Z a-z)" '{
      n = $0; s = tolower(n); sub(/\.ocode\.md$/, "", s);
      if (s == m) { print "EXACT  " n; next }
      if (match(s, /\*$/)) { p = substr(s, 1, length(s)-1);
        if (length(p) > 0 && index(m, p) == 1) print "WILD   " n }
    }'

# Definitive: a small Go program mirroring LoadModelContext's stem-match
# (exact + trailing-* wildcard). Run with `go run /tmp/verify_model.go <model>`.
```

```go
// /tmp/verify_model.go
package main
import ("fmt"; "os"; "path/filepath"; "strings")
func main() {
    want := strings.ToLower(strings.TrimSpace(os.Args[1]))
    dirs := []string{".", filepath.Join(".", ".opencode")}
    if h, _ := os.UserHomeDir(); h != "" { dirs = append(dirs, filepath.Join(h, ".config", "opencode")) }
    for _, d := range dirs {
        es, err := os.ReadDir(d); if err != nil { continue }
        for _, e := range es {
            n := e.Name(); if !strings.HasSuffix(strings.ToUpper(n), ".OCODE.MD") { continue }
            stem := strings.ToLower(n[:len(n)-len(".OCODE.md")])
            if stem == want { fmt.Println("EXACT", filepath.Join(d, n)); return }
            if strings.HasSuffix(stem, "*") {
                if p := strings.TrimSuffix(stem, "*"); p != "" && strings.HasPrefix(want, p) {
                    fmt.Println("WILD ", filepath.Join(d, n)); return
                }
            }
        }
    }
    fmt.Println("NO MATCH for", os.Args[1]); os.Exit(1)
}
```

A green test (or an `EXACT` / `WILD` line) is the only acceptable signal that the loader will see the file. If you skip this, you are guessing.

## Activation — the git-stable-version rule

`.OCODE.md` files follow the same rule as `AGENTS.md` / `CLAUDE.md` (see `internal/agent/context.go::readContextFile`): **if the file is tracked by git and has unstaged changes, the loader silently uses the HEAD (committed) version and logs `[CONTEXT] using HEAD version of <file> due to unstaged changes` to stderr.** Untracked files are read from the working tree.

Concretely:

- **New file** → first `git add` + `git commit` both creates and activates it.
- **Editing an already-tracked file** → the edit is ignored until you commit it. The loader will keep using the previous committed version and the user will see the swap line in stderr.

**Always commit before telling the user the prompt is active.** Uncommitted = inactive.

## Pre-flight: does this loader exist here?

If you're running on a fork or unfamiliar checkout, confirm `LoadModelContext` is wired in before writing files:

```bash
grep -rn "LoadModelContext\|ModelContext" --include='*.go' internal/agent/
```

If the only hits are in `.opencode/snapshots/`, the loader exists in some snapshot but the live `internal/agent/` does not call it. Don't write the file blind — flag it to the user.

## Worked example

User says: *"add a custom prompt for deepseek v4 flash that prevents it from running `git stash` or reverting files by default."*

1. Stem lookup: `internal/agent/small_model.go` shows `"opencode-go/deepseek-v4-flash"`; bare model id is `deepseek-v4-flash`.
2. File: `./deepseek-v4-flash.OCODE.md` at project root (priority 1).
3. Content: a `## Constraints` section that quotes the user's instruction ("do not use `git stash` or revert files via git by default") and the explicit-ask carve-out. Don't fill in Role / Style / Domain Knowledge from the template — the user's constraint is the entire spec.
4. Verify: `go test ./internal/agent/ -run TestLoadModelContext -v` → PASS.
5. Commit: `git add deepseek-v4-flash.OCODE.md && git commit -m "..."`. Remind the user that until they commit, the prompt is inactive.
