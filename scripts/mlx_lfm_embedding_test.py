#!/usr/bin/env python3
"""Smoke-test the MLX embedding backend with LFM2.5-Embedding-350M.

Purpose
-------
Before we wire LFM2.5 as the default discovery embedder on Apple Silicon, we
must prove the risky assumption: that an MLX embedding library can actually
LOAD the LFM2.5 embedding model and produce meaningful vectors. mlx-embeddings'
published architecture list does not yet name `lfm2`/`LFM2`, so this script
discovers the working load + embed API at runtime and validates the output.

It also has an optional `--serve` mode that starts a minimal OpenAI-compatible
HTTP server (POST /v1/embeddings, GET /v1/models) on a port, proving the exact
contract the Go `discovery` package expects from a local MLX embedder.

Exit codes
----------
  0  PASS  - model loaded, embeddings sane (dim 1024, similar>dissimilar)
  1  FAIL  - a functional check failed
  2  SKIP  - environment not capable (missing deps / wrong platform) but the
             script itself ran correctly. The caller should install deps and
             re-run rather than treating this as a code failure.

Usage
-----
  python3 scripts/mlx_lfm_embedding_test.py
  python3 scripts/mlx_lfm_embedding_test.py --model mlx-community/LFM2.5-Embedding-350M-4bit
  python3 scripts/mlx_lfm_embedding_test.py --serve --port 11457
"""
from __future__ import annotations

import argparse
import json
import sys
import traceback


def _die(code: int, msg: str) -> "NoReturn":  # type: ignore[name-defined]
    print(msg, file=sys.stderr)
    sys.exit(code)


def _platform_ok() -> bool:
    import platform

    sysname = platform.system()
    machine = platform.machine()
    # mlx is Apple-Silicon only. Allow an override for CI that just wants to
    # confirm the script logic, but the real MLX path needs darwin/arm64.
    return sysname == "Darwin" and machine == "arm64"


def _cosine(a, b) -> float:
    """Cosine similarity between two 1-D mlx arrays (or lists)."""
    import mlx.core as mx

    a = mx.array(a, dtype=mx.float32)
    b = mx.array(b, dtype=mx.float32)
    na = mx.linalg.norm(a)
    nb = mx.linalg.norm(b)
    if float(na) == 0.0 or float(nb) == 0.0:
        return 0.0
    return float((a @ b) / (na * nb))


def _load_model(model_id: str):
    """Load via mlx_embeddings; fall back to mlx_lm if needed.

    Returns (model, processor, loaded_via). Raises on failure.
    """
    try:
        from mlx_embeddings import load as me_load

        model, processor = me_load(model_id)
        return model, processor, "mlx_embeddings"
    except Exception as e1:  # try mlx_lm path
        try:
            from mlx_lm import load as ml_load

            model, processor = ml_load(model_id)
            return model, processor, "mlx_lm"
        except Exception as e2:
            raise RuntimeError(
                f"mlx_embeddings.load failed: {e1!r}; mlx_lm.load failed: {e2!r}"
            )


def _embed(model, processor, sentences):
    """Discover the working embed API. Returns a (n, dim) list of vectors.

    Tries, in order:
      1. model.encode(sentences)                 (mlx_embeddings text embedders)
      2. model.process(sentences, processor=...)  (mlx_embeddings multimodal API)
      3. mlx_lm path: processor.encode(s) -> model.model(input_ids) -> pool
         (mlx-embeddings 0.1.0 cannot load the 4-bit LFM2.5 MLX model due to a
          dense.0.weight param-name mismatch, so we fall back to mlx_lm, which
          loads the Lfm2Model and exposes hidden states we mean-pool.)
      4. processor(sentences) -> model(...)        (explicit pipeline)
    Returns (vectors, api_name).
    """
    # 1. model.encode
    try:
        out = model.encode(sentences)
        return _to_2d(out), "model.encode"
    except Exception:
        pass
    # 2. model.process
    try:
        out = model.process(sentences, processor=processor)
        return _to_2d(out), "model.process"
    except Exception:
        pass
    # 3. mlx_lm path (per-sentence to avoid ragged batching)
    try:
        vecs = _embed_mlx_lm(model, processor, sentences)
        return vecs, "mlx_lm:model.model+meanpool"
    except Exception:
        pass
    # 4. explicit processor -> model
    try:
        tok = processor(sentences)
        out = model(**tok)
        emb = getattr(out, "embeddings", getattr(out, "last_hidden_state", out))
        return _to_2d(emb), "processor->model"
    except Exception as e:
        raise RuntimeError(f"no embedding API worked: {e!r}")


def _embed_mlx_lm(model, processor, sentences):
    """Embed via mlx_lm: tokenize, run the inner nn.Module, mean-pool seq dim."""
    import mlx.core as mx

    # The mlx_lm-loaded model exposes the nn.Module as `.model`.
    inner = getattr(model, "model", model)
    vecs = []
    for s in sentences:
        ids = processor.encode(s)  # list[int] for one sentence
        if not isinstance(ids, list):
            ids = list(ids)
        arr = mx.array([ids])
        out = inner(arr)
        h = getattr(out, "last_hidden_state", out)
        if not isinstance(h, mx.array):
            h = mx.array(h)
        # mean-pool over the sequence dimension (axis=1)
        v = mx.mean(h, axis=1)[0]
        mx.eval(v)
        vecs.append(v.tolist())
    return vecs


def _to_2d(out) -> list:
    """Normalize a model output into a list of 1-D vectors."""
    import mlx.core as mx

    # mlx models usually return an mlx array; pull to numpy/list.
    if hasattr(out, "tolist"):
        arr = out
    else:
        arr = mx.array(out)
    # Shape may be (n, dim) or (n, seq, dim) -> pool over seq (mean of non-pad).
    np = arr.tolist() if hasattr(arr, "tolist") else list(arr)
    if not isinstance(np, list) or len(np) == 0:
        raise RuntimeError(f"unexpected embed output type: {type(out)}")
    # If 3-D, mean-pool the sequence dimension.
    if isinstance(np[0], list) and np[0] and isinstance(np[0][0], list):
        np = [[sum(tok) / len(tok) for tok in vec] for vec in np]
    if not isinstance(np[0], list):
        # scalar per item -> wrap
        np = [[v] for v in np]
    return np


def _validate_vectors(vecs, dim_expected: int) -> None:
    if len(vecs) < 2:
        raise RuntimeError(f"expected >=2 vectors, got {len(vecs)}")
    dims = {len(v) for v in vecs}
    if len(dims) != 1:
        raise RuntimeError(f"inconsistent vector dims: {dims}")
    dim = dims.pop()
    if dim != dim_expected:
        raise RuntimeError(f"dim {dim} != expected {dim_expected}")


def run_test(model_id: str) -> dict:
    if not _platform_ok():
        _die(2, "SKIP: MLX backend requires macOS Apple Silicon (darwin/arm64).")

    try:
        import mlx.core  # noqa: F401
    except Exception as e:
        _die(2, f"SKIP: mlx not installed ({e}). Install: pip install mlx mlx-embeddings")

    try:
        from mlx_embeddings import load  # noqa: F401
    except Exception as e:
        _die(2, f"SKIP: mlx_embeddings not installed ({e}). Install: pip install mlx-embeddings")

    print(f"[test] loading {model_id} ...", flush=True)
    model, processor, via = _load_model(model_id)
    print(f"[test] loaded via {via}", flush=True)

    sentences = [
        "the cat sat on the mat",          # A
        "a cat was resting on the rug",    # B (similar to A)
        "quantum computers use superposition",  # C (unrelated)
    ]
    vecs, api = _embed(model, processor, sentences)
    print(f"[test] embed api: {api}, n={len(vecs)}, dim={len(vecs[0])}", flush=True)

    _validate_vectors(vecs, 1024)

    sim_ab = _cosine(vecs[0], vecs[1])
    sim_ac = _cosine(vecs[0], vecs[2])
    print(f"[test] cos(A,B)={sim_ab:.4f}  cos(A,C)={sim_ac:.4f}", flush=True)

    if not (sim_ab > sim_ac):
        raise RuntimeError(
            f"semantic sanity failed: similar pair ({sim_ab:.4f}) not greater "
            f"than unrelated pair ({sim_ac:.4f})"
        )

    result = {
        "status": "PASS",
        "model": model_id,
        "loaded_via": via,
        "embed_api": api,
        "dim": len(vecs[0]),
        "cos_similar": round(sim_ab, 4),
        "cos_unrelated": round(sim_ac, 4),
    }
    print(json.dumps(result))
    return result


# --------------------------------------------------------------------------
# Optional OpenAI-compatible server (proves the contract the Go backend uses)
# --------------------------------------------------------------------------
def run_server(model_id: str, port: int) -> None:
    if not _platform_ok():
        _die(2, "SKIP: MLX backend requires macOS Apple Silicon (darwin/arm64).")
    model, processor, _ = _load_model(model_id)
    # warm once
    _embed(model, processor, ["warmup"])

    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

    def embed_one(text: str):
        vecs, _ = _embed(model, processor, [text])
        return vecs[0]

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

        def do_GET(self):  # /v1/models
            if self.path.rstrip("/").endswith("/v1/models"):
                self._send(200, {"data": [{"id": model_id}]})
            else:
                self._send(404, {"error": "not found"})

        def do_POST(self):  # /v1/embeddings
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
            out = []
            for i, t in enumerate(inp):
                v = embed_one(t)
                out.append({"object": "embedding", "index": i, "embedding": v})
            self._send(200, {"object": "list", "data": out, "model": model_id})

    srv = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"[serve] {model_id} on http://127.0.0.1:{port} (Ctrl-C to stop)", flush=True)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--model", default="mlx-community/LFM2.5-Embedding-350M-4bit")
    ap.add_argument("--serve", action="store_true", help="start OpenAI-compatible server")
    ap.add_argument("--port", type=int, default=11457)
    args = ap.parse_args()

    try:
        if args.serve:
            run_server(args.model, args.port)
        else:
            run_test(args.model)
    except SystemExit:
        raise
    except Exception as e:
        print(json.dumps({"status": "FAIL", "error": str(e), "trace": traceback.format_exc()}))
        sys.exit(1)


if __name__ == "__main__":
    main()
