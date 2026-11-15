---
type: Concept
title: Generative media tools (imagegen, videogen, audiogen)
description: 'The three built-in Gemini media tools: imagegen (Nano Banana default, plus OpenAI/Novita/DeepInfra), videogen on the Interactions API (gemini-omni-1.2-flash, .mp4), and audiogen Gemini TTS single/multi-speaker (.wav). Shared helpers, 256 MB cap, confined output paths, disabled-by-default registration, config/HTTP/TUI/Settings surfaces, credentials, and open gaps.'
resource: internal/tool/videogen.go
tags:
  - tools
  - media
  - gemini
  - config
timestamp: 2026-11-15T11:25:55Z
---
# Generative media tools

`videogen` and `audiogen` are new built-ins beside `imagegen`. All three save a
file and return its path; the new two reuse its credential and path rules.

## The three tools

- `imagegen` — Gemini `generateContent`, or an OpenAI-compatible
  `/images/generations` for `openai`, `novita`, `deepinfra`. Default
  `gemini-3.1-flash-image` (Nano Banana). Logs cost.
- `videogen` — `POST {base}/v1beta/interactions`, default
  `gemini-omni-1.2-flash`, writes `.mp4`. No cost logging.
- `audiogen` — same endpoint, default `gemini-3.8-flash-tts`, writes `.wav`.
  No cost logging.

## videogen (`internal/tool/videogen.go`)

`Execute` validates `aspect_ratio` (16:9, 9:16) and `resolution` (360p, 720p,
1080p, 4k), then sends `{model, input, response_format:{type:"video",
aspect_ratio, resolution}}`. The helpers keep `model_output` steps only and
base64-decode the last part whose `type` is `video`. Veo 3.1 Preview shut down on
2026-10-22, so this streaming endpoint replaced `predictLongRunning`.

## audiogen (`internal/tool/audiogen.go`)

- **Single-speaker:** `text` plus optional `voice`/`style`. The text is one
  `user_input` part (a `speech_metadata` style annotation when set);
  `speech_config` is `[{"voice": ...}]`.
- **Multi-speaker:** `turns[{speaker,text,style}]` plus `speakers[{name,voice}]`.
  Each turn is a text part annotated `speech_metadata: {speaker, style}`, and
  `speech_config` is `{"speakers":[{"speaker": name, "voice": ...}]}`. An
  undeclared or unused speaker is an error.

## Shared helpers (`internal/tool/interactions_api.go`)

`callGeminiInteraction` posts the body and parses `steps`/`status`/`error` into
`model_output` parts; `lastInteractionData` decodes the last part of a type;
`saveMediaFile` writes the bytes and reuses `confinedPath` (outside the allowed
roots a path degrades to its base name under the working directory). The body
cap is `256 << 20`, so an oversized response fails instead of truncating.

## Registration and config

Both tools are always registered in `internal/tool/tool.go`, so the model knows
they exist; until enabled, `Execute` returns a `NoticedError` pointing at
`/video enable` or `/audio enable`. Config lives in
`internal/config/generative_media_config.go`: video `enabled=false`,
`timeout=900`, `aspect_ratio=16:9`, `resolution=720p`; audio `enabled=false`,
`timeout=300`, `voice=Kore`. The key resolves through
`geminiGenerativeCredentials` (argument, then `auth.ResolveKey("gemini")`:
GOOGLE_API_KEY, opencode config, auth store, with `x-goog-api-key`).

## Surfaces

- `GET`/`PUT /api/config/ocode/videogen` and `/audiogen` (`internal/server/server.go`).
- TUI `/video` and `/audio` (`internal/tui/media_gen_cmd.go`): `status`,
  `enable`, `disable`, `model`, aspect/resolution or voice, `timeout`.
- Web/desktop Settings → Voice & Media → Video Generation / Audio Generation.
- TUI status: `video_gen_*` and `audio_gen_*` (`internal/server/tui_status.go`).

## Tests and open gaps

`internal/tool/generative_media_test.go` (fake server), the config test and
`internal/tui/media_gen_cmd_test.go`. Neither tool has been run against the live
APIs; model ids may drift, and both are overridable per call and in Settings
(`TODO.md`).
