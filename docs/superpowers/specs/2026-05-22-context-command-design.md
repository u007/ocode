# /context Command Design

## Overview

A `/context` slash command that acts as a token budget inspector — showing everything that contributes tokens to the base prompt, plus session message usage. Similar in spirit to Claude Code's `/context`, but focused on surfacing token costs per source.

## Output Format

Single formatted message appended to the chat panel (same pattern as `/permissions`, `/mcp`). No subcommands.

```
Context Budget
══════════════════════════════════════

Base Prompt
  Mode (BUILD)                   ~180 tok
  Ambient files
    AGENTS.md                    ~320 tok
    CLAUDE.md                  ~1,204 tok
  Plugin instructions
    superpowers                  ~640 tok
  Subtotal                     ~2,344 tok

Tools (injected every request)
  Built-in (23 tools)            ~890 tok
  MCP: claude_ai_Gmail  12 tools · ~1,240 tok
  MCP: context7          2 tools · ~180 tok
  MCP: mempalace        28 tools · ~3,100 tok
  Subtotal                     ~5,410 tok

Injected per request             ~7,754 tok

Skills (on-demand, not pre-injected)
  brainstorming                ~8,420 tok
  feature-dev                  ~3,100 tok
  ... +24 more (26 total)    ~142k tok available

Session Messages
  Context window         12,450 / 200,000 (6%)
  Session total                 23,100 tok
```

All token counts are labeled `~N tok` to signal they are estimates.

## Token Estimation

No external tokenizer. Use `len(text) / 4` as approximation throughout. This is applied to:
- File content (ambient files, plugin instructions)
- JSON-marshalled tool definitions (built-in + MCP)
- Mode system prompt string
- Skill SKILL.md content

## Data Sources

| Section | Source |
|---|---|
| Mode system prompt | `m.agent.Mode().SystemPrompt()` |
| Ambient files | Read individually: `AGENTS.md`, `CLAUDE.md`, `.cursorrules`, `.opencode/rules/*.md` |
| Plugin instructions | `plugins.LoadPlugins()` — per-plugin `Instructions` field |
| Built-in tool defs | `m.agent.GetToolDefinitions()` — tools not in `m.agent.MCPToolNames()` |
| MCP tool defs | `m.agent.GetToolDefinitions()` — grouped by server prefix (`servername_toolname`) |
| Skills | `skill.LoadSkills()` — `Content` field per skill |
| Context window | `latestPromptTokens(m.messages)` + `modelContextWindow(modelName)` |
| Session total | `m.sessionTelemetry.usedTokens()` |

## MCP Grouping

MCP tool names follow the pattern `servername_toolname` (set in `mcp/client.go` line 367: `c.name + "_" + t.Name`). Server names can themselves contain underscores (e.g. `claude_ai_Gmail`), so splitting on the first `_` is incorrect.

**Algorithm:** For each tool definition returned by `GetToolDefinitions()`, check if its name is in the MCP tool name set. If so, determine its server by iterating the known MCP server names (from `m.config.MCP`) and finding the one whose name is a prefix of the tool name (i.e. `strings.HasPrefix(toolName, serverName+"_")`). Group and sum JSON-marshalled definition sizes per matched server. Tools that don't match any server prefix fall through to built-in.

## Skills Caveat

Skills are NOT pre-injected into the base prompt. The LLM only sees a minimal `skill` tool schema. Skill SKILL.md content is loaded on-demand when the LLM calls the `skill` tool. The section is labeled "on-demand, not pre-injected" and shows available skills with their content size as a reference.

## Implementation

**Files to change:**
- `internal/tui/commands.go` — add `{name: "/context", help: "Show context window token budget", handler: runContextCmd}` to `commandSpecs`
- `internal/tui/model.go` — add `handleContextCmd(*model, []string) tea.Cmd` method

**Pattern:** Sync handler (consistent with `/permissions`, `/mcp`, `/skills`). File reads and JSON marshalling are fast enough that blocking is not a concern. The handler reads files, loads skills, marshals tool defs, formats the string, then appends `message{role: roleAssistant, text: output}` and returns nil.

**MCP grouping algorithm:**
1. Get all tool definitions from `m.agent.GetToolDefinitions()`
2. Get MCP tool name set from `m.agent.MCPToolNames()`
3. Get server names from `m.config.MCP` (map keys)
4. For each definition: if name is in MCP set, find matching server by `strings.HasPrefix(toolName, serverName+"_")` over all server names
5. Group and sum `len(json.Marshal(def))/4` per server; unmatched MCP tools count as built-in

**Nil guard:** If `m.agent == nil`, append a `statusMsg{text: "No agent configured."}` and return nil — same pattern as `/permissions`.

**Skills truncation:** Show up to 5 skills by name+size. If more exist, append `... +N more (M total) · ~Xk tok available`. If 0 skills, show `(none found)`.

**`latestPromptTokens`:** Returns the `PromptTokens` value from the most recent `agent.Message` in `m.messages` that has non-nil `Usage.PromptTokens`. This reflects the actual context window used on the last LLM call.

**No new exported methods needed** — all required data is accessible via existing agent/skill/plugin/config APIs.

## LLM Isolation

The output message MUST NOT have a `raw` field set. In `model.go`, only messages where `msg.raw != nil` are included in the agent message list sent to the LLM (see `buildAgentMessagesSnapshot`). Appending `message{role: roleAssistant, text: output}` with no `raw` field guarantees the `/context` output is display-only and never injected into the LLM conversation.
