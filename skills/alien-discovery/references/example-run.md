# Example run: indie SaaS for a solo developer (2026-11-07)

Run directory: `/Users/james/www/alien-discovery-runs/indie-saas/`. All LLM work was done
by Haiku subagents; all scoring by Jev (`jev-1.14.0`) and local bge-m3. Read this as a
plumbing proof with an honest negative result on validation, not as a validated idea list.

## Corpus (Stage 1)

| | |
|---|---|
| Artifacts | 165 (YC vertical B2B 50, Starter Story / product sites 50, niche trade tools 52, solo-founder App Store listings 14, minus duplicates) |
| Actors | 169; **no actor has more than one artifact** (serial founders were not found) |
| Dated | 78 of 165; 52 before 2024-01-01, 26 after |
| Blocked sources | indiehackers.com and producthunt.com return 403 to fetches; reddit.com is refused by search |
| Harvest wall time | ~45 min for four parallel Haiku harvesters |

## Vocabulary (Stages 2–3)

| | |
|---|---|
| Atoms | 575 from 165 artifacts (3–8 each; 132 candidate ideas dropped and logged) |
| Clustering | `--min-cluster-size 2 --min-samples 1 --reduce pca --components 50 --reassign-cosine 0.70` |
| Clusters | 134 (98 of size 2–4, 32 of 5–9, 4 of 10+); 54 atoms left as noise |
| Super-atom coherence | 82 high, 42 medium, 10 low (grab bags: sales channels, founder-origin stories, storefronts) |
| Median repertoire | 3 atoms per actor |

## Scoring (Stage 4)

| Step | Count | Jev tokens (in / out) | Time |
|---|---|---|---|
| `pairs` (noul, 20 per request) | 8,911 pairs, 446 requests | 1,023k / 183k | 34 s |
| `candidates --k 3` | 392,084 enumerated → 280,942 kept (108,674 under pair-min 0.35; 2,468 near-duplicate) | | 10 s |
| `coherence` top 2000 by pair mean | 1,998 kept, 2 dropped (confidence < 0.30) | ~1,080k / 51k | ~6 min |
| `coherence` sampled 3000 at T = 0.1 | 2,881 new | ~1,530k / 72k | ~8 min |
| `coherence` holdout targets + baseline sets | 31 new | small | |
| `availability` soft coverage, top-10 median | 4,805 sets, 169 actors; spread 0.716–0.969 | | 5 s |
| `select` β 0.0 / 0.4 / 0.7 / 1.0, top 50, reuse cap 3 | pool 4,576 | | 1 s |

Total Jev: about 3.7M input tokens for the whole run.

## Proposals at β = 0.7 (Stages 5–6)

Fusion rank is the script's; judge rank is a blind Haiku judge that saw only the proposal
texts and `criteria.md`. "Generic?" is the judge's opinion on whether most LLMs would
propose something close to it unprompted.

| Fusion | Judge | Title | Atoms | Coherence 0–3 | Availability | Nearest actors | Generic? |
|---|---|---|---|---|---|---|---|
| 1 | 1 | Guided Lead Paint Job Checklist With Shareable Compliance Records | 31,61,93 | 2.71 | 0.747 | justin-hosking, carl-wills | no |
| 2 | 3 | Job Record and Crew Credential File for Small Renovation Firms | 61,69,79 | 2.11 | 0.738 | angela-freeman, raphael-momanyi | no |
| 3 | 12 | Field Health Dashboard for Small Farms | 60,68,118 | 2.16 | 0.742 | estimatekit, trucker-budget | yes |
| 4 | 6 | Per-Unit Inspection Records With a City Violation View | 97,103,117 | 1.72 | 0.729 | mustafa-gokalp, michael-douglas-pauley | no |
| 5 | 4 | A Renewal Ledger for Real Estate Agents Who Hold Several Licenses | 79,82,101 | 2.77 | 0.762 | angela-freeman, raphael-momanyi | yes |
| 6 | 11 | One Job Record for Small Landlords: Repairs, Proof and Books | 6,21,93 | 1.83 | 0.734 | covera, finlens | yes |
| 7 | 5 | Free Repair Records for Small Landlords, with Paid Follow-Up Added Later | 21,93,118 | 2.25 | 0.749 | carl-wills, firdaosh-bano | yes |
| 8 | 2 | Shop Software for Small Residential Inspection Firms | 97,102,124 | 2.15 | 0.748 | mustafa-gokalp, michael-douglas-pauley | yes |
| 9 | 8 | A Repair-and-Books Service for Landlords Who Manage Their Own Units | 6,21,132 | 1.94 | 0.743 | covera, finlens | yes |
| 10 | 9 | A Renewal and Placement Tool for Small Independent Brokers | 8,11,110 | 2.00 | 0.745 | vantel, covera | yes |
| 11 | 7 | Automated Books for Independent Funeral Homes | 6,35,117 | 1.07 | 0.716 | simplekeep, covera | yes |
| 12 | 10 | A Job-Record Operations Hub for Small Remodelers and Specialty Contractors | 18,69,84 | 2.42 | 0.758 | vector-legal, usman-butt | yes |

Fusion rank 1 and judge rank 1 agree. Fusion rank 3 (farm sensors) is the clearest
failure: the set is coherent to Jev but needs hardware, which the criteria exclude; the
blind judge put it last. The reconstruction agent flagged it with a "Tension" section
without being told the criteria conflict, which is the behaviour P5 asks for.

### Top proposal (atoms 31, 61, 93), as written

> **Guided Lead Paint Job Checklist With Shareable Compliance Records.** Small lead paint
> renovation contractors, and the office worker or crew lead who fills in each job's
> paperwork. Every job needs its own required paperwork completed before work can proceed.
> The product is a phone checklist per job that will not advance until each item has an
> answer; photos, certificates and electronic signatures attach to the job record, paper
> certificates are photographed and read into the same log, and a PDF export turns the
> record into one document anyone asking for proof can read. Go-to-market starts before the
> product is finished, because the trigger is a paperwork step that recurs with every job
> and the first sale is bottom-up through the staff who handle the forms. What is hard: the
> checklist must match the exact questions the regulation asks, which the atoms do not
> contain, so it must be verified with a working contractor before shipping.

Full texts: `proposals/beta_0.7/1.md` … `12.md` in the run directory.

## Baseline: what a plain LLM picks (for contrast)

A Haiku agent was given all 134 super-atoms and the paper's baseline prompt ("coherent and
novel, no founder would have thought of it") five times. Its picks, scored by the same
pipeline:

| Pick | Atoms | Coherence | Availability | Availability percentile in the pool |
|---|---|---|---|---|
| 1 | 51,89,97 | 2.44 | 0.861 | 83rd |
| 2 | 23,36,100 | 2.13 | 0.833 | 54th |
| 3 | 82,101,107 | 2.83 | 0.814 | 34th |
| 4 | 83,111,112 | 2.10 | 0.898 | 98th |
| 5 | 94,102,103 | 2.01 | 0.788 | 12th |

Pick 3 is exactly the pipeline's β = 0 (pure coherence) rank 1: the license-renewal
tracker, which the corpus already contains several times. Picks 1 and 4 sit in the most
available fifth of the pool. The β = 0.7 selections sit at the 0th–10th percentile. That is
the paper's thesis reproduced in miniature: asked for novelty, the model samples the dense
region.

## Temporal holdout (Stage 7): a negative result

Cutoff 2024-01-01; 52 old and 26 new dated artifacts; 97 target triples from new
artifacts, of which 31 are reachable (every atom appears in some old artifact) and 29 in
the scored pool of 1,429 old-atom sets. Chance at top 300 = 6.09 recovered.

| β | Recovered of 29 | Ratio to chance | Exact tail p |
|---|---|---|---|
| 0.0 | 11 | 1.81 | 0.026 |
| 0.4 | 5 | 0.82 | |
| 0.7 | 0 | 0.00 | 0.001 |
| 1.0 | 0 | 0.00 | |

Coherence alone recovers real post-2024 combinations above chance. Adding the
unavailability term removes every one of them from the top 300. On this corpus the
availability approximation therefore anti-correlates with what actually got built. Two
readings are consistent with the data and this run cannot separate them:

- the frozen-embedding soft-coverage availability measures "closeness to the corpus", and
  new real businesses are close to the corpus by construction (the same mainstream);
- with one artifact per actor and 169 actors, "no actor is positioned to do this" collapses
  to "no artifact resembles this", which is artifact-novelty, the weak kind the method
  warns about.

Either way: **the β = 0.7 proposals above are not validated by this run.** They are
coherent-by-Jev combinations far from this small corpus, selected without the model
choosing. That is still different from the baseline, but it is not the paper's result.

## What to change on the next run

1. **Serial actors.** Harvest founders with several products (indie-hacker portfolios,
   studios, agencies that ship tools). Availability needs repertoires larger than one
   artifact.
2. **Dates on every artifact**, so the holdout has more than 29 targets.
3. **A larger corpus**, 500+ artifacts, before trusting any β > 0.
4. Try availability on atom embeddings instead of cluster centroids, and re-run the holdout
   as the acceptance test for any availability change. The holdout is the only check that
   is not the model grading itself.
5. Drop `coherence == "low"` clusters from the vocabulary before `pairs`.
