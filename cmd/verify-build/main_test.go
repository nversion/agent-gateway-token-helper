package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unsignedPE returns a minimal PE32+ file shaped like Go's output: a DOS
// header pointing at the PE header, a zero checksum and an empty security
// directory, followed by bodyLen bytes of section data.
func unsignedPE(t *testing.T, bodyLen int) []byte {
	t.Helper()
	var b bytes.Buffer
	dos := make([]byte, 64)
	dos[0], dos[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(dos[peHeaderOffsetAt:], 64)
	b.Write(dos)
	b.WriteString("PE\x00\x00")
	must(t, binary.Write(&b, binary.LittleEndian, pe.FileHeader{
		Machine:              pe.IMAGE_FILE_MACHINE_AMD64,
		SizeOfOptionalHeader: uint16(binary.Size(pe.OptionalHeader64{})),
	}))
	must(t, binary.Write(&b, binary.LittleEndian, pe.OptionalHeader64{
		Magic:               pe32PlusMagic,
		NumberOfRvaAndSizes: 16,
	}))
	for i := range bodyLen {
		b.WriteByte(byte(i%251 + 1))
	}
	return b.Bytes()
}

// sign appends a fake certificate table the way signing tools do: pad to 8
// bytes, point the security directory at the table and set the checksum.
func sign(t *testing.T, unsigned []byte) []byte {
	t.Helper()
	// The synthetic file must be a PE file debug/pe accepts.
	_, err := pe.NewFile(bytes.NewReader(unsigned))
	must(t, err)
	out := bytes.Clone(unsigned)
	for len(out)%8 != 0 {
		out = append(out, 0)
	}
	cert := bytes.Repeat([]byte{0xAB}, 1000)
	opt := 64 + 4 + coffHeaderSize
	binary.LittleEndian.PutUint32(out[opt+checkSumAt:], 0x1234567)
	sec := opt + 112 + securityDirectory*8
	binary.LittleEndian.PutUint32(out[sec:], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[sec+4:], uint32(len(cert)))
	return append(out, cert...)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func digest(data []byte) string {
	d := sha256.Sum256(data)
	return hex.EncodeToString(d[:])
}

func TestStripAuthenticode(t *testing.T) {
	for _, bodyLen := range []int{512, 515} { // 8-aligned and needing padding
		t.Run(fmt.Sprint(bodyLen), func(t *testing.T) {
			unsigned := unsignedPE(t, bodyLen)
			sums := map[string]string{digest(unsigned): "agw-token-windows-amd64.exe"}

			if r := check("unsigned", unsigned, sums); !r.ok {
				t.Errorf("unsigned build: %v", r)
			}
			signed := sign(t, unsigned)
			if r := check("signed", signed, sums); !r.ok || r.match != "agw-token-windows-amd64.exe" {
				t.Errorf("signed build: %v", r)
			}

			tampered := bytes.Clone(signed)
			tampered[200] ^= 1
			if r := check("tampered", tampered, sums); r.ok {
				t.Errorf("tampered build verified: %v", r)
			}
		})
	}
}

func TestStripAuthenticodeRejectsTrailingData(t *testing.T) {
	signed := append(sign(t, unsignedPE(t, 512)), "extra"...)
	if _, err := stripAuthenticode(signed); err == nil {
		t.Fatal("data after the certificate table was accepted")
	}
}

func TestOtherBinaries(t *testing.T) {
	elf := []byte("\x7fELF rest of the binary")
	macho := []byte("\xcf\xfa\xed\xfe rest of the binary")
	sums := map[string]string{digest(elf): "agw-token-linux-amd64"}

	if r := check("elf", elf, sums); !r.ok {
		t.Errorf("ELF: %v", r)
	}
	if r := check("elf", append(elf, 0), sums); r.ok {
		t.Errorf("changed ELF verified: %v", r)
	}
	if r := check("macho", macho, sums); r.ok || !strings.Contains(r.how, "not reproducible") {
		t.Errorf("Mach-O: %v", r)
	}
}

func TestRunWithArchive(t *testing.T) {
	dir := t.TempDir()
	unsigned := unsignedPE(t, 600)
	sumsFile := filepath.Join(dir, "SHA256SUMS-unsigned")
	must(t, os.WriteFile(sumsFile, []byte(digest(unsigned)+"  agw-token-windows-amd64.exe\n"), 0o600))

	zipFile := filepath.Join(dir, "agw-token-1.2.3-windows-amd64.zip")
	f, err := os.Create(zipFile)
	must(t, err)
	zw := zip.NewWriter(f)
	for name, data := range map[string][]byte{
		"agw-token.exe": sign(t, unsigned),
		"README.md":     []byte("not checked"),
	} {
		w, err := zw.Create(name)
		must(t, err)
		_, err = w.Write(data)
		must(t, err)
	}
	must(t, zw.Close())
	must(t, f.Close())

	var out, errOut bytes.Buffer
	if code := run([]string{sumsFile, zipFile}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "agw-token.exe: OK (Windows binary, signature removed)") {
		t.Errorf("output: %s", out.String())
	}

	bad := filepath.Join(dir, "agw-token.exe")
	must(t, os.WriteFile(bad, unsignedPE(t, 601), 0o600))
	out.Reset()
	if code := run([]string{sumsFile, bad}, &out, &errOut); code != 1 {
		t.Errorf("mismatch exited %d: %s", code, out.String())
	}
}
