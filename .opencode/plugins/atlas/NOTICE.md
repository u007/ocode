# Atlas plugin notice

This plugin bundles a skill adapted from Atlas, by InsaneArts:

- Upstream: https://github.com/InsaneArts/atlas
- Commit used: `8aed936f88556fac046a0264d1ac32ef31c4a1e5`
- Upstream license: MIT (see `LICENSE` in this directory), Copyright (c) 2026 InsaneArts

## What changed

- `skills/atlas/SKILL.md` keeps Atlas's rules, output-reading, start, work,
  error, cleanup and report guidance.
- Upstream's `references/{testing,environment,limits}.md` are condensed into
  the skill body instead of shipped as separate files, so the skill is
  self-contained.
- Upstream's `references/{commands,troubleshooting}.md` are replaced by
  `atlas help` and `atlas <command> --help`, which stay correct across Atlas
  versions.
- An "In ocode" section documents the host differences: actions run through
  ocode's `bash` tool and permission rules, `ATLAS_SESSION=ocode` supplies the
  session label, and ocode has no embedded Atlas view or screenshot tool.

Atlas itself is not bundled. Install it separately from
https://atlas.insanearts.io/install.sh; the skill tells the agent to ask the
person to finish that admin setup.
