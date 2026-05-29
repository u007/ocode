# OpenCode Update Summary

**Date:** 2026-05-29  
**Period Covered:** Since 2026-05-22  
**Total Commits:** ~100+ commits  
**Release Versions:** v1.15.9 → v1.15.12

---

## Overview

This update period has been highly productive with significant improvements across the entire OpenCode platform. The changes span from infrastructure and performance enhancements to new features and UI refinements.

---

## Key Features & Improvements

### 🚀 Major New Features

#### 1. **Stats & Analytics Site** (feat: initial datalake and stats site)
- Complete new statistics and analytics platform
- Rankings page with top models chart
- Hero section with responsive design
- Honeycomb backfill for data analytics
- Dark mode tooltip support

#### 2. **ACP-Next (Agent Communication Protocol)**
- **Session Lifecycle Management** - Complete session state management
- **Event Routing** - Advanced event handling system
- **Tool Updates Streaming** - Real-time tool update streaming
- **Permission Events** - Proper permission event handling
- **Usage Service** - Usage tracking and management
- **Directory Snapshot Service** - File system snapshot capabilities
- **Pure Tool Conversion Helpers** - Tool conversion utilities
- **Content Conversion Helpers** - Content format conversion

#### 3. **OpenAI WebSocket Transport**
- **Responses WebSocket Transport** - New WebSocket-based communication
- **Custom Base URL Support** - Flexible endpoint configuration
- **Error Message Preservation** - Better error handling
- **Retry Mechanisms** - Automatic retry for stream failures
- **Response Timeouts** - Configurable timeout handling

#### 4. **TUI Enhancements**
- **Workspace Management Dialog** - New dialog for workspace management
- **Responsive Prompt Size** - Configurable and responsive prompt sizing
- **Thinking Spinner Restoration** - Restored thinking indicator
- **Subagent Retry Status** - Better visibility into retry operations
- **Non-git Project Support** - Handle non-git project paths properly
- **Worktree Path Copy** - Copy worktree path from palette

### 🔧 Infrastructure & Performance

#### 1. **Rate Limiting**
- **Redis-based API Key Rate Limits** - Scalable rate limiting with Redis
- **IP Rate Limits** - Redis/Upstash for IP-based rate limiting
- **Console Serving** - Optimized serving from us-east-2

#### 2. **Server Improvements**
- **Unified HTTP API Middleware** - Consolidated middleware routing
- **Session Directory Persistence** - Persisted session directories
- **Server SDK & Sync State** - Global state management
- **Header Timeout Configuration** - Configurable timeout options

#### 3. **Desktop & Installation**
- **Node-PTY Update** - Bumped to 1.2.0-beta.12
- **Dockerfile Improvements** - Better Docker configurations
- **Nix Hash Updates** - Updated node_modules hashes

### 🎨 UI/UX Improvements

#### 1. **App Enhancements**
- **Tabs Layout Toggle** - Toggle between layout modes
- **Tab Close Button** - Proper close button visibility
- **Home Empty State** - Improved empty state design
- **Font Family Migration** - Migrated to --v2-font-family-sans

#### 2. **Stats Page Design**
- **Responsive Hero Layout** - Figma-matched responsive design
- **IBM Plex Mono Preload** - Font weight preloading
- **Top Models Chart** - Refined scaling and mobile axis
- **Leaderboard Layout** - Refined styling and layout

#### 3. **Desktop V2**
- **Home & Session Controls** - Refined controls and interactions
- **Horizontal Jitter Prevention** - Fixed layout issues

### 🔒 Security & Authentication

#### 1. **OAuth & Authentication**
- **Google Auth for Vertex AI** - Proper OAuth scope passing
- **Password Handling** - Allow colons inside passwords

#### 2. **MCP Server Management**
- **Open Directory Detection** - Only start MCP servers for open directories
- **Dynamic Server Disconnection** - Proper cleanup of dynamic servers

### 📚 Documentation & Ecosystem

#### 1. **Documentation Updates**
- **Russian Translation Fix** - Fixed grammar issues
- **Config Documentation** - Grammar improvements
- **LSP Documentation** - Updated wording and tips
- **MiMo Model Updates** - Added MiMo-V2.5 Free model

#### 2. **Ecosystem**
- **Plugin System** - Added dispose hook
- **Referral System** - Improved referral mechanics

---

## Technical Details

### Commit Breakdown by Type
- **Features (feat):** 29 commits
- **Fixes (fix):** ~70 commits
- **Refactoring (refactor):** 8 commits
- **Performance (perf):** 3 commits
- **Tests (test):** 5 commits
- **Documentation (docs):** 8 commits
- **Chores (chore):** 81 commits
- **Zen Updates:** 5 commits

### Key Areas of Focus
1. **ACP-Next Protocol** - Major architectural improvement
2. **WebSocket Transport** - Enhanced communication reliability
3. **Statistics Platform** - Complete analytics solution
4. **Performance Optimization** - Redis-based scaling
5. **UI Polish** - Responsive design and UX improvements

---

## Breaking Changes

None reported in this update period.

---

## Migration Notes

1. **Stats Site** - New analytics platform requires separate deployment
2. **Redis Dependencies** - Rate limiting now requires Redis/Upstash
3. **WebSocket Transport** - May require client updates for custom implementations

---

## Recommendations

1. **Test WebSocket Transport** - Verify custom base URL configurations
2. **Review Rate Limits** - Check Redis configuration for production
3. **Update Documentation** - Ensure custom plugins use new dispose hook
4. **Validate ACP-Next** - Test agent communication protocol changes

---

*Generated on: 2026-05-29*  
*Source: OpenCode Repository (origin/dev)*
