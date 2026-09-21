---
type: Gotcha
title: Desktop subprocess PATH trap — bare CLI names fail under Finder/Dock-launched .app
description: |-
  Desktop .app processes inherit launchd's minimal PATH; bare argv[0] resolves via exec.LookPath against the process PATH before cmd.Env is consulted, so cmd.Env cannot fix it. Absolute path alone is not enough — the child's PATH must resolve its own runtime. A second, complementary trap: interactive rc files (~/.zshrc) are not sourced by login shells, so directories only on ~/.zshrc (e.g. ~/.local/bin) are invisible to every shell ocode spawns.
  tags:
    - gotcha
    - desktop
    - subprocess
    - PATH
    - exec.LookPath
    - cmd.Dir
    - claude
    - cli
    - zshrc
    - login shell
  resource: internal/agent/advisor_tool.go; internal/shell; internal/tool/bash_build.go; internal/config/user_path.go; main.go; cmd/ocode-desktop/main.go
tags:
  - gotcha
  - desktop
  - subprocess
  - PATH
  - exec.LookPath
  - cmd.Dir
  - claude
  - cli
  - zshrc
  - login shell
timestamp: 2026-09-21T06:04:20Z
resource: ""
---
