# Directory Update Log































## 2026-09-24

* **Update**: Browser Password Vault — Phase 1 Implementation Plan ([superpowers/plans/2026-09-24-browser-password-vault-phase1/INDEX.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/INDEX.md))
* **Creation**: Web "All sessions" dialog slow to open (render bottleneck, child filtering + pagination) ([gotchas/web-all-sessions-dialog-slow.md](/gotchas/web-all-sessions-dialog-slow.md))
* **Creation**: Per-Chat MCP Toggle ([concepts/per-chat-mcp-toggle.md](/concepts/per-chat-mcp-toggle.md))
* **Update**: Per-session LLM spend accumulator ([gotchas/per-session-llm-spend-accumulator.md](/gotchas/per-session-llm-spend-accumulator.md))
* **Creation**: Side Pane StateKey Convention: Per-Session Scoping ([gotchas/side-pane-statekey-convention.md](/gotchas/side-pane-statekey-convention.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px)" ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Project scoping is visibility, not mounting — React key remounts ([gotchas/project-scope-is-mounting-not-visibility.md](/gotchas/project-scope-is-mounting-not-visibility.md))
* **Deprecation**: Browser Panel Close Reopens Deleted State: Superseded by per-session side-pane scoping (2026-09-24). The cross-session propagation effect in App.tsx (which called browserActions.open(sideStateKey) for a new session when the old one was open) and the panelClosedByUser ref that caused the close-then-reopen bug were both removed. Pane open/collapsed state now lives under each session's own side:chat:<id> key in localStorage ocode.ui.sidebarPreview.v2; closing the pane in one chat never affects another chat, and switching to a different chat shows that chat's own (likely closed) pane. See gotchas/project-scope-is-mounting-not-visibility.md for the current mechanism. ([gotchas/browser-panel-close-reopens-state.md](/gotchas/browser-panel-close-reopens-state.md))
* **Update**: Embedded Browser Password Vault — Design ([superpowers/specs/2026-09-24-browser-password-vault-design.md](/superpowers/specs/2026-09-24-browser-password-vault-design.md))
* **Update**: Per-session LLM spend accumulator ([gotchas/per-session-llm-spend-accumulator.md](/gotchas/per-session-llm-spend-accumulator.md))
* **Creation**: Per-session LLM spend accumulator ([docs/gotchas/per-session-llm-spend-accumulator.md](/docs/gotchas/per-session-llm-spend-accumulator.md))
* **Update**: Interrupted Turn Notice ([concepts/interrupted-turn-notice.md](/concepts/interrupted-turn-notice.md))
* **Update**: Auto-permission settings not applied to a running chat ([gotchas/auto-permission-settings-not-applied-to-live-chat.md](/gotchas/auto-permission-settings-not-applied-to-live-chat.md))
* **Creation**: Auto-permission settings not applied to a running chat ([gotchas/auto-permission-settings-not-applied-to-live-chat.md](/gotchas/auto-permission-settings-not-applied-to-live-chat.md))
* **Creation**: Advisor Claude Code CLI backend on web/desktop ([docs/concepts/advisor-claude-code-backend.md](/docs/concepts/advisor-claude-code-backend.md))

## 2026-09-23

* **Creation**: Embedded HTR Extension and Managed Daemon Design ([docs/superpowers/specs/2026-09-09-embedded-htr-extension-design.md](/docs/superpowers/specs/2026-09-09-embedded-htr-extension-design.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([docs/gotchas/files-tab-preview-only-routing.md](/docs/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([docs/gotchas/files-tab-preview-only-routing.md](/docs/gotchas/files-tab-preview-only-routing.md))
* **Update**: Multi-Use Preview — Design Spec (Draft, 2026-09-10) ([superpowers/specs/2026-09-10-preview-multipurpose-design.md](/superpowers/specs/2026-09-10-preview-multipurpose-design.md))
* **Update**: MDX Preview: Rendered as Markdown, Never Evaluated ([gotchas/mdx-preview-not-evaluated.md](/gotchas/mdx-preview-not-evaluated.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Deprecation**: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing): Superseded by docs/gotchas/files-tab-preview-only-routing.md — duplicate created during prior edit. ([gotchas/files-tab-preview-only-routing copy.md](/gotchas/files-tab-preview-only-routing copy.md))
* **Creation**: "git commit failed: exit status 1" with no reason (git explains on stdout, not stderr) ([gotchas/git-commit-opaque-exit-status.md](/gotchas/git-commit-opaque-exit-status.md))
* **Update**: Desktop/Web Terminal Wheel Scroll Chains to the App Page (xterm.js Escape Gestures) ([gotchas/terminal-wheel-scroll-chaining.md](/gotchas/terminal-wheel-scroll-chaining.md))
* **Creation**: Desktop/Web Terminal Wheel Scroll Chains to the App Page (xterm.js Escape Gestures) ([gotchas/terminal-wheel-scroll-chaining.md](/gotchas/terminal-wheel-scroll-chaining.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px)" ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Creation**: Web UI Mobile Layout Breakage (≤767px) ([docs/gotchas/web-ui-mobile-layout-breakage.md](/docs/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px) ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Linux Sandbox Lockout: Confiner Shell Path, /dev/null, and Binary Dispatch ([gotchas/linux-sandbox-lockout-confiner-shell-devnull-dispatch.md](/gotchas/linux-sandbox-lockout-confiner-shell-devnull-dispatch.md))
* **Update**: Sandbox Writable-Root Must Exist on Disk ([gotchas/sandbox-writable-root-must-exist.md](/gotchas/sandbox-writable-root-must-exist.md))
* **Update**: Seatbelt Profile Test Coverage Gap ([gotchas/seatbelt-profile-test-coverage-gap.md](/gotchas/seatbelt-profile-test-coverage-gap.md))

## 2026-09-22

* **Update**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Update**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Update**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Creation**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([docs/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/docs/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Update**: Remote git commit/stash messages must be shell-quoted (remoteGitCommand contract) ([gotchas/remote-git-shell-quoting.md](/gotchas/remote-git-shell-quoting.md))
* **Creation**: Remote git commit/stash messages must be shell-quoted (remoteGitCommand contract) ([gotchas/remote-git-shell-quoting.md](/gotchas/remote-git-shell-quoting.md))
* **Update**: Shared tabs.json is written by every ocode server process — whole-map replace dropped projects ([gotchas/shared-tabs-json-multi-writer-clobber.md](/gotchas/shared-tabs-json-multi-writer-clobber.md))
* **Creation**: Shared tabs.json is written by every ocode server process — whole-map replace dropped projects ([gotchas/shared-tabs-json-multi-writer-clobber.md](/gotchas/shared-tabs-json-multi-writer-clobber.md))
* **Update**: TUI: skipLLM is not a render gate — fake-agent and cron replies vanish on fresh sessions ([gotchas/tui-skipllm-is-not-a-render-gate.md](/gotchas/tui-skipllm-is-not-a-render-gate.md))
* **Creation**: TUI: skipLLM is not a render gate — fake-agent and cron replies vanish on fresh sessions ([gotchas/tui-skipllm-is-not-a-render-gate.md](/gotchas/tui-skipllm-is-not-a-render-gate.md))
* **Creation**: File search result ordering: shortest path first ([concepts/file-search-shortest-path-ordering.md](/concepts/file-search-shortest-path-ordering.md))
* **Update**: Web Chat Composer Input History (↑/↓ Navigation) ([concepts/web-chat-input-history.md](/concepts/web-chat-input-history.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Creation**: Web Chat Composer Input History (↑/↓ Navigation) ([concepts/web-chat-input-history.md](/concepts/web-chat-input-history.md))
* **Update**: Interrupted Turn Notice ([concepts/interrupted-turn-notice.md](/concepts/interrupted-turn-notice.md))
* **Creation**: Interrupted Turn Notice ([concepts/interrupted-turn-notice.md](/concepts/interrupted-turn-notice.md))
* **Creation**: MDX Preview: Rendered as Markdown, Never Evaluated ([gotchas/mdx-preview-not-evaluated.md](/gotchas/mdx-preview-not-evaluated.md))
* **Update**: Multi-Use Preview — Design Spec (Draft, 2026-09-10) ([superpowers/specs/2026-09-10-preview-multipurpose-design.md](/superpowers/specs/2026-09-10-preview-multipurpose-design.md))
* **Creation**: Main model pick also sets the global default ([gotchas/main-model-pick-also-sets-global-default.md](/gotchas/main-model-pick-also-sets-global-default.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Deprecation**: Main model pick also sets the global default: Superseded — created at wrong depth (docs/docs/) by doc_write which prepends docs/; correct file written at docs/gotchas/ ([docs/gotchas/main-model-pick-also-sets-global-default.md](/docs/gotchas/main-model-pick-also-sets-global-default.md))
* **Creation**: Main model pick also sets the global default ([docs/gotchas/main-model-pick-also-sets-global-default.md](/docs/gotchas/main-model-pick-also-sets-global-default.md))
## 2026-09-21

* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Creation**: Persistent per-session shell for exclamation-mark commands ([concepts/persistent-shell-session.md](/concepts/persistent-shell-session.md))
* **Update**: Git stash UI: list, per-file restore, delete ([concepts/git-stash-ui.md](/concepts/git-stash-ui.md))
* **Update**: Web Composer Quick-Actions Strip ([concepts/web-composer-quick-actions.md](/concepts/web-composer-quick-actions.md))
* **Creation**: Session-tagged snapshot: override base fields but recompute ALL derived fields ([gotchas/session-snapshot-stale-derived-fields.md](/gotchas/session-snapshot-stale-derived-fields.md))
* **Deprecation**: Session-tagged snapshot: override base fields but recompute ALL derived fields: Created at wrong path (docs/docs/... due to double-prefix). Superseded by the correctly placed version at gotchas/session-snapshot-stale-derived-fields.md ([docs/gotchas/session-snapshot-stale-derived-fields.md](/docs/gotchas/session-snapshot-stale-derived-fields.md))
* **Creation**: Session-tagged snapshot: override base fields but recompute ALL derived fields ([docs/gotchas/session-snapshot-stale-derived-fields.md](/docs/gotchas/session-snapshot-stale-derived-fields.md))
* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Desktop subprocess PATH trap — bare CLI names fail under Finder/Dock-launched .app ([gotchas/desktop-subprocess-path-trap.md](/gotchas/desktop-subprocess-path-trap.md))
* **Update**: Advisor Claude Code CLI backend on web/desktop ([concepts/advisor-claude-code-backend.md](/concepts/advisor-claude-code-backend.md))
* **Update**: Desktop subprocess PATH trap — bare CLI names fail under Finder/Dock-launched .app ([gotchas/desktop-subprocess-path-trap.md](/gotchas/desktop-subprocess-path-trap.md))
* **Creation**: Advisor Claude Code CLI backend on web/desktop ([concepts/advisor-claude-code-backend.md](/concepts/advisor-claude-code-backend.md))
* **Creation**: Desktop subprocess PATH trap — bare CLI names fail under Finder/Dock-launched .app ([gotchas/desktop-subprocess-path-trap.md](/gotchas/desktop-subprocess-path-trap.md))
* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Creation**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Remote Persistent Sessions and Terminals ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Creation**: Session-bound dialogs scope to their chat session ([concepts/session-bound-dialog-scoping.md](/concepts/session-bound-dialog-scoping.md))
* **Creation**: Terminal find bar stuck on "No matches" ([gotchas/terminal-find-stuck-no-matches.md](/gotchas/terminal-find-stuck-no-matches.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px) ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Creation**: Auto-Permission Judge — Credential Material Is Withheld from the Judge Context ([gotchas/auto-permission-judge-withholds-credentials.md](/gotchas/auto-permission-judge-withholds-credentials.md))
* **Creation**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Remote Persistent Sessions and Terminals ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Update**: Server-Side Auto-Continue Loop ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Creation**: Remote SSH chat ignored the desktop active profile — proxy silent on window→profile mapping ([gotchas/remote-ssh-chat-profile-not-applied.md](/gotchas/remote-ssh-chat-profile-not-applied.md))
* **Update**: Concurrent session writers — conflict semantics and recovery ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Update**: Cross-process session activity sync (revision revalidation) ([concepts/cross-process-session-sync.md](/concepts/cross-process-session-sync.md))
* **Update**: Web Composer Quick-Actions Strip ([concepts/web-composer-quick-actions.md](/concepts/web-composer-quick-actions.md))
* **Creation**: Cross-process session activity sync (revision revalidation) ([concepts/cross-process-session-sync.md](/concepts/cross-process-session-sync.md))
* **Creation**: Web Composer Quick-Actions Strip ([concepts/web-composer-quick-actions.md](/concepts/web-composer-quick-actions.md))
* **Update**: Files tab: directory expansion persists across project switches — but only after the fix ([gotchas/files-tab-tree-expansion-persistence.md](/gotchas/files-tab-tree-expansion-persistence.md))
* **Creation**: Files tab: directory expansion persists across project switches — but only after the fix ([gotchas/files-tab-tree-expansion-persistence.md](/gotchas/files-tab-tree-expansion-persistence.md))
* **Creation**: Git stash UI: list, per-file restore, delete ([concepts/git-stash-ui.md](/concepts/git-stash-ui.md))
* **Update**: Local Speech-to-Text (In-App Dictation) — Design ([superpowers/specs/2026-09-21-speech-to-text-design.md](/superpowers/specs/2026-09-21-speech-to-text-design.md))
* **Update**: Local Speech-to-Text (In-App Dictation) — Design ([superpowers/specs/2026-09-21-speech-to-text-design.md](/superpowers/specs/2026-09-21-speech-to-text-design.md))
* **Creation**: Speech-to-Text (Dictation) Design ([superpowers/specs/2026-09-21-speech-to-text-design.md](/superpowers/specs/2026-09-21-speech-to-text-design.md))
* **Update**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
* **Update**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
* **Update**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
* **Update**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
* **Update**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
* **Update**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
* **Update**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
* **Update**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
* **Creation**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
## 2026-09-20

* **Creation**: Radix modal focus trap silently breaks the execCommand clipboard fallback ([gotchas/radix-modal-focus-trap-breaks-execCommand-clipboard.md](/gotchas/radix-modal-focus-trap-breaks-execCommand-clipboard.md))
* **Update**: Profile switch does not affect an already-open chat session (window-id divergence) ([gotchas/profile-switch-window-id-divergence.md](/gotchas/profile-switch-window-id-divergence.md))
* **Creation**: Profile switch does not affect an already-open chat session (window-id divergence) ([gotchas/profile-switch-window-id-divergence.md](/gotchas/profile-switch-window-id-divergence.md))
* **Update**: Web/Desktop Chat Went Stale Because a Dead SSE Body Never Errors ([gotchas/web-sse-stream-silent-death-liveness.md](/gotchas/web-sse-stream-silent-death-liveness.md))
* **Update**: Part 05 — Frontend Status on Activation + Streaming Watchdog ([superpowers/plans/2026-08-12-multiproject-event-architecture/05-frontend-status-streaming.md](/superpowers/plans/2026-08-12-multiproject-event-architecture/05-frontend-status-streaming.md))
* **Update**: Part 03 — Async Bootstrap, Turn State Machine, Reconcile & Status Endpoints ([superpowers/plans/2026-08-12-multiproject-event-architecture/03-async-bootstrap-turn-state.md](/superpowers/plans/2026-08-12-multiproject-event-architecture/03-async-bootstrap-turn-state.md))
* **Update**: Session Re-key via /reset-id ([docs/concepts/session-rekey-reset-id.md](/docs/concepts/session-rekey-reset-id.md))
* **Creation**: Session Re-key via /reset-id ([docs/concepts/session-rekey-reset-id.md](/docs/concepts/session-rekey-reset-id.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Project scoping is visibility, not mounting — React key remounts ([gotchas/project-scope-is-mounting-not-visibility.md](/gotchas/project-scope-is-mounting-not-visibility.md))
* **Update**: PDF viewer zoom, Space navigation, and find ([concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))
* **Creation**: Project scoping is visibility, not mounting — React key remounts ([gotchas/project-scope-is-mounting-not-visibility.md](/gotchas/project-scope-is-mounting-not-visibility.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Concurrent session writers — conflict semantics and recovery ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Deprecation**: Concurrent session writers — conflict semantics and recovery: Path was incorrect — doc_write paths are relative to docs/ root. This file was created by mistake when the path docs/gotchas/session-writers-conflict-recovery.md was used (resolving to docs/docs/...); the correct location is gotchas/session-writers-conflict-recovery.md. Superseded by the update to that path. ([docs/gotchas/session-writers-conflict-recovery.md](/docs/gotchas/session-writers-conflict-recovery.md))
* **Update**: Concurrent session writers — conflict semantics and recovery ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Update**: Concurrent session writers — conflict semantics and recovery ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Creation**: Concurrent session writers — conflict semantics and recovery ([docs/gotchas/session-writers-conflict-recovery.md](/docs/gotchas/session-writers-conflict-recovery.md))
* **Update**: PDF viewer zoom, Space navigation, and find ([docs/concepts/pdf-viewer-zoom-find.md](/docs/concepts/pdf-viewer-zoom-find.md))
* **Creation**: PDF viewer zoom, Space navigation, and find ([docs/concepts/pdf-viewer-zoom-find.md](/docs/concepts/pdf-viewer-zoom-find.md))
* **Creation**: PDF viewer zoom, Space navigation, and find ([concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))
## 2026-09-19

* **Deprecation**: PDF viewer zoom, Space navigation, and find: Path was incorrect — doc_write paths are relative to docs/ root. Replaced by concepts/pdf-viewer-zoom-find.md. ([docs/concepts/pdf-viewer-zoom-find.md](/docs/concepts/pdf-viewer-zoom-find.md))
* **Creation**: PDF viewer zoom, Space navigation, and find ([concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))
* **Creation**: PDF viewer zoom, Space navigation, and find ([docs/concepts/pdf-viewer-zoom-find.md](/docs/concepts/pdf-viewer-zoom-find.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px) ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Creation**: Host and project scoping for web session/project reads ([concepts/web-session-host-scoping.md](/concepts/web-session-host-scoping.md))
* **Creation**: Per-session LLM spend accumulator ([gotchas/per-session-llm-spend-accumulator.md](/gotchas/per-session-llm-spend-accumulator.md))
* **Creation**: Web UI Mobile Layout Breakage (≤767px) ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Web model picker must open from the cached model list, not a live refresh ([gotchas/web-model-picker-cached-not-live.md](/gotchas/web-model-picker-cached-not-live.md))
* **Creation**: Web model picker must open from the cached model list, not a live refresh ([docs/gotchas/web-model-picker-cached-not-live.md](/docs/gotchas/web-model-picker-cached-not-live.md))
* **Creation**: Web Ask Dialog LLM Context Preview ([concepts/web-ask-dialog-llm-context.md](/concepts/web-ask-dialog-llm-context.md))
* **Creation**: After Compaction: Publish Status Snapshot + Record Estimate or Context Gauge Goes Stale ([gotchas/compaction-context-gauge-stale-after-splice.md](/gotchas/compaction-context-gauge-stale-after-splice.md))
* **Creation**: Web Compact Feedback — Final Implementation ([superpowers/specs/2026-09-19-web-compact-feedback-final.md](/superpowers/specs/2026-09-19-web-compact-feedback-final.md))
* **Deprecation**: Web Compact Feedback Design: Superseded by the actual implementation which changed the design significantly: (1) backend now publishes a status snapshot after compaction via publishTurnStatusSnapshot so the Context gauge stays accurate, (2) the "complete" compaction state was removed — only "active" and "error" states remain, (3) the compaction notice moved from a composer bar into the transcript as a persisted [ocode:compaction-summary] system message rendered by CompactionNotice.tsx, (4) clearCompaction was added to compactionState.ts. See superseding spec and gotcha below. ([superpowers/specs/2026-09-18-web-compact-feedback-design.md](/superpowers/specs/2026-09-18-web-compact-feedback-design.md))
* **Update**: Files-Tab OS-Native Reveal (Open/Show in Finder/Explorer/File Manager) ([gotchas/files-tab-os-native-reveal.md](/gotchas/files-tab-os-native-reveal.md))
* **Creation**: Files-Tab OS-Native Reveal (Open/Show in Finder/Explorer/File Manager) ([gotchas/files-tab-os-native-reveal.md](/gotchas/files-tab-os-native-reveal.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Update**: Remote Persistent Sessions and Terminals Design ([superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md](/superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md))
* **Creation**: Terminal close semantics and OSC title source ([gotchas/terminal-close-and-osc-title.md](/gotchas/terminal-close-and-osc-title.md))
* **Creation**: Doc Search Relevance Judge ([concepts/doc-search-relevance-judge.md](/concepts/doc-search-relevance-judge.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
## 2026-09-18

* **Update**: ChatPanel & LogPanel Autoscroll Bounce / Freeze ([gotchas/autoscroll-bounce.md](/gotchas/autoscroll-bounce.md))
* **Update**: ChatPanel Autoscroll Bounce / Freeze ([gotchas/autoscroll-bounce.md](/gotchas/autoscroll-bounce.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Creation**: Discovery Web Surfaces ([concepts/discovery-web-surfaces.md](/concepts/discovery-web-surfaces.md))
* **Creation**: ONNX Runtime POSIX telemetry writes `:memory:.ses` into process cwd ([gotchas/onnx-runtime-telemetry-memory-ses.md](/gotchas/onnx-runtime-telemetry-memory-ses.md))
* **Deprecation**: Remote SSH terminal 502s from provisioning and path bugs: Superseded: doc tools take bundle-relative paths, so this write landed at docs/docs/gotchas/remote-terminal-502-provisioning.md instead of docs/gotchas/.... The correct doc is at gotchas/remote-terminal-502-provisioning.md (bundle-relative). ([docs/gotchas/remote-terminal-502-provisioning.md](/docs/gotchas/remote-terminal-502-provisioning.md))
* **Creation**: Remote SSH terminal 502s from provisioning and path bugs ([gotchas/remote-terminal-502-provisioning.md](/gotchas/remote-terminal-502-provisioning.md))
* **Creation**: Remote SSH terminal 502s from provisioning and path bugs ([docs/gotchas/remote-terminal-502-provisioning.md](/docs/gotchas/remote-terminal-502-provisioning.md))
* **Creation**: Project/Endpoint Isolation — one project must never halt another ([gotchas/project-endpoint-isolation.md](/gotchas/project-endpoint-isolation.md))
* **Update**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Update**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Update**: Server-Side Auto-Continue Loop ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Creation**: espeak-ng N_PATH_HOME buffer truncates long data paths → exit(1) ([gotchas/kokoro-espeak-ng-path-limit.md](/gotchas/kokoro-espeak-ng-path-limit.md))
* **Update**: Sandbox Permission Mode ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
* **Creation**: Remote Persistent Sessions and Terminals ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Deprecation**: System Permissions Settings Section (macOS TCC + cross-platform): Document was created at the wrong path (docs/docs/...). The correct canonical location is superpowers/specs/2026-09-18-system-permissions-design.md. Content has been moved; this copy is superseded. ([docs/superpowers/specs/2026-09-18-system-permissions-design.md](/docs/superpowers/specs/2026-09-18-system-permissions-design.md))
* **Creation**: System Permissions Settings Section (macOS TCC + cross-platform) ([superpowers/specs/2026-09-18-system-permissions-design.md](/superpowers/specs/2026-09-18-system-permissions-design.md))
* **Creation**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Update**: Web Compact Feedback Design ([superpowers/specs/2026-09-18-web-compact-feedback-design.md](/superpowers/specs/2026-09-18-web-compact-feedback-design.md))
* **Creation**: Web Compact Feedback Design ([superpowers/specs/2026-09-18-web-compact-feedback-design.md](/superpowers/specs/2026-09-18-web-compact-feedback-design.md))
## 2026-09-17

* **Creation**: Auto-continue turn transcript rebase: capture base length before the loop ([gotchas/auto-continue-turn-transcript-rebase.md](/gotchas/auto-continue-turn-transcript-rebase.md))
* **Creation**: Server-Side Auto-Continue Loop ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Update**: Question-answer transcript echo: client must mirror server payload shape ([gotchas/question-answer-transcript-echo.md](/gotchas/question-answer-transcript-echo.md))
* **Update**: Computer use ([computer-use.md](/computer-use.md))
* **Update**: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary ([gotchas/remote-project-path-trust-boundary.md](/gotchas/remote-project-path-trust-boundary.md))
* **Update**: Terminal Shells Survive Page Reload (Detach / Reattach) ([architecture/terminal-detach-reattach.md](/architecture/terminal-detach-reattach.md))
* **Update**: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary ([gotchas/remote-project-path-trust-boundary.md](/gotchas/remote-project-path-trust-boundary.md))
* **Update**: Terminal Shells Survive Page Reload (Detach / Reattach) ([architecture/terminal-detach-reattach.md](/architecture/terminal-detach-reattach.md))
* **Update**: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary ([gotchas/remote-project-path-trust-boundary.md](/gotchas/remote-project-path-trust-boundary.md))
* **Update**: Terminal Shells Survive Page Reload (Detach / Reattach) ([architecture/terminal-detach-reattach.md](/architecture/terminal-detach-reattach.md))
* **Update**: Multi-Use Preview — Design Spec (Draft, 2026-09-10) ([superpowers/specs/2026-09-10-preview-multipurpose-design.md](/superpowers/specs/2026-09-10-preview-multipurpose-design.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Computer use ([computer-use.md](/computer-use.md))
* **Update**: Auto-Permission — Interpreter Scripts in Compound Commands ([gotchas/auto-permission-interpreter-scripts-in-compound-commands.md](/gotchas/auto-permission-interpreter-scripts-in-compound-commands.md))
* **Creation**: Bash Control-Flow Loops Are Not Commands ([gotchas/bash-control-flow-loops-are-not-commands.md](/gotchas/bash-control-flow-loops-are-not-commands.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Update**: ChatPanel Autoscroll Bounce/Freeze ([gotchas/autoscroll-bounce.md](/gotchas/autoscroll-bounce.md))
* **Creation**: Pinned pip requirement sets need an upper Python bound, not just a minimum ([gotchas/tts-pinned-python-upper-bound-needed.md](/gotchas/tts-pinned-python-upper-bound-needed.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Update**: Pending ask recovery from live session state (sentinel-less transcript) ([gotchas/pending-ask-recovery-live-session-state.md](/gotchas/pending-ask-recovery-live-session-state.md))
* **Creation**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Creation**: Linux Sandbox Lockout: Confiner Shell Path, /dev/null, and Binary Dispatch ([gotchas/linux-sandbox-lockout-confiner-shell-devnull-dispatch.md](/gotchas/linux-sandbox-lockout-confiner-shell-devnull-dispatch.md))
* **Update**: Pending ask recovery from live session state (sentinel-less transcript) ([gotchas/pending-ask-recovery-live-session-state.md](/gotchas/pending-ask-recovery-live-session-state.md))
* **Update**: Pending ask recovery from live session state (sentinel-less transcript) ([gotchas/pending-ask-recovery-live-session-state.md](/gotchas/pending-ask-recovery-live-session-state.md))
* **Update**: Terminal Shells Survive Page Reload (Detach / Reattach) ([architecture/terminal-detach-reattach.md](/architecture/terminal-detach-reattach.md))
* **Creation**: Pending ask recovery from live session state (sentinel-less transcript) ([docs/gotchas/pending-ask-recovery-live-session-state.md](/docs/gotchas/pending-ask-recovery-live-session-state.md))
* **Creation**: Sandbox Permission Mode ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
* **Update**: Sandbox git push — SSH agent inheritance and fail-closed TTY prompts ([gotchas/sandbox-git-push-ssh-agent-tty.md](/gotchas/sandbox-git-push-ssh-agent-tty.md))
* **Creation**: Port forwards Disable/Enable: URL composed past query + supervisor retained-terminal collision ([gotchas/port-forwards-url-composition-and-supervisor-restart.md](/gotchas/port-forwards-url-composition-and-supervisor-restart.md))
## 2026-09-16

* **Creation**: PDF preview fails on WebKit with `undefined is not a function (near '...e of t...')` ([gotchas/pdf-preview-webkit-async-iterator.md](/gotchas/pdf-preview-webkit-async-iterator.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Creation**: Speech Rendered Text Extraction — DOM, Not Markdown Source ([gotchas/speech-rendered-text-extraction.md](/gotchas/speech-rendered-text-extraction.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Remote-project port forwards in the web/desktop UI ([superpowers/specs/2026-09-16-remote-project-port-forwards-design.md](/superpowers/specs/2026-09-16-remote-project-port-forwards-design.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Multi-Use Preview — Design Spec (Draft, 2026-09-10) ([superpowers/specs/2026-09-10-preview-multipurpose-design.md](/superpowers/specs/2026-09-10-preview-multipurpose-design.md))
* **Deprecation**: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing): Misplaced by a path error: doc tools take bundle-relative paths (root is docs/), so this write landed at docs/docs/gotchas/... instead of docs/gotchas/.... Superseded by the correct doc at gotchas/files-tab-preview-only-routing.md; safe to remove via /docs cleanup. ([docs/gotchas/files-tab-preview-only-routing.md](/docs/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing) ([docs/gotchas/files-tab-preview-only-routing.md](/docs/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Git action errors "come out then disappear by themselves" — TUI truncation, sticky status, and background-refresh error clearing ([gotchas/git-action-errors-disappear.md](/gotchas/git-action-errors-disappear.md))
## 2026-09-14

* **Creation**: TUI Leaves Mouse Tracking On After Silent Exit — Diagnose via tui-crash.log ([gotchas/tui-mouse-garbage-after-idle-crash-log.md](/gotchas/tui-mouse-garbage-after-idle-crash-log.md))
* **Creation**: Auto-Permission — Interpreter Network Effects Denied Loopback and host:port Targets ([gotchas/auto-permission-interpreter-network-loopback.md](/gotchas/auto-permission-interpreter-network-loopback.md))
* **Creation**: Remote Terminal Custom Port Omitted from WebSocket ([gotchas/remote-terminal-custom-port-omitted.md](/gotchas/remote-terminal-custom-port-omitted.md))
* **Creation**: Browser Panel Close Reopens Deleted State ([gotchas/browser-panel-close-reopens-state.md](/gotchas/browser-panel-close-reopens-state.md))
* **Update**: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary ([gotchas/remote-project-path-trust-boundary.md](/gotchas/remote-project-path-trust-boundary.md))
* **Creation**: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary ([gotchas/remote-project-path-trust-boundary.md](/gotchas/remote-project-path-trust-boundary.md))
* **Creation**: LSP Diagnostics Marked Reported Before Emission ([gotchas/lsp-diagnostics-marked-reported-before-emission.md](/gotchas/lsp-diagnostics-marked-reported-before-emission.md))
* **Creation**: TUI Selection Context Lost on Double Preparation ([gotchas/tui-selection-context-lost-on-double-preparation.md](/gotchas/tui-selection-context-lost-on-double-preparation.md))
## 2026-09-13

* **Update**: AGENTS.md Data Storage / README API list — open session tabs now server-side via bulk `GET/PUT /api/tabs` (`tabs.json`), replacing per-origin `localStorage`; fixes empty tab bar on shared Tailscale URL
* **Update**: Computer use ([docs/computer-use.md](/docs/computer-use.md))
## 2026-09-11

* **Creation**: TTS License Acceptance Must Validate the Exact License Text Hash ([gotchas/tts-license-acceptance-hash-validation.md](/gotchas/tts-license-acceptance-hash-validation.md))
## 2026-09-09

* **Update**: TUI Sidebar Title Expand/Collapse Design ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Deprecation**: TUI Sidebar Title Expand/Collapse Design: Resetting to active status per user redesign decision ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TUI Sidebar Title Expand/Collapse Design ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TUI Sidebar Title Expand/Collapse Design ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Creation**: TUI Sidebar Title Expand/Collapse Design ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TUI Sidebar Title Expand/Collapse Design ([superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Deprecation**: TUI Sidebar Title Expand/Collapse Design: User decided: expansion is transient; /new, /clear, /session load, and active-session replacement all reset to collapsed; no session JSON persistence. Contradictory phrases about session JSON restoration need updating. ([superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TUI Sidebar Title Expand/Collapse Design ([superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Fix**: TTS Speech Playback Design Specification deprecation corrected - removed `status: deprecated` and `deprecated_reason` from frontmatter; restored to active status. Index and log updated. ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: TTS Speech Playback Design Specification ([docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md](/docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: TTS Speech Playback Design Specification ([docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md](/docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md))

* **Creation**: TTS Speech Playback Design Specification ([docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md](/docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**:  ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TUI Sidebar Title Expand/Collapse Design (Updated) ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TUI Sidebar Title Expand/Collapse Design (Updated) ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Creation**: TUI Sidebar Title Expand/Collapse Design (Updated) ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TUI Sidebar Title Expand/Collapse Design ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Creation**: TUI Sidebar Title Expand/Collapse Design ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Creation**: Chrome Tab Hang — Unbounded CDP Calls, JS Dialogs, Invisible Popups ([gotchas/chrome-tab-hang-unbounded-cdp-call.md](/gotchas/chrome-tab-hang-unbounded-cdp-call.md))
* **Update**: Browser Chrome CDP design ([docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md](/docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md)) — Conn.Call default deadline, JS dialog auto-accept, popup/middle-click discovery via Target.setDiscoverTargets
* **Update**: Remote SSH Phase 2 — web ([docs/superpowers/specs/2026-08-29-remote-ssh/03-phase2-web.md](/docs/superpowers/specs/2026-08-29-remote-ssh/03-phase2-web.md)) — browse origin port now recorded in serve.json (`browsePort`) and forwarded by the SSH tunnel on the same port number; browser panel enabled in remote (SSH + WSL) sessions

## 2026-09-08

* **Creation**: Terminal Shells Survive Page Reload (Detach / Reattach) ([docs/architecture/terminal-detach-reattach.md](/docs/architecture/terminal-detach-reattach.md))
* **Update**: Changes Tab ([docs/changes-tab.md](/docs/changes-tab.md)) — bash recorder: per-call baseline, ns mtime, same-size edits no longer dropped; sub-agents share the parent registry; agent rebuilds/session loads re-bind the snapshot store via Store.SwitchSession
* **Creation**: Permission Evaluation and Unknown Tool Guard ([gotchas/permission-evaluation-and-unknown-tool-guard.md](/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Update**: Session Storage Critical Issues ([docs/gotchas/session-storage-critical-issues.md](/docs/gotchas/session-storage-critical-issues.md))
* **Creation**: Test Update Doc ([docs/gotchas/test-update.md](/docs/gotchas/test-update.md))
* **Update**: Session Storage Critical Issues ([docs/gotchas/session-storage-critical-issues.md](/docs/gotchas/session-storage-critical-issues.md))
* **Creation**: Session Storage Critical Issues ([docs/gotchas/session-storage-critical-issues.md](/docs/gotchas/session-storage-critical-issues.md))
* **Creation**: Web ask dialogs: broadcast resolved before continuation ([docs/gotchas/web-ask-dialog-resolved-before-continuation.md](/docs/gotchas/web-ask-dialog-resolved-before-continuation.md)) — question_resolved now fires before agent.Step so the QuestionDialog closes on answer
* **Deprecation**: Terminal History Persistence and Restore: Replaced by corrected version at terminal-history-persistence-and-restore.md per user instructions ([docs/docs/terminal-history-persistence-and-restore.md](/docs/docs/terminal-history-persistence-and-restore.md))
* **Update**: Terminal History Persistence and Restore ([docs/terminal-history-persistence-and-restore.md](/docs/terminal-history-persistence-and-restore.md))
* **Creation**: Terminal History Persistence and Restore ([docs/terminal-history-persistence-and-restore.md](/docs/terminal-history-persistence-and-restore.md))
* **Update**: Terminal History Persistence and Restore ([docs/docs/terminal-history-persistence-and-restore.md](/docs/docs/terminal-history-persistence-and-restore.md))
* **Deprecation**: Terminal History Persistence and Restore: Replacing with corrected version per user instructions ([docs/docs/terminal-history-persistence-and-restore.md](/docs/docs/terminal-history-persistence-and-restore.md))
* **Creation**: Terminal History Persistence and Restore ([docs/docs/terminal-history-persistence-and-restore.md](/docs/docs/terminal-history-persistence-and-restore.md))
* **Update**: Terminal History Persistence and Restore ([docs/terminal-history-persistence-and-restore.md](/docs/terminal-history-persistence-and-restore.md))
* **Deprecation**: Permission Evaluation and Unknown Tool Guard: Accidental deprecation from earlier call — document was just updated with new content, not deprecated. Re-verified: doc_content is current and enhanced. ([docs/gotchas/permission-evaluation-and-unknown-tool-guard.md](/docs/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Update**: Permission Evaluation and Unknown Tool Guard ([docs/gotchas/permission-evaluation-and-unknown-tool-guard.md](/docs/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Deprecation**: Permission Evaluation and Unknown Tool Guard: Already updated — no further action needed ([docs/gotchas/permission-evaluation-and-unknown-tool-guard.md](/docs/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Update**: Terminal History Persistence and Restore ([docs/terminal-history-persistence-and-restore.md](/docs/terminal-history-persistence-and-restore.md))
* **Creation**: Terminal History Persistence and Restore ([docs/terminal-history-persistence-and-restore.md](/docs/terminal-history-persistence-and-restore.md))
* **Creation**: Permission Evaluation and Unknown Tool Guard ([docs/gotchas/permission-evaluation-and-unknown-tool-guard.md](/docs/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Update**: Permission Evaluation and Unknown Tool Guard ([gotchas/permission-evaluation-and-unknown-tool-guard.md](/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Creation**: Permission Evaluation and Unknown Tool Guard ([gotchas/permission-evaluation-and-unknown-tool-guard.md](/gotchas/permission-evaluation-and-unknown-tool-guard.md))
## 2026-09-06

* **Creation**: Worktree-Based Parallel Feature Development ([architecture/worktree-based-parallel-feature-development.md](/architecture/worktree-based-parallel-feature-development.md))
* **Creation**: Sandbox git push — SSH agent inheritance and fail-closed TTY prompts ([gotchas/sandbox-git-push-ssh-agent-tty.md](/gotchas/sandbox-git-push-ssh-agent-tty.md))
## 2026-09-05

* **Update**: Auto-Permission Prompt Prose Code Audit Gap ([gotchas/auto-permission-prompt-prose-code-audit-gap.md](/gotchas/auto-permission-prompt-prose-code-audit-gap.md))
* **Update**: Seatbelt Profile Test Coverage Gap ([gotchas/seatbelt-profile-test-coverage-gap.md](/gotchas/seatbelt-profile-test-coverage-gap.md))
* **Update**: Version-Changelog Mismatch ([gotchas/version-changelog-mismatch.md](/gotchas/version-changelog-mismatch.md))
* **Update**: Auto-Permission Prompt Prose Code Audit Gap ([gotchas/auto-permission-prompt-prose-code-audit-gap.md](/gotchas/auto-permission-prompt-prose-code-audit-gap.md))
* **Update**: Seatbelt Profile Test Coverage Gap ([gotchas/seatbelt-profile-test-coverage-gap.md](/gotchas/seatbelt-profile-test-coverage-gap.md))
* **Update**: Version-Changelog Mismatch ([gotchas/version-changelog-mismatch.md](/gotchas/version-changelog-mismatch.md))
* **Update**: Auto-Permission Prompt Prose Code Audit Gap ([gotchas/auto-permission-prompt-prose-code-audit-gap.md](/gotchas/auto-permission-prompt-prose-code-audit-gap.md))
* **Update**: Seatbelt Profile Test Coverage Gap ([gotchas/seatbelt-profile-test-coverage-gap.md](/gotchas/seatbelt-profile-test-coverage-gap.md))
* **Update**: Version-Changelog Mismatch ([gotchas/version-changelog-mismatch.md](/gotchas/version-changelog-mismatch.md))
* **Update**: Version-Changelog Mismatch ([gotchas/version-changelog-mismatch.md](/gotchas/version-changelog-mismatch.md))
* **Creation**: Auto-Permission Prompt Prose Code Audit Gap ([gotchas/auto-permission-prompt-prose-code-audit-gap.md](/gotchas/auto-permission-prompt-prose-code-audit-gap.md))
* **Creation**: Seatbelt Profile Test Coverage Gap ([gotchas/seatbelt-profile-test-coverage-gap.md](/gotchas/seatbelt-profile-test-coverage-gap.md))
* **Creation**: Version-Changelog Mismatch ([gotchas/version-changelog-mismatch.md](/gotchas/version-changelog-mismatch.md))
* **Creation**: Auto-Permission Dependency Binaries — Policy Decision ([gotchas/auto-permission-dependency-bin-policy.md](/gotchas/auto-permission-dependency-bin-policy.md))
* **Creation**: Version-Changelog Mismatch ([gotchas/version-changelog-mismatch.md](/gotchas/version-changelog-mismatch.md))
* **Creation**: Auto-Permission Prompt Prose Code Audit Gap ([gotchas/auto-permission-prompt-prose-code-audit-gap.md](/gotchas/auto-permission-prompt-prose-code-audit-gap.md))
* **Creation**: Seatbelt Profile Test Coverage Gap ([gotchas/seatbelt-profile-test-coverage-gap.md](/gotchas/seatbelt-profile-test-coverage-gap.md))
* **Creation**: Auto-Permission Prompt — Load-Semantics Flip ([gotchas/auto-permission-prompt-load-semantics-flip.md](/gotchas/auto-permission-prompt-load-semantics-flip.md))
## 2026-09-03

* **Creation**: Session Storage Critical Issues ([gotchas/session-storage-critical-issues.md](/gotchas/session-storage-critical-issues.md))
* **Creation**: Sandbox Writable-Root Must Exist on Disk ([gotchas/sandbox-writable-root-must-exist.md](/gotchas/sandbox-writable-root-must-exist.md))
## 2026-09-02

* **Creation**: Radix Select Empty String Sentinel ([gotchas/radix-select-empty-string-sentinel.md](/gotchas/radix-select-empty-string-sentinel.md))
* **Creation**: Browser Panel — Documentation vs. Landed Behavior Mismatch ([gotchas/browser-panel-transition-inconsistency.md](/gotchas/browser-panel-transition-inconsistency.md))
* **Creation**: OKF Naming Convention Enforcement ([okf/_schema/naming-convention-enforcement.md](/okf/_schema/naming-convention-enforcement.md))
* **Creation**: Auto-Permission Prompt — TOCTOU Install Race ([gotchas/auto-permission-prompt-atomic-race.md](/gotchas/auto-permission-prompt-atomic-race.md))
## 2026-09-01

* **Creation**: Shell Sandbox Is Write-Integrity Only, Not Confidentiality ([architecture/shell-sandbox-integrity-only-mode.md](/architecture/shell-sandbox-integrity-only-mode.md))
* **Creation**: PATH Shadowing Can Bypass Sandbox Discovery ([gotchas/shell-sandbox-path-shadowing.md](/gotchas/shell-sandbox-path-shadowing.md))
* **Creation**: Shell Execution Must Set cmd.Dir to Agent Workdir ([gotchas/shell-sandbox-working-directory-not-set.md](/gotchas/shell-sandbox-working-directory-not-set.md))
* **Creation**: BashTool Dynamic Permission Manager Resolution ([gotchas/shell-sandbox-dynamic-bash-permissions.md](/gotchas/shell-sandbox-dynamic-bash-permissions.md))
* **Creation**: Writable-Root Validation Prevents Confinement Defeat ([gotchas/shell-sandbox-writable-root-validation.md](/gotchas/shell-sandbox-writable-root-validation.md))
* **Creation**: Shell Sandbox Backend Availability Status ([architecture/shell-sandbox-backend-status.md](/architecture/shell-sandbox-backend-status.md))
## 2026-08-31

* **Creation**: Concurrent File Editing Risk — Multiple Writers in Same Checkout ([gotchas/concurrent-file-editing-risk.md](/gotchas/concurrent-file-editing-risk.md))
* **Creation**: Auto-Permission — Judge Must See the Session WorkDir, Not the Process CWD ([gotchas/auto-permission-judge-process-cwd.md](/gotchas/auto-permission-judge-process-cwd.md))
* **Update**: Plugin Auto-Permission — Arbitrary Execution Risk ([gotchas/plugin-auto-permission-security.md](/gotchas/plugin-auto-permission-security.md))
* **Creation**: Desktop Single Instance Lock ([architecture/desktop-single-instance.md](/architecture/desktop-single-instance.md))
* **Creation**: Terminal Shells Survive Page Reload (Detach / Reattach) ([architecture/terminal-detach-reattach.md](/architecture/terminal-detach-reattach.md))
* **Update**: V1 Connection Cap Exclusion — Embedded Browser Panel ([architecture/v1-connection-cap-exclusion.md](/architecture/v1-connection-cap-exclusion.md))
* **Update**: V1 Connection Cap Exclusion — Embedded Browser Panel ([architecture/v1-connection-cap-exclusion.md](/architecture/v1-connection-cap-exclusion.md))
* **Update**: V1 Connection Cap Exclusion — Embedded Browser Panel ([architecture/v1-connection-cap-exclusion.md](/architecture/v1-connection-cap-exclusion.md))
* **Update**: V1 Connection Cap Exclusion — Embedded Browser Panel ([architecture/v1-connection-cap-exclusion.md](/architecture/v1-connection-cap-exclusion.md))
* **Update**: V1 Connection Cap Exclusion — Embedded Browser Panel ([architecture/v1-connection-cap-exclusion.md](/architecture/v1-connection-cap-exclusion.md))
* **Update**: V1 Connection Cap Exclusion — Embedded Browser Panel ([architecture/v1-connection-cap-exclusion.md](/architecture/v1-connection-cap-exclusion.md))
* **Creation**: Auto-Permission — Interpreter Scripts in Compound Commands ([gotchas/auto-permission-interpreter-scripts-in-compound-commands.md](/gotchas/auto-permission-interpreter-scripts-in-compound-commands.md))
* **Creation**: Embedded Browser — WebSocket Proxy 404 ([gotchas/embedded-browser-websocket-proxy-404.md](/gotchas/embedded-browser-websocket-proxy-404.md))
* **Creation**: V1 Connection Cap Exclusion — Embedded Browser Panel ([architecture/v1-connection-cap-exclusion.md](/architecture/v1-connection-cap-exclusion.md))
## 2026-08-30

* **Creation**: Journal DB Connection Pool Leak ([gotchas/journal-db-connection-pool-leak.md](/gotchas/journal-db-connection-pool-leak.md))
* **Creation**: Git ext:: Transport Auto-Permission Bypass ([gotchas/git-ext-transport-auto-allow-bypass.md](/gotchas/git-ext-transport-auto-allow-bypass.md))
## 2026-08-28

* **Update**: Plugin Auto-Permission — Arbitrary Execution Risk ([gotchas/plugin-auto-permission-security.md](/gotchas/plugin-auto-permission-security.md))
* **Creation**: Chat Input — Queued Messages Lost on Submission Failure ([gotchas/chat-input-message-loss.md](/gotchas/chat-input-message-loss.md))
* **Creation**: ocode debug CLI Reference ([guides/debug-cli.md](/guides/debug-cli.md))
* **Update**: Plugin Removal — Root Directory Deletion Risk ([gotchas/plugin-removal-root-deletion.md](/gotchas/plugin-removal-root-deletion.md))
* **Update**: Debug Instrumentation Ships Unconditionally ([gotchas/debug-instrumentation-ships-unconditionally.md](/gotchas/debug-instrumentation-ships-unconditionally.md))
* **Update**: Plugin Install Rollback Bug ([gotchas/plugin-install-rollback-bug.md](/gotchas/plugin-install-rollback-bug.md))
* **Creation**: FilePicker.test.tsx Stale Build Status — Corrected ([gotchas/filepicker-stale-todo.md](/gotchas/filepicker-stale-todo.md))
* **Creation**: Debug Instrumentation Ships Unconditionally ([gotchas/debug-instrumentation-ships-unconditionally.md](/gotchas/debug-instrumentation-ships-unconditionally.md))
* **Creation**: Plugin Auto-Permission — Arbitrary Execution Risk ([gotchas/plugin-auto-permission-security.md](/gotchas/plugin-auto-permission-security.md))
* **Creation**: Plugin Removal — Root Directory Deletion Risk ([gotchas/plugin-removal-root-deletion.md](/gotchas/plugin-removal-root-deletion.md))
* **Creation**: Plugin Install Rollback Bug ([gotchas/plugin-install-rollback-bug.md](/gotchas/plugin-install-rollback-bug.md))
## 2026-08-27

* **Update**: Plugin System ([plugins.md](/plugins.md))
* **Creation**: Symlink Escape in Plugin Removal Validation ([gotchas/plugin-removal-symlink-escape.md](/gotchas/plugin-removal-symlink-escape.md))
## 2026-08-26

* **Update**: Foreground Bash Commands — Parent-Death Protection Before Start ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Update**: Foreground Bash Commands — Parent-Death Protection Before Start ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Update**: Foreground Bash Commands — Parent-Death Protection Before Start ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Update**: Foreground Bash Commands — Parent-Death Protection Before Start ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Update**: Foreground Bash Commands — Parent-Death Protection Before Start ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Creation**: Bubble Tea Cleanup Race — Orphaned Goroutines on Shutdown ([gotchas/bubbletea-cleanup-race.md](/gotchas/bubbletea-cleanup-race.md))
* **Creation**: Foreground Bash Commands — Parent-Death Protection Before Start ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Creation**: Agent Replacement — Input Queuing & Stream Event Epochs ([gotchas/agent-replacement-input-queuing.md](/gotchas/agent-replacement-input-queuing.md))
* **Creation**: AIHubMix Test — Global Cache State Leakage ([gotchas/aihubmix-test-cache-leak.md](/gotchas/aihubmix-test-cache-leak.md))
* **Creation**: Local Model Limiter — Stale-Slot Reclamation Race ([gotchas/local-model-limiter-stale-slot-race.md](/gotchas/local-model-limiter-stale-slot-race.md))
* **Creation**: Local Model Auto-Start Hijacks the Controlling Terminal ([gotchas/local-model-tty-hijack.md](/gotchas/local-model-tty-hijack.md))
* **Creation**: run_in_background Bash Commands Orphaned on Force-Killed ocode ([gotchas/background-bash-orphan-on-force-kill.md](/gotchas/background-bash-orphan-on-force-kill.md))
## 2026-08-20

* **Creation**: Shared LSP Broker Server-State and Routing Gotchas ([architecture/lsp-broker-shared-server-gotchas.md](/architecture/lsp-broker-shared-server-gotchas.md))
## 2026-07-22

* **Creation**: Skill Tool Test Fixture Gap — expectedBuiltinTools Missing load_skill ([gotchas/skill-tool-test-fixture-gap.md](/gotchas/skill-tool-test-fixture-gap.md))
## 2026-07-11

* **Creation**: Subagent Feedback-Loop Guard (task tool) ([gotchas/subagent-feedback-loop-guard.md](/gotchas/subagent-feedback-loop-guard.md))
## 2026-07-09

* **Creation**: Sidebar TUI/Web Parity Gaps ([architecture/sidebar-tui-parity-gaps.md](/architecture/sidebar-tui-parity-gaps.md))
* **Creation**: ChatPanel Autoscroll Bounce/Freeze ([gotchas/autoscroll-bounce.md](/gotchas/autoscroll-bounce.md))
## 2026-07-08

* **Creation**: File-Edit Snapshot & Undo Mechanism ([file-edit-snapshot.md](/file-edit-snapshot.md))
## 2026-07-06

* **Update**: Session Title Generation & UI Update Root Cause Analysis ([title-generation-analysis.md](/title-generation-analysis.md))
* **Update**: Plugin System ([plugins.md](/plugins.md))
* **Update**: Zed-compatible ACP Mode Specification ([acp-zed-spec.md](/acp-zed-spec.md))
* **Update**: Using ocode with Zed ([zed.md](/zed.md))
* **Creation**: Knowledge Bundle System ([knowledge-bundle.md](/knowledge-bundle.md))