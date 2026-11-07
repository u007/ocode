# Pipeline contract: files, schemas and script CLIs

Everything for one run lives in one directory, `<project>/.alien/<domain>/` (call it `DIR`).
Every stage reads only files from earlier stages, writes only its own files, and is
re-runnable: an existing complete output is reused, a partial one is resumed. All list
output is sorted (ids ascending, scores descending) and every dropped item is logged with
its reason to stderr and to the stage's `*_log.jsonl`.

The agent writes the files marked **agent**. The scripts write the files marked **script**.
No script holds a chat-model key; the only network calls scripts make are the embeddings
endpoint and the TypeSafe `/v1/systemone` endpoint (`TYPESAFE_API_KEY`).

## Files in `DIR`

| File | Writer | Content |
|---|---|---|
| `domain.json` | agent | `{"domain", "artifact_noun", "actor_noun", "atom_definition", "coherence_question", "created"}`. `coherence_question` is the one-sentence question Jev is asked about a set (e.g. "Could these mechanisms all be parts of ONE viable small software business serving the same customer?"). |
| `corpus.jsonl` | agent | One artifact per line: `{"id", "title", "actor_ids": [..], "date": "YYYY-MM-DD" or null, "url", "text"}`. `text` is the mechanism-level description (what it does and how, not results/praise). `actor_ids` must be non-empty. |
| `actors.jsonl` | agent | `{"id", "name", "url"}`. Every id referenced by `corpus.jsonl` must exist here. |
| `atoms.jsonl` | agent | One atom per line: `{"atom_id", "artifact_id", "text", "quality": "strong"\|"medium"\|"weak"}`. `atom_id` = `<artifact_id>#<n>`. Only atoms that survived rate-and-refine (unrescuable ones are not written). |
| `embeddings.npy`, `embeddings_index.json` | script | float32 `[n_atoms, dim]`, L2-normalised; the index is `{"atom_ids": [..row order..], "model", "url", "sha256_of_atoms_jsonl"}`. |
| `clusters.json` | script | `{"params": {...}, "assignments": {atom_id: cluster_id}, "clusters": {cluster_id: {"size", "member_atom_ids": [..], "centroid_atom_ids": [..up to 15, nearest-to-centroid first..]}}}`. `-1` = noise that was not reassigned. |
| `clusters_to_name.jsonl` | script | `{"cluster_id", "size", "atoms": [..texts of centroid_atom_ids..]}` — the naming worklist for the agent. |
| `cluster_names.jsonl` | agent | `{"cluster_id", "name", "rationale", "coherence": "low"\|"medium"\|"high"}`. `name` is the super-atom text (self-standing, mechanism + purpose). |
| `vocab.json` | script | `{"atoms": {cluster_id: {"name", "size", "sample_atoms": [..3..], "coherence"}}, "artifact_atoms": {artifact_id: [cluster_id..]}}`. Only named, non-noise clusters. Cluster ids are the integer keys from `clusters.json`, serialised as strings. |
| `repertoires.json` | script | `{actor_id: {"name", "atoms": [cluster_id..], "artifact_ids": [..]}}`. Atoms = union over the actor's artifacts. Actors with zero atoms are omitted and logged. |
| `pairs.json` | script | `{"question", "model", "pairs": {"<i>,<j>": noul}}` with `i < j` over every vocab cluster pair. Pairwise "could both be parts of one X" verdicts from Jev. |
| `candidates.json` | script | `{"k", "method", "params", "sets": [[cluster_id..] ..]}` with each set sorted ascending, sets sorted lexicographically. |
| `coherence.json` | script | `{"model", "question", "sets": {"<i>,<j>,<k>": {"pair_mean", "pair_min", "score", "score_max", "confidence"}}}` plus `coherence_log.jsonl` for dropped sets (`{"set", "reason"}`). `score` is Jev's set-level score on the 0..`score_max` level scale. |
| `availability.json` | script | `{"method": "soft_coverage_topk_median", "top_k", "sets": {"<i>,<j>,<k>": {"availability", "top_actors": [{"actor_id", "name", "coverage"} ..10]}}}`. |
| `results.json` | script | `{"k", "betas": {"<beta>": [{"rank", "atoms": [..], "atom_names": [..], "coherence", "pair_mean", "availability", "z_coherence", "z_unavailability", "fusion", "top_actors": [..3..]}]}, "pool_size", "pool": {"coherence", "availability", "candidates", "intersection", "candidates_never_scored"}, "max_atom_reuse", "n_skipped_reuse": {"<beta>": n}, "fusion_formula"}`. |
| `proposals/beta_<b>/<rank>.md` | agent | Reconstructed proposal for one set. First line `# <title>`; a `<!-- atoms: i,j,k -->` comment on line 2. |
| `judge.json` | agent | Blind ranking of proposals: `{"beta", "ranking": [{"rank", "proposal", "verdict", "reason"}]}`. The judge never sees scores. |
| `holdout.json` | script | Temporal validation output, see `alien_holdout.py`. |

## Scripts

All scripts: Python 3.11+, stdlib + `numpy` + `scikit-learn >= 1.3` (`sklearn.cluster.HDBSCAN`);
`umap-learn` optional and only when asked for. HTTP via `urllib`. Every script takes `--dir DIR`
first. Exit non-zero with a one-line error on any missing input; never substitute a default
for a missing file, key or model. `--seed` defaults to 42 everywhere randomness exists.

### `scripts/alien_cluster.py`

- `embed --dir DIR --embed-url URL --embed-model NAME [--batch 32]`
  POSTs `{"input": [texts], "model": NAME}` to `URL` (an OpenAI-compatible
  `/v1/embeddings`), writes `embeddings.npy` + `embeddings_index.json`. Skips when the
  index's `sha256_of_atoms_jsonl` matches the current `atoms.jsonl`. Resumes in batches
  (keeps a `embeddings.partial.npy` + `.partial.json` until complete).
- `cluster --dir DIR [--min-cluster-size 4] [--min-samples 2] [--reduce none|pca|umap]
  [--components 50] [--umap-neighbors 15] [--reassign-cosine 0.65]`
  Reduction default `pca`; `umap` errors clearly if `umap` is not importable. HDBSCAN on
  L2-normalised vectors (euclidean on unit vectors = cosine ordering). After clustering,
  each noise atom whose cosine to the nearest cluster centroid is ≥ `--reassign-cosine`
  is assigned to that cluster (most confident first, as the source method does). Writes
  `clusters.json` and `clusters_to_name.jsonl`. Prints a summary: clusters, noise
  count, reassigned count, size histogram.
- `finalize --dir DIR`
  Requires `cluster_names.jsonl` to cover every non-noise cluster; otherwise exits
  listing the missing ids. Writes `vocab.json` and `repertoires.json`.

### `scripts/alien_score.py`

- `pairs --dir DIR [--jev-model jev-latest] [--per-request 20] [--concurrency 4]`
  For every pair of vocab clusters, asks Jev one `noul` question per pair, batched
  `--per-request` pairs per HTTP call. The request `state` is
  `{"mechanisms": {"<id>": "<super-atom name>", ...}}` for only the ids in that batch;
  each question id is `"<i>,<j>"` and its `instructions` is
  `domain.json.coherence_question` reworded for the two ids (e.g. "Could mechanisms
  <i> and <j> both be parts of ONE ...?"). Writes `pairs.json`; resumes from a partial
  file. Retries 5xx/429 with backoff up to 5 times, then fails the run.
- `candidates --dir DIR --k 3 [--exhaustive-max-vocab 400] [--n-sample 50000]
  [--pair-min 0.35] [--max-pair-cosine 0.85] [--seed 42]`
  Enumerates every k-set when the vocab is ≤ `--exhaustive-max-vocab`, else samples
  `--n-sample` distinct sets by walking the pair matrix (first atom uniform, each next
  atom drawn proportional to its mean noul with the atoms already chosen). A set is kept
  only if its minimum pairwise noul ≥ `--pair-min` (filter 1), then only if no two of its
  clusters have cosine > `--max-pair-cosine` (filter 2). Cluster vectors are built as in
  `availability`, from `clusters.json` and the embeddings, so near-duplicate atoms are not
  a combination. Writes `candidates.json` with `pair_mean` and `pair_min` per set under the
  top-level `stats` key, and `params` counts `n_enumerated`, `n_dropped_pair_min`,
  `n_dropped_pair_cosine`, `n_kept`. Writes one summary line to `candidates_log.jsonl`
  instead of one line per dropped set.
- `coherence --dir DIR [--jev-model jev-latest] [--keep 2000] [--min-confidence 0.30]
  [--sample-temperature T] [--seed 42] [--include-sets FILE] [--concurrency 4]`
  Selects `--keep` candidates (`--keep 0` is allowed): without `--sample-temperature`, the
  highest `pair_mean`
  (method `top`); with it, a draw without replacement weighted by `exp(pair_mean / T)`
  (method `sample`, Gumbel-top-k with `numpy.random.default_rng(--seed)`). Asks Jev one
  `score` question per set not already answered (one set per request; `state` =
  `{"mechanisms": [names]}`, `instructions` = `domain.json.coherence_question`,
  `criteria` = four fixed levels from "Incompatible or unrelated" to "Clearly one coherent
  <artifact_noun>; each mechanism depends on the others"). An answer with `confidence`
  below `--min-confidence`, a non-finite score, or a transport failure after retries drops
  the set with a logged reason. `coherence.json` is the union of every answered set in
  `coherence.partial.jsonl` across all runs, not only this run's selection; it records this
  run's `selection` = `{"method", "temperature", "seed", "keep", "include_sets", "n_included"}`.
  `--include-sets FILE` (a JSON file with a `targets` list, as written by `alien_holdout.py targets`)
  adds those sets to the selection whether or not `candidates.json` holds them; they are cached like
  any other set. Pair stats (`pair_mean`, `pair_min`) are computed from `pairs.json` for every set.
  Writes `coherence.json` and `coherence_log.jsonl`; resumes.
- `availability --dir DIR [--top-k 10]`
  A cluster vector = L2-normalised mean of its member atom embeddings. Soft coverage of
  a set S by an actor with cluster vectors R: `cov(S, actor)` = mean over s in S of the
  max over r in R of cosine(s, r), i.e. how much of the set the actor already holds.
  Availability = median of the top-`--top-k` cov values across actors (mean of the two
  middle values when `top-k` is even), `method` = `soft_coverage_topk_median`. Each set's
  top 10 actors are recorded with their `coverage`. Scores every set in `coherence.json`.
  Asserts permutation invariance on one set. Writes `availability.json`.
- `select --dir DIR [--betas 0.7] [--top 50] [--max-atom-reuse 3]`
  Pool = sets present in `coherence.json`, `availability.json` and `candidates.json`
  (intersection; counts for each are recorded). Over the pool:
  `z_coherence = z(score)`, `z_unavailability = z(-availability)`;
  `fusion = (1 - beta) * z_coherence + beta * z_unavailability`. Per β, walk the ranking
  in fusion order and keep a set only if none of its atoms already appears in
  `--max-atom-reuse` kept sets; stop at `--top`. Records `n_skipped_reuse` per β.
  Writes `results.json` with names resolved from `vocab.json`.

### `scripts/alien_holdout.py`

- `targets --dir DIR --cutoff YYYY-MM-DD [--k 3]`
  Writes `holdout_targets.json` = `{"cutoff", "k", "unreachable", "targets": [[i,j,k] ..]}`.
  "Old" artifacts = date < cutoff, "new" = date ≥ cutoff (from `corpus.jsonl` dates). Targets
  are the distinct k-subsets of each new artifact's atoms. A target is reachable only when all
  its atoms appear in some old artifact; the reachable ones are written, and the count of
  unreachable ones is recorded. Scoring these targets with `coherence --include-sets` is what
  makes them recoverable.
- `run --dir DIR --cutoff YYYY-MM-DD [--k 3] [--top 300] [--betas 0.0 0.7]`
  Same split and targets as `targets`. A target is "strictly novel" if no old artifact
  contains that whole subset. The pool is the same as `select`'s: sets present in
  `coherence.json`, `availability.json` and `candidates.json`, restricted to sets whose atoms
  all appear in old artifacts (those outside are counted as `excluded_outside_old_atoms`).
  Scored sets missing from the intersection are logged with the reason, and candidates never
  scored are counted (`candidates_never_scored`). Z-scores and fusion are recomputed over this
  pool. No reuse cap. Each target gets one status in `holdout.json` `targets.status_counts`:
  `unreachable`, `excluded_by_candidates` (not in `candidates.json`, so it failed a candidate
  filter such as the cosine cap), `not_in_coherence`, `dropped_in_coherence` (listed in
  `coherence_log.jsonl`), `not_in_availability`, or `in_pool`.
  **Headline ratio:** per β, recovered targets in the top `--top` (capped at pool size),
  against chance = `top / pool_size × targets_in_pool`. Only in-pool targets can be
  recovered, so counting targets outside the pool would dilute the rate with targets that
  say nothing about β. **Secondary:** `chance_all_targets` = `top / pool_size × all targets`
  and `ratio_all_targets`. Both are per β, for all targets and for strictly novel targets.
  Writes `holdout.json` and prints the headline ratios.

## Fixed question texts

Set-level coherence `criteria` (four levels, in order):

1. `Incompatible or unrelated; no single <artifact_noun> would combine them`
2. `Could coexist but at least one mechanism is forced or redundant`
3. `Plausibly one <artifact_noun>; the mechanisms reinforce each other`
4. `Clearly one coherent <artifact_noun>; each mechanism depends on the others`

Jev returns `score` on 0..3 and a `confidence`. `noul` answers carry no confidence.
