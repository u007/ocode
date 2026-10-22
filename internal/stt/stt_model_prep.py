# Shared model preparation for ocode's local speech-to-text helpers.
#
# stt_worker.py (the resident worker) and transcribe_onnx.py (the one-shot
# helper) both make an onnx-asr model ready in a model directory: download the
# int8 export, give its graphs the names onnx-asr loads, rebuild the vocab from
# the tokenizer, and verify the result before writing the ready marker. The
# steps live here so the two cannot drift.
#
# Go embeds this file and writes it next to the script that imports it. The
# scripts run under `python -I`, which keeps their directory off sys.path, so
# each one loads this module by its path (see load_prep in each script).
import json
import os
import sys

READY_MARKER = ".ocode-ready"
# The files onnx-asr loads with quantization="int8", plus the vocab and config
# every export needs. A model directory is ready only when all of them exist.
REQUIRED = ("encoder-model.int8.onnx", "decoder_joint-model.int8.onnx", "vocab.txt", "config.json")
# Exports that use other graph names (e.g. bobNight/parakeet-unified-en-0.6b-onnx).
# Only the graph files are renamed: the external weight files are referenced by
# name from inside the graph, so they must keep their names.
RENAMES = {
    "encoder.onnx": "encoder-model.onnx",
    "decoder_joint.onnx": "decoder_joint-model.onnx",
    "encoder.int8.onnx": "encoder-model.int8.onnx",
    "decoder_joint.int8.onnx": "decoder_joint-model.int8.onnx",
}
# Only the int8 weights are loaded (about 0.7 GB against 2.4 GB for fp32), so
# only they are downloaded. The tokenizer and vocab are tiny and always needed.
ALLOW_PATTERNS = ["*.int8.onnx", "*.int8.onnx.data", "vocab.txt", "config.json", "tokenizer.model", "nemo128.onnx"]
# Per-repo config for exports that ship without a config.json. The unified EN
# encoder takes 128 mel bins; onnx-asr defaults to 80.
CONFIG_OVERRIDES = {"bobNight/parakeet-unified-en-0.6b-onnx": {"features_size": 128}}


def rename_graphs(model_dir: str) -> None:
    """Give an export's graph files the names onnx-asr looks for. Idempotent."""
    for src, dst in RENAMES.items():
        src_path = os.path.join(model_dir, src)
        if os.path.isfile(src_path):
            os.replace(src_path, os.path.join(model_dir, dst))


def has_token_ids(vocab_path: str) -> bool:
    with open(vocab_path, "r", encoding="utf-8") as f:
        first = f.readline().rstrip("\n")
    parts = first.split(" ")
    return len(parts) == 2 and parts[1].isdigit()


def prepare_model(repo: str, model_dir: str) -> None:
    """Make a freshly downloaded export loadable by onnx-asr."""
    rename_graphs(model_dir)

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


def missing_files(model_dir: str) -> list:
    return [name for name in REQUIRED if not os.path.isfile(os.path.join(model_dir, name))]


def ensure_model(repo: str, model_dir: str) -> None:
    """Make model_dir hold a ready int8 export of repo, downloading it if needed.

    The ready marker means an earlier install finished, so the model loads
    without the network. A marker whose required files are gone (an install
    from before int8, or files removed by hand) is dropped, and the download
    runs again. A download that still lacks the int8 files is an error: the
    marker is not written and no fp32 fallback is attempted.
    """
    from huggingface_hub import snapshot_download

    marker = os.path.join(model_dir, READY_MARKER)
    populated = os.path.isfile(marker)
    # Installs made before int8 hold the exporter's own graph names. Rename them
    # before judging what is missing, so a renamed install is not re-downloaded.
    rename_graphs(model_dir)
    if populated:
        missing = missing_files(model_dir)
        if missing:
            print(f"stt: {repo} is marked ready but is missing {', '.join(missing)}; downloading it again", file=sys.stderr)
            os.remove(marker)
            populated = False

    snapshot_download(repo, local_dir=model_dir, local_files_only=populated, allow_patterns=ALLOW_PATTERNS)
    if not populated:
        prepare_model(repo, model_dir)
        missing = missing_files(model_dir)
        if missing:
            raise RuntimeError(f"{repo} does not provide the int8 files ocode needs; missing: {', '.join(missing)}")
        open(marker, "w").close()
