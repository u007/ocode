# Local speech-to-text helper for ocode.
#
# Run as: python3 -I transcribe_onnx.py <onnx-asr-model> <hf-repo> <model-dir> <file.wav>
#
# Prints one JSON object {"text": "..."} on stdout. Everything else (download
# progress, warnings) goes to stderr so the Go caller can parse stdout alone.
# Requires: pip install "onnx-asr[cpu,hub]"
#
# The model is materialised into <model-dir> as real files. onnx-asr's default
# Hugging Face cache stores files as symlinks into blobs/, and onnxruntime
# refuses external weight files that resolve outside the model directory.
import json
import os
import sys

# Files onnx-asr's NeMo RNNT/TDT loaders read from the model directory.
REQUIRED = ("encoder-model.onnx", "decoder_joint-model.onnx", "vocab.txt")
# Exports that use other graph names (e.g. bobNight/parakeet-unified-en-0.6b-onnx).
RENAMES = {"encoder.onnx": "encoder-model.onnx", "decoder_joint.onnx": "decoder_joint-model.onnx"}


def main() -> int:
    if len(sys.argv) != 5:
        print("usage: transcribe_onnx.py <model> <hf-repo> <model-dir> <wav>", file=sys.stderr)
        return 2
    model_name, repo, model_dir, wav_path = sys.argv[1:5]

    from huggingface_hub import snapshot_download

    # Offline once populated: only hit the network when a required file is missing.
    populated = all(os.path.isfile(os.path.join(model_dir, f)) for f in REQUIRED)
    snapshot_download(repo, local_dir=model_dir, local_files_only=populated)
    # Some exports name the graph files differently from onnx-asr's loader.
    # Only the graph files are renamed: the external weight files are referenced
    # by name from inside the graph, so they must keep their names.
    for src, dst in RENAMES.items():
        src_path = os.path.join(model_dir, src)
        if os.path.isfile(src_path):
            os.replace(src_path, os.path.join(model_dir, dst))

    import onnx_asr  # imported here so a missing package yields a clear error

    model = onnx_asr.load_model(model_name, path=model_dir)
    result = model.recognize(wav_path)
    if not isinstance(result, str):
        result = getattr(result, "text", str(result))
    json.dump({"text": result}, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
