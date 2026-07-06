# Part 03 — Context agent, doc tools, knowledge_lookup, index injection

Spec: `docs/superpowers/specs/2026-07-06-okf-context-agent-design.md` (sections: Context agent, Tools, Read path, Coexistence). Read it before starting. Depends on Part 01 (`internal/knowledge` Store).

Global constraints (self-contained copy): knowledge system active only when `DocPromptEnabled` AND `knowledge.DetectBundle` finds the `okf_version` marker; doc tools must never be registered on the main agent (subagent inheritance intersects with the main agent's tool list — injecting on main would hand it a write path); `knowledge_lookup` always registered, soft-fails when inactive (stable tool-definition block preserves prompt cache); index injection follows memory-injection refresh semantics (no mid-session prompt rebuild on index regen); caught errors logged; `go build ./...` + `go test ./internal/agent/` per task; TDD; commit per task with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

Verified code anchors: subagent defs `internal/agent/subagent.go:71` (`DefaultSubAgents`; see `explore` at :80 for the allowlist naming convention — the list tool is named `list`, not `ls`); tool inheritance `getToolsForDef` `subagent.go:505`, consumed at `subagent.go:245`; small-model eligibility `smallModelEligibleNames` `internal/agent/small_model.go:45`; docs primary agent `internal/agent/registry.go:49` (has write/edit/delete, lacks `task`); memory prompt injection `internal/agent/prompt.go:99–110`; doc-first fragment `prompt.go:122`.

---

## Task 6: Doc tools as agent tools

**Files:**
- Create: `internal/agent/doc_tools.go` — four tool structs wrapping `internal/knowledge.Store`
- Test: `internal/agent/doc_tools_test.go`

**Interfaces consumed (Part 01):** `knowledge.DetectBundle(workDir) (*Bundle, bool)`, `knowledge.NewStore(b)`, `Store.Search(query, tags, docType, page, pageSize)`, `Store.Get(relPath)`, `Store.Write(relPath, DocMeta, body)`, `Store.Deprecate(relPath, reason)`.

**Interfaces produced:**
- Four tools following the existing tool-struct pattern (mirror an existing simple tool in `internal/agent/` for Name/Description/Schema/Execute shape):
  - `doc_search` — params `query` (required), `tags` (string list, optional), `type` (optional), `page` (optional, default 1). Returns formatted, sorted, paginated results with total count.
  - `doc_get` — param `path` (required, bundle-relative).
  - `doc_write` — params `path`, `type` (both required), `title`, `description`, `resource`, `tags`, `body`. Maps onto `Store.Write`; store-layer guards (reserved files, traversal, empty type) surface as tool errors verbatim.
  - `doc_deprecate` — params `path`, `reason` (both required).
- Constructor `func newDocTools(workDir string) ([]Tool, error)` (match the actual tool interface name used in the codebase) — resolves the bundle via `DetectBundle`; returns an error when no bundle, callers decide how to handle.
- Tool descriptions must tell the model: bundle-relative paths, `type` frontmatter examples ("Decision", "Playbook", "Schema", "Gotcha"), deprecate-don't-delete, and that index/log are maintained automatically.

**Steps:**
- [ ] Write failing tests against a temp fixture bundle: each tool happy path; `doc_write` guard errors pass through; `doc_search` pagination params.
- [ ] Run tests → fail.
- [ ] Implement `doc_tools.go`.
- [ ] `go test ./internal/agent/ -v -run DocTool` → PASS; `go build ./...`.
- [ ] Commit: `feat(agent): doc tools wrapping knowledge store`.

---

## Task 7: `context` subagent + dispatch-time doc-tool injection + eligibility + docs-agent task access

**Files:**
- Modify: `internal/agent/subagent.go` — add `context` to `DefaultSubAgents` (:71); inject doc tools at dispatch (around :245 where `getToolsForDef` output is consumed)
- Modify: `internal/agent/small_model.go` — add `context` to `smallModelEligibleNames` (:45)
- Modify: `internal/agent/registry.go` — add `task` to the `docs` primary agent's `Tools` (:49)
- Test: `internal/agent/subagent_test.go` (extend)

**Interfaces consumed:** `newDocTools(workDir)` from Task 6.

**Interfaces produced:**
- `DefaultSubAgents` entry `context`: description "knowledge curator and retriever for the project's OKF docs/ bundle — answers why/decision/playbook questions from curated docs, cites doc paths, sole automated writer of the bundle"; prompt covering: consult index/frontmatter first (progressive disclosure), verify doc claims against code before answering or writing, write only via doc tools, prefer updating an existing doc over creating near-duplicates, deprecate rather than delete; `Tools` allowlist exactly: `grep`, `glob`, `read`, `list` (verify each name against the parent tool `Name()` values — copy from the `explore` entry) — doc tools are NOT in the allowlist because they don't exist on the main agent.
- Dispatch-time injection: where the subagent's tool set is assembled from `getToolsForDef`, when the resolved agent name is `context`, append `newDocTools(workDir)` output. If the bundle is absent, log at debug and dispatch without doc tools (the agent prompt tells it to say the knowledge system is not initialized).
- `context` added to `smallModelEligibleNames` → small-model routing with existing main-client fallback, no new code.
- `docs` primary agent gains `task` so `knowledge_lookup`/context dispatch work while it is active.

**Steps:**
- [ ] Write failing tests: resolving agent `context` yields a tool set containing the four doc tools plus read-only code tools and nothing writable (`write`/`edit`/`bash` absent); a non-context subagent (e.g. `explore`) does NOT receive doc tools; main agent's `GetTools()` does not contain doc tools; eligibility list contains `context`.
- [ ] Run tests → fail.
- [ ] Implement the three modifications.
- [ ] `go test ./internal/agent/ -v` → PASS; `go build ./...`.
- [ ] Commit: `feat(agent): context subagent with scoped doc tools and small-model routing`.

---

## Task 8: `knowledge_lookup` tool + `[ocode:knowledge]` index injection

**Files:**
- Create: `internal/agent/knowledge_lookup.go`
- Modify: `internal/agent/agent.go` — register `knowledge_lookup` in the main tool set (near where `task` is registered, ~:607)
- Modify: `internal/agent/prompt.go` — inject index under `[ocode:knowledge]` next to the memory injection (:99–110); extend the doc-first fragment (:122) with one short paragraph of usage guidance
- Test: `internal/agent/knowledge_lookup_test.go`, extend `internal/agent/prompt_test.go` (or nearest existing prompt test file)

**Interfaces consumed:** `knowledge.DetectBundle`; `TaskTool` dispatch path (the same internal entry `task` uses to run a named subagent); agent flag `a.DocPromptEnabled()` (`internal/agent/agent.go:521–527`).

**Interfaces produced:**
- Tool `knowledge_lookup`, param `question` (string, required). Always registered. Execute: if `DocPromptEnabled` is false or `DetectBundle` returns false → return soft tool result "knowledge system not active — run /docs init" (a result, not an error, so the model reads it). Otherwise dispatch the `context` subagent synchronously via the TaskTool path with the question, returning its distilled answer (which includes source doc paths per the context-agent prompt).
- Prompt injection: when active (flag + marker), `BasePromptMessages` appends the full current `docs/index.md` content under marker `[ocode:knowledge]`, read at the same time memory scopes are loaded (same rebuild cadence — no extra refresh triggers; a regenerated index is picked up at the next natural prompt rebuild).
- Doc-first fragment addition (only when the knowledge system is active): try `knowledge_lookup` first for why/decision/playbook questions; use `explore` for code-level questions; for mixed questions dispatch `context` and `explore` in background concurrently, take the first sufficient answer, `task_cancel` the other.

**Steps:**
- [ ] Write failing tests: inactive flag → soft message; active but no marker → soft message; prompt contains `[ocode:knowledge]` + index content only when flag AND marker present; fragment guidance present only when active.
- [ ] Run tests → fail.
- [ ] Implement tool, registration, injection, fragment text.
- [ ] `go test ./internal/agent/ -v -run 'Knowledge|Prompt'` → PASS; `go build ./...`.
- [ ] Commit: `feat(agent): knowledge_lookup tool and [ocode:knowledge] index injection`.
