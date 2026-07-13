# ocode Telegram bot

Drive every running `ocode` instance from Telegram. The bot is a **local
companion**: it runs on the same machine as ocode, discovers your running
instances via a local registry, and relays messages to each instance's
built-in `/rc` web server — the same endpoint the web UI uses. Only Telegram
is external, so no public ingress or port-forwarding is required.

## How it maps to `/rc`

- Each `ocode` TUI that runs `/rc` writes a small entry into
  `~/.local/share/opencode/rc/instance-*.json` (session id, model, working
  directory, listen address, and auth token), and keeps it fresh with a
  heartbeat. `/rc off` (or a crash, via TTL pruning) removes it.
- The bot reads that registry and offers every live instance as a selectable
  "session" — this is the multi-instance equivalent of the web `/rc` page, but
  for *all* your running ocode processes at once.
- A message sent in Telegram opens an SSE stream to the selected instance's
  `/api/chat/stream`, and the agent's token/tool output is streamed back and
  edited into the Telegram message (batched to respect Telegram's ~1/sec edit
  rate limit).

## Setup

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy the token.
2. Build the binary:
   ```sh
   go build ./cmd/ocode-telegram
   ```
3. Run it (same user/machine as your ocode instances):
   ```sh
   export OCODE_TG_TOKEN="<token from BotFather>"
   # optional: restrict to specific Telegram user ids (comma-separated)
   export OCODE_TG_ALLOWED_USERS="123456789,987654321"
   ./ocode-telegram
   ```
4. In each `ocode` TUI you want to control, run `/rc` (optionally `/rc <port>`).
   The instance appears in Telegram via `/sessions`.

## Commands (in Telegram)

| Command | Description |
|---------|-------------|
| `/sessions` | List running ocode instances (pick one with the inline buttons) |
| `/session <id>` | Select an instance to talk to (matches instance or session id fragment) |
| `/current` | Show the selected instance |
| `/yolo on` / `/yolo off` | Toggle autonomous mode for the selected instance (no approval prompts) |
| `/help` | Show help |

Any other message is sent to the selected instance and streamed back.

## Permissions & safety

- **Inline approve/deny from anywhere.** When a tool call needs approval during a
  bot-driven (`/rc`) turn, the agent pauses and ocode emits a `permission` event
  over the SSE stream. The **Telegram bot renders an inline keyboard**
  (✅ Approve / ✅ Approve always / ❌ Deny) and the **web `/rc` UI renders its
  existing `PermissionDialog`** — both wired to the same resolution endpoints.
  Tap a button and the decision is posted back; the turn resumes automatically.
  No terminal approval needed.
- **Questions, too.** A `question` tool prompt emits a `question` event; the bot
  renders one button per option (and the web `/rc` UI shows its `QuestionDialog`).
  For prompts with **multiple questions** or **multi-select** options, the bot
  lets you toggle each option (✓ prefix) and tap **✅ Submit answers** to send the
  full selection set at once — so multi-question and multi-select prompts are
  answered correctly, not just the first one. The web `QuestionDialog` supports
  the same, and the bridge now carries every selected answer end-to-end
  (`tool.QuestionAnswerSet`).
- **Free-text ("Other") answers.** Each question also gets an **✏️ Other** button
  (synthesized when the prompt doesn't include one, matching the TUI). Tapping it
  pops up Telegram's reply box; your **next message becomes the answer**. For a
  single-question prompt this resolves in **one step** (no extra confirm). The
  custom text is sent as a `custom: true` answer, identical to the web UI.
- How it works: the TUI keeps the ask paused and listens on a dedicated resolve
  channel. The bot (or web UI) POSTs to `/api/rc/permission/resolve`
  (`{request_id, decision}`) or `/api/rc/question/answer` (`{request_id,
  answers}`) — both require the instance's RC auth token. The TUI applies the
  resolution (reusing its own permission/question resume paths, including
  "approve always" rule persistence and harmful-request blocking) and broadcasts
  a `permission_resolved` / `question_resolved` event so every client dismisses
  its dialog.
- The **web `/rc` UI** reuses its existing `PermissionDialog`/`QuestionDialog`
  (driven by the session-mirror SSE). Its existing `resolvePermission` /
  `submitQuestionAnswers` calls now forward to the TUI bridge when a session is
  bridged, so no web code changes were needed — the same dialogs that work in
  headless serve mode now work for RC sessions.
- `/yolo on` flips the selected instance's agent into YOLO mode remotely
  (the bot calls `PUT /api/permissions/yolo` on that instance). Use it only for
  trusted, low-risk automation — it lets the agent run `bash` and edit files
  without prompts.
- Restrict the bot with `OCODE_TG_ALLOWED_USERS` so only your Telegram account
  can drive your instances. Each instance's auth token lives only in the local
  registry file (same user, same machine) — it is never sent over Telegram.

## Limitations

- Telegram caps messages at 4096 characters; long assistant replies are
  trimmed to the most recent content, and full tool output is attached as a
  `tool-output.txt` document.
- Telegram inline-keyboard callbacks are limited to 64 bytes, so the bot maps
  each pending ask to a short random key and stores the `request_id` → instance
  mapping locally (the selection state also lives server-side, keyed by that
  short token); the web `/rc` UI uses the `request_id` directly.
- The bot talks to instances over `localhost`; it is designed to run alongside
  ocode, not as a shared multi-user service.

## Implementation pointers

- `internal/rc/registry.go` — instance discovery (per-instance JSON files +
  heartbeat + TTL pruning).
- `internal/tui/model.go` — `/rc` registers/heartbeats/unregisters an instance.
- `internal/server/handler_permissions.go` — YOLO/permission toggles also apply
  to the RC (TUI) agent so the bot can switch modes remotely.
- `internal/telegram/*` — minimal Bot API client, SSE reader, and bot logic
  (Telegram inline keyboards + `perm:`/`q:` callback routing).
- `internal/server/handler_rc_resolve.go` — `POST /api/rc/permission/resolve`
  and `POST /api/rc/question/answer` (Telegram → TUI bridge resolution).
- `internal/server/handler_permissions_resolve.go` /
  `handler_questions.go` — the existing web resolvers now **also** forward to
  the TUI bridge when a session is bridged (so the web `/rc` UI's existing
  `PermissionDialog`/`QuestionDialog` work for RC sessions too).
- `internal/tui/model.go` — `appendAgentMessage` emits `permission`/`question`
  SSE events (to the bot via `StreamCh` and to the web via `broadcastRC`) and
  pauses; `rcResolveMsg` applies the resolution through the same resume paths
  the local dialog uses (`handlePermissionChoice`, `submitRCQuestionAnswers`).
- `internal/tool/misc.go` — `QuestionAnswer` is a single selection; `QuestionAnswerSet`
  is the per-question answer set (all selections, multi-select aware) carried by
  the RC bridge so multi-question/multi-select prompts survive end-to-end.
- `cmd/ocode-telegram/main.go` — entrypoint (env: `OCODE_TG_TOKEN`,
  `OCODE_TG_ALLOWED_USERS`, `OCODE_TG_RC_DIR`).
