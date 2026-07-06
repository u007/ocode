---
type: Gotcha
title: Session Title Generation & UI Update Root Cause Analysis
description: Root cause analysis of session title delay/mismatch between generation and UI rendering, covering regex anchoring, Anthropic thinking blocks, and rendering cycle timing.
tags:
  - session
  - title-generation
  - regex
  - ui
  - anthropic
  - bubble-tea
timestamp: 2026-07-06T08:35:17Z
---
# Session Title Generation & UI Update Analysis

## Issue Description
"On new session, the title is generated, but it did not immediately reflected on the top and sidebar"

---

## Executive Summary

The session title system has a **multi-stage flow**: LLM generates a title via a custom XML tag, the TUI extracts it using a regex, and the header/sidebar render it. The root cause of the delay/mismatch is likely a **combination of regex anchoring issues and timing in the Bubble Tea render cycle**, potentially compounded by how different LLM providers return reasoning content.

---

## 1. How Session Titles Are Generated

### 1.1 Title Generation Trigger
**File**: `internal/tui/model.go`
**Lines**: 2574, 2603-2607

```go
// Line 2574: Detection of first user message
isFirstUserMsg := m.sessionTitle == "" && countUserMessages(m.messages) == 1

// Lines 2603-2607: Prompt injection for title generation
if isFirstUserMsg && len(agentMsgs) > 0 {
    last := &agentMsgs[len(agentMsgs)-1]
    if last.Role == "user" {
        last.Content += "\n\n[System: Begin your response with <ocode-title>brief session title</ocode-title> on its own line, then continue normally.]"
    }
}
```

### 1.2 Title Extraction from LLM Response
**File**: `internal/tui/model.go`
**Lines**: 2341-2351 (extraction function), 2363-2371 (usage in appendAgentMessage)

```go
// Line 2341: The regex pattern
var ocodeTitleRe = regexp.MustCompile(`(?s)^<ocode-title>(.*?)</ocode-title>\s*\n?`)

// Lines 2343-2351: Extraction function
func extractSessionTitle(content string) (title, rest string) {
    m := ocodeTitleRe.FindStringSubmatchIndex(content)
    if m == nil {
        return "", content
    }
    title = strings.TrimSpace(content[m[2]:m[3]])
    rest = strings.TrimSpace(content[m[1]:])
    return title, rest
}

// Lines 2363-2371: Called during message processing
if m.sessionTitle == "" && content != "" {
    if title, rest := extractSessionTitle(content); title != "" {
        m.sessionTitle = title      // <-- State is updated here
        content = rest
        copyMsg.Content = content
    }
}
```

### 1.3 Fallback Auto-Title (Session Save)
**File**: `internal/session/session.go`
**Lines**: 115-128

```go
if title != "" {
    s.Title = title
} else if s.Title == "" && len(messages) > 0 {
    // Auto-title from first user message
    for _, m := range messages {
        if m.Role == "user" {
            title = m.Content
            if len(title) > 40 {
                title = title[:37] + "..."
            }
            s.Title = title
            break
        }
    }
}
```

---

## 2. How Top Bar and Sidebar Render the Title

### 2.1 Header (Top Bar) Rendering
**File**: `internal/tui/model.go`
**Lines**: 3071-3076

```go
var headerLeft string
if m.sessionTitle != "" {
    headerLeft = m.styles.Header.Render("◆ ocode "+m.sessionTitle) + hintStyle.Render("  ·  opencode clone v"+version.Version)
} else {
    headerLeft = m.styles.Header.Render("◆ ocode") + hintStyle.Render("  ·  opencode clone v"+version.Version)
}
```

### 2.2 Sidebar Rendering
**File**: `internal/tui/model.go`
**Lines**: 3407-3408 (session info), 3449-3452 (sidebar header)

```go
// Session info section (line 3407-3408)
sessionInfo := []string{m.sessionID}
if m.sessionTitle != "" {
    sessionInfo = append(sessionInfo, m.sessionTitle)
}

// Sidebar header (lines 3449-3452)
var header string
if m.sessionTitle != "" {
    header = m.styles.Header.Render(m.sessionTitle) + hintStyle.Render("  ·  opencode clone v"+version.Version)
} else {
    header = hintStyle.Render("◆ ocode  sidebar  ·  v" + version.Version)
}
```

---

## 3. Title State Flow Through UI Components

### 3.1 State Location
**File**: `internal/tui/model.go`
**Line**: 149

```go
type model struct {
    // ... other fields ...
    sessionTitle          string   // Line 149 - The single source of truth
    // ... other fields ...
}
```

### 3.2 Complete Flow Diagram

```
USER INPUT
    │
    ▼
[Enter Key Pressed] ──→ processFileReferences(text) ──→ fileSearchFinishedMsg
    │                                                                │
    ▼                                                                ▼
handleChatKeys (line 1199)                               Update handler (line 681)
    │                                                                │
    ▼                                                                ▼
m.processFileReferences(text)                              m.messages appended
    │                                                      m.askAgent() called
    ▼                                                                │
[fileSearchFinishedMsg]                                             ▼
                                                        ┌─────────────────────┐
                                                        │ askAgent() (line    │
                                                        │ 2567) starts goroutine│
                                                        └─────────────────────┘
                                                                    │
                                                                    ▼
                                                        ┌─────────────────────┐
                                                        │ a.Step(messages)    │
                                                        │ - Adds title prompt │
                                                        │ - Calls LLM         │
                                                        │ - OnMessage callback│
                                                        └─────────────────────┘
                                                                    │
                                                                    ▼
                                                        ┌─────────────────────┐
                                                        │ OnMessage → ch chan │
                                                        │ waitStreamEvent     │
                                                        │ returns streamMsgEvent│
                                                        └─────────────────────┘
                                                                    │
                                                                    ▼
                                                        ┌─────────────────────┐
                                                        │ Update handler      │
                                                        │ case streamMsgEvent │
                                                        │ (line 828)          │
                                                        └─────────────────────┘
                                                                    │
                                                                    ▼
                                                        m.appendAgentMessage(msg.msg)
                                                        │
                                                        ├── extractSessionTitle()
                                                        │   └── m.sessionTitle = title  ← STATE UPDATED
                                                        │
                                                        ▼
                                                        m.renderTranscript()
                                                        m.viewport.GotoBottom()
                                                        return m, waitStreamEvent(...)
                                                                    │
                                                                    ▼
                                                        Bubble Tea calls View()
                                                        │
                                                        ├── renderContent()
                                                        │   ├── Header uses m.sessionTitle
                                                        │   └── renderSidebar() uses m.sessionTitle
                                                        └── Screen re-rendered
```

### 3.3 Key State Transitions

| Event | `sessionTitle` Value | UI Display |
|-------|---------------------|------------|
| New session created | `""` (empty) | "◆ ocode" (no title) |
| First message sent | `""` (unchanged) | "◆ ocode" (no title) |
| LLM responds with `<ocode-title>X</ocode-title>` | `"X"` (extracted) | "◆ ocode X" (should update) |
| Session saved (fallback) | User message (if LLM failed) | "◆ ocode [user msg...]" |

---

## 4. Root Cause Analysis: Why Delay/Mismatch Occurs

### 4.1 **CRITICAL ISSUE: Regex Anchoring** ⭐⭐⭐

**File**: `internal/tui/model.go`, Line 2341
```go
var ocodeTitleRe = regexp.MustCompile(`(?s)^<ocode-title>(.*?)</ocode-title>\s*\n?`)
```

**Problem**: The `^` anchor requires `<ocode-title>` to be at the **exact start** of the content string.

**Failure Scenarios**:
1. **LLM adds greeting first**: `"Hello! Here's my analysis...\n<ocode-title>My Title</ocode-title>..."` ❌
2. **LLM outputs whitespace**: `"  \n<ocode-title>My Title</ocode-title>..."` ❌
3. **Thinking tags inline**: If the model puts thinking in Content instead of ReasoningContent:
   ```
   <think>Let me think about this...
</think>

<ocode-title>My Title</ocode-title>
   Here's my response...
   ```
   ❌

### 4.2 **ISSUE: Anthropic Thinking Blocks Not Handled** ⭐⭐

**File**: `internal/agent/client.go`, Lines 754-770

```go
for _, block := range result.Content {
    if block.Type == "text" {
        resMsg.Content += block.Text
    } else if block.Type == "tool_use" {
        // ... tool handling
    }
    // MISSING: block.Type == "thinking" is NOT handled!
}
```

**Problem**: Anthropic's API returns thinking/reasoning as separate `{"type": "thinking"}` blocks. The current code ignores these blocks, which means:
- Thinking content is **lost** (not stored in `ReasoningContent`)
- If the title tag appears AFTER thinking blocks, it won't be in the Content at all
- Some Anthropic implementations may concatenate thinking into text, causing the regex to fail

### 4.3 **ISSUE: LLM Response is Blocking, Not Streaming** ⭐

**File**: `internal/agent/client.go`, Lines 79-103, 200-272, 590-789

All LLM clients (`chatOpenAI`, `chatAnthropic`, `chatCopilot`) make **synchronous HTTP requests** and return the **complete response** at once.

```go
func (c *GenericClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
    // ... makes HTTP request, waits for full response
    return msg, nil  // Returns COMPLETE message
}
```

**Implication**: The title is only extracted when the **entire LLM response** is received, not incrementally. This means:
- User sees the assistant "thinking" (activity indicator)
- Then suddenly the full response appears WITH the title
- If there's any latency in the LLM response, there's a visible delay

### 4.4 **POTENTIAL ISSUE: Model Passed by Value** ⭐

**File**: `internal/tui/model.go`, Line 497

```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // model is passed BY VALUE
```

While this is standard Bubble Tea pattern, if any goroutines or callbacks hold references to the old model instance, they would see stale `sessionTitle` values.

### 4.5 **ISSUE: Title Extraction Only Checks First Assistant Message** 

**File**: `internal/tui/model.go`, Lines 2363-2371

```go
if m.sessionTitle == "" && content != "" {
    if title, rest := extractSessionTitle(content); title != "" {
        m.sessionTitle = title
```

The check `m.sessionTitle == ""` means title extraction only happens once. If the first assistant message doesn't contain the title (e.g., it's a tool call result), subsequent messages won't trigger title extraction even if they contain the title tag.

---

## 5. Specific File Paths and Line Numbers Summary

| Component | File | Key Lines |
|-----------|------|-----------|
| Title prompt injection | `internal/tui/model.go` | 2574, 2603-2607 |
| Regex pattern | `internal/tui/model.go` | 2341 |
| Extraction function | `internal/tui/model.go` | 2343-2351 |
| Extraction call site | `internal/tui/model.go` | 2363-2371 |
| Session state field | `internal/tui/model.go` | 149 |
| Header rendering | `internal/tui/model.go` | 3071-3076 |
| Sidebar rendering | `internal/tui/model.go` | 3407-3408, 3449-3452 |
| Fallback auto-title | `internal/session/session.go` | 115-128 |
| Anthropic thinking blocks | `internal/agent/client.go` | 754-770 |
| LLM client (blocking) | `internal/agent/client.go` | 79-103, 200-272, 590-789 |
