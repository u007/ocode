#!/usr/bin/env python3
"""Minimal OpenAI-compatible embeddings server for Apple-Silicon MLX models.

This is the local backend ocode spawns when the user picks an MLX embedding
model (e.g. LFM2.5-Embedding-350M on macOS). It loads the model with `mlx_lm`
(mean-pooling the last hidden state — verified to produce semantically correct
1024-d vectors for LFM2.5) and serves:

  GET  /v1/models        -> {"data":[{"id":<model-id>}]}
  POST /v1/embeddings     -> {"object":"list","data":[{embedding:[...]}],"model":<id>}

The model is fetched from HuggingFace on first load (cached by mlx_lm), so no
static artifact/SHA pin is required here.

Usage:
  python3 mlx_embed_server.py --repo mlx-community/LFM2.5-Embedding-350M-4bit \
                              --model-id local/lfm2.5-embedding --port 11457
"""
from __future__ import annotations

import argparse
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def _load(repo: str):
    """Load via mlx_lm (works for LFM2.5's lfm2-bidir arch). Fall back to
    mlx_embeddings only if present and able to load the repo."""
    try:
        from mlx_lm import load as ml_load

        return ml_load(repo)
    except Exception as e1:
        try:
            from mlx_embeddings import load as me_load

            return me_load(repo)
        except Exception as e2:
            raise RuntimeError(f"could not load {repo}: mlx_lm failed {e1!r}; mlx_embeddings failed {e2!r}")


def _embed(model, processor, sentences):
    """Return a (n, dim) list of vectors using the mlx_lm path that the
    smoke-test verified works: tokenize per sentence, run the inner nn.Module,
    mean-pool the sequence dimension."""
    import mlx.core as mx

    inner = getattr(model, "model", model)
    vecs = []
    for s in sentences:
        ids = processor.encode(s)
        if not isinstance(ids, list):
            ids = list(ids)
        arr = mx.array([ids])
        out = inner(arr)
        h = getattr(out, "last_hidden_state", out)
        if not isinstance(h, mx.array):
            h = mx.array(h)
        v = mx.mean(h, axis=1)[0]
        mx.eval(v)
        vecs.append(v.tolist())
    return vecs


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--repo", required=True, help="HuggingFace repo id to load")
    ap.add_argument("--model-id", required=True, help="discovery model id reported in /v1/models")
    ap.add_argument("--port", type=int, default=11457)
    args = ap.parse_args()

    print(f"[mlx-embed] loading {args.repo} ...", flush=True)
    model, processor = _load(args.repo)
    print(f"[mlx-embed] loaded; serving id={args.model_id} on :{args.port}", flush=True)

    def embed_one(text: str):
        return _embed(model, processor, [text])[0]

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *a):  # quiet
            pass

        def _send(self, code, obj):
            body = json.dumps(obj).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path.rstrip("/").endswith("/v1/models"):
                self._send(200, {"data": [{"id": args.model_id}]})
            else:
                self._send(404, {"error": "not found"})

        def do_POST(self):
            if not self.path.rstrip("/").endswith("/v1/embeddings"):
                self._send(404, {"error": "not found"})
                return
            n = int(self.headers.get("Content-Length", 0))
            raw = self.rfile.read(n) if n else b"{}"
            try:
                req = json.loads(raw or b"{}")
            except Exception:
                self._send(400, {"error": "bad json"})
                return
            inp = req.get("input", [])
            if isinstance(inp, str):
                inp = [inp]
            data = []
            for i, t in enumerate(inp):
                data.append({"object": "embedding", "index": i, "embedding": embed_one(t)})
            self._send(200, {"object": "list", "data": data, "model": args.model_id})

    srv = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
