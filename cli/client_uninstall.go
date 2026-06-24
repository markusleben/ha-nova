package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func removeInstalledClients(paths runtimePaths, state installState) error {
	return removeInstalledClientsWithReport(paths, state, nil)
}

func removeInstalledClientsWithReport(paths runtimePaths, state installState, report *uninstallReport) error {
	clients := append([]string{}, state.InstalledClients...)
	if hermesBundlePresent(paths.Home) || hermesLegacyBundlePresent(paths.Home) {
		clients = normalizeClients(append(clients, "hermes"))
	}
	if len(clients) == 0 {
		clients = []string{"claude", "codex", "opencode", "antigravity", "hermes"}
	}
	sort.Strings(clients)
	for _, client := range clients {
		switch client {
		case "claude":
			if err := removeSkillEntriesWithReport(filepath.Join(paths.Home, ".claude", "skills"), report); err != nil {
				return err
			}
			if !claudePluginInstalled(paths.Home) {
				if err := removeClaudePluginRecord(paths.Home); err != nil {
					return fmt.Errorf("claude plugin removal failed: %w", err)
				}
				if err := removeClaudeMarketplace(paths.Home, report); err != nil {
					return err
				}
				if err := removePathWithReport(filepath.Join(paths.Home, ".claude", "plugins", "cache", "ha-nova"), report); err != nil {
					return err
				}
				continue
			}
			if _, err := exec.LookPath("claude"); err != nil {
				if err := removeClaudePluginRecord(paths.Home); err != nil {
					return fmt.Errorf("claude plugin removal failed: %w", err)
				}
				if err := removeClaudeMarketplace(paths.Home, report); err != nil {
					return err
				}
				if err := removePathWithReport(filepath.Join(paths.Home, ".claude", "plugins", "cache", "ha-nova"), report); err != nil {
					return err
				}
				continue
			}
			cmd := exec.Command("claude", "plugin", "remove", "ha-nova@ha-nova")
			if output, err := cmd.CombinedOutput(); err != nil {
				message := strings.TrimSpace(string(output))
				if strings.Contains(strings.ToLower(message), "not found in installed plugins") {
					if err := removeClaudePluginRecord(paths.Home); err != nil {
						return fmt.Errorf("claude plugin removal failed: %w", err)
					}
					if err := removeClaudeMarketplace(paths.Home, report); err != nil {
						return err
					}
					if err := removePathWithReport(filepath.Join(paths.Home, ".claude", "plugins", "cache", "ha-nova"), report); err != nil {
						return err
					}
					continue
				}
				return fmt.Errorf("claude plugin removal failed: %s", message)
			}
			if err := removeClaudePluginRecord(paths.Home); err != nil {
				return fmt.Errorf("claude plugin removal failed: %w", err)
			}
			report.addRemoved("Claude plugin ha-nova@ha-nova")
			if err := removeClaudeMarketplace(paths.Home, report); err != nil {
				return err
			}
			if err := removePathWithReport(filepath.Join(paths.Home, ".claude", "plugins", "cache", "ha-nova"), report); err != nil {
				return err
			}
		case "codex":
			if err := removeSkillEntriesWithReport(filepath.Join(paths.Home, ".agents", "skills"), report); err != nil {
				return err
			}
		case "opencode":
			if err := removeSkillEntriesWithReport(filepath.Join(paths.Home, ".config", "opencode", "skills"), report); err != nil {
				return err
			}
		case "antigravity":
			if err := removeSkillEntriesWithReport(antigravitySkillsRoot(paths.Home), report); err != nil {
				return err
			}
			if err := removeSkillEntriesWithReport(legacyGeminiSkillsRoot(paths.Home), report); err != nil {
				return err
			}
		case "hermes":
			if err := removeSkillEntriesWithReport(filepath.Join(paths.Home, ".hermes", "skills"), report); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeSkillEntriesWithReport(skillsRoot string, report *uninstallReport) error {
	matches, err := filepath.Glob(filepath.Join(skillsRoot, "ha-nova*"))
	if err != nil {
		return err
	}
	for _, match := range matches {
		if err := removePathWithReport(match, report); err != nil {
			return err
		}
	}
	return nil
}
