// Objective-C helper for the macOS desktop shell.
//
// macOS routes a held key to the "press and hold" accent chooser (à á â …)
// instead of repeating the key. That behaviour is implemented in AppKit's text
// input system (NSTextInputContext) and cannot be suppressed from JavaScript —
// calling preventDefault() on the DOM keydown does not stop the native popup,
// and no further keydown events are delivered while it is open. The only
// supported switch is the per-application `ApplePressAndHoldEnabled` user
// default, so key repeat must be restored from native code before the
// WKWebView's input context handles its first key event.
//
// This is the same class of issue VS Code, Cursor, and other Chromium/WebKit
// shells hit. It matters here because the desktop shell embeds a real terminal
// (xterm), Monaco, and chat inputs, all of which need key repeat (vim motions,
// arrow keys, backspace holding).

#import <Foundation/Foundation.h>
#import <WebKit/WebKit.h>

// ocode_disablePressAndHold turns off the accent chooser for this application
// so held keys repeat.
//
// It only writes the default when the key is absent from every preferences
// domain, so an explicit user override — either the app's own domain
// (`defaults write com.u007.ocode ApplePressAndHoldEnabled -bool true`) or the
// global domain (`defaults write -g ApplePressAndHoldEnabled -bool true`) — is
// respected rather than fought on every launch.
void ocode_disablePressAndHold(void) {
	NSUserDefaults *defaults = [NSUserDefaults standardUserDefaults];
	if ([defaults objectForKey:@"ApplePressAndHoldEnabled"] == nil) {
		[defaults setBool:NO forKey:@"ApplePressAndHoldEnabled"];
	}
}

static CGFloat ocode_windowArea(NSView *view) {
	NSRect rect = [view convertRect:view.bounds toView:nil];
	return rect.size.width * rect.size.height;
}

// ocode_findWebView returns the WKWebView to print from the view tree rooted at
// view, or best if none is found under it. Rule: a hidden view and everything
// under it is skipped, and each visible WKWebView is measured by its area in
// window coordinates. The largest one wins, so the main app content beats a
// small auxiliary webview. Ties keep the first one in subview order. A
// WKWebView's own internal subviews are not searched.
static WKWebView *ocode_findWebView(NSView *view, WKWebView *best) {
	if (view.hidden) {
		return best;
	}
	if ([view isKindOfClass:[WKWebView class]]) {
		if (best == nil || ocode_windowArea(view) > ocode_windowArea(best)) {
			return (WKWebView *)view;
		}
		return best;
	}
	for (NSView *sub in view.subviews) {
		best = ocode_findWebView(sub, best);
	}
	return best;
}

// ocode_printWindow shows the system print panel for the WKWebView inside the
// given NSWindow, using the user's saved print settings. Wails' own windowPrint
// hardcodes landscape and centring on the shared NSPrintInfo, so this path does
// not use it. The copy of the shared NSPrintInfo only gets automatic pagination,
// so long content spans pages; orientation, paper size and margins stay as the
// user left them in the panel.
//
// Returns 0 once the panel is running as a sheet, or 1 when the window has no
// visible WKWebView (nothing is shown). The lookup touches AppKit views, so the
// call blocks until it has run on the main thread. It must not be called from
// the main thread, where dispatch_sync would deadlock.
int ocode_printWindow(void *nativeWindow) {
	NSWindow *window = (NSWindow *)nativeWindow;
	__block int status = 0;
	dispatch_sync(dispatch_get_main_queue(), ^{
		WKWebView *webView = ocode_findWebView(window.contentView, nil);
		if (webView == nil) {
			status = 1;
			return;
		}
		NSPrintInfo *info = [[NSPrintInfo sharedPrintInfo] copy];
		info.horizontalPagination = NSPrintingPaginationModeAutomatic;
		info.verticalPagination = NSPrintingPaginationModeAutomatic;
		NSPrintOperation *op = [webView printOperationWithPrintInfo:info];
		op.showsPrintPanel = YES;
		op.showsProgressPanel = YES;
		// WKWebView requires this or printOperationWithPrintInfo: raises, and the
		// rect's values are ignored (scrolled-off content is still printed). This is
		// the same workaround Wails' own windowPrint carries; do not "clean it up".
		op.view.frame = webView.bounds;
		[op runOperationModalForWindow:window delegate:nil didRunSelector:nil contextInfo:nil];
		[info release];
	});
	return status;
}

// WebKit asks "localhost would like to use your microphone" per origin before
// macOS shows its own system prompt, so dictation prompted twice. Wails v3's
// WebviewWindowDelegate (the WKUIDelegate) implements no media-capture
// handler, so WebKit falls back to its own prompt. This category adds one that
// grants ONLY the microphone, and ONLY to the loopback origin the desktop shell
// serves the app from; the macOS TCC prompt (driven by
// NSMicrophoneUsageDescription) stays the single user-facing gate.
//
// Do NOT widen this to every origin: OCODE_DESKTOP_DEV_URL can point the
// webview at another host, and WKMediaCaptureType also covers the camera, which
// no ocode feature uses. A blanket grant would silently hand both to whatever
// page that origin serves.
@interface WebviewWindowDelegate : NSObject
@end

@interface WebviewWindowDelegate (OcodeMediaCapture)
@end

@implementation WebviewWindowDelegate (OcodeMediaCapture)
- (void)webView:(WKWebView *)webView
    requestMediaCapturePermissionForOrigin:(WKSecurityOrigin *)origin
                          initiatedByFrame:(WKFrameInfo *)frame
                                      type:(WKMediaCaptureType)type
                           decisionHandler:(void (^)(WKPermissionDecision decision))decisionHandler
    API_AVAILABLE(macos(12.0)) {
	NSString *host = origin.host;
	BOOL loopback = [host isEqualToString:@"localhost"] ||
	                [host isEqualToString:@"127.0.0.1"] ||
	                [host isEqualToString:@"::1"];
	if (type == WKMediaCaptureTypeMicrophone && loopback) {
		decisionHandler(WKPermissionDecisionGrant);
		return;
	}
	decisionHandler(WKPermissionDecisionDeny);
}
@end
