---
type: Guide
title: Computer use
description: User-facing guide to enabling and using the opt-in computer desktop-control tool, including actions, coordinate mapping, permissions, platform setup, limitations, and privacy.
resource: internal/tool/computer.go; internal/computer/; internal/config/computeruse_config.go; internal/agent/permissions.go; internal/tui/model.go; internal/server/handler_config.go
tags:
  - computer-use
  - desktop
  - permissions
  - platforms
timestamp: 2026-09-13T07:21:43Z
---
# Computer use

Computer use adds an opt-in `computer` tool that lets the agent see and operate the desktop running ocode. It is disabled by default. When disabled, the tool is not advertised to the model.

## Supported sessions

Computer use is exposed in TUI and web/server sessions. ACP and headless `ocode run` sessions do not expose the tool because those modes have no interactive process supervisor to manage computer use.

## Enable computer use

In the TUI or web chat, use:

```text
/computer status
/computer enable
/computer disable
```

Enabling or disabling is persisted, but takes effect in **new sessions**. Start a new session after changing the setting.

The same setting can be stored in `ocodeconfig.json`:

```json
{
  "computer_use": {
    "enabled": true
  }
}
```

`/computer status` reports whether the setting is enabled and names the platform backend. This is platform backend information, not a readiness or health check: status does not probe the desktop, verify installed Linux binaries, or start a driver. On macOS it also prints the required permission reminder.

## Actions

The `computer` tool accepts one `action` per call:

| Action | Meaning |
| --- | --- |
| `screenshot` | Capture the primary display and return the screenshot to the model. |
| `left_click` | Click once with the left mouse button. |
| `right_click` | Click once with the right mouse button. |
| `middle_click` | Click once with the middle mouse button. |
| `double_click` | Double-click with the left mouse button. |
| `mouse_move` | Move the cursor to a location. |
| `left_click_drag` | Drag from `start_coordinate` to `coordinate` with the left button. |
| `scroll` | Scroll at a location in one of the `up`, `down`, `left`, or `right` directions. `scroll_amount` defaults to 3 ticks. |
| `type` | Type the supplied text into the active application. |
| `key` | Press a key or key combination, such as `ctrl+s`, supplied as the action text. |
| `cursor_position` | Return the current cursor location. |
| `wait` | Wait for the requested duration, from 0 to 10 seconds. This does not take a new screenshot. |

## Screenshot coordinates

Mouse coordinates are pixel coordinates in the **image returned by the most recent screenshot**, with `[0, 0]` at the image's top-left. They are not necessarily the desktop's native coordinate units. The tool downscales large screenshots so their longest side is at most 1568 pixels, then maps image coordinates back to the operating system's input coordinate space internally.

Take a screenshot before interacting so the model is using current geometry. If an input action is sent before the first explicit screenshot, ocode takes an initial capture to establish the scale. Use `coordinate: [x, y]` for clicks, moves, scrolling, and the destination of a drag; use `start_coordinate: [x, y]` for the beginning of a drag.

## Permissions

Computer-use permissions are evaluated per action:

- `screenshot`, `cursor_position`, and `wait` are observational and are allowed without a prompt in normal permission mode.
- Mouse, keyboard, typing, dragging, and scrolling actions require explicit approval by default. The permission request identifies the action and its coordinates or text/key summary.
- Choosing **Always allow** for the computer tool persists the `tool.computer` permission rule according to the selected permission scope.
- Locked permission mode denies all computer actions, including screenshots and cursor queries.

The computer driver operates on the host desktop; it is not a shell command and is not covered by the shell sandbox.

## Platform prerequisites

### macOS

The driver uses the built-in `screencapture` command for screenshots and a JXA helper (run through `osascript`) that posts CoreGraphics events for mouse and key input. Grant these permissions to the terminal that launched ocode or to `ocode-desktop`:

- **Screen Recording** — System Settings > Privacy & Security > Screen Recording
- **Accessibility** — System Settings > Privacy & Security > Accessibility
- **Automation → System Events** — macOS prompts for this the first time `type` is used; approve it once.

Without Screen Recording, macOS returns a wallpaper-only screenshot without an error. Without Accessibility, input actions fail with a permission notice (the helper checks `AXIsProcessTrusted` before posting any event).

Typing on macOS has two paths, because CoreGraphics unicode typing cannot be driven from the JXA bridge:

- Printable ASCII, newlines, and tabs are typed through System Events `keystroke`, with Return and Tab sent as key events.
- Text containing any other character is placed on the general pasteboard and pasted with cmd+v. The pasteboard is **not restored** afterwards (a restore races the asynchronous paste), so the typed text stays on the clipboard.

Key combinations use `+` and xdotool-style names: modifiers `cmd`, `ctrl`, `alt`, `shift`; keys such as `Return`, `Tab`, `Escape`, `space`, `BackSpace`, `Delete`, arrows, `Home`, `End`, `Page_Up`, `Page_Down`, `F1`–`F12`, letters, digits, and `minus`, `equal`, `comma`, `period`, `slash`, `semicolon`, `apostrophe`, `bracketleft`, `bracketright`, `backslash`, `grave`.

### Windows

The driver uses PowerShell, Windows input APIs, and the built-in screen and drawing APIs. No additional package is required, but `powershell` must be available on `PATH`.

### Linux

The backend is selected from the session type:

- **X11:** install `xdotool` and `scrot` (for example, `apt install xdotool scrot`).
- **Wayland:** install `ydotool` and `grim` (for example, `apt install ydotool grim`). Wayland support requires a wlroots compositor; `ydotool` also needs its daemon running and access to uinput.

The Wayland screenshot path is intended for wlroots compositors; `grim` does not provide the required support on GNOME or KDE. Missing Linux binaries produce an install hint when an action is attempted.

## Limitations and privacy

- Screenshots and input target the **primary display only**. There is no multi-display selection or region-zoom support in this version.
- The driver acts on the machine running the ocode process. It does not operate the desktop of a remote SSH workspace.
- A screenshot can contain passwords, tokens, private messages, or any other content visible on the primary display. Review the screen before enabling computer use: screenshots returned to the agent may be included in requests sent to the configured model provider. Input actions can also type into whichever application currently has focus.
