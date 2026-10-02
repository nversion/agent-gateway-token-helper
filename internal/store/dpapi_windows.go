package store

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dpapiEntropy is mixed into the encryption so other DPAPI users in the same
// account can't decrypt the files by accident. It is not a secret.
var dpapiEntropy = []byte("agw-token/v1")

// platformBackend stores tokens in files under %LOCALAPPDATA% encrypted with
// DPAPI (CryptProtectData), which binds them to the user's Windows login.
func platformBackend(dir string) Backend {
	return newFileBackend(dir, "file (DPAPI-encrypted)", &cipher{seal: dpapiProtect, open: dpapiUnprotect})
}

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func dpapiProtect(data []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(blob(data), nil, blob(dpapiEntropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(&out)
}

func dpapiUnprotect(data []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(data), nil, blob(dpapiEntropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(&out)
}

// takeBlob copies a DPAPI output blob into Go memory and frees it.
func takeBlob(b *windows.DataBlob) ([]byte, error) {
	if b.Data == nil {
		return nil, errors.New("DPAPI returned no data")
	}
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data))) }()
	return append([]byte(nil), unsafe.Slice(b.Data, b.Size)...), nil
}
