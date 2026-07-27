//go:build darwin

package main

func platformNativePromptRunsUnderWSL() bool {
	return false
}
