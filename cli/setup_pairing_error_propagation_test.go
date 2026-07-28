package main

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupSecurePairingReturnsFatalErrorsWithoutAnotherCode(
	t *testing.T,
) {
	sentinel := errors.New("activation checkpoint must be resumed")
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{name: "activation", err: sentinel},
		{name: "pin mismatch", err: errPinMismatch},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			originalProbe := probePairingV1ForSetup
			originalPair := securePairForSetup
			probePairingV1ForSetup = func(string) bool { return true }
			calls := 0
			securePairForSetup = func(
				_, _ string,
				_ *runtimeConfig,
				_ func(*runtimeConfig) error,
				_ pairingClientInfo,
			) (string, error) {
				calls++
				return "", testCase.err
			}
			t.Cleanup(func() {
				probePairingV1ForSetup = originalProbe
				securePairForSetup = originalPair
			})

			reader := bufio.NewReader(
				strings.NewReader("\n473921\n654321\n"),
			)
			cfg := &runtimeConfig{
				RelayBaseURL:    "http://relay:8791",
				ClientInstallID: "inst-test",
			}
			_, err := runSetupPairingFlow(
				reader,
				io.Discard,
				runtimePaths{ConfigDir: t.TempDir()},
				cfg,
				false,
			)
			if !errors.Is(err, testCase.err) || calls != 1 {
				t.Fatalf(
					"fatal pairing err=%v calls=%d",
					err,
					calls,
				)
			}
		})
	}
}

func TestSetupSecurePairingPreparesInstallIDBeforeRetry(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{name: "rejected", err: errPairingCodeRejected},
		{name: "inactive", err: errPairingInactive},
		{
			name: "rate limit",
			err: &relayPairingRateLimitError{
				retryAfterSeconds: 1,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			originalProbe := probePairingV1ForSetup
			originalPair := securePairForSetup
			probePairingV1ForSetup = func(string) bool { return true }
			t.Cleanup(func() {
				probePairingV1ForSetup = originalProbe
				securePairForSetup = originalPair
			})

			dir := t.TempDir()
			paths := runtimePaths{
				ConfigDir:  dir,
				ConfigFile: filepath.Join(dir, "config.json"),
			}
			cfg := &runtimeConfig{
				RelayBaseURL: "http://relay:8791",
			}
			if err := saveConfig(paths, *cfg); err != nil {
				t.Fatal(err)
			}
			snapshot, err := readSetupConfigSnapshot(paths)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle := [][]byte{nil, nil, snapshot}
			calls := 0
			securePairForSetup = func(
				_, _ string,
				cfg *runtimeConfig,
				_ func(*runtimeConfig) error,
				_ pairingClientInfo,
			) (string, error) {
				calls++
				if cfg.ClientInstallID == "" {
					t.Fatal("install ID was not persisted before the code")
				}
				if calls == 1 {
					return "", testCase.err
				}
				return "device-id", nil
			}

			reader := bufio.NewReader(
				strings.NewReader("\n473921\n654321\n"),
			)
			_, err = runSetupPairingFlow(
				reader,
				io.Discard,
				paths,
				cfg,
				false,
				lifecycle...,
			)
			if !errors.Is(err, errSetupDevicePaired) || calls != 2 {
				t.Fatalf(
					"retry after install-id write err=%v calls=%d",
					err,
					calls,
				)
			}
			saved, err := loadConfig(paths)
			if err != nil {
				t.Fatal(err)
			}
			if saved.ClientInstallID == "" ||
				saved.RelaySecureBaseURL != "" ||
				saved.PendingSecureBaseURL != "" {
				t.Fatalf("unexpected retry config: %+v", saved)
			}
		})
	}
}

func TestSetupSecurePairingFallbackUsesPreparedInstallID(t *testing.T) {
	originalProbe := probePairingV1ForSetup
	originalPair := securePairForSetup
	originalExchange := exchangeRelayPairingCodeForSetup
	probePairingV1ForSetup = func(string) bool { return true }
	t.Cleanup(func() {
		probePairingV1ForSetup = originalProbe
		securePairForSetup = originalPair
		exchangeRelayPairingCodeForSetup = originalExchange
	})

	dir := t.TempDir()
	paths := runtimePaths{
		ConfigDir:  dir,
		ConfigFile: filepath.Join(dir, "config.json"),
	}
	cfg := &runtimeConfig{
		RelayBaseURL: "http://relay:8791",
	}
	if err := saveConfig(paths, *cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readSetupConfigSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := [][]byte{nil, nil, snapshot}
	securePairForSetup = func(
		_, _ string,
		cfg *runtimeConfig,
		_ func(*runtimeConfig) error,
		_ pairingClientInfo,
	) (string, error) {
		if cfg.ClientInstallID == "" {
			t.Fatal("install ID was not persisted before the code")
		}
		return "", errRelayNotV1
	}
	exchangeRelayPairingCodeForSetup = func(
		_ *http.Client,
		_, _ string,
	) (string, error) {
		return "legacy-token", nil
	}

	reader := bufio.NewReader(strings.NewReader("\n473921\n"))
	token, err := runSetupPairingFlow(
		reader,
		io.Discard,
		paths,
		cfg,
		false,
		lifecycle...,
	)
	if err != nil || token != "legacy-token" {
		t.Fatalf("fallback after install-id write token=%q err=%v", token, err)
	}
}

func TestSetupSecurePairingRejectsConfigDriftDuringRetry(t *testing.T) {
	for _, existingInstallID := range []bool{false, true} {
		name := "fresh install ID"
		if existingInstallID {
			name = "existing install ID"
		}
		t.Run(name, func(t *testing.T) {
			originalProbe := probePairingV1ForSetup
			originalPair := securePairForSetup
			probePairingV1ForSetup = func(string) bool { return true }
			t.Cleanup(func() {
				probePairingV1ForSetup = originalProbe
				securePairForSetup = originalPair
			})

			dir := t.TempDir()
			paths := runtimePaths{
				ConfigDir:  dir,
				ConfigFile: filepath.Join(dir, "config.json"),
			}
			cfg := &runtimeConfig{
				RelayBaseURL: "http://relay:8791",
			}
			if existingInstallID {
				cfg.ClientInstallID = "inst-existing"
			}
			if err := saveConfig(paths, *cfg); err != nil {
				t.Fatal(err)
			}
			snapshot, err := readSetupConfigSnapshot(paths)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle := [][]byte{nil, nil, snapshot}
			calls := 0
			securePairForSetup = func(
				_, _ string,
				cfg *runtimeConfig,
				_ func(*runtimeConfig) error,
				_ pairingClientInfo,
			) (string, error) {
				calls++
				changed := *cfg
				changed.RelayBaseURL = "http://concurrent:8791"
				if err := saveConfig(paths, changed); err != nil {
					t.Fatal(err)
				}
				return "", errPairingInactive
			}

			reader := bufio.NewReader(
				strings.NewReader("\n473921\n654321\n"),
			)
			_, err = runSetupPairingFlow(
				reader,
				io.Discard,
				paths,
				cfg,
				false,
				lifecycle...,
			)
			if err == nil ||
				!strings.Contains(
					err.Error(),
					"server configuration changed during setup",
				) ||
				calls != 1 {
				t.Fatalf("config drift err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestSetupSecurePairingRetriesOnlyRetryableErrors(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{name: "rejected", err: errPairingCodeRejected},
		{name: "inactive", err: errPairingInactive},
		{
			name: "rate limit",
			err: &relayPairingRateLimitError{
				retryAfterSeconds: 1,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			originalProbe := probePairingV1ForSetup
			originalPair := securePairForSetup
			probePairingV1ForSetup = func(string) bool { return true }
			calls := 0
			securePairForSetup = func(
				_, _ string,
				_ *runtimeConfig,
				_ func(*runtimeConfig) error,
				_ pairingClientInfo,
			) (string, error) {
				calls++
				if calls == 1 {
					return "", testCase.err
				}
				return "device-id", nil
			}
			t.Cleanup(func() {
				probePairingV1ForSetup = originalProbe
				securePairForSetup = originalPair
			})

			reader := bufio.NewReader(
				strings.NewReader("\n473921\n654321\n"),
			)
			cfg := &runtimeConfig{
				RelayBaseURL:    "http://relay:8791",
				ClientInstallID: "inst-test",
			}
			_, err := runSetupPairingFlow(
				reader,
				io.Discard,
				runtimePaths{ConfigDir: t.TempDir()},
				cfg,
				false,
			)
			if !errors.Is(err, errSetupDevicePaired) || calls != 2 {
				t.Fatalf(
					"retryable pairing err=%v calls=%d",
					err,
					calls,
				)
			}
		})
	}
}
