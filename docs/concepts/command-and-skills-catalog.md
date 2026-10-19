---
type: Catalog
title: "Discoverable Command & Skills Catalog (JEV reference)"
description: Structured reference list of all available ocode slash commands, built-in skills, and plugin capabilities intended for JEV (TypeSafe/relevance judge) discovery and doc-search indexing.
okf_version: "0.1"
tags: [jev, discovery, skills, commands, reference, catalog]
timestamp: 2026-10-19T00:00:00Z
resource: internal/commands/loader.go, skills/
---
# Discoverable Command & Skills Catalog

This catalog is intended for the TypeSafe/JEV relevance judge (`discovery-typesafe-judge.md`) and the doc-search relevance judge (`doc-search-relevance-judge.md`). It lists commands, skills, and tool categories available to the agent.

## Commands (slash / built-in)

Commands are loaded via `internal/commands/loader.go`. Key categories:

- `/goal` — session goal condition loop
- `/search` — in-session transcript search (`ctrl+f` equivalent)
- `/new`, `/clear`, `/exit`, `/quit` — session lifecycle
- `/reset-id` — session rekeying (`docs/concepts/session-rekey-reset-id.md`)
- `/docs` — knowledge bundle (`docs/index.md`)
- `/discover` — discovery/relevance (`concepts/discovery-typesafe-judge.md`)
- `/agent`, `/agents` — agent session management
- `/memory` — persistent memory scope (`skills/ocode-mem`)
- `/sandbox` — permission sandbox mode (`docs/concepts/sandbox-permission-mode.md`)
- `/compact` — conversation compaction (`concepts/compaction-cancellation.md`)
- `/changes`, `/git` — session change tracking (`docs/changes-tab.md`)
- `/review-changes` — change-set review (`skills/code-review`)
- `/setup-metrics`, `/plan-okrs`, `/write-prd`, etc. — strategic/planning skills

Full command list is derived from `skills/*/SKILL.md` and `internal/commands/loader.go`.

## Skills (installed / bundled)

Installed skills count (as of this reference): ~174 (`skills/*/SKILL.md`). Key bundles:

- `ocode-tui` — TUI navigation, mouse selection, click-safe chrome
- `ocode-web` — React SPA wiring, streaming pipeline, state
- `ocode-usage` — this catalog reference, installation/config
- `ocode-remote-ssh` — remote SSH project handling
- `superpowers:brainstorming` — bounded creative exploration
- `brainstorming` — product/experiment ideation
- `systematic-debugging` — structured bug investigation
- `test-driven-development` — TDD cycle enforcement
- `using-superpowers` — skill invocation protocol
- `writing-plans` — plan-mode spec drafting
- `find-skills` — discover additional agent capabilities
- `docx`, `pdf`, `pptx` — document editing/review
- `agents-sdk`, `cf-cli`, `cloudflare-one` — platform/operator tools
- `gws-*` — Google Workspace automation suite
- `kaizen/*` — tuning skills per model/task

## Plugin capabilities

Plugins (`docs/plugins.md`) extend commands and skills. Plugin types:

- Custom `slash` commands via `.claude-plugin/plugin.json` or `plugin.json`
- Custom tools (MCP server registrations)
- Session hooks (`SessionStart`, `OnMessage`)
- Discovery attachments (`discovery` plugin type)

Plugin namespace rules: one plugin per name, ocode plugins shadow Claude Code plugins; `LoadAllPluginsForProject` provides the full list including disabled defaults.

## JEV/indexing notes

- This file is part of the OKF v0.1 bundle (`docs/index.md`).
- Discovery relevance (`judgeDiscoveryCandidates`) uses `docs/` pages with `tags` matching the request subject; this catalog provides a broad-match reference for any command or skill query.
- Doc-search (`judgeDocSearchResults`) uses the same tag-based relevance; include this file when searching for "commands" or "skills" to get the full list rather than a single skill page.
- Auto-injection (`concepts/discovery-typesafe-judge.md`) selects skill bodies only; this catalog is a reference index, not a body to inject.
