# Part 01: darwin JXA CGEvent spike (throwaway)

**Purpose:** Confirm that `osascript -l JavaScript` with `ObjC.import('CoreGraphics')` can post mouse and keyboard events and read the cursor location and main-screen size on the current macOS. The spec names AppleScript `System Events` as the fallback if this fails. Nothing from this part is kept in the repo.

**Files:**
- Create (scratch only, outside the repo): `<scratchpad>/cgevent_spike.js`

**Interfaces:**
- Produces: a written decision recorded in `docs/superpowers/specs/2026-09-12-computer-use-tool-design.md` under "### darwin" (one sentence: "Spike 2026-09-12: JXA CGEvent works on macOS <version>" or "...fails; darwin driver uses System Events").

## Steps

- [ ] **Step 1: Write the probe script.** In the scratch file, import `CoreGraphics` and `AppKit` via `ObjC.import`, then, in order: print `$.NSScreen.mainScreen.frame.size` width and height; read the cursor via `$.CGEventGetLocation($.CGEventCreate(null))`; move the mouse to (200, 200) with `$.CGWarpMouseCursorPosition`; post a mouse-move event created by `$.CGEventCreateMouseEvent(null, $.kCGEventMouseMoved, {x:200,y:200}, $.kCGMouseButtonLeft)` through `$.CGEventPost($.kCGHIDEventTap, ev)`; post a scroll-wheel event from `$.CGEventCreateScrollWheelEvent(null, $.kCGScrollEventUnitLine, 1, -3)`; post a keyboard event for the `shift` key down then up via `$.CGEventCreateKeyboardEvent(null, 56, true/false)`; post a unicode string via `$.CGEventKeyboardSetUnicodeString` on a keyboard event with keycode 0 and print "done". Do NOT click, and do not type printable characters into whatever window is focused. Read the cursor again at the end and print it.

- [ ] **Step 2: Run it.** `osascript -l JavaScript <scratchpad>/cgevent_spike.js`. Expected: screen size printed, cursor before and after printed, final cursor equals (200,200), exit code 0.

- [ ] **Step 3: If a struct argument fails** (error mentions `CGPoint` or "cannot convert"), retry the move using `$.CGPointMake(200, 200)` as the point argument. If that also fails, the decision is System Events.

- [ ] **Step 4: If the event post fails with a TCC/accessibility error**, grant Accessibility to the terminal in System Settings → Privacy & Security → Accessibility and rerun once. Record which grant was needed.

- [ ] **Step 5: Record the decision** in the spec's darwin section as one sentence with the macOS version (`sw_vers -productVersion`). Note which point-construction form worked (`{x,y}` literal or `CGPointMake`); Part 07 depends on this.

- [ ] **Step 6: Commit** only the spec file: `git add docs/superpowers/specs/2026-09-12-computer-use-tool-design.md && git commit -m "docs: record darwin CGEvent spike result"`.
