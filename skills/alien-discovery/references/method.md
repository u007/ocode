# The method, and how it generalises

Source: Artiles et al., *The Alien Space of Science: Sampling Coherent but Cognitively
Unavailable Research Directions*, NeurIPS 2026 (arXiv 2603.01092; code
alejandrohdez00/alien-space-of-science).

## The problem it solves

Asking a language model for "novel ideas" samples the high-density regions of its training
distribution. Every model has roughly the same prior, so every model recommends roughly the
same thing. The paper calls this the availability bias: an idea is proposed only if some
existing community already holds all of its pieces.

The target is the complement: combinations that are **coherent** (they would plausibly form
one viable thing) but **unavailable** (no existing actor is positioned to produce them).
Those are found by scoring, not by prompting.

## The pipeline (science version)

1. **Atomize.** Paper → mechanism-only summary → self-standing "idea atoms" → rate and
   refine. One rule everywhere: the *foreign reader test* (mechanism + why it matters, no
   dangling references, no name used as a crutch). That rule makes atoms from different
   papers comparable.
2. **Cluster into a vocabulary.** Embed atoms, UMAP + HDBSCAN, name each cluster with a
   synthesized "super atom". A paper becomes a set of atom tokens. An author becomes the
   union of atoms across their papers (their repertoire).
3. **Coherence model.** A small causal LM over atom tokens trained on real papers' atom
   sets. Score = mean log-likelihood over all orderings. "Would these atoms belong in one
   piece of work?" Also used to sample candidate sets.
4. **Availability model.** A two-tower contrastive model: author tower = mean-pool of the
   author's repertoire, set tower = transformer over a candidate set, trained so that a
   random subset of an author's atoms matches that author. At inference: cosine against a
   bank of ~40k authors, availability = median of the top-10 similarities.
   **Unavailability = −availability.**
5. **Select and reconstruct.** Draw candidate sets, z-score both scores over the pool,
   fuse `(1−β)·z(coherence) + β·z(unavailability)` with β = 0.7, take the top K, and have
   an LLM write the methodology that would naturally use all the atoms (no invented names
   or numbers, drop none).

Validation is temporal: build from pre-2025 data, measure how many real 2025 combinations
the top selections recover. Reported: 35.9× chance overall; weighting unavailability raises
recovery of *strictly new* combinations to 3.24× chance.

## Why it works (the parts that must survive any port)

- **Plausibility and novelty are separate scores with an explicit dial.** Folding them into
  one prompt ("coherent AND novel") is the paper's own LLM baseline, and it loses.
- **Novelty is relational.** Not "has this been done" but "is any existing actor equipped
  to do it". A combination can be unpublished yet trivially available (one lab has all
  three pieces). Those are penalised.
- **The model never chooses.** Scores choose the combinations; the model only atomizes,
  names, writes and blind-ranks. Letting it choose reintroduces the prior.
- **Coherence learned from co-occurrence is conservative by construction.** The
  unavailability term is the only force pushing off the map. Remove it and the output is
  the mainstream again.

## Mapping to any field

Nothing in steps 3–5 knows about science. Only the nouns change.

| Science | Business / product | Any field |
|---|---|---|
| Paper | Existing business, product, offer | Artifact |
| Author | Founder, company, studio | Actor |
| Idea atom | Business mechanism: segment, channel, pricing mechanism, core capability, trigger, regulatory wedge | Atom |
| Coherence | "Do these mechanisms form one viable business" | "Do these form one viable artifact" |
| Availability | "Is any existing company positioned to build this" | "Is any existing actor positioned to produce this" |
| Reconstruction | Write the business plan that uses all atoms | Write the artifact |

The **actor link is the signal**. A corpus of artifacts without their producers only
measures artifact-novelty (has it been done), which is the weak kind. Harvest the actor
with every artifact, and merge an actor's artifacts into one repertoire.

## What this skill approximates, and how

No GPU, no training. Each learned component is replaced by something that keeps its
*shape*:

| Paper component | Skill approximation | What is lost |
|---|---|---|
| Coherence LM (trained on co-occurrence) | Pairwise Jev `noul` matrix over the whole vocabulary ("could both be parts of one X?"), then a set-level Jev `score` on the shortlist | Learned higher-order structure; pairwise mean is a proxy |
| Candidate sampler (the LM) | Exhaustive k-sets when the vocabulary is small, else a walk over the pair matrix | Nothing material at k ≤ 3 |
| Two-tower availability (trained) | Soft coverage: for each actor, the mean over the set's atoms of the max cosine to any atom in the actor's repertoire ("how much of this set does the actor already hold"); availability = top-10 median across actors | Contrastive sharpening; frozen embeddings are blunter. Mean-pool-vs-mean-pool was tried first and compressed every set into 0.85–0.98, so it was replaced |
| Clustering resolution | Fine HDBSCAN (`--min-cluster-size 2`) plus a redundancy cap: a candidate set is dropped if two of its clusters have cosine > 0.85 | A finer vocabulary leaves near-duplicate clusters, which would otherwise score as "coherent" sets of one idea |
| Portfolio selection | Greedy atom-reuse cap in `select` (`--max-atom-reuse 3`) | The paper reports diversity as a metric; here it is enforced so the top K is not permutations of four atoms |
| UMAP + HDBSCAN | PCA + HDBSCAN (UMAP optional) | Little |
| z-score fusion, β | Identical | Nothing |
| Temporal validation | `alien_holdout.py` on `corpus.jsonl` dates | Scale |

Why pairwise Jev rather than co-occurrence PMI for the pre-filter: PMI of an unseen pair is
zero or negative, so a PMI pre-filter removes exactly the unavailable combinations the
method exists to find. Jev judges plausibility without having seen the pair.

## Limits to state in every report

- **A thin corpus produces false "nobody does this".** Availability can only be measured
  against actors in the bank. Say how many actors the bank holds.
- **Frozen embeddings over-reward near-duplicates.** Two atoms that say the same thing in
  different words look coherent and sit in every actor's neighbourhood. Clustering is what
  controls this; a vocabulary with many near-duplicate clusters needs a larger
  `--min-cluster-size` or a lower `--reassign-cosine`.
- **Coherence is a judgment, not a fact.** The Jev levels are a rubric; the confidence floor
  drops uncertain answers rather than averaging them in.
- **The reconstruction can smooth over a bad set.** That is why the judge is blind and
  ranks proposals, not sets.
