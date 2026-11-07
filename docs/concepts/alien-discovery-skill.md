---
type: Concept
title: "Alien Discovery Skill — Coherent-but-Unavailable Combinations"
description: "The repo skill skills/alien-discovery: ports the NeurIPS 2026 Alien Space of Science method to any field (atomize, embed, cluster, Jev coherence and availability, beta fusion, reconstruction, blind judge, temporal holdout). Records the Jev request facts it relies on, the IPv6 connect stall in its Python TypeSafe client, and the 2026-11-07 holdout result that does not validate beta > 0."
tags: [alien-discovery, skills, typesafe, jev, embeddings, discovery, holdout]
timestamp: 2026-11-06T19:04:17Z
resource: skills/alien-discovery/SKILL.md
status: active
---
# Alien Discovery Skill

## Overview

`skills/alien-discovery/` finds combinations that are coherent but that no existing actor is positioned to do. It ports the method of "Alien Space of Science" (arXiv 2603.01092) to any field with an observable corpus of artifacts and their producers. The chat model atomizes, names clusters and writes proposals. The scripts choose which combinations are scored and surfaced. The model never picks a combination, because that choice is the availability bias the method removes.

## Pipeline

1. **Domain.** Write `DIR/domain.json` (atom definition, coherence question) and `DIR/criteria.md` (the user's selection criteria). Criteria feed only the blind judge and the reconstruction framing, never scoring.
2. **Harvest.** Subagents collect at least 150 artifacts with their actors, one JSONL file per source.
3. **Atomize.** Subagents extract 3–8 mechanism atoms per artifact and log every dropped revision.
4. **Cluster.** `scripts/alien_cluster.py` embeds atoms with the local bge-m3 `llama-server` (`/v1/embeddings`; find the port with `lsof -nP -iTCP -sTCP:LISTEN | grep llama-server`) and builds an HDBSCAN vocabulary.
5. **Score.** `scripts/alien_score.py` runs a pairwise Jev `noul` matrix over the vocabulary, a set-level Jev `score` for coherence, soft-coverage availability against the actor bank, and beta fusion: `(1 - beta) * z_coherence + beta * z_unavailability`.
6. **Reconstruct and judge.** Subagents write a proposal for each top set. A fresh subagent blind-ranks the proposals from their texts and the criteria only.
7. **Holdout.** `scripts/alien_holdout.py` cuts dated artifacts at a date, scores the pre-cutoff pool and checks whether post-cutoff combinations are recovered above chance.

The scripts call TypeSafe directly over HTTP. They do not go through ocode's Go `TypesafeClient`. Every stage is resumable and logs its drops.

Detail lives in the skill: `references/method.md` (why it works, limits), `references/pipeline-contract.md` (file schemas and flags), `references/prompts.md` (P1–P6), `references/example-run.md` (the proof run and its numbers).

## Jev facts this depends on

Measured 2026-11-07:

- **Many `noul` questions per request.** 20 pair questions in one call ran in 0.40 s at about 2.3k input tokens. `alien_score.py pairs` batches 20 per request (`--per-request`).
- **`noul` answers have no `confidence`.** Only `score` and `choice` answers carry one. The pair path reads `noul` alone.
- **`score` requires `criteria`.** The criteria are an ordered list of 2–10 levels. The score runs `0..len(criteria)-1`. The answer carries `confidence`, `legend` and `probabilities`. Coherence uses four levels, so scores run 0–3.
- **Cost.** 134 vocabulary items give 8,911 pairs, about 1.0M input tokens. The whole example run used about 3.7M Jev input tokens.

## Gotchas

### Python TypeSafe calls stall on IPv6

Python `urllib` and `http.client` requests to `api.typesafe.ai` each hang about 2 minutes on this machine. `getaddrinfo` lists IPv6 first, and the IPv6 connect times out before IPv4 is tried. An IPv4 connect completes in 16 ms, and curl is unaffected because it races the two families. `alien_score.py` avoids the stall in `_OrderedHTTPSConnection`: it sorts IPv4 addresses first and gives each address a 5 s connect timeout. Go's `net` package is not affected, so ocode's own `TypesafeClient` needs no change. Any new Python caller of TypeSafe must reuse that connection class and not `urllib`.

### A high holdout ratio at beta 0.7 is not evidence for the proposals

The 2026-11-07 holdout ran on a 165-artifact corpus with 169 actors and one artifact per actor. The cutoff was 2024-01-01, with 29 scored targets and a chance level of 6.09 at top 300. Coherence alone (beta 0) recovered 11 real post-2024 combinations, 1.8 times chance. Beta 0.7 recovered none. The frozen-embedding availability term is therefore not validated on thin corpora. Proposals produced at beta 0.7 from that run are not validated and must not be presented as such. The numbers are in `references/example-run.md`.

## Related

- [Discovery TypeSafe Relevance Judge](discovery-typesafe-judge.md): the in-product Jev judge pattern (fail-open, confidence floors, state budget).
- [Discoverable Command & Skills Catalog](command-and-skills-catalog.md): the Jev-facing catalog of commands and built-in skills. This skill is not listed there.
- [Local embed server (llama-server) fails with "Exec format error"](../gotchas/local-embed-server-exec-format-error.md): the failure mode of the local embedder the pipeline needs.
