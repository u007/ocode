# Part 10 — Docs, TODO and spec delta

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
("Docs", "Scope → Deferred").

## Files

- Create: `docs/concepts/pulse-dashboard.md`
- Modify: `docs/index.md`, `docs/log.md` (follow the existing entry format),
  `CHANGES.md`, `TODO.md`
- Modify: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
- Modify: `skills/ocode-web/SKILL.md` if it enumerates web views/shortcuts.

## Steps

- [ ] **Concept doc** covering: purpose; `GET /api/pulse` contract (query,
  row fields, status enum and precedence, sort, cursor, live/all windows);
  `todo_updated` event (not replayed); `pulseStore` receives events for all
  sessions ahead of the `sessionIsTracked` filter; jump sequence
  (`selectProject` → `openSessionTab`); cross-process limitation (desktop
  and a separate dev server each see only their own live registry);
  measured `scope=live` and `scope=all` timings from the endpoint part.
- [ ] **Index/log/CHANGES** entries pointing to the concept doc.
- [ ] **TODO.md** entries, one each: remote-host fan-out; inline
  approve/deny from a card; desktop global hotkey; child sessions as cards;
  wire `todo_updated` into the CoworkSidebar TODO stub
  (`CoworkSidebar.tsx:1056`).
- [ ] **Spec delta**: replace "route `/pulse`" with "`activeView` value
  `"pulse"`" (no client router exists); note idle-card tail comes from the
  last assistant message because live frames clear at turn end; note
  `todo_updated` is published from the server tool-result broadcast.
- [ ] Commit only these doc files: `docs: document Pulse dashboard`.
