# Prompts

Adapted from the Alien Space of Science prompts (`atomization/prompts.py`,
`generation/prompts.py` in alejandrohdez00/alien-space-of-science). Placeholders:

- `{artifact}` — the thing the corpus is made of (research paper, software business, product,
  course, recipe, grant, campaign …).
- `{actor}` — who produces artifacts (author, founder, company, studio, lab …).
- `{atom_definition}` — one sentence saying what an atom is in this domain, from `domain.json`.

Fill them from `domain.json` before sending. Keep the examples: they do more work than the
rules. Replace the example atoms with domain-appropriate ones only when the domain is far
from both science and business (two sets are given below).

## P1 — Mechanism summary (one artifact → mechanism-only description)

Use when the raw source is long (a paper, a landing page plus docs, a long interview).
Skip when `corpus.jsonl.text` is already a mechanism-level description of ≤ 600 words.

```
<task_description>
Rewrite this {artifact} as a clear, intuitive explanation of HOW it works.
</task_description>

<requirements>
- Length: 300–600 words
- Focus: mechanism only — what it does, for whom, through which concrete means
- Exclude: results, metrics, praise, testimonials, pricing tables, history
- Style: plain technical prose a practitioner from a different field can follow
</requirements>

<source>
{source_text}
</source>

<output_format>
Plain markdown, short headings allowed. No preamble.
</output_format>
```

## P2 — Atom extraction (mechanism description → self-standing atoms)

```
<task_description>
Extract the key ideas from the following description of a {artifact}.
These ideas will be used to find conceptual connections across many different
{artifact}s. Each idea must work as a standalone "atom" that could meaningfully
cluster with ideas from completely different {artifact}s.
{atom_definition}
</task_description>

<description>
{text}
</description>

<idea_requirements>
Each idea must be:
1. Self-standing: understandable without context from other extracted ideas
2. No cross-references: must not assume knowledge defined in other ideas from the same extraction
3. Names as anchors, not crutches: a product, method or brand name may be included to
   enable clustering, but the idea must remain understandable to someone who has never
   heard that name
4. Specific enough to be useful: concrete mechanisms, but general enough to cluster with
   ideas from other {artifact}s
5. Complete: include both the mechanism (what it does) and enough context that someone
   from a different field understands why it matters

Each idea must pass the "foreign reader test": a practitioner from a different field
should understand it and be able to connect it to their own work without having seen the
source.
</idea_requirements>

<examples>
GOOD IDEAS (science):

"Associative memory can be modeled as a sparse recovery problem where a network learns
linear constraints that all valid memories must satisfy, then uses those constraints to
identify and remove noise from corrupted input signals."
Reason: explains the reframing, the mechanism, and the purpose. No dependencies.

"Greedy algorithms for monotone submodular maximization guarantee a solution at least
63% as good as the optimum, because diminishing returns bound how much value a locally
optimal choice can lose."
Reason: states the result and why it holds; clusters with approximation guarantees anywhere.

GOOD IDEAS (business):

"Pricing a tool per physical location (per clinic, per truck, per store) instead of per
user removes the internal approval step for every extra seat, so adoption inside one
site spreads without a second sales conversation."
Reason: mechanism, who it is for, and why it works. Clusters with other adoption-friction ideas.

"Selling through a supplier's existing monthly visit to the customer (a distributor rep,
a route driver, an inspector) borrows a trusted relationship and a recurring touchpoint,
so a product reaches a hard-to-reach trade without building its own sales motion."
Reason: a reusable channel mechanism, named concretely, explained without the vendor's name.

"Watching a public record feed (court filings, permits, licence boards) for a specific
event and alerting the one party who must act on it within days turns a free data source
into a time-critical service that customers pay for because the deadline is theirs."
Reason: mechanism plus why the customer pays. Clusters with other trigger-based services.

BAD IDEAS:

"MAC provides universal compatibility by letting researchers mix and match embeddings."
Reason: method name as a crutch; a reader who does not know MAC learns nothing actionable.

"The 'Z' step optimizes coordinates using Barnes-Hut approximation."
Reason: assumes a Z step exists. A procedural fragment, not a standalone insight.

"This significantly improves retention over the baseline."
Reason: dangling "this", no mechanism.

"Acme uses AI to help dentists save time."
Reason: no mechanism; "uses AI" and "save time" could describe anything.
</examples>

<extraction_guidance>
- Prefer mechanisms over features, and features over claims
- If an idea needs a definition to make sense, include the definition inline
- If two ideas depend on each other, merge them or drop the weaker
- Fewer strong ideas beat many weak ones; three to eight per {artifact} is typical
</extraction_guidance>

<output_format>
Return ONLY a valid JSON object, no markdown fences:
{"ideas": ["First self-contained idea", "Second self-contained idea"]}
</output_format>
```

## P3 — Rate and refine

```
<task_description>
You are evaluating and refining extracted "atoms" from descriptions of {artifact}s.
For each idea: analyze its quality as a standalone, transferable insight; rate it
"strong", "medium" or "weak"; for medium or weak ideas provide a revised version that
fixes the issues. The goal is to maximize the number of usable atoms, not to filter
aggressively.
</task_description>

<ideas_to_rate>
{ideas_json}
</ideas_to_rate>

<rating_criteria>
strong: fully self-contained; passes the foreign reader test; mechanism AND why it
matters; no dangling references; names explained, not assumed; would cluster with ideas
from other {artifact}s.
medium: mostly self-contained with one minor issue (slightly too specific, one unexplained
term, significance unclear); fixable with a modest edit.
weak: dangling references, unexplained terms, depends on the source, name used as a
crutch, or purely procedural; may still hold an insight worth rescuing.
</rating_criteria>

<refinement_guidance>
For medium and weak ideas write a revised version that adds the missing context inline,
replaces dangling references with explicit descriptions, expands names into mechanisms,
and converts procedure into insight. If an idea is unrescuable (too vague, or no
underlying insight), set revised_idea to null. Keep revisions to 1–3 sentences.
</refinement_guidance>

<rating_guidance>
Ask of each idea: would someone who never saw the source understand it? Does every term
have enough context? Is there a "this" or "the tool" without an antecedent? Would it
cluster with ideas from OTHER {artifact}s, or only with its siblings? Is a name doing the
explanatory work?
Write the rationale FIRST, then the rating, then the revision.
</rating_guidance>

<output_format>
Return ONLY a valid JSON object:
{"ratings": [{"idea": "exact original text", "rationale": "...",
              "quality": "strong|medium|weak", "revised_idea": "... or null"}]}
Keep the input order.
</output_format>
```

Write to `atoms.jsonl`: strong ideas as-is; medium/weak with a non-null `revised_idea` using
the revision and the ORIGINAL rating as `quality`; drop null revisions and log them.

## P4 — Super-atom naming (one cluster → one canonical atom)

```
<task_description>
You are analyzing a cluster of "atoms" extracted from many different {artifact}s.
Synthesize them into ONE "super atom": a single, self-standing insight that captures the
shared mechanism and value of the whole cluster.
</task_description>

<cluster_atoms>
{atoms_text}
</cluster_atoms>

<super_atom_requirements>
1. It is an atom itself: it passes the foreign reader test without the member atoms.
2. Synthesize, do not summarize: name the underlying principle the members share.
3. Mechanism + purpose: how the shared mechanism works and why it matters across the
   contexts shown.
4. Self-standing: no "this", "these", "the above".
5. Specific enough to be concrete, general enough to explain why the members belong together.
</super_atom_requirements>

<examples>
INPUT ATOMS:
- "Residual connections let gradients bypass layers, preventing vanishing gradients in very deep networks."
- "Highway networks gate information flow across layers so networks with hundreds of layers train."
- "Dense blocks connect every layer to every later layer for maximum feature reuse."
BAD: "Various methods for connecting layers to improve training." (vague, no mechanism)
GOOD: "Architectural shortcuts — residual, gated or dense connections — create direct paths
for signal and gradient flow so very deep models keep signal integrity during training."

INPUT ATOMS:
- "Pricing per clinic location instead of per user removes per-seat approval."
- "A flat fee per truck lets a fleet owner add drivers without a purchasing decision."
- "Per-store pricing means a franchisee's staff can all log in from day one."
BAD: "Different pricing models for small businesses." (no mechanism)
GOOD: "Pricing per physical site rather than per person removes the internal approval
step that each additional seat would trigger, so a tool spreads through one site's staff
without a second purchasing decision."
</examples>

<output_format>
Return ONLY a JSON object:
{"rationale": "the common thread", "super_atom": "the synthesized atom",
 "coherence_score": "low|medium|high"}
</output_format>
```

Write to `cluster_names.jsonl` as `{"cluster_id", "name": super_atom, "rationale",
"coherence": coherence_score}`. A `low` cluster is still named; `select` can filter on it later.

## P5 — Reconstruction (one selected set → one proposal)

```
<task_description>
You are reconstructing a {artifact} from high-level mechanisms called "super-atoms".
Each super-atom is a transferable mechanism extracted from real {artifact}s. Synthesize
them into ONE coherent {artifact} that would naturally use all of them together.
You do not know where these atoms came from. Ask: "what would a {actor} build if they
wanted to use all of these mechanisms together, for one customer?"
</task_description>

<super_atoms>
{super_atoms_text}
</super_atoms>

<requirements>
- Infer one coherent {artifact} that uses every super-atom; drop none.
- It must read as a unified thing, not a list of techniques.
- Describe the mechanism step by step; say why each component matters and how they
  interact; be concrete enough that a reader could start building it.
- Name the specific customer it serves and the moment they would reach for it.
- Use only the information in the super-atoms. Do not invent brand names, numbers,
  customer quotes or market sizes.
- About two pages. Structure: who it is for → problem → approach → key mechanisms → why
  it works → what is hard about it.
</requirements>

<output_format>
Plain markdown. First line is a `# title`. Second line is exactly
`<!-- atoms: {atom_ids} -->`. No JSON, no fences.
</output_format>
```

## P6 — Blind judge (proposals → ranking, never sees scores)

```
<task_description>
Rank these {artifact} proposals. You are given only the proposal texts and the
user's criteria. You are not told how they were generated or scored.
</task_description>

<criteria>
{user_criteria}
</criteria>

<proposals>
{proposals_text}
</proposals>

<output_format>
Return ONLY a JSON object:
{"ranking": [{"rank": 1, "proposal": "<file name>", "verdict": "one sentence",
              "reason": "two or three sentences against the criteria"}]}
Every proposal appears exactly once.
</output_format>
```

## LLM-selection baseline (optional, for comparison only)

The source method's own baseline: give an LLM the whole vocabulary and ask it to pick k
atoms that are "coherent and novel". Run it only to show the difference; never use it to
choose what to pursue. It reproduces the high-density prior the method is designed to avoid.
