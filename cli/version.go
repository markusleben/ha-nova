package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// compareSemver compares two semver strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareSemver(a, b string) int {
	ap := parseSemver(a)
	bp := parseSemver(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(s string) [3]int {
	parts := strings.SplitN(s, ".", 3)
	var v [3]int
	for i := range parts {
		if i < 3 {
			v[i], _ = strconv.Atoi(parts[i])
		}
	}
	return v
}

type versionJSON struct {
	SkillVersion    string `json:"skill_version"`
	MinRelayVersion string `json:"min_relay_version"`
}

// readMinRelayVersion reads min_relay_version from version.json in the given directory.
func readMinRelayVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "version.json"))
	if err != nil {
		return ""
	}
	var v versionJSON
	if json.Unmarshal(data, &v) != nil {
		return ""
	}
	return v.MinRelayVersion
}

// findVersionJSON searches for version.json in git root, then ~/.config/ha-nova/.
func findVersionJSON() string {
	// Try git root first
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		dir := strings.TrimSpace(string(out))
		if _, err := os.Stat(filepath.Join(dir, "version.json")); err == nil {
			return dir
		}
	}

	// Fall back to config dir
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	configDir := filepath.Join(home, ".config", "ha-nova")
	if _, err := os.Stat(filepath.Join(configDir, "version.json")); err == nil {
		return configDir
	}
	return ""
}

// runVersionCheckHook executes ~/.config/ha-nova/version-check if it exists and is executable.
func runVersionCheckHook() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	hook := filepath.Join(home, ".config", "ha-nova", "version-check")
	info, err := os.Stat(hook)
	if err != nil || info.Mode()&0o111 == 0 {
		return
	}
	cmd := exec.Command(hook)
	cmd.Stdout = os.Stdout // version-check notices appear before health JSON (matches relay.sh)
	cmd.Stderr = os.Stderr
	cmd.Run() // ignore errors — hook is optional
}

// checkRelayVersion compares relay version from health JSON against min_relay_version.
func checkRelayVersion(healthBody []byte) {
	var health struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(healthBody, &health) != nil || health.Version == "" {
		return
	}

	dir := findVersionJSON()
	if dir == "" {
		return
	}

	minV := readMinRelayVersion(dir)
	if minV == "" {
		return
	}

	if compareSemver(health.Version, minV) < 0 {
		fmt.Fprintf(os.Stdout, "⚠️ RELAY OUTDATED: v%s is below minimum v%s — Inform the user: update the NOVA Relay App in Home Assistant.\n", health.Version, minV)
	}
}
