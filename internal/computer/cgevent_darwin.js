function main(args) {
  var op = args[0];
  var params = args.slice(1);
  switch(op) {
    case "screen":
      var screen = $.NSScreen.mainScreen.frame.size;
      $.NSLog("%d %d", screen.width, screen.height);
      break;
    case "cursor":
      var loc = $.CGEventGetLocation($.CGEventCreate(null));
      $.NSLog("%d %d", loc.x, loc.y);
      break;
    case "move":
      var x = parseInt(params[0]), y = parseInt(params[1]);
      var move = $.CGEventCreateMouseEvent(null, $.kCGEventMouseMoved, $.CGPointMake(x, y), 0);
      $.CGEventPost($.kCGHIDEventTap, move);
      break;
    case "click":
      var x = parseInt(params[0]), y = parseInt(params[1]);
      var button = params[2], count = parseInt(params[3]);
      var type = button === "left" ? $.kCGEventLeftMouseDown
               : button === "right" ? $.kCGEventRightMouseDown
               : $.kCGEventOtherMouseDown;
      var down = $.CGEventCreateMouseEvent(null, type, $.CGPointMake(x, y), 0);
      $.CGEventSetIntegerValueField(down, $.kCGMouseEventClickState, count);
      $.CGEventPost($.kCGHIDEventTap, down);
      var upType = button === "left" ? $.kCGEventLeftMouseUp
                  : button === "right" ? $.kCGEventRightMouseUp
                  : $.kCGEventOtherMouseUp;
      var up = $.CGEventCreateMouseEvent(null, upType, $.CGPointMake(x, y), 0);
      $.CGEventPost($.kCGHIDEventTap, up);
      break;
    case "drag":
      var x1 = parseInt(params[0]), y1 = parseInt(params[1]);
      var x2 = parseInt(params[2]), y2 = parseInt(params[3]);
      var drag = $.CGEventCreateMouseEvent(null, $.kCGEventLeftMouseDragged, $.CGPointMake(x1, y1), 0);
      $.CGEventPost($.kCGHIDEventTap, drag);
      var dragEnd = $.CGEventCreateMouseEvent(null, $.kCGEventLeftMouseDragged, $.CGPointMake(x2, y2), 0);
      $.CGEventPost($.kCGHIDEventTap, dragEnd);
      break;
    case "scroll":
      var x = parseInt(params[0]), y = parseInt(params[1]);
      var dir = params[2], amount = parseInt(params[3]);
      var warp = $.CGEventCreateMouseEvent(null, $.kCGEventMouseMoved, $.CGPointMake(x, y), 0);
      $.CGEventPost($.kCGHIDEventTap, warp);
      var wheel = $.CGEventCreateScrollWheelEvent($.kCGEventScrollWheel, dir === "horizontal" ? 2 : 0, amount);
      $.CGEventPost($.kCGHIDEventTap, wheel);
      break;
    case "type":
      var text = params[0];
      for (var i = 0; i < text.length; i += 20) {
        var chunk = text.substring(i, Math.min(i + 20, text.length));
        var evt = $.CGEventKeyboardEvent(null, 0, 0);
        $.CGEventKeyboardSetUnicodeString(evt, chunk.length, chunk);
        $.CGEventPost($.kCGHIDEventTap, evt);
      }
      break;
    case "key":
      for (var i = 1; i < params.length; i++) {
        var code = parseInt(params[i]);
        var keyEvt = $.CGEventCreateKeyboardEvent(null, code, true);
        $.CGEventPost($.kCGHIDEventTap, keyEvt);
      }
      for (var i = params.length - 1; i >= 1; i--) {
        var code = parseInt(params[i]);
        var keyEvt = $.CGEventCreateKeyboardEvent(null, code, false);
        $.CGEventPost($.kCGHIDEventTap, keyEvt);
      }
      break;
  }
}
