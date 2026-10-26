package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

var claudeDesktopPlatformOS = runtime.GOOS

func claudeRuntimePath() (string, error) {
	path, err := exec.LookPath("claude")
	if err == nil || !errors.Is(err, exec.ErrNotFound) {
		return path, err
	}
	home, homeErr := os.UserHomeDir()
	if homeErr == nil && executableInUserLocalBin("claude") {
		return filepath.Join(home, ".local", "bin", "claude"), nil
	}
	if homeErr == nil && claudeDesktopPlatformOS == "darwin" {
		if bundled := claudeDesktopRuntime(home); bundled != "" {
			return bundled, nil
		}
	}
	return "claude", err
}

func claudeCommand(args ...string) *exec.Cmd {
	path, err := claudeRuntimePath()
	cmd := exec.Command(path, args...)
	if err != nil {
		cmd.Err = err
	}
	return cmd
}

func claudeDesktopRuntime(home string) string {
	root := filepath.Join(home, "Library", "Application Support", "Claude", "claude-code")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	versions := []string{}
	for _, entry := range entries {
		if _, err := parseReleaseVersion(entry.Name()); err == nil && entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		cmp, _ := compareReleaseVersions(versions[i], versions[j])
		return cmp > 0
	})
	for _, version := range versions {
		installs, _ := os.ReadDir(filepath.Join(root, version))
		for _, install := range installs {
			if !install.IsDir() || len(install.Name()) != 12 {
				continue
			}
			dir := filepath.Join(root, version, install.Name())
			marker, err := os.ReadFile(filepath.Join(dir, ".verified"))
			checksum := strings.TrimSpace(string(marker))
			if err != nil || len(checksum) != 64 || !strings.HasPrefix(checksum, install.Name()) || strings.Trim(checksum, "0123456789abcdef") != "" {
				continue
			}
			path := filepath.Join(dir, "claude.app", "Contents", "MacOS", "claude")
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
				continue
			}
			if claudeDesktopRuntimeWorks(path, version) {
				return path
			}
		}
	}
	return ""
}

func claudeDesktopRuntimeWorks(path, version string) bool {
	for _, probe := range []struct {
		args []string
		want []string
	}{
		{[]string{"--version"}, []string{version + " (Claude Code)"}},
		{[]string{"plugin", "--help"}, []string{"install", "update", "remove", "list", "validate"}},
		{[]string{"plugin", "marketplace", "--help"}, []string{"add", "remove", "update"}},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, path, probe.args...)
		cmd.WaitDelay = 100 * time.Millisecond
		output, err := cmd.Output()
		cancel()
		if err != nil {
			return false
		}
		for _, want := range probe.want {
			if !strings.Contains(string(output), want) {
				return false
			}
		}
	}
	return true
}
