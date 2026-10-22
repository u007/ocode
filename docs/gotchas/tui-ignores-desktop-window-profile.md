---
type: Gotcha
title: TUI picked up a stale desktop window profile — expired key on every opencode-go model
description: The TUI resolved profile credentials from the desktop's window-state.json, so a leftover window entry silently swapped its keys; the TUI now uses the base config unless OCODE_PROFILE is set, and provider env keys outrank profiles there.
tags:
  - gotcha
  - profile
  - tui
  - auth
  - window-state
timestamp: 2026-10-22T00:00:00Z
---
# TUI picked up a stale desktop window profile

Symptom: every `opencode-go` model failed in the TUI with
`anthropic error (401): api_key_expired` (first seen on `/btw`) while the
desktop app, showing the **Base** profile, worked.

## Root cause

- `auth.ActiveProfile()` (`internal/auth/providers.go`, used by `NewClient`
  and `ResolveKey`) fell back to `~/.local/share/ocode/window-state.json`,
  written by the desktop app, preferring `win-1` then any non-empty entry.
- Picking **Base** in a window only *deletes that window's entry*
  (`SetActiveProfile`, `internal/config/profile_window_state.go`). Entries of
  other or closed windows stayed on `ocode2`, so the TUI (which has no window)
  inherited `ocode2` and its expired key.
- Separately, profile credentials outranked provider env vars, so an
  `OPENCODE_API_KEY=... ocode` launch was beaten by the profile.
- `/btw` and other side clients also rebuilt their client with `NewClient`
  (process-global profile) instead of the session's profile.

## Rules now

- **Only profile-aware servers read window-state.** `ocode serve` (including
  `--remote` on an SSH host) and the desktop server (`internal/desktop/boot.go`)
  call `auth.EnableWindowStateFallback()`. Do **not** call it from
  `server.New`: the TUI's embedded web server also calls `New` and would
  re-enable it in the TUI.
- **TUI / `run`: base config, no profile**, unless `OCODE_PROFILE` is set
  explicitly. Window-state is never consulted.
- **TUI / `run`: provider env var (e.g. `OPENCODE_API_KEY`) outranks profile
  credentials** (`auth.ProviderEnvOutranksProfile`, applied in
  `resolveKeyWithConfig` and `NewClientWithProfile`). `OPENCODE_AUTH_TOKEN`
  still outranks everything. The server keeps profile > env.
- **Server side clients use the session's profile.** `buildAgentSession` calls
  `Agent.SetProfile`; side clients (`/btw`, recap, small model, title, judges,
  maintenance) are built through `Agent.newClient`. Remote SSH sessions are
  unaffected: the host runs `ocode serve --remote` and receives the profile via
  the proxy header (`remote-ssh-chat-profile-not-applied.md`).
- Known remaining global-profile callers: the advisor tool and the scheduler
  runner (no owning session).

Tests: `TestActiveProfileIgnoresWindowStateOutsideServer`
(`internal/auth/profile_resolution_test.go`),
`TestSideQueryClientUsesAgentProfile` (`internal/agent/ask_test.go`).
