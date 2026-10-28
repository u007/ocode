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
