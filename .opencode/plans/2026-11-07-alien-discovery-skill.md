# Plan: `alien-discovery` skill (generic coherent-but-unavailable discovery)

Source method: alejandrohdez00/alien-space-of-science (NeurIPS 2026). Score candidate
combinations of "atoms" for coherence (would these form one viable thing) and
unavailability (is no existing actor positioned to produce it), fuse with β, write
proposals from the top sets. Field-agnostic: only the nouns (artifact, actor, atom) change.

Lives in `skills/alien-discovery/` in this repo, symlinked into `~/.claude/skills/` and
`~/.config/opencode/skills/` (repo-first convention; sync the VPS copy after).

## Decisions

- **Agent does the LLM work, scripts do the numeric work.** Atomization, super-atom
  naming, reconstruction and blind judging run as subagent fan-outs from SKILL.md
  (batched, JSONL in/out). Scripts never hold chat-model keys; the skill stays portable
  across Claude Code and ocode.
- **Coherence = PMI pre-filter + Jev score.** Co-occurrence PMI over the corpus prunes the
  pool; a TypeSafe `score` question on the shortlist gives the final coherence. Uses
  `TYPESAFE_API_KEY` (set). Own confidence floor; a refused/over-budget answer drops the
  candidate with a logged reason, never a default score.
- **Availability = untrained two-tower.** Actor vector = mean of its atoms' embeddings;
  candidate vector = mean of its atoms' embeddings; availability = median of top-10
  cosines against the actor bank. Embeddings come from an explicit OpenAI-compatible
  `/v1/embeddings` URL + model passed on the CLI (the ocode-managed local MLX server or
  any provider). No silent fallback between providers.
- **Candidates:** exhaustive k≤3 when the vocabulary is ≤ 300 atoms; otherwise PMI-weighted
  pair sampling extended to k. Seeded, deterministic.
- **Fusion:** `(1−β)·z(coherence) + β·z(unavailability)`, z over the whole pool, default
  β = 0.7, top-K per β.
- **State layout:** `<project>/.alien/<domain>/` with `corpus.jsonl`, `actors.jsonl`,
  `atoms.jsonl`, `clusters.json`, `vocab.json`, `repertoires.json`, `candidates.json`,
  `results.json`, `proposals/`. Every stage resumes from existing files.
- **Anti-genericity rule, enforced in SKILL.md:** the chat model never picks which
  combinations to pursue. Scores pick; the model only atomizes, names, writes and
  blind-ranks. Letting the model choose reintroduces the density bias the method removes.

## Tasks

1. **Scaffold + SKILL.md + references.** Files: `skills/alien-discovery/SKILL.md`,
   `references/method.md` (mechanism, science→any-field mapping table, limits: thin
   corpus = false "nobody does this"; actor identity is the signal, artifact-only
   corpora give weak novelty), `references/prompts.md` (the five prompts generalised with
   `{artifact}`, `{actor}`, `{atom_definition}` placeholders; keep the foreign-reader
   test and good/bad examples; add one business-domain example set),
   `references/pipeline-contract.md` (every file schema and every CLI flag).
   SKILL.md workflow: define → harvest → atomize → cluster → score → reconstruct →
   holdout, with batch sizes, resume rules and the anti-genericity rule.
2. **`scripts/alien_cluster.py`.** Subcommands: `embed` (atoms → vectors via HTTP),
   `cluster` (sklearn HDBSCAN on L2-normalised vectors, optional UMAP if available,
   writes clusters needing names + noise reassignment at a confidence checkpoint),
   `finalize` (merge agent-written super-atom names → `vocab.json`, build
   `repertoires.json` per actor from artifact→atom→cluster).
3. **`scripts/alien_score.py`.** Subcommands: `candidates`, `coherence` (PMI then Jev),
   `availability`, `select` (z-score, fuse, per-β top-K, provenance in JSON). Logs every
   dropped candidate with the reason. Permutation-invariance self-check on availability.
4. **`scripts/alien_holdout.py`.** Temporal validation when `date` exists: build from
   entries before D, count recovered post-D combinations vs chance, report the ratio.
5. **Reconstruction + blind judge flow (in SKILL.md).** Top-K sets → proposal prompt →
   `proposals/<rank>.md`; a separate judge subagent ranks proposals without seeing any
   score; final report lists rank, atoms, scores, judge rank.
6. **Proof run.** One domain, ~150–300 artifacts with actors harvested by web search
   (default: indie SaaS from YC / Product Hunt / Indie Hackers unless the user names
   another). Run end to end, record wall time, token and Jev cost, and three sample
   proposals in `references/example-run.md`. Fix what breaks.
7. **Link + docs.** `ln -s` into `~/.claude/skills/alien-discovery` and
   `~/.config/opencode/skills/alien-discovery`; add the skill to the catalog if one lists
   repo skills; `TODO.md` entry for the deferred trained coherence/availability models;
   `requirements` note for the scripts (numpy, scikit-learn ≥ 1.3, requests; umap optional).

## Open points (answer before task 6)

- Test domain: indie SaaS, or something you care about more?
- Embedding source for the scripts: the ocode local MLX server URL, or a hosted model?
