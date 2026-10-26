package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func claudeDesktopConfigHint(home string) string {
	if claudeDesktopPlatformOS != "darwin" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, "Library", "Application Support", "Claude", "ccd-environment-config.json"))
	if isNotExist(err) {
		return ""
	}
	var config struct {
		EnvVars string `json:"envVars"`
	}
	if err == nil && json.Unmarshal(data, &config) == nil && config.EnvVars == "" {
		return ""
	}
	return fmt.Sprintf("Claude Desktop's private Claude Code environment settings can't be verified here. HA NOVA uses %q; if Desktop's Claude Code config dir differs, HA NOVA may be missing there. Set CLAUDE_CONFIG_DIR to that directory and run `ha-nova setup claude` to install there too.", claudeConfigRoot(home))
}
