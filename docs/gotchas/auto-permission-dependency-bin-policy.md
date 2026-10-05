---
type: Gotcha
title: Auto-Permission Dependency Binaries — Policy Decision
description: Deliberate security-policy expansion in bundled auto-permission gatekeeper prompt v1.9.x that auto-allows direct invocation of project/toolchain dependency binaries, with accepted residual risk and maintainer rules.
tags:
  - auto-permission
  - gotcha
  - security
  - policy-decision
timestamp: 2026-09-28T00:00:00Z
---
## 1. THE POLICY

The bundled gatekeeper prose (`internal/config/auto_permission_prompt.go:69`) directs the LLM permission judge to **ALLOW** direct invocation of project/toolchain dependency binaries — `node_modules/.bin`, `node_modules/**/bin`, `.venv/bin|venv/bin|env/bin`, pyenv shims, `go run`/`go tool`/GOPATH/bin, project `./bin`, `cargo` + `~/.cargo/bin`, Rust `target/debug|release`, PHP `vendor/bin`, Ruby `bundle exec`, Java/Kotlin `gradlew`/`mvnw`, `.NET` local tools — judging the **ACTION** (build/test/lint/format/codegen/local file processing → ALLOW) not the binary's location. DENY if the action writes sensitive paths (`.git/`), exfiltrates, or is destructive.

## 2. THREAT MODEL / RESIDUAL RISK (accepted)

(a) The executed file is a tiny shim/console-script wrapper — the deterministic backstop (`Agent.detectExecutedCustomScripts` in `internal/agent/script_detection.go:237` + the truncation guard in `Agent.verifyAutoGrant`, `internal/agent/agent.go:4300`) inspects only the wrapper; the imported payload module or compiled binary is opaque to the judge.

(b) Judge-ALLOW removes the human prompt but adds **no confinement** — the write-wall exists only in sandbox mode; network egress is always open (sandbox is write-integrity only — cite `architecture/shell-sandbox-integrity-only-mode.md`), so a judged-allowed binary can read global files and POST anywhere.

(c) A malicious dependency can ship a plausibly-named binary that passes the "judge the action" test.

## 3. MITIGATIONS THAT MAKE IT DELIBERATE

Package installs are human-gated (npm/pnpm/bun `install`/`ci`/`dlx`/`exec` require human approval in the same prose — `auto_permission_prompt.go:63-74`). The v1.9.1 fail-closed rule: *"If you cannot establish what the command does… require human approval; a familiar-looking name proves nothing"* (version bump to `BundledAutoPermissionPromptVersion = "1.9.1"`). First-word interpreter forms (`.venv/bin/python script.py`) route through the structured effect-verification path, not the prose: `classifyInterpreterExecution` basename-matches in `internal/agent/permissions.go:4147`. The code layer stays **narrower** than the prose: auto-allow only for runnerSafeTools basenames `{tsc,tsgo,eslint,prettier,biome,vitest,jest,stylelint}` under canonical workDir containment — `runnerSafeTools` (list at `internal/agent/permissions.go:3860`) with `shimUnderWorkDir` at `internal/agent/permissions.go:3746` and `nodeModulesBinTool` at `internal/agent/permissions.go:3789` — the prose expansion is judge-mediated only.

## 4. WHY ACCEPTED

Everyday tooling (`tsc`/`eslint`/`pytest`/`go run`) must not ping the human on every invocation; the same trust model as the code layer's pre-existing `make`/`npm run`/`cargo run` auto-allows (project-trusted script runners). Installs are the trust boundary.

## 5. GOTCHA / RULES for maintainers

- Never auto-allow an unfamiliar binary by path alone in **CODE** (the code layer's basename+containment gate must stay narrow).
- When tightening, tighten **prose and code in one changelist**; neither should outpace the other.
- Judge-ALLOW ≠ confinement in any future prose edits — always pair prose changes with code-layer reviews.

## 6. PATH-QUALIFIED BINARIES AND USER ALLOW RULES (2026-09-28)

Allow rules (`permissions.bash.prefixes`) match the command's first word **literally**, so a `vp` rule never covered `./node_modules/.bin/vp` or `~/.local/share/vite-plus/0.3.2/bin/vp`. Those calls fell through to the Jev judge, which scored them 0.75–0.83, below the confidence floor, and a human was asked every time. Deny rules already see through paths (`effectiveCommandWords`).

- **Code:** `trustedToolBasename` (`internal/agent/permissions.go`) lets a path-qualified word inherit its **bare-name allow rule** only when it sits in a trusted location: `node_modules/.bin` inside the project (`nodeModulesBinTool`, canonical containment), or a `trustedToolDirPatterns` dir under `$HOME` (`.vite-plus/bin`, `.local/share/vite-plus/*/bin`). It runs after the harmful check and after the literal rule lookup, and it grants nothing without an existing user rule, so rule 5 above still holds. Test: `TestAllowRuleMatchesPathQualifiedTrustedTool`.
- **Jev:** the TypeSafe state now carries `allowed_command_prefixes` (the user's bash allow rules, `PermissionManager.BashAllowedPrefixes`), and `typesafeJudgeInstructions` tells Jev that a command invoking one of them **by exact name** is user-trusted, and that a same-named binary called by a path is not covered (trusted paths are already resolved in code before Jev runs, so only untrusted ones reach it). Deny rules stay in `banned_command_prefixes`. Test: `TestConsultPermissionModelTypesafeAllowedPrefixesInState`.
- **`git branch`:** it both lists and mutates, so it is not in `bashSubcommandAllow`. `isReadOnlyGitBranchForm` auto-allows only listing forms: no args, listing flags (`-v/-vv/-a/-r/--show-current/--color/--column/--sort/--format`), or list mode (`-l/--list/--contains/--merged/--points-at`) with patterns. Unknown flags and a positional outside list mode (branch creation) fail closed. Test: `TestGitBranchListingFormsAutoAllow`.
