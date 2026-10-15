package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPathContainsEntryMatchesWholeSegmentOnly(t *testing.T) {
	pathValue := `C:\Tools;C:\Users\markus\.local\share\ha-nova-backup;C:\Other`
	target := `C:\Users\markus\.local\share\ha-nova`
	if pathContainsEntry(pathValue, target) {
		t.Fatalf("expected substring-only match to be rejected")
	}
}

func TestPathContainsEntryMatchesCaseInsensitiveSegment(t *testing.T) {
	pathValue := `C:\Tools;C:\Users\Markus\.LOCAL\share\HA-NOVA`
	target := `c:\users\markus\.local\share\ha-nova`
	if !pathContainsEntry(pathValue, target) {
		t.Fatalf("expected case-insensitive segment match")
	}
}

func TestRemoveManagedPathSkipsUnixCleanupWhenPathNotManaged(t *testing.T) {
	tmp := t.TempDir()
	rcFile := filepath.Join(tmp, ".zshrc")
	original := "# existing\nexport PATH=\"$HOME/.local/bin:$PATH\"\n"
	if err := os.WriteFile(rcFile, []byte(original), 0o644); err != nil {
		t.Fatalf("write rc file: %v", err)
	}

	removeManagedPath(runtimePaths{}, installState{
		PathManaged: false,
		PathTarget:  rcFile,
	})

	got, err := os.ReadFile(rcFile)
	if err != nil {
		t.Fatalf("read rc file: %v", err)
	}
	if string(got) != original {
		t.Fatalf("unexpected rc mutation:\n%s", string(got))
	}
}

func TestRemoveManagedPathWithReportFailsLoudWhenUnixWriteFails(t *testing.T) {
	tmp := t.TempDir()
	rcFile := filepath.Join(tmp, ".zshrc")
	if err := os.WriteFile(rcFile, []byte(pathBlockHeader+"\n"+pathExportLine+"\n"+pathBlockFooter+"\n"), 0o444); err != nil {
		t.Fatalf("write rc file: %v", err)
	}

	removed, err := removeManagedPathWithReport(runtimePaths{}, installState{
		PathManaged: true,
		PathTarget:  rcFile,
	})
	if err == nil {
		t.Fatal("expected write failure")
	}
	if removed != "" {
		t.Fatalf("did not expect removal to be reported on failure, got %q", removed)
	}
	if !strings.Contains(err.Error(), "permission") {
		t.Fatalf("expected permission failure, got %v", err)
	}
}

func TestRemoveManagedPathWithReportDoesNotStripGenericLocalBinExportWithoutHANOVABlock(t *testing.T) {
	tmp := t.TempDir()
	rcFile := filepath.Join(tmp, ".zshrc")
	original := "# existing\nexport PATH=\"$HOME/.local/bin:$PATH\"\nexport PATH=\"$HOME/bin:$PATH\"\n"
	if err := os.WriteFile(rcFile, []byte(original), 0o644); err != nil {
		t.Fatalf("write rc file: %v", err)
	}

	removed, err := removeManagedPathWithReport(runtimePaths{}, installState{
		PathManaged: true,
		PathTarget:  rcFile,
	})
	if err != nil {
		t.Fatalf("removeManagedPathWithReport() error: %v", err)
	}
	if removed != "" {
		t.Fatalf("did not expect PATH cleanup to be reported, got %q", removed)
	}

	got, err := os.ReadFile(rcFile)
	if err != nil {
		t.Fatalf("read rc file: %v", err)
	}
	if string(got) != original {
		t.Fatalf("expected generic PATH line to stay untouched:\n%s", string(got))
	}
}

func TestRemoveManagedPathWithReportSkipsWindowsUserPathBranchOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only regression")
	}

	removed, err := removeManagedPathWithReport(runtimePaths{}, installState{
		PathManaged: false,
		PathTarget:  "user-path",
	})
	if err != nil {
		t.Fatalf("expected unix cleanup to ignore Windows user-path marker, got %v", err)
	}
	if removed != "" {
		t.Fatalf("did not expect cleanup report, got %q", removed)
	}
}

func TestBuildSetWindowsUserPathCommandKeepsValueOffTheCommandLine(t *testing.T) {
	longPath := strings.Repeat(`C:\Users\test\some\very\long\path\entry;`, 800) // ~32K, beyond the CreateProcess limit
	cmd := buildSetWindowsUserPathCommand(longPath)

	for _, arg := range cmd.Args {
		if strings.Contains(arg, "very\\long\\path") {
			t.Fatalf("PATH value must not appear on the command line")
		}
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "$env:HA_NOVA_SET_USER_PATH") {
		t.Fatalf("command must read the value from the environment, got %v", cmd.Args)
	}
	found := false
	for _, kv := range cmd.Env {
		if kv == "HA_NOVA_SET_USER_PATH="+longPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the PATH value in the command environment")
	}
}

func TestRemoveManagedPathSweepsEveryInstallerTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	home := t.TempDir()
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("SHELL", "/usr/bin/fish") // SHELL at uninstall time must not matter
	block := "\n" + pathBlockHeader + "\n" + pathExportLine + "\n"
	fishBlock := pathBlockHeader + "\n" + pathFishLine + "\n"
	zshrc := filepath.Join(home, ".zshrc") // pre-fix installer target for fish users
	profile := filepath.Join(home, ".profile")
	homeFish := filepath.Join(home, ".config", "fish", "conf.d", pathFishFile)
	xdgFish := filepath.Join(xdg, "fish", "conf.d", pathFishFile)
	writeTestFile(t, zshrc, "# mine\n"+block)
	writeTestFile(t, profile, "# keep\n")
	writeTestFile(t, homeFish, fishBlock)
	writeTestFile(t, xdgFish, "set -gx EDITOR vim\n"+fishBlock)

	removed, err := removeManagedPathWithReport(runtimePaths{Home: home}, installState{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{zshrc, homeFish, xdgFish} {
		if !strings.Contains(removed, path) {
			t.Fatalf("report %q misses %s", removed, path)
		}
	}
	if strings.Contains(removed, profile) {
		t.Fatalf("report names untouched %s: %q", profile, removed)
	}
	if got, _ := os.ReadFile(zshrc); strings.Contains(string(got), pathBlockHeader) || !strings.Contains(string(got), "# mine") {
		t.Fatalf(".zshrc = %q", got)
	}
	if _, err := os.Stat(homeFish); !os.IsNotExist(err) {
		t.Fatalf("HA NOVA-only fish file must be deleted: %v", err)
	}
	if got, _ := os.ReadFile(xdgFish); string(got) != "set -gx EDITOR vim\n" {
		t.Fatalf("shared fish file = %q", got)
	}
	if got, _ := os.ReadFile(profile); string(got) != "# keep\n" {
		t.Fatalf(".profile = %q", got)
	}
}
