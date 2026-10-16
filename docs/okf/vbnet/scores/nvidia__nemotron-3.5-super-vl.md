---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: vbnet
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on vbnet

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a scratch
cwd containing only the `_prompts/vbnet.md` sheet, one fresh session; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 32 ids came
back once, none empty. Headless runs inject the bundled skill-catalogue digest into
the system prompt; no answer cites a tool or skill the question never named. The
OpenRouter `:free` route variant is not part of the model id. Answers file:
`vbnet/answers/nvidia__nemotron-3.5-super-vl.md`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | calls a Module "a single shared instance" accessed "through the module name"; unqualified access is the norm, qualification is merely allowed |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | says the name-assignment form is "followed by `Exit Function`"; falling through to `End Function` is enough |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | shows `Public WriteOnly Property Name As String` as a bare one-liner, which would be a WriteOnly auto-property (compile error); the ReadOnly auto-property point itself is right |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | never names the String-treats-Nothing-as-"" quirk; value-equality / may-not-compile concept present |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | omits "most specific Catch first" and the cleanup use of Finally; always-runs concept present |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | omits that `On Error` and `Try` cannot be mixed in one method |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 2 | 1.00 | `Group By c.City Into CityCustomers = Group, Count()` is correct; side claims are off: lists `Aggregate` among the functions `Into` accepts (it is not one), and writes `Into Group = Group` (alias collides with the `Group` keyword) |
| vbnet-linq-03 | linq-query | 1 | 2 | 2 | 1.00 | |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-events-01 | events | 3 | 2 | 2 | 1.00 | compile-time auto-wiring present; never says one `Handles` clause may list several events, nor that reassigning the `WithEvents` field rewires |
| vbnet-events-02 | events | 2 | 2 | 2 | 1.00 | dynamic-wiring / unknown-source cases present; never names `Shared` events or `Structure` events as the places `Handles` cannot be used |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 1 | 0.50 | one-base / many-interfaces right; never mentions the per-member `Implements IFoo.Member` clause, and claims a `MustInherit` class need not provide all interface members (it must map every one, `MustOverride` allowed) — point 2 withheld |
| vbnet-oop-02 | oop | 3 | 3 | 2 | 0.67 | Overridable/Overrides and MustOverride/MustInherit right; `NotOverridable` described as "seals a member (the default behavior)" — it is valid only on an `Overrides` member and seals that override; the sealed-override concept is absent, point withheld |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | type-name access present; omits that a Shared member cannot touch instance state / `Me` |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | says `TryCast` works on "reference/nullable types"; `TryCast` to a `Nullable(Of T)` target is a compile error (reference types and type parameters only) |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | labels `CInt`/`CStr`/`CDbl` "legacy" and says modern code prefers `CType`/`DirectCast`/`Convert`; they are the idiomatic intrinsic conversions (and `DirectCast` cannot do Double→Integer) — a style misjudgement, the conversion concept is right |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| syntax-basics | 1.00 | 5 | ok | omit (strong) |
| properties | 1.00 | 4 | ok | omit (strong) |
| nullability | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| linq-query | 1.00 | 4 | ok | omit (strong) |
| events | 1.00 | 4 | ok | omit (strong) |
| oop | 0.79 | 5 | ok | **derive** |
| conversions-arrays | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

oop detail: Σ(normalized×weight) = 2 + 1.5 + 2 + 2 + 2 = 9.5 over Σ(weight) = 12.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.5 / 69 = 96.4%
```

## Derivation targets

Tags below threshold (`< 0.85`): **oop** → feed into
`derived/vbnet.nvidia__nemotron-3.5-super-vl.SKILL.md`.
Every other tag scored 1.00.

## Anomalies

- The answers file is clean: one header comment line, then a valid YAML list of 32
  `{id, answer}` records in sheet order, no duplicates, no empty answers, no
  thinking-aloud, no fabricated tool output, and no reference to any tool or skill.
- Answers are terse (about 340 chars on average, shorter than the 450-char
  references); every one is a single paragraph that answers exactly what was asked.
- No contamination signals: the model uses its own examples (`customers`/`City`,
  `ProcessComplete`/`ProcessEventArgs`, `CityCustomers`) and carries factual errors
  absent from the key (`NotOverridable` as "the default", `MustInherit` exempting
  interface members, `Aggregate` inside `Into`, `TryCast` to nullable, `CInt` as
  "legacy", `Exit Function` after name assignment). A few sentences (null-04,
  oop-04, errors-03) paraphrase the key closely, but these are the textbook phrasings
  of those facts and sit beside the independent errors above, so nothing reads as a
  copy.
- Recurring pattern: both lost points are in `oop`, and both are VB-specific
  inheritance-model details rather than general OO knowledge — the per-member
  `Implements` clause and what `NotOverridable` actually applies to. The same
  `NotOverridable`-as-default confusion appears in the step-5-preview-free scorecard.
- About a quarter of the full-credit answers add one confident wrong side claim
  (listed in the notes column). None contradicted the construct a point tests, so
  none was withheld; judgment calls are recorded per row.
