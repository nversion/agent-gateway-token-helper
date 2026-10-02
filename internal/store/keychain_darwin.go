//go:build darwin && cgo

package store

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security
#include <stdlib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>

static CFStringRef agwStr(const char *s) {
	return CFStringCreateWithCString(NULL, s, kCFStringEncodingUTF8);
}

// agwQuery returns a mutable generic-password query for service/account; the
// caller adds to and releases it.
static CFMutableDictionaryRef agwQuery(const char *service, const char *account) {
	CFMutableDictionaryRef q = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFStringRef s = agwStr(service), a = agwStr(account);
	CFDictionarySetValue(q, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(q, kSecAttrService, s);
	CFDictionarySetValue(q, kSecAttrAccount, a);
	CFRelease(s);
	CFRelease(a);
	return q;
}

static OSStatus agwKeychainSet(const char *service, const char *account, const char *label, const void *data, long len) {
	CFDataRef d = CFDataCreate(NULL, data, len);
	CFMutableDictionaryRef q = agwQuery(service, account);

	CFMutableDictionaryRef upd = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(upd, kSecValueData, d);
	OSStatus st = SecItemUpdate(q, upd);
	CFRelease(upd);

	if (st == errSecItemNotFound) {
		CFStringRef l = agwStr(label);
		CFDictionarySetValue(q, kSecValueData, d);
		CFDictionarySetValue(q, kSecAttrLabel, l);
		CFDictionarySetValue(q, kSecAttrAccessible, kSecAttrAccessibleWhenUnlocked);
		st = SecItemAdd(q, NULL);
		CFRelease(l);
	}
	CFRelease(q);
	CFRelease(d);
	return st;
}

static OSStatus agwKeychainGet(const char *service, const char *account, CFDataRef *out) {
	CFMutableDictionaryRef q = agwQuery(service, account);
	CFDictionarySetValue(q, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(q, kSecMatchLimit, kSecMatchLimitOne);
	OSStatus st = SecItemCopyMatching(q, (CFTypeRef *)out);
	CFRelease(q);
	return st;
}

static OSStatus agwKeychainDelete(const char *service, const char *account) {
	CFMutableDictionaryRef q = agwQuery(service, account);
	OSStatus st = SecItemDelete(q);
	CFRelease(q);
	return st;
}

// agwStatusMessage returns a malloc'ed UTF-8 description of st, or NULL.
static char *agwStatusMessage(OSStatus st) {
	CFStringRef msg = SecCopyErrorMessageString(st, NULL);
	if (msg == NULL) {
		return NULL;
	}
	CFIndex n = CFStringGetMaximumSizeForEncoding(CFStringGetLength(msg), kCFStringEncodingUTF8) + 1;
	char *buf = malloc(n);
	if (buf != NULL && !CFStringGetCString(msg, buf, n, kCFStringEncodingUTF8)) {
		free(buf);
		buf = NULL;
	}
	CFRelease(msg);
	return buf;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/nversion/agent-gateway-token-helper/internal/config"
)

// keychainBackend stores tokens as generic passwords in the user's login
// keychain. Items are created by this binary, so its code signature is on the
// item's access list and it can read them back without a prompt; a rebuilt
// unsigned binary triggers a one-time "allow" dialog.
type keychainBackend struct {
	service string
}

func platformBackend(string) Backend {
	return &keychainBackend{service: config.AppName}
}

func (k *keychainBackend) Name() string {
	return "macOS Keychain (service " + k.service + ")"
}

func (k *keychainBackend) Load(key string) ([]byte, error) {
	svc, acct := C.CString(k.service), C.CString(key)
	defer C.free(unsafe.Pointer(svc))
	defer C.free(unsafe.Pointer(acct))

	var out C.CFDataRef
	st := C.agwKeychainGet(svc, acct, &out)
	if st == C.errSecItemNotFound {
		return nil, ErrNotFound
	}
	if st != C.errSecSuccess {
		return nil, keychainError("reading", st)
	}
	defer C.CFRelease(C.CFTypeRef(out))
	return C.GoBytes(unsafe.Pointer(C.CFDataGetBytePtr(out)), C.int(C.CFDataGetLength(out))), nil
}

func (k *keychainBackend) Save(key string, data []byte) error {
	svc, acct := C.CString(k.service), C.CString(key)
	label := C.CString(k.service + " token")
	defer C.free(unsafe.Pointer(svc))
	defer C.free(unsafe.Pointer(acct))
	defer C.free(unsafe.Pointer(label))

	cdata := C.CBytes(data)
	defer C.free(cdata)
	if st := C.agwKeychainSet(svc, acct, label, cdata, C.long(len(data))); st != C.errSecSuccess {
		return keychainError("writing", st)
	}
	return nil
}

func (k *keychainBackend) Delete(key string) error {
	svc, acct := C.CString(k.service), C.CString(key)
	defer C.free(unsafe.Pointer(svc))
	defer C.free(unsafe.Pointer(acct))

	st := C.agwKeychainDelete(svc, acct)
	if st != C.errSecSuccess && st != C.errSecItemNotFound {
		return keychainError("deleting", st)
	}
	return nil
}

func keychainError(op string, st C.OSStatus) error {
	msg := "unknown error"
	if cmsg := C.agwStatusMessage(st); cmsg != nil {
		msg = C.GoString(cmsg)
		C.free(unsafe.Pointer(cmsg))
	}
	hint := ""
	if st == C.errSecInteractionNotAllowed {
		hint = " (keychain is locked or no GUI session, e.g. over SSH; use --store file)"
	}
	return fmt.Errorf("%s keychain item: %s (OSStatus %d)%s", op, msg, int(st), hint)
}
