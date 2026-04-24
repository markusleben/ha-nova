package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type setupSecureStorageRecoveryResult string
type setupSecureStorageRecoveryAttempt string
type platformSecureStorageRecoveryAction string

type platformSecureStorageRecoveryPlan struct {
	action             platformSecureStorageRecoveryAction
	title              string
	paragraphs         []string
	prompt             string
	secretLabel        string
	confirmSecretLabel string
	successMessage     string
	hint               string
}

const (
	setupSecureStorageRecoveryRecovered setupSecureStorageRecoveryResult = "recovered"
	setupSecureStorageRecoveryDeclined  setupSecureStorageRecoveryResult = "declined"

	setupSecureStorageRecoveryInitialAttempt   setupSecureStorageRecoveryAttempt = "initial"
	setupSecureStorageRecoverySaveRetryAttempt setupSecureStorageRecoveryAttempt = "save-retry"

	platformSecureStorageRecoveryUnlock     platformSecureStorageRecoveryAction = "unlock"
	platformSecureStorageRecoveryInitialize platformSecureStorageRecoveryAction = "initialize"
)

type setupSecureStorageRecoveryState struct {
	initialAttempted   bool
	saveRetryAttempted bool
}

var errLocalSecureStoragePasswordRejected = errors.New("local secure storage password rejected")

var detectPlatformSecureStorageRecoverySupportForSetup = detectPlatformSecureStorageRecoverySupport
var inferPlatformSecureStorageRecoveryActionForSetup = inferPlatformSecureStorageRecoveryAction
var runPlatformSecureStorageRecoveryForSetup = runPlatformSecureStorageRecovery
var readSetupSecretInputForSetup = term.ReadPassword

func inferBasicSetupSecureStorageRecoveryAction(err error) platformSecureStorageRecoveryAction {
	switch {
	case isDesktopKeyringInitializationRequiredError(err):
		return platformSecureStorageRecoveryInitialize
	case isDesktopKeyringLockedError(err):
		return platformSecureStorageRecoveryUnlock
	default:
		return ""
	}
}

func setupSecureStorageRecoveryHint(err error) string {
	plan, available, supportErr := resolveSetupSecureStorageRecoveryPlan(err)
	if supportErr != nil || !available {
		return ""
	}
	return fmt.Sprintf("Recovery: run `ha-nova setup` interactively to %s.", plan.hint)
}

func setupSecureStorageRecoveryAvailableNow(err error) bool {
	if !uiInputSupportsTTY() || !writerSupportsTTYForSetup(os.Stdout) {
		return false
	}
	_, available, supportErr := resolveSetupSecureStorageRecoveryPlan(err)
	return supportErr == nil && available
}

func promptSetupSecretInput(out io.Writer, label string) ([]byte, error) {
	if !uiInputSupportsTTY() || !writerSupportsTTYForSetup(out) {
		return nil, fmt.Errorf("secure storage recovery requires an interactive terminal")
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s: ", label)
	secret, err := readSetupSecretInputForSetup(int(os.Stdin.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

func zeroSecretBytes(secret []byte) {
	for i := range secret {
		secret[i] = 0
	}
}

func markSetupSecureStorageRecoveryAttempt(state *setupSecureStorageRecoveryState, attempt setupSecureStorageRecoveryAttempt) {
	switch attempt {
	case setupSecureStorageRecoverySaveRetryAttempt:
		state.saveRetryAttempted = true
	default:
		state.initialAttempted = true
	}
}

func isRetryableSetupSecureStorageRecoveryError(err error) bool {
	if errors.Is(err, errLocalSecureStoragePasswordRejected) {
		return true
	}
	if isDesktopKeyringLockedError(err) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "local secure storage is still locked on this linux machine")
}

func localSecureStoragePasswordRejectedError() error {
	return fmt.Errorf("%w: local Linux keyring password was rejected", errLocalSecureStoragePasswordRejected)
}

func runSetupSecureStorageRecoveryFlow(reader *bufio.Reader, out io.Writer, triggerErr error, state *setupSecureStorageRecoveryState, attempt setupSecureStorageRecoveryAttempt) (setupSecureStorageRecoveryResult, error) {
	if state == nil {
		return "", errors.New("secure storage recovery state missing")
	}

	recoveryErr := triggerErr
	for {
		plan, available, err := resolveSetupSecureStorageRecoveryPlan(recoveryErr)
		if !available {
			if err == nil {
				err = errors.New("local secure storage recovery is unavailable")
			}
			return "", err
		}

		renderSetupSecureStorageRecoveryPage(out, plan)
		unlockNow, err := promptWizardYesNoFromReader(reader, out, plan.prompt, true)
		if err != nil {
			return "", err
		}
		markSetupSecureStorageRecoveryAttempt(state, attempt)
		if !unlockNow {
			return setupSecureStorageRecoveryDeclined, nil
		}

		secret, err := promptSetupSecretInput(out, plan.secretLabel)
		if err != nil {
			return "", err
		}
		if len(secret) == 0 {
			zeroSecretBytes(secret)
			renderSetupErrorLine(out, "No password entered.")
			continue
		}
		if plan.confirmSecretLabel != "" {
			confirmSecret, confirmErr := promptSetupSecretInput(out, plan.confirmSecretLabel)
			if confirmErr != nil {
				zeroSecretBytes(secret)
				return "", confirmErr
			}
			if len(confirmSecret) == 0 {
				zeroSecretBytes(secret)
				zeroSecretBytes(confirmSecret)
				renderSetupErrorLine(out, "No password entered.")
				continue
			}
			if !bytes.Equal(secret, confirmSecret) {
				zeroSecretBytes(secret)
				zeroSecretBytes(confirmSecret)
				renderSetupErrorLine(out, "Passwords did not match.")
				continue
			}
			zeroSecretBytes(confirmSecret)
		}

		runErr := runPlatformSecureStorageRecoveryForSetup(plan.action, secret)
		zeroSecretBytes(secret)
		if runErr == nil {
			renderSetupSuccessLine(out, "%s", plan.successMessage)
			return setupSecureStorageRecoveryRecovered, nil
		}
		if !isRetryableSetupSecureStorageRecoveryError(runErr) {
			return "", runErr
		}
		if isDesktopKeyringSetupRequiredError(runErr) {
			recoveryErr = runErr
		}

		renderSetupErrorLine(out, "%s", runErr)
	}
}

func resolveSetupSecureStorageRecoveryPlan(err error) (platformSecureStorageRecoveryPlan, bool, error) {
	if !isDesktopKeyringSetupRequiredError(err) {
		return platformSecureStorageRecoveryPlan{}, false, nil
	}
	supported, supportErr := detectPlatformSecureStorageRecoverySupportForSetup()
	if supportErr != nil || !supported {
		return platformSecureStorageRecoveryPlan{}, false, supportErr
	}
	action, actionErr := inferPlatformSecureStorageRecoveryActionForSetup(err)
	if actionErr != nil {
		return platformSecureStorageRecoveryPlan{}, false, actionErr
	}
	if action == "" {
		action = inferBasicSetupSecureStorageRecoveryAction(err)
	}
	if action == "" {
		return platformSecureStorageRecoveryPlan{}, false, nil
	}
	return secureStorageRecoveryPlanForAction(action), true, nil
}

func renderSetupSecureStorageRecoveryPage(out io.Writer, plan platformSecureStorageRecoveryPlan) {
	renderSetupSectionTitle(out, plan.title)
	renderSetupParagraph(out, plan.paragraphs...)
}

func secureStorageRecoveryPlanForAction(action platformSecureStorageRecoveryAction) platformSecureStorageRecoveryPlan {
	switch action {
	case platformSecureStorageRecoveryInitialize:
		return platformSecureStorageRecoveryPlan{
			action: platformSecureStorageRecoveryInitialize,
			title:  "Local secure storage needs setup",
			paragraphs: []string{
				"HA NOVA needs local secure storage before it can save the Relay Auth Token on this Linux machine.",
				"This step creates the local Linux keyring password this computer will ask for when the keyring needs to be unlocked later, not the Relay token or the Home Assistant token.",
				"It stays only on this Linux machine. HA NOVA, NOVA Relay, and Home Assistant never receive it.",
			},
			prompt:             "Set up local secure storage now",
			secretLabel:        "New local Linux keyring password",
			confirmSecretLabel: "Repeat local Linux keyring password",
			successMessage:     "Local secure storage is ready.",
			hint:               "set up local secure storage on this Linux machine",
		}
	default:
		return platformSecureStorageRecoveryPlan{
			action: platformSecureStorageRecoveryUnlock,
			title:  "Local secure storage is locked",
			paragraphs: []string{
				"HA NOVA needs local secure storage before it can save the Relay Auth Token on this Linux machine.",
				"This is the existing local Linux keyring password on this computer, not the Relay token or the Home Assistant token.",
				"It stays only on this Linux machine. HA NOVA, NOVA Relay, and Home Assistant never receive it.",
			},
			prompt:         "Unlock local secure storage now",
			secretLabel:    "Local Linux keyring password",
			successMessage: "Local secure storage is ready.",
			hint:           "unlock local secure storage on this Linux machine",
		}
	}
}
