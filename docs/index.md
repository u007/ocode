---
okf_version: 0.1
---

# Concepts

- [File-Edit Snapshot & Undo Mechanism](file-edit-snapshot.md) - ocode takes a per-agent file snapshot before every write/edit/patch and provides an undo_file_change tool to revert by tool_call_id.
- [Knowledge Bundle System](knowledge-bundle.md) - Internal architecture of the OKF v0.1 knowledge bundle — bundle detection, scanning, frontmatter parsing, doc search, .okfignore exclusion, and store CRUD with documented edge cases and gotchas.
- [Plugin System](plugins.md) - Overview of ocode's plugin system: plugin.json manifest format, custom tools, slash commands, MCP server registration, and plugin lifecycle management.
- [Session Title Generation & UI Update Root Cause Analysis](title-generation-analysis.md) - Root cause analysis of session title delay/mismatch between generation and UI rendering, covering regex anchoring, Anthropic thinking blocks, and rendering cycle timing.
- [Using ocode with Zed](zed.md) - Setup guide and feature matrix for integrating ocode with the Zed editor via ACP (Agent Client Protocol).
- [Zed-compatible ACP Mode Specification](acp-zed-spec.md) - Approved architecture spec for implementing 'ocode acp' using the Agent Client Protocol, enabling ocode as a Zed editor agent.

# architecture

- [Sidebar TUI/Web Parity Gaps](architecture/sidebar-tui-parity-gaps.md) - Gap analysis of web frontend sidebar features missing relative to the TUI sidebar, covering backend fields not consumed and missing TS types.

# gotchas

- [ChatPanel Autoscroll Bounce/Freeze](gotchas/autoscroll-bounce.md) - Root cause analysis of the autoscroll bounce/freeze bug: smooth-scrolling every live token without at-bottom state tracking causes competing animations that lock up the scroll position.
- [Subagent Feedback-Loop Guard (task tool)](gotchas/subagent-feedback-loop-guard.md) - The task/subagent dispatch refuses consecutive same-type launches without new user input to break runaway feedback loops; vary the agent type or wait for user input.

# okf

- [okf/_schema/scorecard.template.md](okf/_schema/scorecard.template.md)
- [okf/golang/scores/novita-ai__tencent__hy3.md](okf/golang/scores/novita-ai__tencent__hy3.md)
- [okf/react/derived/react.claude-opus-4-8.SKILL.md](okf/react/derived/react.claude-opus-4-8.SKILL.md) - Corrective React guidance for the exact areas claude-opus-4-8 tests weak on (RSC boundaries, Suspense, refs). Loaded only in React repos when this exact model is active.
- [okf/react/scores/claude-opus-4-8.md](okf/react/scores/claude-opus-4-8.md)

# Unclassified

- [HOW-TO-EVALUATE.md](okf/HOW-TO-EVALUATE.md)
- [README.md](okf/README.md)
- [question-format.md](okf/_schema/question-format.md)
- [rubric-guide.md](okf/_schema/rubric-guide.md)
- [stack-detection.md](okf/_schema/stack-detection.md)
- [questions.md](okf/conduct/questions.md)
- [questions.md](okf/golang/questions.md)
- [novita-ai__tencent__hy3.md](okf/nextjs/answers/novita-ai__tencent__hy3.md)
- [questions.md](okf/nextjs/questions.md)
- [novita-ai__tencent__hy3.md](okf/react/answers/novita-ai__tencent__hy3.md)
- [questions.md](okf/react/questions.md)
- [README.md](okf/react/scores/README.md)
- [novita-ai__tencent__hy3.md](okf/rust/answers/novita-ai__tencent__hy3.md)
- [questions.md](okf/rust/questions.md)
- [novita-ai__tencent__hy3.md](okf/tanstack/answers/novita-ai__tencent__hy3.md)
- [questions.md](okf/tanstack/questions.md)
- [2026-07-08-global-runtime-artifacts-design.md](superpowers/specs/2026-07-08-global-runtime-artifacts-design.md)
- [2026-07-11-model-stack-benchmark-design.md](superpowers/specs/2026-07-11-model-stack-benchmark-design.md)

