package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNativeSecretWorkerAllowsOnlyOwnedSlots(t *testing.T) {
	user := secretUser()
	valid := []struct {
		service string
		account string
	}{
		{oauthSecretCurrentService, "default"},
		{oauthSecretPendingService, "profile-1"},
		{oauthSecretRetiringService, "profile.1"},
		{deviceCredentialService, user},
		{deviceCredentialPendingService, user},
		{deviceCredentialService + ".home-1", user},
		{deviceCredentialPendingService + ".home-1", user},
		{deviceCredentialProbeService, user},
	}
	for _, key := range valid {
		request := nativeSecretWorkerRequest{
			SchemaVersion: nativeSecretWorkerSchema,
			Operation:     nativeSecretGet,
			UI:            SecretStoreForbidUI,
			Service:       key.service,
			Account:       key.account,
		}
		if err := validateNativeSecretWorkerRequest(request); err != nil {
			t.Errorf("valid key %q/%q: %v", key.service, key.account, err)
		}
	}

	invalid := []struct {
		service string
		account string
	}{
		{"arbitrary", user},
		{deviceCredentialService, "other-user"},
		{deviceCredentialService + ".UPPER", user},
		{oauthSecretCurrentService, "../profile"},
		{oauthSecretPreflightServicePrefix + strings.Repeat("a", 31), "default"},
	}
	for _, key := range invalid {
		request := nativeSecretWorkerRequest{
			SchemaVersion: nativeSecretWorkerSchema,
			Operation:     nativeSecretGet,
			UI:            SecretStoreForbidUI,
			Service:       key.service,
			Account:       key.account,
		}
		if err := validateNativeSecretWorkerRequest(request); !IsCloudErrorCode(
			err,
			CloudErrInvalidInput,
		) {
			t.Errorf("invalid key %q/%q error = %v", key.service, key.account, err)
		}
	}
}

func TestNativeSecretWorkerValidatesOperationShape(t *testing.T) {
	base := nativeSecretWorkerRequest{
		SchemaVersion: nativeSecretWorkerSchema,
		UI:            SecretStoreForbidUI,
		Service:       oauthSecretCurrentService,
		Account:       "default",
	}
	for _, operation := range []nativeSecretOperation{
		nativeSecretGet,
		nativeSecretDelete,
	} {
		request := base
		request.Operation = operation
		request.Value = []byte("unexpected")
		if err := validateNativeSecretWorkerRequest(request); !IsCloudErrorCode(
			err,
			CloudErrInvalidInput,
		) {
			t.Errorf("%s with value error = %v", operation, err)
		}
	}
	request := base
	request.Operation = nativeSecretSet
	if err := validateNativeSecretWorkerRequest(request); !IsCloudErrorCode(
		err,
		CloudErrInvalidInput,
	) {
		t.Fatalf("empty Set error = %v", err)
	}
	request.Value = bytes.Repeat([]byte("x"), oauthSecretMaxEncodedSize+1)
	if err := validateNativeSecretWorkerRequest(request); !IsCloudErrorCode(
		err,
		CloudErrInvalidInput,
	) {
		t.Fatalf("oversize Set error = %v", err)
	}
}

func TestNativeSecretWorkerCommandCarriesNoSecretArguments(t *testing.T) {
	command := newNativeSecretWorkerCommand(
		context.Background(),
		"/path/to/ha-nova",
	)
	if len(command.Args) != 2 ||
		command.Args[0] != "/path/to/ha-nova" ||
		command.Args[1] != nativeSecretWorkerCommand {
		t.Fatalf("worker argv = %#v", command.Args)
	}
}

func TestNativeSecretWorkerRejectsUnverifiedParent(t *testing.T) {
	original := nativeSecretWorkerParentVerified
	nativeSecretWorkerParentVerified = func() bool { return false }
	t.Cleanup(func() { nativeSecretWorkerParentVerified = original })

	request := `{"schema_version":1,"operation":"get","ui":"forbid_ui",` +
		`"service":"ha-nova.oauth.home-assistant-cloud.current",` +
		`"account":"default"}`
	var output bytes.Buffer
	handled, exitCode := maybeRunNativeSecretWorker(
		[]string{nativeSecretWorkerCommand},
		strings.NewReader(request),
		&output,
	)
	if !handled || exitCode == 0 || output.Len() != 0 {
		t.Fatalf(
			"unverified worker: handled=%v exit=%d output=%q",
			handled,
			exitCode,
			output.String(),
		)
	}
}

func TestNativeSecretWorkerProcessKillsBlockedOperations(t *testing.T) {
	original := nativeSecretWorkerCommandForProcess
	nativeSecretWorkerCommandForProcess = func(
		ctx context.Context,
		_ string,
	) *exec.Cmd {
		command := exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=TestNativeSecretWorkerBlockingHelper",
		)
		command.Env = append(
			os.Environ(),
			"HA_NOVA_NATIVE_SECRET_BLOCKING_HELPER=1",
		)
		return command
	}
	t.Cleanup(func() { nativeSecretWorkerCommandForProcess = original })

	for _, test := range []struct {
		operation nativeSecretOperation
		code      CloudErrorCode
		value     []byte
	}{
		{nativeSecretGet, CloudErrTimeout, nil},
		{nativeSecretSet, CloudErrSecretOutcomeUnknown, []byte("secret")},
		{nativeSecretDelete, CloudErrSecretOutcomeUnknown, nil},
	} {
		t.Run(string(test.operation), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(
				context.Background(),
				25*time.Millisecond,
			)
			defer cancel()
			started := time.Now()
			_, err := runNativeSecretWorkerProcess(
				ctx,
				nativeSecretWorkerRequest{
					SchemaVersion: nativeSecretWorkerSchema,
					Operation:     test.operation,
					UI:            SecretStoreForbidUI,
					Service:       oauthSecretCurrentService,
					Account:       "default",
					Value:         test.value,
				},
			)
			if !IsCloudErrorCode(err, test.code) {
				t.Fatalf("blocked %s error = %v", test.operation, err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("blocked %s elapsed = %s", test.operation, elapsed)
			}
		})
	}
}

func TestNativeSecretWorkerTreatsPostStartFailureAsAmbiguousMutation(
	t *testing.T,
) {
	original := nativeSecretWorkerCommandForProcess
	nativeSecretWorkerCommandForProcess = func(
		ctx context.Context,
		_ string,
	) *exec.Cmd {
		command := exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=TestNativeSecretWorkerBlockingHelper",
		)
		command.Env = append(
			os.Environ(),
			"HA_NOVA_NATIVE_SECRET_HELPER_MODE=exit",
		)
		return command
	}
	t.Cleanup(func() { nativeSecretWorkerCommandForProcess = original })

	for _, test := range []struct {
		operation nativeSecretOperation
		code      CloudErrorCode
		value     []byte
	}{
		{nativeSecretGet, CloudErrSecretStore, nil},
		{nativeSecretSet, CloudErrSecretOutcomeUnknown, []byte("secret")},
		{nativeSecretDelete, CloudErrSecretOutcomeUnknown, nil},
	} {
		_, err := runNativeSecretWorkerProcess(
			context.Background(),
			nativeSecretWorkerRequest{
				SchemaVersion: nativeSecretWorkerSchema,
				Operation:     test.operation,
				UI:            SecretStoreForbidUI,
				Service:       oauthSecretCurrentService,
				Account:       "default",
				Value:         test.value,
			},
		)
		if !IsCloudErrorCode(err, test.code) {
			t.Errorf("failed %s error = %v", test.operation, err)
		}
	}
}

func TestNativeSecretWorkerBoundsProcessOutput(t *testing.T) {
	original := nativeSecretWorkerCommandForProcess
	nativeSecretWorkerCommandForProcess = func(
		ctx context.Context,
		_ string,
	) *exec.Cmd {
		command := exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=TestNativeSecretWorkerBlockingHelper",
		)
		command.Env = append(
			os.Environ(),
			"HA_NOVA_NATIVE_SECRET_HELPER_MODE=oversize",
		)
		return command
	}
	t.Cleanup(func() { nativeSecretWorkerCommandForProcess = original })

	for _, test := range []struct {
		operation nativeSecretOperation
		code      CloudErrorCode
		value     []byte
	}{
		{nativeSecretGet, CloudErrSecretStore, nil},
		{nativeSecretSet, CloudErrSecretOutcomeUnknown, []byte("secret")},
		{nativeSecretDelete, CloudErrSecretOutcomeUnknown, nil},
	} {
		_, err := runNativeSecretWorkerProcess(
			context.Background(),
			nativeSecretWorkerRequest{
				SchemaVersion: nativeSecretWorkerSchema,
				Operation:     test.operation,
				UI:            SecretStoreForbidUI,
				Service:       oauthSecretCurrentService,
				Account:       "default",
				Value:         test.value,
			},
		)
		if !IsCloudErrorCode(err, test.code) {
			t.Errorf("oversize %s error = %v", test.operation, err)
		}
	}
}

func TestNativeSecretWorkerBlockingHelper(t *testing.T) {
	switch {
	case os.Getenv("HA_NOVA_NATIVE_SECRET_BLOCKING_HELPER") == "1":
		time.Sleep(time.Minute)
	case os.Getenv("HA_NOVA_NATIVE_SECRET_HELPER_MODE") == "exit":
		os.Exit(7)
	case os.Getenv("HA_NOVA_NATIVE_SECRET_HELPER_MODE") == "oversize":
		_, _ = os.Stdout.Write(bytes.Repeat(
			[]byte("x"),
			nativeSecretWorkerMaxInput*4,
		))
	}
}
