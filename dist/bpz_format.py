"""Shared constants for the blitz2027.bpz sparse patch format (stdlib only).

Layout (little endian):
  magic 8s  b'BZ27PAT\\0'
  version u32 (=1)
  pristine_size u64
  pristine_sha1 20s        sha1 of pristine raw (== chdman "Data SHA1")
  pristine_chd_data_sha1 20s
  target_size u64
  target_sha1 20s
  target_md5 16s
  target_chd_data_sha1 20s
  count u32
  then `count` records:
    offset u64, rawlen u32, complen u32, codec u8, payload[complen]
  codec 0 = stored, 1 = zlib, 2 = lzma (raw bytes replace target[offset:offset+rawlen])
"""
import struct
MAGIC = b"BZ27PAT\0"
VERSION = 1
HDR = struct.Struct("<8sIQ20s20sQ20s16s20sI")
REC = struct.Struct("<QIIB")
CODEC_STORED, CODEC_ZLIB, CODEC_LZMA = 0, 1, 2
CHUNK = 4 * 1024 * 1024
