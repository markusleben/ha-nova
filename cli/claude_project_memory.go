package main

import (
	"os"
	"path/filepath"
	"strings"
)

var claudeProjectMemoryMarkers = []string{
	"ha nova",
	"ha-nova",
	"nova relay",
	"ha-nova-skills.md",
	"ha-nova:",
	"~/.config/ha-nova",
	"ha-nova.relay-auth-token",
}

func removeClaudeProjectMemoryWithReport(home string, report *uninstallReport) error {
	if strings.TrimSpace(home) == "" {
		return nil
	}
	projectsRoot := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(projectsRoot); err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}

	projectEntries, err := os.ReadDir(projectsRoot)
	if err != nil {
		return err
	}
	for _, entry := range projectEntries {
		if !entry.IsDir() {
			continue
		}
		memoryDir := filepath.Join(projectsRoot, entry.Name(), "memory")
		memoryEntries, err := os.ReadDir(memoryDir)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return err
		}
		for _, memoryEntry := range memoryEntries {
			if memoryEntry.IsDir() {
				continue
			}
			path := filepath.Join(memoryDir, memoryEntry.Name())
			switch memoryEntry.Name() {
			case "ha-nova-skills.md":
				if err := noteClaudeProjectMemoryFile(path, report); err != nil {
					return err
				}
			case "MEMORY.md":
				if err := noteClaudeProjectMemoryIndex(path, report); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func noteClaudeProjectMemoryFile(path string, report *uninstallReport) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	if !containsClaudeProjectMemoryMarker(string(data)) {
		return nil
	}
	report.addNote("Claude project memory may still mention HA NOVA: " + path + " (review /memory if Claude still references removed skills)")
	return nil
}

func noteClaudeProjectMemoryIndex(path string, report *uninstallReport) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}

	content := string(data)
	if !containsClaudeProjectMemoryMarker(content) {
		return nil
	}
	report.addNote("Claude project memory may still mention HA NOVA: " + path + " (review /memory if Claude still references removed skills)")
	return nil
}

func containsClaudeProjectMemoryMarker(content string) bool {
	lower := strings.ToLower(content)
	for _, marker := range claudeProjectMemoryMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
