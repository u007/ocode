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
timestamp: 2026-11-15T17:17:52Z
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

`callGeminiInteraction` posts the body through `credentialedHTTPClient` (no redirects) and parses `steps`/`status`/`error` into
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
`timeout=300`, `voice=Kore`. The credential rules are in
[Credentials and base_url](#credentials-and-base_url).

## Credentials and base_url

The stored provider key goes only to the endpoint the user configured. Each tool takes an optional `api_key`, and a model can also name a `base_url`.

- **No `base_url`:** videogen and audiogen resolve the key through `geminiGenerativeCredentials` (`internal/tool/interactions_api.go`): the `api_key` argument, then `auth.ResolveKey("google")`. The missing-key error names `GOOGLE_API_KEY`, `provider.google.options.apiKey` and `/connect`. The request goes to the configured endpoint (`auth.GetBaseURL("google")`), else the provider default. imagegen resolves the key and base URL for its provider id (`prov.id`) the same way.
- **`base_url` with no `api_key`:** refused before any key is resolved, with `custom base_url requires an explicit api_key: the stored key is only sent to the configured endpoint` (`requireExplicitKeyForBaseURL`). This also covers imagegen's unregistered-provider branch: a real auth provider id such as `anthropic` would otherwise resolve that provider's stored key for the caller's URL. The check is strict, so a `base_url` equal to the configured one is refused too.
- **Redirects:** every request that carries a key goes through `credentialedHTTPClient`, which returns a 3xx as the response instead of following it. Go copies custom headers (`x-goog-api-key`, the bearer token) onto a redirect target, so a followed redirect would hand the key to the host it names.
- **Egress guard:** a non-empty `base_url` is an outbound target for the [outbound-network guardrail](webfetch-websearch-guardrails.md). The configured endpoint is not judged. These tools are outside the allowed-URL skip.

## Surfaces

- `GET`/`PUT /api/config/ocode/videogen` and `/audiogen` (`internal/server/server.go`).
- TUI `/video` and `/audio` (`internal/tui/media_gen_cmd.go`): `status`,
  `enable`, `disable`, `model`, aspect/resolution or voice, `timeout`.
- Web/desktop Settings → Voice & Media → Video Generation / Audio Generation.
- TUI status: `video_gen_*` and `audio_gen_*` (`internal/server/tui_status.go`).

## Tests and open gaps

`internal/tool/generative_media_test.go` (fake server), the config test and
`internal/tui/media_gen_cmd_test.go`. The 2026-11-16 pins are
`TestBaseURLWithoutAPIKeyNeverSendsStoredKey` and
`TestCredentialedRequestsDoNotFollowRedirects`. The egress side is
`TestExtractEgressTargetsGenerativeBaseURL` in `internal/agent/network_guard_typesafe_test.go`.

Neither tool has been run against the live APIs; model ids may drift, and both are overridable per call and in Settings (`TODO.md`).

Image-URL download: `fetchBytes(rawURL, baseURL)` (`internal/tool/imagegen.go`) downloads the image URL a provider response names. It sends no credentials. `imageURLRefusal` checks the URL and every redirect hop (5 at most): it refuses non-http(s) schemes and link-local or cloud-metadata addresses (`BrowserURLDenyReason`), and refuses a loopback or private target unless the API base host is itself loopback or private (`isLoopbackOrPrivateHost`), so a local image server still works. Known limit: only literal hosts are checked; a public name that resolves to a private address is not caught, and the network guard does not judge this download. Pinned by `TestOpenAIURLImageFetchGuard`.
