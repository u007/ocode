---
name: atlas
description: "See and use macOS GUIs in a hidden second login (the helper) without touching the person's desktop. Use when a change is visible in a Mac app — a window, dialog, menu, layout or icon — to open the build in the helper and verify it, to reproduce on-screen bugs, run installers, test in Chrome or Firefox, take screenshots, or click and type in apps. Not for unit tests, builds or reading files."
license: MIT
metadata:
  upstream: "https://github.com/InsaneArts/atlas"
  upstream-commit: "8aed936f88556fac046a0264d1ac32ef31c4a1e5"
---

# Atlas

Atlas gives you a hidden macOS login (the helper) to open apps, look, click
and type while the person uses their desktop. If `atlas` is missing or
`doctor` stays not ready, ask them to open Atlas to finish setup.

## In ocode

This plugin is the Atlas skill plus the upstream `atlas` CLI. ocode has no
Atlas tool of its own, so every step below is an ordinary `bash` command.

- ocode's permission rules apply to each `atlas` command like any other shell
  command. Never wrap, alias, background or redirect one to hide what it does.
- ocode does not report a host session to Atlas. Put `ATLAS_SESSION=ocode`
  before every command; beside sibling subagents, add `ATLAS_TASK=NAME` to
  every command too, cleanup included.
- ocode has no embedded Atlas view and no `show_screen` or `take_screenshot`
  tool. Read a screenshot's `path`; run `atlas monitor` only when the person
  asks to watch the helper or take control.
- Never run the Atlas installer, `sudo` or any admin step yourself. Setup
  belongs to the person: https://atlas.insanearts.io/install.sh
- macOS only. On another platform say so instead of trying.

## Rules

- Never act on the person's own desktop. Do the work yourself: open builds,
  copy files in, clean up.
- Never use sudo, type a password, answer a permission prompt, change privacy
  settings, or copy credentials, keychains or tokens into the helper.
- Never set `ATLAS_ACTOR` or act as the person: only they take control or
  resume agents.
- Ask the person before `atlas switch` or anything that locks, sleeps or
  logs out; run `atlas logout` only when they ask.
- Never repeat an action whose outcome is unknown.
- Never claim a change works on screen without a screenshot that shows it.

## Reading output

Commands print compact JSON when their output is not a terminal and exit
non-zero on failure; then read both stdout and stderr. A failure has a
`code`, an `error` and a short `next`. Chain steps with `&&`, never `;`, `|| true` or
`>/dev/null`: those hide failures and `uncertain` replies. Wait with
`atlas wait`, not `sleep`.

## Start

`atlas doctor && atlas windows`. Go on when `ready` is true. Otherwise run the
Atlas commands in `nextSteps` and `warnings`, check again, and tell the
person only what needs them.

Outside Claude Code, Codex, opencode and Pi — ocode included — put
`ATLAS_SESSION=NAME` before every `atlas` command. A subagent shares its
parent's session: beside sibling subagents, put `ATLAS_TASK=NAME` on every
command too, cleanup included.

What `windows` lists was open before you: leave it. Touch only windows you
opened, and never run `atlas close --all`: it quits every app.

Where your host shows Atlas's view (Claude and Codex apps, Cursor), call
`show_screen` once before visible work. ocode has no such view, so run
`atlas monitor` instead when the person wants to watch. When they ask to
watch or take control, open it and leave the helper alone. Never call Stop,
Resume, Take Control or Give Back.

## Work

Two flows have their own sections below: Test your own build, and Give the
app what it needs. Their limits are in Limits.

Open with `atlas open --app NAME [FILE|URL]` or `atlas open /path/App.app`,
then `atlas wait --app NAME`. A listed app control (not a web page's) takes
`atlas element press --ref LOOK.INDEX` (its `look` and `index`; or `check`,
`expand`, their opposites, `set-value --value T`, `pick`). Else
`atlas click --app A --ref LOOK.INDEX` or `--name LABEL [--role ROLE]`,
`atlas click --app A X Y`, `atlas type --app A "text"`,
`atlas key --app A cmd+a`, `scroll`, `drag`: nothing is sent while another
app would get it. Untargeted points use the
top window from your last look. `--app` bypasses this memory;
in a shared app, add `--window N`: `atlas click --app A --window N X Y`.

Finish in few turns:

- Chain known steps with `&&`, each written out in full, no shell
  variables: a whole form, a save and its check.
- For several lines, put real line breaks inside the quotes; never `$'…'`.
- Prove a step by reading it back: `verification: matched`, or
  `atlas elements --app A --values --name X --name Y` for values and
  state. Between steps, screenshot only what text cannot show; end with
  one as evidence.
- Read a screenshot's `path`; ocode has no screenshot tool that accepts it.
- Check results where the app shows them; check a file only when the
  task needs it saved.

Read apps and web pages by name: `atlas elements --app A --name X` for text,
a control, its value and state (`--page`: a web page alone);
`atlas wait --app A` (or `--title T`, `--gone`) for a window,
`--name X --state S` for an element. To see what
shows, crop: `atlas screenshot --app A`, `--window N` or `--region X,Y,W,H`;
a full `atlas screenshot` only when a crop's `overlapping` names a window in
front. Never reopen a screenshot you read or one marked `unchanged: true`.
`ok` means delivered, not that the app reacted.

`atlas run -- PROGRAM ARGS` runs a command as the helper (`--timeout` up to
600, never `&`, never keys or clicks via `osascript` or `cliclick`); a server
that must keep running takes `atlas job start -- PROGRAM ARGS`. Cancel
Save/Open panels: make files with `run`, open them with `open`. Use
Chrome/Firefox for web pages.

A command that changes the helper waits its turn, up to 30 s (`--wait`).
Keep each shell call under ocode's bash timeout (5 minutes by default, up to
600 s), and never end your turn with an Atlas command still running.

## Test your own build

After any change a person could see in a GUI, open the build in the helper
and look at it before saying it works:

1. Build it the way the project does.
2. If `atlas apps` shows an older copy of your app that this task opened,
   close it with `atlas close --app NAME`. If you did not open it, ask the
   person first: another agent may be testing it.
3. `atlas open /path/to/Build/Products/Debug/App.app`, adding
   `--env NAME=VALUE` for any launch variables, then `atlas wait --app NAME`.
   A build the helper cannot read is copied first into a folder the helper may
   read but not change.
4. Check that your build is the one running: `atlas apps` shows each app's
   `executable` path (`atlas apps --all` for a build that runs in the
   background only).
5. Exercise exactly what you changed. Check each state by text where you can,
   and with a crop of the window where you cannot; take a screenshot as
   evidence of each visible state you report. Check the edges too: empty
   states, long text, errors, a second launch.

The helper is its own account, with its own settings, documents, menu bar and
permissions, so an app launched there never changes the person's. If the app
asks for a permission or a sign-in there, tell the person: they allow it or
sign in inside the helper once, and it stays.

`atlas readiness --tool NAME --app NAME` checks the tools and installed apps
the task depends on (not the app you are building: open that by its path).
Name tools by their bare command name (`cat`, not `/bin/cat`). To test an
installer, or an app that must run from Applications,
`atlas install-app /path/App.app` copies it into the helper's
`~/Applications`, never over an app already there; `atlas cleanup --apply`
moves it aside when you are done.

## Give the app what it needs

The helper starts with none of the person's files or settings. When the app
needs some (its settings, a sample document, a tool), copy them yourself;
never ask the person to do it:

1. Write a manifest outside the project, such as in a temporary folder: one
   `[[files]]` table per file, with `source` (the absolute path on this Mac),
   `target` (a plain ASCII path inside the helper's home, like
   `.config/opie/rings.toml`) and `executable = true` for tools (put them in
   `.local/bin`, where `atlas run -- TOOL ARGS` finds them). Copy only what
   the app needs, never credentials, keychains or tokens.
2. Quit the app in the helper first (`atlas close --app NAME`), so it does not
   overwrite the files while they arrive, but only when you alone use it.
3. `atlas environment MANIFEST` previews without changing anything. Read each
   file's `operation`: `conflict` means the helper already has a file Atlas
   did not put there, so stop and tell the person. Keep the `planId`.
4. `atlas environment apply PLAN_ID` copies exactly what was previewed, as a
   one-time snapshot. If it stops partway (`resumeRequired`), run the same
   apply again to finish, or roll it back.

Roll back every copy you applied, newest first:
`atlas environment rollback PLAN_ID --apply`. A file changed in the helper
since the copy stays as it is (`keptTargets`): tell the person. Nothing is
ever copied back to the person's account.

## Limits

- Several agents can use the helper at once; their input takes turns. Touch
  only windows you opened, and pass `--app NAME` to `type`, `key`, `scroll`,
  `drag` and `click X Y`. In an app another agent also uses, add
  `--window N`. An app's settings are shared by all its windows.
- `atlas run --timeout` goes up to 600 seconds, and Atlas needs about 10
  seconds beyond it to stop what is left, so leave headroom.
- Keep every job you started stopped before you finish: `atlas job stop ID`.
- Screenshots and clipboard reads can contain private content. Keep them out
  of public logs and uploads.

## When something goes wrong

- `"uncertain": true` on a reply to your command, or `yours: true`: it may
  or may not have happened. Never repeat it. Take a screenshot, run
  `atlas restart`, look again, then decide. If your host stopped a command
  of yours, run `atlas doctor`; restart only when it says all is yours.
- Any other `uncertain`: someone else's action is unknown. Do not run
  `atlas restart`: wait, and if it stays, ask the person.
- `busy`: someone else has control. When `holder` is `person`, never take it
  from them: wait, and tell them if it lasts. When it is `agent`, `waited`
  says how long Atlas waited: do something else, then try again (if
  `heldBy` is you, wait for your own command).
- `focus-changed`: nothing more was sent. Do what `next` says, then send
  only what was not. `unread: withheld` often means the screen is locked:
  ask the person to unlock it.
- `stopped`: the person stopped agents. Stop at once, say what you were
  doing and ask; recover an uncertain reply only once they resume.
- `on-console`: the helper is on screen; ask the person to switch back.
- `blocked`: your sandbox keeps your shell from the helper. Run Atlas
  outside it; never run `atlas init` for this.
- `session-changed`, `stale-frame` or `lease-lost`: look again first.
- `cancelled`: the person stopped your input; look, then ask.
- Anything else: read `error` and `next`; `next` is the recovery step.
  `atlas --help` lists commands and `atlas <command> --help` documents one.

## Clean up

Always, also after a failure; `atlas activity` lists what you did.

1. Save what you need (`atlas key --app A --window N cmd+s`), then
   `atlas close --app A` for each app you opened: it quits without asking
   to save. In Finder, or an app with windows you did not open, close only
   yours: `atlas key --app A --window N cmd+w`.
2. `atlas collect FILE` what you will report. `atlas cleanup` shows what
   else this session left (with `ATLAS_TASK`, that task's alone): check it,
   then `atlas cleanup --apply` stops its jobs, rolls back its copies and
   moves its installs aside.
3. Delete files you made: `atlas run -- /bin/rm FILE...`, on its own.

Never close or discard what you did not open: another agent's apps,
someone's unsaved work. If it is in your way, tell the person.

## Report

Say briefly what you did and how you checked it, with the screenshot paths
as evidence, and what you could not check and why.
