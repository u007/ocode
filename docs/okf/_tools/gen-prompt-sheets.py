#!/usr/bin/env python3
"""Generate ANSWER-FREE blind question sheets from each stack's questions.yaml.

questions.yaml and questions.md both embed the `answer` + `rubric` on every
record — they are the GRADER'S answer key and must NEVER be shown to the model
being evaluated. This tool emits `docs/okf/_prompts/<stack>.md` containing ONLY
the id + question text, so the answering agent can run closed-book.

Usage:  python3 docs/okf/_tools/gen-prompt-sheets.py
Run from the repo root. Overwrites the sheets in place (idempotent).
"""
import pathlib
import sys

import yaml

OKF = pathlib.Path(__file__).resolve().parent.parent          # docs/okf
OUT = OKF / "_prompts"
STACKS = ["react", "rust", "tanstack", "nextjs", "golang", "conduct"]

HEADER = """# {title} — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: {n}

---
"""


def render(stack: str) -> str:
    src = OKF / stack / "questions.yaml"
    records = [r for r in yaml.safe_load(src.read_text()) if isinstance(r, dict) and "id" in r]
    if not records:
        raise SystemExit(f"{stack}: no question records found in {src}")
    title = stack.capitalize()
    parts = [HEADER.format(title=title, n=len(records))]
    for r in records:
        q = " ".join(str(r["question"]).split())  # collapse folded-scalar whitespace
        parts.append(f"### {r['id']}\n\n{q}\n")
    return "\n".join(parts)


def main() -> None:
    OUT.mkdir(exist_ok=True)
    for stack in STACKS:
        sheet = render(stack)
        dest = OUT / f"{stack}.md"
        dest.write_text(sheet)
        n = sheet.count("\n### ")
        print(f"wrote {dest.relative_to(OKF.parent.parent)}  ({n} questions)")


if __name__ == "__main__":
    sys.exit(main())
