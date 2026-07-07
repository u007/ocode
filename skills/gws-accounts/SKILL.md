---
version: 1.0.0
name: gws-accounts
description: "gws CLI: Switch between multiple Gmail/Google Workspace accounts (james@mercstudio.com, c00lways@gmail.com). Use before any gws command when the task involves a specific account or both accounts."
---

# gws Multi-Account Switching

> **PREREQUISITE:** Read `../gws-shared/SKILL.md` for auth, global flags, and security rules.

**Project root:** `/Users/james/www/personal`

## Accounts

| Alias | Email | Use for |
|-------|-------|---------|
| `james` | james@mercstudio.com | Work / Merc Studio (default) |
| `c00lways` | c00lways@gmail.com | Personal / DigitalOcean billing |

## Switching

Always run from `/Users/james/www/personal`:

```bash
make gws-james       # switch to james@mercstudio.com
make gws-c00lways    # switch to c00lways@gmail.com
make gws-status      # show currently active account
```

## Multi-Account Pattern

```bash
# Check both inboxes
make gws-james && gws gmail +triage --max 10
make gws-c00lways && gws gmail +triage --max 10
make gws-james  # switch back to default
```

## Adding a New Account

```bash
gws auth logout
gws auth login
cp ~/.config/gws/credentials.enc ~/.config/gws/credentials.<alias>.enc
# Add make target to Makefile and update AGENTS.md
```

## See Also

- [gws-shared](../gws-shared/SKILL.md) — Auth, global flags, security rules
- [gws-gmail](../gws-gmail/SKILL.md) — Gmail commands
