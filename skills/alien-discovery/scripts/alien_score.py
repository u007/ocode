#!/usr/bin/env python3
"""Scoring half of the alien-discovery pipeline.

Subcommands: pairs, candidates, coherence, availability, select.
Layout and schemas: skills/alien-discovery/references/pipeline-contract.md

Only network calls go to TypeSafe /v1/systemone (TYPESAFE_API_KEY). All outputs
are deterministic: ids are sorted as integers, sets lexicographically.
"""
from __future__ import annotations

import argparse
from collections import Counter
import http.client
import itertools
import json
import math
import os
import socket
import ssl
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

import numpy as np

TYPESAFE_HOST = "api.typesafe.ai"
TYPESAFE_PATH = "/v1/systemone"
CONNECT_TIMEOUT_S = 5
HTTP_TIMEOUT_S = 60
RETRY_DELAYS_S = (1, 2, 4, 8)  # a sleep follows each failed attempt except the last
MAX_ATTEMPTS = len(RETRY_DELAYS_S) + 1
TOP_ACTORS_RECORDED = 10
CRITERIA_TEMPLATE = (
    "Incompatible or unrelated; no single <artifact_noun> would combine them",
    "Could coexist but at least one mechanism is forced or redundant",
    "Plausibly one <artifact_noun>; the mechanisms reinforce each other",
    "Clearly one coherent <artifact_noun>; each mechanism depends on the others",
)
FUSION_FORMULA = "(1 - beta) * z_coherence + beta * z_unavailability"


class StageError(Exception):
    """Fatal, user-facing failure. Printed as one line, exit 1."""


class TransportError(StageError):
    """Retryable failure (429, 5xx, connection) that stayed after all attempts."""


class FatalApiError(StageError):
    """Protocol or request failure that retrying cannot fix."""


class _Retryable(Exception):
    """Internal: one attempt failed in a way worth retrying."""


def say(msg: str) -> None:
    print(msg, file=sys.stderr, flush=True)


# ---------------------------------------------------------------- files


def load_json(path: Path):
    if not path.is_file():
        raise StageError(f"missing input file: {path}")
    try:
        with path.open(encoding="utf-8") as f:
            return json.load(f)
    except json.JSONDecodeError as e:
        raise StageError(f"{path} is not valid JSON: {e}") from e


def load_jsonl(path: Path) -> list:
    if not path.is_file():
        raise StageError(f"missing input file: {path}")
    out = []
    for n, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        try:
            out.append(json.loads(line))
        except json.JSONDecodeError as e:
            raise StageError(f"{path} line {n} is not valid JSON: {e}") from e
    return out


def write_json(path: Path, obj) -> None:
    tmp = path.with_name(path.name + ".tmp")
    with tmp.open("w", encoding="utf-8") as f:
        json.dump(obj, f, indent=2, ensure_ascii=False, allow_nan=False)
        f.write("\n")
    os.replace(tmp, path)


def load_progress(path: Path) -> list:
    if not path.exists():
        return []
    recs = []
    for n, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        try:
            recs.append(json.loads(line))
        except json.JSONDecodeError as e:
            raise StageError(
                f"{path} line {n} is not valid JSON ({e}); if it is the torn last line "
                "of an interrupted run, delete that line and rerun"
            ) from e
    return recs


def open_progress(path: Path, header: dict):
    """Open an append-only progress file for resume.

    Returns (records after the header, append handle). A file written for other
    settings (model, question, criteria) is refused, never mixed.
    """
    recs = load_progress(path)
    if recs:
        head = recs[0]
        if head.get("kind") != "header":
            raise StageError(f"{path} has no header line; delete it to start over")
        stored = {k: v for k, v in head.items() if k != "kind"}
        if stored != header:
            raise StageError(f"{path} was written for different settings; delete it to start over")
        body = recs[1:]
    else:
        body = []
    fh = path.open("a", encoding="utf-8")
    if not recs:
        append(fh, {"kind": "header", **header})
    return body, fh


def append(fh, rec: dict) -> None:
    fh.write(json.dumps(rec, ensure_ascii=False, allow_nan=False) + "\n")
    fh.flush()


# ---------------------------------------------------------------- validation


def need(obj, key: str, where: str):
    if not isinstance(obj, dict) or key not in obj:
        raise StageError(f"{where}: missing '{key}'")
    return obj[key]


def parse_int(text, where: str) -> int:
    try:
        return int(text)
    except (TypeError, ValueError) as e:
        raise StageError(f"{where}: {text!r} is not an integer cluster id") from e


def parse_key(key: str) -> tuple:
    return tuple(parse_int(p, f"set key {key!r}") for p in key.split(","))


def key_of(ids) -> str:
    return ",".join(str(int(i)) for i in ids)


def vocab_names(vocab) -> dict:
    """Cluster id (int) -> super-atom name, in ascending id order."""
    atoms = need(vocab, "atoms", "vocab.json")
    pairs = [
        (parse_int(k, "vocab.json atoms"), need(v, "name", f"vocab.json atoms[{k}]"))
        for k, v in atoms.items()
    ]
    return dict(sorted(pairs))


def finite_number(x, where: str) -> float:
    if isinstance(x, bool) or not isinstance(x, (int, float)):
        raise FatalApiError(f"{where}: expected a number, got {x!r}")
    return float(x)


def unit_interval(x, where: str) -> float:
    v = finite_number(x, where)
    if not (math.isfinite(v) and 0.0 <= v <= 1.0):
        raise FatalApiError(f"{where}: {v} is outside [0, 1]")
    return v


def usage_of(resp, where: str) -> dict:
    u = need(resp, "usage", where)
    return {
        "input_tokens": int(need(u, "input_tokens", where)),
        "output_tokens": int(need(u, "output_tokens", where)),
    }


def zero_usage() -> dict:
    return {"input_tokens": 0, "output_tokens": 0}


def add_usage(total: dict, u: dict) -> None:
    total["input_tokens"] += u["input_tokens"]
    total["output_tokens"] += u["output_tokens"]


def api_key() -> str:
    try:
        key = os.environ["TYPESAFE_API_KEY"]
    except KeyError as e:
        raise StageError("TYPESAFE_API_KEY is not set in the environment") from e
    if not key:
        raise StageError("TYPESAFE_API_KEY is set but empty")
    return key


# ---------------------------------------------------------------- TypeSafe


class _OrderedHTTPSConnection(http.client.HTTPSConnection):
    """HTTPS connection that tries IPv4 addresses before IPv6, each with a short connect timeout.

    getaddrinfo lists IPv6 first for the TypeSafe host, and on hosts with a dead IPv6 route
    each IPv6 connect stalls for the full timeout. Trying IPv4 first avoids that stall.
    """

    def connect(self) -> None:
        infos = socket.getaddrinfo(self.host, self.port, type=socket.SOCK_STREAM)
        infos.sort(key=lambda info: 0 if info[0] == socket.AF_INET else 1)  # stable: keeps resolver order within a family
        last: OSError | None = None
        for family, socktype, proto, _, addr in infos:
            sock = socket.socket(family, socktype, proto)
            sock.settimeout(CONNECT_TIMEOUT_S)
            try:
                sock.connect(addr)
            except OSError as e:
                sock.close()
                last = e
                continue
            sock.settimeout(HTTP_TIMEOUT_S)
            self.sock = self._context.wrap_socket(sock, server_hostname=self.host)
            return
        raise OSError(f"could not connect to {self.host}:{self.port}: {last}")


def _post(key: str, body: dict) -> dict:
    conn = _OrderedHTTPSConnection(
        TYPESAFE_HOST, 443, timeout=HTTP_TIMEOUT_S, context=ssl.create_default_context()
    )
    try:
        conn.request(
            "POST",
            TYPESAFE_PATH,
            body=json.dumps(body).encode("utf-8"),
            headers={"Authorization": f"Bearer {key}", "content-type": "application/json"},
        )
        resp = conn.getresponse()
        status = resp.status
        raw = resp.read()
    except (OSError, http.client.HTTPException) as e:
        raise _Retryable(f"transport error: {e}") from e
    finally:
        conn.close()
    if status == 429 or status >= 500:
        text = " ".join(raw.decode("utf-8", "replace").split())[:500]
        raise _Retryable(f"HTTP {status}: {text}")
    if not 200 <= status < 300:
        text = " ".join(raw.decode("utf-8", "replace").split())[:500]
        raise FatalApiError(f"HTTP {status} from TypeSafe (not retried): {text}")
    try:
        return json.loads(raw)
    except json.JSONDecodeError as e:
        raise FatalApiError(f"TypeSafe returned non-JSON: {raw[:200]!r}") from e


def systemone(key: str, body: dict, label: str) -> dict:
    """One /v1/systemone call. Retries 429, 5xx and transport errors; raises TransportError after the last attempt."""
    last = ""
    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            return _post(key, body)
        except _Retryable as e:
            last = str(e)
        if attempt < MAX_ATTEMPTS:
            delay = RETRY_DELAYS_S[attempt - 1]
            say(f"typesafe {label}: attempt {attempt}/{MAX_ATTEMPTS} failed ({last}); retrying in {delay}s")
            time.sleep(delay)
    raise TransportError(f"failed after {MAX_ATTEMPTS} attempts: {last}")


# ---------------------------------------------------------------- shared math


def zscore(values) -> np.ndarray:
    x = np.asarray(values, dtype=np.float64)
    sd = float(x.std())
    if not sd > 0.0:
        raise StageError(f"z-score needs non-zero standard deviation; got std={sd} over {x.size} values")
    return (x - x.mean()) / sd


def rank_pool(keys: list, scores, avails, betas) -> dict:
    """Fuse z-scores per beta. Returns {beta: [entry, ...]} sorted by fusion desc, then set asc."""
    zc = zscore(scores)
    zu = zscore([-a for a in avails])
    out = {}
    for b in betas:
        fus = (1.0 - b) * zc + b * zu
        order = sorted(range(len(keys)), key=lambda i: (-fus[i], keys[i]))
        out[b] = [
            {
                "set": keys[i],
                "fusion": float(fus[i]),
                "z_coherence": float(zc[i]),
                "z_unavailability": float(zu[i]),
            }
            for i in order
        ]
    return out


def unit(v: np.ndarray) -> np.ndarray:
    norm = float(np.linalg.norm(v))
    if not norm > 0.0:
        raise StageError("cannot L2-normalise a zero vector")
    return v / norm


def load_cluster_vectors(d: Path, ids: list) -> dict:
    """Unit cluster vector per vocab cluster id: L2-normalised mean of its member atom embeddings."""
    clusters = need(load_json(d / "clusters.json"), "clusters", "clusters.json")
    atom_ids = need(load_json(d / "embeddings_index.json"), "atom_ids", "embeddings_index.json")
    emb_path = d / "embeddings.npy"
    if not emb_path.is_file():
        raise StageError(f"missing input file: {emb_path}")
    emb = np.load(emb_path).astype(np.float64)
    if emb.ndim != 2 or emb.shape[0] != len(atom_ids):
        raise StageError(f"embeddings.npy shape {emb.shape} does not match {len(atom_ids)} index atom_ids")
    row = {a: i for i, a in enumerate(atom_ids)}
    cvec = {}
    for cid in ids:
        cl = clusters.get(str(cid))
        if cl is None:
            raise StageError(f"vocab cluster {cid} is not in clusters.json")
        members = need(cl, "member_atom_ids", f"clusters.json cluster {cid}")
        if not members:
            raise StageError(f"clusters.json cluster {cid} has no member atoms")
        idx = []
        for a in members:
            if a not in row:
                raise StageError(f"clusters.json cluster {cid} member {a!r} is not in embeddings_index.json")
            idx.append(row[a])
        cvec[cid] = unit(emb[idx].mean(axis=0))
    return cvec


# ---------------------------------------------------------------- pairs


def _pairs_request(api: str, batch: list, names: dict, question: str, model: str):
    label = f"pairs batch from {key_of(batch[0])}"
    mech_ids = sorted({i for p in batch for i in p})
    body = {
        "model": model,
        "state": {"mechanisms": {str(i): names[i] for i in mech_ids}},
        "questions": {
            key_of(p): {
                "type": "noul",
                "instructions": f"Considering only mechanisms {p[0]} and {p[1]} in the state: {question}",
            }
            for p in batch
        },
    }
    resp = systemone(api, body, label)
    answers = need(resp, "answers", label)
    got = {}
    for p in batch:
        k = key_of(p)
        ans = need(answers, k, f"{label} response")
        if need(ans, "type", f"{label} answer {k}") != "noul":
            raise FatalApiError(f"{label}: answer {k} is not a noul answer")
        got[k] = unit_interval(need(ans, "noul", f"{label} answer {k}"), f"{label} answer {k}")
    return got, usage_of(resp, label)


def cmd_pairs(args) -> None:
    d = Path(args.dir)
    names = vocab_names(load_json(d / "vocab.json"))
    question = need(load_json(d / "domain.json"), "coherence_question", "domain.json")
    api = api_key()
    ids = list(names)
    expected = list(itertools.combinations(ids, 2))
    expected_keys = [key_of(p) for p in expected]

    recs, fh = open_progress(d / "pairs.partial.jsonl", {"model": args.jev_model, "question": question})
    try:
        answers: dict = {}
        usage = zero_usage()
        for rec in recs:
            kind = need(rec, "kind", "pairs.partial.jsonl")
            if kind == "answer":
                k = need(rec, "id", "pairs.partial.jsonl answer")
                if k in answers:
                    raise StageError(f"pairs.partial.jsonl repeats pair {k}")
                answers[k] = need(rec, "noul", "pairs.partial.jsonl answer")
            elif kind == "usage":
                add_usage(usage, need(rec, "usage", "pairs.partial.jsonl usage"))
            else:
                raise StageError(f"pairs.partial.jsonl: unknown record kind {kind!r}")
        stray = sorted(set(answers) - set(expected_keys), key=parse_key)
        if stray:
            raise StageError(f"pairs.partial.jsonl has pairs not in the current vocab (e.g. {stray[0]}); delete it to start over")

        pending = [p for p, k in zip(expected, expected_keys) if k not in answers]
        say(f"pairs: {len(expected) - len(pending)} of {len(expected)} ids already answered, requesting {len(pending)}")
        batches = [pending[i : i + args.per_request] for i in range(0, len(pending), args.per_request)]

        ex = ThreadPoolExecutor(max_workers=args.concurrency)
        try:
            futs = [ex.submit(_pairs_request, api, b, names, question, args.jev_model) for b in batches]
            for done, fut in enumerate(as_completed(futs), 1):
                got, u = fut.result()
                for k, v in got.items():
                    append(fh, {"kind": "answer", "id": k, "noul": v})
                    answers[k] = v
                append(fh, {"kind": "usage", "ids": len(got), "usage": u})
                add_usage(usage, u)
                say(f"pairs: batch {done}/{len(batches)} done, {len(answers)}/{len(expected)} answered")
        finally:
            ex.shutdown(wait=True, cancel_futures=True)

        missing = [k for k in expected_keys if k not in answers]
        if missing:
            raise StageError(f"pairs incomplete: {len(missing)} pairs unanswered; rerun to resume")
        write_json(
            d / "pairs.json",
            {
                "question": question,
                "model": args.jev_model,
                "pairs": {k: answers[k] for k in expected_keys},
                "usage": usage,
            },
        )
    finally:
        fh.close()
    say(
        f"pairs: wrote {len(expected_keys)} pairs; cumulative usage "
        f"input {usage['input_tokens']} output {usage['output_tokens']}"
    )


# ---------------------------------------------------------------- candidates


def pair_matrix(pairs_doc, ids: list) -> np.ndarray:
    pairs = need(pairs_doc, "pairs", "pairs.json")
    n = len(ids)
    if len(pairs) != n * (n - 1) // 2:
        raise StageError(f"pairs.json has {len(pairs)} pairs but the vocab needs {n * (n - 1) // 2}; rerun pairs")
    pos = {cid: i for i, cid in enumerate(ids)}
    M = np.zeros((n, n), dtype=np.float64)
    for k, v in pairs.items():
        i, j = parse_key(k)
        if i not in pos or j not in pos or i >= j:
            raise StageError(f"pairs.json key {k!r} is not an i<j pair of vocab clusters")
        M[pos[i], pos[j]] = M[pos[j], pos[i]] = float(v)
    return M


def _combo_blocks(n: int, k: int, chunk: int = 200_000):
    it = itertools.combinations(range(n), k)
    while True:
        block = list(itertools.islice(it, chunk))
        if not block:
            return
        yield np.asarray(block, dtype=np.int64)


def _walk_sets(M: np.ndarray, k: int, n_sample: int, seed: int):
    """Sample distinct k-sets by walking the pair matrix. Returns (sets, attempts)."""
    rng = np.random.default_rng(seed)
    n = M.shape[0]
    seen: set = set()
    attempts = 0
    cap = 20 * n_sample
    while len(seen) < n_sample and attempts < cap:
        attempts += 1
        chosen = [int(rng.integers(n))]
        while len(chosen) < k:
            w = M[:, chosen].mean(axis=1)
            w[chosen] = 0.0
            total = float(w.sum())
            if not total > 0.0:
                raise StageError("sampling walk ran out of weight: noul to every remaining atom is 0")
            chosen.append(int(rng.choice(n, p=w / total)))
        seen.add(tuple(sorted(chosen)))
    if len(seen) < n_sample:
        say(f"candidates: sampling walk found {len(seen)} distinct sets of {n_sample} requested after {attempts} attempts")
    arr = np.asarray(sorted(seen), dtype=np.int64).reshape(-1, k)
    return arr, attempts


def _filter_block(block, M, C, pair_idx, pair_min, max_pair_cosine):
    """Apply the pair_min filter, then the cluster-cosine cap. Returns (kept, pair_mean, pair_min, n_dropped_pair_min, n_dropped_pair_cosine)."""
    vals = np.stack([M[block[:, a], block[:, b]] for a, b in pair_idx], axis=1)
    pmin = vals.min(axis=1)
    pmean = vals.mean(axis=1)
    keep_min = pmin >= pair_min
    n_drop_min = int((~keep_min).sum())
    block, pmean, pmin = block[keep_min], pmean[keep_min], pmin[keep_min]
    cvals = np.stack([C[block[:, a], block[:, b]] for a, b in pair_idx], axis=1)
    keep_cos = cvals.max(axis=1) <= max_pair_cosine
    n_drop_cos = int((~keep_cos).sum())
    return block[keep_cos], pmean[keep_cos], pmin[keep_cos], n_drop_min, n_drop_cos


def cmd_candidates(args) -> None:
    d = Path(args.dir)
    ids = list(vocab_names(load_json(d / "vocab.json")))
    n = len(ids)
    k = args.k
    if not 2 <= k <= n:
        raise StageError(f"--k must be between 2 and the vocab size {n}, got {k}")
    if not -1.0 <= args.max_pair_cosine <= 1.0:
        raise StageError(f"--max-pair-cosine must be in [-1, 1], got {args.max_pair_cosine}")
    M = pair_matrix(load_json(d / "pairs.json"), ids)
    cvec = load_cluster_vectors(d, ids)
    V = np.stack([cvec[c] for c in ids])
    C = V @ V.T
    id_arr = np.asarray(ids, dtype=np.int64)
    pair_idx = list(itertools.combinations(range(k), 2))

    exhaustive = n <= args.exhaustive_max_vocab
    method = "exhaustive" if exhaustive else "sample_walk"
    attempts = None
    kept_blocks, kept_means, kept_mins = [], [], []
    n_enumerated = 0
    n_drop_min = 0
    n_drop_cos = 0
    if exhaustive:
        blocks = _combo_blocks(n, k)
    else:
        sampled, attempts = _walk_sets(M, k, args.n_sample, args.seed)
        blocks = [sampled] if sampled.shape[0] else []
    for block in blocks:
        n_enumerated += block.shape[0]
        b, m, mn, dmin, dcos = _filter_block(block, M, C, pair_idx, args.pair_min, args.max_pair_cosine)
        n_drop_min += dmin
        n_drop_cos += dcos
        kept_blocks.append(b)
        kept_means.append(m)
        kept_mins.append(mn)

    if kept_blocks:
        kept = np.concatenate(kept_blocks)
        means = np.concatenate(kept_means)
        mins = np.concatenate(kept_mins)
    else:
        kept = np.zeros((0, k), dtype=np.int64)
        means = np.zeros(0)
        mins = np.zeros(0)
    order = np.lexsort(kept.T[::-1]) if kept.shape[0] else np.zeros(0, dtype=np.int64)
    kept, means, mins = kept[order], means[order], mins[order]

    sets = id_arr[kept].tolist()
    stats = {
        key_of(s): {"pair_mean": float(m), "pair_min": float(mn)}
        for s, m, mn in zip(sets, means, mins)
    }
    write_json(
        d / "candidates.json",
        {
            "k": k,
            "method": method,
            "params": {
                "n_vocab": n,
                "exhaustive_max_vocab": args.exhaustive_max_vocab,
                "n_sample": None if exhaustive else args.n_sample,
                "sampling_attempts": attempts,
                "seed": args.seed,
                "pair_min": args.pair_min,
                "max_pair_cosine": args.max_pair_cosine,
                "n_enumerated": n_enumerated,
                "n_dropped_pair_min": n_drop_min,
                "n_dropped_pair_cosine": n_drop_cos,
                "n_kept": len(sets),
            },
            "sets": sets,
            "stats": stats,
        },
    )
    summary = {
        "stage": "candidates",
        "method": method,
        "pair_min": args.pair_min,
        "max_pair_cosine": args.max_pair_cosine,
        "n_enumerated": n_enumerated,
        "n_dropped_pair_min": n_drop_min,
        "n_dropped_pair_cosine": n_drop_cos,
        "n_kept": len(sets),
    }
    (d / "candidates_log.jsonl").write_text(json.dumps(summary) + "\n", encoding="utf-8")
    say(
        f"candidates: {method}, enumerated {n_enumerated}, dropped {n_drop_min} with pair_min < {args.pair_min} "
        f"and {n_drop_cos} with pair cosine > {args.max_pair_cosine}, kept {len(sets)}"
    )


# ---------------------------------------------------------------- coherence


def _coherence_request(api, members, names, question, criteria, score_max, model):
    key = key_of(members)
    body = {
        "model": model,
        "state": {"mechanisms": [names[c] for c in members]},
        "questions": {key: {"type": "score", "instructions": question, "criteria": criteria}},
    }
    try:
        resp = systemone(api, body, f"coherence {key}")
    except TransportError as e:
        # Not swallowed: the caller prints this reason and records the set in coherence_log.jsonl.
        return key, None, str(e), None
    where = f"coherence {key}"
    answers = need(resp, "answers", where)
    ans = need(answers, key, f"{where} response")
    if need(ans, "type", where) != "score":
        raise FatalApiError(f"{where}: answer is not a score answer")
    raw = need(ans, "score", where)
    # A null, NaN or infinite score is a non-finite answer: recorded as null and dropped at write time.
    score = None if raw is None else finite_number(raw, where)
    if score is not None and not math.isfinite(score):
        score = None
    conf = unit_interval(need(ans, "confidence", where), where)
    if score is not None and not 0.0 <= score <= score_max:
        raise FatalApiError(f"{where}: score {score} is outside [0, {score_max}]")
    return key, (score, conf), None, usage_of(resp, where)


def cmd_coherence(args) -> None:
    d = Path(args.dir)
    domain = load_json(d / "domain.json")
    question = need(domain, "coherence_question", "domain.json")
    noun = need(domain, "artifact_noun", "domain.json")
    names = vocab_names(load_json(d / "vocab.json"))
    criteria = [c.replace("<artifact_noun>", noun) for c in CRITERIA_TEMPLATE]
    score_max = len(criteria) - 1
    if args.keep < 0:
        raise StageError(f"--keep must be >= 0, got {args.keep}")
    if args.sample_temperature is not None and not args.sample_temperature > 0:
        raise StageError(f"--sample-temperature must be > 0, got {args.sample_temperature}")
    cand_sets = [tuple(int(c) for c in s) for s in need(load_json(d / "candidates.json"), "sets", "candidates.json")]
    ids = list(names)
    pos = {cid: i for i, cid in enumerate(ids)}
    M = pair_matrix(load_json(d / "pairs.json"), ids)

    def pair_stats(members):
        """(pair_mean, pair_min) from pairs.json, so a set outside candidates.json gets the same stats."""
        if len(members) < 2:
            raise StageError(f"set {members} needs at least two clusters")
        for c in members:
            if c not in pos:
                raise StageError(f"set member {c} is not a vocab cluster id")
        vals = [M[pos[a], pos[b]] for a, b in itertools.combinations(members, 2)]
        return float(np.mean(vals)), float(np.min(vals))

    def pair_mean(members):
        return pair_stats(members)[0]

    keep = min(args.keep, len(cand_sets))
    if args.sample_temperature is None:
        method = "top"
        selected = sorted(sorted(cand_sets, key=lambda s: (-pair_mean(s), s))[:keep])
    else:
        # Gumbel-top-k: keep the `keep` largest (logit + Gumbel noise). This draws without
        # replacement with weights exp(pair_mean / T). The draw order is the sorted candidate list.
        method = "sample"
        ordered = sorted(cand_sets)
        logits = np.asarray([pair_mean(s) for s in ordered], dtype=np.float64) / args.sample_temperature
        gumbel = np.random.default_rng(args.seed).gumbel(size=len(ordered))
        chosen = np.argsort(-(logits + gumbel), kind="stable")[:keep]
        selected = sorted(ordered[i] for i in chosen)
    # Included sets (e.g. holdout targets) are scored in addition to the normal selection, even if
    # candidates.json does not hold them. Membership in candidates is applied later by select and run.
    included = []
    if args.include_sets is not None:
        inc = need(load_json(Path(args.include_sets)), "targets", args.include_sets)
        included = [tuple(sorted(int(c) for c in s)) for s in inc]
    n_included = len(set(included) - set(selected))
    selected = sorted(set(selected) | set(included))
    api = api_key()

    recs, fh = open_progress(
        d / "coherence.partial.jsonl",
        {"model": args.jev_model, "question": question, "criteria": criteria},
    )
    try:
        answers: dict = {}
        usage = zero_usage()
        for rec in recs:
            kind = need(rec, "kind", "coherence.partial.jsonl")
            if kind == "answer":
                answers[need(rec, "set", "coherence.partial.jsonl")] = (
                    need(rec, "score", "coherence.partial.jsonl"),
                    need(rec, "confidence", "coherence.partial.jsonl"),
                )
            elif kind == "usage":
                add_usage(usage, need(rec, "usage", "coherence.partial.jsonl usage"))
            else:
                raise StageError(f"coherence.partial.jsonl: unknown record kind {kind!r}")

        pending = [s for s in selected if key_of(s) not in answers]
        say(
            f"coherence: {len(selected) - len(pending)} of {len(selected)} ids already answered, "
            f"requesting {len(pending)}"
        )
        failures: dict = {}
        ex = ThreadPoolExecutor(max_workers=args.concurrency)
        try:
            futs = [
                ex.submit(_coherence_request, api, s, names, question, criteria, score_max, args.jev_model)
                for s in pending
            ]
            for done, fut in enumerate(as_completed(futs), 1):
                key, ans, reason, u = fut.result()
                if reason is not None:
                    failures[key] = reason
                    say(f"coherence: set {key} dropped after retries: {reason}")
                else:
                    score, conf = ans
                    append(fh, {"kind": "answer", "set": key, "score": score, "confidence": conf})
                    answers[key] = (score, conf)
                    append(fh, {"kind": "usage", "usage": u})
                    add_usage(usage, u)
                say(f"coherence: {done}/{len(pending)} requested sets done")
        finally:
            ex.shutdown(wait=True, cancel_futures=True)
    finally:
        fh.close()

    # coherence.json is the union of every answered set in the partial, across all runs and
    # selections. Only this run's transport failures are added to the log, since they are not answered.
    kept, drops = {}, {}
    for key in sorted(answers, key=parse_key):
        s = parse_key(key)
        score, conf = answers[key]
        if score is None:
            drops[key] = "non-finite score"
        elif conf < args.min_confidence:
            drops[key] = f"confidence {conf:.4f} < min-confidence {args.min_confidence}"
        else:
            kept[key] = {
                "pair_mean": pair_mean(s),
                "pair_min": pair_stats(s)[1],
                "score": score,
                "score_max": score_max,
                "confidence": conf,
            }
    for key in failures:
        drops[key] = f"transport failure after retries: {failures[key]}"
    for key in sorted(drops, key=parse_key):
        say(f"coherence: dropped set {key}: {drops[key]}")
    log = [{"set": key, "reason": drops[key]} for key in sorted(drops, key=parse_key)]

    with (d / "coherence_log.jsonl").open("w", encoding="utf-8") as f:
        for rec in log:
            f.write(json.dumps(rec, ensure_ascii=False) + "\n")
    write_json(
        d / "coherence.json",
        {
            "model": args.jev_model,
            "question": question,
            "criteria": criteria,
            "selection": {
                "method": method,
                "temperature": args.sample_temperature,
                "seed": args.seed,
                "keep": keep,
                "include_sets": args.include_sets,
                "n_included": n_included,
            },
            "min_confidence": args.min_confidence,
            "score_max": score_max,
            "sets": {key: kept[key] for key in sorted(kept, key=parse_key)},
            "usage": usage,
        },
    )
    say(
        f"coherence: {method} selection of {len(selected)} sets, requested {len(pending)}; "
        f"union holds {len(kept)} kept and {len(drops)} dropped answered sets; "
        f"cumulative usage input {usage['input_tokens']} output {usage['output_tokens']}"
    )


# ---------------------------------------------------------------- availability


COVERAGE_BATCH_SETS = 64


def cmd_availability(args) -> None:
    d = Path(args.dir)
    if args.top_k < 1:
        raise StageError(f"--top-k must be at least 1, got {args.top_k}")
    names = vocab_names(load_json(d / "vocab.json"))
    cvec = load_cluster_vectors(d, list(names))

    rep = load_json(d / "repertoires.json")
    actor_ids = sorted(rep)
    if not actor_ids:
        raise StageError("repertoires.json has no actors")
    rows, starts, actor_names = [], [], []
    for a in actor_ids:
        atoms = need(rep[a], "atoms", f"repertoires.json actor {a}")
        if not atoms:
            raise StageError(f"repertoires.json actor {a} has no atoms")
        for c in atoms:
            if c not in cvec:
                raise StageError(f"repertoires.json actor {a} atom {c!r} is not a vocab cluster id")
        starts.append(len(rows))
        rows.extend(cvec[c] for c in atoms)
        actor_names.append(need(rep[a], "name", f"repertoires.json actor {a}"))
    R = np.stack(rows)  # every actor's cluster vectors, contiguous per actor
    actor_starts = np.asarray(starts, dtype=np.int64)
    n_actors = len(actor_ids)
    k_used = min(args.top_k, n_actors)
    if n_actors < args.top_k:
        say(f"availability: only {n_actors} actors for top-k={args.top_k}; using all {n_actors} actors")

    def coverage(sets: list) -> np.ndarray:
        """cov(S, actor) = mean over members s of max over the actor's cluster vectors r of cosine(s, r).

        Returns shape (len(sets), n_actors).
        """
        flat, sizes = [], []
        for members in sets:
            if not members:
                raise StageError("availability: empty set")
            for c in members:
                if c not in cvec:
                    raise StageError(f"set member {c} is not a vocab cluster id")
                flat.append(cvec[c])
            sizes.append(len(members))
        S = np.stack(flat)
        set_starts = np.concatenate(([0], np.cumsum(sizes)[:-1])).astype(np.int64)
        cos = S @ R.T  # (members, repertoire rows)
        best = np.maximum.reduceat(cos, actor_starts, axis=1)  # (members, actors)
        sums = np.add.reduceat(best, set_starts, axis=0)  # (sets, actors)
        return sums / np.asarray(sizes, dtype=np.float64)[:, None]

    def summarise(row: np.ndarray):
        order = sorted(range(n_actors), key=lambda i: (-float(row[i]), actor_ids[i]))
        med = float(np.median(row[order[:k_used]]))
        top = [
            {"actor_id": actor_ids[i], "name": actor_names[i], "coverage": float(row[i])}
            for i in order[:TOP_ACTORS_RECORDED]
        ]
        return med, top

    sets = need(load_json(d / "coherence.json"), "sets", "coherence.json")
    keys = sorted(sets, key=parse_key)
    if keys:
        t = parse_key(keys[0])
        both = coverage([t, tuple(reversed(t))])
        a, _ = summarise(both[0])
        b, _ = summarise(both[1])
        diff = abs(a - b)
        if diff > 1e-6:
            raise StageError(f"permutation invariance FAILED on set {keys[0]}: {a} vs {b}")
        say(f"availability: permutation invariance passed on set {keys[0]} (|diff| = {diff:.2e})")

    out_sets = {}
    for start in range(0, len(keys), COVERAGE_BATCH_SETS):
        chunk = keys[start : start + COVERAGE_BATCH_SETS]
        cov = coverage([parse_key(key) for key in chunk])
        for key, row in zip(chunk, cov):
            med, top = summarise(row)
            out_sets[key] = {"availability": med, "top_actors": top}
    write_json(
        d / "availability.json",
        {
            "method": "soft_coverage_topk_median",
            "top_k": args.top_k,
            "top_k_used": k_used,
            "n_actors": n_actors,
            "sets": out_sets,
        },
    )
    say(f"availability: scored {len(out_sets)} sets against {n_actors} actors")


# ---------------------------------------------------------------- select


def _valid_betas(values) -> list:
    for b in values:
        if not 0.0 <= b <= 1.0:
            raise StageError(f"beta must be in [0, 1], got {b}")
    return list(dict.fromkeys(values))


def _reuse_filter(ranked_rows: list, max_reuse: int, top: int):
    """Walk the fusion ranking. Keep a set only if none of its atoms already sits in max_reuse kept sets."""
    uses: Counter = Counter()
    kept: list = []
    n_skipped = 0
    for e in ranked_rows:
        if len(kept) >= top:
            break
        if all(uses[c] < max_reuse for c in e["set"]):
            kept.append(e)
            uses.update(e["set"])
        else:
            n_skipped += 1
    return kept, n_skipped


def cmd_select(args) -> None:
    d = Path(args.dir)
    coh = need(load_json(d / "coherence.json"), "sets", "coherence.json")
    avail = need(load_json(d / "availability.json"), "sets", "availability.json")
    cand = load_json(d / "candidates.json")
    cand_sets = need(cand, "sets", "candidates.json")
    k = need(cand, "k", "candidates.json")
    names = vocab_names(load_json(d / "vocab.json"))
    betas = _valid_betas(args.betas)
    if args.max_atom_reuse < 1:
        raise StageError(f"--max-atom-reuse must be at least 1, got {args.max_atom_reuse}")

    sources = {
        "coherence": set(coh),
        "availability": set(avail),
        "candidates": {key_of(s) for s in cand_sets},
    }
    pool_set = set.intersection(*sources.values())
    if not pool_set:
        raise StageError("pool is empty: coherence.json, availability.json and candidates.json share no sets")
    pool = sorted(pool_set, key=parse_key)
    # Sets that were scored (coherence or availability) but are not in the pool are logged one by one.
    # Candidate sets that were never scored are only counted, since they were never sent to Jev.
    scored = sources["coherence"] | sources["availability"]
    excluded = []
    for key in sorted(scored - pool_set, key=parse_key):
        missing = [name for name, keys in sources.items() if key not in keys]
        excluded.append({"set": key, "reason": "missing from " + ", ".join(missing)})
    n_candidates_unscored = len(sources["candidates"] - scored)
    with (d / "select_log.jsonl").open("w", encoding="utf-8") as f:
        for rec in excluded:
            f.write(json.dumps(rec) + "\n")
            say(f"select: set {rec['set']} not in pool: {rec['reason']}")

    ranked = rank_pool(
        [parse_key(s) for s in pool],
        [float(need(coh[s], "score", f"coherence.json set {s}")) for s in pool],
        [float(need(avail[s], "availability", f"availability.json set {s}")) for s in pool],
        betas,
    )

    results, n_skipped_reuse = {}, {}
    for b in betas:
        kept, skipped = _reuse_filter(ranked[b], args.max_atom_reuse, args.top)
        n_skipped_reuse[str(b)] = skipped
        if len(kept) < args.top:
            say(f"select: beta {b} kept {len(kept)} of --top {args.top} sets after the atom-reuse cap")
        rows = []
        for rank, e in enumerate(kept, 1):
            key = key_of(e["set"])
            rows.append(
                {
                    "rank": rank,
                    "atoms": list(e["set"]),
                    "atom_names": [names[c] for c in e["set"]],
                    "coherence": float(coh[key]["score"]),
                    "pair_mean": float(coh[key]["pair_mean"]),
                    "availability": float(avail[key]["availability"]),
                    "z_coherence": e["z_coherence"],
                    "z_unavailability": e["z_unavailability"],
                    "fusion": e["fusion"],
                    "top_actors": avail[key]["top_actors"][:3],
                }
            )
        results[str(b)] = rows
    write_json(
        d / "results.json",
        {
            "k": k,
            "betas": results,
            "pool_size": len(pool),
            "pool": {
                "coherence": len(sources["coherence"]),
                "availability": len(sources["availability"]),
                "candidates": len(sources["candidates"]),
                "intersection": len(pool),
                "candidates_never_scored": n_candidates_unscored,
            },
            "max_atom_reuse": args.max_atom_reuse,
            "n_skipped_reuse": n_skipped_reuse,
            "fusion_formula": FUSION_FORMULA,
        },
    )
    for b in betas:
        for row in results[str(b)][:3]:
            print(f"beta {b} rank {row['rank']}: {' | '.join(row['atom_names'])} (fusion {row['fusion']:.3f})")
        print(f"beta {b}: skipped {n_skipped_reuse[str(b)]} sets for atom reuse (cap {args.max_atom_reuse})")
    say(
        f"select: pool {len(pool)} = intersection of coherence {len(sources['coherence'])}, "
        f"availability {len(sources['availability'])}, candidates {len(sources['candidates'])} "
        f"({n_candidates_unscored} never scored); wrote {d / 'results.json'}"
    )


# ---------------------------------------------------------------- CLI


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description="Alien-discovery scoring: pairs, candidates, coherence, availability, select.")
    sub = p.add_subparsers(dest="cmd", required=True)

    sp = sub.add_parser("pairs", help="Jev noul verdict for every vocab cluster pair")
    sp.add_argument("--dir", required=True)
    sp.add_argument("--jev-model", default="jev-latest")
    sp.add_argument("--per-request", type=int, default=20)
    sp.add_argument("--concurrency", type=int, default=4)
    sp.set_defaults(func=cmd_pairs)

    sp = sub.add_parser("candidates", help="enumerate or sample k-sets, filter on min pairwise noul")
    sp.add_argument("--dir", required=True)
    sp.add_argument("--k", type=int, default=3)
    sp.add_argument("--exhaustive-max-vocab", type=int, default=400)
    sp.add_argument("--n-sample", type=int, default=50000)
    sp.add_argument("--pair-min", type=float, default=0.35)
    sp.add_argument("--max-pair-cosine", type=float, default=0.85)
    sp.add_argument("--seed", type=int, default=42)
    sp.set_defaults(func=cmd_candidates)

    sp = sub.add_parser("coherence", help="Jev score question per top candidate set")
    sp.add_argument("--dir", required=True)
    sp.add_argument("--jev-model", default="jev-latest")
    sp.add_argument("--keep", type=int, default=2000)
    sp.add_argument("--sample-temperature", type=float, default=None,
                    help="draw the keep sets without replacement with weights exp(pair_mean / T); omit for top-keep")
    sp.add_argument("--seed", type=int, default=42)
    sp.add_argument("--include-sets", default=None,
                    help="JSON file with a \"targets\" list of sets to score in addition to the selection")
    sp.add_argument("--min-confidence", type=float, default=0.30)
    sp.add_argument("--concurrency", type=int, default=4)
    sp.set_defaults(func=cmd_coherence)

    sp = sub.add_parser("availability", help="cosine of each set against the actor bank")
    sp.add_argument("--dir", required=True)
    sp.add_argument("--top-k", type=int, default=10)
    sp.set_defaults(func=cmd_availability)

    sp = sub.add_parser("select", help="fuse coherence and unavailability per beta")
    sp.add_argument("--dir", required=True)
    sp.add_argument("--betas", type=float, nargs="+", default=[0.7])
    sp.add_argument("--top", type=int, default=50)
    sp.add_argument("--max-atom-reuse", type=int, default=3)
    sp.set_defaults(func=cmd_select)
    return p


def main(argv=None) -> None:
    args = build_parser().parse_args(argv)
    try:
        args.func(args)
    except StageError as e:
        say(f"error: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
