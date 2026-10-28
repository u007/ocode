#!/usr/bin/env python3
"""Read-only glTF/GLB inspector for AAA asset review.

Parses the file as data only. Never executes anything from the asset and never
fetches URLs found inside it. Exits non-zero with a message on malformed input.

Usage: python3 -I inspect_gltf.py <asset.glb|asset.gltf>
"""
import base64
import json
import struct
import sys
from pathlib import Path
from urllib.parse import unquote

GLB_MAGIC = 0x46546C67
CHUNK_JSON = 0x4E4F534A
CHUNK_BIN = 0x004E4942
MODE_POINTS = 0
MODE_TRIANGLES, MODE_STRIP, MODE_FAN = 4, 5, 6
KTX2_ID = b"\xabKTX 20\xbb\r\n\x1a\n"
SUSPICIOUS = ("ignore previous", "ignore all", "system prompt", "you are an ai",
              "mark as pass", "approve this", "disregard")


def fail(msg):
    sys.exit(f"error: {msg}")


def load(path):
    data = path.read_bytes()
    if data[:4] == b"glTF":
        magic, version, length = struct.unpack_from("<III", data, 0)
        if magic != GLB_MAGIC or version != 2 or length > len(data):
            fail("not a valid GLB 2.0 container")
        doc, binary, off = None, None, 12
        while off + 8 <= length:
            clen, ctype = struct.unpack_from("<II", data, off)
            body = data[off + 8: off + 8 + clen]
            if len(body) != clen:
                fail("truncated GLB chunk")
            if ctype == CHUNK_JSON:
                doc = json.loads(body.decode("utf-8"))
            elif ctype == CHUNK_BIN:
                binary = body
            off += 8 + clen + (-clen % 4)
        if doc is None:
            fail("GLB has no JSON chunk")
        return doc, binary
    return json.loads(data.decode("utf-8")), None


def read_uri(uri, base):
    """Resolve a glTF URI to bytes. Remote URIs are never fetched."""
    if uri.startswith("data:"):
        return base64.b64decode(uri.split(",", 1)[1])
    if "://" in uri:
        raise ValueError(f"remote URI not fetched: {uri[:60]!r}")
    target = (base / unquote(uri)).resolve()
    if base.resolve() not in target.parents:
        raise ValueError(f"uri escapes asset directory: {uri!r}")
    return target.read_bytes()


def image_bytes(doc, binary, img, base):
    if "bufferView" in img:
        bv = doc["bufferViews"][img["bufferView"]]
        buf = doc["buffers"][bv["buffer"]]
        if "uri" in buf:
            data = read_uri(buf["uri"], base)
        elif binary is not None:
            data = binary
        else:
            raise ValueError("bufferView references a missing BIN chunk")
        start = bv.get("byteOffset", 0)
        return data[start: start + bv["byteLength"]]
    return read_uri(img["uri"], base)


def dimensions(raw):
    if raw[:8] == b"\x89PNG\r\n\x1a\n":
        w, h = struct.unpack(">II", raw[16:24])
        return "png", w, h
    if raw[:2] == b"\xff\xd8":
        i = 2
        while i + 9 < len(raw):
            if raw[i] != 0xFF:
                i += 1
                continue
            marker = raw[i + 1]
            if 0xC0 <= marker <= 0xCF and marker not in (0xC4, 0xC8, 0xCC):
                h, w = struct.unpack(">HH", raw[i + 5: i + 9])
                return "jpeg", w, h
            i += 2 + struct.unpack(">H", raw[i + 2: i + 4])[0]
        fail("JPEG without SOF marker")
    if raw[:12] == KTX2_ID:
        w, h = struct.unpack("<II", raw[20:28])
        return "ktx2", w, h
    return "unknown", 0, 0


def pot(n):
    return n > 0 and n & (n - 1) == 0


def main():
    if len(sys.argv) != 2:
        fail("usage: inspect_gltf.py <asset.glb|asset.gltf>")
    path = Path(sys.argv[1])
    if not path.is_file():
        fail(f"no such file: {path}")
    doc, binary = load(path)
    acc = doc.get("accessors", [])

    print(f"FILE {path.name}")
    print(f"generator: {doc.get('asset', {}).get('generator', '?')}")
    print(f"extensions used: {', '.join(doc.get('extensionsUsed', [])) or 'none'}")

    total_tris = total_verts = 0
    print("\nMESHES")
    for mi, mesh in enumerate(doc.get("meshes", [])):
        tris = verts = non_tri = 0
        uv_sets = 0
        for prim in mesh.get("primitives", []):
            attrs = prim.get("attributes", {})
            verts += acc[attrs["POSITION"]]["count"] if "POSITION" in attrs else 0
            mode = prim.get("mode", MODE_TRIANGLES)
            n = acc[prim["indices"]]["count"] if "indices" in prim else \
                acc[attrs["POSITION"]]["count"]
            if mode == MODE_TRIANGLES:
                tris += n // 3
            elif mode in (MODE_STRIP, MODE_FAN):
                tris += max(n - 2, 0)
            else:
                non_tri += 1
            if "JOINTS_1" in attrs or "WEIGHTS_1" in attrs:
                print(f"  ! mesh {mi}: >4 bone influences per vertex (JOINTS_1/WEIGHTS_1)")
            uv_sets = max(uv_sets, sum(k.startswith("TEXCOORD_") for k in attrs))
            if "NORMAL" not in attrs:
                print(f"  ! mesh {mi}: primitive without NORMAL")
            if "TANGENT" not in attrs and prim.get("material") is not None:
                print(f"  - mesh {mi}: no TANGENT (engine must generate MikkTSpace)")
        total_tris += tris
        total_verts += verts
        print(f"  [{mi}] {mesh.get('name', '<unnamed>')}: {tris} tris, {verts} verts, "
              f"{len(mesh.get('primitives', []))} prims, {uv_sets} UV set(s)"
              + (f", {non_tri} point/line prim(s) not counted" if non_tri else ""))
    print(f"TOTAL {total_tris} tris, {total_verts} verts")

    print("\nMATERIALS")
    for i, m in enumerate(doc.get("materials", [])):
        ext = ",".join(m.get("extensions", {}).keys()) or "-"
        pbr = m.get("pbrMetallicRoughness", {})
        print(f"  [{i}] {m.get('name', '<unnamed>')}: alpha={m.get('alphaMode', 'OPAQUE')} "
              f"doubleSided={m.get('doubleSided', False)} "
              f"metallic={pbr.get('metallicFactor', 1)} roughness={pbr.get('roughnessFactor', 1)} "
              f"ext={ext}")

    print("\nTEXTURES")
    base = path.parent
    for i, img in enumerate(doc.get("images", [])):
        name = img.get("name", img.get("uri", "<embedded>")[:40])
        try:
            fmt, w, h = dimensions(image_bytes(doc, binary, img, base))
        except (OSError, ValueError, KeyError, IndexError, struct.error) as exc:
            print(f"  [{i}] {name}: ERROR {type(exc).__name__}: {exc}")
            continue
        if fmt == "unknown":
            print(f"  [{i}] {name}: unknown format, dimensions not read")
            continue
        flag = "" if pot(w) and pot(h) else "  ! non power-of-two"
        print(f"  [{i}] {name}: {fmt} {w}x{h}{flag}")

    skins = doc.get("skins", [])
    print(f"\nSKINS {len(skins)}: " + ", ".join(str(len(s['joints'])) + " joints" for s in skins))
    print(f"ANIMATIONS {len(doc.get('animations', []))}")
    print(f"NODES {len(doc.get('nodes', []))}  SCENES {len(doc.get('scenes', []))}")

    flagged = []
    def scan(label, value):
        if isinstance(value, str):
            low = value.lower()
            if any(s in low for s in SUSPICIOUS):
                flagged.append((label, value[:120]))
        elif isinstance(value, dict):
            for k, v in value.items():
                scan(f"{label}.{k}", v)
        elif isinstance(value, list):
            for i, v in enumerate(value):
                scan(f"{label}[{i}]", v)
    scan("json", doc)
    print("\nSUSPECTED INJECTION")
    if flagged:
        for where, text in flagged:
            print(f"  {where}: {text!r}")
    else:
        print("  none")


if __name__ == "__main__":
    main()
