---
version: 1.0.0
name: cover-letter
description: >
  Write or rewrite a job-application cover letter that is specific, warm, human-sounding and persuasive.
  Use when the user asks to write, improve, "make nicer", tighten or de-slop a cover letter, application
  message or "why should we hire you" answer, for a job or freelance posting.
---

# Cover Letter

Goal: a reader skims it in 60 seconds and thinks "this person understands our problem and has done it before."

## Inputs (gather before writing; read files, don't ask what you can find)

1. **Posting**: role name, company, contact, exact requirements, tools named, location/time-zone/pay terms.
2. **Candidate facts**: CV, biodata, past letters in the repo. Only use facts found there or stated by the user.
3. **Constraints**: form field limits, language, tone the user asked for.

Missing fact that matters (eligibility, rate, notice period)? Ask in the letter as a short question, or ask the user. Never invent.

## Structure (aim 250-350 words, one page)

1. **Opening (1-2 sentences)**: name the work in the posting and why it fits. No "I am writing to apply", no thanking for the posting.
2. **Core pitch (1 paragraph)**: your angle on their problem. One clear position, not a skills list.
3. **Proof (3 bullets max)**: each = requirement from posting -> what you did -> outcome or concrete detail (numbers, system names, tools). Order by relevance to the posting, not by date.
4. **Extras (optional, 1 short paragraph)**: only things that help *this* role (a tool you built, a doc approach). Cut if it does not map to a requirement.
5. **Close**: practical items (location, time zone, availability), any real questions, one friendly line. No "I look forward to hearing from you at your earliest convenience".

## Persuasion rules

- **Mirror their words.** Reuse the posting's terms for tools and tasks so the reader and any ATS see the match.
- **Relate before you sell.** Show you know the pain: one sentence on what is hard about this kind of work (testing, maintenance, handover), then your answer.
- **Evidence over adjectives.** "Built the Curlec payment integration" beats "experienced in payments". Delete "passionate", "dynamic", "results-driven".
- **Claim only what is true.** Every claim traceable to a source. No inflated numbers.
- **Have a point of view.** One opinion the reader can agree or push back on is more memorable than ten matched keywords.
- **Lower the reader's effort.** Short paragraphs, plain words, bullets for proof, answer the obvious worries (location, time zone, rate) before they ask.
- **Friendly, not gushing.** Write like a capable peer emailing a colleague. Contractions ok. One warm touch, not three.
- **Specific ask.** End with the next step you want, or the questions that decide fit.

## De-slop pass (always run last)

Remove:
- Openers/closers: "I am excited to", "I am thrilled", "I hope this finds you well", "Thank you for your time and consideration", "I would welcome the opportunity".
- Words: leverage, utilize, delve, robust, seamless, cutting-edge, passionate, dynamic, synergy, spearhead, testament, tapestry, landscape, realm, pivotal, crucial, ensure, foster, showcase.
- Patterns: "not just X, but Y"; rule-of-three padding; "This is where I come in"; em-dash chains; summary sentence that repeats the paragraph; "That is the core of your role" style echoes of the posting.
- Self-referential filler: "I believe I would be a great fit", "my diverse skill set".
- Hedging stacks and vague nouns ("various", "a range of", "solutions").

Then read aloud test: would a person say this sentence to a hiring manager? If not, rewrite it.

## Output

- Full letter as markdown, header block (From / To / Date / Apply link) matching the user's existing letter files if any (`docs/cv/*cover-letter*.md`).
- Save as a new file (e.g. `<company>-cover-letter-v2.md`); don't overwrite the original unless asked.
- After the letter: 3-line summary of what changed and any open facts the user must confirm.
- If a form copy exists (e.g. `docs/cv/qa/`), say it still holds the old text; update it only when the user picks the new version.
- Never submit an application. Stop and ask for review.
