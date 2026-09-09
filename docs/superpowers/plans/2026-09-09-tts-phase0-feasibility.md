# TTS Phase 0 Feasibility Record

**Date:** 2026-09-09  
**Design:** `docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md`  
**Implementation plan:** `.opencode/plans/2026-09-09-tts-speech-playback.md`

## Exit decision

Browser Native is the only enabled v1 engine in this implementation. Piper,
Kokoro, Fish Audio, and Breeze remain visible in the flat selector with an
explicit `unavailable` status. No runtime URL, checksum, launch command, or
license assumption is invented for an unavailable engine.

This satisfies the Phase 0 exit condition: every local entry has either a
pinned/verifiable manifest or an explicit unavailable state.

## Findings

| Engine | Verified upstream facts | v1 status | Blocker |
| --- | --- | --- | --- |
| Browser Native | Uses the browser `SpeechSynthesis` API; no server artifact or model is required. | **Enabled** | Browser/user-agent support and duration/seek events are inherently best-effort. |
| Piper | Current upstream moved to `OHF-Voice/piper1-gpl`. The current installation path is a Python package and the project is GPL-licensed; voices/models have separate model-card licensing. | **Unavailable** | No ocode-owned, redistributable, pinned runtime + voice artifact pair and checksums; packaging/licensing review is required before enabling. |
| Kokoro | Official repository documents Apache-licensed weights and Python/PyTorch inference, emitting 24 kHz WAV. ONNX runtimes/exports exist, but are separate deployment artifacts. | **Unavailable** | No verified pinned cross-platform runtime/export + voice artifacts and checksums in the repository. |
| Fish Audio | Current Fish Speech repository states that code and weights use the Fish Audio Research License. The documented S2 path depends on a substantial Python/SGLang/vLLM stack. | **Unavailable** | License terms and runtime packaging are not approved for an ocode-managed distribution; no pinned artifacts/checksums or stable ocode synthesis protocol. |
| Breeze | Breeze TTS 2 model metadata states that weights/checkpoints/adapters/outputs are governed separately by the BreezeBlue Research and Non-Commercial License. | **Unavailable** | Non-commercial model terms require an explicit product/legal decision; no pinned runtime/model manifest or cross-platform fallback artifact is verified. |

Sources checked on 2026-09-09:

- Piper: `https://github.com/OHF-Voice/piper1-gpl` and its README.
- Kokoro: `https://github.com/hexgrad/kokoro` and its README.
- Fish Speech: `https://github.com/fishaudio/fish-speech` and its README/license notice.
- Breeze: `https://github.com/breezeblue-ai/breeze-tts` and
  `https://huggingface.co/BreezeBlue/Breeze-TTS-2` model metadata.

## Artifact policy

When an engine is enabled in a later change, its manifest must pin, for each
supported `GOOS`/`GOARCH` and hardware mode:

- the runtime and model/voice URLs;
- SHA-256 checksums for every downloaded byte sequence;
- extraction layout, executable permissions, and launch arguments;
- health-check and synthesis protocol;
- output format (WAV or another browser-range-seekable format);
- GPU and CPU capability requirements;
- license metadata and redistribution constraints.

The implementation must reject missing checksums and must not advertise an
entry as ready merely because a package manager or third-party export exists.
