# 07 — Docs

- AGENTS.md "Do not pin an HTTP connection" rule: desktop is now h2 over TLS;
  the browser/`ocode serve` path is still HTTP/1.1, so the rule stays.
- `docs/gotchas/project-endpoint-isolation.md`: same note.
- New gotcha: desktop https + pin + sniffing listener + migration.
- CHANGES.md entry; TODO.md: Windows/Linux runtime verification.
- Memory `reference_desktop_debug_evidence`: debug handle still http.
