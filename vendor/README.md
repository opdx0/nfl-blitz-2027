# Bundled chdman

chdman is MAME's disk-image tool. The patch downloads bundle it unmodified from **MAME 0.289** so you don't need to install anything. It is a separate program, not linked into the patcher, and stays under its own licence: GPL v2 or later (see [COPYING](COPYING) and [GPL-2.0](GPL-2.0)). Source: https://github.com/mamedev/mame/tree/mame0289

| File | Platform | Origin |
|---|---|---|
| chdman.exe | Windows x64 | Taken from the official mame0289b_x64 release. |
| chdman_macos | macOS, universal (Apple Silicon + Intel) | Built from the mame0289 tag, ad-hoc signed. |
| chdman_linux_x86_64 | Linux x86-64 | Built from the mame0289 tag, statically linked. |
| chdman_linux_arm64 | Linux arm64 | Built from the mame0289 tag, statically linked. |

Checksums: [SHA256SUMS](SHA256SUMS).
