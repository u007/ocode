# Embedded HTR Extension and Managed Daemon Design

**Date:** 2026-09-09
**Status:** Approved for implementation

## Goal

Embed the HTR NControl extension and a platform-matched `htrcli` daemon in ocode's managed Chrome flow without changing the user's existing Chrome installation, profile, extension set, or standalone HTR daemon.

## Defaults and configuration

HTR is enabled by default for ocode-managed Chrome only. External/user Chrome is never modified or controlled by this integration. Setting `browser.htr.enabled: false` restores managed Chrome without HTR.

The integration is parameterized through browser options and config:

- `enabled` (default `true`)
- `extension_path` (optional development override; empty uses the embedded extension)
- `htrcli_path` (optional development override; empty uses the embedded platform binary)
- `daemon_port` (default `3846`, separate from standalone htrcli's `3845`)
- `socket_path` (optional; empty uses an ocode-managed socket)
- `native_host_name` (default `com.ocode.htrcontrol`)

## Build and packaging

`make install` and `make desktop-app` depend on a preparation target that locates `HTRCLI_ROOT` (default `../how-to-recorder/htrcli`) and `HTR_EXTENSION_DIR` (default `../how-to-recorder/build`), validates the extension manifest, builds `htrcli` for the target OS/architecture, and embeds both assets in the resulting ocode artifact. Missing source inputs produce an actionable build error.

The embedded extension uses a dedicated native host name and stable identity. Existing HTR builds retain `com.htrcontrol.host`; ocode's embedded build uses `com.ocode.htrcontrol` and does not overwrite the existing host registration.

## Runtime isolation

At runtime ocode extracts assets under its global data directory in a versioned HTR runtime directory. Installation is serialized across ocode processes, uses unique temporary paths plus atomic replacement, and records the embedded archive digest so partial or stale assets are repaired. Managed Chrome receives a dedicated profile, the extracted extension, and environment/options identifying the managed daemon socket. Native-host registration is namespaced and is created/updated only for the ocode host; existing registrations are preserved.

## Daemon lifecycle

The bundled daemon is launched with explicit port, socket, managed identity, and managed-lease options and with the tray disabled. Ocode processes acquire leases and refresh heartbeats through the shared lock using atomic writes. Health responses must prove the htrcli service, managed identity, port, and socket before a process is reused or terminated. The owner record also validates PID, executable, and process-start token before cleanup. A daemon waiter marks crashed processes exited so a later startup can replace the terminal supervisor record. The daemon remains alive while any lease is valid, expires crashed-process leases, and shuts itself down after the final lease expires. Normal ocode shutdown releases its lease. This is shared across multiple ocode and desktop processes and does not terminate an independently launched standalone htrcli daemon.

## Failure behavior

HTR startup is best effort. If extraction, native-host setup, daemon startup, port binding, browser-family registration, or extension launch fails, ocode logs the reason, starts managed Chrome without HTR, and keeps browsing available. Branded Google Chrome's unsupported unpacked-extension preload is handled the same way. The browser panel displays a persistent actionable notice identifying the failure; there is no silent fallback.

## Cross-platform behavior

The build produces the matching `htrcli` executable for macOS, Linux, and Windows targets. Native-host installation selects the configured browser family (Chrome, Chromium, Canary, Edge, or Brave) and uses each platform's supported mechanism. Process cleanup uses the existing ocode process-supervision abstractions plus the daemon lease timeout for crash recovery. The Windows daemon and packaging path are buildable, but ocode's embedded Chrome CDP pipe remains explicitly gated by `ErrUnsupportedPlatform`; this is not a claim of full Windows browser-panel support.

## Validation

Tests will cover configuration defaults and overrides, asset selection, platform-specific command construction, native-host isolation, shared leases, last-owner shutdown, crashed-owner expiry, startup fallback notices, and unchanged behavior when HTR is disabled.
