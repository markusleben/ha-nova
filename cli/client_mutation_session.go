package main

import (
	"errors"
	"fmt"
	"time"
)

type pausableClientMutationLock struct {
	paths          runtimePaths
	release        func()
	held           bool
	paused         bool
	pauseSnapshot  []byte
	pauseHadConfig bool
	pauseReason    string
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
		if err := session.pauseForExternalWait("Owner confirmation"); err != nil {
			return "", err
		}

		code, promptErr := provider(prompt)
		reacquireErr := session.resumeAfterExternalWait()
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

func (session *pausableClientMutationLock) oauthAuthorizationPause() error {
	return session.pauseForExternalWait("Home Assistant OAuth authorization")
}

func (session *pausableClientMutationLock) oauthAuthorizationResume() error {
	return session.resumeAfterExternalWait()
}

func (session *pausableClientMutationLock) pauseForExternalWait(
	reason string,
) error {
	if err := session.requireHeld(); err != nil {
		return err
	}
	if session.paused {
		return errors.New("HA NOVA client mutation lock is already paused")
	}
	snapshot, existed, err := readOptionalFile(session.paths.ConfigFile)
	if err != nil {
		return fmt.Errorf(
			"snapshot configuration before %s: %w",
			reason,
			err,
		)
	}
	session.paused = true
	session.pauseSnapshot = snapshot
	session.pauseHadConfig = existed
	session.pauseReason = reason
	session.releaseIfHeld()
	return nil
}

func (session *pausableClientMutationLock) resumeAfterExternalWait() error {
	if session == nil || !session.paused {
		return errors.New("HA NOVA client mutation lock is not paused")
	}
	reason := session.pauseReason
	if err := session.reacquire(); err != nil {
		return fmt.Errorf(
			"reacquire mutation lock after %s: %w",
			reason,
			err,
		)
	}
	snapshot := session.pauseSnapshot
	hadConfig := session.pauseHadConfig
	session.paused = false
	session.pauseSnapshot = nil
	session.pauseHadConfig = false
	session.pauseReason = ""
	if err := ensureOptionalFileSnapshotCurrent(
		session.paths.ConfigFile,
		snapshot,
		hadConfig,
	); err != nil {
		return fmt.Errorf(
			"verify configuration after %s: %w",
			reason,
			err,
		)
	}
	return nil
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
