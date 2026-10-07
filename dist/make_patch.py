#!/usr/bin/env python3
"""make_patch.py PRISTINE.raw TARGET.raw -o blitz2027.bpz  (stdlib only, streaming)

Builds a sparse patch of differing byte ranges (coalesced across gaps < --gap
bytes). Never holds more than a few MB plus one pending range in RAM.
"""
import argparse, hashlib, json, lzma, os, struct, sys, time, zlib
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from bpz_format import *

MAX_REC = 16 * 1024 * 1024
BLK = 4096

def compress(data, codec, level):
    if codec == "lzma":
        c, cid = lzma.compress(data, preset=level), CODEC_LZMA
    else:
        c, cid = zlib.compress(data, level), CODEC_ZLIB
    if len(c) >= len(data):
        return data, CODEC_STORED
    return c, cid

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("pristine"); ap.add_argument("target")
    ap.add_argument("-o", "--out", required=True)
    ap.add_argument("--gap", type=int, default=64)
    ap.add_argument("--codec", choices=["zlib", "lzma"], default="zlib")
    ap.add_argument("--level", type=int, default=9)
    ap.add_argument("--pristine-chd-data-sha1", help="from `chdman info` (default: raw sha1)")
    ap.add_argument("--target-chd-data-sha1", help="from `chdman info` (default: raw sha1)")
    ap.add_argument("--manifest", help="write JSON manifest of ranges + regional summary")
    a = ap.parse_args()

    ps, ts = os.path.getsize(a.pristine), os.path.getsize(a.target)
    if ps != ts:
        sys.exit(f"size mismatch: pristine {ps} vs target {ts} (patch format is same-size only)")
    ph, th, tm = hashlib.sha1(), hashlib.sha1(), hashlib.md5()
    out = open(a.out, "wb")
    out.write(b"\0" * HDR.size)
    ranges = []           # (offset, length) for manifest
    state = {"count": 0, "comp": 0}
    cur = None            # [start, bytearray]

    def flush():
        nonlocal cur
        if cur is None: return
        start, buf = cur
        for o in range(0, len(buf), MAX_REC):
            piece = bytes(buf[o:o + MAX_REC])
            c, cid = compress(piece, a.codec, a.level)
            out.write(REC.pack(start + o, len(piece), len(c), cid)); out.write(c)
            state["count"] += 1; state["comp"] += len(c)
            ranges.append((start + o, len(piece)))
        cur = None

    def add_run(off, data, tchunk, cbase):
        """off: abs offset of first differing byte; data: differing-span bytes
        (from off, target); gap fill requires target bytes between runs, which
        we take from the current chunk when in range."""
        nonlocal cur
        if cur is not None:
            end = cur[0] + len(cur[1])
            gap = off - end
            if gap < a.gap:
                if gap > 0:
                    lo = end - cbase
                    cur[1] += tchunk[lo:lo + gap] if lo >= 0 else fill_prev(end, gap)
                cur[1] += data
                return
            flush()
        cur = [off, bytearray(data)]

    tf = open(a.target, "rb")
    def fill_prev(end, gap):
        pos = tf.tell(); tf.seek(end); b = tf.read(gap); tf.seek(pos); return b

    t0 = time.time(); off = 0
    with open(a.pristine, "rb") as fp, tf:
        while True:
            pc = fp.read(CHUNK); tc = tf.read(CHUNK)
            if not pc and not tc: break
            if len(pc) != len(tc): sys.exit("short read")
            ph.update(pc); th.update(tc); tm.update(tc)
            if pc != tc:
                for b in range(0, len(pc), BLK):
                    pb, tb = pc[b:b + BLK], tc[b:b + BLK]
                    if pb == tb: continue
                    idx = [i for i in range(len(pb)) if pb[i] != tb[i]]
                    # split into runs separated by >= gap unchanged bytes
                    s = e = idx[0]
                    for i in idx[1:]:
                        if i - e - 1 >= a.gap:
                            add_run(off + b + s, tb[s:e + 1], tc, off)
                            s = i
                        e = i
                    add_run(off + b + s, tb[s:e + 1], tc, off)
            off += len(pc)
            if (off // CHUNK) % 256 == 0:
                print(f"\r{off/ps*100:5.1f}%  records={state['count']}", end="", file=sys.stderr)
    flush()
    print(file=sys.stderr)
    pchd = bytes.fromhex(a.pristine_chd_data_sha1) if a.pristine_chd_data_sha1 else ph.digest()
    tchd = bytes.fromhex(a.target_chd_data_sha1) if a.target_chd_data_sha1 else th.digest()
    out.seek(0)
    out.write(HDR.pack(MAGIC, VERSION, ps, ph.digest(), pchd, ts, th.digest(), tm.digest(), tchd, state["count"]))
    out.close()
    changed = sum(l for _, l in ranges)
    print(f"pristine sha1 {ph.hexdigest()}\ntarget   sha1 {th.hexdigest()}\ntarget   md5  {tm.hexdigest()}")
    print(f"records {state['count']}  changed bytes {changed}  patch size {os.path.getsize(a.out)} "
          f"({os.path.getsize(a.out)/1e6:.1f} MB)  time {time.time()-t0:.0f}s")
    if a.manifest:
        # regional summary: cluster ranges separated by < 1 MiB
        cl = []
        for o, l in ranges:
            if cl and o - cl[-1][1] < (1 << 20): cl[-1][1] = o + l; cl[-1][2] += l; cl[-1][3] += 1
            else: cl.append([o, o + l, l, 1])
        json.dump({"pristine_sha1": ph.hexdigest(), "target_sha1": th.hexdigest(),
                   "records": state["count"], "changed_bytes": changed,
                   "clusters": [{"start": s, "end": e, "changed": c, "records": n} for s, e, c, n in cl],
                   "ranges": ranges}, open(a.manifest, "w"))

if __name__ == "__main__":
    main()
