---
type: Concept
title: Upstash Box and Firecrawl Browser Session Pricing (looked up 2026-11-21)
description: Dated last-known prices and cost-driving lifecycle rules for Upstash Box (CPU core-hours, storage, auto-pause, keep-alive) and Firecrawl Interact browser sessions (credits per browser minute), kept apart as verified, conflicting and derived figures for a proposal-stage comparison. Re-check the live pages before budgeting.
resource: https://upstash.com/docs/box/overall/pricing
tags: [pricing, reference, browser, upstash, firecrawl, cloud-sandbox]
timestamp: 2026-11-21T00:34:30+08:00
status: active
---

# Upstash Box and Firecrawl Browser Session Pricing

> **Prices were looked up on 2026-11-21. Vendor prices change often. Re-check the live pages under Sources before budgeting on any figure here.**

On 2026-11-21 Upstash Box was evaluated as a possible remote browser backend for htrcli/ocode and as a possible remote host, with Firecrawl Interact browser sessions as a comparison point. Nothing is implemented, and this page does not describe a design. It records prices and the lifecycle rules that drive cost.

Positioning: Box bills the CPU time a box consumes and gives a full Chromium driven over CDP with a persistent disk. Firecrawl bills credits per browser minute and is a managed scrape/crawl API with anti-bot handling. They are not substitutes.

Status key: **Verified** means read on an official page on 2026-11-21. **Conflicting** means official sources disagree, or the claim is not on the official page; do not budget on it. **Derived** means our arithmetic on the verified figures, not a vendor figure.

## Upstash Box

### Verified (official pages, read 2026-11-21)

Sources: pricing page, how-it-works, keep-alive.

Billing basis:
- Pay as You Go bills the CPU a box actually consumes, measured in core-hours. The pricing FAQ example: 100% of 2 cores for one hour costs $0.20; 10% of one core for one hour costs $0.01. A standard box with no CPU use incurs no compute charge.
- Rates per active CPU hour: Small (2 vCPU, 4 GB RAM) $0.10; Medium (4 vCPU, 8 GB) $0.20; Large (8 vCPU, 16 GB) $0.40.
- Storage: $0.10 per GB per month. The FAQ says storage covers all data on disk, including snapshots.
- Size and keepAlive are chosen at create time (keep-alive page: pick the size "when creating the box"). Browser and env are also create-time options per the Box SDK provider-options table, which was not re-fetched on the 2026-11-21 pass.

Lifecycle:
- A standard box with no active commands auto-pauses after 1 hour on the free plan and 6 hours on paid plans (how-it-works).
- A paused box releases compute and keeps its filesystem and environment. It resumes on an explicit resume or on any new work, including after days or weeks (how-it-works). A paused box incurs no active compute cost (how-it-works).
- keepAlive: true keeps the box on, disables pause and resume, and bills open time at a fixed hourly rate by size (keep-alive page: billing model "Open-time / always-on based").
- keepAlive requires a paid plan. On the free plan, creating a box with keepAlive: true returns 403 (keep-alive page).

Plans (pricing table):
- Free: $0; 10 concurrent boxes; 5 CPU hours per month.
- Pay as You Go: $0.10 to $0.40 per active CPU hour; 1,000 concurrent boxes by default (a soft limit that can be raised on request); unlimited CPU hours.
- Fixed (keep-alive): $0.0111 to $0.0444 per box per hour. Small $8/month, Medium $16/month, Large $32/month.

Model usage:
- Upstash does not provide or bill model usage. Agent runs, scheduled runs and browser AI calls need the user's own model-provider key. Shell, files, git, SSH, previews and browser CDP work without a key (pricing FAQ).

### Conflicting or unconfirmed (do not budget on these)

- **Medium disk and storage limit.** The pricing table lists disk as Small 10 GB, Medium 30 GB, Large 30 GB. The pricing FAQ ("10 GB for Small, 20 GB for Medium, and 30 GB for Large") and the how-it-works sizes table (10, 20, 30 GB) say Medium is 20 GB. A tracker entry dated 2026-10-23 says Medium pay-as-you-go includes 30 GB; the official pages disagree.
- **`box browser act` per-call cost.** The pricing page says Upstash bills no model usage and that browser AI calls need the user's own model-provider key. The Upstash box CLI skill text says `box browser act` is metered per call because it needs a model. Read together, the per-call cost is model usage on the user's own provider key (our reading; the pricing page lists no separate Upstash meter for it). The same CLI text lists `content`, `goto`, `screenshot` and `close` as not metered; the pricing page does not list them.

### Derived (our arithmetic on the figures above)

- A Small box with both cores fully busy for one hour is 2 core-hours: $0.20.
- 4 consumed core-hours on Small: $0.40.
- A 5 GB checkout kept paused: 5 x $0.10 = $0.50 per month in storage.
- Keep-alive break-even against Pay as You Go: $8 (Small), $16 (Medium) and $32 (Large) each equal 80 consumed core-hours per month at that size's rate. Above about 80 consumed core-hours per box per month, keep-alive is the cheaper option at that size. Storage is charged either way and is excluded from this comparison.

## Firecrawl Interact browser sessions

Sources: billing page, browser feature page and pricing page, all read directly on 2026-11-21.

### Verified (official pages, read 2026-11-21)

Interact rates:
- 2 credits per browser minute when the session runs Playwright code only; 7 credits per browser minute when a prompt drives it. One-minute minimum. Billed per browser minute of session time "whether or not the session got what you wanted" (billing page).
- Zero Data Retention adds 2 credits per minute: 4 code-only, 9 with a prompt.

Session lifecycle:
- A session starts clean by default. A `profile` saves and reuses cookies and localStorage across sessions. Profiles persist browser state but not files on disk. The sandbox filesystem is ephemeral.
- ZDR must be enabled for the team (otherwise 403). Session recordings and persistent profiles are unavailable in ZDR mode (400).

Plans (monthly credits; concurrent browsers; list prices):
- Free: $0; 1,000 credits per month; 2 concurrent browsers. No pay-as-you-go; requests that need credits return HTTP 402 at zero balance until the monthly reset.
- Hobby: $19/month billed monthly ($16/month billed annually); 5,000 credits; 5 concurrent browsers.
- Standard: $99/month billed monthly ($83/month billed annually); 100,000 credits; 25 concurrent browsers.
- Growth: $399/month billed monthly ($333/month billed annually); 500,000 credits; 50 concurrent browsers.
- Scale: $749/month billed monthly ($599/month billed annually); 1,000,000 credits; 100 concurrent browsers.
- Enterprise: custom.

Credits are shared across all endpoints, so browser minutes compete with scrape and crawl credits.

### Derived (our arithmetic at list price, pay-as-you-go and overage excluded)

- Scale price per credit: $749 / 1,000,000 = $0.000749 monthly billing; $599 / 1,000,000 = $0.000599 annual.
- Scale, per browser hour: prompt-driven 420 credits = $0.31 monthly or $0.25 annual; code-only 120 credits = $0.09 monthly or $0.07 annual.
- Hobby credits cost about five times Scale credits per unit ($0.0038 monthly or $0.0032 annual each). Prompt-driven, that is $1.34 to $1.60 per browser hour.
- Free tier: 1,000 credits is about 2.4 prompt-driven browser hours or about 8.3 code-only browser hours.
- Box and Firecrawl bill different units. Box bills consumed core-hours; Firecrawl bills wall-clock browser minutes. A full-use Small Box hour ($0.20) and a Scale Interact hour are not like-for-like.

## Re-check before budgeting

- Upstash pricing page: the Medium disk conflict and the Fixed plan figures.
- Upstash keep-alive and how-it-works pages: idle timeouts and paid-plan rules.
- Per-call cost of `box browser act`: pricing page versus the Upstash CLI skill text.
- Firecrawl billing page section "Interact and Browser sessions" and the plan table.

## Sources

- https://upstash.com/docs/box/overall/pricing (read 2026-11-21: plan table, FAQ, sizes)
- https://upstash.com/docs/box/overall/how-it-works (read 2026-11-21: lifecycle, auto-pause, sizes)
- https://upstash.com/docs/box/overall/keep-alive (read 2026-11-21: keepAlive behavior, paid-plan rule)
- https://www.usagepricing.com/blueprint/activity/upstash-2026-03-09-box-cpu-hour-billing (third-party tracker; not used for any verified figure)
- https://docs.firecrawl.dev/billing.md (read 2026-11-21: Interact rates, plans, concurrency, free tier)
- https://docs.firecrawl.dev/features/browser (read 2026-11-21: sessions, profiles, ZDR limits)
- https://www.firecrawl.dev/pricing (read 2026-11-21: plan prices)
