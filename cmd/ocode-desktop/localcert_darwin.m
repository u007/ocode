// WKWebView certificate pin for the desktop shell's own loopback server.
//
// The shell serves its webview over TLS so WebKit negotiates HTTP/2 (plain
// HTTP/1.1 caps a host at six connections, which long-lived SSE streams
// exhaust). The certificate is a per-launch self-signed one, so WebKit would
// reject it; Wails v3 (beta.12) does not implement
// webView:didReceiveAuthenticationChallenge:completionHandler: on its
// navigation delegate, so it is added here with class_addMethod. It accepts
// exactly the pinned leaf certificate on the loopback host and leaves every
// other challenge to WebKit's default handling.

#import <Foundation/Foundation.h>
#import <WebKit/WebKit.h>
#import <Security/Security.h>
#import <objc/runtime.h>

static NSData *ocodePinnedDER = nil;

static BOOL ocode_isLoopbackHost(NSString *host) {
	return [host isEqualToString:@"127.0.0.1"] || [host isEqualToString:@"localhost"] || [host isEqualToString:@"::1"];
}

static SecCertificateRef ocode_copyLeaf(SecTrustRef trust) {
	if (@available(macOS 12.0, *)) {
		CFArrayRef chain = SecTrustCopyCertificateChain(trust);
		if (chain == NULL) {
			return NULL;
		}
		SecCertificateRef leaf = NULL;
		if (CFArrayGetCount(chain) > 0) {
			leaf = (SecCertificateRef)CFRetain(CFArrayGetValueAtIndex(chain, 0));
		}
		CFRelease(chain);
		return leaf;
	}
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
	if (SecTrustGetCertificateCount(trust) < 1) {
		return NULL;
	}
	return (SecCertificateRef)CFRetain(SecTrustGetCertificateAtIndex(trust, 0));
#pragma clang diagnostic pop
}

static void ocode_didReceiveChallenge(id self, SEL _cmd, WKWebView *webView, NSURLAuthenticationChallenge *challenge,
		void (^completionHandler)(NSURLSessionAuthChallengeDisposition, NSURLCredential *)) {
	NSURLProtectionSpace *space = challenge.protectionSpace;
	if (ocodePinnedDER != nil
			&& [space.authenticationMethod isEqualToString:NSURLAuthenticationMethodServerTrust]
			&& ocode_isLoopbackHost(space.host)
			&& space.serverTrust != NULL) {
		SecCertificateRef leaf = ocode_copyLeaf(space.serverTrust);
		if (leaf != NULL) {
			CFDataRef der = SecCertificateCopyData(leaf);
			BOOL match = der != NULL && [(NSData *)der isEqualToData:ocodePinnedDER];
			if (der != NULL) {
				CFRelease(der);
			}
			CFRelease(leaf);
			if (match) {
				completionHandler(NSURLSessionAuthChallengeUseCredential, [NSURLCredential credentialForTrust:space.serverTrust]);
				return;
			}
		}
	}
	completionHandler(NSURLSessionAuthChallengePerformDefaultHandling, nil);
}

// ocode_pinLocalCert records der as the only certificate accepted for the
// loopback host and installs the challenge handler on Wails' navigation
// delegate class. Must run before the first window (and so the first
// delegate) is created. Returns 0 on success, -1 when the delegate class is
// missing, -2 when it already implements the method (a Wails upgrade added
// one — this override would then be silently skipped).
int ocode_pinLocalCert(const void *der, int len) {
	ocodePinnedDER = [[NSData alloc] initWithBytes:der length:(NSUInteger)len];
	Class cls = NSClassFromString(@"WebviewWindowDelegate");
	if (cls == nil) {
		return -1;
	}
	SEL sel = @selector(webView:didReceiveAuthenticationChallenge:completionHandler:);
	if (!class_addMethod(cls, sel, (IMP)ocode_didReceiveChallenge, "v@:@@@?")) {
		return -2;
	}
	return 0;
}
