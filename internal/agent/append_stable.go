// append_stable.go documents the cache-stability contract for
// the agent's per-loop context assembly. The contract has two
// parts:
//
//  1. The "stable prefix" — system prompt, transcript, prior
//     <oc-log> blocks — must be byte-identical across two
//     consecutive Step invocations with the same input.
//  2. The "volatile tail" — anything injected at the END of
//     the slice (the current <oc-log> block) — is allowed to
//     change across loops.
//
// The tests in append_stable_test.go pin the prefix part of
// this contract and explicitly exclude the documented
// exceptions below.
//
// DOCUMENTED EXCEPTIONS (volatile content that is currently
// injected in the stable prefix, not at the tail):
//
//   - "Today's date" in the environment prompt (prompt.go:
//     environmentPrompt). The date changes once per day, so
//     the prefix is NOT byte-identical across the midnight
//     boundary. The cache hit is still effective for any
//     given day; the date is a single short line in an
//     otherwise stable prompt. We choose to keep it in the
//     prefix because (a) the LLM uses it for date-aware
//     reasoning, and (b) moving it to the tail would not
//     reduce cache churn (it would still change per day).
//
// LSP diagnostics are NOT an exception: they are attached to
// write-tool results and, for out-of-band changes, rendered
// as a user-role tail block (lsp_inject.go). They were once a
// system-role tail message, which the provider builders hoist
// into the cached system block — so every diagnostic change
// re-cached the whole system prompt. The same hoist is why
// injectNotesTail is user-role.
//
// These exceptions are listed here so a future contributor
// can find them via grep and not be surprised when a
// strict-invariant test fails.

package agent
