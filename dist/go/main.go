// blitzpatch: dependency-free Go port of dist/apply_patch.py for .bpz patches.
//
// Usage:
//
//	blitzpatch apply <in.raw> <patch.bpz> (-o out.raw | --in-place) [--skip-input-check]
//	blitzpatch verify-chd <file.chd> <expected-data-sha1> [--chdman path]
//	blitzpatch info <patch.bpz>
//
// Exit codes: 0 success; 1 verification failure (input hash/size mismatch, result mismatch,
// CHD Data SHA1 mismatch); 2 usage error; 3 I/O or corrupt/unsupported patch; 4 chdman missing/failed.
package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	hdrSize = 8 + 4 + 8 + 20 + 20 + 8 + 20 + 16 + 20 + 4 // 128
	recSize = 8 + 4 + 4 + 1                              // 17
	chunk   = 4 * 1024 * 1024
)

var magic = []byte("BZ27PAT\x00")

type header struct {
	version    uint32
	psize      uint64
	psha, pchd [20]byte
	tsize      uint64
	tsha       [20]byte
	tmd5       [16]byte
	tchd       [20]byte
	count      uint32
}

type exitErr struct {
	code int
	msg  string
}

func fail(code int, f string, a ...interface{}) *exitErr { return &exitErr{code, fmt.Sprintf(f, a...)} }

func parseHeader(b []byte) (*header, *exitErr) {
	if len(b) < hdrSize {
		return nil, fail(3, "patch file truncated / not a .bpz")
	}
	if !bytes.Equal(b[:8], magic) {
		return nil, fail(3, "not a blitz2027 .bpz patch (bad magic)")
	}
	le := binary.LittleEndian
	h := &header{}
	h.version = le.Uint32(b[8:])
	if h.version != 1 {
		return nil, fail(3, "unsupported patch version %d", h.version)
	}
	h.psize = le.Uint64(b[12:])
	copy(h.psha[:], b[20:40])
	copy(h.pchd[:], b[40:60])
	h.tsize = le.Uint64(b[60:])
	copy(h.tsha[:], b[68:88])
	copy(h.tmd5[:], b[88:104])
	copy(h.tchd[:], b[104:124])
	h.count = le.Uint32(b[124:])
	return h, nil
}

type progress struct {
	label string
	total int64
	last  time.Time
}

func (p *progress) show(done int64, force bool) {
	if !force && time.Since(p.last) < 500*time.Millisecond {
		return
	}
	p.last = time.Now()
	fmt.Fprintf(os.Stderr, "\r%s: %5.1f%%", p.label, float64(done)/float64(p.total)*100)
}

func hashFile(path, label string, hs ...hash.Hash) *exitErr {
	f, err := os.Open(path)
	if err != nil {
		return fail(3, "%v", err)
	}
	defer f.Close()
	st, _ := f.Stat()
	p := &progress{label: label, total: st.Size()}
	buf := make([]byte, chunk)
	var done int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			for _, h := range hs {
				h.Write(buf[:n])
			}
			done += int64(n)
			p.show(done, false)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(3, "read %s: %v", path, err)
		}
	}
	p.show(done, true)
	fmt.Fprintln(os.Stderr)
	return nil
}

func copyFile(src, dst string) *exitErr {
	in, err := os.Open(src)
	if err != nil {
		return fail(3, "%v", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return fail(3, "%v", err)
	}
	st, _ := in.Stat()
	p := &progress{label: "copying input -> output", total: st.Size()}
	buf := make([]byte, chunk)
	var done int64
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return fail(3, "write %s: %v", dst, werr)
			}
			done += int64(n)
			p.show(done, false)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return fail(3, "read: %v", rerr)
		}
	}
	p.show(done, true)
	fmt.Fprintln(os.Stderr)
	if err := out.Close(); err != nil {
		return fail(3, "%v", err)
	}
	return nil
}

func apply(args []string) *exitErr {
	var pos []string
	var outPath string
	inPlace, skip := false, false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-o", "--out":
			i++
			if i >= len(args) {
				return fail(2, "-o needs a path")
			}
			outPath = args[i]
		case "--in-place":
			inPlace = true
		case "--skip-input-check", "--skip-pristine-check":
			skip = true
		default:
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				return fail(2, "unknown option %s", a)
			}
			pos = append(pos, a)
		}
	}
	if len(pos) != 2 || (outPath == "") == !inPlace {
		return fail(2, "usage: blitzpatch apply <in.raw> <patch.bpz> (-o out.raw | --in-place)")
	}
	t0 := time.Now()
	inRaw, patch := pos[0], pos[1]
	st, err := os.Stat(inRaw)
	if err != nil || st.IsDir() {
		return fail(3, "not found: %s", inRaw)
	}
	pf, err := os.Open(patch)
	if err != nil {
		return fail(3, "%v", err)
	}
	defer pf.Close()
	pr := bufio.NewReaderSize(pf, 1<<20)
	hb := make([]byte, hdrSize)
	if _, err := io.ReadFull(pr, hb); err != nil {
		return fail(3, "patch file truncated / not a .bpz")
	}
	h, e := parseHeader(hb)
	if e != nil {
		return e
	}
	if uint64(st.Size()) != h.psize {
		return fail(1, "input raw is %d bytes, expected %d. Extract with `chdman extracthd` from the original blitz2k.chd.", st.Size(), h.psize)
	}
	if !skip {
		s := sha1.New()
		if e := hashFile(inRaw, "verifying input", s); e != nil {
			return e
		}
		if !bytes.Equal(s.Sum(nil), h.psha[:]) {
			return fail(1, "input SHA1 mismatch\n  got      %x\n  expected %x\nYour image is not the unmodified original (or is already patched). Nothing was changed.", s.Sum(nil), h.psha[:])
		}
	}
	target := inRaw
	if !inPlace {
		a1, _ := filepath.Abs(outPath)
		a2, _ := filepath.Abs(inRaw)
		if a1 == a2 {
			return fail(2, "use --in-place to overwrite the input")
		}
		if e := copyFile(inRaw, outPath); e != nil {
			return e
		}
		target = outPath
	}
	out, err := os.OpenFile(target, os.O_RDWR, 0)
	if err != nil {
		return fail(3, "%v", err)
	}
	le := binary.LittleEndian
	rb := make([]byte, recSize)
	for n := uint32(0); n < h.count; n++ {
		if _, err := io.ReadFull(pr, rb); err != nil {
			out.Close()
			return fail(3, "patch truncated")
		}
		off := le.Uint64(rb[0:])
		rawlen := le.Uint32(rb[8:])
		clen := le.Uint32(rb[12:])
		codec := rb[16]
		cdata := make([]byte, clen)
		if _, err := io.ReadFull(pr, cdata); err != nil {
			out.Close()
			return fail(3, "patch truncated")
		}
		var data []byte
		switch codec {
		case 0:
			data = cdata
		case 1:
			zr, err := zlib.NewReader(bytes.NewReader(cdata))
			if err != nil {
				out.Close()
				return fail(3, "corrupt record %d: %v", n, err)
			}
			data, err = io.ReadAll(zr)
			if err != nil {
				out.Close()
				return fail(3, "corrupt record %d: %v", n, err)
			}
		case 2:
			out.Close()
			return fail(3, "LZMA records are not supported by blitzpatch (build the patch with zlib/stored only)")
		default:
			out.Close()
			return fail(3, "unknown codec %d", codec)
		}
		if uint32(len(data)) != rawlen || off+uint64(rawlen) > h.tsize {
			out.Close()
			return fail(3, "corrupt record %d", n)
		}
		if _, err := out.WriteAt(data, int64(off)); err != nil {
			out.Close()
			return fail(3, "write: %v", err)
		}
		if n%2000 == 0 {
			fmt.Fprintf(os.Stderr, "\rapplying: %d/%d", n, h.count)
		}
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return fail(3, "fsync: %v", err)
	}
	out.Close()
	fmt.Fprintf(os.Stderr, "\rapplied %d records\n", h.count)
	s, m := sha1.New(), md5.New()
	if e := hashFile(target, "verifying result", s, m); e != nil {
		return e
	}
	if !bytes.Equal(s.Sum(nil), h.tsha[:]) {
		return fail(1, "result SHA1 mismatch\n  got      %x\n  expected %x\nOutput is NOT valid; discard it and re-extract the original raw.", s.Sum(nil), h.tsha[:])
	}
	if !bytes.Equal(m.Sum(nil), h.tmd5[:]) {
		return fail(1, "result MD5 mismatch\n  got      %x\n  expected %x\nOutput is NOT valid.", m.Sum(nil), h.tmd5[:])
	}
	fmt.Printf("OK  patched raw SHA1 %x  (%.0fs)\n", s.Sum(nil), time.Since(t0).Seconds())
	fmt.Printf("Expected chdman 'Data SHA1' of the rebuilt CHD: %x\n", h.tchd[:])
	fmt.Printf("Target raw MD5: %x\n", h.tmd5[:])
	return nil
}

func info(args []string) *exitErr {
	if len(args) != 1 {
		return fail(2, "usage: blitzpatch info <patch.bpz>")
	}
	f, err := os.Open(args[0])
	if err != nil {
		return fail(3, "%v", err)
	}
	defer f.Close()
	b := make([]byte, hdrSize)
	if _, err := io.ReadFull(f, b); err != nil {
		return fail(3, "patch file truncated / not a .bpz")
	}
	h, e := parseHeader(b)
	if e != nil {
		return e
	}
	fmt.Printf("input size %d\ninput sha1 %x\ninput chd data sha1 %x\ntarget size %d\ntarget sha1 %x\ntarget md5 %x\ntarget chd data sha1 %x\nrecords %d\n",
		h.psize, h.psha[:], h.pchd[:], h.tsize, h.tsha[:], h.tmd5[:], h.tchd[:], h.count)
	return nil
}

func verifyCHD(args []string) *exitErr {
	chdman := "chdman"
	var pos []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--chdman" {
			i++
			if i >= len(args) {
				return fail(2, "--chdman needs a path")
			}
			chdman = args[i]
		} else {
			pos = append(pos, args[i])
		}
	}
	if len(pos) != 2 {
		return fail(2, "usage: blitzpatch verify-chd <file.chd> <expected-data-sha1|patch.bpz> [--chdman path]")
	}
	want := strings.ToLower(pos[1])
	if len(want) != 40 {
		// treat as a .bpz: use its target CHD data SHA1
		b, err := os.ReadFile(pos[1])
		if err != nil {
			return fail(2, "expected must be 40 hex chars or a .bpz path: %v", err)
		}
		h, e := parseHeader(b)
		if e != nil {
			return e
		}
		want = hex.EncodeToString(h.tchd[:])
	}
	cmd := exec.Command(chdman, "info", "-i", pos[0])
	var so bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fail(4, "chdman failed: %v", err)
	}
	got := ""
	for _, ln := range strings.Split(so.String(), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "Data SHA1:") {
			got = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ln, "Data SHA1:")))
		}
	}
	if got == "" {
		return fail(4, "no 'Data SHA1:' line in chdman output")
	}
	fmt.Printf("expected Data SHA1 %s\ngot      Data SHA1 %s\n", want, got)
	if got != want {
		return fail(1, "MISMATCH - do not use %s", pos[0])
	}
	fmt.Println("PASS")
	return nil
}

var version = "1.0"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "blitzpatch "+version+"\nusage: blitzpatch run|apply|verify-chd|info ...\nexit codes: 0 ok, 1 verification failed, 2 usage, 3 I/O or bad patch, 4 chdman problem")
		os.Exit(2)
	}
	var e *exitErr
	switch os.Args[1] {
	case "apply":
		e = apply(os.Args[2:])
	case "verify-chd":
		e = verifyCHD(os.Args[2:])
	case "info":
		e = info(os.Args[2:])
	case "run":
		e = run(os.Args[2:])
	default:
		e = fail(2, "unknown command %q (run | apply | verify-chd | info)", os.Args[1])
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, "ERROR: "+e.msg)
		os.Exit(e.code)
	}
}
