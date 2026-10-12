---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: vbnet
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on vbnet

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0): the answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only the `_prompts/vbnet.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. Answers file:
`vbnet/answers/step-5-preview-free.md`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | narrowing example written in C# (`Integer x = 1.5`); all three points present |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | says Module members are accessed as `ModuleName.Member`; unqualified access is the norm, qualification is merely allowed |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | `_Name` and `= value` present; says the field "can't be referenced by name in normal code", but it is usable inside the class |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | ReadOnly auto-property correct (example drops the `Property` keyword); wrongly claims WriteOnly auto-properties are allowed (they are a compile error) |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | explains `=` as overloadable value equality / may not compile; never names the String-treats-Nothing-as-"" quirk, but the point's concept is present |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | structured/scoped/typed contrast present; omits that `On Error` and `Try` cannot be mixed in one method |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 1 | 0.50 | aggregates correctly in `Into` (point 2); `Group By ... Into` form shown but then claims `Into` can be skipped "for the simple case" — `Into` is mandatory, so the form point is withheld |
| vbnet-linq-03 | linq-query | 1 | 2 | 1 | 0.50 | scalar-vs-sequence contrast present (point 2); both code examples are invalid (`From o In orders Where ... Aggregate o In orders Into ...` redeclares the range variable; standalone form written `Aggregate n In numbers In Sum(n)`), and it equates the clause with `Enumerable.Aggregate` — the Aggregate/Into point is withheld |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-events-01 | events | 3 | 2 | 2 | 1.00 | |
| vbnet-events-02 | events | 2 | 2 | 2 | 1.00 | dynamic-wiring / non-WithEvents-source cases present; never names `Shared` events or `Structure` events as the places `Handles` cannot be used |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 2 | 1.00 | says no blank lines may precede `Inherits`; blank lines and comments are fine |
| vbnet-oop-02 | oop | 3 | 3 | 2 | 0.67 | Overridable/Overrides and MustOverride/MustInherit right; NotOverridable described as a sealed override but also "applied to a class forbids inheritance" (that is `NotInheritable`) and "the default for non-overridable members" (it is valid only with `Overrides`) — point withheld |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | dangling fragment "and `Dim a(-1)`." (dropped clause); both points clean |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | says string parsing in `CInt` follows `Option Compare`; that option governs string comparison, not parsing |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| syntax-basics | 1.00 | 5 | ok | omit (strong) |
| properties | 1.00 | 4 | ok | omit (strong) |
| nullability | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| linq-query | 0.79 | 4 | ok | **derive** |
| events | 1.00 | 4 | ok | omit (strong) |
| oop | 0.92 | 5 | ok | omit (above threshold) |
| conversions-arrays | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.5 / 69 = 96.4%
```

## Derivation targets

Tags below threshold (`< 0.85`): **linq-query** → feed into
`derived/vbnet.step-5-preview-free.SKILL.md`.
Nearest above threshold: oop (0.92).

## Anomalies

- The file is not valid YAML as a whole: a prose lead-in precedes the first
  record, and a stray closing code fence, a "Notes on honesty" paragraph and a
  numbered "Recap" block follow the last one. All 32 ids are present and keyed
  `answer:`; grading was unaffected.
- `vbnet-convarr-03` contains a dropped clause ("and `Dim a(-1)`." with no
  predicate). This is a generation glitch inside one answer, not file truncation.
- The model self-flagged `Group By ... Into` spelling and WriteOnly auto-properties
  as its least certain items; both are exactly where it erred.
- No contamination signals: answers are two to four times longer than the
  references, use their own examples (`products`/`Category`, `Timer.Elapsed`,
  `ThresholdEventArgs`) and carry factual errors absent from the key (optional
  `Into`, WriteOnly auto-properties, `NotOverridable` on a class, `Option Compare`
  for parsing). Nothing reads as a copy.
- Recurring pattern: every rubric concept is addressed, but about a third of the
  answers add a confident wrong side claim. Where the wrong claim concerned the
  construct a point tests (`Into`, `Aggregate`, `NotOverridable`) the point was
  withheld; the rest are listed in the notes column only.
