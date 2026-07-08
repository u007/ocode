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

