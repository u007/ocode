# Local speech-to-text worker for ocode.
#
# Run as: python3 -I stt_worker.py <onnx-asr-model> <hf-repo> <model-dir>
#
# Speaks a line-delimited JSON protocol on stdin/stdout:
#
#   -> {"op":"recognize","id":1,"path":"/tmp/clip.wav"}
#   <- {"id":1,"ok":true,"text":"...","pid":1234,"load_count":1}
#   <- {"id":1,"ok":false,"error":"..."}
#
# The model is loaded ONCE and reused for every request, so a burst of
# utterances (or the 3-second live-partial ticks) does not pay the model load
# each time.
#
# stdout is the protocol channel, so it is redirected to a private duplicate
# of the real stdout at startup and fd 1 is pointed at stderr. Any library that
# prints to stdout (a warning, a progress bar) therefore cannot corrupt a
# response line. Diagnostics go to stderr, which the Go side captures.
#
# The model is materialised into <model-dir> as real files: onnx-asr's default
# Hugging Face cache stores files as symlinks into blobs/, and onnxruntime
# refuses external weight files that resolve outside the model directory.
import json
import os
import sys

# Files onnx-asr's NeMo RNNT/TDT loaders read from the model directory.
REQUIRED = ("encoder-model.onnx", "decoder_joint-model.onnx", "vocab.txt", "config.json")
READY_MARKER = ".ocode-ready"
# Exports that use other graph names (e.g. bobNight/parakeet-unified-en-0.6b-onnx).
RENAMES = {"encoder.onnx": "encoder-model.onnx", "decoder_joint.onnx": "decoder_joint-model.onnx"}
# Per-repo config for exports that ship without a config.json. The unified EN
# encoder takes 128 mel bins; onnx-asr defaults to 80.
CONFIG_OVERRIDES = {"bobNight/parakeet-unified-en-0.6b-onnx": {"features_size": 128}}


def prepare_model(repo: str, model_dir: str) -> None:
    """Make an export loadable by onnx-asr. Runs once per download."""
    for src, dst in RENAMES.items():
        src_path = os.path.join(model_dir, src)
        if os.path.isfile(src_path):
            os.replace(src_path, os.path.join(model_dir, dst))

    config_path = os.path.join(model_dir, "config.json")
    if not os.path.isfile(config_path) and repo in CONFIG_OVERRIDES:
        with open(config_path, "w", encoding="utf-8") as f:
            json.dump(CONFIG_OVERRIDES[repo], f)

    vocab_path = os.path.join(model_dir, "vocab.txt")
    tokenizer_path = os.path.join(model_dir, "tokenizer.model")
    if os.path.isfile(tokenizer_path) and not has_token_ids(vocab_path):
        import sentencepiece as spm  # pip install sentencepiece

        sp = spm.SentencePieceProcessor(model_file=tokenizer_path)
        size = sp.get_piece_size()
        with open(vocab_path, "w", encoding="utf-8") as f:
            for i in range(size):
                f.write(f"{sp.id_to_piece(i)} {i}\n")
            f.write(f"<blk> {size}\n")


def has_token_ids(vocab_path: str) -> bool:
    with open(vocab_path, "r", encoding="utf-8") as f:
        first = f.readline().rstrip("\n")
    parts = first.split(" ")
    return len(parts) == 2 and parts[1].isdigit()


def redirect_stdout() -> None:
    """Point fd 1 at stderr and keep the real stdout on a private fd.

    The protocol owns stdout. A library that writes to stdout would otherwise
    inject a non-JSON line into the stream and desynchronise the reader.
    """
    try:
        saved = os.dup(1)
        os.dup2(2, 1)
    except OSError:
        saved = None
    # Expose the saved descriptor to the parent so it can be restored; the Go
    # side reads responses from this descriptor, not from fd 1.
    if saved is not None:
        os.set_inheritable(saved, True)
        # The parent learns the descriptor number through the first handshake
        # line, which is written to the inherited descriptor.
        globals()["_OCODE_PROTO_FD"] = saved


def main() -> int:
    if len(sys.argv) != 4:
        print("usage: stt_worker.py <model> <hf-repo> <model-dir>", file=sys.stderr)
        return 2
    model_name, repo, model_dir = sys.argv[1:4]

    redirect_stdout()
    proto_fd = globals().get("_OCODE_PROTO_FD", 1)
    out = os.fdopen(proto_fd, "w", encoding="utf-8", buffering=1)

    from huggingface_hub import snapshot_download

    # Offline once converted: only hit the network before the ready marker exists.
    marker = os.path.join(model_dir, READY_MARKER)
    populated = os.path.isfile(marker)
    snapshot_download(repo, local_dir=model_dir, local_files_only=populated)
    if not populated:
        prepare_model(repo, model_dir)
        open(marker, "w").close()

    import onnx_asr

    # The CPU provider is pinned on purpose. onnx-asr otherwise selects every
    # available onnxruntime provider, and CoreML compiles the model to the
    # Apple Neural Engine on every fresh process (~12 s per start) while
    # supporting only part of the graph. The CPU provider loads in ~1 s.
    model = onnx_asr.load_model(model_name, path=model_dir, providers=["CPUExecutionProvider"])

    load_count = 1
    out.write(json.dumps({
        "op": "ready",
        "pid": os.getpid(),
        "load_count": load_count,
    }) + "\n")
    out.flush()

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except ValueError as exc:
            out.write(json.dumps({"ok": False, "error": f"bad request: {exc}"}) + "\n")
            out.flush()
            continue
        req_id = req.get("id")
        op = req.get("op")
        try:
            if op == "ping":
                out.write(json.dumps({"id": req_id, "ok": True, "ping": True,
                                      "pid": os.getpid(), "load_count": load_count}) + "\n")
            elif op == "recognize":
                path = req["path"]
                result = model.recognize(path)
                if not isinstance(result, str):
                    result = getattr(result, "text", str(result))
                out.write(json.dumps({"id": req_id, "ok": True, "text": result,
                                      "pid": os.getpid(), "load_count": load_count}) + "\n")
            elif op == "quit":
                out.write(json.dumps({"id": req_id, "ok": True}) + "\n")
                out.flush()
                break
            else:
                out.write(json.dumps({"id": req_id, "ok": False,
                                      "error": f"unknown op {op!r}"}) + "\n")
        except Exception as exc:  # noqa: BLE001 - reported to the caller
            out.write(json.dumps({"id": req_id, "ok": False, "error": str(exc),
                                  "pid": os.getpid(), "load_count": load_count}) + "\n")
        out.flush()
    return 0


if __name__ == "__main__":
    sys.exit(main())
