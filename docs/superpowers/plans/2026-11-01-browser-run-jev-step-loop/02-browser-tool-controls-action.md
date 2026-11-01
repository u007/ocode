# Part 02 — browser tool: read action `controls`, dialog summary for `run_step`

Files: `internal/server/browser_tool.go` (+ `browser_tool_test.go` fakes),
`internal/agent/permissions.go` (`browserObserveActions`, `browserAction`),
`internal/agent/permission_typesafe.go` (`browserUserTabReadActions`),
`internal/agent/content_guard_typesafe.go` (`browserGuardedOutputs`). Depends on part 01.

## 1. Action `controls`

- Add `controls` to `browserActions` (keep the slice sorted), to `browserReadOnlyActions` (read-only
  agent types may run it), and to `browserSurface` as `Controls(ctx, refs) (cdp.ControlsPage, error)`.
  Update `fakeSurface` in the test file to implement it (a `controlsPage` field and a recorded call).
- `perform` case `controls`: calls the surface with the tab's ref table, keeps the last screenshot
  (it is a read), and returns the page as **one JSON object** after the usual untrusted-content
  header line, so the loop can parse it: `{"url","title","fingerprint","settled","text",
  "text_truncated","controls":[{"ref","role","label","value","states","context","operations"}],
  "omitted"}`. The header is the same sentence the other reads use.
- `checkShape`: `controls` takes no `target`; `frame` is not accepted in v1 (it is not in
  `browserFrameActions`).
- `Definition`: add `controls` to the enum only. Do not describe the loop here; the description of
  the browser tool may gain one clause: "action=controls returns the page's interactive elements
  as JSON (used by browser_run)".
- `PageContext` already resolves the tab for any action other than `tabs`/`open`; nothing to add.

## 2. Permission layer

- `browserObserveActions` and `browserUserTabReadActions` gain `controls` (a read: allowed on an
  agent-opened tab, asks on a user tab, exactly like `snapshot`).
- `browserAction` (the one-line dialog summary) reads an optional `run_step` object from the args
  (`goal`, `n`, `of`, `label`). When present the summary becomes
  `browser_run step <n>/<of> (<goal, clipped at 80 runes>): <existing summary> <label, clipped at
  60 runes, quoted>`. Typed values stay unechoed (length only), as today. Unknown fields are already
  ignored by `browserArgs`, so the browser tool needs no change for `run_step`.

## 3. Content guard

- `browserGuardedOutputs["controls"] = "browser controls index"`. The guard reads the JSON text; that
  is fine, it is page text and labels.

## Tests

- `browser_tool_test.go`: `controls` returns the JSON after the header, parses back into the same
  shape, keeps the held screenshot, is permitted for `explore`, refuses `target`/`frame`.
- `permissions` tests: the summary with and without `run_step`; `controls` is observe and a user-tab
  read (extend the existing table tests for `snapshot`).
- Content guard test: `controls` is in the guarded set (extend the existing table test).

## Verify

```
go build ./... && go vet ./internal/server/ ./internal/agent/ && go test ./internal/server/ -run 'Browser' -count=1 && go test ./internal/agent/ -run 'Browser|Guard|Permission.*Browser' -count=1
```
