# React Scorecards

One file per **exact model version**, named by its exact model id:
`<model-id>.md` (e.g. `claude-opus-4-8.md`, `gpt-5-codex.md`,
`gemini-3-pro.md`). Never a family name.

1. Copy `../../_schema/scorecard.template.md`.
2. Ask the model every question in `../questions.yaml`.
3. Grade each answer against its rubric (`../../_schema/rubric-guide.md`).
4. Fill the per-question table, per-tag subscores, and total.
5. Tags below the threshold feed `../derived/react.<model-id>.SKILL.md`.

`claude-opus-4-8.md` here is a **worked example** to show the shape. Its awarded
scores are illustrative, not a real evaluation — re-grade before trusting them.
