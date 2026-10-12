---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: elixir
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — step-5-preview-free on elixir (with-skill validation)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `elixir/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/elixir.step-5-preview-free.SKILL.md` (`elixir-tuning-step-5-preview-free`)
> prepended to the answerer prompt as active guidance (audited 1 user + 1
> assistant message, zero tool calls). Baseline: `scores/step-5-preview-free.md`.
> No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **pattern-matching, immutability-data**.

## Provenance

Closed-book: the answerer session (`ses_2026-10-12-130400-dd375158`, project
`9dce73dae1a2`) holds exactly 1 user + 1 assistant message and 0 tool calls. The
user message is the `_prompts/elixir.md` sheet with the skill body under an
"Active guidance" header; it names `questions.yaml` only inside the CLOSED-BOOK
prohibition and contains no rubric text. The assistant message is a single YAML
list, 32/32 ids, no duplicates.

**Answers-file history:** the first extraction of
`answers/step-5-preview-free.with-skill.md` (29,434 bytes) truncated every
answer containing a code block after its first fence. It was re-published
complete (39,911 bytes) on 2026-10-12 and verified id-by-id against the raw
assistant message: all 32 answers are identical modulo whitespace; the raw
message additionally carries a trailing self-summary footer that is not an
answer and was ignored. Grading below is against that complete text.

Grading rule applied (same strictness as the baseline): a point is withheld
when the answer states the concept but also makes a materially wrong claim
about the same construct, marked `(judgment)`; tangential wrong asides are
noted only. Points are whole; fractions come only from a listed partial.

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation
  for step-5-preview-free on elixir).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 3 | 1.00 | names `MatchError` explicitly, "no partial binding" |
| elixir-pm-02 | pattern-matching | 3 | 3 | 3 | 1.00 | now: guard whitelist (not "side-effect free"), `defguard`, `FunctionClauseError` named, dead clause after catch-all → compiler warning. All three baseline gaps closed |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | adds `%{^key => v}` map-key pin, correct |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | `[h \| t] = []` fails: correct; volunteers the `%{m \| k: v}` runtime-KeyError distinction |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | `{:stop, reason, reply, state}` and `GenServer.reply/2` correct |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | "stale replies after a call timeout" is a correct extra example |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | `__struct__` named this time; aside: garbled "Since Elixir 1.13+ 1.13 keyword lists are as fast as maps for small sizes" |
| elixir-data-03 | immutability-data | 2 | 3 | 3 | 1.00 | KeyError now "AT RUNTIME, whether the key is a literal or a variable"; compile-time check scoped to structs only. Baseline's withheld point earned |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | `update_in` passes `nil` to the fun on a missing last key, raises `ArgumentError` on a missing intermediate, `Access.key/2` default, "there is no `fetch_in`", manual-nesting example valid. Baseline's withheld point earned |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | aside: offers `value \|> fun.(other)` as a way to "break the rule" — it still inserts as first arg |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | aside: "final plain `= expr` binding" sentence is garbled |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | `File.write!` arg-order example is correct; `then/2` and named intermediates both given |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | aside (same as baseline): "the compiler warns on unused match results" is not true for plain tuples |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | aside: cites `String.to_integer!` as a pair — it does not exist (`String.to_integer/1` itself raises); convention point intact |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | `catch :throw/:exit/:error` kinds correct; `reraise` mentioned |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | "still use tuples for expected errors" is only implicit (rescue at the boundary instead) — minor |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | intermediate lists stated as "materialised collection" per Enum step; element-at-a-time shown via `Enum.take(3)` example — minor |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | map/filter/sum/length each written as a reduce |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | names consolidation and stdlib protocols |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | `@optional_callbacks` now mentioned (baseline denied it existed). Aside: an `@impl` on a non-callback is a compiler *warning*, not an error |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | aside (same as baseline): `@impl` does not check argument names or return shape, and mismatches warn rather than error |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | async/await point withheld, same as baseline: says `Task.await` returns `{:ok, value}` — that is `Task.yield`'s shape; `await` returns the bare result (judgment). Linked-exit and `{:timeout, task}` exit behaviour correct this time; aside: an un-awaited completed task does not crash the caller |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | wording slip "Use an Agent when the shared data is SIMPLE and needs CUSTOM LOGIC" contradicts its own next bullet; the contrast (Agent = state only, GenServer = custom handling) is otherwise clear |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | n | trust | delta | target? |
|-----|---------:|-----------:|--:|-------|------:|---------|
| pattern-matching | 0.80 | 1.00 | 4 | ok | +0.20 | **yes** |
| processes-otp | 1.00 | 1.00 | 6 | ok | 0.00 | no |
| immutability-data | 0.83 | 1.00 | 5 | ok | +0.17 | **yes** |
| pipe-with | 1.00 | 1.00 | 4 | ok | 0.00 | no |
| error-handling | 1.00 | 1.00 | 6 | ok | 0.00 | no |
| enum-stream | 1.00 | 1.00 | 4 | ok | 0.00 | no |
| protocols-behaviours | 0.88 | 1.00 | 4 | ok | +0.12 | no |
| concurrency | 0.93 | 0.93 | 5 | ok | 0.00 | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: pattern-matching 10/10; processes-otp 14/14; immutability-data
10/10; pipe-with 8/8; error-handling 15/15; enum-stream 9/9;
protocols-behaviours 8/8; concurrency 9.33/10.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70.33 / 71 = 99.1%
```

(Loss: conc-02 −0.67.) Baseline 65.67 / 71 = 92.5%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| pattern-matching | 0.80 | 1.00 | **PASS** |
| immutability-data | 0.83 | 1.00 | **PASS** |

Every target tag reaches 0.85. The skill is validated for step-5-preview-free @
5-preview. The three baseline `(judgment)` withholds the skill targeted (pm-02
guard whitelist + `FunctionClauseError`, data-03 runtime `KeyError`, data-04
`update_in`/`fetch_in`) are all corrected in the model's own words, and the
only remaining loss (conc-02 `Task.await` return shape) is outside both
targets and identical to the baseline.

Non-target drift: protocols-behaviours 0.88 → 1.00 (proto-02 now mentions
`@optional_callbacks`, which the skill does not cover) and concurrency
unchanged at 0.93 (same conc-02 miss). Single-sample movement, not acted on.
