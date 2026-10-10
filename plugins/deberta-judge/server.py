#!/usr/bin/env python3
"""Local DeBERTa-v3 decision judge for ocode.

Serves the same System One wire contract that ocode's TypeSafe (Jev) client
uses, so a judge slot configured as "deberta/deberta-v3-base-nli" can replace
"typesafe/jev-latest" without any change to the judges themselves:

    POST /systemone   {"model", "state", "questions"} -> {"model", "answers", "usage"}
    GET  /healthz     -> {"ok": true, "model": "deberta-v3-base-nli"}

How the three question types are answered (zero-shot NLI, no fine-tuning):

  The state is the NLI PREMISE. A question's text is the HYPOTHESIS. The model's
  entailment probability is the signal.

  noul    -> noul = P(entailment). ocode's relevance judge keeps a candidate when
             noul >= 0.5, so a high value means "the state supports this".
  score   -> score = P(entailment).
  choice  -> one hypothesis per criteria entry ("label: description"); the
             entailment probabilities are normalised across the options. choice
             is the argmax, and confidence is the margin between the top two
             (a distribution-shape statistic, not the probability itself).

Refusal, not truncation: the NLI model reads at most 512 tokens for the premise
and hypothesis together. A longer state is answered with HTTP 422 and never
silently clipped, because a judge that reads a partial state without being told
can approve something it never saw. ocode treats a refusal as a deferral.

Stdlib HTTP server plus torch and transformers; see requirements.txt.
Run:  python3 server.py --port 8765
"""

import argparse
import json
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SERVED_MODEL = "deberta-v3-base-nli"
DEFAULT_HF_MODEL = "MoritzLaurer/DeBERTa-v3-base-mnli-fever-anli"
MAX_PAIR_TOKENS = 512
QUESTION_TYPES = ("noul", "score", "choice")


class TooLong(Exception):
    def __init__(self, tokens):
        super().__init__(f"premise and hypothesis need {tokens} tokens; the window is {MAX_PAIR_TOKENS}")
        self.tokens = tokens


class Judge:
    def __init__(self, hf_model, device):
        import torch
        from transformers import AutoModelForSequenceClassification, AutoTokenizer

        self.torch = torch
        self.device = device
        self.tokenizer = AutoTokenizer.from_pretrained(hf_model)
        self.model = AutoModelForSequenceClassification.from_pretrained(hf_model).to(device).eval()
        # Look the entailment index up from the checkpoint rather than assuming
        # label 0: an NLI checkpoint with a different label order must still work.
        labels = {str(v).lower(): int(k) for k, v in self.model.config.id2label.items()}
        if "entailment" not in labels:
            raise SystemExit(f"{hf_model} has no 'entailment' label; the judge needs an NLI checkpoint")
        self.entail = labels["entailment"]
        # One forward pass at a time: the HTTP server is threaded, the CPU is not.
        self.lock = threading.Lock()

    def entail_prob(self, premise, hypothesis):
        enc = self.tokenizer(premise, hypothesis, return_tensors="pt", truncation=False)
        n = enc["input_ids"].shape[1]
        if n > MAX_PAIR_TOKENS:
            raise TooLong(n)
        enc = {k: v.to(self.device) for k, v in enc.items()}
        with self.torch.no_grad(), self.lock:
            logits = self.model(**enc).logits[0]
        return float(logits.softmax(-1)[self.entail])

    def answer(self, premise, questions):
        answers = {}
        for qid, q in questions.items():
            if not isinstance(q, dict):
                raise ValueError(f"question {qid!r} is not an object")
            qtype = q.get("type")
            if qtype not in QUESTION_TYPES:
                raise ValueError(f"question {qid!r} has unsupported type {qtype!r}")
            instructions = str(q.get("instructions") or "").strip()
            if qtype in ("noul", "score"):
                if not instructions:
                    raise ValueError(f"question {qid!r} has no instructions")
                p = self.entail_prob(premise, instructions)
                key = "noul" if qtype == "noul" else "score"
                answers[qid] = {"type": qtype, key: p}
                continue
            criteria = q.get("criteria") or {}
            if not criteria:
                raise ValueError(f"choice question {qid!r} has no criteria")
            labels = list(criteria.keys())
            raw = {}
            for label in labels:
                desc = str(criteria.get(label) or "").strip()
                hypothesis = f"{label}: {desc}" if desc else str(label)
                raw[label] = self.entail_prob(premise, hypothesis)
            total = sum(raw.values())
            probs = {l: (raw[l] / total if total > 0 else 1.0 / len(labels)) for l in labels}
            ranked = sorted(labels, key=lambda l: probs[l], reverse=True)
            top = probs[ranked[0]]
            second = probs[ranked[1]] if len(ranked) > 1 else 0.0
            answers[qid] = {
                "type": "choice",
                "choice": ranked[0],
                "probabilities": probs,
                "confidence": top - second,
            }
        return answers


def state_text(state):
    if state is None:
        return ""
    if isinstance(state, str):
        return state
    return json.dumps(state, ensure_ascii=False, sort_keys=True)


class Handler(BaseHTTPRequestHandler):
    judge = None  # set by main()

    def log_message(self, fmt, *args):
        # The judge runs as a background process; per-request access lines are noise.
        pass

    def _send(self, code, obj):
        raw = json.dumps(obj).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path == "/healthz":
            self._send(200, {"ok": True, "model": SERVED_MODEL})
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/systemone":
            self._send(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length") or 0)
            body = json.loads(self.rfile.read(length) or b"{}")
        except (ValueError, json.JSONDecodeError):
            self._send(400, {"error": "body is not JSON"})
            return
        if body.get("model") != SERVED_MODEL:
            self._send(400, {"error": f"unknown model {body.get('model')!r}; this judge serves {SERVED_MODEL!r}"})
            return
        questions = body.get("questions")
        if not isinstance(questions, dict) or not questions:
            self._send(400, {"error": "at least one question is required"})
            return
        premise = state_text(body.get("state")).strip()
        if not premise:
            self._send(400, {"error": "state is empty"})
            return
        try:
            answers = self.judge.answer(premise, questions)
        except TooLong as e:
            self._send(422, {"error": str(e) + "; refusing rather than truncating"})
            return
        except ValueError as e:
            self._send(400, {"error": str(e)})
            return
        self._send(200, {
            "model": SERVED_MODEL,
            "answers": answers,
            "usage": {"input_tokens": 0, "output_tokens": 0},
        })


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--hf-model", default=DEFAULT_HF_MODEL, help="Hugging Face NLI checkpoint")
    p.add_argument("--host", default="127.0.0.1", help="bind address (loopback by default; the judge is unauthenticated)")
    p.add_argument("--port", type=int, default=8765)
    p.add_argument("--device", default="cpu", help="torch device, e.g. cpu or cuda")
    args = p.parse_args(argv)

    if args.host not in ("127.0.0.1", "localhost", "::1"):
        print("warning: the judge has no authentication; binding a non-loopback address exposes it to the network", file=sys.stderr)

    print(f"loading {args.hf_model} on {args.device} ...", file=sys.stderr)
    Handler.judge = Judge(args.hf_model, args.device)
    server = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"deberta judge serving {SERVED_MODEL} on http://{args.host}:{args.port}", file=sys.stderr)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
