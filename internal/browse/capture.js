// ocode browse capture script — dependency-free, injected into every local
// HTML document (local mode only). Runs first in <head> so it wraps console/fetch before page
// code loads. All telemetry is posted to the SPA origin ONLY (never "*").
// Chrome (external) mode does not use this script; it receives console/network
// via CDP.
//
// Coverage honesty: this reroutes URLs that flow through fetch/XHR/WebSocket,
// the constructors we can wrap, and runtime-injected <script>/<link> elements
// (webpack/Next.js lazy chunks — hooked via the src/href property setters).
// It CANNOT reach:
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

  // Root mode: the document is served from its own per-tab origin, mounted at
  // the upstream root, so location.pathname IS the upstream path and there is
  // no /b/ route. The server sets rootMode and the bound scheme/host only for
  // that origin (tab_origin.go); prefix mode leaves them unset.
  var rootMode = cfg.rootMode === true;
  var upScheme = String(cfg.scheme || "");
  var upHost = String(cfg.host || "");
  // sameUpstream reports whether a URL's scheme and host are the bound upstream.
  function sameUpstream(scheme, host) {
    return scheme === upScheme && host.toLowerCase() === upHost.toLowerCase();
  }

  function post(msg) {
    msg.stateKey = stateKey;
    try {
      window.parent.postMessage(msg, spaOrigin);
    } catch (e) {
      // Parent gone or origin mismatch: nothing we can do, don't loop.
      // intentionally not logged: parent unreachability is benign teardown noise
    }
  }

  // The origin may be mounted under a path prefix (a remote project's browse
  // origin is reached through the proxy at "/api/remote/<host>/browse"). The
  // "/b/..." route tree then starts at that prefix, not at the origin root, so
  // every anchor below is derived RELATIVE to the prefix.
  //
  // The stateKey is known (injected in __OCODE_BROWSE__), so the "/b/" marker
  // is located by its exact position rather than by a greedy regex: the
  // document path is {prefix}/b/{stateKey}/...
  // The server writes the stateKey into the path unencoded (rewrite.go), so the
  // pathname carries it raw (e.g. "side:chat:abc"). Encoding it here would make
  // the already-routed check and the marker lookup miss, double-wrapping URLs.
  function mountBase() {
    if (rootMode) return "";
    var marker = "/b/" + stateKey;
    var i = location.pathname.indexOf(marker);
    if (i === -1) {
      // Fall back to the last "/b/<something>" segment.
      var m = location.pathname.match(/^(.*?)\/b\/[^/]+/);
      return m ? m[1] : "";
    }
    return location.pathname.slice(0, i);
  }

  // --- /b/ base of the current document, e.g. "/b/tab:abc" ---
  function browseBase() {
    // Root mode has no /b/ route: the origin root is the route.
    if (rootMode) return "";
    // location.pathname === {prefix}/b/{stateKey}/{scheme}/{host}/{path...}
    return mountBase() + "/b/" + stateKey;
  }

  // Full route prefix of the current document, e.g. "/b/tab:abc/http/localhost:3000".
  // Root-relative ("/x") URLs must be re-prefixed with this, because the
  // browser would otherwise resolve them against the browse origin ROOT and
  // drop out of the stateless route.
  function routeBase() {
    if (rootMode) return "";
    // {prefix}/b/{stateKey}/{scheme}/{host}
    var rest = location.pathname.slice(mountBase().length);
    var m = rest.match(/^(\/b\/[^/]+\/[^/]+\/[^/]+)/);
    return m ? mountBase() + m[1] : browseBase();
  }

  // Root mode: a root-relative URL already resolves on the bound upstream. A
  // same-upstream absolute URL needs only its path. Any other upstream goes
  // through the reserved /__ocode/b/{scheme}/{host} route on this origin, where
  // the proxy applies the SSRF guard, the local-mode gate and the cookie jar. A
  // URL already on this origin is the proxy itself and is returned as-is.
  function rerouteRoot(raw, rootRel) {
    if (rootRel) return raw;
    var u;
    try { u = new URL(raw); } catch (e) { return raw; }
    if (u.origin === location.origin) return raw;
    var scheme = u.protocol.replace(":", "");
    if (sameUpstream(scheme, u.host)) return location.origin + u.pathname + u.search + u.hash;
    return location.origin + "/__ocode/b/" + scheme + "/" + u.host + u.pathname + u.search + u.hash;
  }

  // Map an absolute or root-relative URL to /b/{stateKey}/{scheme}/{host}/{path...}.
  // Returns the input unchanged for bare-relative URLs (they already resolve
  // under the current /b/ route). Protocol-relative "//host/..." is completed
  // with the page protocol. (ws:/wss: are mapped onto the http/https route
  // scheme by the WebSocket wrapper below.)
  function reroute(raw) {
    if (typeof raw !== "string") {
      try { raw = String(raw); } catch (e) { return raw; }
    }
    if (/^\/\//.test(raw)) raw = (location.protocol === "https:" ? "https:" : "http:") + raw; // protocol-relative
    var rootRel = /^\/(?!\/)/.test(raw);
    if (!rootRel && !/^https?:\/\//i.test(raw)) return raw; // bare relative — leave as-is.
    if (rootMode) return rerouteRoot(raw, rootRel);
    var base = browseBase();
    if (raw.indexOf(location.origin + base + "/") === 0) return raw;
    if (rootRel) {
      return location.origin + routeBase() + raw;
    }
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
  // Local-mode network capture: a page's own JS can read headers and bodies
  // only for same-origin ("basic") and CORS-enabled ("cors") responses. An
  // opaque cross-origin response still EXPANDS with method/url/status/timing,
  // but honestly reports "No headers available" rather than inventing any.
  var seq = 0;
  function localId() { return "local-" + Date.now() + "-" + (++seq); }
  var MAX_BODY = 256 * 1024;
  // Request bodies are capped tighter than responses, matching the CDP path
  // (maxPostDataLen). A page can send an arbitrarily large body.
  var MAX_POST_DATA = 64 * 1024;
  // Same bounds the CDP path applies (target.go redactHeaders): local mode must
  // not display credentials the Chrome path hides, and a page can supply an
  // unbounded header map.
  var HEADER_CAP = 20;
  var HEADER_VALUE_MAX = 200;
  var SENSITIVE_HEADER_PREFIXES = ["authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-auth-token", "x-csrf-token"];
  function redactHeader(name, value) {
    var lk = String(name).toLowerCase();
    for (var i = 0; i < SENSITIVE_HEADER_PREFIXES.length; i++) {
      var p = SENSITIVE_HEADER_PREFIXES[i];
      if (lk === p || lk.indexOf(p + "-") === 0) return "[redacted]";
    }
    var v = String(value == null ? "" : value);
    return v.length > HEADER_VALUE_MAX ? v.slice(0, HEADER_VALUE_MAX) + "…" : v;
  }
  function putHeader(out, name, value, count) {
    if (count.n >= HEADER_CAP) return;
    out[String(name)] = redactHeader(name, value);
    count.n++;
  }
  function headersToObj(h) {
    if (!h) return undefined;
    var out = {};
    var count = { n: 0 };
    try {
      // fetch accepts a [name, value] pair list, whose forEach hands over the
      // pair as the value, so pairs are unpacked before the Headers-like path.
      if (Array.isArray(h)) { h.forEach(function (pair) { putHeader(out, pair[0], pair[1], count); }); }
      else if (typeof h.forEach === "function") { h.forEach(function (v, k) { putHeader(out, k, v, count); }); }
      else if (typeof h === "object") { for (var k in h) { if (Object.prototype.hasOwnProperty.call(h, k)) putHeader(out, k, h[k], count); } }
    } catch (e) {
      console.debug("ocode capture: could not read request headers", e);
      return undefined;
    }
    return Object.keys(out).length ? out : undefined;
  }
  function parseXHRHeaders(raw) {
    if (!raw) return undefined;
    var out = {};
    var count = { n: 0 };
    String(raw).split(/\r?\n/).forEach(function (line) {
      var i = line.indexOf(":");
      if (i > 0) putHeader(out, line.slice(0, i).trim(), line.slice(i + 1).trim(), count);
    });
    return Object.keys(out).length ? out : undefined;
  }
  function capBody(s) {
    s = String(s == null ? "" : s);
    return s.length > MAX_BODY ? { body: s.slice(0, MAX_BODY), truncated: true } : { body: s, truncated: false };
  }
  // Display text for a request body. JSON.stringify turns URLSearchParams,
  // FormData, Blob and ArrayBuffer into "{}", so each binary-ish body gets an
  // honest placeholder instead; anything else keeps the JSON.stringify path.
  function bodyText(b) {
    if (typeof b === "string") return b;
    if (typeof URLSearchParams !== "undefined" && b instanceof URLSearchParams) return b.toString();
    if (typeof FormData !== "undefined" && b instanceof FormData) {
      var parts = [];
      b.forEach(function (v, k) {
        parts.push(String(k) + "=" + (typeof File !== "undefined" && v instanceof File ? "[File " + v.name + " " + v.size + "]" : String(v)));
      });
      return parts.join("&");
    }
    if (typeof Blob !== "undefined" && b instanceof Blob) return "[Blob " + b.type + " " + b.size + "]";
    if (b instanceof ArrayBuffer) return "[binary " + b.byteLength + " bytes]";
    if (ArrayBuffer.isView(b)) return "[binary " + b.byteLength + " bytes]";
    if (typeof ReadableStream !== "undefined" && b instanceof ReadableStream) return "[stream]";
    return JSON.stringify(b);
  }
  // Request body text, capped. `what` names the request in the debug line. A
  // body the renderer cannot stringify (a circular object, a BigInt) leaves the
  // row without a request body, which the row already shows as absent.
  function requestBodyText(b, what) {
    if (b == null) return undefined;
    try { return bodyText(b).slice(0, MAX_POST_DATA); } catch (e) {
      console.debug("ocode capture: could not render the request body of " + what, e);
      return undefined;
    }
  }
  function postDataOf(init, input, what) {
    // `init.body` wins (it is what fetch sends); a Request's own body is the
    // fallback. An empty-string body is a real body — never treat it as absent.
    var b;
    if (init && init.body != null) b = init.body;
    else if (input && typeof input === "object" && input.body != null) b = input.body;
    return requestBodyText(b, what);
  }
  function reqHeadersOf(init, input) {
    return headersToObj((init && init.headers) || (input && input.headers));
  }
  // Only decode a body we may actually show as text. Cloning a multi-megabyte
  // image into a string costs the page real memory for a row nobody can read.
  function isTextualType(ct) {
    var t = String(ct || "").toLowerCase();
    if (!t) return false;
    return /^text\//.test(t) || /^application\/(json|xml|javascript|ecmascript|x-www-form-urlencoded|graphql)\b/.test(t) || /\+json\b/.test(t) || /\+xml\b/.test(t);
  }
  // Types that never end on their own. Their stream is never teed: a page's
  // branch of a tee buffers whatever the clone branch pulled, and a stream that
  // does not end has no point at which that buffer stops growing.
  var STREAMING_TYPE = /^(text\/event-stream|multipart\/x-mixed-replace|application\/(x-ndjson|ndjson|x-jsonlines|jsonl|stream\+json|grpc|grpc-web))\b/;
  // Size for the row's Size column. A proxied same-origin response exposes
  // content-length; an opaque or chunked one does not, so the cell stays "—"
  // rather than guessing from a decoded body length the row cannot know yet.
  function headerSize(getHeader) {
    var cl = getHeader("content-length");
    if (cl != null && /^\d+$/.test(String(cl).trim())) return Number(cl);
    return undefined;
  }
  function responseSize(res) {
    return headerSize(function (n) { return res.headers.get(n); });
  }
  // A textual, non-streaming response body is captured whether or not it has a
  // content-length. The length only chooses how the body is read (captureBody).
  function capturesBody(ct) {
    var t = String(ct || "").toLowerCase();
    if (STREAMING_TYPE.test(t)) return false;
    return !t || isTextualType(t);
  }
  // Reads a chunked body until it passes the cap. The reader stops as soon as
  // the decoded text exceeds MAX_BODY (one chunk past the cap at most) and
  // cancels the clone branch, so nothing beyond the cap is pulled. capBody then
  // marks the body truncated, which is exact: the text really exceeds the cap.
  function readCapped(body) {
    var reader = body.getReader();
    var dec = new TextDecoder();
    var text = "";
    function step() {
      return reader.read().then(function (r) {
        if (r.done) return capBody(text + dec.decode());
        text += dec.decode(r.value, { stream: true });
        if (text.length <= MAX_BODY) return step();
        reader.cancel().then(null, function () {
          // intentionally not logged: the body is already capped; a cancel that
          // rejects only means the stream had already errored
        });
        return capBody(text);
      });
    }
    return step();
  }
  // Captures a response body. Every length takes the readCapped path: a
  // clone().text() read of a finite body would buffer the whole body (the page's
  // own tee branch holds the same bytes) before the cap could apply. The row is
  // already posted; this resolves later and posts the body under the same
  // requestId. A bodiless response (204/304) has a null clone body and nothing
  // to capture.
  function captureBody(res, id, what) {
    var copy = res.clone();
    if (!copy.body) return;
    readCapped(copy.body).then(function (c) {
      post({ type: "ocode:browse:networkbody", requestId: id, responseBody: c.body, responseBodyTruncated: c.truncated });
    }, function (err) {
      console.debug("ocode capture: could not read the response body of " + what, err);
    });
  }

  // Dedup: fetch/XHR wrappers report the same request Resource Timing does, so a
  // Resource Timing entry is skipped when a wrapper request matches it. Each
  // wrapper call is one record, kept while the request is in flight and for
  // WRAP_GRACE_MS after it completes (the timing entry is queued at response
  // end, so a long fetch or long poll must still match). A match CONSUMES its
  // record, so a later request to the same URL is shown, and only fetch/XHR
  // timing entries can match, so an <img> or <script> for a URL a fetch is
  // loading is never swallowed.
  // A record holds ABSOLUTE URLs for BOTH the page URL and the rerouted /b/
  // URL: Resource Timing reports the URL actually fetched (the rerouted one),
  // while a bare-relative request is stored as typed, so a raw string
  // comparison never matches and every fetch would appear twice.
  var WRAP_GRACE_MS = 2000;
  var MAX_WRAPPED = 200;
  var wrapped = [];
  function absUrl(u) {
    try { return new URL(String(u), location.href).href; } catch (e) {
      // intentionally not logged: an unparseable URL makes fetch/XHR reject on
      // their own, and the raw string is still the best key for the match.
      return String(u);
    }
  }
  function beginWrapped(url, routed) {
    var urls = [absUrl(url)];
    var r = absUrl(routed);
    if (r !== urls[0]) urls.push(r);
    var rec = { urls: urls, doneAt: 0 };
    wrapped.push(rec);
    while (wrapped.length > MAX_WRAPPED) wrapped.shift();
    return rec;
  }
  function endWrapped(rec) {
    rec.doneAt = Date.now();
  }
  // Consumes the oldest record for this URL, if any. Records still in flight
  // (doneAt 0) never expire; a completed one expires after the grace period.
  function consumeWrapped(url) {
    var u = absUrl(url);
    var now = Date.now();
    wrapped = wrapped.filter(function (rec) {
      return rec.doneAt === 0 || now - rec.doneAt <= WRAP_GRACE_MS;
    });
    for (var i = 0; i < wrapped.length; i++) {
      if (wrapped[i].urls.indexOf(u) !== -1) {
        wrapped.splice(i, 1);
        return true;
      }
    }
    return false;
  }
  // Only entries the page's own fetch/XHR produced can match a wrapper record.
  // A sendBeacon is included: the beacon wrapper posts its own row, and its
  // Resource Timing entry would otherwise appear a second time as an anonymous
  // GET with no method or body.
  function isWrapperInitiated(entry) {
    return entry.initiatorType === "fetch" || entry.initiatorType === "xmlhttprequest" || entry.initiatorType === "beacon";
  }

  // Chromium delivers the one navigation entry to a buffered observer more than
  // once — first while the document is still loading (duration 0 even though the
  // response has already been received, so `responseEnd > 0` cannot tell them
  // apart) and again once the load event has fired. Posting every delivery
  // renders the page's own document as several rows, and posting the first one
  // shows a meaningless 0 ms, so the row is posted once, from the finished
  // entry. loadEventEnd is the discriminator.
  var navPosted = false;
  if (window.PerformanceObserver) {
    try {
      var po = new PerformanceObserver(function (list) {
        list.getEntries().forEach(function (e) {
          if (e.entryType !== "resource" && e.entryType !== "navigation") return;
          if (e.entryType === "navigation") {
            if (navPosted || !(e.loadEventEnd > 0)) return;
            navPosted = true;
          }
          var url = e.name || "";
          if (!url || url.indexOf("data:") === 0 || url.indexOf("blob:") === 0) return;
          if (isWrapperInitiated(e) && consumeWrapped(url)) return;
          // Resource Timing carries no method; "-" marks it unknown rather than
          // guessing from initiatorType (fetch/xhr/img/script are not methods).
          // responseStatus is 0 when the engine withholds it (cross-origin
          // without Timing-Allow-Origin); the SPA maps a non-number to 0 too.
          var ms = Math.round(e.duration);
          post({
            type: "ocode:browse:network",
            requestId: localId(),
            method: "-",
            url: url,
            status: e.responseStatus,
            duration: ms,
            durationMs: ms,
            ts: Math.round(performance.timeOrigin + e.startTime),
            size: e.transferSize,
          });
        });
      });
      po.observe({ type: "resource", buffered: true });
      po.observe({ type: "navigation", buffered: true });
    } catch (e) { /* intentionally not logged: PerformanceObserver is optional */ }
  }

  if (window.fetch) {
    var origFetch = window.fetch.bind(window);
    window.fetch = function (input, init) {
      var url = typeof input === "string" ? input : (input && input.url) || "";
      var method = (init && init.method) || (input && input.method) || "GET";
      var routed = reroute(url);
      var arg = routed === url ? input : routed;
      var start = Date.now();
      var id = localId();
      var what = method + " " + url;
      // Record both the page URL and the URL actually fetched (Resource Timing
      // reports the latter).
      var rec = beginWrapped(url, routed);
      return origFetch(arg, init).then(
        function (res) {
          endWrapped(rec);
          var base = {
            type: "ocode:browse:network", requestId: id, method: method, url: url,
            status: res.status, duration: Date.now() - start, durationMs: Date.now() - start, ts: start,
            requestHeaders: reqHeadersOf(init, input),
            responseHeaders: headersToObj(res.headers),
            postData: postDataOf(init, input, what),
            size: responseSize(res),
          };
          // A readable body exists for same-origin ("basic") and CORS-enabled
          // ("cors") responses only; an opaque response hides it. Clone so the
          // page still consumes the original stream. `contentType` always
          // travels, so the expanded row can name the type even when the body
          // is binary and not decoded.
          var ct = res.headers.get("content-type") || "";
          base.contentType = ct || undefined;
          // The row is posted now, before any body is read, so a response that
          // never settles (SSE, a long chunked stream) still shows up in the
          // panel. The body follows as its own message with the same requestId.
          post(base);
          if ((res.type === "basic" || res.type === "cors" || res.type === "default") && capturesBody(ct)) {
            captureBody(res, id, what);
          }
          return res;
        },
        function (err) {
          endWrapped(rec);
          post({ type: "ocode:browse:network", requestId: id, method: method, url: url, status: 0, duration: Date.now() - start, durationMs: Date.now() - start, ts: start, requestHeaders: reqHeadersOf(init, input), postData: postDataOf(init, input, what) });
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
    var setReqHeader = XHR.prototype.setRequestHeader;
    XHR.prototype.open = function (method, url) {
      var routed = reroute(url);
      this.__ocode = { method: method, url: url, routed: routed, start: 0, requestHeaders: {}, requestId: localId() };
      return open.apply(this, [method, routed].concat([].slice.call(arguments, 2)));
    };
    XHR.prototype.setRequestHeader = function (k, v) {
      if (this.__ocode) this.__ocode.requestHeaders[k] = v;
      return setReqHeader.apply(this, arguments);
    };
    XHR.prototype.send = function () {
      var self = this;
      if (self.__ocode) {
        self.__ocode.start = Date.now();
        var o0 = self.__ocode;
        var postData = requestBodyText(arguments[0], o0.method + " " + o0.url);
        // Recorded before send(): a synchronous XHR fires loadend inside send().
        var rec = beginWrapped(o0.url, o0.routed);
        self.addEventListener("loadend", function () {
          endWrapped(rec);
          var o = self.__ocode;
          var ct = self.getResponseHeader("content-type") || "";
          var msg = {
            type: "ocode:browse:network", requestId: o.requestId, method: o.method, url: o.url,
            status: self.status, duration: Date.now() - o.start, durationMs: Date.now() - o.start, ts: o.start,
            // Same redaction + bounds as the fetch path: the page's raw stored
            // copy is display data, never the wire value.
            requestHeaders: headersToObj(o.requestHeaders),
            responseHeaders: parseXHRHeaders(self.getAllResponseHeaders()),
            postData: postData,
            contentType: ct || undefined,
            size: headerSize(function (n) { return self.getResponseHeader(n); }),
          };
          // responseText is only readable for these responseTypes; any other
          // responseType leaves the row without a body, as the row shows.
          if ((self.responseType === "" || self.responseType === "text") && (!ct || isTextualType(ct))) {
            var c = capBody(self.responseText);
            msg.responseBody = c.body; msg.responseBodyTruncated = c.truncated;
          }
          post(msg);
        });
      }
      return send.apply(this, arguments);
    };
  }

  // --- navigator.sendBeacon ---
  // A beacon is fire-and-forget: the browser discards the response, so there is
  // no status, no response header and no response body to report. Wrapping it
  // still earns the row its real method and request body, which is the whole
  // reason analytics traffic used to appear as an unexpandable anonymous GET.
  // The beacon is NOT rerouted (it is not a fetch/XHR this script proxies), so
  // the record is matched against the URL as typed and freed immediately: the
  // call returns before any Resource Timing entry for it is queued.
  if (navigator.sendBeacon) {
    var origBeacon = navigator.sendBeacon.bind(navigator);
    navigator.sendBeacon = function (url, data) {
      var id = localId();
      var start = Date.now();
      var rec = beginWrapped(url, url);
      endWrapped(rec);
      post({
        type: "ocode:browse:network", requestId: id, method: "POST", url: String(url),
        status: 0, duration: 0, durationMs: 0, ts: start,
        postData: requestBodyText(data, "POST " + String(url)),
      });
      return origBeacon(url, data);
    };
  }

  // --- WebSocket (reroute ws/wss to the browse origin) ---
  // The route carries the upstream's HTTP-equivalent scheme (ws→http, wss→https)
  // because parseTarget only accepts http/https; the browser still dials the
  // browse origin over ws/wss per page protocol. handleLocal passes the 101
  // upgrade through untouched (HMR works). External wss endpoints route to
  // external mode, which streams HTTP — such sockets fail visibly in the
  // console (v1 limitation).
  var OrigWS = window.WebSocket;
  if (OrigWS) {
    var Wrapped = function (url, protocols) {
      var routed = url;
      if (/^wss?:\/\//i.test(url)) {
        try {
          var u = new URL(url);
          var scheme = u.protocol === "wss:" ? "https" : "http";
          var wsProto = location.protocol === "https:" ? "wss://" : "ws://";
          if (rootMode) {
            // Root mode: a same-upstream socket is on this origin's root; any
            // other upstream goes through the reserved route (rerouteRoot). A
            // socket already on this origin stays as typed.
            if (u.host !== location.host) {
              var routeRoot = sameUpstream(scheme, u.host) ? "" : "/__ocode/b/" + scheme + "/" + u.host;
              routed = wsProto + location.host + routeRoot + u.pathname + u.search;
            }
          } else {
            routed = wsProto + location.host + browseBase() + "/" + scheme + "/" + u.host + u.pathname + u.search;
          }
        } catch (e) { routed = url; }
      }
      return protocols === undefined ? new OrigWS(routed) : new OrigWS(routed, protocols);
    };
    Wrapped.prototype = OrigWS.prototype;
    Wrapped.CONNECTING = OrigWS.CONNECTING; Wrapped.OPEN = OrigWS.OPEN;
    Wrapped.CLOSING = OrigWS.CLOSING; Wrapped.CLOSED = OrigWS.CLOSED;
    window.WebSocket = Wrapped;
  }

  // --- runtime-injected <script>/<link> reroute (webpack/Next.js lazy chunks) ---
  // Lazy chunks never appear in the initial HTML, so the server-side rewrite
  // (Part 04) cannot see them: webpack assigns script.src / link.href at
  // runtime with root-relative or absolute URLs that would resolve against the
  // browse origin ROOT and 404 (ChunkLoadError, blank SPA). Hook the DOM
  // properties so every assignment flows through reroute(). Parsed markup does
  // not invoke the JS setters, so server-rewritten tags are untouched; native
  // dynamic import() remains uncatchable here (documented at the top).
  function hookURLProp(proto, prop) {
    var desc = Object.getOwnPropertyDescriptor(proto, prop);
    if (!desc || !desc.set) return;
    Object.defineProperty(proto, prop, {
      configurable: true,
      enumerable: desc.enumerable,
      get: desc.get,
      set: function (v) {
        return desc.set.call(this, reroute(v));
      }
    });
  }
  try {
    hookURLProp(HTMLScriptElement.prototype, "src");
    hookURLProp(HTMLLinkElement.prototype, "href");
  } catch (e) {
    // Old engine without element prototypes: chunks are best-effort anyway.
    // intentionally not logged: benign fallback
  }
  // setAttribute bypasses the property setters; cover the script/link exotic
  // path too so no injected chunk URL escapes the /b/ route.
  if (Element && Element.prototype && Element.prototype.setAttribute) {
    var origSetAttr = Element.prototype.setAttribute;
    Element.prototype.setAttribute = function (name, value) {
      var low = String(name).toLowerCase();
      if ((low === "src" || low === "href") && (this instanceof HTMLScriptElement || this instanceof HTMLLinkElement)) {
        value = reroute(value);
      }
      return origSetAttr.call(this, name, value);
    };
  }

  // --- navigation hint (DISPLAY-UNTRUSTED) ---
  // The authoritative address bar is driven by server nav events (Part 07).
  // This is only a low-latency hint the SPA MAY use; the SPA must not treat it
  // as authoritative because page JS controls it and could spoof the URL.
  // kind "push" = history.pushState (adds a Back entry); "traverse" = popstate
  // or hashchange (the page moved to an entry it already has); "replace" =
  // replaceState. A URL equal to the last one posted is not re-sent: routers
  // call pushState/replaceState for the same route again and again.
  var lastNavURL = "";
  function navHint(kind) {
    var u = originalURL();
    if (!u || u === lastNavURL) return;
    lastNavURL = u;
    post({ type: "ocode:browse:nav", url: u, kind: kind, ts: Date.now() });
  }
  var ps = history.pushState, rs = history.replaceState;
  history.pushState = function () { var r = ps.apply(this, arguments); navHint("push"); return r; };
  history.replaceState = function () { var r = rs.apply(this, arguments); navHint("replace"); return r; };
  window.addEventListener("popstate", function () { navHint("traverse"); });
  window.addEventListener("hashchange", function () { navHint("traverse"); });

  // --- page title (DISPLAY ONLY) ---
  // The tab strip shows the site's own <title>, including JS-driven changes.
  // Titles are display data only: the SPA never uses them for navigation or
  // the address bar. originalURL() reconstructs the site URL from the /b/
  // route so the SPA can drop titles that don't match the current surface.
  function originalURL() {
    // Root mode: the document path already is the upstream path.
    if (rootMode) return upScheme + "://" + upHost + location.pathname + location.search + location.hash;
    // location.pathname === {prefix}/b/{stateKey}/{scheme}/{host}/{path...}
    // The {prefix}/b/{stateKey} part is stripped so the scheme/host/path can
    // be read back out; under a prefixed mount the leading "/b/..." anchor
    // would not match and the title would be lost entirely.
    var rest = location.pathname.slice(mountBase().length);
    var m = rest.match(/^\/b\/[^/]+\/([^/]+)\/([^/]+)(\/.*)?$/);
    if (!m) return "";
    var scheme = m[1], host = m[2], path = m[3] || "/";
    if (scheme !== "http" && scheme !== "https") return "";
    return scheme + "://" + host + path + location.search + location.hash;
  }
  var lastTitle = null;
  function reportTitle() {
    var t = "";
    try { t = document.title || ""; } catch (e) { t = ""; }
    if (t === lastTitle) return;
    lastTitle = t;
    post({ type: "ocode:browse:title", title: t, url: originalURL(), ts: Date.now() });
  }
  function watchTitle() {
    reportTitle();
    try {
      // characterData on the <title> node covers text edits; childList +
      // subtree on <head> covers title-node replacement (SPA re-renders).
      // Setting document.title mutates the <title> node's text, so no
      // setter wrapping is needed.
      var root = document.head || document.documentElement;
      if (root && window.MutationObserver) {
        var obs = new MutationObserver(function () { reportTitle(); });
        obs.observe(root, { childList: true, characterData: true, subtree: true });
      }
    } catch (e) { /* old engine: the initial report stands */ }
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", watchTitle);
  } else {
    watchTitle();
  }

  // --- scroll position (reported only; the SPA owns persistence) ---
  // The page REPORTS scrollY; the SPA persists it. A proxied page must never
  // write application storage directly, so restore arrives via window message
  // from the SPA origin and is applied here.
  var lastScrollY = -1;
  var scrollTimer = null;
  function reportScroll() {
    var y = 0;
    try { y = window.scrollY || 0; } catch (e) { y = 0; }
    if (y === lastScrollY) return;
    lastScrollY = y;
    post({ type: "ocode:browse:scroll", y: y, url: originalURL(), ts: Date.now() });
  }
  window.addEventListener("scroll", function () {
    if (scrollTimer) return;
    scrollTimer = setTimeout(function () { scrollTimer = null; reportScroll(); }, 300);
  }, { passive: true });
  window.addEventListener("message", function (e) {
    if (e.origin !== spaOrigin) return;
    var d = e.data;
    if (!d || typeof d !== "object") return;
    if (d.stateKey !== stateKey || d.type !== "ocode:browse:restore-scroll") return;
    var y = Number(d.y);
    if (!(y > 0)) return;
    try {
      if ("scrollRestoration" in history) history.scrollRestoration = "manual";
      window.scrollTo(0, y);
      lastScrollY = y;
    } catch (err) { /* benign teardown/restriction noise */ }
  });
  // First report after load settles (images/fonts shift layout).
  window.addEventListener("load", function () { setTimeout(reportScroll, 500); });
})();
