// Command verify-build checks downloaded agw-token release files against the
// SHA256SUMS-unsigned file of the same release, which lists the binaries exactly
// as CI built them, before signing. Since Linux and Windows builds are
// reproducible (see scripts/build.sh), those sums are also what anyone gets by
// building the release tag themselves.
//
//	go run ./cmd/verify-build SHA256SUMS-unsigned agw-token-1.2.3-windows-amd64.zip agw-token.exe
//
// Windows binaries are released with an Authenticode signature, which changes
// their hash. verify-build removes the signature (the certificate table at the
// end of the file, its entry in the PE data directory and the PE checksum the
// signing tool fills in) and hashes what is left, which for a Go binary is the
// unsigned build. Everything else the signature covers stays in place, so any
// other change to the file makes the check fail.
//
// Linux binaries are not signed and are hashed as they are. .zip and .tar.gz
// release archives are opened and every agw-token binary in them is checked.
// Files are matched by hash, not by name, so renamed or extracted binaries
// verify too.
//
// macOS binaries cannot be checked this way: they are built with cgo, and the C
// toolchain and SDK make the output differ between machines. verify-build
// reports them as not verified; check them with codesign and gh attestation
// verify instead, as described in DEVELOPMENT.md.
package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const usageText = `Usage: verify-build SHA256SUMS-unsigned FILE...

Checks agw-token binaries, or .zip and .tar.gz release archives, against the
sums of the unsigned CI build, ignoring Windows Authenticode signatures.
Exits 1 if any binary does not match or cannot be verified.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] == "-h" || args[0] == "--help" {
		_, _ = fmt.Fprint(stderr, usageText)
		return 2
	}
	sums, err := readSums(args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "verify-build: %v\n", err)
		return 2
	}
	ok := true
	for _, name := range args[1:] {
		results, err := checkFile(name, sums)
		if err != nil {
			_, _ = fmt.Fprintf(stdout, "%s: ERROR %v\n", name, err)
			ok = false
			continue
		}
		for _, r := range results {
			_, _ = fmt.Fprintln(stdout, r)
			ok = ok && r.ok
		}
	}
	if !ok {
		return 1
	}
	return 0
}

// readSums parses sha256sum/shasum output into a map from hex digest to file name.
func readSums(name string) (map[string]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sums := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		digest, file, found := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !found || len(digest) != sha256.Size*2 {
			continue
		}
		sums[strings.ToLower(digest)] = strings.TrimLeft(file, " *")
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("%s contains no SHA-256 sums", name)
	}
	return sums, nil
}

type result struct {
	name  string
	ok    bool
	how   string
	match string
}

func (r result) String() string {
	if r.ok {
		return fmt.Sprintf("%s: OK (%s) matches %s", r.name, r.how, r.match)
	}
	return fmt.Sprintf("%s: FAILED (%s)", r.name, r.how)
}

func checkFile(name string, sums map[string]string) ([]result, error) {
	switch {
	case strings.HasSuffix(name, ".zip"):
		return checkZip(name, sums)
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return checkTarGz(name, sums)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return []result{check(name, data, sums)}, nil
}

func checkZip(name string, sums map[string]string) ([]result, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	var results []result
	for _, f := range zr.File {
		if !isBinaryName(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		results = append(results, check(name+":"+f.Name, data, sums))
	}
	if len(results) == 0 {
		return nil, errors.New("no agw-token binaries in archive")
	}
	return results, nil
}

func checkTarGz(name string, sums map[string]string) ([]result, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	var results []result
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg || !isBinaryName(h.Name) {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", h.Name, err)
		}
		results = append(results, check(name+":"+h.Name, data, sums))
	}
	if len(results) == 0 {
		return nil, errors.New("no agw-token binaries in archive")
	}
	return results, nil
}

func isBinaryName(name string) bool {
	return strings.HasPrefix(path.Base(name), "agw-token")
}

func check(name string, data []byte, sums map[string]string) result {
	switch {
	case isPE(data):
		candidates, err := stripAuthenticode(data)
		if err != nil {
			return result{name: name, how: "Windows binary: " + err.Error()}
		}
		how := "Windows binary, unsigned"
		if len(candidates[0]) != len(data) {
			how = "Windows binary, signature removed"
		}
		for _, c := range candidates {
			if match, ok := lookup(c, sums); ok {
				return result{name: name, ok: true, how: how, match: match}
			}
		}
		return result{name: name, how: how + ", no matching sum"}
	case isMachO(data):
		if match, ok := lookup(data, sums); ok {
			return result{name: name, ok: true, how: "macOS binary, unsigned", match: match}
		}
		return result{name: name, how: "macOS binary, not verified: macOS builds are not " +
			"reproducible, check it with codesign and gh attestation verify"}
	default:
		if match, ok := lookup(data, sums); ok {
			return result{name: name, ok: true, how: "unchanged", match: match}
		}
		return result{name: name, how: "no matching sum"}
	}
}

func lookup(data []byte, sums map[string]string) (string, bool) {
	d := sha256.Sum256(data)
	match, ok := sums[hex.EncodeToString(d[:])]
	return match, ok
}

func isPE(data []byte) bool {
	return len(data) >= 2 && data[0] == 'M' && data[1] == 'Z'
}

func isMachO(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	switch binary.BigEndian.Uint32(data) {
	case 0xfeedface, 0xfeedfacf, 0xcefaedfe, 0xcffaedfe, 0xcafebabe:
		return true
	}
	return false
}

// PE offsets, from the PE format specification.
const (
	peHeaderOffsetAt  = 0x3c // e_lfanew in the DOS header
	coffHeaderSize    = 20
	optHeaderSizeAt   = 16 // SizeOfOptionalHeader within the COFF header
	checkSumAt        = 64 // CheckSum within the optional header, PE32 and PE32+
	pe32Magic         = 0x10b
	pe32PlusMagic     = 0x20b
	securityDirectory = 4 // IMAGE_DIRECTORY_ENTRY_SECURITY
)

// stripAuthenticode returns the file as it was before Authenticode signing:
// the checksum and security directory entry zeroed and the certificate table
// cut off. Signing tools pad the file to 8 bytes before appending the table,
// so the result is followed by candidates with up to 7 trailing zero bytes
// removed, most likely first. An unsigned file is returned with only its
// checksum zeroed, which Go's linker leaves at zero anyway.
func stripAuthenticode(data []byte) ([][]byte, error) {
	if len(data) < peHeaderOffsetAt+4 {
		return nil, errors.New("truncated DOS header")
	}
	peOff := int(binary.LittleEndian.Uint32(data[peHeaderOffsetAt:]))
	if peOff < 0 || peOff+4+coffHeaderSize > len(data) || !bytes.Equal(data[peOff:peOff+4], []byte("PE\x00\x00")) {
		return nil, errors.New("no PE header")
	}
	coff := peOff + 4
	optSize := int(binary.LittleEndian.Uint16(data[coff+optHeaderSizeAt:]))
	opt := coff + coffHeaderSize
	if opt+optSize > len(data) || optSize < 2 {
		return nil, errors.New("truncated optional header")
	}
	var numDirsAt, dirsAt int
	switch binary.LittleEndian.Uint16(data[opt:]) {
	case pe32Magic:
		numDirsAt, dirsAt = 92, 96
	case pe32PlusMagic:
		numDirsAt, dirsAt = 108, 112
	default:
		return nil, errors.New("unknown optional header magic")
	}
	if numDirsAt+4 > optSize {
		return nil, errors.New("truncated optional header")
	}
	numDirs := int(binary.LittleEndian.Uint32(data[opt+numDirsAt:]))
	secEntry := opt + dirsAt + securityDirectory*8
	if numDirs <= securityDirectory || dirsAt+(securityDirectory+1)*8 > optSize {
		return nil, errors.New("no security directory")
	}

	out := bytes.Clone(data)
	binary.LittleEndian.PutUint32(out[opt+checkSumAt:], 0)
	certOff := int(binary.LittleEndian.Uint32(data[secEntry:]))
	certSize := int(binary.LittleEndian.Uint32(data[secEntry+4:]))
	if certOff == 0 && certSize == 0 {
		return [][]byte{out}, nil
	}
	if certOff <= secEntry+8 || certSize <= 0 || certOff+certSize != len(data) {
		return nil, errors.New("certificate table is not at the end of the file")
	}
	clear(out[secEntry : secEntry+8])
	out = out[:certOff]

	candidates := [][]byte{out}
	for n := 1; n <= 7 && n <= len(out); n++ {
		if out[len(out)-n] != 0 {
			break
		}
		candidates = append(candidates, out[:len(out)-n])
	}
	return candidates, nil
}
