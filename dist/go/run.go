package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func chdmanDataSHA1(chdman, chd string) (string, string, *exitErr) {
	cmd := exec.Command(chdman, "info", "-i", chd)
	var so bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", "", fail(4, "chdman info failed on %s: %v", chd, err)
	}
	data, sha := "", ""
	for _, ln := range strings.Split(so.String(), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "Data SHA1:") {
			data = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ln, "Data SHA1:")))
		} else if strings.HasPrefix(ln, "SHA1:") {
			sha = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ln, "SHA1:")))
		}
	}
	if data == "" {
		return "", "", fail(4, "no 'Data SHA1:' line in chdman output for %s", chd)
	}
	return data, sha, nil
}

func runChdman(chdman string, args ...string) *exitErr {
	cmd := exec.Command(chdman, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fail(4, "chdman %s failed: %v", args[0], err)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

func unq(s string) string {
	s = strings.TrimSpace(s)
	return strings.Trim(s, "\"'")
}

// run: whole pipeline (info -> extracthd -> apply -> createhd -> info -> compare).
// The patched CHD is built as <input>.patching in the input's folder; only after the
// Data SHA1 check passes is the input renamed to <input>.original (never overwriting an
// existing one: .original.1, .original.2, ...) and the new file renamed to the input's name.
// With --out the result is written to that path instead and the input is left untouched.
func run(args []string) *exitErr {
	var chdman, bpz, out, in string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--chdman", "--bpz", "--out", "-o":
			i++
			if i >= len(args) {
				return fail(2, "%s needs a value", a)
			}
			v := unq(args[i])
			switch a {
			case "--chdman":
				chdman = v
			case "--bpz":
				bpz = v
			default:
				out = v
			}
		default:
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				return fail(2, "unknown option %s", a)
			}
			if in != "" {
				return fail(2, "only one input CHD may be given (got %q and %q)", in, a)
			}
			in = unq(a)
		}
	}
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	if chdman == "" {
		name := "chdman"
		if runtime.GOOS == "windows" {
			name = "chdman.exe"
		}
		if p := filepath.Join(exeDir, name); fileExists(p) {
			chdman = p
		} else if p, err := exec.LookPath("chdman"); err == nil {
			chdman = p
		} else {
			return fail(4, "chdman not found beside blitzpatch or on PATH (use --chdman <path>)")
		}
	}
	if bpz == "" {
		m, _ := filepath.Glob(filepath.Join(exeDir, "*.bpz"))
		if len(m) != 1 {
			return fail(2, "expected exactly one .bpz beside blitzpatch, found %d (use --bpz <path>)", len(m))
		}
		bpz = m[0]
	}
	if in == "" {
		in = filepath.Join(exeDir, "blitz2k.chd")
	}
	in, _ = filepath.Abs(in)
	if !fileExists(in) {
		return fail(3, "original CHD not found: %s\nPut your original blitz2k.chd in the kit folder or drag it onto the PATCH script.", in)
	}
	swap := out == ""
	if swap {
		out = in + ".patching"
	}
	out, _ = filepath.Abs(out)
	if out == in {
		return fail(2, "output and input are the same file")
	}
	hb := make([]byte, hdrSize)
	pf, err := os.Open(bpz)
	if err != nil {
		return fail(3, "%v", err)
	}
	n, _ := pf.Read(hb)
	pf.Close()
	h, e := parseHeader(hb[:n])
	if e != nil {
		return e
	}
	wantIn := hex.EncodeToString(h.pchd[:])
	wantOut := hex.EncodeToString(h.tchd[:])

	fmt.Println("== 1/5 checking original CHD")
	got, sha, e := chdmanDataSHA1(chdman, in)
	if e != nil {
		return e
	}
	fmt.Printf("SHA1:      %s\nData SHA1: %s\n", sha, got)
	if got == wantOut {
		fmt.Println("\nThis blitz2k.chd is already patched (Data SHA1 matches NFL Blitz 2027). Nothing was changed. Your original should be beside it as blitz2k.chd.original.")
		return nil
	}
	if got != wantIn {
		return fail(1, "this is not the original blitz2k.chd (Data SHA1 should be %s). Nothing was changed.", wantIn)
	}
	outDir := filepath.Dir(out)
	need := h.psize + (1 << 30)
	free, err := freeBytes(outDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot determine free disk space: %v\n", err)
	} else if free < need {
		return fail(1, "not enough free disk space in %s: %.1f GB free, about %.1f GB needed (temporary 6.4 GB raw image plus the new CHD). Free some space or use --out on another drive.", outDir, float64(free)/1e9, float64(need)/1e9)
	}
	work := filepath.Join(filepath.Dir(in), ".blitz2027_work")
	if filepath.Dir(in) != outDir {
		work = filepath.Join(outDir, ".blitz2027_work")
	}
	os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return fail(3, "cannot create work folder %s: %v", work, err)
	}
	defer os.RemoveAll(work)
	raw := filepath.Join(work, "in.raw")

	fmt.Println("\n== 2/5 extracting raw image (1-5 min depending on your system)")
	if e := runChdman(chdman, "extracthd", "-f", "-i", in, "-o", raw); e != nil {
		return e
	}
	fmt.Println("== 3/5 applying and verifying patch")
	if e := apply([]string{raw, bpz, "--in-place"}); e != nil {
		return e
	}
	fmt.Println("\n== 4/5 building CHD (1-5 min depending on your system)")
	if e := runChdman(chdman, "createhd", "-f", "-i", raw, "-o", out, "-c", "zlib", "-hs", "4096", "-chs", "784,255,63", "-ss", "512"); e != nil {
		os.Remove(out)
		return e
	}
	os.Remove(raw)
	fmt.Println("== 5/5 verifying new CHD")
	got, sha, e = chdmanDataSHA1(chdman, out)
	if e != nil {
		return e
	}
	fmt.Printf("SHA1:      %s\nData SHA1: %s\nexpected   %s\n", sha, got, wantOut)
	if got != wantOut {
		os.Remove(out)
		return fail(1, "Data SHA1 mismatch - output deleted.")
	}
	if !swap {
		fmt.Printf("\nPASS: %s\nNext: copy it to <rompath>/blitz2k/blitz2k.chd (next to your original blitz2k.zip), then: mame blitz2k -rompath <rompath>\n", out)
		return nil
	}
	orig := in + ".original"
	for i := 1; pathExists(orig); i++ {
		orig = fmt.Sprintf("%s.original.%d", in, i)
	}
	if orig != in+".original" {
		fmt.Printf("\n%s.original already exists; it was not touched.\n", in)
	}
	if err := os.Rename(in, orig); err != nil {
		return fail(3, "the patched CHD is VERIFIED and kept as %s\nbut renaming your original failed: %v\nYour original is still %s (unchanged).\nRename them yourself: move %s to a backup name, then rename %s to %s", out, err, in, in, out, in)
	}
	if err := os.Rename(out, in); err != nil {
		return fail(3, "the patched CHD is VERIFIED and kept as %s\nbut renaming it to %s failed: %v\nYour original is now %s\nRename %s to %s yourself (keep the original as a backup).", out, in, err, orig, out, in)
	}
	fmt.Printf("\nPASS: %s\nYour original is kept as %s\nNext: copy %s to <rompath>/blitz2k/%s (next to your original blitz2k.zip), then: mame blitz2k -rompath <rompath>\n", in, orig, filepath.Base(in), filepath.Base(in))
	return nil
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
