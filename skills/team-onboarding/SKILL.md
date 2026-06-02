---
name: team-onboarding
description: Analyzes the ocode codebase and generates comprehensive team onboarding documentation covering architecture, build/test/run commands, development workflow, configuration, providers, TUI, agent system, and conventions. Use when onboarding new team members, generating CLAUDE.md updates, or documenting team workflows.
when_to_use: When the user asks to generate or update team onboarding documentation, analyze codebase conventions, document build/run/test commands, or create a team ramp-up guide for this ocode project. Also triggered by: "onboard", "onboarding", "ramp up", "new team member", "team guide", "document workflows", "how does ocode work", "codebase overview".
---

# ocode Team Onboarding Generator

## Overview

This skill analyzes the ocode codebase and generates a comprehensive team onboarding guide. It:
- Identifies the project's architecture and key modules
- Documents build, test, and run commands
- Captures development workflow and conventions
- Maps the LLM provider and configuration system
- Records agent/tool/TUI architecture
- Produces structured documentation suitable for new team members

The output can be written to `CLAUDE.md`, `TEAM_ONBOARDING.md`, or printed to the session.

---

## Step 1: Codebase Discovery

First, run the discovery commands to gather facts about this ocode codebase.

### 1.1 Project identity

!`cat go.mod | head -3`
!`git log --oneline -5`
!`git remote -v | head -5`

### 1.2 Directory structure

!`find . -maxdepth 3 -type d -not -path './.git/*' -not -path './.opencode/*' -not -path './web/node_modules/*' -not -path './web/dist/*' -not -path './.claude/*' -not -path './internal/tui/.opencode/*' | sort`

### 1.3 Build system

!`grep -E '^(build|test|run|lint|fmt|check):' Makefile 2>/dev/null || echo "No Makefile targets found"`
!`ls -la *.go go.mod 2>/dev/null`

### 1.4 Configuration

!`find . -maxdepth 3 -name '*.json' -not -path './.opencode/*' -not -path './.git/*' -not -path './web/node_modules/*' | sort`

---

## Step 2: Architecture Analysis

### 2.1 Entry points

!`head -50 main.go`

### 2.2 Package map and responsibilities

Document each internal package with its role:

!`for pkg in internal/*/; do name=$(basename "$pkg"); desc=$(head -5 "$pkg"/*.go 2>/dev/null | grep -m1 "package\|description\|// " | head -1); echo "- **$name**: $desc"; done`

### 2.3 LLM Providers

!`grep -n 'Provider\s*=' internal/agent/client.go | head -20`
!`grep -n 'provider\|Provider' internal/auth/*.go | head -20`
!`cat internal/agent/small_model.go`

### 2.4 Agent system

!`cat internal/agent/registry.go`
!`cat internal/agent/agent_loader.go | head -40`

### 2.5 Tools

!`grep -n 'func.*Tool\b' internal/tool/tool.go | head -20`
!`ls internal/tool/*.go`

### 2.6 TUI architecture

!`head -80 internal/tui/tui.go`
!`grep -n 'func.*View\(\)\|func.*Update\|func.*renderContent\|func.*newModel' internal/tui/model.go | head -20`

---

## Step 3: Workflow & Convention Discovery

### 3.1 Testing patterns

!`find . -name '*_test.go' -not -path './.opencode/*' -not -path './web/node_modules/*' | head -20`
!`grep -rn 'func Test' internal/ --include='*_test.go' -l | head -10`

### 3.2 Hooks system

!`ls internal/hooks/`
!`cat internal/hooks/*.go | head -60`

### 3.3 Session system

!`ls internal/session/`
!`head -30 internal/session/*.go 2>/dev/null | head -30`

### 3.4 Plugin system

!`ls internal/plugins/`

### 3.5 Model config

!`head -60 internal/config/ocodeconfig.go`

---

## Step 4: Generate Onboarding Documentation

After gathering the data above, synthesize it into structured onboarding documentation covering:

### 4.1 What to include

- **Project Overview**: What ocode is (a Go TUI coding agent using Charm Bubble Tea)
- **Quick Start**: Build and run instructions
- **Architecture Overview**: Package map and data flow (main → TUI → agent → LLM providers → tools)
- **Configuration**: Config file location, structure, environment variables
- **LLM Providers**: Supported providers (OpenAI, Anthropic, Google, Z.AI, Alibaba, DeepSeek via opencode-go)
- **Agent System**: The agent registry, modes (build/plan/review/debug/docs), sub-agents
- **Tools**: Available tools (read, write, edit, bash, glob, grep, lsp, websearch, webfetch, skill, agent, etc.)
- **TUI Architecture**: Layout, mouse/selection, themes, keyboard shortcuts
- **Skills System**: How bundled skills work (SKILL.md frontmatter, install/upgrade lifecycle)
- **Testing**: Test patterns, running tests
- **Development Workflow**: Git workflow, code review process, conventions
- **Hooks & Plugins**: Hook lifecycle, plugin system

### 4.2 Output location

Write the generated documentation:
- If a `CLAUDE.md` exists and has changed: write to `TEAM_ONBOARDING.md` in the project root
- If no `CLAUDE.md` exists: offer to write one, or write to `TEAM_ONBOARDING.md`
- Always present a summary in the chat first for review before writing files

---

## Step 5: Review Checklist

Before finalizing, verify:

- [ ] Build command is correct (`go build ./...` or similar)
- [ ] Test command is correct (`go test ./...` or similar)
- [ ] Run command is correct (`go run .` or `ocode` without args)
- [ ] All provider names and required auth are documented
- [ ] Agent modes and their purposes are listed
- [ ] TUI layout and key shortcuts are included
- [ ] Skill system (install/upgrade) is explained
- [ ] Package dependencies and architecture are mapped
- [ ] Git workflow and conventions are covered
- [ ] No secrets or sensitive paths in the output

---

## Output Format

The generated onboarding document should use this structure:

```markdown
# ocode Team Onboarding Guide

## Quick Start
[build, run, test]

## Project Architecture
[package map, data flow]

## Configuration
[config file, env vars]

## LLM Providers
[supported providers, auth setup]

## Agent & Tool System
[agents, modes, tools]

## TUI
[layout, themes, shortcuts, mouse]

## Skills
[bundled skills, custom skills]

## Development Workflow
[git, testing, review, conventions]

## Hooks & Plugins
[hook events, plugin setup]
```
