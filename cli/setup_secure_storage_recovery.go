package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type setupSecureStorageRecoveryResult string
type setupSecureStorageRecoveryAttempt string
type platformSecureStorageRecoveryAction string

type platformSecureStorageRecoveryPlan struct {
	action         platformSecureStorageRecoveryAction
	title          string
	paragraphs     []string
	prompt         string
	successMessage string
	hint           string
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
	unlockOnly         bool // uninstall: never offer to create a keyring
}

var errLocalSecureStoragePromptCanceled = errors.New("local secure storage prompt canceled")

var detectPlatformSecureStorageRecoverySupportForSetup = detectPlatformSecureStorageRecoverySupport
var inferPlatformSecureStorageRecoveryActionForSetup = inferPlatformSecureStorageRecoveryAction
var runPlatformSecureStorageRecoveryForSetup = runPlatformSecureStorageRecovery
var platformSecureStorageRecoveryInteractiveAvailableForSetup = platformNativeSecretPromptAvailable

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
	if !platformSecureStorageRecoveryInteractiveAvailableForSetup() {
		return false
	}
	_, available, supportErr := resolveSetupSecureStorageRecoveryPlan(err)
	return supportErr == nil && available
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
	if isDesktopKeyringLockedError(err) {
		return true
	}
	if isDesktopKeyringInitializationRequiredError(err) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "local secure storage is still locked on this linux machine")
}

func runSetupSecureStorageRecoveryFlow(reader *bufio.Reader, out io.Writer, triggerErr error, state *setupSecureStorageRecoveryState, attempt setupSecureStorageRecoveryAttempt) (setupSecureStorageRecoveryResult, error) {
	if state == nil {
		return "", errors.New("secure storage recovery state missing")
	}
	if !uiInputSupportsTTY() || !writerSupportsTTYForSetup(out) {
		return "", errors.New("secure storage recovery requires an interactive desktop terminal")
	}
	if !platformSecureStorageRecoveryInteractiveAvailableForSetup() {
		return "", errors.New("secure storage recovery requires a local graphical desktop session")
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

		if state.unlockOnly && plan.action != platformSecureStorageRecoveryUnlock {
			return "", recoveryErr
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

		runErr := runPlatformSecureStorageRecoveryForSetup(plan.action)
		if runErr == nil {
			renderSetupSuccessLine(out, "%s", plan.successMessage)
			return setupSecureStorageRecoveryRecovered, nil
		}
		if errors.Is(runErr, errLocalSecureStoragePromptCanceled) {
			return setupSecureStorageRecoveryDeclined, nil
		}
		if !isRetryableSetupSecureStorageRecoveryError(runErr) {
			return "", runErr
		}
		if isDesktopKeyringSetupRequiredError(runErr) {
			recoveryErr = runErr
		}

		switch {
		case isDesktopKeyringLockedError(runErr):
			renderSetupErrorLine(out, "The keyring is still locked. Try again, or answer n to stop.")
		case isDesktopKeyringInitializationRequiredError(runErr):
			renderSetupErrorLine(out, "The keyring still isn't set up. Try again, or answer n to stop.")
		default:
			renderSetupErrorLine(out, "%s", runErr)
		}
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
			title:  "This computer's keyring isn't set up yet",
			paragraphs: []string{
				"HA NOVA keeps its key in this computer's keyring, and the keyring isn't set up yet.",
				"A system window opens to create it. Type a password only in that window; HA NOVA never sees it.",
			},
			prompt:         "Open the window to set up the keyring now",
			successMessage: "The keyring is ready.",
			hint:           "set up this computer's keyring",
		}
	default:
		return platformSecureStorageRecoveryPlan{
			action: platformSecureStorageRecoveryUnlock,
			title:  "This computer's keyring is locked",
			paragraphs: []string{
				"HA NOVA keeps its key in this computer's keyring, and the keyring is locked right now.",
				"A system window opens to unlock it. Type your login password only in that window; HA NOVA never sees it.",
			},
			prompt:         "Open the window to unlock the keyring now",
			successMessage: "The keyring is unlocked.",
			hint:           "unlock this computer's keyring",
		}
	}
}
