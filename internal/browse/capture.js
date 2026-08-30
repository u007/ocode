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
      // intentionally not logged: parent unreachability is benign teardown noise
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
