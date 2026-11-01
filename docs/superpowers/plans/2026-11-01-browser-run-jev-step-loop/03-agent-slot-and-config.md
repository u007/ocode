# Part 03 — judge slot `browser_run` and its config key

Files: `internal/config/ocodeconfig.go` (+ its tests), `internal/agent/decider.go` (+ tests).
Independent of parts 01–02.

## Config

- `OcodeConfig` gains `BrowserRun JudgeModelConfig`, JSON key `browser_run` with sub-key
  `judge_model`, seeded by `defaultJudgeModelConfig()` and applied with `applyJudgeModelConfig`
  exactly as `DocSearch`, `Search`, `NetworkGuard`, `ContentGuard` and `Goal` are. Follow every place
  those five are touched (default, load, save, any settings DTO or test table). Do not add an
  enable flag.
- If the web settings UI or `/api/config` enumerates judge slots (search `web/src` and
  `internal/server/handler_config*.go` for `judge_model`), add the new slot there in the same shape.
  If nothing enumerates them, leave the UI alone.

## Decider

- `slotBrowserRun judgeSlot = "browser_run"` in `decider.go`; `slotModel` reads
  `oc.BrowserRun.JudgeModel`; the default stays `defaultJudgeModel`. `resolveDecider` needs no
  change. Extend the slot table test so the new slot resolves the configured model and the default.
- Constants for the loop's floors live with the loop (part 04), not here; this part only wires the
  slot.

## Verify

```
go build ./... && go test ./internal/config/ ./internal/agent/ -run 'Judge|Slot|Decider|Config' -count=1
```
