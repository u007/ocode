// JXA helper for the ocode computer tool. Invoked as:
//   osascript -l JavaScript <this file> <op> [params...]
// Every op prints its documented result to stdout via the return value and
// exits non-zero with a message on stderr on failure. Coordinates are in
// screen points (the CGEvent input space), matching NSScreen.mainScreen.
ObjC.import('Cocoa');
ObjC.import('CoreGraphics');
ObjC.import('ApplicationServices');

function fail(msg) {
  $.NSFileHandle.fileHandleWithStandardError.writeData(
    $.NSString.alloc.initWithUTF8String(msg + "\n").dataUsingEncoding($.NSUTF8StringEncoding));
  ObjC.unwrap($.NSApplication);
  $.exit(1);
}

function requireAccessibility() {
  if (!$.AXIsProcessTrusted()) {
    fail("accessibility permission denied: allow the terminal or ocode-desktop under System Settings > Privacy & Security > Accessibility");
  }
}

function point(x, y) { return $.CGPointMake(x, y); }

function post(evt) { $.CGEventPost($.kCGHIDEventTap, evt); }

function mouseEvent(type, x, y, button) {
  return $.CGEventCreateMouseEvent(null, type, point(x, y), button);
}

function buttonTypes(name) {
  if (name === "left") return { down: $.kCGEventLeftMouseDown, up: $.kCGEventLeftMouseUp, btn: $.kCGMouseButtonLeft };
  if (name === "right") return { down: $.kCGEventRightMouseDown, up: $.kCGEventRightMouseUp, btn: $.kCGMouseButtonRight };
  if (name === "middle") return { down: $.kCGEventOtherMouseDown, up: $.kCGEventOtherMouseUp, btn: $.kCGMouseButtonCenter };
  fail("unknown button " + name);
}

function num(s, what) {
  var n = parseInt(s, 10);
  if (isNaN(n)) fail("invalid " + what + ": " + s);
  return n;
}


function modifierFlag(code) {
  if (code === 55) return $.kCGEventFlagMaskCommand;
  if (code === 59) return $.kCGEventFlagMaskControl;
  if (code === 58) return $.kCGEventFlagMaskAlternate;
  if (code === 56) return $.kCGEventFlagMaskShift;
  fail("keycode " + code + " is not a modifier");
}

// postKey presses and releases one virtual keycode with the given modifier
// flags applied to both events (posting the modifier keys alone does not
// flag the main key event).
function postKey(code, flags) {
  var down = $.CGEventCreateKeyboardEvent(null, code, true);
  $.CGEventSetFlags(down, flags);
  post(down);
  var up = $.CGEventCreateKeyboardEvent(null, code, false);
  $.CGEventSetFlags(up, flags);
  post(up);
}

// typeText enters text into the focused app. CGEventKeyboardSetUnicodeString
// cannot receive a UniChar buffer through the JXA bridge (it types "a" for
// every unit), and System Events keystroke only handles printable ASCII, so:
// printable ASCII goes through keystroke with Return/Tab as key events, and
// anything else is pasted through the general pasteboard. The pasteboard is
// deliberately not restored afterwards: paste delivery is asynchronous and a
// restore races it (verified: even 0.5s later the app pasted the restored
// contents), so the typed text stays on the clipboard. Documented limitation.
function typeText(text) {
  if (/^[\x20-\x7e\n\t]*$/.test(text)) {
    var se = Application("System Events");
    var segs = text.split(/(\n|\t)/);
    for (var i = 0; i < segs.length; i++) {
      if (segs[i] === "\n") postKey(36, 0);
      else if (segs[i] === "\t") postKey(48, 0);
      else if (segs[i].length > 0) se.keystroke(segs[i]);
    }
    return;
  }
  var pb = $.NSPasteboard.generalPasteboard;
  pb.clearContents;
  if (!pb.setStringForType($(text), $.NSPasteboardTypeString)) fail("pasteboard write failed");
  delay(0.1);
  postKey(9, $.kCGEventFlagMaskCommand); // cmd+v
  delay(0.2);
}

// ---- window management (System Events / Accessibility) ----
// Window ids are "<pid>:<1-based index in System Events' window list>"; they
// are only valid until a window opens or closes.

function clean(s) { return String(s).replace(/[\t\r\n]/g, " "); }

function flag(v) { return v ? "1" : "0"; }

function appProcesses() {
  var se = Application("System Events");
  return se.processes.whose({ backgroundOnly: false })();
}

function resolveWindow(id) {
  var m = /^(\d+):(\d+)$/.exec(id);
  if (!m) fail("invalid window id " + id);
  var se = Application("System Events");
  var procs = se.processes.whose({ unixId: parseInt(m[1], 10) })();
  if (procs.length === 0) fail("window " + id + ": process is gone, list windows again");
  var wins = procs[0].windows();
  var idx = parseInt(m[2], 10);
  if (idx < 1 || idx > wins.length) fail("window " + id + ": no longer exists, list windows again");
  return { proc: procs[0], win: wins[idx - 1] };
}

// A process or window whose accessibility attributes cannot be read (it quit
// mid-listing, or exposes no AXMain) is skipped and reported as a "#skipped"
// line that parseWindowList logs, so one bad window never fails the listing.
function listWindows() {
  var lines = [];
  var procs = appProcesses();
  for (var i = 0; i < procs.length; i++) {
    var proc = procs[i];
    var app = "?";
    try {
      var pid = proc.unixId();
      app = proc.name();
      var front = proc.frontmost();
      var wins = proc.windows();
    } catch (e) {
      lines.push("#skipped\t" + clean(app) + "\t" + clean(String(e)));
      continue;
    }
    for (var j = 0; j < wins.length; j++) {
      try {
        var w = wins[j];
        var pos = w.position(), size = w.size();
        var min = w.attributes.byName("AXMinimized").value();
        var main = w.attributes.byName("AXMain").value();
        lines.push([pid + ":" + (j + 1), clean(app), clean(w.name() || ""),
          Math.round(pos[0]), Math.round(pos[1]), Math.round(size[0]), Math.round(size[1]),
          flag(min), flag(front && main)].join("\t"));
      } catch (e) {
        lines.push("#skipped\t" + clean(app) + " window " + (j + 1) + "\t" + clean(String(e)));
      }
    }
  }
  return lines.join("\n");
}

function focusWindow(id) {
  var r = resolveWindow(id);
  r.win.attributes.byName("AXMinimized").value = false;
  r.proc.frontmost = true;
  r.win.actions.byName("AXRaise").perform();
  return "";
}

function setBounds(id, x, y, w, h) {
  var r = resolveWindow(id);
  r.win.attributes.byName("AXMinimized").value = false;
  r.win.position = [x, y];
  r.win.size = [w, h];
  return "";
}

function windowState(id, state) {
  var r = resolveWindow(id);
  if (state === "minimize") {
    r.win.attributes.byName("AXMinimized").value = true;
  } else if (state === "restore") {
    r.win.attributes.byName("AXMinimized").value = false;
  } else if (state === "maximize") {
    // The green zoom button enters native full screen on current macOS, so
    // fill the primary screen's visible frame (below menu bar, beside Dock).
    var screen = $.NSScreen.mainScreen;
    var vf = screen.visibleFrame, full = screen.frame;
    var top = full.size.height - (vf.origin.y + vf.size.height);
    r.win.attributes.byName("AXMinimized").value = false;
    r.win.position = [Math.round(vf.origin.x), Math.round(top)];
    r.win.size = [Math.round(vf.size.width), Math.round(vf.size.height)];
  } else if (state === "close") {
    var btns = r.win.buttons.whose({ subrole: "AXCloseButton" })();
    if (btns.length === 0) fail("window " + id + " has no close button");
    btns[0].click();
  } else {
    fail("unknown window state " + state);
  }
  return "";
}

function run(argv) {
  var op = argv[0];
  var p = argv.slice(1);
  switch (op) {
    case "screen": {
      var size = $.NSScreen.mainScreen.frame.size;
      return Math.round(size.width) + " " + Math.round(size.height);
    }
    case "cursor": {
      var loc = $.CGEventGetLocation($.CGEventCreate(null));
      return Math.round(loc.x) + " " + Math.round(loc.y);
    }
    case "move": {
      requireAccessibility();
      var x = num(p[0], "x"), y = num(p[1], "y");
      post(mouseEvent($.kCGEventMouseMoved, x, y, $.kCGMouseButtonLeft));
      return "";
    }
    case "click": {
      requireAccessibility();
      var x = num(p[0], "x"), y = num(p[1], "y");
      var bt = buttonTypes(p[2]);
      var count = num(p[3], "count");
      post(mouseEvent($.kCGEventMouseMoved, x, y, bt.btn));
      for (var i = 1; i <= count; i++) {
        var down = mouseEvent(bt.down, x, y, bt.btn);
        $.CGEventSetIntegerValueField(down, $.kCGMouseEventClickState, i);
        post(down);
        var up = mouseEvent(bt.up, x, y, bt.btn);
        $.CGEventSetIntegerValueField(up, $.kCGMouseEventClickState, i);
        post(up);
        if (i < count) delay(0.05);
      }
      return "";
    }
    case "drag": {
      requireAccessibility();
      var x1 = num(p[0], "x1"), y1 = num(p[1], "y1"), x2 = num(p[2], "x2"), y2 = num(p[3], "y2");
      post(mouseEvent($.kCGEventMouseMoved, x1, y1, $.kCGMouseButtonLeft));
      post(mouseEvent($.kCGEventLeftMouseDown, x1, y1, $.kCGMouseButtonLeft));
      delay(0.05);
      var steps = 8;
      for (var s = 1; s <= steps; s++) {
        var mx = Math.round(x1 + (x2 - x1) * s / steps);
        var my = Math.round(y1 + (y2 - y1) * s / steps);
        post(mouseEvent($.kCGEventLeftMouseDragged, mx, my, $.kCGMouseButtonLeft));
        delay(0.02);
      }
      post(mouseEvent($.kCGEventLeftMouseUp, x2, y2, $.kCGMouseButtonLeft));
      return "";
    }
    case "scroll": {
      requireAccessibility();
      var x = num(p[0], "x"), y = num(p[1], "y");
      var dir = p[2], amount = num(p[3], "amount");
      var dy = 0, dx = 0;
      if (dir === "up") dy = amount;
      else if (dir === "down") dy = -amount;
      else if (dir === "left") dx = amount;
      else if (dir === "right") dx = -amount;
      else fail("unknown scroll direction " + dir);
      post(mouseEvent($.kCGEventMouseMoved, x, y, $.kCGMouseButtonLeft));
      post($.CGEventCreateScrollWheelEvent(null, $.kCGScrollEventUnitLine, 2, dy, dx));
      return "";
    }
    case "type": {
      requireAccessibility();
      typeText(p[0]);
      return "";
    }
    case "key": {
      requireAccessibility();
      var codes = p.map(function (c) { return num(c, "keycode"); });
      var main = codes[codes.length - 1];
      var flags = 0;
      for (var i = 0; i < codes.length - 1; i++) flags |= modifierFlag(codes[i]);
      for (var i = 0; i < codes.length - 1; i++) post($.CGEventCreateKeyboardEvent(null, codes[i], true));
      postKey(main, flags);
      for (var i = codes.length - 2; i >= 0; i--) post($.CGEventCreateKeyboardEvent(null, codes[i], false));
      return "";
    }
    case "windows":
      requireAccessibility();
      return listWindows();
    case "window-focus":
      requireAccessibility();
      return focusWindow(p[0]);
    case "window-bounds":
      requireAccessibility();
      return setBounds(p[0], num(p[1], "x"), num(p[2], "y"), num(p[3], "w"), num(p[4], "h"));
    case "window-state":
      requireAccessibility();
      return windowState(p[0], p[1]);
    default:
      fail("unknown op " + op);
  }
}
