# Part 11: Documentation

**Files:**
- Create: `docs/computer-use.md`
- Modify: `AGENTS.md` (add a pointer line in the section that lists feature docs such as `docs/tts-speech-playback.md`; grep `tts-speech-playback` to find it)
- Modify: `README.md` (tool table row for `computer`; grep `imagegen` or `ocr` in the tools table)
- Modify: `CHANGES.md` (new top entry in the existing date-stamped bold format, listing the files)
- Modify: `TODO.md` (under a "Computer use" heading: web Settings toggle, multi-display selection, region zoom, and any pending live verification noted by Parts 08/09)
- Modify: `cmd/ocode-desktop/embedded-assets/skills/ocode-tools/SKILL.md` (add `computer` to the tool list in the same style as neighbouring entries)
- Modify: `skills/ocode-usage/SKILL.md` (one short "Computer use" subsection: enable command, what it does, OS prerequisites)

**Interfaces:**
- Consumes: final behaviour from all previous parts (action list, `/computer` command text, endpoint paths, permission behaviour).

## `docs/computer-use.md` contents

1. What it is: one `computer` tool, action list with one-line meaning each, coordinate space explanation (image pixels of the last screenshot; tool maps to OS coordinates).
2. Enabling: `/computer enable` (TUI or web), or `"computer_use": {"enabled": true}` in the ocode config file; takes effect in new sessions.
3. Permissions: screenshot/cursor/wait auto-allowed; input actions prompt; "always" persists `tool.computer`; locked mode denies everything.
4. Platform prerequisites: macOS (Screen Recording + Accessibility grants for the terminal or ocode-desktop; denied Screen Recording yields wallpaper-only captures with no error); Windows (none; PowerShell on PATH); Linux (X11: `xdotool scrot`; Wayland: `ydotool grim`, wlroots-only, ydotool daemon + uinput).
5. Limitations: primary display only; runs on the machine hosting the ocode process (not the remote SSH workspace); no region zoom.
6. Privacy note: screenshots may include sensitive content and are sent to the model provider.

## Steps

- [ ] **Step 1: Write** `docs/computer-use.md` per the outline. Verify every command and path mentioned exists by grepping the code.
- [ ] **Step 2: Update** AGENTS.md, README.md, CHANGES.md, TODO.md, both SKILL.md files.
- [ ] **Step 3: Verify** `go build ./... && go vet ./internal/computer ./internal/tool ./internal/agent` and `go test ./internal/computer ./internal/tool ./internal/agent ./internal/config ./internal/server ./internal/tui` pass; `cd web && pnpm tsc --noEmit`.
- [ ] **Step 4: Commit** `git add docs/computer-use.md AGENTS.md README.md CHANGES.md TODO.md cmd/ocode-desktop/embedded-assets/skills/ocode-tools/SKILL.md skills/ocode-usage/SKILL.md && git commit -m "docs: computer use tool"`.
