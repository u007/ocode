---
type: Gotcha
title: Auto-Permission Dependency Binaries — Policy Decision
description: Policy split for project/toolchain dependency binaries: the bundled chat-judge v1.9.5 action rule remains prose-only, while the TypeSafe/Jev rubric is category-based and the deterministic layer statically allows only named development tools behind an argument gate.
tags:
  - auto-permission
  - gotcha
  - security
  - policy-decision
timestamp: 2026-11-27T16:04:13Z
---
## 1. THE POLICY

The bundled **chat-judge** addendum (`internal/config/auto_permission_prompt.go:91`, bundled v1.9.5 at `internal/config/auto_permission_prompt.go:30`) directs the chat LLM permission judge to **ALLOW** direct invocation of project/toolchain dependency binaries — `node_modules/.bin`, `node_modules/**/bin`, `.venv/bin|venv/bin|env/bin`, pyenv shims, `go run`/`go tool`/GOPATH/bin, project `./bin`, `cargo` + `~/.cargo/bin`, Rust `target/debug|release`, PHP `vendor/bin`, Ruby `bundle exec`, Java/Kotlin `gradlew`/`mvnw`, `.NET` local tools — judging the **ACTION** (build/test/lint/format/codegen/local file processing → ALLOW), not the binary's location. DENY if the action writes sensitive paths (`.git/`), exfiltrates, or is destructive.

**This action rule does not reach Jev.** It lives in `BundledAutoPermissionPromptBody` for the bundled chat judge. TypeSafe/Jev receives `typesafeJudgeInstructions`, embedded from `internal/agent/prompts/permission_judge.txt` (`internal/agent/permission_typesafe.go:199`), whose development-tool rule is category-based (`internal/agent/prompts/permission_judge.txt:33`): named formatter/linter/type-checker/test invocations are ordinary work, but installs, package overrides, plugin/custom-code loading, unknown binaries, outside-root paths, secrets, and destructive/banned actions are not excused by a dependency-local path.

## 2. THREAT MODEL / RESIDUAL RISK (accepted)

(a) The executed file is a tiny shim/console-script wrapper — the deterministic backstop (`Agent.detectExecutedCustomScripts` in `internal/agent/script_detection.go:249` + the truncation guard in `Agent.verifyAutoGrant`, `internal/agent/agent.go:5240`, with a refusal at `internal/agent/agent.go:5256`) inspects only the wrapper; the imported payload module or compiled binary is opaque to the judge.

(b) Judge-ALLOW removes the human prompt but adds **no confinement** — the write-wall exists only in sandbox mode; network egress is always open (sandbox is write-integrity only — cite `architecture/shell-sandbox-integrity-only-mode.md`), so a judged-allowed binary can read global files and POST anywhere.

(c) A malicious dependency can ship a plausibly-named binary that passes the "judge the action" test.

## 3. MITIGATIONS THAT MAKE IT DELIBERATE

The bundled chat prose human-gates package installs and package-manager script/execution commands (`internal/config/auto_permission_prompt.go:85-89`). Its fail-closed sentence — *"If you cannot establish what the command does… require human approval; a familiar-looking name proves nothing"* — remains in bundled v1.9.5 (`internal/config/auto_permission_prompt.go:30`, sentence at `internal/config/auto_permission_prompt.go:96`). First-word interpreter forms (`.venv/bin/python script.py`) route through the structured effect-verification path, not this prose: `classifyInterpreterExecution` basename-matches at `internal/agent/permissions.go:6140`.

The deterministic layer stays narrower than the chat prose's path-only trust, but it is no longer limited to eight `runnerSafeTools` names. Known formatter/linter/type-checker commands statically allow through `bashSubcommandAllow` / `runnerSafeTools` (`internal/agent/permissions.go:5760`) only after `developmentToolArgsAllowed` (`internal/agent/permissions.go:5401`) accepts their arguments. Project `node_modules/.bin` forms must also pass canonical containment through `shimUnderWorkDir` (`internal/agent/permissions.go:5280`) and `nodeModulesBinTool` (`internal/agent/permissions.go:5323`). Ordinary test runners keep their prior static allowances, but escape flags fall through to Jev or a human. Other dependency binaries remain judge-mediated. See [Auto-Permission Enforced Categories](../concepts/auto-permission-enforced-categories.md#hybrid-development-tool-auto-allow-user-decision-2026-11-25).

## 4. WHY ACCEPTED

Everyday tooling (`tsc`/`eslint`/`pytest`/`go run`) must not ping the human on every invocation. The deterministic layer already trusts project script/test runners such as `make`, `npm run`, and `cargo run`; the HYBRID policy extends that trust only to named development tools behind `developmentToolArgsAllowed`.

The layers are deliberately not identical. The bundled chat prose human-gates package-manager `run`/`test`/`exec`/`dlx`, while the deterministic layer retains its pre-existing project script/test allowances and named-tool static allows. The trust boundary is explicit install/remove/link/publish behavior and package/plugin/custom-code selection, not the mere presence of a binary in a dependency directory.

## 5. GOTCHA / RULES for maintainers

- Never auto-allow an unfamiliar binary by path alone in **CODE**. The deterministic allowlist stays name-based, with canonical containment for project `node_modules/.bin` forms and an argument gate for development tools.
- Keep the layer attribution explicit: the dependency-binary **action** rule is bundled chat-judge prose; Jev's rule is category-based; the deterministic layer statically allows only named tools and ordinary test forms.
- When policy changes, update each affected layer and its regression tests, but do not claim prose/code parity where the shipped policy is intentionally hybrid.
- Judge-ALLOW ≠ confinement. Always pair prose changes with a code-layer security review; sandbox mode is write-integrity only, and network egress remains open.

## 6. PATH-QUALIFIED BINARIES AND USER ALLOW RULES (2026-09-28)

Allow rules (`permissions.bash.prefixes`) match the command's first word **literally**, so a `vp` rule never covered `./node_modules/.bin/vp` or `~/.local/share/vite-plus/0.3.2/bin/vp`. Those calls fell through to the Jev judge, which scored them 0.75–0.83, below the confidence floor, and a human was asked every time. Deny rules already see through paths (`effectiveCommandWords`).

- **Code:** `trustedToolBasename` (`internal/agent/permissions.go:5350`) lets a path-qualified word inherit its **bare-name allow rule** only when it sits in a trusted location: `node_modules/.bin` inside the project (`nodeModulesBinTool`, canonical containment), or a `trustedToolDirPatterns` dir under `$HOME` (`.vite-plus/bin`, `.local/share/vite-plus/*/bin`). It runs after the harmful check and the literal/granular rule lookups, but the development-tool argument gate runs earlier (`internal/agent/permissions.go:5401`), so a rejected escape flag still asks before a saved rule can be inherited. It grants nothing without an existing user rule, so rule 1 above still holds. Test: `TestAllowRuleMatchesPathQualifiedTrustedTool`.
- **Jev:** the TypeSafe state now carries `allowed_command_prefixes` (the user's bash allow rules, `PermissionManager.BashAllowedPrefixes`), and `typesafeJudgeInstructions` tells Jev that a command invoking one of them **by exact name** is user-trusted, and that a same-named binary called by a path is not covered (trusted paths are already resolved in code before Jev runs, so only untrusted ones reach it). Deny rules stay in `banned_command_prefixes`. Test: `TestConsultPermissionModelTypesafeAllowedPrefixesInState`.
- **`git branch`:** it both lists and mutates, so it is not in `bashSubcommandAllow`. `isReadOnlyGitBranchForm` auto-allows only listing forms: no args, listing flags (`-v/-vv/-a/-r/--show-current/--color/--column/--sort/--format`), or list mode (`-l/--list/--contains/--merged/--points-at`) with patterns. Unknown flags and a positional outside list mode (branch creation) fail closed. Test: `TestGitBranchListingFormsAutoAllow`.
