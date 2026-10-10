# Local speech-to-text helper for ocode (run as: python3 -I transcribe_onnx.py <onnx-asr-model> <file.wav>).
#
# Prints one JSON object {"text": "..."} on stdout. Everything else (model
# download progress, warnings) goes to stderr so the Go caller can parse stdout
# alone. Requires: pip install "onnx-asr[cpu,hub]"
import json
import sys


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: transcribe_onnx.py <model> <wav>", file=sys.stderr)
        return 2
    model_name, wav_path = sys.argv[1], sys.argv[2]
    import onnx_asr  # imported here so a missing package yields a clear error

    model = onnx_asr.load_model(model_name)
    result = model.recognize(wav_path)
    if not isinstance(result, str):
        result = getattr(result, "text", str(result))
    json.dump({"text": result}, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
