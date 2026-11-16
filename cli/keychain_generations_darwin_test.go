//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type fakeDarwinKeychain struct {
	items   map[string][]byte
	handles []string
	keep    [][]byte
	failAdd bool
}

func installFakeDarwinKeychain(t *testing.T) *fakeDarwinKeychain {
	t.Helper()
	t.Setenv("HA_NOVA_KEYRING_SERVICE", "")
	fake := &fakeDarwinKeychain{items: map[string][]byte{}}
	lockPath := darwinKeychainWriteLockPath
	lock := filepath.Join(t.TempDir(), "keychain-write.lock")
	darwinKeychainWriteLockPath = func() (string, error) { return lock, nil }
	t.Cleanup(func() { darwinKeychainWriteLockPath = lockPath })
	find, add, free, del, release := darwinOAuthSecurity.findGeneric, darwinOAuthSecurity.addGeneric,
		darwinOAuthSecurity.freeContent, darwinOAuthSecurity.deleteItem, darwinOAuthSecurity.cfRelease
	t.Cleanup(func() {
		darwinOAuthSecurity.findGeneric, darwinOAuthSecurity.addGeneric = find, add
		darwinOAuthSecurity.freeContent, darwinOAuthSecurity.deleteItem, darwinOAuthSecurity.cfRelease = free, del, release
	})
	key := func(sl uint32, s *byte, al uint32, a *byte) string {
		return string(unsafe.Slice(s, sl)) + "\x00" + string(unsafe.Slice(a, al))
	}
	darwinOAuthSecurity.findGeneric = func(_ uintptr, sl uint32, s *byte, al uint32, a *byte, length *uint32, data *unsafe.Pointer, item *uintptr) darwinOAuthOSStatus {
		k := key(sl, s, al, a)
		value, ok := fake.items[k]
		if !ok {
			return darwinOAuthItemNotFound
		}
		if length != nil {
			copied := append([]byte(nil), value...)
			fake.keep = append(fake.keep, copied)
			*length, *data = uint32(len(copied)), unsafe.Pointer(&copied[0])
		}
		fake.handles = append(fake.handles, k)
		*item = uintptr(len(fake.handles))
		return darwinOAuthSuccess
	}
	darwinOAuthSecurity.addGeneric = func(_ uintptr, sl uint32, s *byte, al uint32, a *byte, vl uint32, v *byte, item *uintptr) darwinOAuthOSStatus {
		k := key(sl, s, al, a)
		if _, exists := fake.items[k]; exists {
			return -25299
		}
		if fake.failAdd {
			return darwinOAuthAuthFailed
		}
		fake.items[k] = append([]byte(nil), unsafe.Slice(v, vl)...)
		fake.handles = append(fake.handles, k)
		*item = uintptr(len(fake.handles))
		return darwinOAuthSuccess
	}
	darwinOAuthSecurity.deleteItem = func(item uintptr) darwinOAuthOSStatus {
		delete(fake.items, fake.handles[item-1])
		return darwinOAuthSuccess
	}
	darwinOAuthSecurity.freeContent = func(uintptr, unsafe.Pointer) darwinOAuthOSStatus { return darwinOAuthSuccess }
	darwinOAuthSecurity.cfRelease = func(uintptr) {}
	return fake
}

func (fake *fakeDarwinKeychain) names() []string {
	var names []string
	for k := range fake.items {
		names = append(names, strings.SplitN(k, "\x00", 2)[0])
	}
	return names
}

func mustReadDarwinSlot(t *testing.T, service string) string {
	t.Helper()
	value, found, status := darwinOAuthGet(service, "markus")
	if status != darwinOAuthSuccess || !found {
		t.Fatalf("read %s: found=%v status=%d", service, found, status)
	}
	return string(value)
}

func TestDarwinKeychainRewriteKeepsOnlyTheNewestGeneration(t *testing.T) {
	fake := installFakeDarwinKeychain(t)
	for _, value := range []string{"one", "two", "three"} {
		if status := darwinOAuthSet(deviceCredentialService, "markus", []byte(value)); status != darwinOAuthSuccess {
			t.Fatalf("write %s: %d", value, status)
		}
		if got := mustReadDarwinSlot(t, deviceCredentialService); got != value {
			t.Fatalf("read %q, want %q", got, value)
		}
		if names := fake.names(); len(names) != 1 || !strings.HasPrefix(names[0], darwinKeychainRoot+"/device/default#") {
			t.Fatalf("items after writing %s: %v", value, names)
		}
	}
}

func TestDarwinKeychainFailedWriteKeepsThePreviousValue(t *testing.T) {
	fake := installFakeDarwinKeychain(t)
	if status := darwinOAuthSet(oauthSecretCurrentService, "markus", []byte("grant-1")); status != darwinOAuthSuccess {
		t.Fatalf("first write: %d", status)
	}
	fake.failAdd = true
	if status := darwinOAuthSet(oauthSecretCurrentService, "markus", []byte("grant-2")); status == darwinOAuthSuccess {
		t.Fatal("a failed add reported success")
	}
	if got := mustReadDarwinSlot(t, oauthSecretCurrentService); got != "grant-1" {
		t.Fatalf("after a failed write read %q, want the previous grant", got)
	}
}

func TestDarwinKeychainCutBeforeRemovingTheOldGenerationReadsTheNewOne(t *testing.T) {
	fake := installFakeDarwinKeychain(t)
	slot := darwinKeychainSlot(deviceCredentialService, "markus")
	fake.items[slot+"#2\x00markus"] = []byte("7\nold")
	fake.items[slot+"#1\x00markus"] = []byte("8\nnew")
	if got := mustReadDarwinSlot(t, deviceCredentialService); got != "new" {
		t.Fatalf("read %q, want the newer generation", got)
	}
	if status := darwinOAuthSet(deviceCredentialService, "markus", []byte("newer")); status != darwinOAuthSuccess {
		t.Fatalf("write: %d", status)
	}
	if got := fake.items[slot+"#2\x00markus"]; string(got) != "9\nnewer" || len(fake.items) != 1 {
		t.Fatalf("items after the write: %v", fake.names())
	}
}

func TestDarwinKeychainReadThatRacesARewriteStillFindsTheKey(t *testing.T) {
	fake := installFakeDarwinKeychain(t)
	slot := darwinKeychainSlot(deviceCredentialService, "markus")
	fake.items[slot+"#2\x00markus"] = []byte("5\nold")
	find := darwinOAuthSecurity.findGeneric
	lookups := 0
	darwinOAuthSecurity.findGeneric = func(k uintptr, sl uint32, s *byte, al uint32, a *byte, length *uint32, data *unsafe.Pointer, item *uintptr) darwinOAuthOSStatus {
		status := find(k, sl, s, al, a, length, data, item)
		if lookups++; lookups == 1 {
			fake.items[slot+"#1\x00markus"] = []byte("6\nnew")
			delete(fake.items, slot+"#2\x00markus")
		}
		return status
	}
	if got := mustReadDarwinSlot(t, deviceCredentialService); got != "new" {
		t.Fatalf("read %q, want the rewritten key", got)
	}
}

func TestDarwinKeychainWriterWaitsForAnotherProcessWriter(t *testing.T) {
	fake := installFakeDarwinKeychain(t)
	path, _ := darwinKeychainWriteLockPath()
	other, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := unix.Flock(int(other.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	done := make(chan darwinOAuthOSStatus, 1)
	go func() { done <- darwinOAuthSet(deviceCredentialService, "markus", []byte("key")) }()
	select {
	case status := <-done:
		t.Fatalf("wrote while another writer held the lock: %d", status)
	case <-time.After(150 * time.Millisecond):
	}
	_ = unix.Flock(int(other.Fd()), unix.LOCK_UN)
	if status := <-done; status != darwinOAuthSuccess || len(fake.items) != 1 {
		t.Fatalf("write after the lock was released: status=%d items=%v", status, fake.names())
	}
}

func TestDarwinKeychainRefusesMalformedOrTiedGenerations(t *testing.T) {
	for name, items := range map[string]map[string]string{
		"no header": {"#1": "secret"},
		"zero":      {"#1": "0\nsecret"},
		"empty":     {"#1": "3\n"},
		"tie":       {"#1": "4\na", "#2": "4\nb"},
	} {
		fake := installFakeDarwinKeychain(t)
		slot := darwinKeychainSlot(deviceCredentialService, "markus")
		for suffix, value := range items {
			fake.items[slot+suffix+"\x00markus"] = []byte(value)
		}
		if _, found, status := darwinOAuthGet(deviceCredentialService, "markus"); status != darwinOAuthAuthFailed || found {
			t.Errorf("%s: found=%v status=%d, want a refusal", name, found, status)
		}
		if status := darwinOAuthSet(deviceCredentialService, "markus", []byte("x")); status != darwinOAuthAuthFailed {
			t.Errorf("%s: write status %d, want a refusal", name, status)
		}
	}
}

func TestDarwinKeychainDeleteRemovesBothGenerations(t *testing.T) {
	fake := installFakeDarwinKeychain(t)
	if status := darwinOAuthDelete(deviceCredentialService, "markus"); status != darwinOAuthItemNotFound {
		t.Fatalf("delete of nothing: %d", status)
	}
	slot := darwinKeychainSlot(deviceCredentialService, "markus")
	fake.items[slot+"#1\x00markus"] = []byte("1\na")
	fake.items[slot+"#2\x00markus"] = []byte("2\nb")
	if status := darwinOAuthDelete(deviceCredentialService, "markus"); status != darwinOAuthSuccess || len(fake.items) != 0 {
		t.Fatalf("delete: status=%d items=%v", status, fake.names())
	}
}

func TestDarwinKeychainNeverTouchesAHistoricSlot(t *testing.T) {
	fake := installFakeDarwinKeychain(t)
	fake.items[deviceCredentialService+"\x00markus"] = []byte("old-key")
	if _, found, status := darwinOAuthGet(deviceCredentialService, "markus"); found || status != darwinOAuthSuccess {
		t.Fatalf("read found=%v status=%d, want nothing under the new root", found, status)
	}
	if status := darwinOAuthSet(deviceCredentialService, "markus", []byte("new-key")); status != darwinOAuthSuccess {
		t.Fatalf("write: %d", status)
	}
	if string(fake.items[deviceCredentialService+"\x00markus"]) != "old-key" || len(fake.items) != 2 {
		t.Fatalf("the historic item changed: %v", fake.names())
	}
}
