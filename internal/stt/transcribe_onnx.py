# Local speech-to-text helper for ocode.
#
# Run as: python3 -I transcribe_onnx.py <onnx-asr-model> <hf-repo> <model-dir> <file.wav>
#
# Prints one JSON object {"text": "..."} on stdout. Everything else (download
# progress, warnings) goes to stderr so the Go caller can parse stdout alone.
# Requires: pip install "onnx-asr[cpu,hub]" sentencepiece
#
# The model is prepared by stt_model_prep.py, which materialises it into
# <model-dir> as real files. onnx-asr's default Hugging Face cache stores files
# as symlinks into blobs/, and onnxruntime refuses external weight files that
# resolve outside the model directory.
import importlib.util
import json
import os
import sys


def load_prep():
    # python -I keeps this script's directory off sys.path, so the shared module
    # is loaded by its path, next to this script.
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "stt_model_prep.py")
    spec = importlib.util.spec_from_file_location("stt_model_prep", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


prep = load_prep()


def main() -> int:
    if len(sys.argv) != 5:
        print("usage: transcribe_onnx.py <model> <hf-repo> <model-dir> <wav>", file=sys.stderr)
        return 2
    model_name, repo, model_dir, wav_path = sys.argv[1:5]

    # Offline once converted: the network is used only until the ready marker exists.
    prep.ensure_model(repo, model_dir)

    import onnx_asr  # imported here so a missing package yields a clear error

    model = onnx_asr.load_model(
        model_name, path=model_dir, quantization="int8", providers=["CPUExecutionProvider"]
    )
    result = model.recognize(wav_path)
    if not isinstance(result, str):
        result = getattr(result, "text", str(result))
    json.dump({"text": result}, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
