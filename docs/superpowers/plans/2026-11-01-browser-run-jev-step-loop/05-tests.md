# Part 05 — tests for `browser_run`

Files: new `internal/agent/browser_run_test.go`. Depends on part 04. Patterns to copy: the
`httptest` TypeSafe server used by `permission_typesafe_test.go` / `typesafe_test.go` (answers keyed
by question id), `newTestAgent` for a preloaded agent (see the memory note on git-shim cost; never
construct an agent that spawns git per test), and the fake `browser` tool approach: register a
minimal `tool.Tool` named `browser` in the agent's tool map whose `ExecuteCtx` records the args it
got and returns canned `controls` JSON or action results per call index.

## Cases

1. **Happy path, two steps then done.** Canned controls page with a button and a textbox; TypeSafe
   answers: step 1 `done` 0.1, `operation` click (confidence 0.9), `target_click` the button; step 2
   `done` 0.95. Assert: the fake browser saw `controls`, `click` (with `target=@eN` and a `run_step`
   object carrying n=1), `controls`; result header `status=done steps=1 jev_calls=2`; the step line
   quotes the label; nothing was appended to the agent's messages.
2. **Fill picks a data key.** `operation` fill, `target_fill` the textbox, `value_fill` `email`.
   Assert the `fill` sub-step carried `value` = the data value and the step line says `← data.email`;
   assert the TypeSafe request body contained the key name and **not** the value.
3. **`needs_input`.** `value_fill` = `none`. No mutating sub-step was sent.
4. **`unsure`.** `operation` confidence 0.4. Result lists `next:` controls; no mutation sent.
5. **`denied`.** The fake browser returns the permission-denied tool message for `click`. Loop stops
   after that step; status `denied`; the log names the step.
6. **Stall.** Three mutations with identical fingerprints → `stalled` after the third.
7. **`max_steps`.** `max_steps=2` with Jev always picking click on an unchanged page; two steps,
   status `max_steps` (stall needs three).
8. **Budget.** A controls page whose text is 40 KB: the request state sent to the fake server has
   `page.text` ≤ 2 KB and `elements` intact; a page with 160 huge labels that still exceeds 30 KB →
   `page_too_dense` with no request sent.
9. **No judge.** Agent with no TypeSafe key: `error: no decision judge configured`; no sub-step sent.
10. **validateAnswer.** `operation` answer names a choice that was not offered → treated as `unsure`.
11. **Stop.** Cancel the context during the second `controls` call → the tool returns promptly with
    an error wrapping `context.Canceled`.

Also extend `TestToolDefinitionsDeterministic` (or the equivalent sorted-tools test) if it
enumerates tool names, and the test that lists which tools subagents receive, if `browser_run` must
appear for `scout`/`general` and be absent where `browser` is absent.

## Verify

```
go test ./internal/agent/ -run 'BrowserRun' -count=1 -race
go test ./internal/agent/ ./internal/server/ ./internal/browse/cdp/ ./internal/config/ -count=1
```
