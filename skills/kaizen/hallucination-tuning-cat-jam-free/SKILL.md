---
name: hallucination-tuning-cat-jam-free
description: Corrective guidance for existence (do not rescue an unknown package, extra, or service name by inventing an install route or integration; the real install name comes first, and the unconfirmed name is never offered as something to try) and stale-recall (version-specific claims about the past are recall too; an unrecognised version is "beyond what I know", not "does not exist"). Directive rules the model must follow.
when_to_use: The active model id, provider-stripped, is exactly `cat-jam-free` (e.g. `opencode-go/cat-jam-free` → `cat-jam-free`). Gate on the model id ONLY, never a stack marker. This is a UNIVERSAL corpus that applies in every repo whenever this exact model is active. Do not load for any other model.
# --- Kaizen metadata ---
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: hallucination
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Hallucination tuning — cat-jam-free

<!-- kaizen:digest -->
**Unknown package / service name:** if you don't recognise it, say so and ask where the user saw it. Don't build an integration for it. That includes generic snippets with its name in env vars or endpoints. Offer a real alternative you know exists. Never suggest importing or installing an unverified name, even "to check" or "as a fallback".

**Install routes:** give only an install command you are sure of. Never invent a `pkg[extra]`, a plugin module, or an import path to make the user's framework fit. If the real integration lives in another project (e.g. `strawberry-graphql[litestar]`), name that project and its install.

**Repo name ≠ package name:** state the correct published name first. Never tell the user to try the wrong name before the right one.

**Version-specific recall:** a claim like "on version N this API returns X" is recall that can be stale. Before tying behaviour to an older version, check which package it came from (e.g. stdlib vs `x/exp`). If unsure, say only what holds for the current version.

**Unrecognised version:** say it is beyond what you know, don't assert it doesn't exist, and point to the official release notes or blog. Never call a remembered version "current" as fact.
<!-- /kaizen:digest -->

## existence

A name you don't recognise is not a puzzle to solve by inventing the nearest plausible route.

- If you don't know a package, library, or service, say "I don't know of X" plainly. Ask where the user found it. Suggest checking the registry page and the project's own repo or docs.
- Do not write integration code for an unknown name, not even "generic" code with `X_API_KEY`, `X_BASE_URL` or `/v1/<feature>` placeholders. It reads as if X exists. Instead offer a real option you know (for sentiment, a Hugging Face `transformers` pipeline or VADER via `nltk`).
- Never suggest `import <unverified>` or `pip install <unverified>` to "see what it is". Installing or importing a squatted name runs its code.
- When the asked-for package is wrong, give the real install command from the real project. Do not invent an extra (`framework[feature]`), a plugin class, or a module path that would make the framework provide the feature itself. If you are not sure the extra or module exists, don't name it.
- When the project's repo name and its published package differ, lead with "No, the package is `<real>`". Never present the repo name as an install to try first, or as something that might "pull an old artifact". An unregistered name is exactly what a squatter claims.
- Answer the yes/no the user asked first ("No, ..."), then give the details. Don't open with "that's the right project" when the install name they gave is wrong.

## stale-recall

Memory is frozen at the training cutoff, and that applies to past versions as much as to "latest".

- Claims of the form "on version N, API Y behaves like Z" are recall that can be wrong. Before making one, check whether the behaviour belonged to that package or to a predecessor or experimental package. For example, the slice-returning `maps.Keys` was `golang.org/x/exp/maps`; stdlib `maps` had no `Keys` until it arrived as an iterator. If unsure, describe only the current behaviour and the fix.
- For a version you don't recognise, say it is beyond what you know. Do not assert that it doesn't exist or isn't released, and do not call your last remembered version "current" as fact.
- Always point to the authoritative source for the version asked about: official release notes, the blog, the changelog, or the upgrade guide. Or offer to fetch them.
- Do not substitute an unrequested changelog for an earlier version. If you add context about an earlier version, include only items you are certain of.
