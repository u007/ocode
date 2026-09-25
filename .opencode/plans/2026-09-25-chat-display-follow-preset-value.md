# Chat display: show the resolved value on "Follow preset" + align Balanced

Status: approved (2026-09-25)

## Problem

1. `ChatDisplayForm.tsx` renders the override `<option>` list from a static
   `OVERRIDE_OPTIONS`, so a category set to `Follow preset` never shows what the
   currently selected Full/Balanced/Quiet preset resolves to.
2. `balanced` did NOT collapse `tool_calls`, contradicting the design spec
   (`docs/superpowers/specs/2026-09-24-chat-verbosity-display-design.md` §9
   table, §8 copy, §10) and the form's own Balanced description. The spec is
   the source of truth; the resolver is the defect. Both resolvers
   (web + Go) and their tests had drifted, plus `skills/ocode-web/SKILL.md`
   item 35(b) documented the deviated behavior.

## Design

- Option VALUE stays `"preset"`; only option TEXT becomes
  `Follow preset — <Expanded|Collapsed>`, computed from the DRAFT preset via
  the existing `resolveChatDisplayPolicy` (single source of truth, so the label
  can never disagree with the renderer). Updates immediately when the Preset
  radios change, before Save.
- The `<select>` keeps its category `aria-label`; the suffix is option text
  only (tests query option text, not the aria-label).
- `balanced` collapses `tool_calls` in BOTH resolvers, per spec §9.

## Changes

| File | Change |
|---|---|
| `web/src/lib/chatVerbosity.ts` | `balanced` branch also sets `toolCalls = "collapsed"` |
| `internal/config/ocodeconfig.go` | `ResolveChatVerbosityPolicy` balanced case also sets `toolCalls = ChatDisplayCollapsed` |
| `web/src/components/Settings/ChatDisplayForm.tsx` | dynamic `Follow preset — …` label; override key → policy key map |
| `web/src/lib/chatVerbosity.test.ts` | balanced matrix cell → collapsed; override test now forces a cell back open |
| `internal/config/chat_verbosity_test.go` | balanced cell → collapsed; override case exercises both directions |
| `web/src/components/Chat/TurnParts.verbosity.test.tsx` | fixture `base.balanced.tool_calls` → collapsed |
| `web/src/components/Settings/ChatDisplayForm.test.tsx` | new: label shows resolved value; updates on radio switch before save |
| `skills/ocode-web/SKILL.md` | item 35(b) balanced/quiet sentence |
| `CHANGES.md` | entry |
| design spec §8 (via context agent) | dynamic-label amendment 7 + resolver-alignment note |

## TDD order

1. RED: form test (dynamic label) + corrected matrix tests (web + Go).
2. GREEN: form label, then both resolvers.
3. Mutation-check: revert each resolver line and confirm the matching test fails.

## Validation

- `cd web && npm test -- --run` on: `ChatDisplayForm`, `chatVerbosity`,
  `chatVerbosity.store`, `TurnParts.verbosity`, Chat suites.
- `go test ./internal/config/...`, `go build ./...`, `go vet ./internal/config`,
  `gofmt -l`.
- `cd web && npm run typecheck && npm run build`.

## Notes / risks

- `full` keeps the render-time `lineCount <= 50` first-paint heuristic; that is
  not a policy value and is deliberately NOT reflected in the label.
- No wire/config change: the persisted shape is untouched.
