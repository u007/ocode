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
    default:
      fail("unknown op " + op);
  }
}
