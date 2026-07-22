---
okf_version: 0.1
---

# Concepts

- [File-Edit Snapshot & Undo Mechanism](file-edit-snapshot.md) - ocode takes a per-agent file snapshot before every write/edit/patch and provides an undo_file_change tool to revert by tool_call_id.
- [Changes Tab](changes-tab.md) - Per-session TUI tab listing files added or edited by the current chat session, with unified diffs and undo.
- [Knowledge Bundle System](knowledge-bundle.md) - Internal architecture of the OKF v0.1 knowledge bundle — bundle detection, scanning, frontmatter parsing, doc search, .okfignore exclusion, and store CRUD with documented edge cases and gotchas.
- [Plugin System](plugins.md) - Overview of ocode's plugin system: plugin.json manifest format, custom tools, slash commands, MCP server registration, and plugin lifecycle management.
- [Scheduled Jobs / Cron Dispatch](scheduled-jobs.md) - Persistent, disk-backed cron engine + headless agent dispatcher for ocode, modeled on nanobot's CronService and Claude Code's CronCreate/CronList/CronDelete semantics.
- [Session Title Generation & UI Update Root Cause Analysis](title-generation-analysis.md) - Root cause analysis of session title delay/mismatch between generation and UI rendering, covering regex anchoring, Anthropic thinking blocks, and rendering cycle timing.
- [Using ocode with Zed](zed.md) - Setup guide and feature matrix for integrating ocode with the Zed editor via ACP (Agent Client Protocol).
- [Zed-compatible ACP Mode Specification](acp-zed-spec.md) - Approved architecture spec for implementing 'ocode acp' using the Agent Client Protocol, enabling ocode as a Zed editor agent.

# architecture

- [Sidebar TUI/Web Parity Gaps](architecture/sidebar-tui-parity-gaps.md) - Gap analysis of web frontend sidebar features missing relative to the TUI sidebar, covering backend fields not consumed and missing TS types.

# gotchas

- [ChatPanel Autoscroll Bounce/Freeze](gotchas/autoscroll-bounce.md) - Root cause analysis of the autoscroll bounce/freeze bug: smooth-scrolling every live token without at-bottom state tracking causes competing animations that lock up the scroll position.
- [Skill Tool Test Fixture Gap — expectedBuiltinTools Missing load_skill](gotchas/skill-tool-test-fixture-gap.md) - expectedBuiltinTools in tool_test.go only lists "skill" but InitBuiltinTools also registers "load_skill" as a second alias, causing a stale test failure.
- [Subagent Feedback-Loop Guard (task tool)](gotchas/subagent-feedback-loop-guard.md) - The task/subagent dispatch refuses consecutive same-type launches without new user input to break runaway feedback loops; vary the agent type or wait for user input.

# okf

- [okf/_schema/scorecard.template.md](okf/_schema/scorecard.template.md)
- [okf/conduct/scores/deepseek-v4-flash.md](okf/conduct/scores/deepseek-v4-flash.md)
- [okf/conduct/scores/tencent__hy3.md](okf/conduct/scores/tencent__hy3.md)
- [okf/conduct/scores/tencent__hy3.with-skill.md](okf/conduct/scores/tencent__hy3.with-skill.md)
- [okf/csharp/scores/tencent__hy3.md](okf/csharp/scores/tencent__hy3.md)
- [okf/dotnet/scores/tencent__hy3.md](okf/dotnet/scores/tencent__hy3.md)
- [okf/elixir/derived/elixir.tencent__hy3.SKILL.md](okf/elixir/derived/elixir.tencent__hy3.SKILL.md) - Corrective Elixir knowledge for tencent/hy3, targeting the pattern-matching gaps this model showed on the closed-book elixir benchmark (guard restrictions, the FunctionClauseError / MatchError failure modes, and the "match is not assignment" framing).

- [okf/elixir/scores/tencent__hy3.md](okf/elixir/scores/tencent__hy3.md)
- [okf/elixir/scores/tencent__hy3.with-skill.md](okf/elixir/scores/tencent__hy3.with-skill.md)
- [okf/golang/scores/tencent__hy3.md](okf/golang/scores/tencent__hy3.md)
- [okf/nestjs/scores/tencent__hy3.md](okf/nestjs/scores/tencent__hy3.md)
- [okf/nextjs/scores/tencent__hy3.md](okf/nextjs/scores/tencent__hy3.md)
- [okf/php/scores/tencent__hy3.md](okf/php/scores/tencent__hy3.md)
- [okf/python/scores/tencent__hy3.md](okf/python/scores/tencent__hy3.md)
- [okf/react/derived/react.claude-opus-4-8.SKILL.md](okf/react/derived/react.claude-opus-4-8.SKILL.md) - Corrective React guidance for the exact areas claude-opus-4-8 tests weak on (RSC boundaries, Suspense, refs). Loaded only in React repos when this exact model is active.
- [okf/react/scores/claude-opus-4-8.md](okf/react/scores/claude-opus-4-8.md)
- [okf/react/scores/tencent__hy3.md](okf/react/scores/tencent__hy3.md)
- [okf/ror/scores/tencent__hy3.md](okf/ror/scores/tencent__hy3.md)
- [okf/ruby/scores/tencent__hy3.md](okf/ruby/scores/tencent__hy3.md)
- [okf/rust/scores/tencent__hy3.md](okf/rust/scores/tencent__hy3.md)
- [okf/tanstack/scores/tencent__hy3.md](okf/tanstack/scores/tencent__hy3.md)
- [okf/vbnet/scores/tencent__hy3.md](okf/vbnet/scores/tencent__hy3.md)

# Unclassified

- [HOW-TO-EVALUATE.md](okf/HOW-TO-EVALUATE.md)
- [README.md](okf/README.md)
- [conduct.md](okf/_prompts/conduct.md)
- [csharp.md](okf/_prompts/csharp.md)
- [dotnet.md](okf/_prompts/dotnet.md)
- [elixir.md](okf/_prompts/elixir.md)
- [golang.md](okf/_prompts/golang.md)
- [nestjs.md](okf/_prompts/nestjs.md)
- [nextjs.md](okf/_prompts/nextjs.md)
- [php.md](okf/_prompts/php.md)
- [python.md](okf/_prompts/python.md)
- [react.md](okf/_prompts/react.md)
- [ror.md](okf/_prompts/ror.md)
- [ruby.md](okf/_prompts/ruby.md)
- [rust.md](okf/_prompts/rust.md)
- [tanstack.md](okf/_prompts/tanstack.md)
- [vbnet.md](okf/_prompts/vbnet.md)
- [question-format.md](okf/_schema/question-format.md)
- [rubric-guide.md](okf/_schema/rubric-guide.md)
- [stack-detection.md](okf/_schema/stack-detection.md)
- [deepseek-v4-flash.spotcheck.md](okf/conduct/answers/deepseek-v4-flash.spotcheck.md)
- [tencent__hy3.digest-spotcheck.md](okf/conduct/answers/tencent__hy3.digest-spotcheck.md)
- [tencent__hy3.md](okf/conduct/answers/tencent__hy3.md)
- [tencent__hy3.with-skill.md](okf/conduct/answers/tencent__hy3.with-skill.md)
- [conduct.deepseek-v4-flash.SKILL.md](okf/conduct/derived/conduct.deepseek-v4-flash.SKILL.md)
- [conduct.tencent__hy3.SKILL.md](okf/conduct/derived/conduct.tencent__hy3.SKILL.md)
- [questions.md](okf/conduct/questions.md)
- [tencent__hy3.md](okf/csharp/answers/tencent__hy3.md)
- [questions.md](okf/csharp/questions.md)
- [tencent__hy3.md](okf/dotnet/answers/tencent__hy3.md)
- [questions.md](okf/dotnet/questions.md)
- [tencent__hy3.md](okf/elixir/answers/tencent__hy3.md)
- [tencent__hy3.with-skill.md](okf/elixir/answers/tencent__hy3.with-skill.md)
- [questions.md](okf/elixir/questions.md)
- [tencent__hy3.md](okf/golang/answers/tencent__hy3.md)
- [questions.md](okf/golang/questions.md)
- [tencent__hy3.md](okf/nestjs/answers/tencent__hy3.md)
- [questions.md](okf/nestjs/questions.md)
- [tencent__hy3.md](okf/nextjs/answers/tencent__hy3.md)
- [questions.md](okf/nextjs/questions.md)
- [tencent__hy3.md](okf/php/answers/tencent__hy3.md)
- [questions.md](okf/php/questions.md)
- [tencent__hy3.md](okf/python/answers/tencent__hy3.md)
- [questions.md](okf/python/questions.md)
- [tencent__hy3.md](okf/react/answers/tencent__hy3.md)
- [questions.md](okf/react/questions.md)
- [README.md](okf/react/scores/README.md)
- [tencent__hy3.md](okf/ror/answers/tencent__hy3.md)
- [questions.md](okf/ror/questions.md)
- [tencent__hy3.md](okf/ruby/answers/tencent__hy3.md)
- [questions.md](okf/ruby/questions.md)
- [tencent__hy3.md](okf/rust/answers/tencent__hy3.md)
- [questions.md](okf/rust/questions.md)
- [tencent__hy3.md](okf/tanstack/answers/tencent__hy3.md)
- [questions.md](okf/tanstack/questions.md)
- [tencent__hy3.md](okf/vbnet/answers/tencent__hy3.md)
- [questions.md](okf/vbnet/questions.md)
- [2026-07-21-session-storage-ojsonl.md](superpowers/plans/2026-07-21-session-storage-ojsonl.md)
- [2026-07-08-global-runtime-artifacts-design.md](superpowers/specs/2026-07-08-global-runtime-artifacts-design.md)
- [2026-07-11-live-preview-design.md](superpowers/specs/2026-07-11-live-preview-design.md)
- [2026-07-11-model-stack-benchmark-design.md](superpowers/specs/2026-07-11-model-stack-benchmark-design.md)
- [2026-07-14-tool-result-smart-sizing-design.md](superpowers/specs/2026-07-14-tool-result-smart-sizing-design.md)
- [2026-07-21-session-storage-ojsonl-design.md](superpowers/specs/2026-07-21-session-storage-ojsonl-design.md)
- [2026-07-22-changes-tab-design.md](superpowers/specs/2026-07-22-changes-tab-design.md)
- [telegram-bot.md](telegram-bot.md)

