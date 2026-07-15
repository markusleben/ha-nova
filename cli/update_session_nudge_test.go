package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostUpdateSessionInstructionDistinguishesSyncOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		result postUpdateSyncResult
		want   string
	}{
		{
			name:   "complete sync",
			result: postUpdateSyncResult{FullySynced: true},
			want:   postUpdateSessionInstruction,
		},
		{
			name:   "partial sync",
			result: postUpdateSyncResult{RefreshedClients: true},
			want:   postUpdatePartialSessionInstruction,
		},
		{
			name:   "all clients skipped",
			result: postUpdateSyncResult{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStdout(t, func() {
				printPostUpdateSessionInstruction(tt.result)
			})
			if tt.want == "" && output != "" {
				t.Fatalf("expected no session instruction, got %q", output)
			}
			if tt.want != "" && !strings.Contains(output, tt.want) {
				t.Fatalf("expected %q, got %q", tt.want, output)
			}
		})
	}
}

func TestPostUpdateSyncReportsPartialRefreshWhenOneClientIsSkipped(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true, "opencode": false})
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	t.Setenv("HA_NOVA_DEV_ROOT", filepath.Clean(filepath.Join(cwd, "..")))

	state := loadStateOrDefault(paths)
	state.InstalledClients = []string{"codex", "opencode"}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	result := postUpdateSyncWithResult(paths)
	if result.Err != nil {
		t.Fatalf("postUpdateSyncWithResult() error: %v", result.Err)
	}
	if result.FullySynced {
		t.Fatal("expected one skipped client to keep the result partial")
	}
	if !result.RefreshedClients {
		t.Fatal("expected the successfully synced Codex client to require a new session")
	}
}
