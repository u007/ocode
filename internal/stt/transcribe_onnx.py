# Local speech-to-text helper for ocode.
#
# Run as: python3 -I transcribe_onnx.py <onnx-asr-model> <hf-repo> <model-dir> <file.wav>
#
# Prints one JSON object {"text": "..."} on stdout. Everything else (download
# progress, warnings) goes to stderr so the Go caller can parse stdout alone.
# Requires: pip install "onnx-asr[cpu,hub]" sentencepiece
#
# The model is materialised into <model-dir> as real files. onnx-asr's default
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
    # Some exports name the graph files differently from onnx-asr's loader.
    # Only the graph files are renamed: the external weight files are referenced
    # by name from inside the graph, so they must keep their names.
    for src, dst in RENAMES.items():
        src_path = os.path.join(model_dir, src)
        if os.path.isfile(src_path):
            os.replace(src_path, os.path.join(model_dir, dst))

    config_path = os.path.join(model_dir, "config.json")
    if not os.path.isfile(config_path) and repo in CONFIG_OVERRIDES:
        with open(config_path, "w", encoding="utf-8") as f:
            json.dump(CONFIG_OVERRIDES[repo], f)

    # onnx-asr reads "token id" lines. Exports that ship a SentencePiece
    # tokenizer.model without ids get the vocab rebuilt from it, plus the
    # "<blk>" entry that the RNNT decoder needs for its blank index.
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
    with open(vocab_path, encoding="utf-8") as f:
        first = f.readline().rstrip("\n")
    parts = first.split(" ")
    return len(parts) == 2 and parts[1].isdigit()


def main() -> int:
    if len(sys.argv) != 5:
        print("usage: transcribe_onnx.py <model> <hf-repo> <model-dir> <wav>", file=sys.stderr)
        return 2
    model_name, repo, model_dir, wav_path = sys.argv[1:5]

    from huggingface_hub import snapshot_download

    # Offline once converted: only hit the network before the ready marker exists.
    marker = os.path.join(model_dir, READY_MARKER)
    populated = os.path.isfile(marker)
    snapshot_download(repo, local_dir=model_dir, local_files_only=populated)
    if not populated:
        prepare_model(repo, model_dir)
        open(marker, "w").close()

    import onnx_asr  # imported here so a missing package yields a clear error

    model = onnx_asr.load_model(model_name, path=model_dir)
    result = model.recognize(wav_path)
    if not isinstance(result, str):
        result = getattr(result, "text", str(result))
    json.dump({"text": result}, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
