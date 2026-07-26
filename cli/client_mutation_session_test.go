package main

import (
	"errors"
	"strings"
	"testing"
)

func TestOwnerPromptReleasesAndReacquiresClientMutationLock(
	t *testing.T,
) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	err := withPausableClientMutationLock(
		paths,
		func(session *pausableClientMutationLock) error {
			provider := session.pairingCodeProvider(func(
				cloudRemotePairingPrompt,
			) (string, error) {
				release, acquired := acquireAutoRepairLock(paths)
				if !acquired {
					t.Fatal("Owner prompt kept the client mutation lock")
				}
				release()
				return "123456", nil
			})
			code, err := provider(cloudRemotePairingPrompt{})
			if err != nil || code != "123456" {
				t.Fatalf("pairing code=%q err=%v", code, err)
			}
			return session.requireHeld()
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if release, acquired := acquireAutoRepairLock(paths); !acquired {
		t.Fatal("client mutation lock remained held after transaction")
	} else {
		release()
	}
}

func TestOwnerPromptRejectsCodeAfterConcurrentConfigChange(
	t *testing.T,
) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	err := withPausableClientMutationLock(
		paths,
		func(session *pausableClientMutationLock) error {
			provider := session.pairingCodeProvider(func(
				cloudRemotePairingPrompt,
			) (string, error) {
				top := readTestConfigTopLevel(t, paths)
				top["concurrent_edit"] = []byte(`true`)
				if err := writeJSONFile(
					paths.ConfigFile,
					top,
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				return "123456", nil
			})
			_, promptErr := provider(cloudRemotePairingPrompt{})
			if promptErr == nil ||
				!strings.Contains(promptErr.Error(), "code was not used") {
				t.Fatalf("concurrent-edit error = %v", promptErr)
			}
			return session.requireHeld()
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOwnerPromptErrorStillRestoresClientMutationLock(t *testing.T) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	promptErr := errors.New("Owner cancelled")
	err := withPausableClientMutationLock(
		paths,
		func(session *pausableClientMutationLock) error {
			provider := session.pairingCodeProvider(func(
				cloudRemotePairingPrompt,
			) (string, error) {
				return "", promptErr
			})
			_, got := provider(cloudRemotePairingPrompt{})
			if !errors.Is(got, promptErr) {
				t.Fatalf("prompt error = %v", got)
			}
			return session.requireHeld()
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}
