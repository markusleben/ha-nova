package main

import (
	"bufio"
	"errors"
	"io"
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
				RelayBaseURL: "http://relay:8791",
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
				RelayBaseURL: "http://relay:8791",
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
