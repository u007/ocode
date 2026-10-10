# deberta-judge

A local decision judge for ocode, built on a DeBERTa-v3 natural-language-inference
checkpoint. It answers the same typed questions Jev does (`noul`, `score`,
`choice`), so any judge slot can use it in place of `typesafe/jev-latest`.

It is a sidecar, not an in-process model: ocode talks to it over loopback HTTP.

## Install and run

```bash
cd plugins/deberta-judge
python3 -m venv .venv && . .venv/bin/activate
pip install -r requirements.txt          # add --index-url https://download.pytorch.org/whl/cpu for a CPU-only torch
python server.py --port 8765             # first run downloads the checkpoint (~740 MB)
```

Check it is up: `curl -s http://127.0.0.1:8765/healthz` → `{"ok": true, "model": "deberta-v3-base-nli"}`.

## Point ocode at it

Select the judge model `deberta/deberta-v3-base-nli` for the slot you want to replace
(for example the permission judge, `permissions.auto.model`). The URL defaults to
`http://127.0.0.1:8765`; override it with `OCODE_DEBERTA_URL`.

If the sidecar is not running, the judge reports an error and the slot defers to the
human (permission) or keeps every candidate (relevance judges). It never falls back to Jev.

## What it is, and what it is not

Measured on the default checkpoint (`MoritzLaurer/DeBERTa-v3-base-mnli-fever-anli`, CPU),
zero-shot, no fine-tuning:

| Probe | Result | Why it matters |
|---|---|---|
| `noul`, relevant doc | 0.32 | Relevance keeps a candidate at `noul >= 0.5`, so a relevant doc is **vetoed** |
| `noul`, irrelevant docs | 0.001, 0.001 | Ordering is right, but the scale is far below 0.5 |
| `choice`, `rm -rf /` | allow 0.71, margin 0.41 | **Wrong verdict.** The margin keeps it under the 0.80 auto floor, so it defers |
| `choice`, `ls -la` | allow 0.92, margin 0.83 | Right verdict, above the floor |
| 800-word state | HTTP 422 | Refused as designed |

Consequences:

- **Do not use it for the permission judge** unless you accept that some destructive
  commands get a confident-looking verdict. The confidence floor is the only thing that
  stops a wrong approval, and that floor is calibrated for Jev, not for this model.
- **Do not use it for the relevance judges** (`discovery`, `doc_search`, `code_search`)
  without recalibrating. At the 0.5 keep threshold it hides almost every candidate.
- Rewording the criteria changes the verdicts a lot. Treat the mapping in `server.py` as a
  starting point, not a validated judge.

Other limits:

- **512-token window.** State and question together must fit in 512 tokens. A longer state
  gets HTTP 422 and is refused, never truncated. Permission states usually exceed this, so
  most permission decisions will defer to the human.
- **Unauthenticated.** It binds `127.0.0.1` by default. Do not expose it to a network.
- **Not a chat model.** It cannot be used for chat, compaction, or small-model roles.

A real fix needs fine-tuning on ocode's own judge labels. That is a separate piece of work.
