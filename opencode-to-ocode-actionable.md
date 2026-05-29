# OpenCode → Ocode: Actionable Updates

**Date:** 2026-05-29  
**Source:** OpenCode origin/dev (v1.15.9 → v1.15.12)

---

## Executive Summary

After analyzing the OpenCode updates (100+ commits, May 22-29), here are the **actionable improvements** that could enhance ocode:

---

## 🔥 High Priority (Direct Feature Parity)

### 1. **OpenAI WebSocket Transport** 
**OpenCode Commit:** `62da1e768` - feat(openai): add responses websocket transport

**What it does:**
- Adds WebSocket-based communication for OpenAI Responses API
- More reliable than HTTP SSE for bidirectional streaming
- Better error recovery and retry mechanisms
- Custom base URL support

**Ocode Impact:**
- ocode currently uses HTTP SSE for streaming (`parseOpenAIChatCompletionsStream`)
- WebSocket could improve reliability for long-running responses
- Better handling of connection drops

**Implementation Effort:** Medium (3-5 days)  
**Files to modify:**
- `internal/agent/client.go` - Add WebSocket transport option
- New file: `internal/agent/websocket.go` - WebSocket client

---

### 2. **Header Timeout Configuration**
**OpenCode Commit:** `f965db9e1` - feat: add headerTimeout cfg option

**What it does:**
- Configurable timeout for HTTP headers (default 10s for OpenAI)
- Prevents hanging connections

**Ocode Impact:**
- ocode has `llmRequestTimeout = 5 * time.Minute` but no header timeout
- Could prevent hanging on slow providers

**Implementation Effort:** Low (1 day)  
**Files to modify:**
- `internal/config/config.go` - Add `headerTimeout` field
- `internal/agent/client.go` - Apply timeout

---

### 3. **TUI Workspace Management Dialog**
**OpenCode Commit:** `28a06e52f` - feat(tui): add workspace management dialog

**What it does:**
- Dialog to switch between workspaces/projects
- Better project navigation

**Ocode Impact:**
- ocode has basic project support but no workspace switching
- Could improve multi-project workflows

**Implementation Effort:** Medium (2-3 days)  
**Files to modify:**
- `internal/tui/model.go` - Add workspace dialog
- `internal/tui/commands.go` - Add workspace commands

---

## 🎯 Medium Priority (UX Improvements)

### 4. **Responsive Prompt Sizing**
**OpenCode Commit:** `0de5f1ff3` - feat(tui): make prompt size responsive and configurable

**What it does:**
- Prompt textarea adjusts to terminal size
- Configurable min/max height

**Ocode Impact:**
- ocode has fixed prompt sizing
- Better UX on different terminal sizes

**Implementation Effort:** Low (1 day)  
**Files to modify:**
- `internal/tui/model.go` - Add responsive sizing logic

---

### 5. **Subagent Retry Status Surfacing**
**OpenCode Commit:** `9814dc652` - fix(tui): surface subagent retry status

**What it does:**
- Shows retry status in TUI when subagents fail
- Better visibility into background operations

**Ocode Impact:**
- ocode has subagent support but limited retry visibility
- Improves debugging of failed operations

**Implementation Effort:** Low (1 day)  
**Files to modify:**
- `internal/tui/model.go` - Add retry status display

---

### 6. **Tab Close Button Fix**
**OpenCode Commit:** `f195c952f` - fix(app): show tab close button properly

**What it does:**
- Properly shows close button on tabs
- Better tab management UX

**Ocode Impact:**
- ocode has tab support but may have similar UI issues
- Minor UX improvement

**Implementation Effort:** Low (0.5 days)  
**Files to modify:**
- `internal/tui/model.go` - Fix tab rendering

---

## 🔧 Infrastructure (If Scaling)

### 7. **Redis-based Rate Limiting**
**OpenCode Commit:** `5acc368ef` - perf: use redis for api key rate limit

**What it does:**
- Redis/Upstash for distributed rate limiting
- Scalable for multi-instance deployments

**Ocode Impact:**
- Only relevant if ocode is deployed as a service
- Currently uses in-memory rate limiting

**Implementation Effort:** Medium (2-3 days)  
**Files to modify:**
- `internal/server/server.go` - Add Redis rate limiter
- `go.mod` - Add Redis dependency

---

### 8. **Session Directory Persistence**
**OpenCode Commit:** `69910f361` - fix(server): use persisted session directory

**What it does:**
- Persists session directories across restarts
- Better session management

**Ocode Impact:**
- ocode already has session persistence
- Minor improvement for long-running sessions

**Implementation Effort:** Low (1 day)

---

## 📋 Implementation Roadmap

### Phase 1: Quick Wins (1-2 days)
1. ✅ Header timeout configuration
2. ✅ Responsive prompt sizing
3. ✅ Subagent retry status
4. ✅ Tab close button fix

### Phase 2: Core Features (3-5 days)
5. ⏳ OpenAI WebSocket transport
6. ⏳ Workspace management dialog

### Phase 3: Infrastructure (If needed)
7. 🔮 Redis rate limiting (for production scaling)
8. 🔮 Session directory persistence

---

## 🎯 Recommended Starting Points

### Option A: TUI Improvements (Low Risk)
Start with responsive prompt sizing and subagent retry status. These are low-risk, high-UX-value changes.

### Option B: OpenAI Enhancement (Medium Risk)
Add WebSocket transport for OpenAI. This improves reliability but requires careful testing.

### Option C: Full Parity (High Effort)
Implement all features systematically following the enhancement plan.

---

## 📝 Technical Notes

### ocode vs OpenCode Architecture
- **ocode**: Go + Bubble Tea TUI, direct HTTP/SSE for LLM calls
- **OpenCode**: TypeScript + React, WebSocket + HTTP for LLM calls

### Key Differences
1. ocode uses HTTP SSE; OpenCode uses WebSocket for new features
2. ocode has simpler permission system
3. ocode lacks stats/analytics site (may not be needed)

### What NOT to Port
- ❌ Stats & Analytics Site (web-only, not relevant for TUI)
- ❌ Desktop-specific fixes (node-pty, etc.)
- ❌ Nix-specific updates (build system)

---

## ✅ Validation Checklist

Before implementing any feature:
- [ ] Test on macOS, Linux, Windows
- [ ] Verify with multiple providers (OpenAI, Anthropic, Google)
- [ ] Check terminal compatibility (Warp, Ghostty, iTerm2)
- [ ] Run existing tests
- [ ] Update documentation

---

*Generated: 2026-05-29*  
*Next Steps: Review and prioritize based on user needs*
