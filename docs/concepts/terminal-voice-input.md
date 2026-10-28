---
type: Concept
title: Terminal voice input (mic button in the web Terminal panel)
description: A mic button in the web/desktop Terminal panel records with the shared recorder and pastes the transcript into the pty as one bracketed paste, never sent. It bypasses the phone keyboard's IME, which mangles dictation into xterm's hidden textarea.
tags:
  - terminal
  - voice
  - stt
  - mobile
  - ime
  - xterm
  - web-ui
timestamp: 2026-10-27T17:30:00Z
---

# Terminal voice input

The Terminal panel has a mic button. Tap to start recording, tap again to stop.
The clip goes through the same speech-to-text path as the chat composer
(`POST /api/stt/transcribe`, see [speech-to-text.md](speech-to-text.md)), and the
transcript is pasted into the shell as one paste. Nothing is sent: the user reviews
it and presses Return.

## Why not the phone's own dictation

On a phone, the OS keyboard's built-in mic dictates into whatever element holds
focus. For the terminal that element is xterm's hidden input textarea. Composition
(IME) rewrites text in place, and xterm diffs that textarea's value to decide what
to send. Dictated words land wrong, duplicated, or not at all. The in-app button
never touches the IME path: it records audio itself and writes the finished text
through `term.paste`. `terminalImeHygiene.ts` covers the keyboard path separately.

## How it fits together

| Piece | Role |
| --- | --- |
| `web/src/components/Terminal/useTerminalVoice.ts` | Terminal hook. Wraps the shared `useVoiceRecorder`, owns the install confirm state, forwards recorder errors to the app toast. |
| `web/src/components/Chat/useVoiceRecorder.ts` | Shared recorder (MediaRecorder + getUserMedia). Used as-is. |
| `web/src/components/Terminal/TerminalPanel.tsx` | Renders the button, gates it on the shell being attached, and pastes the transcript (`insertVoiceTranscript`). |
| `api.transcribeSpeech` / `api.prewarmSpeech` | The existing client calls. |

`shellReady` is React state set when the server's `attach` frame arrives on the
live socket, and cleared when that socket is replaced or closes. `attachedRef`
still gates `term.onData`, so the button is enabled only when a paste would reach
the pty.

## Behaviour

- **Insertion.** The transcript is trimmed, control characters (ESC, 0x03/0x04) and newlines collapse to single
  spaces, then `term.paste(text)` runs once. `term.paste` honours bracketed-paste
  mode, so a TUI that enabled it (Claude Code) receives it as one paste. Without
  that mode, no newline means no Return. `/voice status` documents insert-at-cursor (not a next-message send). Afterwards `term.focus()` is called.
- **Connection re-checked at paste time.** Transcription takes seconds. If the
  socket dropped meanwhile, nothing is pasted (`term.paste` would be silently
  dropped), and the app toast shows the transcript so the user can copy it.
- **Empty transcript.** The recorder reports "Nothing was heard. Try again." and
  nothing is pasted.
- **Recording cap.** A recording stops automatically after 5 minutes (`VOICE_MAX_RECORDING_MS`); the clip is transcribed as if the user tapped stop.
- **Install-required.** Same contract as the chat composer. A 409
  `stt_install_required` opens the shared `ConfirmDialog` (never `window.confirm`,
  which returns false in the desktop webview). Approve retries once with
  `installApproved`. Decline reports that nothing was installed. A failed install
  shows the server's pip tail prefixed "Voice dependency install failed:".
- **Disconnected.** The button is natively disabled while idle and not attached,
  with the title "Terminal is not connected". Once recording has started it stays
  enabled, so the user can always stop. A stop after a drop reports the transcript
  instead of pasting it.
- **Session-bound.** A recording is tied to the session it started in; switching sessions stops it, and a transcript that arrives for the old session is dropped with a notice (never inserted into another chat's terminal).
- **Unsupported.** Without `getUserMedia` or `MediaRecorder` the button is
  `aria-disabled`, not disabled, so a tap can explain why. On a plain-HTTP page
  reached from a phone on the LAN, the message is "Voice input needs a secure
  connection: open ocode over HTTPS or localhost", because `window.isSecureContext`
  is false there. Other browsers get `VOICE_UNAVAILABLE_MESSAGE`.
- **Errors.** Every recorder failure (mic denied, nothing heard, transcription or
  install failure) is logged with `console.error` and shown in the app-wide
  `ActionErrorToast` via `reportActionErrorMessage`. Sticky until dismissed.

## Focus and gestures

- The button's `onMouseDown` calls `preventDefault`, so tapping it does not move
  focus out of the terminal. That keeps the phone keyboard from dropping and
  keeps desktop keystrokes going to the shell.
- The button sits inside the container with `data-terminal-chrome`. Without it a
  tap reads as a terminal press: the selection snapshot is reset and copy-on-select
  arms.
- The `ConfirmDialog` is rendered as a sibling of the container, not inside it.
  React bubbles portal events through the component tree, so a click in the dialog
  would otherwise reach the container's mouse handlers.

## Placement

Bottom-right overlay (`absolute bottom-3 right-3 z-10`), 44px round touch target.
The status chip ("Recording… tap to stop", "Transcribing…", the install message)
sits to its left. The find bar is top-right, so the two do not overlap.

## Remote projects

Transcription runs on whichever server the browser talks to, the same as the chat
composer. The paste goes out over the terminal socket, which for a remote project
is proxied to the host. A remote failure surfaces as a socket drop, so the
disconnected path above applies.

## Known limits

- Tap to toggle only. The chat composer's press-and-hold lock is not reproduced.
- No live partial transcript and no cancel control while recording. Tapping stop
  always transcribes.
- A transcript that arrives after a socket drop is shown in the toast, not
  inserted. It is not copied to the clipboard.
- Tested in jsdom only (`web/src/components/Terminal/TerminalPanel.voice.test.tsx`
  plus the hook's error path). Phone behaviour is unverified: focus retention on
  iOS Safari and Android Chrome, and the overlay's clearance from Claude Code's
  prompt row, need a real device check.
