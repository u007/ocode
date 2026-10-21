# Local Kyutai Pocket TTS worker for ocode.
#
# Run as: python kyutai_worker.py
#
# Speaks a line-delimited JSON protocol on stdin/stdout:
#
#   -> {"id":1,"op":"synth","voice":"alba","text":"...","out":"/abs/clip.wav"}
#   <- {"id":1,"ok":true}
#   <- {"id":1,"ok":false,"error":"..."}
#
# The model is loaded ONCE at start and each voice state is computed once per
# voice and reused. Loading the model and the voice prompt is the slow part of
# synthesis; per-utterance generation is cheap by comparison.
#
# stdout is the protocol channel, so it is redirected to a private duplicate of
# the real stdout at startup and fd 1 is pointed at stderr. A library that
# prints to stdout cannot corrupt a response line. Diagnostics go to stderr,
# which the Go side captures.
import json
import os
import sys

import scipy.io.wavfile

from pocket_tts import TTSModel


def redirect_stdout():
    """Return a file for protocol replies and point fd 1 at stderr."""
    saved = os.dup(1)
    os.dup2(2, 1)
    sys.stdout = sys.stderr
    return os.fdopen(saved, "w", encoding="utf-8")


def main():
    proto = redirect_stdout()
    model = TTSModel.load_model()
    states = {}
    for line in sys.stdin:
        req = json.loads(line)
        try:
            if req["op"] != "synth":
                raise ValueError("unknown op %r" % req["op"])
            voice = req["voice"]
            if voice not in states:
                states[voice] = model.get_state_for_audio_prompt(voice)
            audio = model.generate_audio(states[voice], req["text"]).detach().cpu().numpy()
            scipy.io.wavfile.write(req["out"], model.sample_rate, audio)
            reply = {"id": req["id"], "ok": True}
        except Exception as err:  # returned to the Go caller as the request error
            reply = {"id": req.get("id"), "ok": False, "error": "%s: %s" % (type(err).__name__, err)}
        proto.write(json.dumps(reply) + "\n")
        proto.flush()


if __name__ == "__main__":
    main()
