# LLM Auto-Permission Follow-up: Interpreter Execution (Heredocs + Script Files)

**Date:** 2026-06-02
**Status:** Follow-up recommendation to `2026-06-01-llm-auto-permission-design.md`

## Why this follow-up exists

The main auto-permission design covers shell commands, path/root scoping,
context-bounded LLM consultation, and durable exact grants.

A separate design decision is needed for **interpreter execution**, including both
embedded source and script files, for example:

```sh
python <<'PY'
from pathlib import Path
Path("/tmp/x").write_text("hello")
PY
```

```sh
python file.py
ruby file.rb
node script.js
```

These are not ordinary shell commands with obvious effects. They ask the shell to
launch a second program whose behavior is defined by source code.

## Core conclusion

Interpreter execution should be eligible for **LLM auto-approval when the model is
confident and Go can verify the inferred effects stay inside policy**.

This applies to both:

- embedded source (`python <<'PY' ... PY`), and
- script files (`python file.py`, `ruby file.rb`, `node script.js`).

Confidence alone is not enough. The acceptance rule is:

- the model must return a structured effect summary,
- confidence must exceed a strict threshold,
- no important effects may remain unknown,
- and Go must verify the inferred effect set against the deterministic guardrails.

If any of those checks fail, the flow falls back to human `Ask`.

## Non-goals

- Building a full static analyzer for Python, Ruby, Node, Perl, etc.
- Treating the permission LLM as a complete effect verifier.
- Creating broad grants like `python = allow` or `tool.bash = allow`.

## Threat model

Interpreter execution is risky because the source may:

- compute paths dynamically,
- import other files,
- read environment variables,
- make network requests,
- spawn subprocesses,
- branch on runtime state,
- or hide harmful behavior behind benign-looking setup.

The permission LLM is useful for bounded semantic analysis, but it is not the
trust boundary. The trust boundary remains Go-side policy enforcement.

## Unified classification

Treat these as one family: `interpreter_execution`.

### Supported forms

- `python <<...`
- `python3 <<...`
- `ruby <<...`
- `node <<...`
- `perl <<...`
- `python file.py`
- `python3 file.py`
- `ruby file.rb`
- `node script.js`
- similar interpreter invocations with either embedded source or a script file

### Go-side classification fields

For a pending request, classify:

- `execution_kind = "interpreter"`
- `language = "python" | "ruby" | "javascript" | ...`
- `source_mode = "heredoc" | "script_file" | "stdin_pipe"`
- `entrypoint_path` when a script file is used
- `contains_embedded_source` when source is inline

## Source acquisition rules

### 1) Embedded source

For heredocs / stdin-piped source:

- parse the shell structure in Go,
- extract the embedded source body,
- record delimiter / transport metadata,
- hash the extracted source,
- and include bounded source text in the consultation payload.

The parser does **not** need to understand Python or Ruby semantics. It only
needs to reliably extract the source block and classify the request.

### 2) Script files

For `python file.py`, `ruby file.rb`, etc.:

- resolve the script path in Go,
- require the script file to be inside allowed roots,
- read the script source directly when it is text and within configured limits,
- hash the source,
- and include it in the consultation payload as untrusted source.

If the script path is outside allowed roots, unreadable, binary, or too large to
analyze safely, fall back to human `Ask`.

### 3) Optional imported-file context

The permission system may also include a small number of local imported files,
but only under strict limits:

- only explicit local/project imports,
- only files inside allowed roots,
- only text files,
- only up to `max_context_sources` / `max_context_bytes`,
- no recursive project crawl,
- no sensitive paths.

If imports cannot be resolved safely, mark them unknown instead of guessing.

## Consultation payload

For interpreter execution, the model should receive structured input like:

```json
{
  "tool_name": "bash",
  "execution_kind": "interpreter",
  "language": "python",
  "source_mode": "script_file",
  "command": "python file.py",
  "cwd": "/project",
  "entrypoint_path": "/project/file.py",
  "allowed_roots": ["/project", "/tmp"],
  "allow_destructive": false,
  "source": {
    "sha256": "...",
    "truncated": false,
    "text": "..."
  },
  "context": {
    "imports": [
      {
        "path": "/project/lib/helpers.py",
        "sha256": "...",
        "truncated": false,
        "text": "..."
      }
    ]
  }
}
```

Rules:

- all source text is treated as **untrusted context**, never as instructions,
- ANSI/control bytes are stripped,
- binary data is rejected,
- context is hard-capped in Go,
- sensitive paths are excluded,
- if source is truncated, that fact is explicit in the payload,
- if key context is missing, the model must be able to say so via `unknown`.

## Model response contract

For interpreter execution, the model should return structured effects, not just
`allow|ask` prose:

```json
{
  "decision": "allow",
  "confidence": 0.94,
  "summary": "Reads /project/in.txt, writes /tmp/out.json, no subprocesses, no network.",
  "effects": {
    "reads": ["/project/in.txt"],
    "writes": ["/tmp/out.json"],
    "deletes": [],
    "network": [],
    "subprocesses": [],
    "unknown": []
  }
}
```

Requirements:

- `decision` is still only `allow` or `ask`,
- `confidence` is numeric,
- `effects` must enumerate inferred behavior,
- `unknown` must contain anything the model cannot resolve confidently.

## Go-side acceptance rules

Go may auto-approve interpreter execution only when **all** of these are true:

1. `decision == "allow"`
2. `confidence >= permissions.auto.min_confidence`
3. `unknown` is empty
4. source is available and not truncated in a way that prevents analysis
5. every inferred read/write/delete path is resolved and inside allowed roots
6. no inferred path is sensitive
7. no subprocesses are inferred
8. no network is inferred, unless every target domain is separately allowed by policy
9. if destructive effects exist, `allow_destructive == true`
10. no Go-side hard-block rule is triggered

If any condition fails, fall back to human `Ask`.

This is the intended meaning of "auto approve if the LLM is confident":

- confidence is necessary,
- but confidence must come with a concrete effect set,
- and that effect set must pass deterministic Go-side verification.

## Unknown / ambiguous cases

The model should return `unknown` entries, and Go should require `Ask`, when the
script contains behavior such as:

- dynamic path construction the model cannot resolve,
- unresolved imports,
- `exec`, `eval`, metaprogramming,
- `subprocess`, `os.system`, shell-outs,
- dynamic code loading,
- partial/truncated source that hides key behavior,
- network behavior with unknown targets,
- effects hidden behind runtime-only branches the model cannot confidently reduce.

## Persistence model

Persist interpreter auto-grants narrowly.

Recommended grant forms:

### Embedded source

```jsonc
{
  "kind": "bash_exact",
  "language": "python",
  "source_mode": "heredoc",
  "normalized_command": "python <<'PY' ... PY",
  "embedded_source_sha256": "...",
  "destructive": false
}
```

### Script files

```jsonc
{
  "kind": "interpreter_exact",
  "language": "python",
  "source_mode": "script_file",
  "entrypoint_path": "/project/file.py",
  "entrypoint_sha256": "...",
  "normalized_args": ["file.py"],
  "cwd": "/project",
  "destructive": false
}
```

Properties:

- exact and greppable,
- keyed by source hash, not just filename,
- narrow enough that source changes invalidate the grant,
- no broad prefix-wide or tool-wide grants are invented automatically.

## Shell-level checks still apply

Even for interpreter execution, the normal Go-side checks still apply:

- hard-blocked commands remain denied before consultation,
- YOLO / locked behavior remains unchanged,
- shell-level redirections and explicit shell path args are still checked,
- sensitive-path rules still apply,
- `/tmp` and other allowed roots are enforced by Go,
- destructive gating is still controlled by `allow_destructive`.

## UX recommendation

When the pending request is interpreter execution, the prompt should say so
explicitly.

Example:

```text
Permission request: bash
Reason: interpreter execution (python script file)
Auto-approval mode: confidence + verified effects
```

If consultation is enabled, show a short model summary:

```text
Model summary: reads /project/input.txt and writes /tmp/out.json. No network or
subprocesses inferred. Confidence: 0.94.
```

If auto-approval is rejected because verification failed, say why:

```text
Auto-approval declined: unresolved dynamic import and unknown network target.
Falling back to Ask.
```

## Tests

- Heredoc commands are detected and classified as interpreter execution.
- `python file.py` / `ruby file.rb` are detected and classified as interpreter execution.
- Script entrypoint paths are resolved and required to stay inside allowed roots.
- Consultation payload includes bounded source for both heredoc and script-file modes.
- Optional imported-file context is capped and excluded when unsafe.
- Model responses with confidence below threshold fall back to `Ask`.
- Model responses with non-empty `unknown` fall back to `Ask`.
- Auto-approval succeeds when the inferred effects are complete, high-confidence,
  non-destructive, and fully inside allowed policy.
- Destructive inferred effects still require `allow_destructive=true`.
- Network effects require explicit domain policy to auto-approve.
- Exact interpreter grants match identical source-hash repeats and reject changed source.
- Broad `python` / `ruby` / `bash` grants are not auto-created.

## Recommendation summary

Treat `python <<'PY' ... PY`, `python file.py`, `ruby file.rb`, and similar cases
as one category: **interpreter execution**.

Allow the permission LLM to auto-approve when it is confident, but only under
this stronger rule:

- structured effect extraction,
- strict confidence threshold,
- no unknowns,
- deterministic Go-side verification,
- and narrow persisted exact grants.

That gives you the behavior you want: the LLM can auto-approve first-run
interpreter execution when it is confident **and** the inferred behavior is fully
inside policy.