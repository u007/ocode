# Part 05 — Capture-Script Injection + Client-Side URL Reroute

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (§ Injected capture script, § Unrewritable, patched client-side).

**Goal:** Inject a dependency-free capture script into every proxied HTML document. The script (1) forwards `console.*`, uncaught errors, and unhandled rejections to the SPA; (2) records `fetch`/`XHR` network telemetry; (3) reroutes absolute cross-origin URLs constructed in JavaScript back through the `/b/{stateKey}/...` proxy so they don't escape the browse origin; (4) posts a *display-untrusted* nav hint. All `postMessage` calls target the SPA origin explicitly — never `"*"`.

**Files:**
- Create: `internal/browse/capture.go`
- Create: `internal/browse/capture.js` (embedded via `go:embed`)
- Create: `internal/browse/capture_test.go`
- Modify: `internal/browse/server.go` (register `GET /__ocode_capture.js` on the browse mux)

**Interfaces:**
- Consumes (from Part 01): `Server` + its `mux` in `internal/browse/server.go`; `target` shape `/b/{stateKey}/{scheme}/{host}/...`.
- Produces (consumed by Part 03 — external rewrite): `injectCapture(html []byte, stateKey, spaOrigin string) []byte`.
- Produces (consumed by Parts 08/09 — SPA store + panel): the `window.postMessage` event contract:
  - `{ type: "ocode:browse:console", stateKey, level, args, ts }`
  - `{ type: "ocode:browse:network", stateKey, method, url, status, duration, ts }`
  - `{ type: "ocode:browse:nav", stateKey, url, ts }` — **display-untrusted latency hint only** (authoritative address bar comes from the server nav events in Part 07; the SPA MAY ignore this).

---

- [ ] **Step 1: Write the failing injection test**

Create `internal/browse/capture_test.go`:

```go
package browse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInjectCaptureInsertsBootstrapAndScript(t *testing.T) {
	in := []byte(`<!doctype html><html><head><title>x</title></head><body>hi</body></html>`)
	out := string(injectCapture(in, "tab:abc", "http://127.0.0.1:5000"))

	// Bootstrap must carry the exact stateKey + spaOrigin.
	if !strings.Contains(out, `window.__OCODE_BROWSE__`) {
		t.Fatal("missing bootstrap global")
	}
	if !strings.Contains(out, `"tab:abc"`) || !strings.Contains(out, `"http://127.0.0.1:5000"`) {
		t.Fatalf("bootstrap missing stateKey/spaOrigin: %s", out)
	}
	// Script tag referencing the served capture bundle must be present.
	if !strings.Contains(out, `src="/__ocode_capture.js"`) {
		t.Fatal("missing capture script tag")
	}
	// Injection must land inside <head>, before the existing <title>.
	headIdx := strings.Index(out, "<head>")
	titleIdx := strings.Index(out, "<title>")
	bootIdx := strings.Index(out, "__OCODE_BROWSE__")
	if !(headIdx < bootIdx && bootIdx < titleIdx) {
		t.Fatalf("bootstrap not first child of head: head=%d boot=%d title=%d", headIdx, bootIdx, titleIdx)
	}
}

func TestInjectCaptureFallbackNoHead(t *testing.T) {
	// A fragment without <head> should still receive the script (before </html>
	// or appended), so capture never silently no-ops.
	in := []byte(`<html><body>hi</body></html>`)
	out := string(injectCapture(in, "tab:x", "http://o"))
	if !strings.Contains(out, `src="/__ocode_capture.js"`) {
		t.Fatalf("fallback injection missing: %s", out)
	}
}

func TestServeCaptureBundle(t *testing.T) {
	s := New("apitoken", nil)
	r := httptest.NewRequest("GET", "/__ocode_capture.js", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") &&
		!strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("content-type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), "ocode:browse:console") {
		t.Fatal("served bundle missing console event type")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/browse/ -run Capture`
Expected: FAIL — `undefined: injectCapture`, and `/__ocode_capture.js` returns 401/404.

- [ ] **Step 3: Write the embedded capture JS**

Create `internal/browse/capture.js`:

```js
// ocode browse capture script — dependency-free, injected into every proxied
// HTML document. Runs first in <head> so it wraps console/fetch before page
// code loads. All telemetry is posted to the SPA origin ONLY (never "*").
//
// Coverage honesty: this reroutes URLs that flow through fetch/XHR/WebSocket
// and the constructors we can wrap. It CANNOT reach:
//   - requests issued from a Web Worker / Service Worker (separate global;
//     Service Workers are blocked at the proxy in Part 06 anyway),
//   - URLs already baked into <img>/<script> markup (those are rewritten
//     server-side in Part 04),
//   - native module `import()` of a literal absolute URL in some engines.
// Such requests will fail against the browse origin and surface as a network
// error in the console — visible, not silent.
(function () {
  "use strict";
  var cfg = window.__OCODE_BROWSE__ || {};
  var stateKey = cfg.stateKey || "";
  var spaOrigin = cfg.spaOrigin || "";
  if (!spaOrigin) return; // nothing to report to; do not touch the page.

  function post(msg) {
    msg.stateKey = stateKey;
    try {
      window.parent.postMessage(msg, spaOrigin);
    } catch (e) {
      // Parent gone or origin mismatch: nothing we can do, don't loop.
    }
  }

  // --- /b/ base of the current document, e.g. "/b/tab:abc" ---
  function browseBase() {
    // location.pathname === /b/{stateKey}/{scheme}/{host}/{path...}
    var m = location.pathname.match(/^\/b\/[^/]+/);
    return m ? m[0] : "/b/" + encodeURIComponent(stateKey);
  }

  // Map an absolute URL to /b/{stateKey}/{scheme}/{host}/{path...}. Returns the
  // input unchanged for relative URLs (already correctly scoped) and for URLs
  // already under the browse base.
  function reroute(raw) {
    if (typeof raw !== "string") {
      try { raw = String(raw); } catch (e) { return raw; }
    }
    if (!/^https?:\/\//i.test(raw)) return raw; // relative — leave as-is.
    var base = browseBase();
    if (raw.indexOf(location.origin + base + "/") === 0) return raw;
    var u;
    try { u = new URL(raw); } catch (e) { return raw; }
    var scheme = u.protocol.replace(":", "");
    return location.origin + base + "/" + scheme + "/" + u.host + u.pathname + u.search + u.hash;
  }

  // --- console.* ---
  var levels = ["log", "info", "warn", "error", "debug"];
  levels.forEach(function (lvl) {
    var orig = console[lvl] ? console[lvl].bind(console) : function () {};
    console[lvl] = function () {
      var args = [];
      for (var i = 0; i < arguments.length; i++) {
        var a = arguments[i];
        try {
          args.push(typeof a === "object" ? JSON.stringify(a) : String(a));
        } catch (e) {
          args.push(String(a));
        }
      }
      post({ type: "ocode:browse:console", level: lvl, args: args, ts: Date.now() });
      orig.apply(null, arguments);
    };
  });

  window.addEventListener("error", function (ev) {
    post({
      type: "ocode:browse:console",
      level: "error",
      args: [String(ev.message || ev.error || "error"), (ev.filename || "") + ":" + (ev.lineno || 0)],
      ts: Date.now(),
    });
  });
  window.addEventListener("unhandledrejection", function (ev) {
    var reason = ev && ev.reason;
    post({
      type: "ocode:browse:console",
      level: "error",
      args: ["Unhandled rejection: " + (reason && reason.message ? reason.message : String(reason))],
      ts: Date.now(),
    });
  });

  // --- fetch ---
  if (window.fetch) {
    var origFetch = window.fetch.bind(window);
    window.fetch = function (input, init) {
      var url = typeof input === "string" ? input : (input && input.url) || "";
      var method = (init && init.method) || (input && input.method) || "GET";
      var routed = reroute(url);
      var arg = routed === url ? input : routed;
      var start = Date.now();
      return origFetch(arg, init).then(
        function (res) {
          post({ type: "ocode:browse:network", method: method, url: url, status: res.status, duration: Date.now() - start, ts: start });
          return res;
        },
        function (err) {
          post({ type: "ocode:browse:network", method: method, url: url, status: 0, duration: Date.now() - start, ts: start });
          throw err;
        }
      );
    };
  }

  // --- XMLHttpRequest ---
  var XHR = window.XMLHttpRequest;
  if (XHR) {
    var open = XHR.prototype.open;
    var send = XHR.prototype.send;
    XHR.prototype.open = function (method, url) {
      this.__ocode = { method: method, url: url, start: 0 };
      var routed = reroute(url);
      return open.apply(this, [method, routed].concat([].slice.call(arguments, 2)));
    };
    XHR.prototype.send = function () {
      var self = this;
      if (self.__ocode) {
        self.__ocode.start = Date.now();
        self.addEventListener("loadend", function () {
          var o = self.__ocode;
          post({ type: "ocode:browse:network", method: o.method, url: o.url, status: self.status, duration: Date.now() - o.start, ts: o.start });
        });
      }
      return send.apply(this, arguments);
    };
  }

  // --- WebSocket (reroute ws/wss to the browse origin) ---
  var OrigWS = window.WebSocket;
  if (OrigWS) {
    var Wrapped = function (url, protocols) {
      var routed = url;
      if (/^wss?:\/\//i.test(url)) {
        try {
          var u = new URL(url);
          var scheme = u.protocol.replace(":", "");
          routed = (location.protocol === "https:" ? "wss://" : "ws://") + location.host + browseBase() + "/" + scheme + "/" + u.host + u.pathname + u.search;
        } catch (e) { routed = url; }
      }
      return protocols === undefined ? new OrigWS(routed) : new OrigWS(routed, protocols);
    };
    Wrapped.prototype = OrigWS.prototype;
    Wrapped.CONNECTING = OrigWS.CONNECTING; Wrapped.OPEN = OrigWS.OPEN;
    Wrapped.CLOSING = OrigWS.CLOSING; Wrapped.CLOSED = OrigWS.CLOSED;
    window.WebSocket = Wrapped;
  }

  // --- navigation hint (DISPLAY-UNTRUSTED) ---
  // The authoritative address bar is driven by server nav events (Part 07).
  // This is only a low-latency hint the SPA MAY use; the SPA must not treat it
  // as authoritative because page JS controls it and could spoof the URL.
  function navHint() {
    post({ type: "ocode:browse:nav", url: location.href, ts: Date.now() });
  }
  var ps = history.pushState, rs = history.replaceState;
  history.pushState = function () { var r = ps.apply(this, arguments); navHint(); return r; };
  history.replaceState = function () { var r = rs.apply(this, arguments); navHint(); return r; };
  window.addEventListener("popstate", navHint);
})();
```

- [ ] **Step 4: Write `capture.go`**

Create `internal/browse/capture.go`:

```go
package browse

import (
	_ "embed"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

//go:embed capture.js
var captureJS []byte

// injectCapture inserts the bootstrap + capture <script> as the first children
// of <head> so wrapping happens before any page script runs. Falls back to
// before </body>, </html>, or append so capture is never silently skipped.
func injectCapture(html []byte, stateKey, spaOrigin string) []byte {
	sk, _ := json.Marshal(stateKey)
	so, _ := json.Marshal(spaOrigin)
	var b strings.Builder
	b.WriteString("<script>window.__OCODE_BROWSE__={stateKey:")
	b.Write(sk)
	b.WriteString(",spaOrigin:")
	b.Write(so)
	b.WriteString("};</script>")
	b.WriteString(`<script src="/__ocode_capture.js"></script>`)
	snippet := []byte(b.String())

	lower := bytes.ToLower(html)
	if i := bytes.Index(lower, []byte("<head>")); i != -1 {
		at := i + len("<head>")
		return concat(html[:at], snippet, html[at:])
	}
	// Head with attributes: <head ...>
	if i := bytes.Index(lower, []byte("<head")); i != -1 {
		if end := bytes.IndexByte(html[i:], '>'); end != -1 {
			at := i + end + 1
			return concat(html[:at], snippet, html[at:])
		}
	}
	if i := bytes.Index(lower, []byte("</head>")); i != -1 {
		return concat(html[:i], snippet, html[i:])
	}
	if i := bytes.Index(lower, []byte("<body")); i != -1 {
		return concat(html[:i], snippet, html[i:])
	}
	return concat(html, snippet, nil)
}

func concat(a, b, c []byte) []byte {
	out := make([]byte, 0, len(a)+len(b)+len(c))
	out = append(out, a...)
	out = append(out, b...)
	out = append(out, c...)
	return out
}

// serveCapture returns the embedded capture bundle from the browse origin.
func (s *Server) serveCapture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err := w.Write(captureJS); err != nil {
		s.log.Printf("browse: serve capture bundle: %v", err)
	}
}
```

- [ ] **Step 5: Register the bundle route**

In `internal/browse/server.go`, in `New(...)` after the `/b/` handler registration, add:

```go
	s.mux.HandleFunc("GET /__ocode_capture.js", s.serveCapture)
```

- [ ] **Step 6: Run to verify pass**

Run: `go test ./internal/browse/ -run Capture`
Expected: PASS (injection, fallback, and bundle-serving tests).

> **JS behavior coverage:** `capture.js` has no Go-side unit harness. Its runtime behavior (console forwarding, fetch/XHR reroute, nav hints) is exercised by the frontend vitest suite (Part 09 — the SPA `message` handler) and the manual QA matrix (Part 10 — Vite HMR + external sites). This is intentional; note it so a reviewer doesn't expect JS unit tests in the Go package.

- [ ] **Step 7: Build + commit**

```bash
go build ./... && go test ./internal/browse/
git add internal/browse/capture.go internal/browse/capture.js internal/browse/capture_test.go internal/browse/server.go
git commit -m "feat(browse): inject capture script for console/network telemetry and JS URL reroute"
```
