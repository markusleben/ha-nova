package main

import (
	"errors"
	"fmt"
	"time"
)

type pausableClientMutationLock struct {
	paths   runtimePaths
	release func()
	held    bool
}

func withPausableClientMutationLock(
	paths runtimePaths,
	mutate func(*pausableClientMutationLock) error,
) error {
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		return fmt.Errorf("another HA NOVA client update is already in progress")
	}
	session := &pausableClientMutationLock{
		paths:   paths,
		release: release,
		held:    true,
	}
	defer session.releaseIfHeld()
	return mutate(session)
}

func (session *pausableClientMutationLock) pairingCodeProvider(
	provider cloudRemotePairingCodeProvider,
) cloudRemotePairingCodeProvider {
	return func(prompt cloudRemotePairingPrompt) (string, error) {
		if provider == nil {
			return "", newCloudError(
				CloudErrInvalidInput,
				"request remote owner pairing code",
				nil,
			)
		}
		if err := session.requireHeld(); err != nil {
			return "", err
		}
		snapshot, existed, err := readOptionalFile(
			session.paths.ConfigFile,
		)
		if err != nil {
			return "", fmt.Errorf(
				"snapshot configuration before Owner confirmation: %w",
				err,
			)
		}
		session.releaseIfHeld()

		code, promptErr := provider(prompt)
		reacquireErr := session.reacquire()
		if reacquireErr == nil {
			reacquireErr = ensureOptionalFileSnapshotCurrent(
				session.paths.ConfigFile,
				snapshot,
				existed,
			)
		}
		if reacquireErr != nil {
			stateErr := fmt.Errorf(
				"server configuration changed while waiting for Owner confirmation; the code was not used: %w",
				reacquireErr,
			)
			if promptErr != nil {
				return "", errors.Join(promptErr, stateErr)
			}
			return "", stateErr
		}
		return code, promptErr
	}
}

func (session *pausableClientMutationLock) requireHeld() error {
	if session == nil || !session.held {
		return errors.New("HA NOVA client mutation lock is not held")
	}
	return nil
}

func (session *pausableClientMutationLock) releaseIfHeld() {
	if session == nil || !session.held {
		return
	}
	session.held = false
	release := session.release
	session.release = nil
	if release != nil {
		release()
	}
}

func (session *pausableClientMutationLock) reacquire() error {
	if session == nil || session.held {
		return errors.New("invalid HA NOVA client mutation-lock state")
	}
	release, acquired := acquireAutoRepairLockUntil(
		session.paths,
		time.Now().Add(cloudRecoveryCheckpointLockTimeout),
	)
	if !acquired {
		return errors.New("another HA NOVA client update is still in progress")
	}
	session.release = release
	session.held = true
	return nil
}
