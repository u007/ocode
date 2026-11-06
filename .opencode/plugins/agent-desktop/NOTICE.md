# agent-desktop plugin notice

This plugin bundles a skill adapted from agent-desktop, by Vercel Labs:

- Upstream: https://github.com/vercel-labs/agent-desktop
- Commit used: `b43ac4f6e892ef608f8851428613c4009f0e84c7` (2026-11-05)
- Upstream license: Apache-2.0 (see `LICENSE` in this directory), Copyright Vercel Labs

## What changed

- `skills/agent-desktop/SKILL.md` keeps agent-desktop's loop, ref, delivery,
  retry and headless/headed guidance verbatim.
- An "In ocode" section documents the host: every step runs through ocode's
  `bash` tool and permission rules, the binary is installed by the person and
  never by the agent, and privacy/`--headed` limits that matter on a shared
  desktop are called out.
- Upstream's `commands-*` and `workflows` references are not shipped. Run
  `agent-desktop skills get desktop <reference>` instead, so the text always
  matches the installed binary's version.
- The per-OS skills (`skills/agent-desktop-macos`, `-windows`, `-linux`) are
  not shipped either; `agent-desktop skills get platform` loads the one that
  matches the running binary.

agent-desktop itself is not bundled. Install it with
`npm install -g agent-desktop` or `brew install agent-desktop`, then run
`agent-desktop status`; the skill says the agent must not install it.
