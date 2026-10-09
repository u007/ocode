#!/usr/bin/env python3
"""Probe server: serves pages/, injects a state beacon, records submissions.

usage: PROBE_OUT=<outdir> server.py <port> [pagesdir]  (outdir via env so it is not visible in `ps`)
  GET  /<page>.html   served with /_beacon.js injected (reports live field values to /_state)
  ANY  /submit        recorded to <outdir>/submissions.jsonl, answered with a thank-you page
  POST /_state        last live field state per page, kept in <outdir>/state.json
Every submission is a line {"method","path","query","form","files","t"}; files carry name, size, sha256.
"""
import hashlib
import http.server
import json
import os
import pathlib
import sys
import threading
import time
import urllib.parse
from email import message_from_bytes
from email.policy import default

PORT = int(sys.argv[1])
OUT = pathlib.Path(os.environ["PROBE_OUT"])
PAGES = pathlib.Path(sys.argv[2]) if len(sys.argv) > 2 else pathlib.Path(__file__).resolve().parent / "pages"
OUT.mkdir(parents=True, exist_ok=True)
STATE = {}
STATE_LOCK = threading.Lock()

BEACON = """(function(){
function snap(){var o={};document.querySelectorAll('input,select,textarea').forEach(function(e,i){
 var k=e.name||e.id||('el'+i); var t=e.type;
 if(t==='checkbox'||t==='radio'){ if(!o[k]) o[k]=[]; if(e.checked) o[k].push(e.value||e.id||'on'); }
 else if(t==='file'){ o[k]=Array.prototype.map.call(e.files||[],function(f){return f.name}); }
 else { o[k]=e.value; }});
 return o;}
function send(){try{fetch('/_state',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({page:location.pathname,data:snap()}),keepalive:true});}catch(e){}}
['input','change','click','keyup'].forEach(function(ev){document.addEventListener(ev,function(){setTimeout(send,0)},true)});
window.addEventListener('load',send);})();"""


class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _send(self, code, body, ctype="text/html; charset=utf-8"):
        b = body.encode() if isinstance(body, str) else body
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        if u.path == "/_beacon.js":
            return self._send(200, BEACON, "application/javascript")
        if u.path == "/submit":
            return self._record(u, b"", "")
        name = u.path.lstrip("/") or "index.html"
        f = (PAGES / name).resolve()
        if PAGES not in f.parents or not f.is_file():
            return self._send(404, "not found")
        data = f.read_text()
        if name.endswith(".html"):
            data = data.replace("</body>", '<script src="/_beacon.js"></script></body>') if "</body>" in data else data + '<script src="/_beacon.js"></script>'
        self._send(200, data)

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n)
        u = urllib.parse.urlparse(self.path)
        if u.path == "/_state":
            try:
                d = json.loads(body)
                with STATE_LOCK:  # beacons arrive on concurrent threads: serialise, and replace atomically so check.py never reads a torn file
                    STATE[d["page"]] = d["data"]
                    tmp = OUT / "state.json.tmp"
                    tmp.write_text(json.dumps(STATE))
                    os.replace(tmp, OUT / "state.json")
            except Exception:  # intentionally not logged: a malformed beacon must not break the probe
                pass
            return self._send(204, "")
        if u.path == "/submit":
            return self._record(u, body, self.headers.get("Content-Type", ""))
        self._send(404, "not found")

    def _record(self, u, body, ctype):
        form, files = {}, []
        for k, v in urllib.parse.parse_qs(u.query, keep_blank_values=True).items():
            form[k] = v
        if "multipart/form-data" in ctype:
            msg = message_from_bytes(b"Content-Type: " + ctype.encode() + b"\r\n\r\n" + body, policy=default)
            for part in msg.iter_parts():
                nm = part.get_param("name", header="content-disposition")
                fn = part.get_filename()
                payload = part.get_payload(decode=True) or b""
                if fn is not None:
                    files.append({"field": nm, "name": fn, "size": len(payload), "sha256": hashlib.sha256(payload).hexdigest()})
                else:
                    form.setdefault(nm, []).append(payload.decode())
        elif body:
            for k, v in urllib.parse.parse_qs(body.decode(), keep_blank_values=True).items():
                form.setdefault(k, []).extend(v)
        rec = {"method": self.command, "path": u.path, "query": u.query, "form": form, "files": files, "t": time.time()}
        with open(OUT / "submissions.jsonl", "a") as fh:
            fh.write(json.dumps(rec) + "\n")
        self._send(200, "<html><body><h1>Thank you</h1><p id='ok'>Submission received.</p></body></html>")


if __name__ == "__main__":
    http.server.ThreadingHTTPServer(("127.0.0.1", PORT), H).serve_forever()
