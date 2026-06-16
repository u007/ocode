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
// The LSP diagnostics injection is NOT an exception: it was
// moved from "after system, before transcript" to "at the
// tail" as part of the Part 04 audit. The prefix is now
// stable across loops that have no LSP change (in addition
// to no transcript / no notes change).
//
// These exceptions are listed here so a future contributor
// can find them via grep and not be surprised when a
// strict-invariant test fails.

package agent
