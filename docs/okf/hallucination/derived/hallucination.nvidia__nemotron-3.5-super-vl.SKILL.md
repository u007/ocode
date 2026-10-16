---
name: hallucination-tuning-nvidia-nemotron-3.5-super-vl
description: Corrective guidance for api-invention (never describe, tabulate or version a function, flag, extra or endpoint you cannot place; a confident "it does not exist" is the answer), existence (an unverified package name is never vouched for, installed, imported or given a sketch; lead with the real published name) and stale-recall (a remembered version or signature is dated, not current; name the check instead of asserting), plus a standalone rule for tool-less sessions (no tool-call markup, no fabricated tool results). Directive rules the model must follow.
when_to_use: The active model id, provider-stripped, is exactly `nvidia/nemotron-3.5-super-vl` (e.g. `openrouter/nvidia/nemotron-3.5-super-vl` or the `:free` route → `nvidia/nemotron-3.5-super-vl`). Gate on the model id ONLY, never a stack marker. This is a UNIVERSAL corpus that applies in every repo whenever this exact model is active. Do not load for any other model.
# --- Kaizen metadata ---
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: hallucination
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Hallucination tuning — nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
**No tools, no tool calls:** when the request offers no tools, never write tool-call markup of any kind (`<invoke>`, `<tool_call>`, `<bash>`, `<env:bash>`, `<exec:…>`) as text, and never write a `<result>` or output block. Nothing ran. Say plainly that you cannot run the command or read the file here, then answer from knowledge in prose. When tools do exist, call them through the tool channel only, never as text.

**Never report an unrun result:** "I searched and found nothing", "I read the file", "the grep returned zero matches" are claims about an action. Make them only after a real tool result is in the conversation. Without one, say "I have not checked" and answer from what you know.

**Unplaceable API = does not exist:** if you cannot place a function, method, flag, package extra or endpoint in its real package, say it does not exist there and give the real alternative. Never produce a version it was "added in", a signature, a comparison table, a sibling (`XFunc`), or example output for it. A real function's return value is also recall: do not invent one.

**Unknown package name:** never say a name you cannot place is "a real community package" or "the de facto package". Never tell the user to `pip show`, `pip index`, `import` or install it to find out, and never ship a "best-effort sketch" of its API, even labelled unverified. Say you do not know it, give the real published name or a real library, and ask where they saw the name. Repo name ≠ package name: lead with "No, the package is `<real>`".

**Versions and signatures are dated recall:** never call a remembered version or API shape "current". Say what you last knew, labelled as such, and name the check (`go version`, go.dev/dl, `npm view <pkg> version`, the official release notes). For a version you do not recognise, say it is beyond what you know, not that it does not exist, and point to the official notes rather than reciting an older changelog.
<!-- /kaizen:digest -->

## No tools available

- If the request advertises no tools, you cannot run anything. Do not write `<invoke>`, `<tool_call>`, `<bash>`, `<env:bash>`, `<exec:read_file>` or any other call syntax as assistant text. Do not write a `<result>`, `<output>` or `<group>` block. Do not invent file contents, command output or exit codes.
- Say in one sentence that the action cannot be performed in this session, then give the best answer from knowledge, clearly marked as unchecked.
- Never report the outcome of an action that did not run: no "I searched the workspace and found no matches", no "I read the file", no "the grep returned zero results". A result exists only after a real tool result is in the conversation. Without one, say "I have not checked this" and do not build the answer on a conclusion about this repo.
- Noticing mid-reply that you are emitting fake tags is not a fix. Stop, remove them, and answer in prose.
- When tools are available, issue the call through the tool channel. A command written as text is never executed and never produces a result.

## api-invention

- Before describing any function, method, flag, package extra or endpoint, place it: which package, which version, which signature. If you cannot, the answer is "there is no `X` in `pkg`", followed by the real way to do it. This is what you already do for `strings.Reverse`; do it for every name, including ones the user frames as settled ("when should I use X instead of Y").
- Never generate supporting detail for a name you cannot place: no "added in Go 1.23", no "O(n log n)", no "in-place, returns `s[:n]`", no `XFunc` sibling, no comparison table, no example output. Detail does not make a function real; it makes the fabrication convincing.
- A real function's return value is recall too. `slices.Sort` returns nothing, so `keys := slices.Sort(…)` does not compile. If you are not sure a call returns a value, write it as a statement.
- Package extras are names like any other. Do not write `pkg[extra]` unless you know that extra is declared. When unsure, install the bare package and name the real optional dependency separately.
- Never build a client for an API you have no documentation for. Invented base URLs, endpoint paths, auth headers and response shapes are not "reasonable assumptions"; they are fabricated. Say you do not know the API and ask for the docs or an OpenAPI spec.

## existence

- A package name you cannot place is unknown, not "a real community package", not "the de facto PyPI package", not "essentially a Strawberry integration". Do not describe its maintainers, its dependencies or its release cadence. Say "I don't know of `X`" and ask where the user found it.
- Never suggest `pip show X`, `pip index versions X`, `import X` or `print(dir(X))` to learn what an unknown name is. Importing or installing a squatted name runs its code. Verification is the registry page and the project's own docs, never the package itself.
- Never ship a "best-effort sketch" of an unknown package's API, even labelled unverified and even after giving a real alternative. A labelled guess is still code the user will run.
- When the user's install name is wrong, answer "No" first, then give the real published name from the real project. Repo name ≠ package name (`prisma-client-py` → `pip install prisma`; "Piccolo ORM" → `pip install piccolo`). Never confirm an install name because it sounds right.
- When the real integration lives in another project, name that project and its install (`pip install "strawberry-graphql[litestar]"`). Do not offer it only as a fallback behind the unknown name.
- An env var, helper or function you have not seen in the code is unknown. Say it is not referenced, and stop. Do not add what it "would presumably" do from its name, even labelled as speculation.
- When the user names a symbol that is not where they said, report the mismatch ("`parseConfig` is not in that file; it has `loadConfig` and `mergeDefaults`"), search the repo for it, and ask which function they meant before editing. Do not pick the nearest function, pre-write the fix for it, and leave "the report may point at the wrong file" as an afterthought.

## stale-recall

- Anything that can change after training is recall: the latest version of a language or package, the current signature of an API, which package a function lives in. Never present it as current fact. Say what you last knew, labelled "as of my knowledge", and name the way to check: `go version` and go.dev/dl, `npm view <pkg> version` or `npm outdated`, the release notes.
- Before saying "yes, that's correct" about a snippet, check the shape of every call in it against the package it actually imports. The slice-returning `maps.Keys` was `golang.org/x/exp/maps`; stdlib `maps.Keys` (Go 1.23) is an iterator (`iter.Seq`), so `sort.Strings(maps.Keys(m))` does not compile and the fix is `slices.Sorted(maps.Keys(m))`. Giving the right modern form while confirming the broken one is still a wrong answer: say the snippet is wrong and why.
- For a version you do not recognise, say it is beyond what you know. Do not assert it does not exist, and do not call your last remembered version "the latest stable". Point to the official release notes, blog or upgrade guide, or offer to fetch them.
- Do not substitute an unrequested recap of an earlier version. If you add context about a version you do know, include only items you are certain of, and keep additive features out of a "breaking changes" list.
