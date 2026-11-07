#!/usr/bin/env python3
"""Embed, cluster and name alien-discovery atoms.

embed     POST atom texts to an OpenAI-compatible /v1/embeddings URL; write embeddings.npy + embeddings_index.json.
cluster   Reduce, run HDBSCAN, reassign noise by centroid cosine; write clusters.json + clusters_to_name.jsonl.
finalize  Join the agent's cluster_names.jsonl with the clusters; write vocab.json + repertoires.json.
Every subcommand takes --dir DIR. Dropped items go to stderr and to DIR/cluster_log.jsonl.
"""

import argparse
import hashlib
import json
import os
import sys
import urllib.request

import numpy as np
from sklearn.cluster import HDBSCAN
from sklearn.decomposition import PCA

LOG_NAME = "cluster_log.jsonl"
CENTROID_IDS = 15
SAMPLE_ATOMS = 3
COHERENCE_LEVELS = ("low", "medium", "high")
SIZE_BUCKETS = (("1", 1, 1), ("2-4", 2, 4), ("5-9", 5, 9), ("10-19", 10, 19), ("20+", 20, None))


class AlienError(Exception):
    """Expected input or state problem: reported on one line, exit 1."""


def fail(msg):
    raise AlienError(msg)


# ---- file helpers -------------------------------------------------------------

def require_file(p):
    if not os.path.isfile(p):
        fail(f"missing input file: {p}")


def get(obj, key, where):
    if not isinstance(obj, dict) or key not in obj:
        fail(f"{where}: missing key '{key}'")
    return obj[key]


def load_json(p):
    require_file(p)
    with open(p, encoding="utf-8") as f:
        try:
            return json.load(f)
        except json.JSONDecodeError as e:
            fail(f"{p}: invalid JSON ({e})")


def read_jsonl(p):
    require_file(p)
    rows = []
    with open(p, encoding="utf-8") as f:
        for n, line in enumerate(f, 1):
            if line.strip():
                try:
                    rows.append(json.loads(line))
                except json.JSONDecodeError as e:
                    fail(f"{p}:{n}: invalid JSON ({e})")
    return rows


def write_atomic(p, writer):
    tmp = p + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        writer(f)
    os.replace(tmp, p)


def write_json(p, obj):
    def writer(f):
        json.dump(obj, f, indent=1, ensure_ascii=False)
        f.write("\n")
    write_atomic(p, writer)


def write_jsonl(p, rows):
    def writer(f):
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    write_atomic(p, writer)


def save_npy(p, arr):
    # Binary handle: np.save would append ".npy" to a path that lacks it.
    tmp = p + ".tmp"
    with open(tmp, "wb") as f:
        np.save(f, arr)
    os.replace(tmp, p)


def sha256_file(p):
    require_file(p)
    h = hashlib.sha256()
    with open(p, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def unit_rows(arr):
    norms = np.linalg.norm(arr, axis=1, keepdims=True)
    if (norms == 0).any():
        fail("zero-norm vector cannot be L2-normalised")
    return arr / norms


def log_stage(d, stage, drops):
    """drops: list of (item, reason). Replaces this stage's old lines so reruns do not duplicate."""
    p = os.path.join(d, LOG_NAME)
    kept = []
    if os.path.isfile(p):
        kept = [r for r in read_jsonl(p) if get(r, "stage", LOG_NAME) != stage]
    new = [{"stage": stage, "item": item, "reason": reason} for item, reason in sorted(drops)]
    for row in new:
        print(f"alien_cluster: {stage}: dropped {row['item']}: {row['reason']}", file=sys.stderr)
    write_jsonl(p, kept + new)


# ---- inputs -------------------------------------------------------------------

def load_atoms(d):
    rows = read_jsonl(os.path.join(d, "atoms.jsonl"))
    seen = set()
    for n, r in enumerate(rows, 1):
        where = f"atoms.jsonl row {n}"
        for key in ("atom_id", "artifact_id", "text", "quality"):
            get(r, key, where)
        if not isinstance(r["text"], str) or not r["text"].strip():
            fail(f"{where}: empty text for {r['atom_id']}")
        if r["atom_id"] in seen:
            fail(f"atoms.jsonl: duplicate atom_id {r['atom_id']}")
        seen.add(r["atom_id"])
    return sorted(rows, key=lambda r: r["atom_id"])


def load_corpus(d):
    rows = read_jsonl(os.path.join(d, "corpus.jsonl"))
    seen = set()
    for n, r in enumerate(rows, 1):
        where = f"corpus.jsonl row {n}"
        for key in ("id", "title", "actor_ids", "date", "url", "text"):
            get(r, key, where)
        if r["id"] in seen:
            fail(f"{where}: duplicate artifact id {r['id']}")
        seen.add(r["id"])
    return sorted(rows, key=lambda r: r["id"])


def load_actors(d):
    rows = read_jsonl(os.path.join(d, "actors.jsonl"))
    seen = set()
    for n, r in enumerate(rows, 1):
        where = f"actors.jsonl row {n}"
        for key in ("id", "name", "url"):
            get(r, key, where)
        if r["id"] in seen:
            fail(f"{where}: duplicate actor id {r['id']}")
        seen.add(r["id"])
    return sorted(rows, key=lambda r: r["id"])


def load_index_and_vectors(d):
    index_p = os.path.join(d, "embeddings_index.json")
    npy_p = os.path.join(d, "embeddings.npy")
    require_file(npy_p)
    index = load_json(index_p)
    ids = get(index, "atom_ids", "embeddings_index.json")
    emb = np.load(npy_p)
    if emb.ndim != 2 or emb.shape[0] != len(ids):
        fail(f"embeddings.npy shape {emb.shape} does not match {len(ids)} atom_ids")
    return index, ids, emb


# ---- embed --------------------------------------------------------------------

def post_embeddings(url, model, texts):
    body = json.dumps({"input": texts, "model": model}).encode("utf-8")
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"}, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=300) as resp:
            payload = json.loads(resp.read().decode("utf-8"))
    except (OSError, ValueError) as e:
        fail(f"embeddings request to {url} failed: {e}")
    items = get(payload, "data", f"response from {url}")
    if len(items) != len(texts):
        fail(f"response from {url} has {len(items)} embeddings for {len(texts)} inputs")
    items = sorted(items, key=lambda it: get(it, "index", "embedding item"))
    if [it["index"] for it in items] != list(range(len(texts))):
        fail(f"response from {url} has indices that are not 0..{len(texts) - 1}")
    vecs = [get(it, "embedding", "embedding item") for it in items]
    if len({len(v) for v in vecs}) != 1:
        fail(f"response from {url} has embeddings of differing dimensions")
    arr = np.array(vecs, dtype=np.float64)
    if not np.isfinite(arr).all():
        fail(f"response from {url} has non-finite embedding values")
    return arr


def embed(d, url, model, batch):
    if batch < 1:
        fail("--batch must be at least 1")
    atoms = load_atoms(d)
    if not atoms:
        fail("atoms.jsonl has no atoms")
    ids = [a["atom_id"] for a in atoms]
    texts = [a["text"] for a in atoms]
    sha = sha256_file(os.path.join(d, "atoms.jsonl"))
    index_p = os.path.join(d, "embeddings_index.json")
    npy_p = os.path.join(d, "embeddings.npy")

    if os.path.isfile(index_p) and os.path.isfile(npy_p):
        index = load_json(index_p)
        if get(index, "sha256_of_atoms_jsonl", "embeddings_index.json") == sha:
            if get(index, "model", "embeddings_index.json") != model or get(index, "url", "embeddings_index.json") != url:
                fail("embeddings.npy was built with a different model or url; delete it to re-embed")
            print(f"embed: skip, embeddings_index.json hash matches atoms.jsonl ({len(ids)} atoms)")
            return

    part_npy = os.path.join(d, "embeddings.partial.npy")
    part_json = os.path.join(d, "embeddings.partial.json")
    parts = []
    done = 0
    if os.path.isfile(part_json):
        meta = load_json(part_json)
        where = "embeddings.partial.json"
        if get(meta, "atoms_sha256", where) != sha:
            fail("embeddings.partial.json was built from a different atoms.jsonl; delete embeddings.partial.* to restart")
        if get(meta, "model", where) != model or get(meta, "url", where) != url:
            fail("embeddings.partial.json was built with a different model or url; delete embeddings.partial.* to restart")
        require_file(part_npy)
        part = np.load(part_npy)
        rows = get(meta, "rows", where)
        if part.ndim != 2 or part.shape[0] != rows or rows > len(ids):
            fail(f"embeddings.partial.npy shape {part.shape} does not match rows={rows}")
        if rows:
            parts.append(part)
            done = rows
        print(f"embed: resuming at row {done} of {len(ids)}", file=sys.stderr)

    for start in range(done, len(ids), batch):
        chunk = texts[start:start + batch]
        vecs = unit_rows(post_embeddings(url, model, chunk))
        if parts and vecs.shape[1] != parts[0].shape[1]:
            fail(f"embedding dimension changed from {parts[0].shape[1]} to {vecs.shape[1]}")
        parts.append(vecs)
        save_npy(part_npy, np.vstack(parts))
        write_json(part_json, {"atoms_sha256": sha, "model": model, "url": url, "rows": start + len(chunk)})
        print(f"embed: {start + len(chunk)}/{len(ids)} rows", file=sys.stderr)

    emb = np.vstack(parts).astype(np.float32)
    if emb.shape[0] != len(ids):
        fail(f"internal: {emb.shape[0]} embedded rows for {len(ids)} atoms")
    # The index marks completion, so drop any stale one before the matrix changes.
    if os.path.isfile(index_p):
        os.remove(index_p)
    save_npy(npy_p, emb)
    write_json(index_p, {"atom_ids": ids, "model": model, "url": url, "sha256_of_atoms_jsonl": sha})
    for p in (part_npy, part_json):
        if os.path.isfile(p):
            os.remove(p)
    print(f"embed: {len(ids)} atoms, dim {emb.shape[1]}, model {model}")


# ---- cluster ------------------------------------------------------------------

def reduce_vectors(X, method, components, umap_neighbors, seed):
    if method == "none":
        return X
    if components >= min(X.shape):
        fail(f"--components {components} must be below min(n_atoms, dim) = {min(X.shape)}")
    if method == "pca":
        return PCA(n_components=components, svd_solver="full", random_state=seed).fit_transform(X)
    try:
        import umap
    except ImportError as e:
        fail(f"--reduce umap needs the umap-learn package, which is not importable ({e})")
    return umap.UMAP(n_components=components, n_neighbors=umap_neighbors, metric="cosine", random_state=seed).fit_transform(X)


def reassign_noise(X, labels, threshold):
    """Assign noise rows whose cosine to a fixed cluster centroid is >= threshold.

    Centroids come from the HDBSCAN members and are not recomputed. Returns the new
    labels and {row: max cosine} for noise rows left unassigned (None when no clusters).
    """
    labels = labels.copy()
    cids = sorted(set(labels.tolist()) - {-1})
    noise_rows = np.where(labels == -1)[0]
    if not cids:
        return labels, {int(r): None for r in noise_rows}
    centroids = unit_rows(np.vstack([X[labels == c].mean(axis=0) for c in cids]))
    sims = X[noise_rows] @ centroids.T
    best = sims.argmax(axis=1)
    best_sim = sims.max(axis=1)
    order = sorted(range(len(noise_rows)), key=lambda i: (-float(best_sim[i]), int(noise_rows[i])))
    unassigned = {}
    for i in order:
        row = int(noise_rows[i])
        if best_sim[i] >= threshold:
            labels[row] = cids[int(best[i])]
        else:
            unassigned[row] = float(best_sim[i])
    return labels, unassigned


def size_histogram(sizes):
    parts = []
    for label, lo, hi in SIZE_BUCKETS:
        n = sum(1 for s in sizes if s >= lo and (hi is None or s <= hi))
        parts.append(f"{label}: {n}")
    return ", ".join(parts)


def cluster(d, min_cluster_size, min_samples, reduce, components, umap_neighbors, reassign_cosine, seed):
    sha = sha256_file(os.path.join(d, "atoms.jsonl"))
    corpus_sha = sha256_file(os.path.join(d, "corpus.jsonl"))
    index, ids, emb = load_index_and_vectors(d)
    if get(index, "sha256_of_atoms_jsonl", "embeddings_index.json") != sha:
        fail("embeddings are stale: atoms.jsonl changed since embed; run embed again")
    atoms = {a["atom_id"]: a for a in load_atoms(d)}
    known = {r["id"] for r in load_corpus(d)}

    drops = []
    keep = []
    for row, aid in enumerate(ids):
        art = atoms[aid]["artifact_id"]
        if art in known:
            keep.append(row)
        else:
            drops.append((aid, f"artifact {art} is not in corpus.jsonl"))
    if not keep:
        fail("no atom belongs to a corpus artifact")
    keep_ids = [ids[r] for r in keep]
    X = unit_rows(emb[np.array(keep, dtype=int)].astype(np.float64))
    Z = unit_rows(reduce_vectors(X, reduce, components, umap_neighbors, seed))

    labels = HDBSCAN(min_cluster_size=min_cluster_size, min_samples=min_samples, metric="euclidean").fit_predict(Z)
    noise_before = int((labels == -1).sum())
    labels, unassigned = reassign_noise(X, labels, reassign_cosine)
    noise_after = int((labels == -1).sum())
    for row, best in unassigned.items():
        reason = "noise: no cluster exists to reassign to" if best is None else f"noise: best centroid cosine {best:.4f} < {reassign_cosine}"
        drops.append((keep_ids[row], reason))
    log_stage(d, "cluster", drops)

    cids = sorted(set(labels.tolist()) - {-1})
    if not cids:
        print("alien_cluster: warning: HDBSCAN found no clusters; lower --min-cluster-size", file=sys.stderr)
    clusters = {}
    for c in cids:
        mem = np.where(labels == c)[0]
        centroid = unit_rows(X[mem].mean(axis=0, keepdims=True))
        sims = (X[mem] @ centroid.T).ravel()
        order = sorted(range(len(mem)), key=lambda j: (-float(sims[j]), keep_ids[mem[j]]))
        clusters[str(c)] = {
            "size": int(len(mem)),
            "member_atom_ids": sorted(keep_ids[r] for r in mem),
            "centroid_atom_ids": [keep_ids[mem[j]] for j in order[:CENTROID_IDS]],
        }

    assignments = dict(sorted({keep_ids[r]: int(labels[r]) for r in range(len(keep_ids))}.items()))
    params = {
        "sha256_of_atoms_jsonl": sha,
        "sha256_of_corpus_jsonl": corpus_sha,
        "min_cluster_size": min_cluster_size,
        "min_samples": min_samples,
        "reduce": reduce,
        "components": components,
        "umap_neighbors": umap_neighbors,
        "reassign_cosine": reassign_cosine,
        "seed": seed,
        "atoms_clustered": len(keep_ids),
        "atoms_dropped": len(drops),
    }
    write_json(os.path.join(d, "clusters.json"), {"params": params, "assignments": assignments, "clusters": clusters})
    write_jsonl(os.path.join(d, "clusters_to_name.jsonl"), [
        {
            "cluster_id": c,
            "size": clusters[str(c)]["size"],
            "atoms": [atoms[a]["text"] for a in clusters[str(c)]["centroid_atom_ids"]],
        }
        for c in cids
    ])

    print(f"clusters: {len(cids)}")
    print(f"noise before reassignment: {noise_before}")
    print(f"noise after reassignment: {noise_after}")
    print(f"reassigned: {noise_before - noise_after} (cosine >= {reassign_cosine})")
    print(f"size histogram: {size_histogram([clusters[str(c)]['size'] for c in cids])}")


# ---- finalize -----------------------------------------------------------------

def load_names(d):
    names = {}
    for n, r in enumerate(read_jsonl(os.path.join(d, "cluster_names.jsonl")), 1):
        where = f"cluster_names.jsonl row {n}"
        cid = get(r, "cluster_id", where)
        name = get(r, "name", where)
        get(r, "rationale", where)
        coherence = get(r, "coherence", where)
        if type(cid) is not int:
            fail(f"{where}: cluster_id must be an integer")
        if not isinstance(name, str) or not name.strip():
            fail(f"{where}: empty name for cluster {cid}")
        if coherence not in COHERENCE_LEVELS:
            fail(f"{where}: coherence must be one of {', '.join(COHERENCE_LEVELS)}")
        if cid in names:
            fail(f"cluster_names.jsonl: duplicate cluster_id {cid}")
        names[cid] = r
    return names


def finalize(d):
    clusters_doc = load_json(os.path.join(d, "clusters.json"))
    params = get(clusters_doc, "params", "clusters.json")
    assignments = get(clusters_doc, "assignments", "clusters.json")
    clusters = get(clusters_doc, "clusters", "clusters.json")
    if get(params, "sha256_of_atoms_jsonl", "clusters.json params") != sha256_file(os.path.join(d, "atoms.jsonl")):
        fail("clusters.json was built from a different atoms.jsonl; run cluster again")
    if get(params, "sha256_of_corpus_jsonl", "clusters.json params") != sha256_file(os.path.join(d, "corpus.jsonl")):
        fail("clusters.json was built from a different corpus.jsonl; run cluster again")

    atoms = {a["atom_id"]: a for a in load_atoms(d)}
    corpus = load_corpus(d)
    actors = load_actors(d)
    actor_ids = {a["id"] for a in actors}
    for art in corpus:
        if not isinstance(art["actor_ids"], list) or not art["actor_ids"]:
            fail(f"corpus.jsonl artifact {art['id']}: actor_ids must be a non-empty list")
        for aid in art["actor_ids"]:
            if aid not in actor_ids:
                fail(f"corpus.jsonl artifact {art['id']} references actor {aid}, not in actors.jsonl")

    names = load_names(d)
    cids = sorted(int(k) for k in clusters)
    missing = [c for c in cids if c not in names]
    if missing:
        fail("cluster_names.jsonl has no name for cluster ids: " + ", ".join(str(c) for c in missing))
    unknown = sorted(c for c in names if c not in set(cids))
    if unknown:
        fail("cluster_names.jsonl names ids that are not non-noise clusters in clusters.json: " + ", ".join(str(c) for c in unknown))

    vocab_atoms = {}
    for c in cids:
        entry = clusters[str(c)]
        vocab_atoms[str(c)] = {
            "name": names[c]["name"],
            "size": get(entry, "size", f"clusters.json cluster {c}"),
            "sample_atoms": [atoms[a]["text"] for a in get(entry, "centroid_atom_ids", f"clusters.json cluster {c}")[:SAMPLE_ATOMS]],
            "coherence": names[c]["coherence"],
        }

    by_artifact = {}
    for aid, cid in assignments.items():
        if cid == -1:
            continue
        by_artifact.setdefault(atoms[aid]["artifact_id"], set()).add(cid)
    corpus_ids = {r["id"] for r in corpus}
    stale = sorted(a for a in by_artifact if a not in corpus_ids)
    if stale:
        fail("clusters.json references artifacts not in corpus.jsonl: " + ", ".join(stale))

    drops = []
    artifact_atoms = {}
    for art in sorted(corpus_ids):
        if art in by_artifact:
            artifact_atoms[art] = sorted(by_artifact[art])
        else:
            drops.append((f"artifact:{art}", "no clustered (non-noise) atoms"))

    actor_artifacts = {a["id"]: [] for a in actors}
    for art in corpus:
        for aid in art["actor_ids"]:
            actor_artifacts[aid].append(art["id"])
    repertoires = {}
    for actor in actors:
        aid = actor["id"]
        arts = sorted(actor_artifacts[aid])
        repertoire_atoms = sorted({c for art in arts for c in artifact_atoms.get(art, [])})
        if not repertoire_atoms:
            drops.append((f"actor:{aid}", "no atoms across its artifacts"))
            continue
        repertoires[aid] = {"name": actor["name"], "atoms": repertoire_atoms, "artifact_ids": arts}

    log_stage(d, "finalize", drops)
    write_json(os.path.join(d, "vocab.json"), {"atoms": vocab_atoms, "artifact_atoms": artifact_atoms})
    write_json(os.path.join(d, "repertoires.json"), repertoires)
    print(f"finalize: {len(vocab_atoms)} named clusters, {len(artifact_atoms)} artifacts, {len(repertoires)} actors with repertoires")


# ---- entry point --------------------------------------------------------------

def build_parser():
    p = argparse.ArgumentParser(prog="alien_cluster.py")
    sub = p.add_subparsers(dest="cmd", required=True)

    e = sub.add_parser("embed", help="embed atoms.jsonl texts")
    e.add_argument("--dir", required=True)
    e.add_argument("--embed-url", required=True)
    e.add_argument("--embed-model", required=True)
    e.add_argument("--batch", type=int, default=32)

    c = sub.add_parser("cluster", help="HDBSCAN over embeddings")
    c.add_argument("--dir", required=True)
    c.add_argument("--min-cluster-size", type=int, default=4)
    c.add_argument("--min-samples", type=int, default=2)
    c.add_argument("--reduce", choices=("none", "pca", "umap"), default="pca")
    c.add_argument("--components", type=int, default=50)
    c.add_argument("--umap-neighbors", type=int, default=15)
    c.add_argument("--reassign-cosine", type=float, default=0.65)
    c.add_argument("--seed", type=int, default=42)

    f = sub.add_parser("finalize", help="join cluster names into vocab and repertoires")
    f.add_argument("--dir", required=True)
    return p


def main(argv=None):
    args = build_parser().parse_args(argv)
    try:
        if not os.path.isdir(args.dir):
            fail(f"--dir is not a directory: {args.dir}")
        if args.cmd == "embed":
            embed(args.dir, args.embed_url, args.embed_model, args.batch)
        elif args.cmd == "cluster":
            cluster(args.dir, args.min_cluster_size, args.min_samples, args.reduce,
                    args.components, args.umap_neighbors, args.reassign_cosine, args.seed)
        else:
            finalize(args.dir)
    except AlienError as err:
        print(f"alien_cluster: error: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
