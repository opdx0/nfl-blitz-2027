#!/usr/bin/env python3
"""apply_patch.py PRISTINE.raw blitz2027.bpz -o OUT.raw   (or --in-place)

Python 3.8+, stdlib only. Verifies the pristine SHA1 (streaming) before
touching anything, applies the patch, then verifies the target SHA1.
"""
import argparse, hashlib, lzma, os, shutil, struct, sys, time, zlib
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from bpz_format import *

def sha1_file(path, label):
    h = hashlib.sha1(); size = os.path.getsize(path); done = 0
    with open(path, "rb") as f:
        while True:
            b = f.read(CHUNK)
            if not b: break
            h.update(b); done += len(b)
            print(f"\r{label}: {done/size*100:5.1f}%", end="", file=sys.stderr)
    print(file=sys.stderr)
    return h.digest()

def die(msg):
    sys.exit("ERROR: " + msg)

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("pristine_raw"); ap.add_argument("patch")
    g = ap.add_mutually_exclusive_group(required=True)
    g.add_argument("-o", "--out", help="write patched raw to this NEW file (pristine untouched)")
    g.add_argument("--in-place", action="store_true", help="modify pristine_raw directly (needs no extra disk; a failure mid-way corrupts it)")
    ap.add_argument("--skip-pristine-check", action="store_true")
    a = ap.parse_args()
    t0 = time.time()

    if not os.path.isfile(a.pristine_raw): die(f"not found: {a.pristine_raw}")
    with open(a.patch, "rb") as pf:
        raw = pf.read(HDR.size)
        if len(raw) < HDR.size: die("patch file truncated / not a .bpz")
        (magic, ver, psize, psha, pchd, tsize, tsha, tmd5, tchd, count) = HDR.unpack(raw)
        if magic != MAGIC: die("not a blitz2027 .bpz patch (bad magic)")
        if ver != VERSION: die(f"unsupported patch version {ver}")
        size = os.path.getsize(a.pristine_raw)
        if size != psize:
            die(f"pristine raw is {size} bytes, expected {psize}. Extract with `chdman extracthd` from the original blitz2k.chd.")
        if not a.skip_pristine_check:
            got = sha1_file(a.pristine_raw, "verifying pristine")
            if got != psha:
                die(f"pristine SHA1 mismatch\n  got      {got.hex()}\n  expected {psha.hex()}\n"
                    "Your image is not the unmodified original (or is already patched). Nothing was changed.")
        if a.in_place:
            target = a.pristine_raw
        else:
            if os.path.abspath(a.out) == os.path.abspath(a.pristine_raw): die("use --in-place to overwrite the input")
            print("copying pristine -> output ...", file=sys.stderr)
            shutil.copyfile(a.pristine_raw, a.out); target = a.out
        with open(target, "r+b") as out:
            for n in range(count):
                hdr = pf.read(REC.size)
                if len(hdr) < REC.size: die("patch truncated")
                off, rawlen, clen, codec = REC.unpack(hdr)
                data = pf.read(clen)
                if len(data) != clen: die("patch truncated")
                if codec == CODEC_ZLIB: data = zlib.decompress(data)
                elif codec == CODEC_LZMA: data = lzma.decompress(data)
                elif codec != CODEC_STORED: die(f"unknown codec {codec}")
                if len(data) != rawlen or off + rawlen > tsize: die("corrupt record")
                out.seek(off); out.write(data)
                if n % 2000 == 0: print(f"\rapplying: {n}/{count}", end="", file=sys.stderr)
            out.flush(); os.fsync(out.fileno())
    print(f"\rapplied {count} records", file=sys.stderr)
    got = sha1_file(target, "verifying result")
    if got != tsha:
        die(f"result SHA1 mismatch\n  got      {got.hex()}\n  expected {tsha.hex()}\nOutput is NOT valid; discard it and re-extract the pristine raw.")
    print(f"OK  patched raw SHA1 {got.hex()}  ({time.time()-t0:.0f}s)")
    print(f"Expected chdman 'Data SHA1' of the rebuilt CHD: {tchd.hex()}")
    print(f"Target raw MD5: {tmd5.hex()}")

if __name__ == "__main__":
    main()
