# Plan — Suggested responses (system-prompt injection + composer row above quick actions)

**STATUS: implemented and validated.** Design approved in chat 2026-10-20; spec at
`docs/superpowers/specs/2026-10-20-suggested-responses-design.md`.

## User Expectation Checklist

| # | Expectation | Status | Evidence |
|---|---|---|---|
| 1 | A setting exists in the web/desktop Settings UI that turns suggested responses on/off | ✅ | `FeaturesForm.tsx` toggle; `GET/PUT /api/config/ocode/features` carries `suggested_responses_enabled`. Web test: `FeaturesForm.test.tsx` |
| 2 | It is **enabled by default** | ✅ | `defaultOcodeConfig()` → `SuggestedResponsesEnabled: true`; `TestSuggestedResponsesEnabledDefaultsOn`. A nil config also means ON (`NewAgent`: `cfg == nil \|\| cfg.Ocode.…`) |
| 3 | When enabled, the system prompt tells the model to suggest up to five options | ✅ | `prompt.go` const + `BasePromptMessages()` injection; `TestSuggestedResponsesPromptFragmentGatedOnFlag` + `…IsSystemRole` |
| 4 | The suggestion options are NOT shown as raw text in the transcript | ✅ | `ExtractSuggestions` strips the block at the ONE choke point in `Agent.Step`; the stream filter keeps it out of the live buffer. `TestExtractSuggestionsNeverLeaksRawBlock` |
| 5 | The options render in a row **before the quick actions** at the bottom of the chat input | ✅ | `ChatInput.tsx` renders `<SuggestedResponses>` immediately before `<QuickActionsBar>`. **Mutation-verified**: moving the row below the strip FAILS the test (the first version of the test was vacuous — the strip was empty, so the ordering assertion never ran; it was strengthened with the real `SEED_CHIPS`) |
| 6 | Clicking an option fills the composer (does not send) | ✅ | `ChatInput.suggestedResponses.test.tsx`: click → textarea value set, `sendMessage` NOT called |
| 7 | The options survive a page reload | ✅ | Persisted on `Message.Suggestions`; `MERGE_SNAPSHOT`/`SET_MESSAGES` restore via `latestSuggestions`. `chatStore.suggestions.test.tsx` |
| 8 | Disabling the setting removes the prompt fragment and the row | ✅ | The flag gates the prompt fragment AND the agent's extraction; the row is absent when the server sends no options |
| 9 | Prompt cache is not busted per turn | ✅ | No new TOOL (the tools array is unchanged); the block rides in the assistant's own output (outside the cached prefix). `TestSuggestionsNeverReachTheProviderPayload` pins the field never reaches a provider payload |
| 10 | Existing suites stay green | ✅ | Go: `internal/config` + `internal/server` fully green; `internal/agent` has **5 pre-existing failures also present at pristine HEAD** (verified in a clean worktree). Web: **409 files / 3873 tests green**; `tsc --noEmit` clean; `vite build` clean |

## Mutation verification (all mutants COMPILED first, then were caught)

| Mutant | Result |
|---|---|
| Remove the 5-option cap in `parseSuggestionsPayload` | CAUGHT (`TestExtractSuggestions/more_than_five_entries_are_truncated`) |
| Never strip the block (`if true` instead of the empty-suggestions branch) | CAUGHT (6 subtests) |
| Never inject the prompt fragment (`if false`) | CAUGHT (`TestSuggestedResponsesPromptFragmentGatedOnFlag`) |
| `MERGE_SNAPSHOT` no longer restores the suggestions | CAUGHT (`chatStore.suggestions.test.tsx`) |
| Move the suggestions row BELOW the quick-action strip | **SURVIVED the first version** → the test was strengthened (real `SEED_CHIPS`) → now CAUGHT |

The surviving mutant is the important one: the original ordering assertion was
vacuous because the quick-action strip was stubbed empty, so the `if (quick)`
guard skipped the comparison entirely. The test now asserts the strip IS
rendered (`getByRole`, not `queryByRole`) and compares the two toolbars'
document positions.

## Bugs found and fixed during validation

- **The composer crashed on a missing `suggestions` field.** Twelve
  `ChatInput.*` suites mock `useChat` without the field, and `suggestions.length`
  threw. Fixed in BOTH places: `useChat` returns a module-level
  `EMPTY_SUGGESTIONS` constant (a fresh `[]` per call would fail the store's
  `Object.is` comparator and re-render every mounted composer), and `ChatInput`
  defaults again with `rawSuggestions ?? []`.

- **The raw block was persisted as a DUPLICATE assistant row.** Found by a
  LIVE end-to-end check against a real server, not by a unit test. In
  `Agent.Step`, `newMsgs = append(newMsgs, *resp)` copied the message BEFORE
  `resp.Content` was cleaned. `newMsgs` holds a VALUE copy, and it is exactly
  what the server appends to the persisted transcript
  (`as.messages = append(as.messages, resp...)`), so the transcript ended up
  with TWO assistant messages: the cleaned one and a second one still carrying
  the raw `<ocode:suggestions>` JSON — which the model would then see echoed
  back on the next turn.

  Fixed by moving the extraction BEFORE the copy. Pinned by
  `TestStepPersistsCleanContentWithSuggestions`, which asserts the returned
  messages hold exactly ONE assistant row with no marker in it.
  **Mutation-verified**: moving the extraction back after the copy makes the
  test FAIL (the mutant compiles first).

  This is the strongest argument for a live check: the parser was correct, the
  extraction was correct, and every unit test passed — the defect lived in the
  ORDER of two statements, visible only in the persisted transcript.

## Live end-to-end verification (real server, real Chromium)

A mock OpenAI-compatible provider (`/v1/chat/completions`) returned a reply
containing the marker, deliberately split across two SSE deltas so the stream
filter's hold-back logic was exercised. Against the live server:

| Check | Result |
|---|---|
| `GET /api/config/ocode/features` on a fresh config | `suggested_responses_enabled: true` (default ON) |
| A 2-arg PUT (`/mem`, `/docs`) keeps the stored value | `true` → `true` (the `*bool` decode) |
| An explicit `false` persists to `ocodeconfig.json` | yes |
| The persisted transcript | 2 rows: the user message + ONE clean assistant message with `suggestions` |
| The raw marker anywhere in the SSE event stream | **0 occurrences** |
| The `suggestions` event | fires with the 5 parsed options |
| The row in Chromium | rendered, exactly 5 options, the 6th capped out |
| Order in Chromium | suggestions row `y=803`, quick-action strip `y=833` — ABOVE |
| Click behaviour | composer becomes `Run the tests`; no message sent |
| The raw marker in the rendered page | absent |

## Known environment note (not a regression)

- `web/src/components/Git/GitPanel.test.tsx` fails with
  `expected 'nothing to push' to be 'pushed'`. The string `"nothing to push"`
  appears **0 times at HEAD** and 2 times in the working tree: it comes from a
  concurrent session's uncommitted `GitPanel.tsx` change (an ahead/behind
  commit-count formatter), while the committed test still asserts the old
  message. `GitPanel` imports nothing this change modified except `api/client.ts`,
  whose diff touches only `getFeaturesConfig`/`setFeaturesConfig`. Not caused by
  this work.


## Implementation order

### Phase 1 — Go backend

1. `internal/config/ocodeconfig.go`
   - `OcodeConfig.SuggestedResponsesEnabled bool`
   - `ocodeConfigFile.SuggestedResponsesEnabled *bool` (`suggested_responses_enabled`)
   - `defaultOcodeConfig()` → `SuggestedResponsesEnabled: true`
   - file-load application
   - config payload dump key
   - `SaveOcodeFeatures(mem, doc, suggested bool)` — extend the existing saver
     (NOT a new saver: the endpoint already owns these three toggles)

2. `internal/agent/agent.go`
   - field `suggestedResponsesEnabled bool`
   - `SetSuggestedResponsesEnabled` / `SuggestedResponsesEnabled`
   - `NewAgent`: `a.suggestedResponsesEnabled = cfg != nil && cfg.Ocode.SuggestedResponsesEnabled`
   - **default-true caveat**: `NewAgent` receives the fully-merged config, whose
     `defaultOcodeConfig()` already has it true, so `cfg.Ocode.SuggestedResponsesEnabled`
     is true for a default install. Same shape as `MemoryEnabled` (which uses
     `cfg == nil || cfg.Ocode.MemoryEnabled` so a nil config means enabled).

3. `internal/agent/prompt.go`
   - `promptSuggestionsMarker = "[ocode:suggestions]"` (the in-text block marker)
   - `suggestedResponsesPromptContent` const
   - `BasePromptMessages()`: append the fragment when enabled

4. `internal/agent/suggestions.go` (new)
   - `const SuggestionsBlockOpen = "<ocode:suggestions>"`, `SuggestionsBlockClose`
   - `ExtractSuggestions(content string) (clean string, suggestions []string)`
     - finds the block, parses the JSON array, truncates to 5, strips empties
     - malformed → returns the content unchanged and nil suggestions
   - `MaxSuggestions = 5`

5. `internal/server/handler_config.go`
   - `HandleGetFeaturesConfig` / `HandleSetFeaturesConfig`: add the third bool

6. `internal/server/event_bus.go`
   - add `"suggestions": true` to `sessionScopedEvents`

7. `internal/server/handler.go` — `wireHeadlessAgentCallbacks`
   - in the `OnMessage` assistant branch: extract suggestions, publish a
     `suggestions` event with `{suggestions: [...]}`

8. `internal/server/handler_sse.go` — the legacy SSE path: same publication

9. Persistence: the assistant message carries the suggestions.
   - Add `Suggestions []string` to `agent.Message` with `json:"suggestions,omitempty"`.
   - **Must not reach the LLM**: the provider builders construct their own payloads
     from `Role`/`Content`/`ToolCalls`, so a new field is invisible to them — but
     verify `buildAnthropicMessages` and the OpenAI converter do not spread the
     struct. (They build explicit maps → safe.)
   - The `Content` stored/persisted is the CLEANED content (block stripped).
   - Where to strip: in the agent, right after the model response is produced, so
     every consumer (persistence, SSE, bus) sees clean content + the suggestions
     field. This is the single choke point.

### Phase 2 — Web frontend

10. `web/src/api/client.ts` — extend `getFeaturesConfig` / `setFeaturesConfig`
11. `web/src/components/Settings/FeaturesForm.tsx` — the toggle
12. `web/src/api/types.ts` — `Message.suggestions?: string[]`
13. `web/src/stores/chatStore.tsx` — `suggestions` on the slice + `SET_SUGGESTIONS`
14. `web/src/lib/sessionEvents.ts` — `"suggestions"` in `SESSION_SCOPED_EVENTS`
15. `web/src/components/Chat/SuggestedResponses.tsx` (new) — the row
16. `web/src/components/Chat/ChatInput.tsx` — render above `QuickActionsBar`

### Phase 3 — Tests & validation

17. Go tests: config, prompt injection, extract/strip, event publish
18. Web tests: features form, store reducer, row render, click-fills
19. `go build ./... && go vet ./... && gofmt -l`
20. `go test ./internal/agent/ ./internal/config/ ./internal/server/`
21. `tsgo --noEmit` + `vite build` + the web test suite
22. Docs: `skills/ocode-web/SKILL.md` + `CHANGES.md`

## Risks / notes

- **Prompt cache**: the fragment is a `const` → construction-time only. The tools
  array is UNCHANGED (no new tool). The suggestions ride in the assistant's own
  output, which is outside the cached prefix.
- **`agent.Message` field**: adding `Suggestions` must not leak into provider
  payloads. The builders construct explicit maps, so it cannot — but the JSON
  round-trip through the session store WILL carry it (that is the point).
- **The web `SESSION_SCOPED_EVENTS` list must mirror the Go `sessionScopedEvents`.**
  The repo comment says so explicitly; both are updated.
- **TUI**: not in scope. The TUI reads the same `agent.Message`, so a suggestions
  field on the message is simply ignored there.
