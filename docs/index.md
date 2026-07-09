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

# Unclassified

- [2026-07-08-global-runtime-artifacts-design.md](superpowers/specs/2026-07-08-global-runtime-artifacts-design.md)

