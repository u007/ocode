# browser_run: Jev-driven step loop — implementation plan

Spec: `docs/superpowers/specs/2026-11-01-browser-run-jev-step-loop-design.md`. Read it first; every
part below assumes it.

Parts are ordered by dependency. Each part file is self-contained: it names the files to touch, the
behaviour to add, the constraints, and the verify commands. Plans are high-level: no code in them.

| Part | Scope | Depends on |
| --- | --- | --- |
| `01-cdp-controls-and-settle.md` | `internal/browse/cdp`: `Settle` and a structured `Controls` index from the accessibility tree | – |
| `02-browser-tool-controls-action.md` | `internal/server`: new read action `controls`, dialog summary for `run_step` | 01 |
| `03-agent-slot-and-config.md` | `internal/config`, `internal/agent/decider.go`: slot `browser_run`, floors | – |
| `04-browser-run-tool.md` | `internal/agent/browser_run.go`: the tool, the Jev request, the loop, registration | 02, 03 |
| `05-tests.md` | unit tests for 02 and 04 with the existing fakes and a fake TypeSafe server | 04 |
| `06-docs.md` | concept page, `browser-agent-tools.md`, `CLAUDE.md`, `TODO.md` | 05 |

Global constraints for every part (from `CLAUDE.md` and the user's rules):

- Go 1.26, `gofmt`, `go vet`. No fallbacks, no `value ?? default`-style defaults, no empty `catch`,
  no flags that change behaviour unless the spec names them.
- Never `git stash`, `git reset --hard`, `git checkout -- <file>`, `git clean`. The working tree
  has many unrelated uncommitted changes; touch only the files the part names.
- Every caught error is logged with what was attempted (`log.Printf` or `a.emitDebug`), or carries
  an `// intentionally not logged: <reason>` comment.
- Nothing per-turn-varying in a tool `Definition` (prompt cache). Enums sorted.
- Never widen `Decider`; never cache a nil decision client; never route a decision model through chat.
- Tests that override `HOME` use `setHomeTree`. Run the package's tests, not `make test`.
