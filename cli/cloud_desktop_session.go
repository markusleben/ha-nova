package main

import (
	"io"
	"os"
	"strings"
)

var nativePromptProcessElevated = platformNativePromptProcessElevated
var nativePromptRunsUnderWSL = platformNativePromptRunsUnderWSL

func nativeSecretPromptSessionAvailable(out io.Writer) bool {
	return uiInputSupportsTTY() &&
		writerSupportsTTYForSetup(out) &&
		platformNativeSecretPromptContextAvailable()
}

func platformNativeSecretPromptAvailable() bool {
	return nativeSecretPromptSessionAvailable(os.Stdout)
}

func nativeSecretPromptBaseContextAvailable() bool {
	for _, name := range []string{
		"SSH_CONNECTION",
		"SSH_CLIENT",
		"SSH_TTY",
		"SUDO_USER",
		"SUDO_UID",
		"SUDO_GID",
		"DOAS_USER",
		"PKEXEC_UID",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return false
		}
	}
	return !nativePromptProcessElevated() && !nativePromptRunsUnderWSL()
}
