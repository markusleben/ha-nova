package main

import (
	"strings"
	"testing"
)

func validCredential(seed byte) string {
	return "hanova-dev-v1." + strings.Repeat(string(rune('A'+seed%26)), 22) + "." + strings.Repeat(string(rune('a'+seed%26)), 43)
}

func TestDeviceCredentialSlots(t *testing.T) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())

	if _, ok, err := readDeviceCredential(); err != nil || ok {
		t.Fatalf("expected empty current slot, ok=%v err=%v", ok, err)
	}

	cur := validCredential(0)
	if err := writeDeviceCredential(cur); err != nil {
		t.Fatal(err)
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != cur {
		t.Fatalf("current read mismatch got=%q ok=%v err=%v", got, ok, err)
	}

	// Malformed credentials are refused on write.
	if err := writeDeviceCredential("garbage"); err == nil {
		t.Fatalf("malformed credential accepted")
	}
}

func TestPromotePendingCredential(t *testing.T) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	old := validCredential(1)
	fresh := validCredential(2)

	if err := writeDeviceCredential(old); err != nil {
		t.Fatal(err)
	}
	if err := writePendingDeviceCredential(fresh); err != nil {
		t.Fatal(err)
	}
	// Old still current until promotion.
	if got, _, _ := readDeviceCredential(); got != old {
		t.Fatalf("current changed before promotion")
	}
	if err := promotePendingDeviceCredential(); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := readDeviceCredential(); got != fresh {
		t.Fatalf("promotion did not swap current")
	}
	if _, ok, _ := readPendingDeviceCredential(); ok {
		t.Fatalf("pending slot not cleared after promotion")
	}
}

func TestGetOrCreateClientInstallID(t *testing.T) {
	cfg := &runtimeConfig{}
	saved := 0
	persist := func(*runtimeConfig) error { saved++; return nil }

	id1, err := getOrCreateClientInstallID(cfg, persist)
	if err != nil || !strings.HasPrefix(id1, "inst-") {
		t.Fatalf("bad id %q err %v", id1, err)
	}
	if saved != 1 {
		t.Fatalf("expected one persist, got %d", saved)
	}
	// Stable on second call, no re-persist.
	id2, _ := getOrCreateClientInstallID(cfg, persist)
	if id2 != id1 || saved != 1 {
		t.Fatalf("install id not stable: %q vs %q saved=%d", id1, id2, saved)
	}
}
